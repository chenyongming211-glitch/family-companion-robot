package realtimelive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEffectiveSessionParity(t *testing.T) {
	yes := true
	cfg := Config{Search: &yes, Voice: "Tina"}
	s := sessionSettings(cfg, "完整提示")
	if s["turn_detection"] != nil || s["input_audio_transcription"] != nil || s["enable_search"] != true {
		t.Fatal(s)
	}
	if _, ok := s["tools"]; ok {
		t.Fatal("tools must stay absent")
	}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"sample_rate":16000`) {
		t.Fatal(string(b))
	}
}
func TestSearchEvidenceOptionalMetadata(t *testing.T) {
	r := map[string]any{"usage": map[string]any{"plugins": map[string]any{"search": map[string]any{"count": json.Number("1"), "strategy": "auto"}}}, "search_info": map[string]any{"search_results": []any{nil, map[string]any{"title": "来源", "url": "https://example.com/x", "index": json.Number("2")}, map[string]any{"title": "bad", "url": "https://secret@example.com"}}}}
	e := searchEvidence(r)
	if e["count"] != int64(1) || len(e["sources"].([]any)) != 1 {
		t.Fatal(e)
	}
	if len(searchEvidence(map[string]any{"usage": "bad", "search_info": true})) != 0 {
		t.Fatal("malformed optional fields")
	}
	if _, ok := searchEvidence(map[string]any{})["count"]; ok {
		t.Fatal("unknown is not zero")
	}
}
func TestNativeStreamsBeforeDoneAndKeepsSearchConfirmation(t *testing.T) {
	gate := make(chan struct{})
	first := make(chan struct{})
	sessions := make(chan map[string]any, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		var request map[string]any
		c.ReadJSON(&request)
		sessions <- object(request["session"])
		c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": true, "search_options": map[string]any{"enable_source": true}}})
		for {
			if c.ReadJSON(&request) != nil {
				return
			}
			if request["type"] == "input_audio_buffer.commit" {
				break
			}
		}
		c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
		c.ReadJSON(&request)
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
		c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "r1", "delta": base64.StdEncoding.EncodeToString(make([]byte, 2880))})
		<-gate
		c.WriteJSON(map[string]any{"type": "response.audio_transcript.done", "response_id": "r1", "transcript": "五。"})
		c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r1", "status": "completed"}})
	}))
	defer srv.Close()
	yes := true
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Search: &yes, Voice: "Tina"}, t.TempDir())
	defer n.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := n.Open(ctx, "原始提示", false); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		done <- n.Stream(ctx, make([]byte, 640), func(kind string, v []byte) error {
			if kind == "pcm" {
				close(first)
			}
			return nil
		})
	}()
	select {
	case <-first:
	case <-ctx.Done():
		t.Fatal("no streaming PCM")
	}
	select {
	case <-done:
		t.Fatal("generation finished before gate")
	default:
	}
	close(gate)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if s := <-sessions; s["instructions"] != "原始提示" || s["enable_search"] != true {
		t.Fatal(s)
	}
}
func TestSearchMustBeConfirmedAndToolsRejectBeforeDial(t *testing.T) {
	yes := true
	n := NewNative(Config{Search: &yes, Tools: []any{map[string]any{"type": "function"}}}, t.TempDir())
	if e := n.Open(context.Background(), "", false); e == nil || e.Error() != "search_tools_incompatible" {
		t.Fatal(e)
	}
}
func TestProtectedConfigDoesNotExecuteShell(t *testing.T) {
	cfg, e := ParseConfig("DASHSCOPE_API_KEY='abc'\nQWEN_REALTIME_URL='wss://example.com/realtime?model=test'\nQWEN_VOICE=Tina\nIGNORED=$(touch /no)\n", "1")
	if e != nil || cfg.Model != "test" || cfg.ASRURL != "wss://example.com/api-ws/v1/inference" || cfg.Search == nil || !*cfg.Search {
		t.Fatal(cfg, e)
	}
	if _, e = ParseConfig("DASHSCOPE_API_KEY=$(touch /no)\nQWEN_REALTIME_URL=wss://example.com\n", "1"); e == nil {
		t.Fatal("shell expression accepted")
	}
	if _, e = ParseConfig("", "true"); e == nil {
		t.Fatal("bad bool accepted")
	}
}
func TestIntentFailurePreventsAnyProviderSubmission(t *testing.T) {
	yes := true
	n := NewNative(Config{URL: "ws://127.0.0.1:1", Search: &yes}, t.TempDir()+"/missing")
	if e := n.Open(context.Background(), "", false); e == nil {
		t.Fatal("provider call must not start without durable intent")
	}
}
func TestRecorderDiskFailureClosesSession(t *testing.T) {
	server, _ := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir()+"/missing", nil)
	if e := s.record("synthetic", nil); e == nil {
		t.Fatal("disk failure hidden")
	}
	if s.ctx.Err() == nil {
		t.Fatal("session did not fail closed")
	}
}
