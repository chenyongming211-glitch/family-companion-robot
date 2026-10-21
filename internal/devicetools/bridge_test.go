package devicetools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      uint64         `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

func request(t *testing.T, u Update, method string) rpcRequest {
	t.Helper()
	if len(u.Requests) != 1 {
		t.Fatalf("want one %s request, got %+v", method, u)
	}
	var r rpcRequest
	if err := json.Unmarshal(u.Requests[0], &r); err != nil {
		t.Fatal(err)
	}
	if r.JSONRPC != "2.0" || r.ID == 0 || r.Method != method {
		t.Fatalf("bad request: %+v", r)
	}
	return r
}

func reply(b *Bridge, id uint64, result any, now time.Time) Update {
	p, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	return b.Receive(p, now)
}

func page(names []string, cursor any) map[string]any {
	ts := make([]map[string]any, 0, len(names))
	for _, n := range names {
		ts = append(ts, map[string]any{"name": n, "description": "private device description", "inputSchema": map[string]any{}})
	}
	r := map[string]any{"tools": ts}
	if cursor != nil {
		r["nextCursor"] = cursor
	}
	return r
}

func listed(t *testing.T, names ...string) *Bridge {
	t.Helper()
	b := New(epoch)
	r := request(t, b.Start(epoch), "initialize")
	r = request(t, reply(b, r.ID, map[string]any{"protocolVersion": "2024-11-05"}, epoch), "tools/list")
	u := reply(b, r.ID, page(names, nil), epoch)
	if !b.Ready() || len(u.Requests) != 0 {
		t.Fatalf("discovery not ready: %+v", u)
	}
	return b
}

func ready(t *testing.T) *Bridge {
	return listed(t, "self.get_device_status", "self.audio_speaker.set_volume", "self.screen.set_brightness", "self.reboot")
}

func output(t *testing.T, u Update, id string, gen uint64) map[string]any {
	t.Helper()
	if len(u.Results) != 1 || u.Results[0].CallID != id || u.Results[0].Generation != gen {
		t.Fatalf("bad completion: %+v", u)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(u.Results[0].Output), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func wantError(t *testing.T, u Update, id, tag string, gen uint64) {
	t.Helper()
	m := output(t, u, id, gen)
	if m["ok"] != false || m["verified"] != false || m["error"] != tag {
		t.Fatalf("want %s, got %v", tag, m)
	}
}

func status(volume, brightness int) map[string]any {
	s := fmt.Sprintf(`{"audio_speaker":{"volume":%d},"screen":{"brightness":%d},"network":{"ssid":"secret-home"},"description":"private"}`, volume, brightness)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}}
}

func ack(v string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": v}}}
}

func TestDiscoveryPaginationAndStaticWhitelist(t *testing.T) {
	b := New(epoch)
	if b.Ready() || len(b.Tools()) != 0 {
		t.Fatal("ready before discovery")
	}
	r := request(t, b.Start(epoch), "initialize")
	if len(b.Start(epoch).Requests) != 0 {
		t.Fatal("Start must be idempotent")
	}
	r = request(t, reply(b, r.ID, map[string]any{}, epoch), "tools/list")
	r = request(t, reply(b, r.ID, page([]string{"self.audio_speaker.set_volume", "self.reboot"}, "p2"), epoch), "tools/list")
	if r.Params["cursor"] != "p2" || b.Ready() {
		t.Fatal("bad cursor or premature readiness")
	}
	reply(b, r.ID, page([]string{"self.get_device_status", "self.screen.set_brightness"}, nil), epoch)
	if !b.Ready() {
		t.Fatal("discovery incomplete")
	}
	var names []string
	for _, tool := range b.Tools() {
		if tool["type"] != "function" {
			t.Fatalf("bad tool %v", tool)
		}
		f := tool["function"].(map[string]any)
		names = append(names, f["name"].(string))
		p := f["parameters"].(map[string]any)
		if p["additionalProperties"] != false {
			t.Fatal("schema must disallow unknown fields")
		}
	}
	if !reflect.DeepEqual(names, []string{"device_get_status", "device_set_volume", "device_set_brightness"}) {
		t.Fatalf("wrong tools %v", names)
	}
	raw, _ := json.Marshal(b.Tools())
	if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "reboot") {
		t.Fatal("remote metadata leaked")
	}
}

func TestDiscoveryRequiresStatusForSetters(t *testing.T) {
	b := listed(t, "self.audio_speaker.set_volume", "self.screen.set_brightness")
	if len(b.Tools()) != 0 {
		t.Fatalf("setters without readback: %v", b.Tools())
	}
	wantError(t, b.Call("x", "device_set_volume", `{"volume":42}`, 1, epoch), "x", "unavailable", 1)
	b = listed(t, "self.get_device_status", "self.audio_speaker.set_volume")
	if len(b.Tools()) != 2 {
		t.Fatal("wrong partial tools")
	}
}

func TestDiscoveryFailureAndBounds(t *testing.T) {
	for _, mode := range []string{"timeout", "rpc_error", "malformed", "repeat_cursor", "long_cursor", "wrong_cursor", "page_limit"} {
		t.Run(mode, func(t *testing.T) {
			b := New(epoch)
			r := request(t, b.Start(epoch), "initialize")
			if mode == "timeout" {
				b.Expire(epoch.Add(3 * time.Second))
			} else if mode == "rpc_error" {
				b.Receive(json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"message":"secret-home"}}`, r.ID)), epoch)
			} else {
				r = request(t, reply(b, r.ID, map[string]any{}, epoch), "tools/list")
				switch mode {
				case "malformed":
					reply(b, r.ID, map[string]any{"tools": "wrong"}, epoch)
				case "repeat_cursor":
					r = request(t, reply(b, r.ID, page([]string{"self.get_device_status"}, "same"), epoch), "tools/list")
					reply(b, r.ID, page(nil, "same"), epoch)
				case "long_cursor":
					reply(b, r.ID, page(nil, strings.Repeat("x", 1025)), epoch)
				case "wrong_cursor":
					reply(b, r.ID, page(nil, 17), epoch)
				case "page_limit":
					for i := 1; i <= 8; i++ {
						u := reply(b, r.ID, page([]string{"self.get_device_status"}, fmt.Sprint(i)), epoch)
						if i < 8 {
							r = request(t, u, "tools/list")
						} else if len(u.Requests) != 0 {
							t.Fatal("requested page nine")
						}
					}
				}
			}
			if !b.Ready() || len(b.Tools()) != 0 {
				t.Fatalf("failure did not finish safely: ready=%v tools=%v", b.Ready(), b.Tools())
			}
		})
	}
}

