package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"github.com/gorilla/websocket"
)

type toolWire struct {
	kind int
	body map[string]any
	err  error
}
type toolPeer struct {
	c       *websocket.Conn
	in      chan toolWire
	harness *toolHarness
}

func newToolPeer(c *websocket.Conn) *toolPeer {
	p := &toolPeer{c: c, in: make(chan toolWire, 256)}
	go func() {
		defer close(p.in)
		for {
			k, b, e := c.ReadMessage()
			v := map[string]any{}
			if k == websocket.TextMessage && e == nil {
				e = json.Unmarshal(b, &v)
			}
			p.in <- toolWire{k, v, e}
			if e != nil {
				return
			}
		}
	}()
	return p
}
func (p *toolPeer) send(t *testing.T, v any) {
	t.Helper()
	if err := p.c.WriteJSON(v); err != nil {
		t.Fatal(err)
	}
}
func (p *toolPeer) read(t *testing.T) toolWire {
	t.Helper()
	select {
	case v := <-p.in:
		if v.err != nil {
			t.Fatal(v.err)
		}
		return v
	case <-time.After(7 * time.Second):
		t.Fatal("timed out waiting for wire event")
		return toolWire{}
	}
}
func (p *toolPeer) want(t *testing.T, typ string) map[string]any {
	t.Helper()
	for {
		v := p.read(t)
		if v.body["type"] == "session.update" {
			p.send(t, map[string]any{"type": "session.updated"})
			cfg, _ := v.body["session"].(map[string]any)
			// Existing scenario fixtures inject provider responses. Service the new
			// generation synchronization separately; ordering tests use raw read().
			if cfg["instructions"] != nil && (len(cfg) == 1 || len(cfg) == 2 && cfg["tools"] != nil || cfg["tool_choice"] == "auto") {
				continue
			}
		}
		if v.body["type"] != typ {
			t.Fatalf("wire type = %v, want %s", v.body, typ)
		}
		return v.body
	}

}
func (p *toolPeer) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case v := <-p.in:
		t.Fatalf("unexpected wire event while waiting: %+v", v)
	case <-time.After(d):
	}
}

type toolHarness struct {
	device *toolPeer
	models chan *toolPeer
	hello  map[string]any
}

func newToolHarness(t *testing.T, mcp bool, configure ...func(*Config)) *toolHarness {
	t.Helper()
	return newToolHarnessFeatures(t, map[string]bool{"mcp": mcp}, configure...)
}

func newToolHarnessFeatures(t *testing.T, features map[string]bool, configure ...func(*Config)) *toolHarness {
	return newToolHarnessStore(t, features, nil, configure...)
}

