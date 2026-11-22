package realtimelive

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The acknowledgement must own the front of the turn. If the answer's first
// packet could slip into the channel while the acknowledgement is still being
// written, the user would hear the answer and then "我先查一下".
func TestPrefaceOrderKeepsAcknowledgementFirst(t *testing.T) {
	order := newPrefaceOrder()
	if !order.claim() {
		t.Fatal("the first claim must win when the answer is silent")
	}
	if !order.begin() {
		t.Fatal("the acknowledgement must take the front when nothing else has")
	}
	ctx := context.Background()
	answered := make(chan error, 1)
	go func() {
		_, err := order.answerPacket(ctx)
		answered <- err
	}()
	select {
	case <-answered:
		t.Fatal("the answer overtook the acknowledgement")
	case <-time.After(50 * time.Millisecond):
	}
	order.finish()
	select {
	case err := <-answered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the answer never resumed after the acknowledgement finished")
	}
}

// The answer arriving while the acknowledgement is still generating must win.
// This is the turn that made the robot answer "what is your name" with a promise
// to look it up and a first packet at 2.5s: the front was taken before anything
// was ready, so the answer queued behind a request that had not produced audio.
func TestPrefaceOrderLetsAnswerTakeTheFrontWhileGenerating(t *testing.T) {
	order := newPrefaceOrder()
	if !order.claim() {
		t.Fatal("claim must be granted before generation starts")
	}
	// The acknowledgement is still generating: no packet, no begin yet.
	if _, err := order.answerPacket(context.Background()); err != nil {
		t.Fatal(err)
	}
	if order.begin() {
		t.Fatal("the acknowledgement must not take the front once the answer spoke")
	}
}

// Once the answer speaks there is nothing to acknowledge: the user has already
// heard it, so a spoken apology would arrive as a second, contradictory answer.
func TestPrefaceOrderDeclinesOnceAnswerSpeaks(t *testing.T) {
	order := newPrefaceOrder()
	if _, err := order.answerPacket(context.Background()); err != nil {
		t.Fatal(err)
	}
	if order.claim() {
		t.Fatal("claiming after the answer spoke would speak over it")
	}
}

// Only the first packet needs the gate; later ones follow it in the channel.
func TestPrefaceOrderOnlyGatesTheFirstPacket(t *testing.T) {
	order := newPrefaceOrder()
	if !order.claim() || !order.begin() {
		t.Fatal("the acknowledgement must own the front")
	}
	order.finish()
	if _, err := order.answerPacket(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := order.answerPacket(ctx); err != nil {
		t.Fatalf("later packets must not wait behind the acknowledgement: %v", err)
	}
}

// An acknowledgement must never claim a lookup. The switch it used to read says
// search is allowed this turn, not that a search is running, and the robot was
// answering "what is your name" with "I'll go look that up".
func TestPrefaceInputNeverClaimsASearch(t *testing.T) {
	prompt := prefaceInput("你叫什么？", nil)
	if strings.Contains(prompt, "查") || strings.Contains(prompt, "搜") {
		t.Fatalf("the acknowledgement must not promise a lookup: %s", prompt)
	}
	if !strings.Contains(prompt, "你叫什么？") {
		t.Fatal("the acknowledgement must be about this turn")
	}
	for _, required := range []string{"不要说去查", "不要给答案", "不要用「我这就"} {
		if !strings.Contains(prefaceInstructions, required) {
			t.Fatalf("instructions must constrain %q", required)
		}
	}
}

// Repeating one sentence is the failure the user hears first, and the model
// cannot audit a rule like "vary your wording". Naming the used lines is what
// actually moves it.
func TestPrefaceInputListsTheLinesAlreadyUsed(t *testing.T) {
	avoid := []string{"嗯，我在呢", "稍等一下啊"}
	prompt := prefaceInput("明天杭州天气怎么样", avoid)
	for _, line := range avoid {
		if !strings.Contains(prompt, line) {
			t.Fatalf("prompt must list the line to avoid %q: %s", line, prompt)
		}
	}
	if strings.Contains(prefaceInput("明天杭州天气怎么样", nil), "已经说过") {
		t.Fatal("the avoid list must not appear before anything was said")
	}
}

func TestRememberPrefaceKeepsDistinctRecentLines(t *testing.T) {
	s := &Session{}
	s.rememberPreface("  ")
	if len(s.prefaceSaid) != 0 {
		t.Fatal("an empty line must not enter the list")
	}
	for _, line := range []string{"a", "b", "a", "c", "d", "e"} {
		s.rememberPreface(line)
	}
	if len(s.prefaceSaid) != prefaceRecall {
		t.Fatalf("the list must hold %d lines, got %v", prefaceRecall, s.prefaceSaid)
	}
	seen := map[string]bool{}
	for _, line := range s.prefaceSaid {
		if seen[line] {
			t.Fatalf("a repeated line was stored twice: %v", s.prefaceSaid)
		}
		seen[line] = true
	}
	if seen["a"] {
		t.Fatal("the oldest line must have fallen off")
	}
}

// The acknowledgement is generated from text, because the reply path submits
// audio and an empty audio commit is rejected by the provider.
func TestStreamTextSendsTextItemAndNoAudio(t *testing.T) {
	provider := httptest.NewServer(prefaceWebsocketHandler(t, func(c *websocket.Conn) {
		c.WriteJSON(map[string]any{"type": "session.created"})
		var req map[string]any
		if c.ReadJSON(&req) != nil || req["type"] != "session.update" {
			return
		}
		settings := object(req["session"])
		if _, present := settings["enable_search"]; present {
			t.Error("an acknowledgement must not enable search")
		}
		c.WriteJSON(map[string]any{"type": "session.updated"})
		if c.ReadJSON(&req) != nil {
			return
		}
		if req["type"] != "conversation.item.create" {
			t.Errorf("first request after the handshake must carry the text item, got %v", req["type"])
			return
		}
		content, _ := object(req["item"])["content"].([]any)
		if len(content) != 1 || object(content[0])["type"] != "input_text" {
			t.Errorf("item must carry input_text, got %v", content)
			return
		}
		if !strings.Contains(object(content[0])["text"].(string), "明天天气") {
			t.Error("item text lost the user's request")
		}
		if c.ReadJSON(&req) != nil || req["type"] != "response.create" {
			return
		}
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
		c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "r1", "delta": base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})})
		c.WriteJSON(map[string]any{"type": "response.audio_transcript.delta", "response_id": "r1", "delta": "我这就去查一下。"})
		c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r1", "status": "completed"}})
	}))
	defer provider.Close()

	dir := t.TempDir()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, dir)
	defer n.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.Open(ctx, prefaceInstructions, false); err != nil {
		t.Fatal(err)
	}
	var pcm []byte
	text := ""
	err := n.StreamText(ctx, prefaceInput("明天天气怎么样", nil), func(kind string, value []byte) error {
		switch kind {
		case "pcm":
			pcm = append(pcm, value...)
		case "text":
			text = string(value)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != 4 {
		t.Fatalf("audio not forwarded: %v", pcm)
	}
	if text != "我这就去查一下。" {
		t.Fatalf("transcript not captured: %q", text)
	}
}

func prefaceWebsocketHandler(t *testing.T, serve func(*websocket.Conn)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		serve(c)
	}
}