func TestDiscoveryDeadlineIsTotal(t *testing.T) {
	b := New(epoch)
	r := request(t, b.Start(epoch), "initialize")
	r = request(t, reply(b, r.ID, map[string]any{}, epoch.Add(2*time.Second)), "tools/list")
	u := reply(b, r.ID, page([]string{"self.get_device_status"}, nil), epoch.Add(3*time.Second))
	if !b.Ready() || len(b.Tools()) != 0 || len(u.Requests) != 0 {
		t.Fatal("late discovery reply accepted")
	}
}

func TestStatusPrivacy(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("s", "device_get_status", `{}`, 7, epoch), "tools/call")
	if r.Params["name"] != "self.get_device_status" {
		t.Fatal("incorrect mapping")
	}
	u := reply(b, r.ID, status(70, 80), epoch)
	m := output(t, u, "s", 7)
	if !reflect.DeepEqual(m, map[string]any{"ok": true, "verified": true, "volume": float64(70), "brightness": float64(80)}) {
		t.Fatalf("unfiltered or incorrect status %v", m)
	}
	if strings.Contains(u.Results[0].Output, "secret") || strings.Contains(u.Results[0].Output, "network") {
		t.Fatal("privacy leak")
	}
}

func TestSettersRequireReadbackAndRunFIFO(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("v", "device_set_volume", `{"volume":42}`, 1, epoch), "tools/call")
	if r.Params["name"] != "self.audio_speaker.set_volume" || r.Params["arguments"].(map[string]any)["volume"] != float64(42) {
		t.Fatal("bad volume mapping")
	}
	if u := b.Call("b", "device_set_brightness", `{"brightness":55}`, 1, epoch); len(u.Requests)+len(u.Results) != 0 {
		t.Fatal("setter was not queued")
	}
	u := reply(b, r.ID, ack("true"), epoch)
	if len(u.Results) != 0 {
		t.Fatal("ACK was reported as success without readback")
	}
	r = request(t, u, "tools/call")
	if r.Params["name"] != "self.get_device_status" {
		t.Fatal("missing readback")
	}
	u = reply(b, r.ID, status(42, 80), epoch)
	if m := output(t, u, "v", 1); m["ok"] != true || m["verified"] != true {
		t.Fatal(m)
	}
	r = request(t, u, "tools/call")
	if r.Params["name"] != "self.screen.set_brightness" {
		t.Fatal("FIFO did not advance")
	}
	r = request(t, reply(b, r.ID, ack("true"), epoch), "tools/call")
	if m := output(t, reply(b, r.ID, status(42, 55), epoch), "b", 1); m["ok"] != true || m["verified"] != true {
		t.Fatal(m)
	}
}

