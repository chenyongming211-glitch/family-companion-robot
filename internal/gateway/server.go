// Package gateway bridges Xiaozhi devices to native Qwen Realtime audio.
package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

type Config struct {
	DeviceToken, BootstrapSecret, PublicWSURL, AllowedDevice string
	Qwen                                                     qwen.Config
	MaxSessions                                              int
	IdleTimeout, SessionTimeout                              time.Duration
}
type Server struct {
	cfg    Config
	slots  chan struct{}
	mu     sync.Mutex
	conns  map[*websocket.Conn]struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg Config) (*Server, error) {
	if len(cfg.DeviceToken) < 32 || len(cfg.BootstrapSecret) < 32 || strings.ContainsAny(cfg.BootstrapSecret, "/?# ") {
		return nil, errors.New("device token and URL-safe bootstrap secret must be at least 32 characters")
	}
	u, err := url.Parse(cfg.PublicWSURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return nil, errors.New("invalid public WebSocket URL")
	}
	u, err = url.Parse(cfg.Qwen.URL)
	if err != nil || u.Host == "" || (u.Scheme != "wss" && !(u.Scheme == "ws" && net.ParseIP(u.Hostname()).IsLoopback())) || u.User != nil || cfg.Qwen.APIKey == "" {
		return nil, errors.New("upstream requires a secure URL and API key")
	}
	if cfg.AllowedDevice != "" {
		m, err := net.ParseMAC(cfg.AllowedDevice)
		if err != nil {
			return nil, errors.New("invalid allowed device MAC")
		}
		cfg.AllowedDevice = strings.ToLower(m.String())
	}
	if cfg.MaxSessions < 1 || cfg.MaxSessions > 8 {
		return nil, errors.New("max sessions must be 1-8")
	}
	if cfg.IdleTimeout <= 0 || cfg.SessionTimeout <= 0 {
		return nil, errors.New("session timeouts must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg, slots: make(chan struct{}, cfg.MaxSessions), conns: make(map[*websocket.Conn]struct{}), ctx: ctx, cancel: cancel}, nil
}
func (s *Server) Active() int { return len(s.slots) }
func (s *Server) Close() {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.Close()
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "active_sessions": s.Active()})
	})
	mux.HandleFunc("/xiaozhi/ota/"+s.cfg.BootstrapSecret+"/", s.ota)
	mux.HandleFunc("/xiaozhi/v1/", s.upgrade)
	return mux
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
func (s *Server) deviceAllowed(r *http.Request) bool {
	m, err := net.ParseMAC(r.Header.Get("Device-Id"))
	if err != nil || len(m) != 6 {
		return false
	}
	return s.cfg.AllowedDevice == "" || strings.EqualFold(m.String(), s.cfg.AllowedDevice)
}
func (s *Server) ota(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/xiaozhi/ota/"+s.cfg.BootstrapSecret+"/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.deviceAllowed(r) {
		http.Error(w, "device not allowed", http.StatusForbidden)
		return
	}
	var body struct {
		Application struct {
			Version string `json:"version"`
		} `json:"application"`
	}
	if r.Method == "POST" {
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&body) != nil {
			http.Error(w, "invalid device metadata", 400)
			return
		}
	}
	version := body.Application.Version
	if len(version) > 64 {
		http.Error(w, "invalid firmware version", 400)
		return
	}
	if version == "" {
		version = "0.0.0"
	}
	writeJSON(w, map[string]any{
		"websocket":   map[string]any{"url": s.cfg.PublicWSURL, "token": s.cfg.DeviceToken, "version": 1},
		"firmware":    map[string]any{"version": version},
		"server_time": map[string]any{"timestamp": time.Now().UnixMilli(), "timezone_offset": 480},
	})
}
func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	expected := "Bearer " + s.cfg.DeviceToken
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	if !s.deviceAllowed(r) {
		http.Error(w, "device not allowed", 403)
		return
	}
	version := r.Header.Get("Protocol-Version")
	if version == "" {
		version = "1"
	}
	if version != "1" && version != "2" && version != "3" {
		http.Error(w, "unsupported protocol", 400)
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		http.Error(w, "device limit reached", 429)
		return
	}
	up := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.conns, c); s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(s.ctx, s.cfg.SessionTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	sess := newSession(ctx, c, s.cfg, int(version[0]-'0'))
	if err := sess.run(); err != nil && ctx.Err() == nil {
		slog.Info("device_session_closed", "session", sess.id, "reason", err.Error())
	}
}
