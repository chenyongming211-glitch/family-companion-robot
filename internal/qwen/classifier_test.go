package qwen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func classifierPeer(t *testing.T, events []map[string]any, stall string) (Config, <-chan struct{}) {
	t.Helper()
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		defer close(closed)
		c.WriteJSON(map[string]any{"type": "session.created"})
		var update map[string]any
		if c.ReadJSON(&update) != nil {
			return
		}
		cfg := update["session"].(map[string]any)
		if len(cfg) != 4 || cfg["instructions"] != "你只进行暂停任务意图分类，不回答用户问题。" || cfg["turn_detection"] != nil || len(cfg["tools"].([]any)) != 0 || len(cfg["modalities"].([]any)) != 1 || cfg["modalities"].([]any)[0] != "text" {
			t.Errorf("classifier setup: %v", cfg)
		}
		if stall == "handshake" {
			c.ReadMessage()
			return
		}
		c.WriteJSON(map[string]any{"type": "session.updated"})
		var create map[string]any
		if c.ReadJSON(&create) != nil {
			return
		}
		if create["type"] != "response.create" || len(create["response"].(map[string]any)) != 1 {
			t.Errorf("classifier create: %v", create)
		}
		instructions := create["response"].(map[string]any)["instructions"].(string)
		prefix, data, ok := strings.Cut(instructions, "\n")
		if !ok || !strings.HasPrefix(prefix, "仅输出一个英文枚举值：") {
			t.Error("missing proven response instructions")
		}
		var input map[string]any
		if json.Unmarshal([]byte(data), &input) != nil || len(input) != 3 || input["original_request"] != "原任务" || input["latest_user_text"] != "继续" || input["text_position"] != "unknown" {
			t.Errorf("classifier data: %v", input)
		}
		for _, event := range events {
			if c.WriteJSON(event) != nil {
				return
			}
		}
		c.ReadMessage() // cancellation or normal return must close the socket
	}))
	t.Cleanup(server.Close)
	return Config{URL: "ws" + strings.TrimPrefix(server.URL, "http"), APIKey: "private-test-key", Voice: "must-not-leak", Tools: []map[string]any{{"name": "must-not-leak"}}, Instructions: "must-not-leak"}, closed
}
func enumEvents(text string) []map[string]any {
	return []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "classify"}},
		{"type": "response.text.delta", "response_id": "classify", "delta": text},
		{"type": "response.text.done", "response_id": "classify", "text": text},
		{"type": "response.done", "response": map[string]any{"id": "classify", "status": "completed"}},
	}
}
func TestIsolatedClassifierStrictEnumAndDedicatedTextProtocol(t *testing.T) {
	for _, text := range []string{"side", "resume", "discard", "replace", " \nresume\t", "resuming", "{\"action\":\"resume\"}", "side resume", "resume explanation", ""} {
		t.Run(text, func(t *testing.T) {
			cfg, closed := classifierPeer(t, enumEvents(text), "")
			got, err := ClassifyPausedTask(context.Background(), cfg, "原任务", "继续")
			valid := text == "side" || text == "resume" || text == "discard" || text == "replace" || strings.TrimSpace(text) == "resume"
			if valid && (err != nil || got != strings.TrimSpace(text)) {
				t.Fatalf("got %q, %v", got, err)
			}
			if !valid && (err == nil || got != "") {
				t.Fatalf("accepted %q", text)
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("classifier socket not closed")
			}
		})
	}
}
func TestIsolatedClassifierRejectsMalformedOrUnexpectedOutput(t *testing.T) {
	cases := map[string][]map[string]any{}
	for _, typ := range []string{"response.audio.delta", "response.audio_transcript.done", "response.function_call_arguments.done"} {
		e := enumEvents("resume")
		e = append(e[:2], append([]map[string]any{{"type": typ, "response_id": "classify"}}, e[2:]...)...)
		cases[typ] = e
	}
	e := enumEvents("resume")
	e[2]["text"] = "side"
	cases["inconsistent"] = e
	e = enumEvents("resume")
	e[3]["response"].(map[string]any)["status"] = "cancelled"
	cases["incomplete"] = e
	e = enumEvents("resume")
	e[1]["response_id"] = "other"
	cases["wrong response"] = e
	e = enumEvents("resume")
	e = append(e[:3], append([]map[string]any{e[2]}, e[3:]...)...)
	cases["duplicate text"] = e
	e = enumEvents("resume")
	e = append(e[:1], append([]map[string]any{e[0]}, e[1:]...)...)
	cases["duplicate response"] = e
	e = enumEvents(strings.Repeat("x", 1024))
	cases["oversize"] = e
	e = enumEvents("resume")
	e[3]["response"].(map[string]any)["output"] = []any{map[string]any{"type": "function_call"}}
	cases["hidden function"] = e
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, _ := classifierPeer(t, events, "")
			got, err := ClassifyPausedTask(context.Background(), cfg, "原任务", "继续")
			if err == nil || got != "" {
				t.Fatalf("accepted %q", got)
			}
		})
	}
}
func TestIsolatedClassifierCancellationClosesHandshakeAndRead(t *testing.T) {
	for _, stall := range []string{"handshake", "response"} {
		t.Run(stall, func(t *testing.T) {
			cfg, closed := classifierPeer(t, nil, stall)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()
			_, err := ClassifyPausedTask(ctx, cfg, "原任务", "继续")
			if err == nil {
				t.Fatal("cancel ignored")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("socket survived cancellation")
			}
		})
	}
}
func TestIsolatedClassifierSecretSafeErrors(t *testing.T) {
	cfg, _ := classifierPeer(t, []map[string]any{{"type": "error", "error": map[string]any{"message": "private-test-key 原任务"}}}, "")
	_, err := ClassifyPausedTask(context.Background(), cfg, "原任务", "继续")
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "原任务") || strings.Contains(err.Error(), cfg.URL) {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestIsolatedClassifierNativeEnvelopeWithOmittedItemText(t *testing.T) {
	events := enumEvents("resume")
	item := map[string]any{"id": "item", "type": "message", "role": "assistant", "content": []any{}}
	final := map[string]any{"id": "item", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": "resume"}}}
	events = []map[string]any{events[0],
		{"type": "response.output_item.added", "response_id": "classify", "output_index": 0, "item": item},
		{"type": "conversation.item.created", "item": item},
		{"type": "response.content_part.added", "response_id": "classify", "item_id": "item", "part": map[string]any{"type": "text", "text": ""}},
		events[1], events[2],
		{"type": "response.content_part.done", "response_id": "classify", "item_id": "item", "part": map[string]any{"type": "text", "text": "resume"}},
		{"type": "response.output_item.done", "response_id": "classify", "item": map[string]any{"id": "item", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "text"}}}},
		{"type": "response.done", "response": map[string]any{"id": "classify", "status": "completed", "output": []any{final}}},
	}
	cfg, _ := classifierPeer(t, events, "")
	got, err := ClassifyPausedTask(context.Background(), cfg, "原任务", "继续")
	if err != nil || got != "resume" {
		t.Fatalf("native envelope rejected: %q %v", got, err)
	}
}

