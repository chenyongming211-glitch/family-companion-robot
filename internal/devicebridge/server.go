// Package devicebridge is the isolated Xiaozhi/Opus adapter for the drain PCM service.
package devicebridge

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"familyrobot/internal/protocol"
	"github.com/gorilla/websocket"
)

const ticketLife = 120 * time.Second

type Config struct {
	BackendURL     string
	BackendToken   string
	SessionTimeout time.Duration
	KeepOpen       bool
}

type ticket struct {
	device  string
	expires time.Time
}

type Server struct {
	cfg         Config
	mu          sync.Mutex
	tickets     map[string]ticket
	connections map[*websocket.Conn]struct{}
	active      bool
	closed      bool
	ctx         context.Context
	cancel      context.CancelFunc
	jobs        sync.WaitGroup
}

func New(cfg Config) (*Server, error) {
	u, err := url.Parse(cfg.BackendURL)
	if err != nil || u.Scheme != "ws" || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.Path != "/v1/audio" || u.Query().Get("mode") != "drain" || u.Fragment != "" || cfg.BackendToken == "" || len(cfg.BackendToken) > 4096 {
		return nil, errors.New("loopback protected drain backend required")
	}
	if cfg.SessionTimeout == 0 {
		cfg.SessionTimeout = 15 * time.Minute
	}
	if cfg.SessionTimeout <= 0 || cfg.SessionTimeout > 15*time.Minute {
		return nil, errors.New("session timeout must be at most 15 minutes")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg, tickets: make(map[string]ticket), connections: make(map[*websocket.Conn]struct{}), ctx: ctx, cancel: cancel}, nil
}

func (s *Server) Active() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return 1
	}
	return 0
}
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	for c := range s.connections {
		c.Close()
	}
	s.mu.Unlock()
	s.jobs.Wait()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "engine": "livekit-device-drain", "active_sessions": s.Active(), "input_rate": 16000, "output_rate": 24000, "recording": false})
	})
	mux.HandleFunc("POST /v1/pair", s.pair)
	mux.HandleFunc("GET /xiaozhi/v1/", s.upgrade)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
func loopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	return err == nil && net.ParseIP(host).IsLoopback()
}
func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Pair is deliberately reachable only through a local/SSH-authorized route.
// It has no model or upstream token in its response, URL, or log output.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if !loopback(r.RemoteAddr) || r.Header.Get("Origin") != "" {
		http.Error(w, "local SSH pairing required", 403)
		return
	}
	var body struct {
		Device string `json:"device_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF || body.Device != "native-mac" {
		http.Error(w, "native device required", 400)
		return
	}
	id, err := randomID()
	if err != nil {
		http.Error(w, "pairing unavailable", 503)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.tickets {
		if !v.expires.After(now) {
			delete(s.tickets, k)
		}
	}
	if s.closed || len(s.tickets) >= 8 {
		http.Error(w, "pairing busy", 429)
		return
	}
	s.tickets[id] = ticket{device: body.Device, expires: now.Add(ticketLife)}
	writeJSON(w, map[string]any{"ticket": id, "expires_in": 120, "device_id": body.Device, "websocket_path": "/xiaozhi/v1/"})
}

func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) {
	if !loopback(r.RemoteAddr) || r.Header.Get("Origin") != "" {
		http.Error(w, "local native connection required", 403)
		return
	}
	version := r.Header.Get("Protocol-Version")
	if version != "" && version != "1" {
		http.Error(w, "protocol version 1 required", 400)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		http.Error(w, "invalid native ticket", 401)
		return
	}
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	t, ok := s.tickets[key]
	if !ok || !t.expires.After(time.Now()) || t.device != r.Header.Get("Device-Id") {
		s.mu.Unlock()
		http.Error(w, "invalid native ticket", 401)
		return
	}
	// Consume even when busy: a native authorization can never be replayed.
	delete(s.tickets, key)
	if s.closed || s.active {
		s.mu.Unlock()
		http.Error(w, "one native session allowed", 409)
		return
	}
	s.active = true
	s.jobs.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active = false; s.mu.Unlock(); s.jobs.Done() }()
	u := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	c, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.connections[c] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.connections, c); s.mu.Unlock() }()
	var ctx context.Context
	var cancel context.CancelFunc
	if s.cfg.KeepOpen {
		ctx, cancel = context.WithCancel(s.ctx)
	} else {
		ctx, cancel = context.WithTimeout(s.ctx, s.cfg.SessionTimeout)
	}
	defer cancel()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	c.SetReadLimit(16384)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, data, err := c.ReadMessage()
	if err != nil || kind != websocket.TextMessage {
		return
	}
	hello, err := protocol.ParseHello(data, 1)
	if err != nil || !hello.Features["drain"] || !hello.Features["staging"] || !hello.Features["coreaudio_playback"] {
		sendJSON(c, map[string]any{"type": "error", "code": "capability_required", "required": []string{"drain", "staging", "coreaudio_playback"}})
		return
	}
	up, err := s.connectBackend(ctx)
	if err != nil {
		sendJSON(c, map[string]any{"type": "error", "code": "backend_unavailable"})
		return
	}
	defer up.Close()
	stopUp := context.AfterFunc(ctx, func() { up.Close() })
	defer stopUp()
	up.SetReadLimit(16384)
	up.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, data, err = up.ReadMessage()
	if err != nil || kind != websocket.TextMessage {
		sendJSON(c, map[string]any{"type": "error", "code": "backend_unavailable"})
		return
	}
	ready, err := parseEvent(data)
	if err != nil || ready["type"] != "ready" || ready["mode"] != "drain" || integer(ready, "input_rate") != 16000 || integer(ready, "output_rate") != 24000 || (s.cfg.KeepOpen && ready["manual_close"] != true) {
		sendJSON(c, map[string]any{"type": "error", "code": "backend_capability_required"})
		return
	}
	id, err := randomID()
	if err != nil {
		return
	}
	if sendJSON(c, map[string]any{"type": "hello", "control_seq": 1, "version": 1, "transport": "websocket", "session_id": id, "audio_params": protocol.AudioParams{Format: "opus", SampleRate: 24000, Channels: 1, FrameDuration: 60}, "features": map[string]bool{"drain": true, "staging": true, "coreaudio_playback": true}, "playback_basis": "coreaudio_playback"}) != nil {
		return
	}
	ready["session_id"] = id
	ready["control_seq"] = 2
	if sendJSON(c, ready) != nil {
		return
	}
	sess, err := newSession(ctx, c, up, id, hello.AudioParams.FrameDuration)
	if err != nil {
		return
	}
	defer sess.close()
	if err = sess.run(); err != nil && ctx.Err() == nil {
		sendJSON(c, map[string]any{"type": "error", "code": "device_session_closed"})
	}
}

func (s *Server) connectBackend(ctx context.Context) (*websocket.Conn, error) {
	// A disconnected client releases the Go lease before AgentSession has
	// finished its bounded cleanup. A 409 has not created a new voice session
	// or sent any user audio, so wait only for that explicit busy response.
	wait, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		up, response, err := (&websocket.Dialer{HandshakeTimeout: 3 * time.Second}).DialContext(wait, s.cfg.BackendURL, http.Header{"Authorization": {"Bearer " + s.cfg.BackendToken}})
		if err == nil {
			return up, nil
		}
		busy := response != nil && response.StatusCode == http.StatusConflict
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if !busy {
			return nil, err
		}
		select {
		case <-wait.Done():
			return nil, wait.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func sendJSON(c *websocket.Conn, e any) error {
	c.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
	return c.WriteJSON(e)
}
