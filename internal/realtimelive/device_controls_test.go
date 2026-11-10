package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/realtimestate"
	"fmt"
	"github.com/gorilla/websocket"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeviceControlCapabilityRequiresSessionDiscovery(t *testing.T) {
	p, err := realtimestate.LoadProduct(filepath.Join("..", "..", "config", "product-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, connected := range []bool{false, true} {
		facts := map[string]any{}
		if connected {
			facts["device_control_tools"] = []string{"device_get_status"}
		}
		snapshot, err := p.Snapshot(facts, `{"recent_turns":[]}`, true)
		if err != nil {
			t.Fatal(err)
		}
		got := object(object(snapshot["capabilities"])["device_control"])["status"]
		want := "not_connected"
		if connected {
			want = "available"
		}
		if got != want {
			t.Fatalf("discovered=%v: %v, want %s", connected, got, want)
		}
	}
}

func TestDeviceControlPartialDiscoveryCapabilityFacts(t *testing.T) {
	p, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		deviceTools     []string
		wantStatus      string
		wantDescription string
	}{
		{"status_only", []string{"self.get_device_status"}, "available", "查询当前设备状态；仅限当前对话设备，不含其他设备或家电"},
		{"status_volume", []string{"self.get_device_status", "self.audio_speaker.set_volume"}, "available", "查询当前设备状态、调节当前设备音量；仅限当前对话设备，不含其他设备或家电"},
		{"status_brightness", []string{"self.get_device_status", "self.screen.set_brightness"}, "available", "查询当前设备状态、调节当前设备屏幕亮度；仅限当前对话设备，不含其他设备或家电"},
		{"both", []string{"self.get_device_status", "self.audio_speaker.set_volume", "self.screen.set_brightness"}, "available", "查询当前设备状态、调节当前设备音量、调节当前设备屏幕亮度；仅限当前对话设备，不含其他设备或家电"},
		{"no_status", []string{"self.audio_speaker.set_volume", "self.screen.set_brightness"}, "not_connected", "控制音量、亮度、家电等设备"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c := controlSession(t)
			// An unfinished discovery must retain the frozen no-device facts.
			before, err := p.Snapshot(s.sessionFacts(), `{"recent_turns":[]}`, true)
			if err != nil || object(object(before["capabilities"])["device_control"])["status"] != "not_connected" {
				t.Fatal(before, err)
			}
			r := nextMCP(t, c)
			answerMCP(t, s, r, map[string]any{})
			r = nextMCP(t, c)
			var tools []any
			for _, name := range tc.deviceTools {
				tools = append(tools, map[string]any{"name": name, "description": "untrusted discovery prose"})
			}
			answerMCP(t, s, r, map[string]any{"tools": tools})
			if _, err := s.controls.Tools(context.Background()); err != nil {
				t.Fatal(err)
			}
			facts := s.sessionFacts()
			snapshot, err := p.Snapshot(facts, `{"recent_turns":[]}`, true)
			if err != nil {
				t.Fatal(err)
			}
			cap := object(object(snapshot["capabilities"])["device_control"])
			if cap["status"] != tc.wantStatus || cap["description"] != tc.wantDescription {
				t.Fatal("partial discovery advertised unsupported operation", cap)
			}
			if _, ok := s.cfg.facts()["device_control_tools"]; ok {
				t.Fatal("shared configuration acquired session capabilities")
			}
			server, _ := testSocket(t)
			mac := NewSession(server, s.cfg, t.TempDir(), nil)
			defer mac.cancel()
			macSnapshot, err := p.Snapshot(mac.sessionFacts(), `{"recent_turns":[]}`, true)
			if err != nil || object(object(macSnapshot["capabilities"])["device_control"])["status"] != "not_connected" {
				t.Fatal("Mac inherited other device capabilities", macSnapshot, err)
			}
		})
	}
}

func TestDeviceControlDiscoveryAndCallTimeouts(t *testing.T) {
	t.Run("discovery", func(t *testing.T) {
		s, c := controlSession(t)
		nextMCP(t, c)
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		tools, err := s.controls.Tools(ctx)
		if err != nil || len(tools) != 0 {
			t.Fatal(tools, err)
		}
	})
	t.Run("call", func(t *testing.T) {
		s, c := controlSession(t)
		discoverControls(t, s, c)
		done := make(chan string, 1)
		go func() {
			v, _ := s.controls.Call(context.Background(), 1, "timeout", "device_get_status", `{}`)
			done <- v
		}()
		nextMCP(t, c)
		select {
		case v := <-done:
			if !strings.Contains(v, `"error":"timeout"`) {
				t.Fatal(v)
			}
		case <-time.After(6 * time.Second):
			t.Fatal("call did not expire")
		}
	})
}