func newToolHarnessStore(t *testing.T, features map[string]bool, store ownedRecordStore, configure ...func(*Config)) *toolHarness {
	t.Helper()
	models := make(chan *toolPeer, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		p := newToolPeer(c)
		models <- p
		// Tests own protocol progression; closing the device closes its model.
	}))
	t.Cleanup(upstream.Close)
	cfg := testConfig("ws" + strings.TrimPrefix(upstream.URL, "http"))
	for _, f := range configure {
		f(&cfg)
	}
	g, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if store != nil {
		g.records = store
	}
	server := httptest.NewServer(g.Handler())
	t.Cleanup(server.Close)
	t.Cleanup(g.Close)
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testToken}, "Device-Id": []string{"c4:9e:7e:12:99:10"}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	p := newToolPeer(c)
	p.send(t, map[string]any{"type": "hello", "version": 1, "transport": "websocket", "features": features, "audio_params": map[string]any{"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60}})
	hello := p.want(t, "hello")
	if features["playback_progress_v1"] {
		if hello["features"].(map[string]any)["playback_progress_v1"] != true {
			t.Fatal("missing server negotiation")
		}
	} else if hello["features"] != nil {
		t.Fatal("legacy server hello unexpectedly negotiated playback")
	}
	return &toolHarness{device: p, models: models, hello: hello}
}
func (h *toolHarness) model(t *testing.T) (*toolPeer, map[string]any) {
	t.Helper()
	var p *toolPeer
	select {
	case p = <-h.models:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not open")
	}
	t.Cleanup(func() { p.c.Close() })
	p.send(t, map[string]any{"type": "session.created"})
	cfg := p.want(t, "session.update")["session"].(map[string]any)
	p.harness = h
	return p, cfg
}
func rpcReply(t *testing.T, p *toolPeer, req map[string]any, result any) {
	t.Helper()
	payload := req["payload"].(map[string]any)
	p.send(t, map[string]any{"type": "mcp", "payload": map[string]any{"jsonrpc": "2.0", "id": payload["id"], "result": result}})
}
func discoverTools(t *testing.T, h *toolHarness) {
	t.Helper()
	req := h.device.want(t, "mcp")
	if req["payload"].(map[string]any)["method"] != "initialize" {
		t.Fatal(req)
	}
	rpcReply(t, h.device, req, map[string]any{})
	req = h.device.want(t, "mcp")
	if req["payload"].(map[string]any)["method"] != "tools/list" {
		t.Fatal(req)
	}
	rpcReply(t, h.device, req, map[string]any{"tools": []any{map[string]any{"name": "self.get_device_status"}, map[string]any{"name": "self.audio_speaker.set_volume"}, map[string]any{"name": "self.screen.set_brightness"}, map[string]any{"name": "untrusted_tool"}}})
}
func toolParent(t *testing.T, p *toolPeer, id string, frames int) {
	t.Helper()
	p.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
	if frames > 0 {
		p.send(t, map[string]any{"type": "response.audio.delta", "response_id": id, "delta": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes*frames))})
	}
}
func toolCall(t *testing.T, p *toolPeer, response, call, name, args string) {
	t.Helper()
	p.send(t, map[string]any{"type": "response.function_call_arguments.done", "response_id": response, "call_id": call, "name": name, "arguments": args})
}
func toolDone(t *testing.T, p *toolPeer, id string) {
	t.Helper()
	p.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
}
func toolContent(text string) any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}
func wantToolOutput(t *testing.T, p *toolPeer, call string) map[string]any {
	t.Helper()
	item := p.want(t, "conversation.item.create")["item"].(map[string]any)
	if item["type"] != "function_call_output" || item["call_id"] != call {
		t.Fatal(item)
	}
	var output map[string]any
	if e := json.Unmarshal([]byte(item["output"].(string)), &output); e != nil {
		t.Fatal(e)
	}
	return output
}
func finishToolVoice(t *testing.T, h *toolHarness, p *toolPeer) {
	t.Helper()
	p.want(t, "response.create")
	toolParent(t, p, "continuation", 1)
	toolDone(t, p, "continuation")
	if h.device.want(t, "tts")["state"] != "start" {
		t.Fatal("phantom stop before continuation")
	}
	if h.device.read(t).kind != websocket.BinaryMessage {
		t.Fatal("missing continuation audio")
	}
	if h.device.want(t, "tts")["state"] != "stop" {
		t.Fatal("missing stop")
	}
}

func TestDeviceToolsDiscoveryExecutionAndContinuation(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, cfg := h.model(t)
	defs, ok := cfg["tools"].([]any)
	if !ok || len(defs) != 5 {
		t.Fatalf("fixed native tools missing: %v", cfg)
	}
	for _, def := range defs {
		d := def.(map[string]any)
		if d["type"] != "function" || d["function"] == nil {
			t.Fatal(d)
		}
	}
	toolParent(t, p, "parent", 0)
	toolCall(t, p, "parent", "call", "device_set_volume", `{"volume":60}`)
	// Duplicate complete events must neither execute twice nor increment pending.
	toolCall(t, p, "parent", "call", "device_set_volume", `{"volume":60}`)
	req := h.device.want(t, "mcp")
	params := req["payload"].(map[string]any)["params"].(map[string]any)
	if params["name"] != "self.audio_speaker.set_volume" {
		t.Fatal(params)
	}
	rpcReply(t, h.device, req, toolContent("true"))
	req = h.device.want(t, "mcp")
	if req["payload"].(map[string]any)["params"].(map[string]any)["name"] != "self.get_device_status" {
		t.Fatal(req)
	}
	rpcReply(t, h.device, req, toolContent(`{"audio_speaker":{"volume":60},"screen":{"brightness":80}}`))
	p.quiet(t, 70*time.Millisecond) // Result alone cannot continue an active parent.
	toolDone(t, p, "parent")
	out := wantToolOutput(t, p, "call")
	if out["ok"] != true || out["verified"] != true || out["volume"] != float64(60) {
		t.Fatal(out)
	}
	finishToolVoice(t, h, p)
}

