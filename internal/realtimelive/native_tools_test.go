package realtimelive

import (
	"encoding/json"
	"familyrobot/internal/devicetools"
	"testing"
	"time"
)

func TestNativeToolSettingsAreTextOnlyAndSearchDisabled(t *testing.T) {
	off := false
	tool := map[string]any{"type": "function", "function": map[string]any{"name": "device_get_status"}}
	s := sessionSettings(Config{Search: &off, Tools: []any{tool}}, "planner")
	b, _ := json.Marshal(s)
	if string(b) == "" || s["tools"] == nil {
		t.Fatalf("tool planner lost registered definitions: %s", b)
	}
	modalities, _ := s["modalities"].([]string)
	if len(modalities) != 1 || modalities[0] != "text" || s["enable_search"] != false {
		t.Fatalf("planner must be silent and search-free: %s", b)
	}
	if _, ok := s["audio"]; ok {
		t.Fatal("text planner must not configure output audio")
	}
}

func plannerTools() []map[string]any {
	// Use the Bridge's actual, fixed discovery surface.
	b := devicetools.New(time.Now())
	b.Start(time.Now())
	b.Receive(json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{}}`), time.Now())
	b.Receive(json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"self.get_device_status"},{"name":"self.audio_speaker.set_volume"},{"name":"self.screen.set_brightness"}]}}`), time.Now())
	return b.Tools()
}
