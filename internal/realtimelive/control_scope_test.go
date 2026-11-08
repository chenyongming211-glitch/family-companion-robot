package realtimelive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestControlScopeOrdinaryRequestsNeverStartPlanner(t *testing.T) {
	var connections atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer provider.Close()
	for _, text := range []string{
		"听，一小声恐龙故事吧。", "讲一个小声说话的恐龙故事", "小恐龙把音量调到20的故事",
		"昨天我把音量调到20了", "他说把屏幕亮度设30", "‘把音量调到20’是什么意思",
		"如何把音量调到20？", "音量调节是什么原理", "如果把亮度设0会怎样", "不要把音量调到20",
		"tell a story about a quieter dinosaur", "how do I set volume to 20?", "he said set brightness to 30",
		"他说，音量调到20", "请模仿我说，音量调到20", "如果天气不好，音量设20",
		"把音量设20，不要改音量", "音量不要动，音量设20",
		"你现在有多少种屏幕表情？请告诉我数量，再举三个例子。", "先停一下，请告诉我你有多少种屏幕表情。",
		"把屏幕大一点", "把音量调暗一点",
		"妈妈说，音量调到20。", "小恐龙说，小声一点。", "爸爸说：亮度设30。", "有人说 ，音量调到20。",
		"妈妈告诉我，音量调到20。", "爸爸告诉我：亮度设30。", "有人告诉我 ，音量调到20。",
		"妈妈说\u3000，音量调到20。", "妈妈告诉我\u00a0，音量调到20。",
	} {
		t.Run(text, func(t *testing.T) {
			s, c := controlSession(t)
			discoverControls(t, s, c)
			s.cfg.URL = "ws" + strings.TrimPrefix(provider.URL, "http")
			rt := s.addTurn()
			rt.turn.User = text
			rt.transcriptOnce.Do(func() { close(rt.transcriptReady) })
			before := connections.Load()
			extra, err := s.routeDeviceControls(rt, `{"recent_turns":[]}`)
			if err != nil || !strings.Contains(extra, `"outcome":"not_candidate"`) || connections.Load() != before {
				t.Fatalf("ordinary request reached planner: extra=%s err=%v connections=%d", extra, err, connections.Load()-before)
			}
		})
	}
}

