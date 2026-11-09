package management

import (
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"testing"
)

func TestProjectionEvidenceAndTail(t *testing.T) {
	info := realtimelive.SessionInfo{Ownership: realtimelive.Ownership{SessionID: "0123456789abcdef0123456789abcdef", DeviceID: "dev_0123456789abcdef0123456789abcdef", ClientType: "xiaozhi"}}
	event := func(kind string, fields map[string]any) []byte {
		fields["event"] = kind
		fields["session_id"] = info.SessionID
		fields["device_id"] = info.DeviceID
		fields["client_type"] = info.ClientType
		b, _ := json.Marshal(fields)
		return append(b, '\n')
	}
	raw := append(event("user_text", map[string]any{"id": 1, "text": "你好"}), event("generated", map[string]any{"id": 1, "text": "你好呀", "sent": 1440})...)
	raw = append(raw, event("playback", map[string]any{"id": 1, "completed": 1440, "accepted": 1440, "sent": 1440, "terminal": "played", "basis": "i2s_dma"})...)
	turns, pending, err := project(append(raw, []byte("{partial")...), info)
	if err != nil || !pending || len(turns) != 1 || turns[0].Played != "你好呀" || turns[0].Playback != "played" {
		t.Fatalf("%+v %v %v", turns, pending, err)
	}
	_, _, err = project(append(raw, []byte("{bad}\n")...), info)
	if !errors.Is(err, ErrSource) {
		t.Fatal(err)
	}
}
