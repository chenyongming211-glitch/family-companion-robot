package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/devicetools"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func unsupportedControlTargets() []struct{ text, outcome string } {
	return []struct{ text, outcome string }{
		{"把旁边那一台设备的音量调到百分之八十，不要动当前这台。", "unsupported_target"},
		{"把另一台的音量调到80", "unsupported_target"},
		{"把那一台音量调到80", "unsupported_target"},
		{"把其他设备音量调到80", "unsupported_target"},
		{"把旁边设备音量调到80", "unsupported_target"},
		{"把手机音量调到80", "unsupported_target"},
		{"把电脑屏幕亮度调到80", "unsupported_target"},
		{"把客厅电视的音量调到80", "unsupported_target"},
		{"把卧室空调屏幕亮度调到80", "unsupported_target"},
		{"不要改当前设备", "unsupported_target"},
		{"set the other device volume to 80", "unsupported_target"},
		{"set another device brightness to 80", "unsupported_target"},
		{"set my phone volume to 80", "unsupported_target"},
		{"set computer brightness to 80", "unsupported_target"},
		{"set the bedroom speaker volume to 80", "unsupported_target"},
		{"set the TV volume to80", "unsupported_target"},
		{"set the other speaker volume to80", "unsupported_target"},
		{"set the other watch volume to80", "unsupported_target"},
		{"set that device volume to80", "unsupported_target"},
		{"把另外一只手表的音量设为80", "unsupported_target"},
		{"don't change this device", "unsupported_target"},
		{"do not change the current device", "unsupported_target"},
		{"不要改当前设备的任何设置包括音量", "unsupported_target"},
		{"不要再动当前设备", "unsupported_target"},
		{"do not change any settings on this device including volume", "unsupported_target"},
		{"别忘了把手机音量调到80，把当前设备保持原样", "unsupported_target"},
		{"不要忘了把手机音量调到80，把当前设备保持原样", "unsupported_target"},
		{"别忘了把手机音量调到80，把当前设备音量设20", "needs_clarification"},
		{"don't forget to set the phone volume to80, set this device volume to20", "needs_clarification"},
		{"把手机音量调到80但不要动亮度，把当前设备音量设20", "needs_clarification"},
		{"手机不要动，把音量设20", "needs_clarification"},
		{"不要调手机音量，查当前天气", "unsupported_target"},
		{"把第二台设备的音量调到80", "unsupported_target"},
		{"把那个设备的音量调到80", "unsupported_target"},
		{"把另外一个设备的音量调到80", "unsupported_target"},
		{"把第2台设备的音量调到80", "unsupported_target"},
		{"把第二只手表音量调到80", "unsupported_target"},
		{"把微雪的音量调到80", "unsupported_target"},
		{"把M5的音量调到80", "unsupported_target"},
		{"set the Waveshare volume to80", "unsupported_target"},
		{"set the second device volume to80", "unsupported_target"},
		{"set device two volume to80", "unsupported_target"},
		{"不要调手机的亮度而把手机音量设为80，把当前设备屏幕亮度设为30", "needs_clarification"},
		{"不要动其他设备的亮度而把其他设备音量设80，把当前设备音量设20", "needs_clarification"},
		{"不要动空调的显示亮度而把空调音量调80，把当前设备音量设20", "needs_clarification"},
		{"set volume to80 on the TV don't touch brightness, set this device volume to20", "needs_clarification"},
		{"请不要对当前设备做任何修改，音量设为80", "unsupported_target"},
		{"当前设备音量20，另外一台音量80", "needs_clarification"},
		{"set this device volume to20 and the other device volume to80", "needs_clarification"},
	}
}

