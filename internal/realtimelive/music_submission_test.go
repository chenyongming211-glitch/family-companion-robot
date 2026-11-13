package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/music"
	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const privateSubmissionHistory = `{"recent_turns":[{"id":2,"user":"之前什么歌","assistant_spoken_prefix":"SecretTitle SecretArtist"}]}`

// Freeze after session.update, before Open completes, then inspect the real
// conversation.item.create payload rather than a caller's earlier snapshot.
func gatedMusicInputProvider(t *testing.T, kind string) (Config, <-chan struct{}, chan struct{}, <-chan map[string]any) {
	t.Helper()
	reached := make(chan struct{})
	release := make(chan struct{})
	payload := make(chan map[string]any, 1)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		var req map[string]any
		if c.ReadJSON(&req) != nil {
			return
		}
		close(reached)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
		if c.ReadJSON(&req) != nil {
			return
		}
		content, _ := object(req["item"])["content"].([]any)
		var data map[string]any
		if len(content) != 1 {
			t.Error("missing submitted input")
			return
		}
		if e = json.Unmarshal([]byte(object(content[0])["text"].(string)), &data); e != nil {
			t.Error(e)
			return
		}
		payload <- data
		if c.ReadJSON(&req) != nil {
			return
		}
		switch kind {
		case "music":
			musicPlannerTerminal(c, `{"action":"none","title":"","artist":""}`, false)
		case "expression":
			expressionTerminal(c, "neutral")
		default:
			call := map[string]any{"type": "function_call", "call_id": "plan", "name": controlPlanName, "arguments": `{"intent":"none","target":"current"}`}
			c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
			c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": "completed", "output": []any{call}}})
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		h.Close()
	})
	return Config{URL: "ws" + strings.TrimPrefix(h.URL, "http"), Synthetic: true}, reached, release, payload
}
func scopedSubmissionSelection(t *testing.T, s *Session) *atomic.Bool {
	t.Helper()
	var allowed atomic.Bool
	allowed.Store(true)
	s.ownerErr = nil
	s.rec.owner.ClientType = "xiaozhi"
	s.rec.owner.DeviceID = memoryDeviceA
	s.cfg.Music = music.New()
	g := &guardedMusicSource{live: &allowed, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "SecretTitle", Artist: "SecretArtist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	selectMusicForFacts(t, s)
	return &allowed
}
func TestMusicDeferredModelInputRefreshesAfterOpen(t *testing.T) {
	for _, kind := range []string{"control", "music", "expression"} {
		t.Run(kind, func(t *testing.T) {
			var s *Session
			if kind == "control" {
				var c *websocket.Conn
				s, c = controlSession(t)
				discoverControls(t, s, c)
			} else {
				s, _ = expressionSession(t)
			}
			allowed := scopedSubmissionSelection(t, s)
			cfg, reached, release, payload := gatedMusicInputProvider(t, kind)
			s.cfg.URL = cfg.URL
			s.cfg.Synthetic = true
			rt := musicRequest(s, "当前请求保留")
			done := make(chan error, 1)
			go func() {
				switch kind {
				case "control":
					_, e := s.routeDeviceControls(rt, privateSubmissionHistory)
					done <- e
				case "music":
					_, e := s.routeMusic(rt, privateSubmissionHistory)
					done <- e
				default:
					r := SelectExpression(rt.ctx, s.cfg, s.directory, ExpressionInput{CurrentTranscript: rt.turn.User, DeviceName: "团子", PlayedContext: privateSubmissionHistory, AssistantPrefix: "SecretTitle SecretArtist"})
					if r.Reason != "completed" {
						done <- context.Canceled
					} else {
						done <- nil
					}
				}
			}()
			select {
			case <-reached:
			case <-time.After(time.Second):
				t.Fatal("Open gate not reached")
			}
			allowed.Store(false)
			close(release)
			var data map[string]any
			select {
			case data = <-payload:
			case <-time.After(time.Second):
				t.Fatal("no actual model payload")
			}
			if e := <-done; e != nil {
				t.Fatal("request failed instead of retaining clean current input", e)
			}
			encoded, _ := json.Marshal(data)
			if strings.Contains(string(encoded), "SecretTitle") || strings.Contains(string(encoded), "SecretArtist") {
				t.Error("revoked metadata submitted after Open", string(encoded))
			}
			current := data["current_request"]
			if kind == "expression" {
				current = data["current_transcript"]
			}
			if current != "当前请求保留" {
				t.Fatal("current input lost", data)
			}
		})
	}
}
func TestMusicMemoryRouteDistinguishesExcludedInput(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	product, e := realtimestate.LoadProduct("../../config/product-identity.json")
	if e != nil {
		t.Fatal(e)
	}
	s.product = product
	before := rememberedTurn(s, "记住之前偏好紫色", "", false)
	before.musicClassified = true // The ordinary music route classifies before routeMemory.
	_, receipt, e := s.routeMemory(before, realtimestate.ContextFor(nil))
	if e != nil || !strings.Contains(receipt, "verified_saved") {
		t.Fatal(receipt, e)
	}
	scopedSubmissionSelection(t, s) // Establish the real selection/exclusion path.
	excluded := rememberedTurn(s, "记住之后偏好绿色", "", false)
	_, receipt, e = s.routeMemory(excluded, realtimestate.ContextFor(nil))
	if e != nil || strings.Contains(receipt, "verified_saved") || !strings.Contains(receipt, "not_saved") {
		t.Error("excluded current input falsely acknowledged", receipt, e)
	}
	v, e := server.memory.Load(context.Background(), memoryDeviceA)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(v.Data), "紫色") || strings.Contains(string(v.Data), "绿色") {
		t.Fatal("wrong persisted boundary", string(v.Data))
	}
}

func TestMusicNativePromptRejectedAfterHandshakeWait(t *testing.T) {
	s, _ := expressionSession(t)
	allowed := scopedSubmissionSelection(t, s)
	_, _, guard := s.musicReplyState(privateSubmissionHistory, musicRoute{})
	if guard == nil {
		t.Fatal("private turn has no submission capability")
	}
	reached := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	var submitted atomic.Bool
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		defer close(closed)
		close(reached)
		<-release
		c.WriteJSON(map[string]any{"type": "session.created"})
		var input map[string]any
		if c.ReadJSON(&input) == nil {
			submitted.Store(true)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		h.Close()
	})
	s.cfg.URL = "ws" + strings.TrimPrefix(h.URL, "http")
	s.cfg.Synthetic = true
	ctx := context.WithValue(s.ctx, musicSubmissionGuardKey{}, guard)
	done := make(chan error, 1)
	go func() {
		done <- s.generateNative(ctx, 2, "SecretTitle SecretArtist", nil, func(string, []byte) error { return nil })
	}()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("handshake gate not reached")
	}
	allowed.Store(false)
	close(release)
	select {
	case e := <-done:
		if e == nil {
			t.Error("revoked prompt accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("native request did not stop")
	}
	<-closed
	if submitted.Load() {
		t.Fatal("revoked native instructions submitted after handshake")
	}
}
