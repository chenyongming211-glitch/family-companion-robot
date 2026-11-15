package realtimelive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestNativeStreamPersistsNumericBillingDetailsOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]any{"type": "session.created"})
		var request map[string]any
		if c.ReadJSON(&request) != nil {
			return
		}
		_ = c.WriteJSON(map[string]any{"type": "session.updated"})
		for {
			if c.ReadJSON(&request) != nil {
				return
			}
			if request["type"] == "input_audio_buffer.commit" {
				break
			}
		}
		_ = c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
		if c.ReadJSON(&request) != nil {
			return
		}
		_ = c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "usage-r"}})
		_ = c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "usage-r", "delta": base64.StdEncoding.EncodeToString(make([]byte, 2880))})
		_ = c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "usage-r", "status": "completed", "usage": map[string]any{
			"input_tokens": 12, "output_tokens": 5, "total_tokens": 17, "string_counter": "17",
			"input_tokens_details":  map[string]any{"text_tokens": 2, "audio_tokens": 10, "cached_tokens": 4, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 1, "secret_number": 99, "secret": "private", "arbitrary": map[string]any{"text_tokens": 999}},
			"output_tokens_details": map[string]any{"text_tokens": 1, "audio_tokens": 4, "cached_tokens": "9", "cache_read_input_tokens": -1, "cache_creation_input_tokens": 1.5, "arbitrary": []any{1}},
			"arbitrary_details":     map[string]any{"text_tokens": 999},
		}}})
	}))
	defer srv.Close()
	dir := t.TempDir()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, dir)
	defer n.Close()
	if err := n.Open(context.Background(), "fixture", false); err != nil {
		t.Fatal(err)
	}
	if err := n.Stream(context.Background(), make([]byte, 640), func(string, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "live_native_turn-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	usage := object(record["usage"])
	input, output := object(usage["input_tokens_details"]), object(usage["output_tokens_details"])
	if usage["input_tokens"] != float64(12) || usage["output_tokens"] != float64(5) || usage["total_tokens"] != float64(17) {
		t.Fatal("old counters changed", usage)
	}
	if len(input) != 5 || input["audio_tokens"] != float64(10) || input["cached_tokens"] != float64(4) || len(output) != 2 || output["text_tokens"] != float64(1) || output["audio_tokens"] != float64(4) {
		t.Fatal("numeric billing details missing or unsafe fields retained", usage)
	}
	if usage["string_counter"] != nil || usage["arbitrary_details"] != nil {
		t.Fatal("unsafe payload retained", usage)
	}
}
