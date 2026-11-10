package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func semanticProvider(t *testing.T, args, scenario string, inspect func(map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		settings := object(req["session"])
		tools, _ := settings["tools"].([]any)
		if settings["enable_search"] != false || len(tools) != 1 || object(object(tools[0])["function"])["name"] != "submit_device_control_plan" {
			t.Error("unsafe settings", settings)
		}
		c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
		if c.ReadJSON(&req) != nil {
			return
		}
		content, _ := object(req["item"])["content"].([]any)
		if inspect != nil {
			var input map[string]any
			json.Unmarshal([]byte(object(content[0])["text"].(string)), &input)
			inspect(input)
		}
		if c.ReadJSON(&req) != nil || req["type"] != "response.create" {
			return
		}
		if scenario == "timeout" {
			c.SetReadDeadline(time.Now().Add(4 * time.Second))
			c.ReadJSON(&req)
			return
		}
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
		name := "submit_device_control_plan"
		if scenario == "physical" {
			name = "device_set_volume"
		}
		call := map[string]any{"type": "function_call", "call_id": "plan", "name": name, "arguments": args}
		rid := "r"
		if scenario == "identity" {
			rid = "old"
		}
		c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": rid, "call_id": "plan", "name": name, "arguments": args})
		output := []any{call}
		if scenario == "duplicate" {
			output = append(output, call)
		}
		if scenario == "no_call" {
			output = nil
		}
		if scenario == "mismatch" {
			call["arguments"] = `{"intent":"none","target":"current"}`
		}
		if scenario == "incomplete" {
			return
		}
		status := "completed"
		if scenario == "failed" {
			status = "failed"
		}
		c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": status, "output": output}})
	}))
}
func runSemantic(t *testing.T, args, scenario string, cb func(context.Context, string, string, string) (string, error)) (ControlResult, error) {
	t.Helper()
	srv := semanticProvider(t, args, scenario, nil)
	defer srv.Close()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, t.TempDir())
	return n.RunControlPlan(context.Background(), ControlInput{CurrentRequest: "眼睛都快睁不开了，能让我看清吗", PlayedContext: `{"recent_turns":[]}`, Turn: 4}, plannerTools(), cb)
}
func TestSemanticAdmissionWithoutKeywords(t *testing.T) {
	r, e := runSemantic(t, `{"intent":"none","target":"current"}`, "", func(context.Context, string, string, string) (string, error) {
		t.Fatal("none dispatched")
		return "", nil
	})
	if e != nil || r.Outcome != "none" || !r.NoActionIssued {
		t.Fatal(r, e)
	}
}
func TestSemanticPlansValidateBeforeAnyDispatch(t *testing.T) {
	cases := []string{
		`{"intent":"query","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20}}`,
		`{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20},"brightness":{"mode":"absolute","basis":"explicit","reference":"current","value":101}}`,
		`{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20.5}}`,
		`{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"current","direction":"up","value":20}}`,
		`{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"current","direction":"up","step":0}}`,
		`{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"current","direction":"up","step":11,"feedback":true}}`,
		`{"intent":"none","intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20}}`,
		`{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20,"value":30}}`,
		`{"intent":"none","target":"current","extra":true}`,
		`{"intent":"none","target":"current","volume":{"mode":"query"}}`,
		`{"intent":"adjust","target":"other","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20}}`,
		`{"intent":"query","target":"current"}`,
		`{"intent":"adjust","target":"current","volume":{"mode":"query"}}`,
	}
	for i, args := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r, e := runSemantic(t, args, "", func(context.Context, string, string, string) (string, error) {
				t.Fatal("invalid plan dispatched")
				return "", nil
			})
			if e == nil || !r.NoActionIssued {
				t.Fatal(r, e)
			}
		})
	}
	for _, scenario := range []string{"physical", "identity", "duplicate", "mismatch", "incomplete", "failed", "no_call"} {
		t.Run(scenario, func(t *testing.T) {
			r, e := runSemantic(t, `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20}}`, scenario, func(context.Context, string, string, string) (string, error) {
				t.Fatal("bad response dispatched")
				return "", nil
			})
			if e == nil || !r.NoActionIssued {
				t.Fatal(r, e)
			}
		})
	}
}
func TestSemanticTerminalNonActions(t *testing.T) {
	for _, args := range []string{`{"intent":"none","target":"current"}`, `{"intent":"clarify","target":"ambiguous","reason":"unclear_field"}`, `{"intent":"unsupported","target":"other","reason":"unsupported"}`} {
		r, e := runSemantic(t, args, "", func(context.Context, string, string, string) (string, error) {
			t.Fatal("nonaction dispatched")
			return "", nil
		})
		if e != nil || !r.NoActionIssued {
			t.Fatal(r, e)
		}
	}
}
func TestSemanticFreshReadArithmeticBoundariesAndExactWrites(t *testing.T) {
	for _, tc := range []struct {
		mode              string
		start, want, step int
		direction         string
		noop              bool
	}{
		{"relative", 37, 47, 0, "up", false}, {"relative", 7, 0, 0, "down", false}, {"relative", 96, 100, 0, "up", false}, {"relative", 100, 100, 0, "up", true}, {"relative", 45, 42, 3, "down", false}, {"absolute", 37, 81, 0, "", false},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			action := map[string]any{"mode": tc.mode, "basis": "explicit", "reference": "current"}
			if tc.mode == "absolute" {
				action["value"] = tc.want
			} else {
				action["direction"] = tc.direction
				if tc.step != 0 {
					action["step"] = tc.step
				}
			}
			raw, _ := json.Marshal(map[string]any{"intent": "adjust", "target": "current", "volume": action})
			var calls []string
			r, e := runSemantic(t, string(raw), "", func(_ context.Context, id, name, args string) (string, error) {
				calls = append(calls, name)
				v := tc.start
				if name != "device_get_status" {
					var a map[string]int
					json.Unmarshal([]byte(args), &a)
					if a["volume"] != tc.want {
						t.Fatal("wrong arithmetic", args)
					}
					v = tc.want
				}
				return fmt.Sprintf(`{"ok":true,"verified":true,"volume":%d}`, v), nil
			})
			want := 1
			if tc.mode == "relative" && !tc.noop {
				want = 2
			}
			if e != nil || len(calls) != want || r.Outcome != "completed" {
				t.Fatal(r, calls, e)
			}
			if tc.noop && !strings.Contains(string(r.Receipts[len(r.Receipts)-1].Output), `"unchanged":true`) {
				t.Fatal("missing unchanged", r)
			}
		})
	}
}
func TestSemanticReceiptFailuresStopLaterWrites(t *testing.T) {
	for _, output := range []string{`{"ok":true,"verified":true,"volume":21}`, `{"ok":true,"verified":true}`, `{"ok":false,"verified":false}`, `invalid`} {
		t.Run(output, func(t *testing.T) {
			calls := 0
			r, e := runSemantic(t, `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20},"brightness":{"mode":"absolute","basis":"explicit","reference":"current","value":30}}`, "", func(context.Context, string, string, string) (string, error) { calls++; return output, nil })
			if e == nil || calls != 1 || r.NoActionIssued {
				t.Fatal(r, e, calls)
			}
		})
	}
	r, e := runSemantic(t, `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20}}`, "", func(context.Context, string, string, string) (string, error) {
		return "", errors.New("unknown transport")
	})
	if e == nil || r.NoActionIssued {
		t.Fatal("unknown falsely no action", r, e)
	}
	calls := 0
	r, e = runSemantic(t, `{"intent":"query","target":"current","volume":{"mode":"query"},"brightness":{"mode":"query"}}`, "", func(context.Context, string, string, string) (string, error) {
		calls++
		return `{"ok":true,"verified":true,"volume":20}`, nil
	})
	if e == nil || calls != 1 {
		t.Fatal("missing query field accepted", r, e)
	}
}
func TestSemanticHistoryProjection(t *testing.T) {
	var items []any
	for i := 0; i < 7; i++ {
		items = append(items, map[string]any{"user": fmt.Sprint(i) + strings.Repeat("x", 1500), "assistant_spoken_prefix": "played", "generated": "SECRET"})
	}
	raw, _ := json.Marshal(map[string]any{"recent_turns": items, "prior_session_memory": map[string]any{"secret": "PRIOR"}})
	h := controlHistory(string(raw))
	if strings.Contains(h, "PRIOR") || strings.Contains(h, "SECRET") {
		t.Fatal(h)
	}
	var data map[string]any
	json.Unmarshal([]byte(h), &data)
	if len(data["recent_turns"].([]any)) != 4 {
		t.Fatal(h)
	}
}
func TestSemanticOrdinaryPlanningBudget(t *testing.T) {
	start := time.Now()
	r, e := runSemantic(t, `{"intent":"none","target":"current"}`, "timeout", func(context.Context, string, string, string) (string, error) {
		t.Fatal("timeout dispatch")
		return "", nil
	})
	if e == nil || !r.NoActionIssued || time.Since(start) > 3500*time.Millisecond {
		t.Fatal(r, e, time.Since(start))
	}
}