func TestNativePlannerTargetAuthorizationBeforeModelAndDispatch(t *testing.T) {
	var connections atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		var req map[string]any
		if c.ReadJSON(&req) != nil {
			return
		}
		c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
		if c.ReadJSON(&req) != nil {
			return
		}
		content, _ := object(req["item"])["content"].([]any)
		var input map[string]any
		if len(content) > 0 {
			raw, _ := object(content[0])["text"].(string)
			json.Unmarshal([]byte(raw), &input)
		}
		current, _ := input["current_request"].(string)
		relative := strings.Contains(current, "再小一点")
		for round := 0; round < 3; round++ {
			if c.ReadJSON(&req) != nil || req["type"] != "response.create" {
				return
			}
			rid := fmt.Sprintf("r%d", round)
			c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": rid}})
			if (!relative && round == 1) || (relative && round == 2) {
				c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": rid, "status": "completed", "output": []any{}}})
				return
			}
			name, args := "device_set_volume", `{"volume":20}`
			if relative && round == 0 {
				name, args = "device_get_status", `{}`
			}
			call := map[string]any{"type": "function_call", "call_id": fmt.Sprintf("c%d", round), "name": name, "arguments": args}
			c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": rid, "call_id": call["call_id"], "name": name, "arguments": args})
			c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": rid, "status": "completed", "output": []any{call}}})
			if c.ReadJSON(&req) != nil {
				return
			}
		}
	}))
	defer srv.Close()
	for _, tc := range unsupportedControlTargets() {
		t.Run(tc.text, func(t *testing.T) {
			before := connections.Load()
			executed := 0
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, t.TempDir())
			receipts, err := n.planControls(context.Background(), tc.text, `{"recent_turns":[]}`, plannerTools(), func(context.Context, string, string, string) (string, error) {
				executed++
				return `{"ok":true,"verified":true,"volume":20}`, nil
			})
			if err == nil || err.Error() != tc.outcome || executed != 0 || len(receipts) != 0 || connections.Load() != before {
				t.Fatalf("unsupported target reached model or hardware: executed=%d connections=%d receipts=%v err=%v", executed, connections.Load()-before, receipts, err)
			}
		})
	}
	for _, text := range []string{"把当前设备音量调到20", "把当前设备音量再小一点", "把当前设备音量调到20，然后查北京天气", "不要调手机，把你自己的音量设20", "手机不要动，把当前这台音量调到20", "不要动其他设备，把当前设备音量设20", "其他设备不要动，把当前设备音量设20", "不要调另一台设备，把当前设备音量设20", "不要调空调，把当前设备音量设20", "空调不要动，把当前设备音量设20", "音量别动，把屏幕亮度设30", "don't change my phone, set this device volume to20", "don't change the TV, set this device volume to20", "讲完第二章再把当前设备音量设20"} {
		t.Run(text, func(t *testing.T) {
			executed := 0
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, t.TempDir())
			receipts, err := n.planControls(context.Background(), text, `{"recent_turns":[]}`, plannerTools(), func(context.Context, string, string, string) (string, error) {
				executed++
				return `{"ok":true,"verified":true,"volume":20}`, nil
			})
			if err != nil || executed == 0 || len(receipts) == 0 {
				t.Fatal("current-device command blocked", executed, receipts, err)
			}
		})
	}
}

func TestNativeToolSettingsAreTextOnlyAndSearchDisabled(t *testing.T) {
	off := false
	tool := map[string]any{"type": "function", "function": map[string]any{"name": "device_get_status"}}
	s := sessionSettings(Config{Search: &off, Tools: []any{tool}}, "planner")
	b, _ := json.Marshal(s)
	if string(b) == "" || s["tools"] == nil {
		t.Fatalf("tool planner lost registered definitions: %s", b)
	}
	modalities, _ := s["modalities"].([]string)
	if len(modalities) != 1 || modalities[0] != "text" || s["enable_search"] != false {
		t.Fatalf("planner must be silent and search-free: %s", b)
	}
	if _, ok := s["audio"]; ok {
		t.Fatal("text planner must not configure output audio")
	}
}

func plannerTools() []map[string]any {
	// Use the Bridge's actual, fixed discovery surface.
	b := devicetools.New(time.Now())
	b.Start(time.Now())
	b.Receive(json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{}}`), time.Now())
	b.Receive(json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"self.get_device_status"},{"name":"self.audio_speaker.set_volume"},{"name":"self.screen.set_brightness"}]}}`), time.Now())
	return b.Tools()
}