func TestBrightnessReadbackWaitsForFirmwareFade(t *testing.T) {
	for _, tc := range []struct {
		name         string
		target, step int
	}{
		{"downward", 0, 49},
		{"upward", 50, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := ready(t)
			r := request(t, b.Call("fade", "device_set_brightness", fmt.Sprintf(`{"brightness":%d}`, tc.target), 1, epoch), "tools/call")
			if r.Params["name"] != brightnessName {
				t.Fatal("missing brightness setter")
			}
			r = request(t, reply(b, r.ID, ack("true"), epoch), "tools/call")
			if u := reply(b, r.ID, status(60, tc.step), epoch); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("intermediate firmware brightness must remain pending: %+v", u)
			}
			if u := b.Expire(epoch.Add(99 * time.Millisecond)); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("readback polled before 100ms: %+v", u)
			}
			next := request(t, b.Expire(epoch.Add(100*time.Millisecond)), "tools/call")
			if next.Params["name"] != statusName || len(next.Params["arguments"].(map[string]any)) != 0 || next.ID <= r.ID {
				t.Fatalf("fade must only issue a fresh status read: %+v", next)
			}
			if m := output(t, reply(b, next.ID, status(60, tc.target), epoch.Add(100*time.Millisecond)), "fade", 1); m["ok"] != true || m["verified"] != true || m["brightness"] != float64(tc.target) {
				t.Fatal(m)
			}
		})
	}
}

func TestBrightnessReadbackObservesMultipleIntermediateValuesAndIgnoresOldReplies(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("fade", "device_set_brightness", `{"brightness":0}`, 1, epoch), "tools/call")
	r = request(t, reply(b, r.ID, ack("true"), epoch), "tools/call")
	for _, id := range []string{"next", "third", "fourth"} {
		if u := b.Call(id, "device_get_status", `{}`, 1, epoch); len(u.Requests)+len(u.Results) != 0 {
			t.Fatalf("queued call overtook brightness verification: %+v", u)
		}
	}
	wantError(t, b.Call("overflow", "device_get_status", `{}`, 1, epoch), "overflow", "queue_full", 1)
	for i, brightness := range []int{49, 25, 1} {
		now := epoch.Add(time.Duration(i) * 100 * time.Millisecond)
		if u := reply(b, r.ID, status(60, brightness), now); len(u.Requests)+len(u.Results) != 0 {
			t.Fatalf("intermediate readback completed: %+v", u)
		}
		// A consumed RPC cannot settle the call or reset its scheduled read.
		if u := reply(b, r.ID, status(60, 0), now.Add(50*time.Millisecond)); len(u.Requests)+len(u.Results) != 0 {
			t.Fatalf("duplicate old readback acted: %+v", u)
		}
		oldID := r.ID
		r = request(t, b.Expire(now.Add(100*time.Millisecond)), "tools/call")
		if r.Params["name"] != statusName || r.ID <= oldID {
			t.Fatalf("wrong fade readback: %+v", r)
		}
		if u := reply(b, oldID, status(60, 0), now.Add(100*time.Millisecond)); len(u.Requests)+len(u.Results) != 0 {
			t.Fatalf("old readback consumed new RPC: %+v", u)
		}
	}
	u := reply(b, r.ID, status(60, 0), epoch.Add(300*time.Millisecond))
	if m := output(t, u, "fade", 1); m["ok"] != true || m["verified"] != true {
		t.Fatal(m)
	}
	r = request(t, u, "tools/call")
	if m := output(t, reply(b, r.ID, status(60, 0), epoch.Add(300*time.Millisecond)), "next", 1); m["ok"] != true {
		t.Fatal(m)
	}
}