func TestDeviceToolsParentAudioDrainsBeforeContinuation(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, _ := h.model(t)
	toolParent(t, p, "parent", 4)
	toolCall(t, p, "parent", "call", "device_get_status", `{}`)
	toolDone(t, p, "parent")
	// The RPC can be interleaved with TTS start; collect it and all parent audio.
	var req map[string]any
	frames := 0
	for req == nil {
		v := h.device.read(t)
		if v.body["type"] == "mcp" {
			req = v.body
		}
		if v.kind == websocket.BinaryMessage {
			frames++
		}
	}
	rpcReply(t, h.device, req, toolContent(`{"audio_speaker":{"volume":60}}`))
	p.quiet(t, 70*time.Millisecond)
	var last time.Time
	for frames < 4 {
		v := h.device.read(t)
		if v.kind == websocket.BinaryMessage {
			frames++
			last = time.Now()
		}
		if v.body["state"] == "stop" {
			t.Fatal("intermediate parent stop")
		}
	}
	wantToolOutput(t, p, "call")
	if time.Since(last) < 50*time.Millisecond {
		t.Fatal("continuation before last parent frame drained")
	}
	p.want(t, "response.create")
	toolParent(t, p, "continuation", 1)
	toolDone(t, p, "continuation")
	if h.device.read(t).kind != websocket.BinaryMessage {
		t.Fatal("continuation must preserve open TTS block")
	}
	if h.device.want(t, "tts")["state"] != "stop" {
		t.Fatal("missing final stop")
	}

}

func TestDeviceToolsFailureAndTimeout(t *testing.T) {
	for _, failure := range []string{"device_error", "verification_failed", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			h := newToolHarness(t, true)
			discoverTools(t, h)
			p, _ := h.model(t)
			toolParent(t, p, "parent", 0)
			toolCall(t, p, "parent", "call", "device_set_volume", `{"volume":60}`)
			toolDone(t, p, "parent")
			req := h.device.want(t, "mcp")
			switch failure {
			case "device_error":
				rpcReply(t, h.device, req, map[string]any{"isError": true, "content": []any{}})
			case "verification_failed":
				rpcReply(t, h.device, req, toolContent("true"))
				req = h.device.want(t, "mcp")
				rpcReply(t, h.device, req, toolContent(`{"audio_speaker":{"volume":59}}`))
			}
			out := wantToolOutput(t, p, "call")
			if out["ok"] != false || out["verified"] != false || out["error"] != failure {
				t.Fatal(out)
			}
			finishToolVoice(t, h, p)
		})
	}
}

func TestDeviceToolsDiscoveryFailureRetainsVoiceAndBufferedEvents(t *testing.T) {
	for _, failure := range []string{"error", "timeout", "unsupported"} {
		t.Run(failure, func(t *testing.T) {
			h := newToolHarness(t, failure != "unsupported")
			if failure != "unsupported" {
				req := h.device.want(t, "mcp")
				h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "manual"})
				if failure == "error" {
					h.device.send(t, map[string]any{"type": "mcp", "payload": map[string]any{"jsonrpc": "2.0", "id": req["payload"].(map[string]any)["id"], "error": map[string]any{"code": -1}}})
				}
			}
			p, cfg := h.model(t)
			if defs, ok := cfg["tools"].([]any); ok && len(defs) != 2 {
				t.Fatal("failed discovery exposed tools")
			}
			if !strings.Contains(cfg["instructions"].(string), "不可用") {
				t.Fatal("missing unavailable policy")
			}
			if failure != "unsupported" {
				if p.want(t, "session.update")["session"].(map[string]any)["turn_detection"] != nil {
					t.Fatal("buffered listen lost")
				}
			}
			toolParent(t, p, "voice", 1)
			toolDone(t, p, "voice")
			if h.device.want(t, "tts")["state"] != "start" {
				t.Fatal("no audio start")
			}
			if h.device.read(t).kind != websocket.BinaryMessage {
				t.Fatal("missing audio")
			}
			h.device.want(t, "tts")
		})
	}
}

