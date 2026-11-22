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
	"testing"
	"time"
)

// Freeze the actual selector handshake while private playback authorization is
// revoked; inspect the submitted JSON, rather than an earlier caller snapshot.
func TestMemorySelectionDeferredModelInputRefreshesAfterOpen(t *testing.T) {
	s, _ := expressionSession(t)
	allowed := scopedSubmissionSelection(t, s)
	reached, release := make(chan struct{}), make(chan struct{})
	payload := make(chan MemorySelectionInput, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if len(content) != 1 {
			t.Error("missing selector input")
			return
		}
		var input MemorySelectionInput
		if err := json.Unmarshal([]byte(object(content[0])["text"].(string)), &input); err != nil {
			t.Error(err)
			return
		}
		payload <- input
		if c.ReadJSON(&req) != nil {
			return
		}
		args := `{"intent":"none","indices":[]}`
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
		c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "r", "call_id": "selection", "name": memorySelectionName, "arguments": args})
		call := map[string]any{"type": "function_call", "call_id": "selection", "name": memorySelectionName, "arguments": args}
		c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": "completed", "output": []any{call}}})
	}))
	defer provider.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s.cfg.URL = "ws" + strings.TrimPrefix(provider.URL, "http")
	input := selectionInput()
	input.CurrentRequest, input.PlayedContext = "当前请求保留", privateSubmissionHistory
	done := make(chan error, 1)
	go func() { _, err := NewNative(s.cfg, s.directory).SelectMemory(s.ctx, input); done <- err }()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("selector Open gate not reached")
	}
	allowed.Store(false)
	close(release)
	var submitted MemorySelectionInput
	select {
	case submitted = <-payload:
	case <-time.After(time.Second):
		t.Fatal("no actual selector payload")
	}
	if err := <-done; err != nil {
		t.Fatal("selector failed instead of retaining clean current input", err)
	}
	encoded, _ := json.Marshal(submitted)
	if strings.Contains(string(encoded), "SecretTitle") || strings.Contains(string(encoded), "SecretArtist") {
		t.Error("revoked metadata submitted after Open", string(encoded))
	}
	wantCandidates, _ := json.Marshal(input.Candidates)
	gotCandidates, _ := json.Marshal(submitted.Candidates)
	if submitted.PlayedContext != `{"recent_turns":[]}` || submitted.CurrentRequest != input.CurrentRequest || string(gotCandidates) != string(wantCandidates) {
		t.Error("refresh changed current request/candidate evidence or failed to project clean history", submitted)
	}
}

