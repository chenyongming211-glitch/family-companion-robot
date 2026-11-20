package voicecompare

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConversationAcceptsProductBounds(t *testing.T) {
	c := fixtureClient("wss://example.com/provider")
	stop := errors.New("intent stop")
	c.BeforeCall = func(string, float64) error { return stop }
	if _, err := c.RunConversation(context.Background(), "native", strings.Repeat("上下文", 3000), make([]byte, 16000*2*20)); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	if _, err := c.RunConversation(context.Background(), "native", "context", make([]byte, 16000*2*26)); err == nil || errors.Is(err, stop) {
		t.Fatal("oversized input accepted", err)
	}
	if _, err := c.Run(context.Background(), "native", "context", make([]byte, 16000*2*20), ""); err == nil || errors.Is(err, stop) {
		t.Fatal("benchmark bounds changed", err)
	}
}

// Consume one committed turn, checking the same 20 ms input bound as Native.Generate.
func readNativeTestPCM(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	var pcm []byte
	for {
		message := readJSON(t, c)
		switch message["type"] {
		case "input_audio_buffer.append":
			audio, ok := message["audio"].(string)
			part, err := base64.StdEncoding.DecodeString(audio)
			if !ok || err != nil || len(part) == 0 || len(part)%2 != 0 || len(part) > 640 {
				t.Errorf("invalid native PCM append: %d bytes", len(part))
			}
			pcm = append(pcm, part...)
		case "input_audio_buffer.commit":
			return pcm
		default:
			t.Errorf("unexpected native input event: %v", message["type"])
			return pcm
		}
	}
}

func productNativeFixture(t *testing.T, c *websocket.Conn, text string, inputBytes int) []byte {
	_ = c.WriteJSON(map[string]any{"type": "session.created"})
	update := readJSON(t, c)["session"].(map[string]any)
	if strings.Contains(update["instructions"].(string), "120") || update["max_tokens"] != float64(4096) {
		t.Error("benchmark restriction leaked", update)
	}
	_ = c.WriteJSON(map[string]any{"type": "session.updated", "session": update})
	pcm := readNativeTestPCM(t, c)
	if len(pcm) != inputBytes {
		t.Error("product input lost", len(pcm))
	}
	_ = c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
	if message := readJSON(t, c); message["type"] != "response.create" {
		t.Error("expected exactly one input commit before response.create")
	}
	_ = c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "product"}})
	_ = c.WriteJSON(map[string]any{"type": "response.text.done", "response_id": "product", "text": text})
	_ = c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "product", "status": "completed", "usage": usageFixture("native"), "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}}}})
	return pcm
}
func TestConversationProductRouteNormalTextAndNoCallCaps(t *testing.T) {
	text := strings.Repeat("这是一段自然的回复。", 20)
	c := fixtureClient(wsFixture(t, func(c *websocket.Conn) { ttsFixture(t, c, "") }))
	input := make([]byte, 16000*2*20-2)
	for i := range input {
		input[i] = byte(i/640 + i%640)
	}
	c.RealtimeURL = wsFixture(t, func(conn *websocket.Conn) {
		if received := productNativeFixture(t, conn, text, len(input)); !bytes.Equal(input, received) {
			t.Error("chunking changed or reordered PCM")
		}
	})
	stages := []string{}
	c.BeforeCall = func(stage string, _ float64) error { stages = append(stages, stage); return nil }
	result, err := c.RunConversation(context.Background(), "native", "normal history", input)
	if err != nil || result.Text != text || len(result.PCM) == 0 || len(stages) <= 4 {
		t.Fatal("product generation", err, result.Text, len(result.PCM), len(stages))
	}
}

func TestConversationKeepsRawReplyButSpeaksOnlyStory(t *testing.T) {
	const body = "小刺猬阿团总嫌自己满身尖刺。"
	const raw = "（表情：开心）\n\n" + body
	c := fixtureClient(wsFixture(t, func(conn *websocket.Conn) { ttsFixture(t, conn, "omit-pictograph") }))
	c.RealtimeURL = wsFixture(t, func(conn *websocket.Conn) { productNativeFixture(t, conn, raw, 3200) })
	result, err := c.RunConversation(context.Background(), "native", "讲个故事", make([]byte, 3200))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != raw || len(result.TaskBoundaries) != 1 || result.TaskBoundaries[0].Text != body || len(result.PCM) != 9600 {
		t.Fatalf("raw=%q, synthesized tasks=%v, PCM bytes=%d", result.Text, result.TaskBoundaries, len(result.PCM))
	}
}

