package realtimelive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var expressionNames = []string{"neutral", "happy", "loving", "embarrassed", "confident", "delicious", "sad", "crying", "sleepy", "silly", "angry", "surprised", "winking", "relaxed", "confused"}

type expressionFake struct {
	settings, input chan map[string]any
	requests        atomic.Int32
	closed          chan struct{}
}

func expressionProvider(t *testing.T, handshakeStall bool, respond func(*websocket.Conn)) (Config, *expressionFake) {
	t.Helper()
	f := &expressionFake{settings: make(chan map[string]any, 1), input: make(chan map[string]any, 1), closed: make(chan struct{})}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		defer close(f.closed)
		if !handshakeStall {
			c.WriteJSON(map[string]any{"type": "session.created"})
			var req map[string]any
			if c.ReadJSON(&req) != nil {
				return
			}
			f.settings <- object(req["session"])
			c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
		}
		for {
			var req map[string]any
			if c.ReadJSON(&req) != nil {
				return
			}
			switch req["type"] {
			case "conversation.item.create":
				f.input <- object(req["item"])
			case "response.create":
				f.requests.Add(1)
				if respond != nil {
					respond(c)
				}
			}
		}
	}))
	t.Cleanup(h.Close)
	yes := true
	return Config{URL: "ws" + strings.TrimPrefix(h.URL, "http"), Search: &yes, Synthetic: true, Voice: "Tina"}, f
}

func expressionTerminal(c *websocket.Conn, emotion string) {
	args, _ := json.Marshal(map[string]any{"emotion": emotion})
	c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "expression-r1"}})
	c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "expression-r1", "call_id": "call-1", "name": "set_expression", "arguments": string(args)})
	c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "expression-r1", "status": "completed", "output": []any{map[string]any{"type": "function_call", "call_id": "call-1", "name": "set_expression", "arguments": string(args)}}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 4, "total_tokens": 16, "private_text": "never retained"}}})
}

func expressionTestInput() ExpressionInput {
	return ExpressionInput{CurrentTranscript: "我今天考得很好", DeviceName: "团子\"\\\n身份", PlayedContext: `{"recent_turns":[{"user":"聊考试","assistant_spoken_prefix":"我陪你准备"}]}`}
}

