package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/music"
	"familyrobot/internal/realtimestate"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Both providers stop at response.create, so overlap assertions do not depend
// on their relative durations. Cancellation is observed through the real socket.
type planningGate struct {
	musicStarted, controlStarted, controlClosed chan struct{}
	replanStarted, allowReplan                  chan struct{}
	replanArgs                                  string
	allowMusic, allowControl                    chan struct{}
	inputs                                      chan map[string]any
	controls                                    atomic.Int32
	musicAction, controlArgs                    string
	usage                                       any
}

func parallelProvider(t *testing.T, action, args string) (*planningGate, string) {
	t.Helper()
	g := &planningGate{musicStarted: make(chan struct{}), controlStarted: make(chan struct{}), controlClosed: make(chan struct{}), replanStarted: make(chan struct{}), allowMusic: make(chan struct{}), allowControl: make(chan struct{}), inputs: make(chan map[string]any, 4), musicAction: action, controlArgs: args}
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
		tools, _ := object(req["session"])["tools"].([]any)
		if len(tools) != 1 {
			t.Error("unexpected planner tools")
			return
		}
		name, _ := object(object(tools[0])["function"])["name"].(string)
		c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
		if c.ReadJSON(&req) != nil {
			return
		}
		content, _ := object(req["item"])["content"].([]any)
		var input map[string]any
		json.Unmarshal([]byte(object(content[0])["text"].(string)), &input)
		if c.ReadJSON(&req) != nil {
			return
		}
		disconnected := make(chan struct{})
		go func() { var extra any; c.ReadJSON(&extra); close(disconnected) }()
		release := g.allowMusic
		raw := `{"action":"` + g.musicAction + `","title":"","artist":""}`
		if name == controlPlanName {
			call := g.controls.Add(1)
			g.inputs <- input
			if call == 1 {
				close(g.controlStarted)
				defer close(g.controlClosed)
				release = g.allowControl
				raw = g.controlArgs
			} else {
				close(g.replanStarted)
				release = g.allowReplan
				raw = `{"intent":"none","target":"current"}`
				if g.replanArgs != "" {
					raw = g.replanArgs
				}
			}
		} else if name == musicPlanName {
			close(g.musicStarted)
		} else {
			t.Error("unexpected planner", name)
			return
		}
		if release != nil {
			select {
			case <-release:
			case <-disconnected:
				return
			}
		}
		call := map[string]any{"type": "function_call", "call_id": "plan", "name": name, "arguments": raw}
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
		c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "r", "call_id": "plan", "name": name, "arguments": raw})
		c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": "completed", "output": []any{call}, "usage": g.usage}})
		<-disconnected
	}))
	t.Cleanup(func() {
		releaseGate(g.allowMusic)
		releaseGate(g.allowControl)
		if g.allowReplan != nil {
			releaseGate(g.allowReplan)
		}
		h.Close()
	})
	return g, "ws" + strings.TrimPrefix(h.URL, "http")
}
func releaseGate(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
func awaitPlanning(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(label)
	}
}
func parallelSession(t *testing.T, url string, enabled bool) (*Session, *websocket.Conn) {
	t.Helper()
	s, c := controlSession(t)
	discoverControls(t, s, c)
	p, e := realtimestate.LoadProduct("../../config/product-identity.json")
	if e != nil {
		t.Fatal(e)
	}
	s.product = p
	s.ownerErr = nil
	s.rec.owner.ClientType = "xiaozhi"
	s.rec.owner.DeviceID = memoryDeviceA
	s.cfg.Music = music.New()
	s.cfg.URL = url
	s.cfg.Synthetic = true
	s.cfg.ParallelPlanning = enabled
	return s, c
}
func planningRequest(s *Session, text string) *liveTurn {
	// The music gate skips classification for turns with no music intent. These
	// overlap tests still need a real planner round-trip, so carry explicit
	// music intent while keeping the control wording under test.
	return musicRequest(s, "放点音乐，"+text)
}
func replyForPlanning(t *testing.T, s *Session, rt *liveTurn) (<-chan string, <-chan struct{}) {
	t.Helper()
	prompt := make(chan string, 1)
	done := make(chan struct{})
	s.generate = func(_ context.Context, _ int64, text string, _ []byte, _ func(string, []byte) error) error {
		prompt <- text
		return context.Canceled
	}
	go func() { s.reply(rt, make([]byte, 640)); close(done) }()
	t.Cleanup(func() { rt.cancel(); awaitPlanning(t, done, "reply goroutine not joined") })
	return prompt, done
}
func assertNoPrompt(t *testing.T, prompt <-chan string) {
	t.Helper()
	select {
	case p := <-prompt:
		t.Fatal("main answer began before routing settled", p)
	default:
	}
}
func TestParallelPlanningOverlapsPurePlansAndWaitsForNone(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"none","target":"current"}`)
	s, _ := parallelSession(t, url, true)
	rt := planningRequest(s, "讲个故事")
	prompt, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music did not start")
	awaitPlanning(t, g.controlStarted, "control planner did not overlap blocked music")
	assertNoPrompt(t, prompt)
	releaseGate(g.allowMusic)
	assertNoPrompt(t, prompt)
	releaseGate(g.allowControl)
	awaitPlanning(t, done, "reply not finished")
	if p := <-prompt; !strings.Contains(p, `"outcome":"none"`) {
		t.Fatal(p)
	}
	if g.controls.Load() != 1 {
		t.Fatal("ordinary chat replanned", g.controls.Load())
	}
}
func TestParallelPlanningMusicHandledCancelsAndJoinsWithoutHardware(t *testing.T) {
	g, url := parallelProvider(t, "stop", `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":80}}`)
	s, _ := parallelSession(t, url, true)
	rt := planningRequest(s, "停止音乐")
	prompt, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music did not start")
	awaitPlanning(t, g.controlStarted, "control did not overlap")
	releaseGate(g.allowMusic)
	awaitPlanning(t, done, "music handled did not cancel/join planning")
	awaitPlanning(t, g.controlClosed, "control provider remained open")
	if p := <-prompt; strings.Contains(p, "设备控制规则") {
		t.Fatal("handled music acquired control authority", p)
	}
	if len(s.controls.seen) != 0 {
		t.Fatal("hardware called on handled music")
	}
}
func TestParallelPlanningDisabledKeepsSerialOrder(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"none","target":"current"}`)
	s, _ := parallelSession(t, url, false)
	rt := planningRequest(s, "你好")
	_, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music did not start")
	select {
	case <-g.controlStarted:
		t.Fatal("default-off candidate ran")
	default:
	}
	releaseGate(g.allowMusic)
	awaitPlanning(t, g.controlStarted, "serial control missing")
	releaseGate(g.allowControl)
	awaitPlanning(t, done, "reply not finished")
}
func TestParallelPlanningInvalidatesPriorControlAnchor(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"context","direction":"up"}}`)
	s, _ := parallelSession(t, url, true)
	planningRequest(s, "之前设过音量")
	s.controls.recent = []ControlRecord{{Turn: 1, At: time.Now(), Operations: []ControlOperation{{Field: "volume", Mode: "absolute"}}}}
	rt := planningRequest(s, "再大一点")
	prompt, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music missing")
	awaitPlanning(t, g.controlStarted, "control missing")
	s.updateControlContext(s.history[0], ControlResult{NoActionIssued: false}, context.Canceled)
	releaseGate(g.allowControl)
	releaseGate(g.allowMusic)
	awaitPlanning(t, done, "reply not finished")
	if g.controls.Load() != 2 {
		t.Fatal("changed recent did not replan once", g.controls.Load())
	}
	<-g.inputs
	fresh := <-g.inputs
	if len(fresh["verified_controls"].([]any)) != 0 {
		t.Fatal("stale controls reused", fresh)
	}
	if p := <-prompt; !strings.Contains(p, `"outcome":"none"`) {
		t.Fatal(p)
	}
	if len(s.controls.seen) != 0 {
		t.Fatal("stale relative plan called hardware")
	}
}
func TestParallelPlanningCanceledOrStaleTurnNeverExecutes(t *testing.T) {
	for _, kind := range []string{"canceled", "generation", "memory_floor"} {
		t.Run(kind, func(t *testing.T) {
			g, url := parallelProvider(t, "none", `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":80}}`)
			s, _ := parallelSession(t, url, true)
			rt := planningRequest(s, "音量80")
			_, done := replyForPlanning(t, s, rt)
			awaitPlanning(t, g.musicStarted, "music missing")
			awaitPlanning(t, g.controlStarted, "control missing")
			switch kind {
			case "canceled":
				rt.cancel()
			case "generation":
				s.addTurn()
			case "memory_floor":
				s.mu.Lock()
				s.memoryFloor = rt.turn.ID + 1
				s.mu.Unlock()
			}
			releaseGate(g.allowMusic)
			releaseGate(g.allowControl)
			awaitPlanning(t, done, "stale reply did not join")
			if len(s.controls.seen) != 0 {
				t.Fatal("stale turn issued hardware")
			}
		})
	}
}
func TestParallelPlanningClearMemoryDoesNotPrefetch(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"none","target":"current"}`)
	s, _ := parallelSession(t, url, true)
	rt := musicRequest(s, "清除聊天记忆")
	if p := s.prefetchControls(rt); p != nil {
		t.Fatal("memory clear preplanned")
	}
	_, done := replyForPlanning(t, s, rt)
	// A clear command carries no music intent, so classification is gated off
	// and only the serial control path runs.
	awaitPlanning(t, g.controlStarted, "control missing")
	releaseGate(g.allowControl)
	awaitPlanning(t, done, "reply not finished")
}

func TestParallelPlanningWaitsForVerifiedHardwareReceipt(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":80}}`)
	s, c := parallelSession(t, url, true)
	rt := planningRequest(s, "音量80")
	prompt, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music missing")
	awaitPlanning(t, g.controlStarted, "control missing")
	releaseGate(g.allowControl)
	releaseGate(g.allowMusic)
	var event map[string]any
	for {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if e := c.ReadJSON(&event); e != nil {
			t.Fatal(e)
		}
		if event["type"] == "mcp" {
			break
		}
	}
	setter := object(event["payload"])
	if object(setter["params"])["name"] != "self.audio_speaker.set_volume" {
		t.Fatal(setter)
	}
	assertNoPrompt(t, prompt)
	answerMCP(t, s, setter, deviceContent(`true`))
	readback := nextMCP(t, c)
	if object(readback["params"])["name"] != "self.get_device_status" {
		t.Fatal(readback)
	}
	assertNoPrompt(t, prompt)
	answerMCP(t, s, readback, deviceContent(`{"audio_speaker":{"volume":80},"screen":{"brightness":50}}`))
	awaitPlanning(t, done, "reply not finished")
	p := <-prompt
	if !strings.Contains(p, `"outcome":"completed"`) || !strings.Contains(p, `"verified":true`) {
		t.Fatal(p)
	}
	if len(s.controls.recent) != 1 {
		t.Fatal("verified control not anchored")
	}
}
func TestParallelPlanningDispatchRechecksAnchorAfterFreshRead(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"context","direction":"up"}}`)
	s, c := parallelSession(t, url, true)
	planningRequest(s, "之前设过音量")
	s.controls.recent = []ControlRecord{{Turn: 1, At: time.Now(), Operations: []ControlOperation{{Field: "volume", Mode: "absolute"}}}}
	rt := planningRequest(s, "再大一点")
	prompt, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music missing")
	awaitPlanning(t, g.controlStarted, "control missing")
	releaseGate(g.allowControl)
	releaseGate(g.allowMusic)
	var event map[string]any
	for {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if e := c.ReadJSON(&event); e != nil {
			t.Fatal(e)
		}
		if event["type"] == "mcp" {
			break
		}
	}
	query := object(event["payload"])
	if object(query["params"])["name"] != "self.get_device_status" {
		t.Fatal(query)
	}
	s.updateControlContext(s.history[0], ControlResult{NoActionIssued: false}, context.Canceled)
	answerMCP(t, s, query, deviceContent(`{"audio_speaker":{"volume":70},"screen":{"brightness":50}}`))
	awaitPlanning(t, done, "invalidated dispatch did not settle")
	p := <-prompt
	if strings.Contains(p, `"outcome":"completed"`) || !strings.Contains(p, `"verified":false`) {
		t.Fatal("invalidated mutation appeared successful", p)
	}
	if len(s.controls.recent) != 0 {
		t.Fatal("unknown execution retained anchor")
	}
	// No setter was written: drain all immediately queued output to a close marker.
	s.mu.Lock()
	e := s.ws.WriteJSON(map[string]any{"type": "test_end"})
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	for {
		if e := c.ReadJSON(&event); e != nil {
			t.Fatal(e)
		}
		if event["type"] == "test_end" {
			break
		}
		if event["type"] == "mcp" {
			t.Fatal("stale setter dispatched", event)
		}
	}
}
func TestParallelPlanningPrivateRevocationReplansWithCleanContext(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"context","direction":"up"}}`)
	s, _ := parallelSession(t, url, true)
	allowed := scopedSubmissionSelection(t, s)
	s.cfg.URL = url
	old := planningRequest(s, "之前的私有音乐背景")
	s.mu.Lock()
	old.turn.User = "SecretTitle SecretArtist"
	old.turn.Generated = "SecretTitle SecretArtist"
	old.turn.Sent = 24000
	old.turn.Completed = 24000
	old.turn.Ended = true
	old.turn.PlaybackFinal = true
	old.turn.PlaybackBasis = "i2s_dma"
	s.mu.Unlock()
	rt := planningRequest(s, "再大一点")
	prompt, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music missing")
	awaitPlanning(t, g.controlStarted, "control missing")
	first := <-g.inputs
	b, _ := json.Marshal(first)
	if !strings.Contains(string(b), "SecretTitle") {
		t.Fatal("private projection not established", first)
	}
	allowed.Store(false)
	releaseGate(g.allowControl)
	releaseGate(g.allowMusic)
	awaitPlanning(t, done, "revoked reply did not settle")
	if g.controls.Load() != 2 {
		t.Fatal("revoked projection not replanned", g.controls.Load())
	}
	fresh := <-g.inputs
	b, _ = json.Marshal(fresh)
	if strings.Contains(string(b), "SecretTitle") {
		t.Fatal("revoked metadata reused", fresh)
	}
	if p := <-prompt; !strings.Contains(p, `"outcome":"none"`) {
		t.Fatal(p)
	}
	if len(s.controls.seen) != 0 {
		t.Fatal("revoked old plan issued hardware")
	}
}
func TestParallelPlanningRequiresFinalTranscript(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"none","target":"current"}`)
	s, _ := parallelSession(t, url, true)
	rt := s.addTurn()
	s.mu.Lock()
	rt.turn.User = "预览不是最终转写"
	s.mu.Unlock()
	if p := s.prefetchControls(rt); p != nil {
		p.close()
		t.Fatal("prefetched preview text")
	}
	select {
	case <-g.controlStarted:
		t.Fatal("preview reached provider")
	default:
	}
}

func TestParallelPlanningChangedHistoryOrFloorReplansOnce(t *testing.T) {
	for _, kind := range []string{"history", "floor"} {
		t.Run(kind, func(t *testing.T) {
			g, url := parallelProvider(t, "none", `{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"context","direction":"up"}}`)
			s, _ := parallelSession(t, url, true)
			old := planningRequest(s, "我们在聊音量")
			s.mu.Lock()
			old.turn.Generated = "我们在聊机器人音量"
			old.turn.Sent = 24000
			old.turn.Completed = 24000
			old.turn.Ended = true
			old.turn.PlaybackFinal = true
			old.turn.PlaybackBasis = "i2s_dma"
			s.mu.Unlock()
			rt := planningRequest(s, "再大一点")
			prompt, done := replyForPlanning(t, s, rt)
			awaitPlanning(t, g.musicStarted, "music missing")
			awaitPlanning(t, g.controlStarted, "control missing")
			s.mu.Lock()
			if kind == "history" {
				old.turn.Generated = "现在在聊亮度"
			} else {
				s.memoryFloor = old.turn.ID
			}
			s.mu.Unlock()
			releaseGate(g.allowControl)
			releaseGate(g.allowMusic)
			awaitPlanning(t, done, "changed input did not settle")
			if g.controls.Load() != 2 {
				t.Fatal("changed basis did not replan exactly once", g.controls.Load())
			}
			if len(s.controls.seen) != 0 {
				t.Fatal("old relative plan issued hardware")
			}
			if p := <-prompt; !strings.Contains(p, `"outcome":"none"`) {
				t.Fatal(p)
			}
		})
	}
}

func TestParallelPlanningInvalidationKeepsOriginalBudget(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"none","target":"current"}`)
	s, _ := parallelSession(t, url, true)
	planningRequest(s, "之前设过音量")
	s.controls.recent = []ControlRecord{{Turn: 1, At: time.Now(), Operations: []ControlOperation{{Field: "volume", Mode: "absolute"}}}}
	rt := planningRequest(s, "再大一点")
	p := s.prefetchControls(rt)
	if p == nil {
		t.Fatal("prefetch missing")
	}
	defer p.close()
	awaitPlanning(t, g.controlStarted, "control missing")
	releaseGate(g.allowControl)
	awaitPlanning(t, p.done, "plan missing")
	s.updateControlContext(s.history[0], ControlResult{NoActionIssued: false}, context.Canceled)
	// Advance the stored absolute deadline, without a scheduler-dependent sleep.
	p.deadline = time.Now().Add(-time.Second)
	guidance, e := s.routePrefetchedControls(rt, p)
	if e != nil || !errors.Is(p.err, context.DeadlineExceeded) || !p.result.NoActionIssued || !strings.Contains(guidance, `"no_action_issued":true`) {
		t.Fatal(guidance, e)
	}
	if g.controls.Load() != 1 || len(s.controls.seen) != 0 {
		t.Fatal("invalidation granted a fresh planning budget")
	}
}