func TestConversationSpokenTextRecognizesOnlyStandaloneFirstLine(t *testing.T) {
	for _, raw := range []string{
		"（表情：开心）这是一句台词。",
		"“（表情：开心）”\n这是书里的原话。",
		"他说：\n（表情：开心）\n这是一句台词。",
		"（小声地说）\n夜深了。",
		"（表情：为难）\n夜深了。",
		"(表情：开心）\n夜深了。",
		"（表情：开心）",
	} {
		if got := conversationSpokenText(raw); got != raw {
			t.Errorf("changed spoken prose %q to %q", raw, got)
		}
	}
	const body = "小刺猬醒来了。"
	for _, cue := range []string{"（表情：开心）\n\n", " \r\n(表情:困倦) \r\n"} {
		if got := conversationSpokenText(cue + body); got != body {
			t.Errorf("expected only the story after %q; got %q", cue, got)
		}
	}
}

func TestConversationProductRouteBadMetadataAndCancellation(t *testing.T) {
	for _, mode := range []string{"wrong-text", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			c := fixtureClient(wsFixture(t, func(c *websocket.Conn) { ttsFixture(t, c, mode) }))
			c.RealtimeURL = wsFixture(t, func(c *websocket.Conn) { productNativeFixture(t, c, "你好。", 3200) })
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			result, err := c.RunConversation(ctx, "native", "context", make([]byte, 3200))
			if err == nil || len(result.PCM) != 0 {
				t.Fatal("invalid synthesis returned", err, len(result.PCM))
			}
			if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("cancel lost", err)
			}
		})
	}
}
func TestConversationProductRouteCascadeLongReplyAndProviderFailure(t *testing.T) {
	const sentence = "保持上下文自然回答。"
	text := "（表情：开心）\n\n" + strings.Repeat(sentence, 20)
	textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["max_tokens"] != float64(4096) || strings.Contains(fmt.Sprint(body["messages"]), "120") {
			t.Error("benchmark prompt", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": text}}}, "usage": usageFixture("llm")})
	}))
	defer textServer.Close()
	c := fixtureClient(wsFixture(t, func(c *websocket.Conn) {
		run := readJSON(t, c)
		p := run["payload"].(map[string]any)
		id := run["header"].(map[string]any)["task_id"].(string)
		if p["task"] == "tts" {
			t.Fatal("fixture expected separate TTS server")
		}
		asrEvent(c, id, "task-started", nil)
		total := 0
		for {
			kind, b, e := c.ReadMessage()
			if e != nil {
				return
			}
			if kind == websocket.TextMessage {
				break
			}
			total += len(b)
		}
		if total != 16000*2*20 {
			t.Error("ASR input truncated", total)
		}
		asrEvent(c, id, "result-generated", map[string]any{"sentence": map[string]any{"text": "接着讲吧。", "sentence_end": true}})
		asrEvent(c, id, "task-finished", nil)
	}))
	// Both ASR and TTS use one endpoint; dispatch their first task on an isolated mux.
	asrURL := c.InferenceURL
	ttsURL := wsFixture(t, func(ws *websocket.Conn) { ttsFixture(t, ws, "") })
	inference := wsFixture(t, func(conn *websocket.Conn) {
		// Proxy fixtures retain the real websocket protocol rather than stubbing Client.
		var first map[string]any
		if err := conn.ReadJSON(&first); err != nil {
			return
		}
		task := first["payload"].(map[string]any)["task"].(string)
		target := asrURL
		if task == "tts" {
			target = ttsURL
		}
		upstream, _, err := websocket.DefaultDialer.Dial(target, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer upstream.Close()
		_ = upstream.WriteJSON(first)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				kind, b, e := conn.ReadMessage()
				if e != nil {
					return
				}
				if upstream.WriteMessage(kind, b) != nil {
					return
				}
			}
		}()
		for {
			kind, b, e := upstream.ReadMessage()
			if e != nil {
				break
			}
			if conn.WriteMessage(kind, b) != nil {
				break
			}
		}
		_ = conn.Close()
		<-done
	})
	c.InferenceURL = inference
	c.TextURL = textServer.URL
	result, err := c.RunConversation(context.Background(), "cascade", "history", make([]byte, 16000*2*20))
	if err != nil || result.Text != text || len(result.PCM) == 0 {
		t.Fatal(err, result.Text)
	}
	if len(result.TaskBoundaries) != 20 || result.TaskBoundaries[0].Text != sentence {
		t.Fatal("unexpected synthesized story opening", result.TaskBoundaries)
	}
	delivered := 0
	err = c.StreamConversation(context.Background(), "cascade", "history", make([]byte, 16000*2*20), func(draft, spoken string, pcm []byte) error {
		wantDraft := ""
		if delivered == 0 {
			wantDraft = text
		}
		if draft != wantDraft || spoken != sentence || len(pcm) != 9600 {
			t.Fatal("stream lost the generated draft or validated TTS content")
		}
		delivered++
		return nil
	})
	if err != nil || delivered != 20 {
		t.Fatal("incomplete cascade stream", delivered, err)
	}
	c.TextURL = "http://127.0.0.1:1"
	result, err = c.RunConversation(context.Background(), "cascade", "history", make([]byte, 16000*2*20))
	if err == nil || len(result.PCM) != 0 {
		t.Fatal("failed provider fell back", err)
	}
}