func TestDeviceToolsAbortDropsLateResult(t *testing.T) {
	for _, trigger := range []string{"abort", "listen", "model"} {
		t.Run(trigger, func(t *testing.T) {
			h := newToolHarness(t, true)
			discoverTools(t, h)
			old, _ := h.model(t)
			toolParent(t, old, "parent", 0)
			toolCall(t, old, "parent", "old-call", "device_set_volume", `{"volume":60}`)
			toolCall(t, old, "parent", "queued-old-call", "device_set_brightness", `{"brightness":0}`)
			toolDone(t, old, "parent")
			req := h.device.want(t, "mcp")
			// Let the no-audio parent drain internally: outstanding tools alone must interrupt.
			old.quiet(t, 50*time.Millisecond)
			event := map[string]any{"type": trigger}
			if trigger == "listen" {
				event["state"] = "start"
				event["mode"] = "auto"
			}
			if trigger == "model" {
				old.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
			} else {
				h.device.send(t, event)
			}
			stop := h.device.want(t, "tts")
			if stop["state"] != "stop" || stop["reason"] != "interrupted" {
				t.Fatalf("missing explicit interrupted stop: %v", stop)
			}
			p, _ := h.model(t)
			rpcReply(t, h.device, req, toolContent("true"))
			p.quiet(t, 70*time.Millisecond)
			toolParent(t, p, "new", 0)
			toolCall(t, p, "new", "old-call", "device_get_status", `{}`)
			toolDone(t, p, "new")
			req = h.device.want(t, "mcp")
			rpcReply(t, h.device, req, toolContent(`{"audio_speaker":{"volume":60}}`))
			wantToolOutput(t, p, "old-call")
			finishToolVoice(t, h, p)
		})
	}
}

func TestDeviceToolsTurnLimitStopsModelLoop(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, _ := h.model(t)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("parent-%d", i)
		toolParent(t, p, id, 0)
		toolCall(t, p, id, fmt.Sprintf("call-%d", i), "not_a_tool", `{}`)
		toolDone(t, p, id)
		out := wantToolOutput(t, p, fmt.Sprintf("call-%d", i))
		if out["ok"] != false {
			t.Fatal(out)
		}
		p.want(t, "response.create")
	}
	toolParent(t, p, "exhausted", 0)
	toolCall(t, p, "exhausted", "too-many", "device_get_status", `{}`)
	// An uncooperative model cannot drive unbounded function-output continuations.
	select {
	case v := <-p.in:
		if v.err == nil {
			t.Fatalf("turn limit did not close safely: %v", v.body)
		}
	case <-time.After(time.Second):
		t.Fatal("turn limit did not terminate")
	}
}

func TestDeviceToolsWaitsForEveryResultAndIgnoresDeltas(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, _ := h.model(t)
	toolParent(t, p, "parent", 0)
	p.send(t, map[string]any{"type": "response.function_call_arguments.delta", "response_id": "parent", "call_id": "partial", "name": "device_set_volume", "arguments": `{"volume":60}`})
	toolCall(t, p, "old-response", "stale", "device_set_volume", `{"volume":60}`)
	toolCall(t, p, "parent", "first", "device_get_status", `{}`)
	toolCall(t, p, "parent", "second", "device_set_brightness", `{"brightness":0}`)
	toolDone(t, p, "parent")
	req := h.device.want(t, "mcp")
	if req["payload"].(map[string]any)["params"].(map[string]any)["name"] != "self.get_device_status" {
		t.Fatal("partial/stale event executed")
	}
	rpcReply(t, h.device, req, toolContent(`{"audio_speaker":{"volume":60}}`))
	req = h.device.want(t, "mcp")
	params := req["payload"].(map[string]any)["params"].(map[string]any)
	if params["name"] != "self.screen.set_brightness" || params["arguments"].(map[string]any)["brightness"] != float64(0) {
		t.Fatal(params)
	}
	p.quiet(t, 50*time.Millisecond)
	rpcReply(t, h.device, req, toolContent("true"))
	req = h.device.want(t, "mcp")
	rpcReply(t, h.device, req, toolContent(`{"screen":{"brightness":0}}`))
	if wantToolOutput(t, p, "first")["ok"] != true || wantToolOutput(t, p, "second")["ok"] != true {
		t.Fatal("missing results")
	}
	finishToolVoice(t, h, p)
}

func TestDeviceToolsInvalidArgumentsNeverReachDevice(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, _ := h.model(t)
	toolParent(t, p, "parent", 0)
	toolCall(t, p, "parent", "invalid", "device_set_volume", `{"volume":101}`)
	toolDone(t, p, "parent")
	out := wantToolOutput(t, p, "invalid")
	if out["error"] != "invalid_arguments" || out["verified"] != false {
		t.Fatal(out)
	}
	finishToolVoice(t, h, p)
}