func TestDeviceControlsQueuedCancelAndVerificationFailure(t *testing.T) {
	s, c := controlSession(t)
	discoverControls(t, s, c)
	done := make(chan string, 2)
	go func() {
		v, _ := s.controls.Call(context.Background(), 1, "active", "device_get_status", `{}`)
		done <- v
	}()
	active := nextMCP(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() {
		_, err := s.controls.Call(ctx, 2, "queued", "device_set_volume", `{"volume":5}`)
		queued <- err
	}()
	// Receipt of the durable requested event proves the call entered the
	// controller queue, before we cancel it behind the outstanding status RPC.
	deadline := time.Now().Add(time.Second)
	for {
		raw, _ := os.ReadFile(filepath.Join(s.directory, "events.jsonl"))
		if strings.Count(string(raw), `"phase":"requested"`) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue not entered")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if <-queued == nil {
		t.Fatal("queued call not canceled")
	}
	answerMCP(t, s, active, deviceContent(`{"audio_speaker":{"volume":50}}`))
	<-done
	go func() {
		v, _ := s.controls.Call(context.Background(), 3, "verify", "device_set_volume", `{"volume":20}`)
		done <- v
	}()
	r := nextMCP(t, c)
	if object(object(r["params"])["arguments"])["volume"] != float64(20) {
		t.Fatal("canceled queued mutation dispatched", r)
	}
	answerMCP(t, s, r, deviceContent("true"))
	r = nextMCP(t, c)
	answerMCP(t, s, r, deviceContent(`{"audio_speaker":{"volume":21}}`))
	if v := <-done; !strings.Contains(v, "verification_failed") {
		t.Fatal(v)
	}
}

func TestDeviceControlRecorderFailurePreventsMutation(t *testing.T) {
	s, c := controlSession(t)
	discoverControls(t, s, c)
	if err := os.Remove(filepath.Join(s.directory, "events.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.directory, "events.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.controls.Call(ctx, 1, "unsafe", "device_set_volume", `{"volume":20}`); err == nil {
		t.Fatal("disk failure ignored")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("mutation written after failed durable intent")
	}
}

func TestDeviceControlLifetimeCallBudget(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	w := newXiaozhiWire(server, 1, 60, "scope")
	w.mcp = true
	s.wire = w
	s.enableDeviceControls()
	for i := 0; i < controlSessionCallLimit; i++ {
		s.controls.seen[fmt.Sprintf("old-%d", i)] = true
	}
	s.startDeviceControls()
	defer func() { s.cancel(); s.wg.Wait() }()
	discoverControls(t, s, client)
	v, err := s.controls.Call(context.Background(), 1, "new", "device_get_status", `{}`)
	if err != nil || !strings.Contains(v, "call_limit") {
		t.Fatal(v, err)
	}
}

func controlSession(t *testing.T) (*Session, *websocket.Conn) {
	t.Helper()
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	w := newXiaozhiWire(server, 1, 60, "scope")
	w.mcp = true
	s.wire = w
	s.enableDeviceControls()
	s.startDeviceControls()
	t.Cleanup(func() { s.cancel(); s.wg.Wait() })
	return s, client
}

func nextMCP(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(time.Second))
	var e map[string]any
	if err := c.ReadJSON(&e); err != nil {
		t.Fatal(err)
	}
	if e["type"] != "mcp" || e["session_id"] != "scope" {
		t.Fatal(e)
	}
	return object(e["payload"])
}
func answerMCP(t *testing.T, s *Session, req map[string]any, result any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
	if err := s.controls.Receive(raw); err != nil {
		t.Fatal(err)
	}
}
func discoverControls(t *testing.T, s *Session, c *websocket.Conn) {
	t.Helper()
	r := nextMCP(t, c)
	if r["method"] != "initialize" {
		t.Fatal(r)
	}
	answerMCP(t, s, r, map[string]any{})
	r = nextMCP(t, c)
	if r["method"] != "tools/list" {
		t.Fatal(r)
	}
	answerMCP(t, s, r, map[string]any{"tools": []any{map[string]any{"name": "self.get_device_status"}, map[string]any{"name": "self.audio_speaker.set_volume"}, map[string]any{"name": "self.screen.set_brightness"}, map[string]any{"name": "evil.tool", "description": "untrusted"}}})
	tools, err := s.controls.Tools(context.Background())
	if err != nil || len(tools) != 3 {
		t.Fatal(tools, err)
	}
}
func deviceContent(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}

func TestDeviceControlsDiscoveryReadbackAndIsolation(t *testing.T) {
	a, ca := controlSession(t)
	b, cb := controlSession(t)
	discoverControls(t, a, ca)
	discoverControls(t, b, cb)
	result := make(chan string, 1)
	go func() {
		v, _ := a.controls.Call(context.Background(), 1, "set", "device_set_volume", `{"volume":20}`)
		result <- v
	}()
	r := nextMCP(t, ca)
	if object(r["params"])["name"] != "self.audio_speaker.set_volume" {
		t.Fatal(r)
	}
	// A reply on the other session cannot complete A's call.
	answerMCP(t, b, r, deviceContent("true"))
	select {
	case <-result:
		t.Fatal("cross-device result")
	case <-time.After(20 * time.Millisecond):
	}
	answerMCP(t, a, r, deviceContent("true"))
	r = nextMCP(t, ca)
	if object(r["params"])["name"] != "self.get_device_status" {
		t.Fatal(r)
	}
	answerMCP(t, a, r, deviceContent(`{"audio_speaker":{"volume":20},"screen":{"brightness":50},"secret":"never"}`))
	if v := <-result; !strings.Contains(v, `"verified":true`) || strings.Contains(v, "secret") {
		t.Fatal(v)
	}
	if v, err := a.controls.Call(context.Background(), 1, "set", "device_set_volume", `{"volume":20}`); err != nil || !strings.Contains(v, "duplicate_call") {
		t.Fatal("duplicate executed or hung", v, err)
	}
	if v, err := a.controls.Call(context.Background(), 1, "different-call-id", "device_set_volume", `{"volume":80}`); err != nil || !strings.Contains(v, "duplicate_field") {
		t.Fatal("same turn changed the same field again", v, err)
	}
}

func TestDeviceControlsFailureInvalidAndCancellation(t *testing.T) {
	s, c := controlSession(t)
	discoverControls(t, s, c)
	for _, args := range []string{`{"volume":101}`, `{"volume":null}`, `{"volume":2,"volume":3}`, `{"volume":2,"target":"other"}`} {
		v, err := s.controls.Call(context.Background(), 1, args, "device_set_volume", args)
		if err != nil || !strings.Contains(v, "invalid_arguments") {
			t.Fatal(v, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.controls.Call(ctx, 2, "cancel-before", "device_set_volume", `{"volume":20}`); err == nil {
		t.Fatal("canceled dispatch")
	}
	ctx, cancel = context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.controls.Call(ctx, 3, "cancel-active", "device_set_volume", `{"volume":20}`)
		done <- err
	}()
	r := nextMCP(t, c)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("late active success")
	}
	answerMCP(t, s, r, deviceContent("true"))
	go func() {
		_, err := s.controls.Call(context.Background(), 4, "new", "device_get_status", `{}`)
		done <- err
	}()
	r = nextMCP(t, c)
	answerMCP(t, s, r, deviceContent(`{"audio_speaker":{"volume":20}}`))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMCPNegotiationScopeAndCandidateRouting(t *testing.T) {
	w := newXiaozhiWire(nil, 1, 60, "scope")
	if e, err := w.control([]byte(`{"type":"mcp","payload":{"jsonrpc":"2.0","id":1,"result":{}}}`)); e != nil || err != nil {
		t.Fatal(e, err)
	}
	w.mcp = true
	if e, err := w.control([]byte(`{"type":"mcp","payload":{"jsonrpc":"2.0","id":1,"result":{}}}`)); e == nil || err != nil {
		t.Fatal(e, err)
	}
	if _, err := w.control([]byte(`{"type":"mcp","session_id":"other","payload":{}}`)); err == nil {
		t.Fatal("wrong MCP scope")
	}
	server, _ := testSocket(t)
	mac := NewSession(server, Config{}, t.TempDir(), nil)
	defer mac.cancel()
	if mac.controls != nil {
		t.Fatal("Mac got hardware tools")
	}
	rt := mac.addTurn()
	start := time.Now()
	extra, err := mac.routeDeviceControls(rt, `{"recent_turns":[]}`)
	if err != nil || extra != "" || time.Since(start) > 100*time.Millisecond {
		t.Fatal("Mac path delayed", extra, err)
	}
}
