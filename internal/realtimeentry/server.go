// Package realtimeentry adds a loopback-only Mac test entrance to the existing
// Xiaozhi/Qwen gateway. It owns no voice, history, task or model protocol logic.
package realtimeentry

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"familyrobot/internal/gateway"
	"familyrobot/internal/qwen"
)

const DeviceID = "native-realtime"
const gatewayDevice = "02:00:00:00:30:01"

type Server struct {
	g                   *gateway.Server
	handler             http.Handler
	token, model, voice string
	mu                  sync.Mutex
	tickets             map[string]time.Time
	closed              bool
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func New(model qwen.Config) (*Server, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	bootstrap, err := randomToken()
	if err != nil {
		return nil, err
	}
	g, err := gateway.New(gateway.Config{DeviceToken: token, BootstrapSecret: bootstrap,
		PublicWSURL: "ws://127.0.0.1:18030/xiaozhi/v1/", AllowedDevice: gatewayDevice, Qwen: model,
		MaxSessions: 1, IdleTimeout: 24 * time.Hour, SessionTimeout: 24 * time.Hour})
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(model.URL)
	return &Server{g: g, handler: g.Handler(), token: token, model: u.Query().Get("model"), voice: model.Voice, tickets: make(map[string]time.Time)}, nil
}

func (s *Server) Close() { s.mu.Lock(); s.closed = true; clear(s.tickets); s.mu.Unlock(); s.g.Close() }
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(value)
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "backend": "qwen-native-existing", "model": s.model, "voice": s.voice, "active_sessions": s.g.Active(), "text_recording": false, "transport": "xiaozhi-opus-v1", "playback_receipts": false})
	})
	mux.HandleFunc("POST /v1/pair", s.pair)
	mux.HandleFunc("GET /xiaozhi/v1/", s.upgrade)
	return mux
}
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "native client required", 403)
		return
	}
	var body struct {
		DeviceID string `json:"device_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil || body.DeviceID != DeviceID || dec.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid pairing", 400)
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		http.Error(w, "closing", 503)
		return
	}
	for ticket, expires := range s.tickets {
		if !now.Before(expires) {
			delete(s.tickets, ticket)
		}
	}
	if len(s.tickets) >= 8 || s.g.Active() > 0 {
		http.Error(w, "test session busy", 429)
		return
	}
	token, err := randomToken()
	if err != nil {
		http.Error(w, "pairing unavailable", 503)
		return
	}
	s.tickets[token] = now.Add(120 * time.Second)
	writeJSON(w, map[string]any{"ticket": token, "expires_in": 120, "device_id": DeviceID, "backend": "qwen-native-existing", "model": s.model, "voice": s.voice, "transport": "xiaozhi-opus-v1"})
}
func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" || r.Header.Get("Device-Id") != DeviceID || r.Header.Get("Protocol-Version") != "1" || r.URL.RawQuery != "" {
		http.Error(w, "invalid native request", 403)
		return
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) != len(prefix)+43 || auth[:len(prefix)] != prefix {
		http.Error(w, "unauthorized", 401)
		return
	}
	ticket := auth[len(prefix):]
	s.mu.Lock()
	expires, ok := s.tickets[ticket]
	delete(s.tickets, ticket)
	closed := s.closed
	s.mu.Unlock()
	if closed || !ok || !time.Now().Before(expires) {
		http.Error(w, "unauthorized", 401)
		return
	}
	// The long-lived/internal gateway token never leaves this process. The test
	// MAC is separate from the real watch; recording is disabled for this instance.
	cloned := r.Clone(r.Context())
	cloned.Header = r.Header.Clone()
	cloned.Header.Set("Authorization", "Bearer "+s.token)
	cloned.Header.Set("Device-Id", gatewayDevice)
	s.handler.ServeHTTP(w, cloned)
}