func TestBrightnessSettlingKeepsOriginalDeadlineIncludingQueue(t *testing.T) {
	for _, mode := range []string{"scheduled", "inflight"} {
		t.Run(mode, func(t *testing.T) {
			b := ready(t)
			r := request(t, b.Call("first", "device_get_status", `{}`, 1, epoch), "tools/call")
			b.Call("fade", "device_set_brightness", `{"brightness":0}`, 1, epoch)
			u := reply(b, r.ID, status(60, 50), epoch.Add(4*time.Second))
			output(t, u, "first", 1)
			r = request(t, u, "tools/call")
			observedAt := epoch.Add(4900 * time.Millisecond)
			if mode == "inflight" {
				observedAt = epoch.Add(4800 * time.Millisecond)
			}
			r = request(t, reply(b, r.ID, ack("true"), observedAt), "tools/call")
			if u = reply(b, r.ID, status(60, 49), observedAt); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("fade should still be pending: %+v", u)
			}
			if mode == "inflight" {
				// Poll is due before the deadline, and its reply arrives at the deadline.
				r = request(t, b.Expire(epoch.Add(4900*time.Millisecond)), "tools/call")
			}
			u = reply(b, r.ID, status(60, 0), epoch.Add(5*time.Second))
			wantError(t, u, "fade", "timeout", 1)
			if len(u.Requests) != 0 {
				t.Fatal("deadline issued another readback")
			}
			if u = reply(b, r.ID, status(60, 0), epoch.Add(5100*time.Millisecond)); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("late expired readback acted: %+v", u)
			}
		})
	}
}

func TestCancelBrightnessSettlingPreservesNextGeneration(t *testing.T) {
	for _, mode := range []string{"scheduled", "inflight"} {
		t.Run(mode, func(t *testing.T) {
			b := ready(t)
			r := request(t, b.Call("fade", "device_set_brightness", `{"brightness":0}`, 1, epoch), "tools/call")
			r = request(t, reply(b, r.ID, ack("true"), epoch), "tools/call")
			if u := reply(b, r.ID, status(60, 49), epoch); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("fade should still be pending: %+v", u)
			}
			b.Call("next", "device_get_status", `{}`, 2, epoch)
			if mode == "inflight" {
				r = request(t, b.Expire(epoch.Add(100*time.Millisecond)), "tools/call")
			}
			u := b.CancelGeneration(1, epoch.Add(100*time.Millisecond))
			wantError(t, u, "fade", "canceled", 1)
			next := request(t, u, "tools/call")
			if next.ID <= r.ID || next.Params["name"] != statusName {
				t.Fatal("next generation did not advance")
			}
			if u = b.Expire(epoch.Add(200 * time.Millisecond)); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("canceled scheduled readback was sent: %+v", u)
			}
			if u = reply(b, r.ID, status(60, 0), epoch.Add(200*time.Millisecond)); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("canceled late readback acted: %+v", u)
			}
			if m := output(t, reply(b, next.ID, status(60, 50), epoch.Add(200*time.Millisecond)), "next", 2); m["ok"] != true {
				t.Fatal(m)
			}
		})
	}
}