// Exercise production reply ordering with actual music classification. A prior
// memory can be selected for the reply, but must never reach the music planner.
func TestMusicMemoryReplyClassifiesBeforeCheckpointAndSelection(t *testing.T) {
	for _, scenario := range []string{"ordinary", "private", "handled"} {
		t.Run(scenario, func(t *testing.T) {
			s := selectionSession(t)
			s.ownerErr = nil
			s.rec.owner.ClientType = "xiaozhi"
			s.cfg.Music = music.New()
			if scenario == "private" {
				scopedSubmissionSelection(t, s)
			}
			type submission struct {
				tool  string
				input map[string]any
			}
			submitted := make(chan submission, 4)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				settings := object(req["session"])
				tools, _ := settings["tools"].([]any)
				if len(tools) != 1 {
					t.Error("unexpected planner tools", tools)
					return
				}
				tool, _ := object(object(tools[0])["function"])["name"].(string)
				c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
				if c.ReadJSON(&req) != nil {
					return
				}
				content, _ := object(req["item"])["content"].([]any)
				if len(content) != 1 {
					t.Error("missing planner payload")
					return
				}
				var input map[string]any
				if err := json.Unmarshal([]byte(object(content[0])["text"].(string)), &input); err != nil {
					t.Error(err)
					return
				}
				submitted <- submission{tool, input}
				if c.ReadJSON(&req) != nil {
					return
				}
				args := `{"intent":"recall","indices":[1]}`
				if tool == musicPlanName {
					args = `{"action":"none","title":"","artist":""}`
					if scenario == "handled" {
						args = `{"action":"stop","title":"","artist":""}`
					}
				} else if tool != memorySelectionName {
					t.Error("unexpected tool", tool)
					return
				}
				call := map[string]any{"type": "function_call", "call_id": "plan", "name": tool, "arguments": args}
				c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
				c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "r", "call_id": "plan", "name": tool, "arguments": args})
				c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": "completed", "output": []any{call}}})
			}))
			defer provider.Close()
			s.cfg.URL = "ws" + strings.TrimPrefix(provider.URL, "http")
			ws, _ := testSocket(t)
			s.ws, s.wire = ws, &macWire{ws: ws}
			product, err := realtimestate.LoadProduct("../../config/product-identity.json")
			if err != nil {
				t.Fatal(err)
			}
			s.product = product
			prompt := ""
			s.generate = func(_ context.Context, _ int64, p string, _ []byte, _ func(string, []byte) error) error {
				prompt = p
				return context.Canceled
			}
			// A handled stop request must still reach the planner; ordinary and
			// private turns now skip classification entirely, so the ordering
			// guarantee narrows to "when classified, always before selection".
			text := "上次说的那个，记住本轮偏好紫色"
			if scenario == "handled" {
				text = "别放了，记住本轮偏好紫色"
			}
			rt := rememberedTurn(s, text, "", false)
			s.reply(rt, make([]byte, 640))
			close(submitted)
			var calls []submission
			for call := range submitted {
				calls = append(calls, call)
			}
			for i, call := range calls {
				if call.tool == musicPlanName && i != 0 {
					t.Error("music classification submitted after another call", calls)
				}
			}
			for _, call := range calls {
				if call.tool == musicPlanName {
					history, _ := call.input["played_context"].(string)
					if strings.Contains(history, "旧模板") || strings.Contains(history, "紫风铃") || !strings.Contains(history, "当前会话已播上下文") {
						t.Error("music planner did not receive current-only context", history)
					}
				}
			}
			if prompt == "" {
				t.Fatal("reply not reached")
			}
			if scenario == "handled" {
				if len(calls) != 1 || calls[0].tool != musicPlanName {
					t.Error("handled music incurred memory selection", calls)
				}
				return
			}
			if scenario == "private" {
				// A selected scoped track keeps every turn on the planner, so the
				// original classification-before-selection order still applies.
				if len(calls) != 2 || calls[0].tool != musicPlanName || calls[1].tool != memorySelectionName {
					t.Error("private turn lost classification-before-selection", calls)
				}
				return
			}
			if len(calls) != 1 || calls[0].tool != memorySelectionName {
				t.Error("ordinary turn did not select memory exactly once", calls)
			}
			wantSaved := scenario == "ordinary"
			if strings.Contains(prompt, "verified_saved") != wantSaved || !wantSaved && !strings.Contains(prompt, "not_saved") {
				t.Errorf("wrong current input receipt: wantSaved=%v verified_saved=%v not_saved=%v", wantSaved, strings.Contains(prompt, "verified_saved"), strings.Contains(prompt, "not_saved"))
			}
			if strings.Contains(prompt, "旧模板不得泄漏") || !strings.Contains(prompt, "必要旧暗号紫风铃") {
				t.Errorf("reply lost selected-only evidence: unselected=%v selected=%v", strings.Contains(prompt, "旧模板不得泄漏"), strings.Contains(prompt, "必要旧暗号紫风铃"))
			}
			s.memory.mu.Lock()
			snapshot := s.memorySnapshotLocked()
			s.memory.mu.Unlock()
			for _, turn := range snapshot.Turns {
				if scenario == "private" && turn.SessionID == s.rec.owner.SessionID && turn.TurnID >= s.music.privateFrom {
					t.Error("private current-session window entered candidate storage", turn)
				}
			}
		})
	}
}
