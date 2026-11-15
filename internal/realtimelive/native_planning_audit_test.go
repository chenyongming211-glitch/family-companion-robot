package realtimelive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planningIntent(t *testing.T, dir string) map[string]any {
	t.Helper()
	paths, e := filepath.Glob(filepath.Join(dir, "native_session-*.json"))
	if e != nil || len(paths) != 1 {
		t.Fatal(paths, e)
	}
	b, e := os.ReadFile(paths[0])
	if e != nil {
		t.Fatal(e)
	}
	var data map[string]any
	if e = json.Unmarshal(b, &data); e != nil {
		t.Fatal(e)
	}
	return data
}
func TestPlanningAuditCountsRequestBeforeUnknownOrNumericUsage(t *testing.T) {
	for _, kind := range []string{"complete", "partial", "missing", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			g, url := parallelProvider(t, "none", `{"intent":"none","target":"current"}`)
			if kind == "complete" {
				g.usage = map[string]any{"input_tokens": 10, "output_tokens": 4, "total_tokens": 14, "cached_tokens": 2, "secret": "excluded"}
			}
			if kind == "partial" {
				g.usage = map[string]any{"input_tokens": -1, "output_tokens": 4, "total_tokens": "14", "private_detail": 123}
			}
			dir := t.TempDir()
			n := NewNative(Config{URL: url, Model: "audit-model", Synthetic: true}, dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				n.RunControlPlan(ctx, ControlInput{CurrentRequest: "你好", PlayedContext: `{"recent_turns":[]}`, Turn: 1}, plannerTools(), nil)
			}()
			awaitPlanning(t, g.controlStarted, "request missing")
			before := planningIntent(t, dir)
			if before["request_count"] != float64(1) || before["usage_state"] != "unknown" || before["model"] != "audit-model" {
				t.Fatal("missing durable pre-request accounting", before)
			}
			names, _ := before["tool_names"].([]any)
			if len(names) != 1 || names[0] != controlPlanName {
				t.Fatal("planner source not identified", before)
			}
			if _, ok := before["connect_ms"].(float64); !ok {
				t.Fatal("connect duration missing", before)
			}
			if kind == "canceled" {
				cancel()
			} else {
				releaseGate(g.allowControl)
			}
			awaitPlanning(t, done, "planner did not finish")
			after := planningIntent(t, dir)
			if elapsed, ok := after["planning_ms"].(float64); !ok || elapsed < 0 {
				t.Fatal("planner exit omitted final elapsed time", after)
			}
			outcome := "response_completed"
			if kind == "canceled" {
				outcome = "canceled"
			}
			if after["planning_outcome"] != outcome {
				t.Fatal("planner exit omitted fixed outcome", after)
			}
			cancel()
			n.Close()
			n.Close()
			closed := planningIntent(t, dir)
			if closed["planning_ms"] != after["planning_ms"] || closed["planning_outcome"] != after["planning_outcome"] {
				t.Fatal("later close overwrote final measurement", closed)
			}
			want := "unknown"
			if kind == "complete" {
				want = "complete"
			}
			if after["request_count"] != float64(1) || after["usage_state"] != want {
				t.Fatal(after)
			}
			if kind != "canceled" && after["response_id"] != "r" {
				t.Fatal("matching response identity missing", after)
			}
			usage := object(after["usage"])
			if usage["secret"] != nil || usage["private_detail"] != nil {
				t.Fatal("unsafe usage payload", usage)
			}
			if kind == "complete" && usage["input_tokens"] != float64(10) {
				t.Fatal(usage)
			}
			if kind == "partial" && (usage["input_tokens"] != nil || usage["total_tokens"] != nil || usage["output_tokens"] != float64(4)) {
				t.Fatal("invalid usage accepted", usage)
			}
		})
	}
}