func TestSemanticCapabilitiesCancellationAndPartialReceipts(t *testing.T) {
	t.Run("unsupported_second_field", func(t *testing.T) {
		p, e := parseControlPlan(`{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20},"brightness":{"mode":"absolute","basis":"explicit","reference":"current","value":30}}`, map[string]bool{"device_get_status": true, "device_set_volume": true})
		if e == nil || p != nil {
			t.Fatal(p, e)
		}
	})
	t.Run("cancel_after_first_write", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		v, b := 20, 30
		p := &ControlPlan{Intent: "adjust", Target: "current", Volume: &ControlAction{Mode: "absolute", Value: &v}, Brightness: &ControlAction{Mode: "absolute", Value: &b}}
		r := ControlResult{NoActionIssued: true}
		calls := 0
		e := executeControlPlan(ctx, p, func(context.Context, string, string, string) (string, error) {
			calls++
			cancel()
			return `{"ok":true,"verified":true,"volume":20}`, nil
		}, &r)
		if e == nil || calls != 1 || r.NoActionIssued {
			t.Fatal(r, e, calls)
		}
	})
	t.Run("cancel_before_dispatch", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		v := 20
		r := ControlResult{NoActionIssued: true}
		e := executeControlPlan(ctx, &ControlPlan{Volume: &ControlAction{Mode: "absolute", Value: &v}}, func(context.Context, string, string, string) (string, error) {
			t.Fatal("canceled dispatch")
			return "", nil
		}, &r)
		if e == nil || !r.NoActionIssued {
			t.Fatal(r, e)
		}
	})
	t.Run("partial_verified_preserved", func(t *testing.T) {
		r, e := runSemantic(t, `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20},"brightness":{"mode":"absolute","basis":"explicit","reference":"current","value":30}}`, "", func(_ context.Context, id, name, args string) (string, error) {
			if name == "device_set_brightness" {
				return `{"ok":false,"verified":false}`, nil
			}
			return `{"ok":true,"verified":true,"volume":20}`, nil
		})
		if e == nil || len(r.Receipts) != 2 || !verifiedControlOutput(string(r.Receipts[0].Output)) || r.Outcome != "partial_or_unknown" {
			t.Fatal(r, e)
		}
	})
}
func TestSemanticMismatchCannotLeaveVerifiedReceipt(t *testing.T) {
	r, e := runSemantic(t, `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20}}`, "", func(context.Context, string, string, string) (string, error) {
		return `{"ok":true,"verified":true,"volume":21}`, nil
	})
	if e == nil || len(r.Receipts) != 1 || verifiedControlOutput(string(r.Receipts[0].Output)) {
		t.Fatal("mismatched request appears verified", r, e)
	}
}
func TestSemanticContextLifetimeUnknownAndStaleCompletion(t *testing.T) {
	s, c := controlSession(t)
	discoverControls(t, s, c)
	now := time.Now()
	records := []ControlRecord{{1, now, []ControlOperation{{"volume", "absolute", ""}}}, {4, now.Add(-6 * time.Minute), []ControlOperation{{"volume", "absolute", ""}}}, {4, now, []ControlOperation{{"brightness", "relative", "up"}}}, {5, now, []ControlOperation{{"volume", "query", ""}}}}
	projected := recentControlRecords(records, 5, now)
	if len(projected) != 1 || projected[0].Turn != 4 {
		t.Fatal(projected)
	}
	old := s.addTurn()
	current := s.addTurn()
	v := 20
	r := ControlResult{Outcome: "completed", Plan: &ControlPlan{Volume: &ControlAction{Mode: "absolute", Value: &v}}, NoActionIssued: false}
	s.updateControlContext(old, r, nil)
	if len(s.controls.recent) != 0 {
		t.Fatal("stale completion anchored")
	}
	s.updateControlContext(current, r, nil)
	if len(s.controls.recent) != 1 {
		t.Fatal("current verified missing")
	}
	s.updateControlContext(old, ControlResult{NoActionIssued: false}, errors.New("unknown mutation"))
	if len(s.controls.recent) != 0 {
		t.Fatal("unknown mutation kept context")
	}
}
func TestSemanticSessionRoutesOrdinaryChatOnce(t *testing.T) {
	var calls int
	srv := semanticProvider(t, `{"intent":"none","target":"current"}`, "", func(m map[string]any) {
		calls++
		if m["current_request"] != "讲个故事" {
			t.Error(m)
		}
	})
	defer srv.Close()
	s, c := controlSession(t)
	discoverControls(t, s, c)
	s.cfg.URL = "ws" + strings.TrimPrefix(srv.URL, "http")
	on := true
	s.cfg.Search = &on
	rt := s.addTurn()
	rt.turn.User = "讲个故事"
	rt.transcriptOnce.Do(func() { close(rt.transcriptReady) })
	extra, e := s.routeDeviceControls(rt, `{"recent_turns":[]}`)
	if e != nil || !strings.Contains(extra, `"outcome":"none"`) || calls != 1 || len(s.cfg.Tools) != 0 || !*s.cfg.Search {
		t.Fatal(extra, e, calls)
	}
}
func TestSemanticProjectionDropsUnplayedPair(t *testing.T) {
	got := controlHistory(`{"recent_turns":[{"user":"old request","generated":"never played"}]}`)
	if strings.Contains(got, "old request") {
		t.Fatal(got)
	}
}
func TestSemanticFeedbackAndExactKeyValidation(t *testing.T) {
	for _, raw := range []string{
		`{"intent":"adjust","target":"current","brightness":{"mode":"relative","basis":"feedback","reference":"current","direction":"up","step":20}}`,
		`{"intent":"adjust","target":"current","brightness":{"mode":"absolute","basis":"feedback","reference":"current","value":90}}`,
		`{"INTENT":"none","target":"current"}`,
		`{"intent":"adjust","target":"current","volume":{"MODE":"absolute","value":20}}`,
	} {
		r, e := runSemantic(t, raw, "", func(context.Context, string, string, string) (string, error) {
			t.Fatal("invalid semantic shape dispatched")
			return "", nil
		})
		if e == nil || !r.NoActionIssued {
			t.Fatal(r, e)
		}
	}
}
func TestSemanticToolSchemaUsesNativeSingleTypes(t *testing.T) {
	p := object(object(controlPlanTool()["function"])["parameters"])
	for _, field := range []string{"volume", "brightness"} {
		a := object(object(p["properties"])[field])
		if a["type"] != "object" {
			t.Fatal("provider cannot reliably use nullable-union action schema", a)
		}
		for key, value := range object(a["properties"]) {
			if _, ok := object(value)["type"].(string); !ok {
				t.Fatal("provider cannot reliably use nullable-union argument schema", key)
			}
		}
	}
}
func TestSemanticAmbiguousContextPlanClarifiesBeforeHardware(t *testing.T) {
	srv := semanticProvider(t, `{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"context","direction":"up"}}`, "", nil)
	defer srv.Close()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, t.TempDir())
	r, e := n.RunControlPlan(context.Background(), ControlInput{CurrentRequest: "再大一点", PlayedContext: `{"recent_turns":[{"user":"音量亮度都设70","assistant_spoken_prefix":"两项都已设70"}]}`, Turn: 5, VerifiedControls: []ControlRecord{{4, time.Now(), []ControlOperation{{"volume", "absolute", ""}, {"brightness", "absolute", ""}}}}}, plannerTools(), func(context.Context, string, string, string) (string, error) {
		t.Fatal("ambiguous anchor dispatched")
		return "", nil
	})
	if e != nil || r.Outcome != "needs_clarification" || !r.NoActionIssued {
		t.Fatal(r, e)
	}
}
func TestSemanticMalformedLaterReceiptPreservesEarlierVerification(t *testing.T) {
	r, e := runSemantic(t, `{"intent":"adjust","target":"current","volume":{"mode":"absolute","basis":"explicit","reference":"current","value":20},"brightness":{"mode":"absolute","basis":"explicit","reference":"current","value":30}}`, "", func(_ context.Context, id, name, args string) (string, error) {
		if name == "device_set_brightness" {
			return "not json", nil
		}
		return `{"ok":true,"verified":true,"volume":20}`, nil
	})
	if e == nil || len(r.Receipts) != 2 || !verifiedControlOutput(string(r.Receipts[0].Output)) || verifiedControlOutput(string(r.Receipts[1].Output)) {
		t.Fatal("earlier receipt overwritten or malformed receipt unbounded", r, e)
	}
}
func TestSemanticSchemaCannotNudgeExplicitValuesIntoBounds(t *testing.T) {
	p := object(object(controlPlanTool()["function"])["parameters"])
	a := object(object(p["properties"])["volume"])
	for _, key := range []string{"value", "step"} {
		property := object(object(a["properties"])[key])
		if property["minimum"] != nil || property["maximum"] != nil {
			t.Fatal("semantic schema should preserve out-of-range original; Go rejects it before execution", key, property)
		}
	}
}
func TestSemanticNewerPlayedTopicOverridesOlderVerifiedAnchor(t *testing.T) {
	for _, ops := range [][]ControlOperation{{{"volume", "absolute", ""}, {"brightness", "absolute", ""}}, {{"brightness", "absolute", ""}}} {
		srv := semanticProvider(t, `{"intent":"adjust","target":"current","volume":{"mode":"relative","basis":"explicit","reference":"context","direction":"up"}}`, "", nil)
		n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, t.TempDir())
		calls := 0
		r, e := n.RunControlPlan(context.Background(), ControlInput{CurrentRequest: "再大一点", PlayedContext: `{"recent_turns":[{"id":2,"user":"现在声音听起来还是有点小","assistant_spoken_prefix":"我们正在说机器人音量。"}]}`, Turn: 3, VerifiedControls: []ControlRecord{{1, time.Now(), ops}}}, plannerTools(), func(_ context.Context, id, name, args string) (string, error) {
			calls++
			if name == "device_get_status" {
				return `{"ok":true,"verified":true,"volume":20}`, nil
			}
			if args != `{"volume":30}` {
				t.Error(args)
			}
			return `{"ok":true,"verified":true,"volume":30}`, nil
		})
		srv.Close()
		if e != nil || r.Outcome != "completed" || calls != 2 {
			t.Fatal("newer actually played topic was blocked by older device record", r, e, calls)
		}
	}
}
