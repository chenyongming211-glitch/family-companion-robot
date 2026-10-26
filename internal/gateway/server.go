// Package gateway bridges Xiaozhi devices to native Qwen Realtime audio.
package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"familyrobot/internal/deviceauth"
	"familyrobot/internal/qwen"
	"familyrobot/internal/recordstore"
	"github.com/gorilla/websocket"
)

type Config struct {
	DeviceToken, BootstrapSecret, PublicWSURL, AllowedDevice string
	DeviceRegistryPath                                       string
	Qwen                                                     qwen.Config
	MaxSessions                                              int
	IdleTimeout, SessionTimeout                              time.Duration
	RecordDirectory                                          string
	RecordRetention                                          time.Duration
}
type Server struct {
	deviceAuth *deviceauth.Registry
	cfg        Config
	slots      chan struct{}
	mu         sync.Mutex
	conns      map[*websocket.Conn]struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	records    ownedRecordStore
	devices    map[string]bool
	sessions   sync.WaitGroup
	closing    bool
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeCtx   context.Context
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
	var deviceAuth *deviceauth.Registry
	if cfg.DeviceRegistryPath != "" {
		deviceAuth, err = deviceauth.Load(cfg.DeviceRegistryPath, cfg.AllowedDevice, cfg.DeviceToken)
		if err != nil {
			return nil, errors.New("device_auth_registry_invalid")
		}
	}
	if cfg.MaxSessions < 1 || cfg.MaxSessions > 8 {
		return nil, errors.New("max sessions must be 1-8")
	}
	if cfg.IdleTimeout <= 0 || cfg.SessionTimeout <= 0 {
		return nil, errors.New("session timeouts must be positive")
	}
	if cfg.RecordRetention == 0 {
		cfg.RecordRetention = 168 * time.Hour
	}
	if cfg.RecordRetention < 0 {
		return nil, errors.New("record retention must be positive")
	}
	var records ownedRecordStore
	if cfg.RecordDirectory != "" {
		records, err = recordstore.Open(recordstore.Options{Directory: cfg.RecordDirectory, Retention: cfg.RecordRetention})
		if err != nil {
			return nil, errors.New("record storage initialization failed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{deviceAuth: deviceAuth, cfg: cfg, slots: make(chan struct{}, cfg.MaxSessions), conns: make(map[*websocket.Conn]struct{}), ctx: ctx, cancel: cancel, records: records, devices: make(map[string]bool), closeDone: make(chan struct{})}, nil
}
func (s *Server) Active() int { return len(s.slots) }
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		var cancel context.CancelFunc
		s.closeCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		s.mu.Lock()
		s.closing = true
		s.cancel()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
		go func() {
			defer cancel()
			s.sessions.Wait()
			if s.records != nil {
				if err := s.records.Close(s.closeCtx); err != nil {
					slog.Error("record_store_close_failed")
				}
			}
			close(s.closeDone)
		}()
	})
	select {
	case <-s.closeDone:
	case <-s.closeCtx.Done():
		if errors.Is(s.closeCtx.Err(), context.DeadlineExceeded) {
			slog.Error("gateway_shutdown_drain_timeout")
		}
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
	if r.Header.Get("Origin") != "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	token := s.cfg.DeviceToken
	if s.deviceAuth != nil {
		entry, ok := s.deviceAuth.Lookup(r.Header.Get("Device-Id"))
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		token = entry.Token
	} else if !s.deviceAllowed(r) {
		http.Error(w, "device not allowed", http.StatusForbidden)
		return
	}
	var body struct {
		Application struct {
			Version string `json:"version"`
		} `json:"application"`
	}
	if r.Method == "POST" {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
		if err != nil || json.Unmarshal(raw, &body) != nil {
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
		"websocket":   map[string]any{"url": s.cfg.PublicWSURL, "token": token, "version": 1},
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
	m, _ := net.ParseMAC(r.Header.Get("Device-Id"))
	deviceID := strings.ToLower(m.String())
	s.mu.Lock()
	if s.closing || s.devices[deviceID] {
		s.mu.Unlock()
		http.Error(w, "device already connected or server closing", http.StatusConflict)
		return
	}
	s.devices[deviceID] = true
	s.sessions.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.devices, deviceID); s.mu.Unlock(); s.sessions.Done() }()
	up := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.conns, c); s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(s.ctx, s.cfg.SessionTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	sess := newSession(ctx, c, s.cfg, int(version[0]-'0'))
	if s.records != nil {
		sess.loadRecords(s.records, deviceID)
	}
	if err := sess.run(); err != nil && ctx.Err() == nil {
		slog.Info("device_session_closed", "session", sess.id, "reason", err.Error())
	}
}
