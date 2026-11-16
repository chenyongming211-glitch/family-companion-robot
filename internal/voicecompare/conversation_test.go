package voicecompare

import (
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

func productNativeFixture(t *testing.T, c *websocket.Conn, text string, inputBytes int) {
	_ = c.WriteJSON(map[string]any{"type": "session.created"})
	update := readJSON(t, c)["session"].(map[string]any)
	if strings.Contains(update["instructions"].(string), "120") || update["max_tokens"] != float64(4096) {
		t.Error("benchmark restriction leaked", update)
	}
	_ = c.WriteJSON(map[string]any{"type": "session.updated", "session": update})
	append := readJSON(t, c)
	pcm, err := base64.StdEncoding.DecodeString(append["audio"].(string))
	if err != nil || len(pcm) != inputBytes {
		t.Error("product input lost", len(pcm), err)
	}
	_ = readJSON(t, c)
	_ = c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
	_ = readJSON(t, c)
	_ = c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "product"}})
	_ = c.WriteJSON(map[string]any{"type": "response.text.done", "response_id": "product", "text": text})
	_ = c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "product", "status": "completed", "usage": usageFixture("native"), "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}}}})
}
func TestConversationProductRouteNormalTextAndNoCallCaps(t *testing.T) {
	text := strings.Repeat("这是一段自然的回复。", 20)
	c := fixtureClient(wsFixture(t, func(c *websocket.Conn) { ttsFixture(t, c, "") }))
	c.RealtimeURL = wsFixture(t, func(c *websocket.Conn) { productNativeFixture(t, c, text, 16000*2*20) })
	stages := []string{}
	c.BeforeCall = func(stage string, _ float64) error { stages = append(stages, stage); return nil }
	result, err := c.RunConversation(context.Background(), "native", "normal history", make([]byte, 16000*2*20))
	if err != nil || result.Text != text || len(result.PCM) == 0 || len(stages) <= 4 {
		t.Fatal("product generation", err, result.Text, len(result.PCM), len(stages))
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
	text := strings.Repeat("保持上下文自然回答。", 20)
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