func TestReadbackMismatchAndMalformedValuesStillFail(t *testing.T) {
	for _, tc := range []struct{ name, args, text, tag string }{
		{"device_set_volume", `{"volume":60}`, `{"audio_speaker":{"volume":59},"screen":{"brightness":50}}`, "verification_failed"},
		{"device_set_brightness", `{"brightness":0}`, `{"audio_speaker":{"volume":60}}`, "verification_failed"},
		{"device_set_brightness", `{"brightness":0}`, `{"audio_speaker":{"volume":60},"screen":{"brightness":999}}`, "verification_failed"},
		{"device_set_brightness", `{"brightness":0}`, `not json`, "invalid_response"},
	} {
		t.Run(tc.name+tc.text, func(t *testing.T) {
			b := ready(t)
			r := request(t, b.Call("bad", tc.name, tc.args, 1, epoch), "tools/call")
			r = request(t, reply(b, r.ID, ack("true"), epoch), "tools/call")
			wantError(t, reply(b, r.ID, ack(tc.text), epoch), "bad", tc.tag, 1)
			if u := b.Expire(epoch.Add(100 * time.Millisecond)); len(u.Requests)+len(u.Results) != 0 {
				t.Fatalf("invalid readback scheduled polling: %+v", u)
			}
		})
	}
}

func TestInvalidArgumentsAndUnavailableTools(t *testing.T) {
	for i, tc := range []struct{ name, args, tag string }{
		{"device_get_status", `{"extra":1}`, "invalid_arguments"},
		{"device_get_status", `[]`, "invalid_arguments"},
		{"device_get_status", `null`, "invalid_arguments"},
		{"device_set_volume", `{}`, "invalid_arguments"},
		{"device_set_volume", `{"volume":-1}`, "invalid_arguments"},
		{"device_set_volume", `{"volume":101}`, "invalid_arguments"},
		{"device_set_volume", `{"volume":1.5}`, "invalid_arguments"},
		{"device_set_volume", `{"volume":"10"}`, "invalid_arguments"},
		{"device_set_volume", `{"volume":10,"other":1}`, "invalid_arguments"},
		{"device_set_volume", `{"volume":10} {}`, "invalid_arguments"},
		{"device_set_volume", `{"volume":10,"volume":11}`, "invalid_arguments"},
		{"device_set_brightness", `{"brightness":null}`, "invalid_arguments"},
		{"self.reboot", `{}`, "unavailable"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			u := ready(t).Call("bad", tc.name, tc.args, 3, epoch)
			wantError(t, u, "bad", tc.tag, 3)
			if len(u.Requests) != 0 {
				t.Fatal("invalid call sent to device")
			}
		})
	}
	wantError(t, New(epoch).Call("early", "device_get_status", `{}`, 3, epoch), "early", "not_ready", 3)
}

