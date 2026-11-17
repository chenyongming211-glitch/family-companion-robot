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
	"time"

	"github.com/gorilla/websocket"
)

func TestNativeStreamPersistsSafeTerminalEvidence(t *testing.T) {
	secret := "private-provider-payload"
	for _, tt := range []struct {
		name          string
		status        any
		details       any
		wrongIdentity bool
		noPCM         bool
		wantStatus    string
		wantReason    any
		wantType      any
		wantError     bool
	}{
		{name: "completed_missing_details", status: "completed", wantStatus: "completed"},
		{name: "failed", status: "failed", details: map[string]any{"type": "failed", "error": map[string]any{"message": secret}}, wantStatus: "failed", wantType: "failed", wantError: true},
		{name: "incomplete", status: "incomplete", details: map[string]any{"type": "incomplete", "reason": "max_output_tokens"}, wantStatus: "incomplete", wantType: "incomplete", wantReason: "max_output_tokens", wantError: true},
		{name: "cancelled", status: "cancelled", details: map[string]any{"type": "cancelled", "reason": "client_cancelled"}, wantStatus: "cancelled", wantType: "cancelled", wantReason: "client_cancelled", wantError: true},
		{name: "failed_missing_details", status: "failed", wantStatus: "failed", wantError: true},
		{name: "secret_extra_fields", status: "completed", details: map[string]any{"type": "incomplete", "reason": "content_filter", "message": secret, "error": map[string]any{"message": secret}, "body": secret, "extra": secret}, wantStatus: "completed", wantType: "incomplete", wantReason: "content_filter"},
		{name: "long_unknown_identifiers", status: "completed", details: map[string]any{"type": strings.Repeat(secret, 1000), "reason": strings.Repeat(secret, 1000)}, wantStatus: "completed"},
		{name: "malformed_identifiers", status: "completed", details: map[string]any{"type": []any{"incomplete"}, "reason": map[string]any{"secret": secret}}, wantStatus: "completed"},
		{name: "unknown_status", status: secret, wantStatus: "unknown", wantError: true},
		{name: "missing_status", wantStatus: "unknown", wantError: true},
		{name: "mismatched_identity", status: "completed", wrongIdentity: true, wantError: true},
		{name: "completed_without_audio", status: "completed", noPCM: true, wantStatus: "completed", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
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
				_ = c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "terminal-r"}})
				if !tt.noPCM {
					_ = c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "terminal-r", "delta": base64.StdEncoding.EncodeToString(make([]byte, 2880))})
				}
				_ = c.WriteJSON(map[string]any{"type": "response.audio_transcript.delta", "response_id": "terminal-r", "delta": "小兔出门。"})
				_ = c.WriteJSON(map[string]any{"type": "response.audio_transcript.done", "response_id": "terminal-r", "transcript": "小兔回家了。🐰"})
				id := "terminal-r"
				if tt.wrongIdentity {
					id = "other-r"
				}
				_ = c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": tt.status, "status_details": tt.details, "extra": secret, "usage": map[string]any{"input_tokens": 3}}})
			}))
			defer srv.Close()
			dir := t.TempDir()
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, dir)
			defer n.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := n.Open(ctx, "fixture", false); err != nil {
				t.Fatal(err)
			}
			var texts []string
			pcmBytes := 0
			err := n.Stream(ctx, make([]byte, 640), func(kind string, data []byte) error {
				if kind == "text" {
					texts = append(texts, string(data))
				} else if kind == "pcm" {
					pcmBytes += len(data)
				}
				return nil
			})
			if tt.wantError {
				if err == nil || err.Error() != "response_terminal" {
					t.Fatalf("original terminal error changed: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(texts) != 2 || texts[1] != "小兔回家了。🐰" || (!tt.noPCM && pcmBytes != 2880) || (tt.noPCM && pcmBytes != 0) {
				t.Fatalf("stream output changed: %q, %d bytes", texts, pcmBytes)
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
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "小兔") {
				t.Fatal("private provider payload or raw transcript retained")
			}
			if tt.wrongIdentity {
				if _, ok := record["terminal_status"]; ok {
					t.Fatal("unmatched response terminal retained", record)
				}
			} else {
				if record["terminal_status"] != tt.wantStatus || record["output_text_chars"] != float64(7) {
					t.Fatal("terminal status or Unicode character count missing", record)
				}
				for field, want := range map[string]any{"terminal_reason": tt.wantReason, "terminal_details_type": tt.wantType} {
					if got, ok := record[field]; !ok || got != want {
						t.Fatalf("%s: got %v, present %v; want %v", field, got, ok, want)
					}
				}
			}
			if tt.wantError {
				if record["state"] != "cancelled_or_outcome_unknown" || record["usage"] != nil || record["received_samples"] != float64(pcmBytes/2) {
					t.Fatal("original error state or usage changed", record)
				}
			} else if record["state"] != "completed" || object(record["usage"])["input_tokens"] != float64(3) || record["output_samples"] != float64(1440) {
				t.Fatal("original completed state or usage changed", record)
			}
		})
	}
}

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
