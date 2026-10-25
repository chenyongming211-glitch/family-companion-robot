package realtimelive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Ownership struct {
	DeviceID      string `json:"device_id,omitempty"`
	SessionID     string `json:"session_id"`
	ClientType    string `json:"client_type"`
	ClientVersion string `json:"client_version,omitempty"`
}
type SessionInfo struct {
	Ownership
	SchemaVersion int        `json:"schema_version"`
	Build         string     `json:"build"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	EndReason     string     `json:"end_reason,omitempty"`
}

func ReadSessionInfo(dir string) (SessionInfo, error) {
	var info SessionInfo
	raw, e := os.ReadFile(filepath.Join(dir, "session.json"))
	if e != nil {
		return info, e
	}
	if json.Unmarshal(raw, &info) != nil || info.SchemaVersion != 1 || len(info.SessionID) != 32 || info.ClientType == "" || info.StartedAt.IsZero() {
		return SessionInfo{}, errors.New("invalid_session_manifest")
	}
	return info, nil
}
func (o Ownership) apply(m map[string]any) {
	if o.SessionID == "" {
		return
	}
	m["session_id"] = o.SessionID
	m["device_id"] = o.DeviceID
	m["client_type"] = o.ClientType
	m["client_version"] = o.ClientVersion
}
func (s *Session) validateScope(event map[string]any) error {
	owner := s.rec.owner
	got, present := event["session_id"]
	if owner.SessionID != "" && (present || owner.DeviceID != "") && got != owner.SessionID {
		return errors.New("wrong_session_scope")
	}
	return nil
}