func TestIsolatedClassifierRejectsDuplicateConversationOutput(t *testing.T) {
	events := enumEvents("resume")
	item := map[string]any{"id": "item", "type": "message", "role": "assistant", "content": []any{}}
	created := map[string]any{"type": "conversation.item.created", "item": item}
	events = append(events[:1], append([]map[string]any{created, created}, events[1:]...)...)
	cfg, _ := classifierPeer(t, events, "")
	if got, err := ClassifyPausedTask(context.Background(), cfg, "原任务", "继续"); err == nil || got != "" {
		t.Fatal("duplicate conversation output accepted")
	}
}
func TestIsolatedClassifierRejectsUnexpectedInitialOutput(t *testing.T) {
	events := enumEvents("resume")
	events[0]["response"].(map[string]any)["output"] = []any{map[string]any{"type": "function_call"}}
	cfg, _ := classifierPeer(t, events, "")
	if got, err := ClassifyPausedTask(context.Background(), cfg, "原任务", "继续"); err == nil || got != "" {
		t.Fatal("unexpected initial output accepted")
	}
}
func TestIsolatedClassifierBoundsEventCountAndBytes(t *testing.T) {
	for _, kind := range []string{"count", "aggregate-bytes", "frame-bytes"} {
		t.Run(kind, func(t *testing.T) {
			events := enumEvents("resume")[:1]
			count, padding := 129, ""
			if kind == "aggregate-bytes" {
				count = 10
				padding = strings.Repeat("x", 8000)
			}
			if kind == "frame-bytes" {
				count = 1
				padding = strings.Repeat("x", 32768)
			}
			for i := 0; i < count; i++ {
				events = append(events, map[string]any{"type": "response.text.delta", "response_id": "classify", "delta": "", "padding": padding})
			}
			events = append(events, enumEvents("resume")[2:]...)
			cfg, _ := classifierPeer(t, events, "")
			if got, err := ClassifyPausedTask(context.Background(), cfg, "原任务", "继续"); err == nil || got != "" {
				t.Fatal("classifier limit ignored")
			}
		})
	}
}
