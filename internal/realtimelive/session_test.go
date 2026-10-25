package realtimelive

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSocket(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	ch := make(chan *websocket.Conn, 1)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e == nil {
			ch <- c
		}
	}))
	t.Cleanup(h.Close)
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http"), nil)
	if e != nil {
		t.Fatal(e)
	}
	s := <-ch
	t.Cleanup(func() { c.Close(); s.Close() })
	return s, c
}
func TestSealIncludesPacketAndRejectsLaterPacket(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	rt := s.addTurn()
	rt.turn.Begun = true
	if ok, e := s.binary(rt, []byte{1, 2}); e != nil || !ok {
		t.Fatal(e)
	}
	_, b, e := client.ReadMessage()
	if e != nil || binary.BigEndian.Uint32(b) != 1 {
		t.Fatal(b, e)
	}
	if e = s.interrupt(); e != nil {
		t.Fatal(e)
	}
	var drain map[string]any
	if e = client.ReadJSON(&drain); e != nil {
		t.Fatal(e)
	}
	if drain["type"] != "drain" || drain["sent"] != float64(1440) {
		t.Fatal(drain)
	}
	if ok, e := s.binary(rt, []byte{3}); e != nil || ok {
		t.Fatal("late old packet", e)
	}
	s.mu.Lock()
	ok := rt.turn.Receipt(map[string]any{"id": json.Number("1"), "basis": "CoreAudio_dataPlayedBack", "completed": json.Number("1440"), "accepted": json.Number("1440"), "terminal": "drained"})
	s.mu.Unlock()
	if !ok {
		t.Fatal("drain receipt rejected")
	}
	s.cancel()
}
func TestSettleWaitsDrainBeforeRestoringContext(t *testing.T) {
	server, _ := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	old := s.addTurn()
	old.turn.User = "讲故事"
	old.turn.Generated = "不应出现的未播稿"
	old.turn.Begun = true
	s.mu.Lock()
	old.turn.SealForDrain()
	s.mu.Unlock()
	next := s.addTurn()
	done := make(chan string, 1)
	go func() { v, _ := s.settle(context.Background(), next); done <- v }()
	select {
	case <-done:
		t.Fatal("passed unresolved drain")
	case <-time.After(40 * time.Millisecond):
	}
	s.mu.Lock()
	old.turn.Receipt(map[string]any{"id": json.Number("1"), "basis": "CoreAudio_dataPlayedBack", "completed": json.Number("0"), "accepted": json.Number("0"), "terminal": "drained"})
	s.mu.Unlock()
	select {
	case v := <-done:
		if strings.Contains(v, "不应出现的未播稿") {
			t.Fatal(v)
		}
	case <-time.After(time.Second):
		t.Fatal("not released")
	}
	s.cancel()
}
func TestStrictJSONAndProtocol(t *testing.T) {
	if _, e := decodeObject([]byte(`{"id":1} {}`)); e == nil {
		t.Fatal("trailing JSON")
	}
	turn := realtimestate.NewTurn(1)
	if turn.Receipt(map[string]any{"id": float64(1)}) {
		t.Fatal("float receipt")
	}
}
func TestReplyReleasesFirstPacketWhileProviderIsStillGenerating(t *testing.T) {
	server, client := testSocket(t)
	product, e := realtimestate.LoadProduct("../../config/product-identity.json")
	if e != nil {
		t.Fatal(e)
	}
	s := NewSession(server, Config{Model: "test", Voice: "Tina"}, t.TempDir(), product)
	defer s.cancel()
	gate := make(chan struct{})
	defer close(gate)
	s.generate = func(ctx context.Context, id int64, prompt string, pcm []byte, emit func(string, []byte) error) error {
		if !strings.Contains(prompt, "小满") {
			t.Error("identity missing")
		}
		if e := emit("pcm", make([]byte, 2880)); e != nil {
			return e
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		return emit("text", []byte("等于五。"))
	}
	rt := s.addTurn()
	done := make(chan struct{})
	go func() { s.reply(rt, make([]byte, 640)); close(done) }()
	client.SetReadDeadline(time.Now().Add(time.Second))
	for {
		kind, _, e := client.ReadMessage()
		if e != nil {
			t.Fatal(e)
		}
		if kind == websocket.BinaryMessage {
			break
		}
	}
	select {
	case <-done:
		t.Fatal("waited for full generation")
	default:
	}
	// Stop while upstream is held: no unplayed tail may enter the next context.
	if e = s.interrupt(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not free generator")
	}
	s.mu.Lock()
	asr := rt.asr
	s.mu.Unlock()
	if asr != nil {
		asr.Close()
	}
	s.cancel()
	s.wg.Wait()
}
func TestLateOldFailureDoesNotInterruptNewReply(t *testing.T) {
	server, _ := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	defer s.cancel()
	old := s.addTurn()
	next := s.addTurn()
	s.turnError(old)
	s.mu.Lock()
	defer s.mu.Unlock()
	if next.turn.Cut || s.output != next {
		t.Fatal("late error cancelled newer question")
	}
}
func TestOrderlyClientExitDoesNotCreateProviderErrors(t *testing.T) {
	for _, mode := range []string{"stop", "normal_close", "transport_close"} {
		t.Run(mode, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if e != nil {
					return
				}
				defer c.Close()
				c.WriteJSON(map[string]any{"type": "session.created"})
				var request map[string]any
				c.ReadJSON(&request)
				c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
				c.ReadJSON(&request)
				c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{}})
				for {
					if _, _, e = c.ReadMessage(); e != nil {
						return
					}
				}
			}))
			defer provider.Close()
			server, client := testSocket(t)
			dir := t.TempDir()
			s := NewSession(server, Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, dir, nil)
			result := make(chan error, 1)
			go func() { result <- s.Run() }()
			var ready map[string]any
			if e := client.ReadJSON(&ready); e != nil {
				t.Fatal(e)
			}
			if mode == "transport_close" {
				client.Close()
			} else if mode == "normal_close" {
				client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, ""), time.Now().Add(time.Second))
			} else {
				client.WriteJSON(map[string]any{"type": "stop"})
			}
			select {
			case e := <-result:
				if e != nil {
					t.Errorf("orderly exit error: %v", e)
				}
			case <-time.After(time.Second):
				t.Fatal("cleanup blocked")
			}
			b, e := os.ReadFile(filepath.Join(dir, "events.jsonl"))
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(b), "input_failed") || strings.Contains(string(b), "session_error") {
				t.Fatal("orderly close reported as provider failure:", string(b))
			}
		})
	}
}
