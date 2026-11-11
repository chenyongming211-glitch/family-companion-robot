package realtimelive

import (
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// connectionTrace is advisory request-local correlation only. Authentication,
// ownership and admission remain entirely in DeviceHandler and its existing stores.
// It never accepts client text other than the strict, bounded correlation ID.
type connectionTrace struct {
	id        string
	started   time.Time
	logger    *log.Logger
	sessionID string
}

type connectionStage string

const (
	connectionEntry      connectionStage = "entry"
	connectionAuth       connectionStage = "auth"
	connectionVersion    connectionStage = "version"
	connectionAdmission  connectionStage = "admission"
	connectionUpgrade    connectionStage = "upgrade"
	connectionHello      connectionStage = "hello"
	connectionSession    connectionStage = "session"
	connectionDisconnect connectionStage = "disconnect"
)

func (s *Server) traceConnection(r *http.Request) *connectionTrace {
	values := r.Header.Values("X-Robot-Connection-Id")
	if len(values) != 1 {
		return nil
	}
	id := values[0]
	if len(id) != 28 || id[:3] != "c1-" || id[19] != '-' || !lowerHex(id[3:19]) || !lowerHex(id[20:]) {
		return nil
	}
	logger := s.connectionLog
	if logger == nil {
		logger = log.Default()
	}
	return &connectionTrace{id: id, started: time.Now(), logger: logger}
}

func lowerHex(value string) bool {
	for _, c := range value {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Status uses HTTP results before/at upgrade and websocket close codes for
// hello failures. 408 means a hello read timeout; 1009 is the known gorilla
// read-limit close attempt. Zero means there is no observed protocol result
// (including session handler return), never a claim of clean transport closure.
func (t *connectionTrace) stage(stage connectionStage, status int) {
	if t == nil {
		return
	}
	if t.sessionID != "" {
		t.logger.Printf("robot_connection connection_id=%s stage=%s status=%d elapsed_ms=%d session_id=%s", t.id, stage, status, time.Since(t.started).Milliseconds(), t.sessionID)
	} else {
		t.logger.Printf("robot_connection connection_id=%s stage=%s status=%d elapsed_ms=%d", t.id, stage, status, time.Since(t.started).Milliseconds())
	}
}
func (t *connectionTrace) session(id string) {
	if t != nil && len(id) == 32 && lowerHex(id) {
		t.sessionID = id
	}
}

func connectionReadStatus(err error) int {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code
	}
	// The pinned gorilla read-limit path attempts close 1009 before returning
	// this sentinel instead of a CloseError. Other plain read errors do not
	// expose a reliable protocol status and must stay unknown.
	if errors.Is(err, websocket.ErrReadLimit) {
		return websocket.CloseMessageTooBig
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return http.StatusRequestTimeout
	}
	return 0
}