func TestExpressionCanonicalToolsAndImmutableVoiceConfig(t *testing.T) {
	for _, name := range expressionNames {
		t.Run(name, func(t *testing.T) {
			cfg, fake := expressionProvider(t, false, func(c *websocket.Conn) { expressionTerminal(c, name) })
			originalTools := []any{map[string]any{"name": "existing-voice-tool"}}
			cfg.Tools = originalTools
			result := SelectExpression(context.Background(), cfg, t.TempDir(), expressionTestInput())
			<-fake.closed
			if result.Emotion != name || result.Reason != "completed" || result.RequestCount != 1 || fake.requests.Load() != 1 {
				t.Fatalf("%+v requests=%d", result, fake.requests.Load())
			}
			if result.Usage["total_tokens"] != int64(16) || len(result.Usage) != 3 {
				t.Fatal(result.Usage)
			}
			if !*cfg.Search || !reflect.DeepEqual(cfg.Tools, originalTools) {
				t.Fatal("selector mutated voice config")
			}
			settings := <-fake.settings
			if settings["audio"] != nil || settings["enable_search"] != false || !reflect.DeepEqual(settings["modalities"], []any{"text"}) {
				t.Fatal(settings)
			}
			tools, _ := settings["tools"].([]any)
			if len(tools) != 1 {
				t.Fatal(tools)
			}
			fn := object(object(tools[0])["function"])
			p := object(fn["parameters"])
			if fn["name"] != "set_expression" || p["additionalProperties"] != false || len(object(p["properties"])) != 1 {
				t.Fatal(fn)
			}
			enums := object(object(p["properties"])["emotion"])["enum"]
			want, _ := json.Marshal(expressionNames)
			got, _ := json.Marshal(enums)
			if string(got) != string(want) {
				t.Fatalf("%s != %s", got, want)
			}
			item := <-fake.input
			content := item["content"].([]any)
			var input map[string]any
			if json.Unmarshal([]byte(object(content[0])["text"].(string)), &input) != nil || input["device_name"] != expressionTestInput().DeviceName || input["current_transcript"] != expressionTestInput().CurrentTranscript {
				t.Fatal(item)
			}
		})
	}
	voice := sessionSettings(Config{Search: boolPtr(true)}, "voice")
	if voice["audio"] == nil || voice["enable_search"] != true || voice["tools"] != nil {
		t.Fatal(voice)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestExpressionRejectsInvalidTerminalToNeutral(t *testing.T) {
	cases := []struct {
		name, args, status, tool     string
		multiple, textOnly, mismatch bool
	}{
		{"unknown-emotion", `{"emotion":"laughing"}`, "completed", "set_expression", false, false, false},
		{"legacy-alias", `{"emotion":"sleep"}`, "completed", "set_expression", false, false, false},
		{"unknown-field", `{"emotion":"happy","playback_id":3}`, "completed", "set_expression", false, false, false},
		{"duplicate-field", `{"emotion":"happy","emotion":"sad"}`, "completed", "set_expression", false, false, false},
		{"trailing-json", `{"emotion":"happy"} {}`, "completed", "set_expression", false, false, false},
		{"invalid-json", `{"emotion":`, "completed", "set_expression", false, false, false},
		{"failed", `{"emotion":"happy"}`, "failed", "set_expression", false, false, false},
		{"unknown-tool", `{"emotion":"happy"}`, "completed", "device_set_volume", false, false, false},
		{"multiple", `{"emotion":"happy"}`, "completed", "set_expression", true, false, false},
		{"text-only", `{"emotion":"happy"}`, "completed", "set_expression", false, true, false},
		{"argument-mismatch", `{"emotion":"happy"}`, "completed", "set_expression", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, f := expressionProvider(t, false, func(c *websocket.Conn) {
				c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
				args := tc.args
				if tc.mismatch {
					args = `{"emotion":"sad"}`
				}
				c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "r", "call_id": "c", "name": tc.tool, "arguments": args})
				call := map[string]any{"type": "function_call", "call_id": "c", "name": tc.tool, "arguments": tc.args}
				output := []any{call}
				if tc.multiple {
					output = append(output, map[string]any{"type": "function_call", "call_id": "c2", "name": "set_expression", "arguments": tc.args})
				}
				if tc.textOnly {
					output = []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "happy"}}}}
				}
				c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": tc.status, "output": output}})
			})
			result := SelectExpression(context.Background(), cfg, t.TempDir(), expressionTestInput())
			<-f.closed
			if result.Emotion != "neutral" || result.Reason == "completed" || result.RequestCount != 1 || f.requests.Load() != 1 {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestExpressionTotalDeadlineIncludesHandshakeAndResponse(t *testing.T) {
	for _, handshake := range []bool{true, false} {
		t.Run(map[bool]string{true: "handshake", false: "response"}[handshake], func(t *testing.T) {
			cfg, f := expressionProvider(t, handshake, nil)
			start := time.Now()
			result := SelectExpression(context.Background(), cfg, t.TempDir(), expressionTestInput())
			elapsed := time.Since(start)
			if elapsed < 1900*time.Millisecond || elapsed > 2600*time.Millisecond || result.Emotion != "neutral" || result.Reason != "timeout" {
				t.Fatalf("elapsed=%s result=%+v", elapsed, result)
			}
			select {
			case <-f.closed:
			case <-time.After(time.Second):
				t.Fatal("deadline left connection alive")
			}
			want := int32(1)
			if handshake {
				want = 0
			}
			if f.requests.Load() != want || result.RequestCount != int(want) {
				t.Fatal(result, f.requests.Load())
			}
		})
	}
}

func TestExpressionCancelledAndInvalidInputNeverSubmit(t *testing.T) {
	for _, kind := range []string{"cancelled", "history", "transcript", "name"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input := expressionTestInput()
			switch kind {
			case "cancelled":
				cancel()
			case "history":
				input.PlayedContext = `not JSON`
			case "transcript":
				input.CurrentTranscript = strings.Repeat("a", 16385)
			case "name":
				input.DeviceName = strings.Repeat("a", 257)
			}
			result := SelectExpression(ctx, Config{URL: "ws://127.0.0.1:1"}, t.TempDir(), input)
			if result.Emotion != "neutral" || result.Reason == "completed" || result.RequestCount != 0 {
				t.Fatal(result)
			}
		})
	}
}

func TestExpressionUnexpectedProviderOutputFallsBackWithoutAudioPath(t *testing.T) {
	for _, kind := range []string{
		"response.audio.delta", "response.audio.done",
		"response.audio_transcript.delta", "response.audio_transcript.done",
		"response.text.delta", "response.text.done",
		"response.output_text.delta", "response.output_text.done",
		"response.output_audio.delta", "response.output_audio.done",
		"response.output_audio_transcript.delta", "response.output_audio_transcript.done",
		"response.content_part.added", "response.content_part.done",
		"response.output_content_part.added", "response.output_content_part.done",
		"response.refusal.delta", "response.refusal.done",
	} {
		t.Run(kind, func(t *testing.T) {
			cfg, f := expressionProvider(t, false, func(c *websocket.Conn) {
				c.WriteJSON(map[string]any{"type": kind, "delta": "unexpected-output-is-never-decoded-or-routed", "part": map[string]any{"type": "audio", "audio": "never-routed", "text": "never-forwarded"}})
				expressionTerminal(c, "happy")
			})
			result := SelectExpression(context.Background(), cfg, t.TempDir(), expressionTestInput())
			<-f.closed
			if result.Emotion != "neutral" || result.Reason != "unexpected_output" || result.RequestCount != 1 || f.requests.Load() != 1 {
				t.Fatal(result, f.requests.Load())
			}
		})
	}
}