func TestConversationProductRouteOutputSixtySecondBound(t *testing.T) {
	for _, seconds := range []int{31, 61} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			endpoint := wsFixture(t, func(c *websocket.Conn) {
				run := readJSON(t, c)
				id := run["header"].(map[string]any)["task_id"].(string)
				taskEvent(c, id, "task-started", nil)
				cont := readJSON(t, c)
				text := cont["payload"].(map[string]any)["input"].(map[string]any)["text"].(string)
				_ = readJSON(t, c)
				out := func(kind string, words any) any {
					return map[string]any{"type": kind, "original_text": text, "sentence": map[string]any{"index": 0, "words": words}}
				}
				taskEvent(c, id, "result-generated", out("sentence-begin", nil))
				// Provider binary frames remain individually bounded.
				for n := 0; n < seconds; n++ {
					taskEvent(c, id, "result-generated", out("sentence-synthesis", nil))
					if c.WriteMessage(websocket.BinaryMessage, make([]byte, 24000*2)) != nil {
						return
					}
				}
				taskEvent(c, id, "result-generated", out("sentence-end", []any{map[string]any{"text": "你好", "begin_index": 0, "end_index": 2, "begin_time": 0, "end_time": seconds * 1000}}))
				taskEvent(c, id, "task-finished", nil)
			})
			c := fixtureClient(endpoint)
			c.RealtimeURL = wsFixture(t, func(c *websocket.Conn) { productNativeFixture(t, c, "你好。", 3200) })
			result, err := c.RunConversation(context.Background(), "native", "history", make([]byte, 3200))
			if seconds == 31 && (err != nil || len(result.PCM) != 31*24000*2) {
				t.Fatal("old 30 second cap", err, len(result.PCM))
			}
			if seconds == 31 {
				c.RealtimeURL = wsFixture(t, func(conn *websocket.Conn) { productNativeFixture(t, conn, "你好。你好。", 3200) })
				bytesDelivered := 0
				err = c.StreamConversation(context.Background(), "native", "history", make([]byte, 3200), func(_, _ string, pcm []byte) error {
					bytesDelivered += len(pcm)
					return nil
				})
				if err == nil || bytesDelivered != 31*24000*2 {
					t.Fatal("60 second limit must span clauses", bytesDelivered, err)
				}
			}
			if seconds == 61 && (err == nil || len(result.PCM) != 0) {
				t.Fatal("output unbounded", err, len(result.PCM))
			}
		})
	}
}
func TestConversationProductRouteLiveGuardAfterHandshake(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	endpoint := wsFixture(t, func(c *websocket.Conn) {
		allowed.Store(false)
		_ = c.WriteJSON(map[string]any{"type": "session.created"})
		if _, _, err := c.ReadMessage(); err == nil {
			t.Error("revoked prompt sent")
		}
	})
	c := fixtureClient(endpoint)
	denied := errors.New("revoked")
	c.BeforeSubmit = func() error {
		if !allowed.Load() {
			return denied
		}
		return nil
	}
	result, err := c.RunConversation(context.Background(), "native", "private prompt", make([]byte, 3200))
	if !errors.Is(err, denied) || len(result.PCM) != 0 {
		t.Fatal(err)
	}
}

func TestNativeInputStopsAtRevokedChunk(t *testing.T) {
	for _, mode := range []string{"permission", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			var reads []string
			var received int
			done := make(chan struct{})
			endpoint := wsFixture(t, func(conn *websocket.Conn) {
				defer close(done)
				_ = conn.WriteJSON(map[string]any{"type": "session.created"})
				session := readJSON(t, conn)["session"]
				_ = conn.WriteJSON(map[string]any{"type": "session.updated", "session": session})
				for {
					var frame struct{ Type, Audio string }
					if conn.ReadJSON(&frame) != nil {
						return
					}
					reads = append(reads, frame.Type)
					pcm, _ := base64.StdEncoding.DecodeString(frame.Audio)
					received += len(pcm)
				}
			})
			c := fixtureClient(endpoint)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			writes := 0
			denied := errors.New("permission revoked")
			if mode == "cancellation" {
				denied = context.Canceled
			}
			c.BeforeSubmit = func() error {
				writes++
				if writes == 3 { // after session.update and the first audio chunk
					if mode == "cancellation" {
						cancel()
					}
					return denied
				}
				return nil
			}
			_, err := c.RunConversation(ctx, "native", "current turn", make([]byte, 32000))
			<-done
			if !errors.Is(err, denied) || len(reads) != 1 || reads[0] != "input_audio_buffer.append" || received != 640 {
				t.Errorf("rejected write leaked input or commit: reads=%v bytes=%d err=%v", reads, received, err)
			}
		})
	}
}