func TestPlanningAuditRejectedProviderOutputRetainsUnknownCost(t *testing.T) {
	for _, scenario := range []string{"failed", "identity"} {
		t.Run(scenario, func(t *testing.T) {
			h := semanticProvider(t, `{"intent":"none","target":"current"}`, scenario, nil)
			defer h.Close()
			dir := t.TempDir()
			n := NewNative(Config{URL: "ws" + strings.TrimPrefix(h.URL, "http"), Synthetic: true}, dir)
			if _, e := n.RunControlPlan(context.Background(), ControlInput{CurrentRequest: "你好", PlayedContext: `{"recent_turns":[]}`, Turn: 1}, plannerTools(), nil); e == nil {
				t.Fatal("bad provider output accepted")
			}
			data := planningIntent(t, dir)
			if data["request_count"] != float64(1) || data["usage_state"] != "unknown" {
				t.Fatal("failure counted as free", data)
			}
			if data["planning_outcome"] != "failed" {
				t.Fatal("failed planning omitted fixed outcome", data)
			}
			if _, ok := data["planning_ms"].(float64); !ok {
				t.Fatal("failed planning omitted elapsed time", data)
			}
			if scenario == "identity" && data["response_id"] != "" {
				t.Fatal("wrong response counted", data)
			}
		})
	}
}

func TestPlanningAuditNewConnectionResetsRequestCount(t *testing.T) {
	g, url := parallelProvider(t, "none", `{"intent":"none","target":"current"}`)
	releaseGate(g.allowControl)
	dir := t.TempDir()
	n := NewNative(Config{URL: url, Synthetic: true}, dir)
	for range 2 {
		if _, e := n.RunControlPlan(context.Background(), ControlInput{CurrentRequest: "你好", PlayedContext: `{"recent_turns":[]}`, Turn: 1}, plannerTools(), nil); e != nil {
			t.Fatal(e)
		}
	}
	paths, e := filepath.Glob(filepath.Join(dir, "native_session-*.json"))
	if e != nil || len(paths) != 2 {
		t.Fatal(paths, e)
	}
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var data map[string]any
		if e = json.Unmarshal(b, &data); e != nil {
			t.Fatal(e)
		}
		if data["request_count"] != float64(1) {
			t.Fatal("connection inherited previous count", data)
		}
	}
}

func TestPlanningAuditFailedRequestWriteRetainsUnknownCost(t *testing.T) {
	dir := t.TempDir()
	server, _ := testSocket(t)
	audit, e := newIntent(dir, "native_session", true)
	if e != nil {
		t.Fatal(e)
	}
	n := NewNative(Config{Tools: []any{controlPlanTool()}, Synthetic: true}, dir)
	n.conn = server
	n.audit = audit
	server.Close()
	if e := n.write(map[string]any{"type": "response.create"}); e == nil {
		t.Fatal("closed socket accepted request")
	}
	data := planningIntent(t, dir)
	if data["request_count"] != float64(1) || data["usage_state"] != "unknown" || data["planning_outcome"] != "request_write_failed" {
		t.Fatal(data)
	}
	if _, ok := data["planning_ms"].(float64); !ok {
		t.Fatal("failed request write omitted elapsed time", data)
	}
}

func TestPlanningAuditMusicHandledPersistsCanceledPrefetch(t *testing.T) {
	g, url := parallelProvider(t, "stop", `{"intent":"none","target":"current"}`)
	s, _ := parallelSession(t, url, true)
	rt := musicRequest(s, "停止音乐")
	_, done := replyForPlanning(t, s, rt)
	awaitPlanning(t, g.musicStarted, "music request missing")
	awaitPlanning(t, g.controlStarted, "prefetch request missing")
	releaseGate(g.allowMusic)
	awaitPlanning(t, done, "handled music did not join canceled planning")
	paths, e := filepath.Glob(filepath.Join(s.directory, "native_session-*.json"))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var data map[string]any
		if e = json.Unmarshal(b, &data); e != nil {
			t.Fatal(e)
		}
		names, _ := data["tool_names"].([]any)
		if len(names) != 1 || names[0] != controlPlanName {
			continue
		}
		found = true
		if data["request_count"] != float64(1) || data["usage_state"] != "unknown" || data["planning_outcome"] != "canceled" {
			t.Fatal("handled music lost canceled request evidence", data)
		}
		if _, ok := data["planning_ms"].(float64); !ok {
			t.Fatal("handled music lost planning duration", data)
		}
	}
	if !found {
		t.Fatal("prefetch intent missing")
	}
}