func TestNativePlannerValidatesWholeResponseBeforeExecuting(t *testing.T) {
	for _, scenario := range []string{"relative", "wrong_response", "duplicate", "incomplete", "invalid_arguments", "late_cancel", "failure", "no_call", "partial_status", "batch_invalid_tail", "batch_cancel", "failed_status"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			seenOutput := make(chan string, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer c.Close()
				c.WriteJSON(map[string]any{"type": "session.created"})
				var req map[string]any
				if c.ReadJSON(&req) != nil {
					return
				}
				s := object(req["session"])
				if s["enable_search"] != false || len(s["tools"].([]any)) != 3 {
					t.Error("wrong planner settings", s)
				}
				c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
				if c.ReadJSON(&req) != nil || req["type"] != "conversation.item.create" {
					return
				}
				for turn := 0; turn < 3; turn++ {
					if c.ReadJSON(&req) != nil || req["type"] != "response.create" {
						return
					}
					rid := fmt.Sprintf("r%d", turn)
					c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": rid}})
					if scenario == "no_call" || turn == 2 {
						c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": rid, "status": "completed", "output": []any{}}})
						return
					}
					name, args := "device_get_status", "{}"
					if turn == 1 {
						name, args = "device_set_volume", `{"volume":30}`
					}
					if turn == 1 && scenario == "partial_status" {
						name, args = "device_set_brightness", `{"brightness":30}`
					}
					if scenario == "invalid_arguments" {
						args = `{"network":"bad"}`
					}
					call := map[string]any{"type": "function_call", "name": name, "arguments": args, "call_id": fmt.Sprintf("c%d", turn)}
					callRID := rid
					if scenario == "wrong_response" {
						callRID = "old"
					}
					c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": callRID, "name": name, "arguments": args, "call_id": call["call_id"]})
					output := []any{call}
					if scenario == "batch_invalid_tail" || scenario == "batch_cancel" {
						args2 := `{"volume":101}`
						if scenario == "batch_cancel" {
							args2 = `{"volume":20}`
						}
						other := map[string]any{"type": "function_call", "call_id": "batch2", "name": "device_set_volume", "arguments": args2}
						c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": rid, "call_id": "batch2", "name": "device_set_volume", "arguments": args2})
						output = append(output, other)
					}
					if scenario == "duplicate" {
						output = append(output, call)
					}
					if scenario == "incomplete" {
						c.Close()
						return
					}
					if scenario == "late_cancel" {
						cancel()
					}
					status := "completed"
					if scenario == "failed_status" {
						status = "failed"
					}
					c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": rid, "status": status, "output": output}})
					if c.ReadJSON(&req) != nil {
						return
					}
					if req["type"] == "response.cancel" {
						return
					}
					item := object(req["item"])
					if req["type"] != "conversation.item.create" || item["type"] != "function_call_output" || item["call_id"] != call["call_id"] {
						t.Error("missing matching output", req)
						return
					}
					seenOutput <- item["output"].(string)
					if scenario == "failure" {
						return
					}
				}
			}))
			defer srv.Close()
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, t.TempDir())
			defer n.Close()
			var calls []string
			receipts, err := n.planControls(ctx, "音量再小一点", `{"recent_turns":[]}`, plannerTools(), func(_ context.Context, id, name, args string) (string, error) {
				calls = append(calls, name)
				if scenario == "batch_cancel" {
					cancel()
				}
				if scenario == "failure" {
					return `{"ok":false,"verified":false,"error":"verification_failed"}`, nil
				}
				return `{"ok":true,"verified":true,"volume":30}`, nil
			})
			if scenario == "relative" {
				if err != nil || len(calls) != 2 || calls[0] != "device_get_status" || calls[1] != "device_set_volume" || len(receipts) != 2 {
					t.Fatalf("%v %v %v", calls, receipts, err)
				}
				return
			}
			if scenario == "failure" {
				if len(calls) != 1 || len(receipts) != 1 || err == nil {
					t.Fatal(calls, receipts, err)
				}
				return
			}
			if scenario == "batch_cancel" {
				if len(calls) != 1 || err == nil {
					t.Fatal("queued batch continued after cancel", calls, err)
				}
				return
			}
			if scenario == "partial_status" {
				if len(calls) != 1 || err == nil {
					t.Fatal("relative setting without relevant status", calls, err)
				}
				return
			}
			if scenario == "no_call" {
				if err != nil || len(calls) != 0 {
					t.Fatal(calls, err)
				}
				return
			}
			if err == nil || len(calls) != 0 {
				t.Fatalf("unsafe response executed: calls=%v err=%v", calls, err)
			}
		})
	}
}