func TestExpressionAllowsFunctionCallLifecycleOnly(t *testing.T) {
	cfg, f := expressionProvider(t, false, func(c *websocket.Conn) {
		args := `{"emotion":"happy"}`
		call := map[string]any{"type": "function_call", "call_id": "c", "name": "set_expression", "arguments": args, "status": "completed"}
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
		c.WriteJSON(map[string]any{"type": "response.output_item.added", "response_id": "r", "item": map[string]any{"type": "function_call", "call_id": "c", "name": "set_expression", "arguments": "", "status": "in_progress"}})
		c.WriteJSON(map[string]any{"type": "response.function_call_arguments.delta", "response_id": "r", "call_id": "c", "delta": args})
		c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "r", "call_id": "c", "name": "set_expression", "arguments": args})
		c.WriteJSON(map[string]any{"type": "response.output_item.done", "response_id": "r", "item": call})
		c.WriteJSON(map[string]any{"type": "rate_limits.updated", "rate_limits": []any{}})
		c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": "completed", "output": []any{call}}})
	})
	result := SelectExpression(context.Background(), cfg, t.TempDir(), expressionTestInput())
	<-f.closed
	if result.Emotion != "happy" || result.Reason != "completed" || result.RequestCount != 1 || f.requests.Load() != 1 {
		t.Fatal(result, f.requests.Load())
	}
}

func TestExpressionHistoryIncludesOnlyBoundedPlayedPrefixes(t *testing.T) {
	cfg, f := expressionProvider(t, false, func(c *websocket.Conn) { expressionTerminal(c, "neutral") })
	input := expressionTestInput()
	input.PlayedContext = `{"recent_turns":[{"user":"当前上下文","assistant_spoken_prefix":"已经播放","generated":"未播放草稿不可出现"}],"prior_session_memory":{"recent_turns":[{"user":"过去提问","assistant_spoken":"过去已播"}],"older_excerpts":[]},"unplayed":"不要转发"}`
	result := SelectExpression(context.Background(), cfg, t.TempDir(), input)
	<-f.closed
	item := <-f.input
	content := item["content"].([]any)
	text := object(content[0])["text"].(string)
	if result.Reason != "completed" || strings.Contains(text, "未播放草稿") || strings.Contains(text, "不要转发") || !strings.Contains(text, "已经播放") || !strings.Contains(text, "过去已播") {
		t.Fatal(result, text)
	}
}

func TestExpressionConfigurationDefaultsOffAndSnapshotsFlag(t *testing.T) {
	cfg, err := ParseConfig("DASHSCOPE_API_KEY=fake\nQWEN_REALTIME_URL=wss://example.invalid/realtime?model=fake\n", "1")
	if err != nil || cfg.ExpressionsEnabled {
		t.Fatal(cfg, err)
	}
	cfg.ExpressionsEnabled = true
	snapshot := cfg.snapshot()
	cfg.ExpressionsEnabled = false
	if !snapshot.ExpressionsEnabled || !*snapshot.Search {
		t.Fatal("flag snapshot or voice search changed")
	}
}

func TestExpressionRejectsContradictoryStreamedTools(t *testing.T) {
	for _, kind := range []string{"multiple-added", "unknown-added", "unknown-terminal-field", "incomplete-terminal"} {
		t.Run(kind, func(t *testing.T) {
			cfg, f := expressionProvider(t, false, func(c *websocket.Conn) {
				c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
				call := map[string]any{"type": "function_call", "call_id": "c1", "name": "set_expression", "arguments": `{"emotion":"happy"}`}
				switch kind {
				case "multiple-added":
					c.WriteJSON(map[string]any{"type": "response.output_item.added", "response_id": "r", "item": map[string]any{"type": "function_call", "call_id": "c1", "name": "set_expression", "arguments": ""}})
					c.WriteJSON(map[string]any{"type": "response.output_item.added", "response_id": "r", "item": map[string]any{"type": "function_call", "call_id": "c2", "name": "set_expression", "arguments": ""}})
				case "unknown-added":
					c.WriteJSON(map[string]any{"type": "response.output_item.added", "response_id": "r", "item": map[string]any{"type": "function_call", "call_id": "c1", "name": "device_set_volume", "arguments": ""}})
				case "unknown-terminal-field":
					call["brightness"] = 100
				case "incomplete-terminal":
					call["status"] = "in_progress"
				}
				c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": "completed", "output": []any{call}}})
			})
			result := SelectExpression(context.Background(), cfg, t.TempDir(), expressionTestInput())
			<-f.closed
			if result.Emotion != "neutral" || result.Reason == "completed" || result.RequestCount != 1 {
				t.Fatal(result)
			}
		})
	}
}