func TestDeviceToolsNewTranscriptionRenewsTurnBudget(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, _ := h.model(t)
	for i := 0; i < 9; i++ {
		if i == 8 {
			p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "再次读取音量"})
			h.device.want(t, "stt")
		}
		id := fmt.Sprintf("parent-%d", i)
		call := fmt.Sprintf("call-%d", i)
		toolParent(t, p, id, 0)
		toolCall(t, p, id, call, "unknown", `{}`)
		toolDone(t, p, id)
		wantToolOutput(t, p, call)
		p.want(t, "response.create")
	}
}

func TestDeviceToolsBoundedFields(t *testing.T) {
	for _, field := range []string{"call_id", "name", "arguments"} {
		t.Run(field, func(t *testing.T) {
			h := newToolHarness(t, true)
			discoverTools(t, h)
			p, _ := h.model(t)
			toolParent(t, p, "parent", 0)
			v := map[string]any{"type": "response.function_call_arguments.done", "response_id": "parent", "call_id": "call", "name": "device_get_status", "arguments": "{}"}
			sizes := map[string]int{"call_id": 257, "name": 129, "arguments": 4097}
			v[field] = strings.Repeat("x", sizes[field])
			p.send(t, v)
			select {
			case e := <-p.in:
				if e.err == nil {
					t.Fatal(e)
				}
			case <-time.After(time.Second):
				t.Fatal("oversized function fields not rejected")
			}
		})
	}
}

func TestDeviceToolsDiscoveryBufferBounds(t *testing.T) {
	for _, size := range []int{0, 16000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			h := newToolHarness(t, true)
			h.device.want(t, "mcp")
			// 129 small controls exceed event cap; 66 large controls exceed byte cap.
			count := 129
			if size > 0 {
				count = 66
			}
			for i := 0; i < count; i++ {
				h.device.send(t, map[string]any{"type": "listen", "state": "detect", "padding": strings.Repeat("x", size)})
			}
			select {
			case e := <-h.device.in:
				if e.err == nil {
					t.Fatal(e)
				}
			case <-time.After(time.Second):
				t.Fatal("discovery buffer was not bounded")
			}
			select {
			case <-h.models:
				t.Fatal("overflow opened upstream")
			default:
			}
		})
	}
}

func TestDeviceToolsRejectsNewResponseWhileResultOutstanding(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, _ := h.model(t)
	toolParent(t, p, "parent", 0)
	toolCall(t, p, "parent", "call", "device_get_status", `{}`)
	toolDone(t, p, "parent")
	h.device.want(t, "mcp")
	p.quiet(t, 50*time.Millisecond)
	// A parent that drained but still has tool work is still an active turn.
	toolParent(t, p, "unsolicited", 0)
	select {
	case ev := <-p.in:
		if ev.err == nil {
			t.Fatalf("unexpected event: %v", ev.body)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("accepted overlapping response while tool pending")
	}
}

func TestDeviceToolsFirmwareListenAfterStopDoesNotCancelContinuation(t *testing.T) {
	h := newToolHarness(t, true)
	discoverTools(t, h)
	p, _ := h.model(t)
	toolParent(t, p, "parent", 1)
	toolCall(t, p, "parent", "call", "device_get_status", `{}`)
	toolDone(t, p, "parent")
	var req map[string]any
	starts, frames := 0, 0
	for req == nil || frames < 1 {
		v := h.device.read(t)
		if v.body["type"] == "mcp" {
			req = v.body
		}
		if v.body["state"] == "start" {
			starts++
		}
		if v.kind == websocket.BinaryMessage {
			frames++
		}
	}
	rpcReply(t, h.device, req, toolContent(`{"audio_speaker":{"volume":60}}`))
	wantToolOutput(t, p, "call")
	p.want(t, "response.create")
	toolParent(t, p, "continuation", 1)
	toolDone(t, p, "continuation")
	for {
		v := h.device.read(t)
		if v.body["state"] == "start" {
			starts++
		}
		if v.kind == websocket.BinaryMessage {
			frames++
		}
		if v.body["state"] == "stop" {
			// Actual firmware automatically resumes listening after every TTS stop.
			h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "auto"})
			if frames != 2 || starts != 1 {
				t.Fatalf("intermediate TTS boundary cancels continuation: starts=%d frames=%d", starts, frames)
			}
			break
		}
	}
	select {
	case <-h.models:
		t.Fatal("firmware listening rebuilt the model")
	case <-time.After(80 * time.Millisecond):
	}
}