func TestDeviceFailuresAreSafeAndAdvanceQueue(t *testing.T) {
	for _, mode := range []string{"rpc", "isError", "false_ack", "malformed_status", "invalid_value"} {
		t.Run(mode, func(t *testing.T) {
			b := ready(t)
			name, args := "device_get_status", `{}`
			if mode == "false_ack" {
				name, args = "device_set_volume", `{"volume":42}`
			}
			r := request(t, b.Call("a", name, args, 5, epoch), "tools/call")
			b.Call("next", "device_get_status", `{}`, 5, epoch)
			var u Update
			tag := "invalid_response"
			switch mode {
			case "rpc":
				u = b.Receive(json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-1,"message":"secret-home"}}`, r.ID)), epoch)
				tag = "device_error"
			case "isError":
				u = reply(b, r.ID, map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "secret-home"}}}, epoch)
				tag = "device_error"
			case "false_ack":
				u = reply(b, r.ID, ack("false"), epoch)
				tag = "device_error"
			case "malformed_status":
				u = reply(b, r.ID, ack(`not json secret-home`), epoch)
			case "invalid_value":
				u = reply(b, r.ID, ack(`{"audio_speaker":{"volume":999},"network":{"ssid":"secret-home"}}`), epoch)
			}
			wantError(t, u, "a", tag, 5)
			request(t, u, "tools/call")
			if strings.Contains(u.Results[0].Output, "secret") {
				t.Fatal("raw error leaked")
			}
		})
	}
}

func TestQueueLimitDeduplicationAndTotalDeadline(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("a", "device_set_volume", `{"volume":42}`, 9, epoch), "tools/call")
	for _, id := range []string{"b", "c", "d"} {
		b.Call(id, "device_get_status", `{}`, 9, epoch.Add(time.Second))
	}
	wantError(t, b.Call("overflow", "device_get_status", `{}`, 9, epoch), "overflow", "queue_full", 9)
	if u := b.Call("a", "device_get_status", `{}`, 10, epoch); len(u.Requests)+len(u.Results) != 0 {
		t.Fatal("duplicate active call")
	}
	r = request(t, reply(b, r.ID, ack("true"), epoch.Add(4*time.Second)), "tools/call")
	u := reply(b, r.ID, status(42, 80), epoch.Add(5*time.Second))
	wantError(t, u, "a", "timeout", 9)
	next := request(t, u, "tools/call")
	if next.ID == r.ID {
		t.Fatal("RPC id reused")
	}
	if u = b.Receive(json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, r.ID)), epoch.Add(5*time.Second)); len(u.Requests)+len(u.Results) != 0 {
		t.Fatal("late result changed state")
	}
	u = b.Expire(epoch.Add(6 * time.Second))
	if len(u.Results) != 3 || len(u.Requests) != 0 {
		t.Fatalf("queued deadlines did not include wait time: %+v", u)
	}
	for _, c := range u.Results {
		if !strings.Contains(c.Output, `"timeout"`) {
			t.Fatal(c)
		}
	}
	if u = b.Call("a", "device_get_status", `{}`, 9, epoch.Add(7*time.Second)); len(u.Requests)+len(u.Results) != 0 {
		t.Fatal("duplicate completed call")
	}
}

func TestCancelGenerationDropsActiveAndQueuedAndIgnoresLateReply(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("a", "device_set_volume", `{"volume":42}`, 1, epoch), "tools/call")
	b.Call("b", "device_get_status", `{}`, 1, epoch)
	b.Call("c", "device_get_status", `{}`, 2, epoch)
	u := b.CancelGeneration(1, epoch)
	if len(u.Results) != 2 {
		t.Fatalf("cancellation completions: %+v", u)
	}
	for _, c := range u.Results {
		if c.Generation != 1 || !strings.Contains(c.Output, `"canceled"`) {
			t.Fatal(c)
		}
	}
	next := request(t, u, "tools/call")
	if next.Params["name"] != "self.get_device_status" {
		t.Fatal("next valid generation did not start")
	}
	if u = reply(b, r.ID, ack("true"), epoch); len(u.Requests)+len(u.Results) != 0 {
		t.Fatal("late canceled ACK triggered readback")
	}
	u = reply(b, next.ID, status(42, 80), epoch)
	if m := output(t, u, "c", 2); m["ok"] != true {
		t.Fatal(m)
	}
}

func TestUnrelatedAndMalformedRepliesDoNotConsumePendingRPC(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("s", "device_get_status", `{}`, 1, epoch), "tools/call")
	for _, p := range []string{`{`, `{"method":"notifications/test"}`, `{"jsonrpc":"2.0","id":"` + fmt.Sprint(r.ID) + `","result":{}}`, `{"jsonrpc":"2.0","id":999,"result":{}}`} {
		if u := b.Receive(json.RawMessage(p), epoch); len(u.Requests)+len(u.Results) != 0 {
			t.Fatalf("unrelated reply acted: %+v", u)
		}
	}
	if m := output(t, reply(b, r.ID, status(0, 100), epoch), "s", 1); m["ok"] != true {
		t.Fatal(m)
	}
}
