package management

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type auditEntry struct {
	UTC      time.Time `json:"utc"`
	ActorID  string    `json:"actorID"`
	Scope    string    `json:"scope"`
	Action   string    `json:"action"`
	DeviceID string    `json:"deviceID"`
	RecordID string    `json:"recordID"`
}

func appendAudit(path string, entry auditEntry) error {
	raw, e := json.Marshal(entry)
	if e != nil || len(raw) > 4096 {
		return ErrSource
	}
	parent, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return ErrSource
	}
	defer parent.Close()
	name := filepath.Base(path)
	before, e := parent.Lstat(name)
	if e != nil && !os.IsNotExist(e) {
		return ErrSource
	}
	if e == nil && (!before.Mode().IsRegular() || before.Mode().Perm() != 0600) {
		return ErrSource
	}
	f, e := parent.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if e != nil {
		return ErrSource
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || (before != nil && !os.SameFile(before, st)) {
		return ErrSource
	}
	if _, e = f.Write(append(raw, '\n')); e != nil {
		return ErrSource
	}
	return f.Sync()
}
func (s *Server) audit(a, scope, device, record string) error {
	return s.auditAction(a, scope, "read_conversation", device, record)
}
func (s *Server) auditAction(a, scope, action, device, record string) error {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	return s.auditWrite(s.opts.AuditPath, auditEntry{s.now().UTC(), a, scope, action, device, record})
}
