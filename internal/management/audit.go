package management

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type auditEntry struct {
	SchemaVersion  int       `json:"schemaVersion,omitempty"`
	UTC            time.Time `json:"utc"`
	ActorID        string    `json:"actorID"`
	Scope          string    `json:"scope"`
	Action         string    `json:"action"`
	DeviceID       string    `json:"deviceID"`
	RecordID       string    `json:"recordID"`
	OperationID    string    `json:"operationID,omitempty"`
	FamilyID       string    `json:"familyID,omitempty"`
	BindingID      string    `json:"bindingID,omitempty"`
	BindingVersion int       `json:"bindingVersion,omitempty"`
	Outcome        string    `json:"outcome,omitempty"`
	Field          string    `json:"field,omitempty"`
	Value          *int      `json:"value,omitempty"`
	Actual         *int      `json:"actual,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	legacy         bool
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
	if e = f.Sync(); e != nil {
		return ErrSource
	}
	if before == nil {
		dir, e := parent.Open(".")
		if e != nil {
			return ErrSource
		}
		defer dir.Close()
		if e = dir.Sync(); e != nil {
			return ErrSource
		}
	}
	return nil
}
func (s *Server) audit(a, scope, device, record string) error {
	return s.auditAction(a, scope, "read_conversation", device, record)
}
func (s *Server) auditAction(a, scope, action, device, record string) error {
	return s.auditCapture(nil, a, scope, action, device, record)
}

// Readers pass their exact immutable configuration rather than reacquiring a
// replacement after the resource has already been read.
func (s *Server) auditReadAction(readConfig Config, a, scope, action, device, record string) error {
	return s.auditCapture(&readConfig, a, scope, action, device, record)
}
func (s *Server) auditCapture(readConfig *Config, a, scope, action, device, record string) error {
	entry := auditEntry{SchemaVersion: 1, UTC: s.now().UTC(), ActorID: a, Scope: scope, Action: action, DeviceID: device, RecordID: record}
	// Capture the binding at the access/request time. This helper is never used
	// while holding runtimeMu; runtime commands already carry their own anchor.
	if device != "" {
		c, e := s.snapshot()
		if e != nil {
			return e
		}
		b, e := c.Authorize(a, scope, device, entry.UTC, false)
		safe := e == nil && auditAccessAnchorSafe(c, a, scope, action, device, entry.UTC, b)
		if safe && readConfig != nil {
			initial, e := readConfig.Authorize(a, scope, device, entry.UTC, false)
			safe = e == nil && initial.ID == b.ID && initial.Version == b.Version && initial.FamilyID == b.FamilyID && initial.From.Equal(b.From) && auditAccessAnchorSafe(*readConfig, a, scope, action, device, entry.UTC, b)
		}
		if safe {
			entry.FamilyID = b.FamilyID
			entry.BindingID = b.ID
			entry.BindingVersion = b.Version
		}
	}
	return s.auditRecord(entry)
}

// Historical record readers may authorize an older family era. Without the
// resource binding at this append boundary, a current grant cannot distinguish
// that access from another family era the same actor can also read. Keep the
// private event, but omit its anchor rather than guess its household.
func auditAccessAnchorSafe(c Config, actor, scope, action, device string, at time.Time, current Binding) bool {
	if action != "read_conversation" && action != "read_chat" && action != "list_chats" {
		return true
	}
	for _, b := range c.Bindings {
		if b.DeviceID != device || b.FamilyID == current.FamilyID || at.Before(b.From) {
			continue
		}
		if allowed, e := c.Authorize(actor, scope, device, b.From, false); e == nil && allowed.FamilyID != current.FamilyID {
			return false
		}
	}
	return true
}
func (s *Server) auditRecord(entry auditEntry) error {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	return s.auditWrite(s.opts.AuditPath, entry)
}
func (s *Server) operationAudit(actor, scope, action, id, op string, b Binding, field string, value *int) auditEntry {
	return auditEntry{SchemaVersion: 1, UTC: s.now().UTC(), ActorID: actor, Scope: scope, Action: action, DeviceID: id, OperationID: op, FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Field: field, Value: value}
}

// terminalAudit is an attempt, not a durable guarantee. The private flag is set
// before append so an I/O failure cannot cause a repeated operation or retry.
func (s *Server) terminalAudit(id string, cmd *runtimeCommand) {
	if cmd == nil || runtimePending(cmd) || cmd.auditAttempted {
		return
	}
	cmd.auditAttempted = true
	e := s.operationAudit(cmd.actor, cmd.scope, "device_control_result", id, cmd.ID, cmd.binding, cmd.Field, &cmd.Value)
	e.Outcome = cmd.Status
	if cmd.auditOutcome != "" {
		e.Outcome = cmd.auditOutcome
	}
	e.Actual = cmd.Actual
	_ = s.auditRecord(e)
}