// The fake is only the remote model: scope selection, batch validation,
// dispatch, receipt handling and planner termination use production methods.
func TestControlScopePlannerAuthorizationAndStop(t *testing.T) {
	type call struct{ name, args string }
	status := call{"device_get_status", `{}`}
	volume := call{"device_set_volume", `{"volume":20}`}
	brightness := call{"device_set_brightness", `{"brightness":30}`}
	for _, tc := range []struct {
		name, request string
		batches       [][]call
		tools, calls  []string
		rounds        int
		wantError     bool
		partialStatus bool
		failWrite     bool
	}{
		{"volume_only", "把音量设20", [][]call{{volume}}, []string{"device_get_status", "device_set_volume"}, []string{"device_set_volume"}, 1, false, false, false},
		{"brightness_only", "音量别动，把屏幕亮度设30", [][]call{{brightness}}, []string{"device_get_status", "device_set_brightness"}, []string{"device_set_brightness"}, 1, false, false, false},
		{"query_stops", "查询当前设备状态", [][]call{{status}}, []string{"device_get_status"}, []string{"device_get_status"}, 1, false, false, false},
		{"tell_current_volume_query", "请告诉我当前音量是多少", [][]call{{status}}, []string{"device_get_status"}, []string{"device_get_status"}, 1, false, false, false},
		{"tell_current_brightness_query", "请告诉我屏幕亮度是多少", [][]call{{status}}, []string{"device_get_status"}, []string{"device_get_status"}, 1, false, false, false},
		{"tell_volume_query_without_please", "告诉我当前音量是多少", [][]call{{status}}, []string{"device_get_status"}, []string{"device_get_status"}, 1, false, false, false},
		{"tell_brightness_query_without_please", "告诉我屏幕亮度是多少", [][]call{{status}}, []string{"device_get_status"}, []string{"device_get_status"}, 1, false, false, false},
		{"tell_volume_query_cannot_write", "请告诉我当前音量是多少", [][]call{{volume}}, []string{"device_get_status"}, nil, 1, true, false, false},
		{"tell_brightness_query_cannot_write", "请告诉我屏幕亮度是多少", [][]call{{brightness}}, []string{"device_get_status"}, nil, 1, true, false, false},
		{"query_cannot_write", "当前音量是多少", [][]call{{volume}}, []string{"device_get_status"}, nil, 1, true, false, false},
		{"volume_cannot_brightness", "把音量设20", [][]call{{volume, brightness}}, []string{"device_get_status", "device_set_volume"}, nil, 1, true, false, false},
		{"duplicate_same_value", "把音量设20", [][]call{{volume, volume}}, []string{"device_get_status", "device_set_volume"}, nil, 1, true, false, false},
		{"conflicting_values", "把音量设20", [][]call{{volume, {"device_set_volume", `{"volume":80}`}}}, []string{"device_get_status", "device_set_volume"}, nil, 1, true, false, false},
		{"two_fields_one_batch", "把音量设20，亮度设30", [][]call{{volume, brightness}}, []string{"device_get_status", "device_set_volume", "device_set_brightness"}, []string{"device_set_volume", "device_set_brightness"}, 1, false, false, false},
		{"joint_fields_enumeration", "音量、亮度都设30", [][]call{{status}, {{"device_set_volume", `{"volume":30}`}, brightness}}, []string{"device_get_status", "device_set_volume", "device_set_brightness"}, []string{"device_get_status", "device_set_volume", "device_set_brightness"}, 2, false, false, false},
		{"two_fields_two_rounds", "把音量设20，亮度设30", [][]call{{volume}, {brightness}}, []string{"device_get_status", "device_set_volume", "device_set_brightness"}, []string{"device_set_volume", "device_set_brightness"}, 2, false, false, false},
		{"new_id_repeat_rejects_entire_batch", "把音量设20，亮度设30", [][]call{{volume}, {brightness, volume}}, []string{"device_get_status", "device_set_volume", "device_set_brightness"}, []string{"device_set_volume"}, 2, true, false, false},
		{"relative_reads_first", "把音量再小一点", [][]call{{status}, {volume}}, []string{"device_get_status", "device_set_volume"}, []string{"device_get_status", "device_set_volume"}, 2, false, false, false},
		{"direct_speak_quieter", "说话小声一点", [][]call{{status}, {volume}}, []string{"device_get_status", "device_set_volume"}, []string{"device_get_status", "device_set_volume"}, 2, false, false, false},
		{"relative_cannot_write_first", "把音量再小一点", [][]call{{volume}}, []string{"device_get_status", "device_set_volume"}, nil, 1, true, false, false},
		{"relative_batch_cannot_read_then_write", "把音量再小一点", [][]call{{status, volume}}, []string{"device_get_status", "device_set_volume"}, nil, 1, true, false, false},
		{"mixed_fields_need_relevant_read", "把音量设20和亮度调暗一点", [][]call{{volume, brightness}}, []string{"device_get_status", "device_set_volume", "device_set_brightness"}, nil, 1, true, false, false},
		{"relative_relevant_field_required", "把亮度调暗一点", [][]call{{status}, {brightness}}, []string{"device_get_status", "device_set_brightness"}, []string{"device_get_status"}, 2, true, true, false},
		{"failure_no_retry", "把音量设20", [][]call{{volume}, {volume}}, []string{"device_get_status", "device_set_volume"}, []string{"device_set_volume"}, 1, true, false, true},
		{"query_and_write", "查设备状态，把音量设20", [][]call{{volume}, {status}}, []string{"device_get_status", "device_set_volume"}, []string{"device_set_volume", "device_get_status"}, 2, false, false, false},
		{"ambiguous_no_model", "再小一点", nil, nil, nil, 0, true, false, false},
		{"quotation_no_model", "他说‘把音量设20’", nil, nil, nil, 0, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rounds atomic.Int32
			var connections atomic.Int32
			var advertised []string
			serverDone := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connections.Add(1)
				defer close(serverDone)
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
				for _, raw := range object(req["session"])["tools"].([]any) {
					advertised = append(advertised, object(object(raw)["function"])["name"].(string))
				}
				c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
				if c.ReadJSON(&req) != nil {
					return
				}
				for round := 0; ; round++ {
					if c.ReadJSON(&req) != nil || req["type"] != "response.create" {
						return
					}
					rounds.Add(1)
					rid := fmt.Sprintf("response-%d", round)
					c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": rid}})
					var batch []call
					if round < len(tc.batches) {
						batch = tc.batches[round]
					}
					output := []any{}
					for i, planned := range batch {
						id := fmt.Sprintf("call-%d-%d", round, i)
						item := map[string]any{"type": "function_call", "call_id": id, "name": planned.name, "arguments": planned.args}
						c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": rid, "call_id": id, "name": planned.name, "arguments": planned.args})
						output = append(output, item)
					}
					c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": rid, "status": "completed", "output": output}})
					if len(batch) == 0 {
						return
					}
					for range batch {
						if c.ReadJSON(&req) != nil || req["type"] != "conversation.item.create" {
							return
						}
					}
				}
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, t.TempDir())
			var executed []string
			receipts, err := n.planControls(ctx, tc.request, `{"recent_turns":[]}`, plannerTools(), func(_ context.Context, id, name, args string) (string, error) {
				executed = append(executed, name)
				if tc.failWrite {
					return `{"ok":false,"verified":false,"error":"verification_failed"}`, nil
				}
				if tc.partialStatus {
					return `{"ok":true,"verified":true,"volume":50}`, nil
				}
				if tc.name == "joint_fields_enumeration" {
					return `{"ok":true,"verified":true,"volume":30,"brightness":30}`, nil
				}
				return `{"ok":true,"verified":true,"volume":20,"brightness":30}`, nil
			})
			n.Close()
			if connections.Load() > 0 {
				<-serverDone
			}
			if (err != nil) != tc.wantError || !reflect.DeepEqual(executed, tc.calls) || int(rounds.Load()) != tc.rounds || !reflect.DeepEqual(advertised, tc.tools) || len(receipts) != len(executed) {
				b, _ := json.Marshal(receipts)
				t.Fatalf("err=%v calls=%v want=%v rounds=%d want=%d tools=%v want=%v receipts=%s", err, executed, tc.calls, rounds.Load(), tc.rounds, advertised, tc.tools, b)
			}
		})
	}
}
