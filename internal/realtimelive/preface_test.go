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
	ctx := context.Background()
	if err := order.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	answered := make(chan error, 1)
	go func() { answered <- order.answerPacket(ctx) }()
	select {
	case <-answered:
		t.Fatal("the answer overtook the acknowledgement")
	case <-time.After(50 * time.Millisecond):
	}
	order.release()
	select {
	case err := <-answered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the answer never resumed after the acknowledgement finished")
	}
}

// Once the answer speaks there is nothing to acknowledge: the user has already
// heard it, so a spoken apology would arrive as a second, contradictory answer.
func TestPrefaceOrderDeclinesOnceAnswerSpeaks(t *testing.T) {
	order := newPrefaceOrder()
	if err := order.answerPacket(context.Background()); err != nil {
		t.Fatal(err)
	}
	if order.claim() {
		t.Fatal("claiming after the answer spoke would speak over it")
	}
}

// Only the first packet needs the gate; later ones follow it in the channel.
func TestPrefaceOrderOnlyGatesTheFirstPacket(t *testing.T) {
	order := newPrefaceOrder()
	if err := order.answerPacket(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := order.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer order.release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := order.answerPacket(ctx); err != nil {
		t.Fatalf("later packets must not wait behind the acknowledgement: %v", err)
	}
}

// The wording may vary, the claim may not: a turn without search must never say
// it is looking something up.
func TestPrefaceInputNeverClaimsASearchItWillNotRun(t *testing.T) {
	thinking := prefaceInput("明天天气怎么样", false)
	if !strings.Contains(thinking, "想一想") {
		t.Fatalf("a turn without search must say it is thinking: %s", thinking)
	}
	if strings.Contains(thinking, "查") {
		t.Fatalf("a turn without search must not promise a lookup: %s", thinking)
	}
	searching := prefaceInput("明天天气怎么样", true)
	if !strings.Contains(searching, "联网查") {
		t.Fatalf("a searching turn must name the lookup: %s", searching)
	}
	if !strings.Contains(prefaceInput("明天杭州天气", true), "明天杭州天气") {
		t.Fatal("the acknowledgement must be about this turn's request")
	}
	for _, required := range []string{"十五个字", "每次换个说法", "不要给答案"} {
		if !strings.Contains(prefaceInstructions, required) {
			t.Fatalf("instructions must constrain %q", required)
		}
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
	err := n.StreamText(ctx, prefaceInput("明天天气怎么样", true), func(kind string, value []byte) error {
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
