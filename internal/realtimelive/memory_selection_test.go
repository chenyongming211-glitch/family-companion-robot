package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemorySelectionSettleKeepsCurrentButExcludesOld(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	old := memorySession(t, server, memoryDeviceA)
	rememberedTurn(old, "旧故事", "旧模板开场不能进新回答", true)
	if err := old.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	s := memorySession(t, server, memoryDeviceA)
	rememberedTurn(s, "当前问题", "当前已经播放的上下文", true)
	rt := rememberedTurn(s, "讲个新故事", "", false)
	got, err := s.settle(context.Background(), rt)
	if err != nil || strings.Contains(got, "旧模板开场") || !strings.Contains(got, "当前已经播放的上下文") {
		t.Fatalf("unselected old text leaked or current context lost: %s %v", got, err)
	}
}

func TestMemorySelectionArgumentsFailClosed(t *testing.T) {
	for _, raw := range []string{
		`{}`, `null`, `{"intent":"unknown","indices":[]}`, `{"intent":"none","indices":[0]}`,
		`{"intent":"recall","indices":[]}`, `{"intent":"recall","indices":[0,0]}`, `{"intent":"recall","indices":[0,1,2,3]}`,
		`{"intent":"recall","indices":[-1]}`, `{"intent":"recall","indices":[3]}`, `{"intent":"recall","indices":[0.0]}`,
		`{"intent":"recall","indices":["0"]}`, `{"intent":"none","indices":null}`, `{"intent":"none","indices":[],"extra":true}`,
		`{"intent":"none","intent":"recall","indices":[]}`, `{"intent":"none","indices":[],"indices":[]}`,
		`{"intent":"none","indices":[]} {}`, `{"intent":"recall","indices":[null]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if p, err := memorySelectionArguments(raw, 3); err == nil {
				t.Fatalf("invalid selection accepted: %#v", p)
			}
		})
	}
	for _, raw := range []string{`{"intent":"none","indices":[]}`, `{"intent":"recall","indices":[2,0]}`, `{"intent":"continue","indices":[1]}`} {
		if _, err := memorySelectionArguments(raw, 3); err != nil {
			t.Fatal(raw, err)
		}
	}
}

// Only fake cloud-local WebSocket providers are used by these tests.
func memorySelectionProvider(t *testing.T, args, scenario string, inspect func(map[string]any), gate <-chan struct{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if settings["enable_search"] != false || len(tools) != 1 || object(object(tools[0])["function"])["name"] != memorySelectionName || settings["modalities"].([]any)[0] != "text" {
			t.Error("unsafe memory selector tools", settings)
		}
		c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
		if c.ReadJSON(&req) != nil {
			return
		}
		content, _ := object(req["item"])["content"].([]any)
		var input map[string]any
		json.Unmarshal([]byte(object(content[0])["text"].(string)), &input)
		if inspect != nil {
			inspect(input)
		}
		if c.ReadJSON(&req) != nil || req["type"] != "response.create" {
			return
		}
		if gate != nil {
			<-gate
		}
		if scenario == "timeout" {
			c.SetReadDeadline(time.Now().Add(4 * time.Second))
			c.ReadJSON(&req)
			return
		}
		if scenario == "provider_error" {
			c.WriteJSON(map[string]any{"type": "error"})
			return
		}
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
		name := memorySelectionName
		if scenario == "physical" {
			name = "device_set_volume"
		}
		rid := "r"
		if scenario == "identity" {
			rid = "old"
		}
		c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": rid, "call_id": "selection", "name": name, "arguments": args})
		call := map[string]any{"type": "function_call", "call_id": "selection", "name": name, "arguments": args}
		output := []any{call}
		if scenario == "duplicate" {
			output = append(output, call)
		}
		if scenario == "no_call" {
			output = []any{}
		}
		if scenario == "mismatch" {
			call["arguments"] = `{"intent":"none","indices":[]}`
		}
		if scenario == "incomplete" {
			return
		}
		status := "completed"
		if scenario == "failed" {
			status = "failed"
		}
		usage := map[string]any{"input_tokens": 12, "output_tokens": 4, "total_tokens": 16, "secret": "private-provider-body"}
		if scenario == "partial_usage" {
			usage = map[string]any{"input_tokens": 12, "output_tokens": -4, "total_tokens": "16", "private_detail": 999}
		}
		response := map[string]any{"id": "r", "status": status, "output": output, "usage": usage, "private_text": "private-provider-body"}
		if scenario == "missing_usage" {
			delete(response, "usage")
		}
		if scenario == "malformed_terminal" {
			response["output"] = "invalid"
		}
		if scenario == "terminal_identity" {
			response["id"] = "other"
		}
		c.WriteJSON(map[string]any{"type": "response.done", "response": response})
	}))
}
func selectionInput() MemorySelectionInput {
	return MemorySelectionInput{CurrentRequest: "上次讲到哪里", PlayedContext: `{"recent_turns":[]}`, Candidates: []MemorySelectionCandidate{{0, MemoryEvidence{SessionID: strings.Repeat("a", 32), TurnID: 1, At: time.Now().Add(-time.Hour), User: "以前的故事", Assistant: "小猫出门", Boundary: "complete_response", Source: "native_transcript_full_playback"}}}}
}
func TestMemorySelectionNativeContractAndUsage(t *testing.T) {
	provider := memorySelectionProvider(t, `{"intent":"continue","indices":[0]}`, "", func(input map[string]any) {
		if input["current_request"] != "上次讲到哪里" || input["played_context"] != `{"recent_turns":[]}` || len(input["candidates"].([]any)) != 1 {
			t.Error(input)
		}
	}, nil)
	defer provider.Close()
	dir := t.TempDir()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, dir)
	plan, err := n.SelectMemory(context.Background(), selectionInput())
	if err != nil || plan.Intent != "continue" || len(plan.Indices) != 1 {
		t.Fatal(plan, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	found := false
	for _, file := range files {
		raw, _ := os.ReadFile(file)
		if strings.Contains(string(raw), memorySelectionName) && strings.Contains(string(raw), `"total_tokens":16`) {
			found = true
		}
	}
	if !found {
		t.Fatal("missing Native planning usage", files)
	}
}
func TestMemorySelectionNativeRejectsBadToolTerminal(t *testing.T) {
	for _, scenario := range []string{"physical", "identity", "duplicate", "no_call", "mismatch", "incomplete", "failed", "provider_error"} {
		t.Run(scenario, func(t *testing.T) {
			provider := memorySelectionProvider(t, `{"intent":"recall","indices":[0]}`, scenario, nil, nil)
			defer provider.Close()
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, t.TempDir())
			if _, err := n.SelectMemory(context.Background(), selectionInput()); err == nil {
				t.Fatal("invalid terminal accepted")
			}
		})
	}
}
func selectionSession(t *testing.T) *Session {
	server := memoryServer(t, t.TempDir())
	old := memorySession(t, server, memoryDeviceA)
	rememberedTurn(old, "旧故事", "旧模板不得泄漏", true)
	rememberedTurn(old, "旧暗号", "必要旧暗号紫风铃", true)
	if err := old.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	s := memorySession(t, server, memoryDeviceA)
	rememberedTurn(s, "当前会话的问题", "当前会话已播上下文", true)
	return s
}
func TestMemorySelectionFinalReplyUsesOnlySelectedEvidence(t *testing.T) {
	for _, tc := range []struct {
		intent, indices string
		want            bool
	}{{"none", "[]", false}, {"recall", "[1]", true}, {"continue", "[1]", true}, {"recall", "[1,7]", false}} {
		t.Run(tc.intent+tc.indices, func(t *testing.T) {
			s := selectionSession(t)
			provider := memorySelectionProvider(t, `{"intent":"`+tc.intent+`","indices":`+tc.indices+`}`, "", nil, nil)
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
			s.generate = func(ctx context.Context, id int64, p string, pcm []byte, emit func(string, []byte) error) error {
				prompt = p
				return context.Canceled
			}
			rt := rememberedTurn(s, "当前需要的回答", "", false)
			s.reply(rt, make([]byte, 640))
			if prompt == "" || strings.Contains(prompt, "旧模板不得泄漏") || strings.Contains(prompt, "必要旧暗号紫风铃") != tc.want || !strings.Contains(prompt, "当前会话已播上下文") {
				t.Fatal("bad final prompt", prompt)
			}
		})
	}
}
func TestMemorySelectionProviderFailureKeepsCurrentContext(t *testing.T) {
	s := selectionSession(t)
	provider := memorySelectionProvider(t, "", "provider_error", nil, nil)
	defer provider.Close()
	s.cfg.URL = "ws" + strings.TrimPrefix(provider.URL, "http")
	rt := rememberedTurn(s, "讲个新故事", "", false)
	history, err := s.settle(rt.ctx, rt)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.routeMemory(rt, history)
	if err != nil || strings.Contains(got, "旧模板") || strings.Contains(got, "紫风铃") || !strings.Contains(got, "当前会话已播上下文") {
		t.Fatal(got, err)
	}
}
func TestMemorySelectionClearAndCancelInvalidateInFlight(t *testing.T) {
	for _, action := range []string{"clear", "failed_clear", "cancel"} {
		t.Run(action, func(t *testing.T) {
			s := selectionSession(t)
			started, release := make(chan struct{}), make(chan struct{})
			provider := memorySelectionProvider(t, `{"intent":"recall","indices":[0]}`, "", func(map[string]any) { close(started) }, release)
			defer provider.Close()
			s.cfg.URL = "ws" + strings.TrimPrefix(provider.URL, "http")
			rt := rememberedTurn(s, "回忆旧故事", "", false)
			history, err := s.settle(rt.ctx, rt)
			if err != nil {
				t.Fatal(err)
			}
			type result struct {
				history string
				err     error
			}
			done := make(chan result, 1)
			go func() { h, _, e := s.routeMemory(rt, history); done <- result{h, e} }()
			<-started
			if action == "clear" || action == "failed_clear" {
				if action == "failed_clear" {
					if err := s.memory.store.Close(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				clear := rememberedTurn(s, "清除聊天记忆", "", false)
				err := s.clearMemory(clear)
				if (err != nil) != (action == "failed_clear") {
					t.Fatal("unexpected clear outcome", err)
				}
			} else {
				rt.cancel()
			}
			close(release)
			got := <-done
			if strings.Contains(got.history, "旧模板") || strings.Contains(got.history, "当前会话已播上下文") && action != "cancel" || action == "cancel" && got.err == nil {
				t.Fatal("stale selection revived", got)
			}
		})
	}
}
func TestMemorySelectionSkipsAbsentCandidatesTranscriptAndClear(t *testing.T) {
	for _, scenario := range []string{"no_memory", "no_candidates", "missing_transcript", "clear", "same_session_only"} {
		t.Run(scenario, func(t *testing.T) {
			s := selectionSession(t)
			var calls atomic.Int32
			provider := memorySelectionProvider(t, `{"intent":"none","indices":[]}`, "", func(map[string]any) { calls.Add(1) }, nil)
			defer provider.Close()
			s.cfg.URL = "ws" + strings.TrimPrefix(provider.URL, "http")
			text := "你好"
			switch scenario {
			case "no_memory":
				s.memory = nil
			case "no_candidates":
				s.memory.base = nil
			case "missing_transcript":
				text = ""
			case "clear":
				text = "清除聊天记忆"
			case "same_session_only":
				for i := range s.memory.base {
					s.memory.base[i].SessionID = s.rec.owner.SessionID
				}
			}
			rt := rememberedTurn(s, text, "", false)
			h, _, err := s.routeMemory(rt, realtimestate.ContextFor(nil))
			if err != nil || calls.Load() != 0 || strings.Contains(h, "旧模板") {
				t.Fatal(h, err, calls.Load())
			}
		})
	}
}
func TestMemorySelectionSharedThreeSecondBudget(t *testing.T) {
	provider := memorySelectionProvider(t, "", "timeout", nil, nil)
	defer provider.Close()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, t.TempDir())
	started := time.Now()
	if _, err := n.SelectMemory(context.Background(), selectionInput()); err == nil {
		t.Fatal("timeout accepted")
	}
	if elapsed := time.Since(started); elapsed < 2800*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Fatal("not one shared 3s budget", elapsed)
	}
}

func TestMemorySelectionPreparedContextRevalidatesAfterClear(t *testing.T) {
	s := selectionSession(t)
	provider := memorySelectionProvider(t, `{"intent":"recall","indices":[1]}`, "", nil, nil)
	defer provider.Close()
	s.cfg.URL = "ws" + strings.TrimPrefix(provider.URL, "http")
	rt := rememberedTurn(s, "记得旧暗号吗", "", false)
	h, err := s.settle(rt.ctx, rt)
	if err != nil {
		t.Fatal(err)
	}
	selected, _, err := s.routeMemory(rt, h)
	if err != nil || !strings.Contains(selected, "紫风铃") {
		t.Fatal(selected, err)
	}
	if refreshed := s.selectedMemoryContext(selected); !strings.Contains(refreshed, "紫风铃") {
		t.Fatal("valid selection lost", refreshed)
	}
	clear := rememberedTurn(s, "清除聊天记忆", "", false)
	if err := s.clearMemory(clear); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := s.cfg.refreshMusicInput(selected, "")
	if strings.Contains(refreshed, "紫风铃") || strings.Contains(refreshed, "当前会话已播上下文") {
		t.Fatal("stale prepared selection survived", refreshed)
	}
}
func TestMemorySelectionProjectionBoundedAndDoesNotMutateSource(t *testing.T) {
	s := selectionSession(t)
	original := strings.Repeat("字", 512)
	s.memory.mu.Lock()
	s.memory.base = nil
	for i := range 24 {
		s.memory.base = append(s.memory.base, memoryTurn{SessionID: strings.Repeat("a", 32), TurnID: int64(i + 1), At: time.Now().Add(-time.Hour), User: original, Assistant: strings.Repeat("答", 1024), Source: "source", Boundary: "complete_response"})
	}
	before := s.memory.base[0]
	excerpts, recent := s.projectedMemoryLocked()
	s.memory.mu.Unlock()
	if len(excerpts) > 6 || len(recent) > 8 || len(excerpts)+len(recent) > 14 || s.memory.base[0] != before {
		t.Fatal("unbounded or mutated source", len(excerpts), len(recent))
	}
	total := 0
	for _, turn := range append(excerpts, recent...) {
		total += len([]rune(turn.User)) + len([]rune(turn.Assistant))
		if turn.Source != "source" || turn.At.IsZero() {
			t.Fatal("lost provenance", turn)
		}
	}
	if total > 7680 {
		t.Fatal("candidate budget exceeded", total)
	}
	if _, err := memorySelectionArguments(`{"intent":"recall","indices":[0,1,2]}`, 3); err != nil {
		t.Fatal("valid maximum rejected", err)
	}
}
func TestMemorySelectionInputValidationDoesNotOpenProvider(t *testing.T) {
	for _, kind := range []string{"empty", "history", "index", "count", "text_budget", "future", "oversized", "source"} {
		t.Run(kind, func(t *testing.T) {
			input := selectionInput()
			switch kind {
			case "empty":
				input.CurrentRequest = ""
			case "history":
				input.PlayedContext = "not JSON"
			case "index":
				input.Candidates[0].Index = 7
			case "count":
				input.Candidates = nil
			case "text_budget":
				input.CurrentRequest = strings.Repeat("x", 16385)
			case "future":
				input.Candidates[0].Evidence.At = time.Now().Add(time.Hour)
			case "oversized":
				input.Candidates[0].Evidence.Assistant = strings.Repeat("字", 1025)
			case "source":
				input.Candidates[0].Evidence.Source = strings.Repeat("x", 257)
			}
			n := NewNative(Config{URL: "ws://127.0.0.1:1"}, t.TempDir())
			if _, err := n.SelectMemory(context.Background(), input); err == nil || n.audit != nil {
				t.Fatal("invalid input opened provider", err)
			}
		})
	}
}

func memorySelectionIntent(t *testing.T, dir string) map[string]any {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "memory_selection_request-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("missing selection-owned request audit", paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-provider-body") || strings.Contains(string(raw), "private_detail") {
		t.Fatal("uncontrolled provider fields in audit", string(raw))
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data
}
func TestMemorySelectionHasIndependentRequestAudit(t *testing.T) {
	for _, scenario := range []string{"complete", "missing_usage", "partial_usage", "malformed_terminal", "failed", "terminal_identity", "provider_error", "bad_plan"} {
		t.Run(scenario, func(t *testing.T) {
			args := `{"intent":"recall","indices":[0]}`
			if scenario == "bad_plan" {
				args = `{"intent":"recall","indices":[9]}`
			}
			provider := memorySelectionProvider(t, args, scenario, nil, nil)
			defer provider.Close()
			dir := t.TempDir()
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http"), Model: "selection-audit-model", Synthetic: true}, dir)
			_, err := n.SelectMemory(context.Background(), selectionInput())
			data := memorySelectionIntent(t, dir)
			submitted, ok := data["submitted_at"].(string)
			if data["request_count"] != float64(1) || !ok || submitted == "" || data["model"] != "selection-audit-model" {
				t.Fatal("missing pre-request facts", data)
			}
			expectedState := "completed"
			if scenario == "malformed_terminal" || scenario == "failed" || scenario == "terminal_identity" || scenario == "provider_error" || scenario == "bad_plan" {
				expectedState = "failed"
			}
			if (err != nil) != (expectedState == "failed") || data["state"] != expectedState {
				t.Fatal("incorrect final request state", data, err)
			}
			expectedUsage := "complete"
			if scenario == "missing_usage" || scenario == "partial_usage" || scenario == "terminal_identity" || scenario == "provider_error" {
				expectedUsage = "unknown"
			}
			if data["usage_state"] != expectedUsage {
				t.Fatal("incorrect usage confidence", data)
			}
			usage, _ := data["usage"].(map[string]any)
			if expectedUsage == "complete" && (usage["input_tokens"] != float64(12) || usage["total_tokens"] != float64(16)) {
				t.Fatal("matching terminal usage lost", data)
			}
			if scenario == "partial_usage" && (len(usage) != 1 || usage["input_tokens"] != float64(12)) {
				t.Fatal("partial numeric evidence not preserved", data)
			}
			if elapsed, ok := data["total_ms"].(float64); !ok || elapsed < 0 {
				t.Fatal("missing elapsed", data)
			}
		})
	}
}
func TestMemorySelectionAuditCancellationRetainsUnknownSubmittedRequest(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	provider := memorySelectionProvider(t, `{"intent":"none","indices":[]}`, "", func(map[string]any) { close(started) }, release)
	defer provider.Close()
	dir := t.TempDir()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := n.SelectMemory(ctx, selectionInput()); done <- err }()
	<-started
	deadline := time.Now().Add(time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(dir, "memory_selection_request-*.json"))
		var data map[string]any
		if len(files) == 1 {
			raw, _ := os.ReadFile(files[0])
			json.Unmarshal(raw, &data)
		}
		if data["request_count"] == float64(1) {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			close(release)
			<-done
			t.Fatal("request audit missing before terminal")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	err := <-done
	close(release)
	if err == nil {
		t.Fatal("canceled request accepted")
	}
	data := memorySelectionIntent(t, dir)
	if data["state"] != "failed" || data["request_count"] != float64(1) || data["usage_state"] != "unknown" || data["outcome"] != "canceled" {
		t.Fatal("cancellation rewrote unknown usage", data)
	}
	n.Close()
	after := memorySelectionIntent(t, dir)
	if data["total_ms"] != after["total_ms"] || data["state"] != after["state"] {
		t.Fatal("later Close changed selection-owned audit", data, after)
	}
}

func TestMemorySelectionTerminalHookPreservesCallerAndGenericAudit(t *testing.T) {
	provider := memorySelectionProvider(t, `{"intent":"none","indices":[]}`, "", nil, nil)
	defer provider.Close()
	dir := t.TempDir()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, dir)
	called := 0
	n.OnToolTerminal = func(response map[string]any) error {
		called++
		if response["id"] != "r" {
			t.Error("hook received wrong identity", response)
		}
		return nil
	}
	if _, err := n.SelectMemory(context.Background(), selectionInput()); err != nil {
		t.Fatal(err)
	}
	if called != 1 || n.OnToolTerminal == nil {
		t.Fatal("existing observer lost", called)
	}
	own, generic := memorySelectionIntent(t, dir), planningIntent(t, dir)
	if own["usage_state"] != "complete" || generic["usage_state"] != "complete" || generic["request_count"] != float64(1) {
		t.Fatal("audit regressed", own, generic)
	}
}
