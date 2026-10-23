package qwen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHandshakeHonorsContextCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		var v map[string]any
		c.ReadJSON(&v)
		// Never acknowledge session.update; wait until the client disconnects.
		c.ReadMessage()
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	c, err := Open(ctx, Config{URL: "ws" + strings.TrimPrefix(s.URL, "http"), APIKey: "test-key"}, "auto", "")
	if c != nil {
		c.Close()
	}
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("cancelled handshake retained the connection")
	}
}

func TestOpenConfiguresNativeAudioAndManualMode(t *testing.T) {
	got := make(chan map[string]any, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Error("missing API auth")
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		var v map[string]any
		c.ReadJSON(&v)
		got <- v
		c.WriteJSON(map[string]any{"type": "session.updated"})
		var cmd map[string]any
		c.ReadJSON(&cmd)
	}))
	defer s.Close()
	c, err := Open(context.Background(), Config{URL: "ws" + strings.TrimPrefix(s.URL, "http"), APIKey: "private-test-key", Voice: "Tina", Instructions: "hello", VAD: "semantic_vad"}, "manual", "recent history")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	v := <-got
	session := v["session"].(map[string]any)
	if v["type"] != "session.update" || session["turn_detection"] != nil {
		t.Fatalf("manual config: %v", v)
	}
	input := session["audio"].(map[string]any)["input"].(map[string]any)["format"].(map[string]any)
	if input["sample_rate"] != float64(16000) {
		t.Fatal("wrong input sample rate")
	}
	if !strings.Contains(session["instructions"].(string), "recent history") {
		t.Fatal("missing rebuilt context")
	}
	if err := c.Send(map[string]any{"type": "response.cancel"}); err != nil {
		t.Fatal(err)
	}
}

func TestUpstreamErrorDoesNotExposeCredentialsOrBody(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "sk-secret-private-body", 401) }))
	defer s.Close()
	_, err := Open(context.Background(), Config{URL: "ws" + strings.TrimPrefix(s.URL, "http"), APIKey: "sk-secret-private-body"}, "auto", "")
	if err == nil || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("unsafe error %v", err)
	}
}

func TestNativeFunctionEventsAndSessionTools(t *testing.T) {
	got := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		var update map[string]any
		if c.ReadJSON(&update) != nil {
			return
		}
		got <- update
		c.WriteJSON(map[string]any{"type": "session.updated"})
		c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "parent", "call_id": "call", "name": "device_get_status", "arguments": "{}"})
		c.ReadMessage()
	}))
	defer server.Close()
	tools := []map[string]any{{"type": "function", "function": map[string]any{"name": "device_get_status", "description": "read", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}}
	c, err := Open(context.Background(), Config{URL: "ws" + strings.TrimPrefix(server.URL, "http"), Tools: tools}, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cfg := (<-got)["session"].(map[string]any)
	if cfg["audio"] == nil {
		t.Fatal("native audio missing")
	}
	defs, ok := cfg["tools"].([]any)
	if !ok || len(defs) != 1 {
		t.Fatal(cfg)
	}
	def := defs[0].(map[string]any)
	if def["type"] != "function" || def["function"].(map[string]any)["name"] != "device_get_status" {
		t.Fatal(def)
	}
	ev, err := c.Read()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "response.function_call_arguments.done" || ev.ResponseID != "parent" || ev.CallID != "call" || ev.Name != "device_get_status" || ev.Arguments != "{}" {
		t.Fatal(ev)
	}
}

func TestModeChangesDoNotRewriteImmutableVadParameters(t *testing.T) {
	received := make(chan []map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		var initial map[string]any
		if c.ReadJSON(&initial) != nil {
			return
		}
		c.WriteJSON(map[string]any{"type": "session.updated"})
		all := []map[string]any{initial}
		for i := 0; i < 3; i++ {
			var v map[string]any
			if c.ReadJSON(&v) != nil {
				return
			}
			all = append(all, v)
		}
		received <- all
	}))
	defer server.Close()
	c, err := Open(context.Background(), Config{URL: "ws" + strings.TrimPrefix(server.URL, "http"), VAD: "semantic_vad"}, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, v := range []map[string]any{{"type": "input_audio_buffer.append", "audio": "AAA="}} {
		if err := c.Send(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.SetMode("manual"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMode("realtime"); err != nil {
		t.Fatal(err)
	}
	all := <-received
	initial := all[0]["session"].(map[string]any)["turn_detection"].(map[string]any)
	if initial["silence_duration_ms"] != float64(600) {
		t.Fatal("initial VAD config changed")
	}
	if all[2]["session"].(map[string]any)["turn_detection"] != nil {
		t.Fatal("manual segmentation not disabled")
	}
	enabled := all[3]["session"].(map[string]any)["turn_detection"].(map[string]any)
	if len(enabled) != 3 || enabled["type"] != "semantic_vad" || enabled["create_response"] != false || enabled["interrupt_response"] != true {
		t.Fatal("mode toggle rewrote immutable parameters after append", enabled)
	}
}