func TestParallelPlanningSecondInvalidationStillAnswersCurrentRequest(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":80}}`)
	g.allowReplan = make(chan struct{})
	g.replanArgs = g.controlArgs
	s, _ := parallelSession(t, url, true)
	old := planningRequest(s, "我们在聊音量")
	s.mu.Lock()
	old.turn.Generated = "我们在聊机器人音量"
	old.turn.Sent, old.turn.Completed = 24000, 24000
	old.turn.Ended, old.turn.PlaybackFinal, old.turn.PlaybackBasis = true, true, "i2s_dma"
	s.mu.Unlock()
	rt := planningRequest(s, "音量设80，然后讲个故事")
	prompt, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music missing")
	awaitPlanning(t, g.controlStarted, "initial control missing")
	s.mu.Lock()
	old.turn.Generated = "现在在聊亮度"
	s.mu.Unlock()
	releaseGate(g.allowControl)
	releaseGate(g.allowMusic)
	awaitPlanning(t, g.replanStarted, "sole replan missing")
	s.mu.Lock()
	old.turn.Generated = "现在又在聊音量"
	s.mu.Unlock()
	releaseGate(g.allowReplan)
	awaitPlanning(t, done, "current request did not finish")
	select {
	case p := <-prompt:
		if !strings.Contains(p, `"outcome":"failed_or_unknown"`) || !strings.Contains(p, `"no_action_issued":true`) || strings.Contains(p, `"plan":`) {
			t.Fatal("stale candidate was not discarded with unverified guidance", p)
		}
	default:
		t.Fatal("second invalidation silently dropped the current main answer")
	}
	if g.controls.Load() != 2 || len(s.controls.seen) != 0 {
		t.Fatal("second invalidation replanned again or dispatched hardware", g.controls.Load(), len(s.controls.seen))
	}
}
