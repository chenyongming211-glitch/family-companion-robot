package realtimelive

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/voiceidentity"
	"github.com/gorilla/websocket"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	stopping    bool
	activeDone  chan struct{}
	mu          sync.Mutex
	tickets     map[string]ticketBinding
	devices     *voiceidentity.Store
	initErr     error
	busy        bool
	active      *websocket.Conn
	cfg         Config
	product     *realtimestate.Product
	root, build string
	run         func(*websocket.Conn, string)
}

func NewServer(cfg Config, product *realtimestate.Product, root, build string) *Server {
	devices, initErr := voiceidentity.Open(filepath.Join(root, "devices.json"))
	s := &Server{tickets: map[string]ticketBinding{}, devices: devices, initErr: initErr, cfg: cfg, product: product, root: root, build: build}
	s.run = func(ws *websocket.Conn, dir string) {
		session := NewSession(ws, cfg, dir, product)
		session.serverStopping = func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopping }
		session.Run()
	}
	return s
}

type ticketBinding struct {
	expiry time.Time
	owner  Ownership
}

func (s *Server) InitializationError() error { return s.initErr }

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func (s *Server) CloseActive(ctx context.Context) error {
	s.mu.Lock()
	s.stopping = true
	c, done := s.active, s.activeDone
	s.mu.Unlock()
	if c != nil {
		c.Close()
	}
	if done != nil {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		busy := s.busy
		s.mu.Unlock()
		status := 200
		if s.initErr != nil {
			status = 503
		}
		respond(w, status, map[string]any{"device_identity": 1, "ready": s.initErr == nil, "busy": busy, "experiment": "output-asr-live", "backend": "go", "build": s.build})
	})
	mux.HandleFunc("POST /devices", s.enrollDevice)
	mux.HandleFunc("POST /pair", func(w http.ResponseWriter, r *http.Request) {
		if s.initErr != nil {
			w.WriteHeader(503)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Header.Get("Origin") != "" || s.busy || s.stopping {
			w.WriteHeader(http.StatusConflict)
			return
		}
		owner, status := s.pairOwner(r)
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			w.WriteHeader(500)
			return
		}
		ticket := base64.RawURLEncoding.EncodeToString(b)
		s.tickets = map[string]ticketBinding{ticket: {time.Now().Add(120 * time.Second), owner}}
		value := map[string]any{"ticket": ticket}
		if owner.DeviceID != "" {
			value["device_id"] = owner.DeviceID
		}
		respond(w, 200, value)
	})
	mux.HandleFunc("GET /live", func(w http.ResponseWriter, r *http.Request) {
		if s.initErr != nil {
			w.WriteHeader(503)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		// Python short-circuits Origin/busy checks before consuming the ticket.
		if r.Header.Get("Origin") != "" || s.busy || s.stopping {
			s.mu.Unlock()
			w.WriteHeader(403)
			return
		}
		binding, ok := s.tickets[token]
		delete(s.tickets, token)
		if !ok || binding.expiry.Before(time.Now()) {
			s.mu.Unlock()
			w.WriteHeader(403)
			return
		}
		s.busy = true
		done := make(chan struct{})
		s.activeDone = done
		s.mu.Unlock()
		defer func() { s.mu.Lock(); s.busy = false; s.activeDone = nil; close(done); s.mu.Unlock() }()
		ws, e := (&websocket.Upgrader{ReadBufferSize: 8192, WriteBufferSize: 8192, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer ws.Close()
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		s.active = ws
		s.mu.Unlock()
		defer func() { s.mu.Lock(); s.active = nil; s.mu.Unlock() }()
		ws.SetReadLimit(8192)
		owner := binding.owner
		owner.SessionID = randomID()
		dir := filepath.Join(s.root, time.Now().UTC().Format("20060102T150405")+"-"+owner.SessionID)
		if e = os.Mkdir(dir, 0700); e != nil {
			return
		}
		info := SessionInfo{Ownership: owner, SchemaVersion: 1, Build: s.build, StartedAt: time.Now().UTC()}
		if e = privateJSON(filepath.Join(dir, "session.json"), info); e != nil {
			return
		}
		s.run(ws, dir)
	})
	return http.MaxBytesHandler(mux, 8192)
}
