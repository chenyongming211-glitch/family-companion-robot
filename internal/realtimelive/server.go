package realtimelive

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/recordstore"
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
	stopping          bool
	mu                sync.Mutex
	tickets           map[string]ticketBinding
	devices           *voiceidentity.Store
	memory            *recordstore.Store
	memoryErr         error
	initErr           error
	leases            map[string]*sessionLease
	cfg               Config
	product           *realtimestate.Product
	root, build       string
	run               func(*websocket.Conn, string)
	runDevice         func(*Session)
	diagnosticClaimed bool
	diagnosticCapture *audioDiagnostics
}

func NewServer(cfg Config, product *realtimestate.Product, root, build string) *Server {
	if cfg.MaxSessions == 0 {
		cfg.MaxSessions = 1
	}
	cfg = cfg.snapshot()
	devices, initErr := voiceidentity.Open(filepath.Join(root, "devices.json"))
	if cfg.MaxSessions < 1 || cfg.MaxSessions > 8 {
		initErr = errors.New("invalid_max_sessions")
	}
	if cfg.DiagnosticsDevice != "" {
		if cfg.DiagnosticsDuration == 0 {
			cfg.DiagnosticsDuration = diagnosticWindow
		}
		if !diagnosticDevicePattern.MatchString(cfg.DiagnosticsDevice) || cfg.DiagnosticsDuration <= 0 || cfg.DiagnosticsDuration > diagnosticWindow {
			initErr = errors.New("invalid_diagnostics_config")
		}
	}
	s := &Server{leases: map[string]*sessionLease{}, tickets: map[string]ticketBinding{}, devices: devices, initErr: initErr, cfg: cfg, product: product, root: root, build: build}
	if cfg.MemoryEnabled {
		s.memory, s.memoryErr = recordstore.Open(recordstore.Options{Directory: filepath.Join(root, ".memory"), Retention: memoryRetention})
	}
	s.run = func(ws *websocket.Conn, dir string) {
		session := NewSession(ws, cfg, dir, product)
		s.attachMemory(session)
		session.serverStopping = func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopping }
		session.Run()
	}
	s.runDevice = func(session *Session) { session.Run() }
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

// A lease belongs to one authenticated terminal (or the separate legacy scope).
// Pointer identity prevents an old cleanup from releasing any newer lease.
type sessionLease struct {
	key  string
	conn *websocket.Conn
	done chan struct{}
}

func ownerKey(owner Ownership) string {
	if owner.DeviceID != "" {
		return owner.DeviceID
	}
	return "legacy_unattributed"
}

// Caller holds s.mu. No network operation is performed under this lock.
func (s *Server) reserveLocked(key string) *sessionLease {
	if s.stopping || len(s.leases) >= s.cfg.MaxSessions || s.leases[key] != nil {
		return nil
	}
	lease := &sessionLease{key: key, done: make(chan struct{})}
	s.leases[key] = lease
	return lease
}
func (s *Server) releaseLease(lease *sessionLease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leases[lease.key] == lease {
		delete(s.leases, lease.key)
		close(lease.done)
	}
}
func (s *Server) attachLease(lease *sessionLease, conn *websocket.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.leases[lease.key] != lease {
		return false
	}
	lease.conn = conn
	return true
}
func (s *Server) CloseActive(ctx context.Context) error {
	s.mu.Lock()
	s.stopping = true
	leases := make([]*sessionLease, 0, len(s.leases))
	conns := make([]*websocket.Conn, 0, len(s.leases))
	for _, lease := range s.leases {
		leases = append(leases, lease)
		if lease.conn != nil {
			conns = append(conns, lease.conn)
		}
	}
	s.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
	for _, lease := range leases {
		select {
		case <-lease.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.memory != nil {
		return s.memory.Close(ctx)
	}
	return nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		active := len(s.leases)
		diagnosticArmed := s.cfg.DiagnosticsDevice != "" && !s.diagnosticClaimed
		diagnosticClaimed := s.diagnosticClaimed
		diagnosticActive := s.diagnosticCapture != nil && s.diagnosticCapture.enabled()
		s.mu.Unlock()
		status := 200
		if s.initErr != nil {
			status = 503
		}
		respond(w, status, map[string]any{"device_identity": 1, "device_memory": s.memory != nil, "memory_ready": s.cfg.MemoryEnabled && s.memoryErr == nil, "ready": s.initErr == nil, "busy": active > 0, "active_sessions": active, "max_sessions": s.cfg.MaxSessions, "capacity_full": active >= s.cfg.MaxSessions, "experiment": "output-asr-live", "backend": "go", "build": s.build, "diagnostics_armed": diagnosticArmed, "diagnostics_claimed": diagnosticClaimed, "diagnostics_active": diagnosticActive})
	})
	mux.HandleFunc("POST /devices", s.enrollDevice)
	mux.HandleFunc("POST /pair", func(w http.ResponseWriter, r *http.Request) {
		if s.initErr != nil {
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("Origin") != "" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		owner, status := s.pairOwner(r)
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		ticket, status := s.issueTicket(owner)
		if status != 200 {
			w.WriteHeader(status)
			return
		}
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
		if r.Header.Get("Origin") != "" || len(s.leases) >= s.cfg.MaxSessions || s.stopping {
			s.mu.Unlock()
			w.WriteHeader(403)
			return
		}
		binding, ok := s.tickets[token]
		if !ok || !binding.expiry.After(time.Now()) {
			delete(s.tickets, token)
			s.mu.Unlock()
			w.WriteHeader(403)
			return
		}
		lease := s.reserveLocked(ownerKey(binding.owner))
		if lease == nil {
			s.mu.Unlock()
			w.WriteHeader(403)
			return
		}
		delete(s.tickets, token)
		s.mu.Unlock()
		defer s.releaseLease(lease)
		ws, e := (&websocket.Upgrader{ReadBufferSize: 8192, WriteBufferSize: 8192, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer ws.Close()
		if !s.attachLease(lease, ws) {
			return
		}
		ws.SetReadLimit(8192)
		dir, e := s.sessionDirectory(binding.owner)
		if e != nil {
			return
		}
		s.run(ws, dir)
	})
	return http.MaxBytesHandler(mux, 8192)
}

func (s *Server) sessionDirectory(owner Ownership) (string, error) {
	owner.SessionID = randomID()
	dir := filepath.Join(s.root, time.Now().UTC().Format("20060102T150405")+"-"+owner.SessionID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", err
	}
	info := SessionInfo{Ownership: owner, SchemaVersion: 1, Build: s.build, StartedAt: time.Now().UTC()}
	if err := privateJSON(filepath.Join(dir, "session.json"), info); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Server) issueTicket(owner Ownership) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ownerKey(owner)
	if s.stopping || len(s.leases) >= s.cfg.MaxSessions || s.leases[key] != nil {
		return "", http.StatusConflict
	}
	now := time.Now()
	for ticket, binding := range s.tickets {
		if !binding.expiry.After(now) || ownerKey(binding.owner) == key {
			delete(s.tickets, ticket)
		}
	}
	if len(s.tickets) >= 64 {
		return "", http.StatusConflict
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", http.StatusInternalServerError
	}
	ticket := base64.RawURLEncoding.EncodeToString(b)
	s.tickets[ticket] = ticketBinding{time.Now().Add(120 * time.Second), owner}
	return ticket, http.StatusOK
}
