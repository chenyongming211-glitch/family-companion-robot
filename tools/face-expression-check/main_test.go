package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"familyrobot/internal/realtimelive"
)

const testFixture = "../../docs/verification/fixtures/2026-11-08-expression-scenes.json"

func fixtureForTest(t *testing.T) fixture {
	t.Helper()
	f, _, err := loadFixture(testFixture)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func runArgs(t *testing.T) (string, []string) {
	t.Helper()
	parent := t.TempDir()
	config := filepath.Join(parent, "protected.env")
	if err := os.WriteFile(config, []byte("never read by test loader"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(parent, "run")
	return out, []string{"--fixture", testFixture, "--run", "--config", config, "--out", out, "--source-sha", strings.Repeat("a", 40), "--version", "tool-v1", "--run-label", "fake-only"}
}

func fakeDependencies(t *testing.T, selectFn func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult) dependencies {
	t.Helper()
	return dependencies{
		loadConfig:       func(string) (realtimelive.Config, error) { return realtimelive.Config{}, nil },
		selectExpression: selectFn,
	}
}

func TestDryRunNeverLoadsConfigSelectsOrWrites(t *testing.T) {
	out := filepath.Join(t.TempDir(), "must-not-exist")
	panicDeps := dependencies{
		loadConfig: func(string) (realtimelive.Config, error) { panic("protected config accessed") },
		selectExpression: func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult {
			panic("inference attempted")
		},
	}
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"--fixture", testFixture, "--config", "/protected/never-read", "--out", out}, &stdout, &stderr, panicDeps)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "20") || !strings.Contains(stdout.String(), "fixture_sha256") || !strings.Contains(stdout.String(), "not_measured") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote output: %v", err)
	}
}

func TestFixtureRejectsProtocolDriftBeforeInference(t *testing.T) {
	raw, err := os.ReadFile(testFixture)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(map[string]any){
		"version":  func(v map[string]any) { v["schema_version"] = 2 },
		"protocol": func(v map[string]any) { v["protocol"] = "expression-v2" },
		"timeout":  func(v map[string]any) { v["selector_timeout_ms"] = 2001 },
		"requests": func(v map[string]any) { v["max_generation_requests_per_case"] = 2 },
		"count":    func(v map[string]any) { v["cases"] = v["cases"].([]any)[:19] },
		"id":       func(v map[string]any) { v["cases"].([]any)[1].(map[string]any)["id"] = "E01" },
		"emotion": func(v map[string]any) {
			v["cases"].([]any)[0].(map[string]any)["acceptable_emotions"] = []string{"joy"}
		},
		"empty_set":   func(v map[string]any) { v["cases"].([]any)[0].(map[string]any)["acceptable_emotions"] = []string{} },
		"name":        func(v map[string]any) { v["robot_name"] = "bad\nname" },
		"faults":      func(v map[string]any) { v["fault_cases"] = []string{"timeout"} },
		"voice_order": func(v map[string]any) { v["voice_comparison_order"] = []string{"on", "off", "on", "off"} },
		"C3_scope":    func(v map[string]any) { v["firmware_target"] = "ESP32-C3" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "fixture.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadFixture(path); err == nil {
				t.Fatal("accepted changed protocol")
			}
		})
	}
}

func TestInputPreservesE19AlreadyPlayedTextAndEscapesName(t *testing.T) {
	f := fixtureForTest(t)
	f.RobotName = `豆"豆\\` // Valid profile data must remain data after JSON encoding.
	input := expressionInput(f, f.Cases[18])
	if input.CurrentTranscript != f.Cases[18].Request || input.DeviceName != f.RobotName {
		t.Fatal(input)
	}
	var history struct {
		RecentTurns []struct {
			User      string `json:"user"`
			Assistant string `json:"assistant_spoken_prefix"`
		} `json:"recent_turns"`
	}
	if err := json.Unmarshal([]byte(input.PlayedContext), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.RecentTurns) != 1 || history.RecentTurns[0].User != "在家可以怎样简单运动？" || history.RecentTurns[0].Assistant != "可以先在屋里散步，再做轻柔拉伸。" {
		t.Fatalf("lost played text: %s", input.PlayedContext)
	}
	if strings.Contains(input.PlayedContext, "assistant_played") {
		t.Fatal("fixture alias leaked into production schema")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded realtimelive.ExpressionInput
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.DeviceName != f.RobotName {
		t.Fatalf("name not safely encoded: %s", encoded)
	}
	if got := expressionInput(f, f.Cases[0]).PlayedContext; got != `{"recent_turns":[]}` {
		t.Fatal(got)
	}
}

func TestTwentyCasesUseOneSequentialProductionBoundaryAndPrivateProvenance(t *testing.T) {
	f := fixtureForTest(t)
	out, args := runArgs(t)
	calls := 0
	deps := fakeDependencies(t, func(ctx context.Context, cfg realtimelive.Config, dir string, input realtimelive.ExpressionInput) realtimelive.ExpressionResult {
		meta := readJSON[runMetadata](t, filepath.Join(out, "metadata.json"))
		if meta.FixtureSHA256 == "" || meta.SourceSHA != strings.Repeat("a", 40) || meta.Version != "tool-v1" || meta.RunLabel != "fake-only" {
			t.Fatal(meta)
		}
		if _, err := os.Stat(filepath.Join(out, "fixture.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out, fmt.Sprintf("summary-%03d.json", calls))); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out, f.Cases[calls].ID+"-attempt.json")); err != nil {
			t.Fatal(err)
		}
		if input.CurrentTranscript != f.Cases[calls].Request || input.DeviceName != "豆豆" || !cfg.Synthetic || ctx.Err() != nil {
			t.Fatal(input)
		}
		if !strings.HasPrefix(dir, out+string(os.PathSeparator)) {
			t.Fatalf("selector records escaped: %s", dir)
		}
		emotion := f.Cases[calls].AcceptableEmotions[0]
		calls++
		return realtimelive.ExpressionResult{Emotion: emotion, Reason: "completed", TotalMS: 12, ConnectMS: 3, RequestCount: 1}
	})
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), args, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d %s", code, &stderr)
	}
	got := readJSON[summary](t, filepath.Join(out, "summary.json"))
	if calls != 20 || got.Attempted != 20 || got.Passed != 20 || got.Failed != 0 || got.NotRun != 0 || got.RequestCount != 20 || len(got.UsageTotals) != 0 || got.UsageObservedCases != 0 {
		t.Fatalf("calls=%d summary=%+v", calls, got)
	}
	if got.PlaybackWindow != "not_measured" || got.PhysicalStatus != "not_measured" || got.VoiceComparison != "not_executed" {
		t.Fatal(got)
	}
	if got.Cases[14].Result.Emotion != "neutral" || !got.Cases[14].Passed {
		t.Fatal("actual completed neutral was rejected")
	}
	if err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			return fmt.Errorf("%s mode=%o", path, info.Mode().Perm())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFailuresAreRetainedAndNeverRetriedOrCountedAsNeutralSuccess(t *testing.T) {
	f := fixtureForTest(t)
	out, args := runArgs(t)
	calls := 0
	deps := fakeDependencies(t, func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult {
		i := calls
		calls++
		r := realtimelive.ExpressionResult{Emotion: f.Cases[i].AcceptableEmotions[0], Reason: "completed", TotalMS: 50, ConnectMS: 5, RequestCount: 1}
		switch i {
		case 0:
			r.Emotion = "unknown"
		case 1:
			r.Emotion = "angry"
		case 2:
			r.RequestCount = 2
		case 3:
			r.Reason, r.Emotion, r.TotalMS = "timeout", "neutral", 2000
		case 14:
			r.Reason, r.Emotion = "provider_failed", "neutral"
		case 17:
			r.TotalMS = 2000
		case 18:
			r.RequestCount = 0
		}
		if i == 5 {
			r.Usage = map[string]int64{"input_tokens": 9, "output_tokens": 2, "total_tokens": 11}
		}
		if i == 6 {
			r.Usage = map[string]int64{"input_tokens": 4}
		}
		return r
	})
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), args, &stdout, &stderr, deps); code == 0 {
		t.Fatal("failures returned success")
	}
	got := readJSON[summary](t, filepath.Join(out, "summary.json"))
	if calls != 20 || got.Attempted != 20 || got.Failed != 7 || got.Passed != 13 || got.NotRun != 0 || got.MaxRequestViolations != 1 || len(got.Cases) != 20 {
		t.Fatalf("calls=%d %+v", calls, got)
	}
	if got.Cases[14].Passed || !got.Cases[14].Acceptable || got.Cases[14].Result.Reason != "provider_failed" {
		t.Fatal("fallback neutral counted as success")
	}
	if got.UsageTotals["input_tokens"] != 13 || got.UsageTotals["output_tokens"] != 2 || got.UsageObservedCases != 2 || got.UsageMetricCases["input_tokens"] != 2 || got.UsageMetricCases["output_tokens"] != 1 {
		t.Fatal(got)
	}
	if got.TotalLatency == nil || got.TotalLatency.Samples != 20 || got.TotalLatency.Min != 50 || got.TotalLatency.Max != 2000 || got.TotalLatency.Median != 50 || got.TotalLatency.P95 != 2000 || got.TotalLatency.Mean != 245 {
		t.Fatalf("incorrect latency summary: %+v", got.TotalLatency)
	}
	for _, c := range got.Cases {
		saved := readJSON[caseRecord](t, filepath.Join(out, c.ID+".json"))
		if saved.ID != c.ID || saved.Passed != c.Passed || saved.Result.Reason != c.Result.Reason {
			t.Fatal("summary lost case")
		}
	}
}

func TestCancelledRunRetainsCurrentAttemptAndRemainingCases(t *testing.T) {
	out, args := runArgs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := fixtureForTest(t)
	calls := 0
	deps := fakeDependencies(t, func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult {
		i := calls
		calls++
		if calls == 3 {
			cancel()
			return realtimelive.ExpressionResult{Emotion: "neutral", Reason: "cancelled", TotalMS: 1, RequestCount: 1}
		}
		return realtimelive.ExpressionResult{Emotion: f.Cases[i].AcceptableEmotions[0], Reason: "completed", TotalMS: 1, RequestCount: 1}
	})
	var stdout, stderr bytes.Buffer
	if code := runCLI(ctx, args, &stdout, &stderr, deps); code == 0 {
		t.Fatal("cancel returned success")
	}
	got := readJSON[summary](t, filepath.Join(out, "summary.json"))
	if calls != 3 || got.Attempted != 3 || got.NotRun != 17 || got.Passed != 2 || got.Failed != 1 || got.Status != "cancelled" || len(got.Cases) != 20 {
		t.Fatal(got)
	}
	if got.Cases[3].Status != "not_run" || got.Cases[3].NotRunReason != "cancelled" {
		t.Fatal(got.Cases[3])
	}
	if _, err := os.Stat(filepath.Join(out, "E03.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "summary-003.json")); err != nil {
		t.Fatal(err)
	}
}

func TestUnsafeArgumentsAndExistingOutputsNeverCallProtectedLoader(t *testing.T) {
	parent := t.TempDir()
	config := filepath.Join(parent, "config")
	if err := os.WriteFile(config, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(parent, "existing")
	if err := os.Mkdir(existing, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "failure.json")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(existing, alias); err != nil {
		t.Fatal(err)
	}
	for name, extra := range map[string][]string{
		"existing":       {"--out", existing},
		"symlink_root":   {"--out", alias},
		"symlink_parent": {"--out", filepath.Join(alias, "new")},
		"relative":       {"--out", "relative"},
		"unclean":        {"--out", parent + "/x/../new"},
		"source":         {"--out", filepath.Join(parent, "new"), "--source-sha", "main"},
		"missing_out":    {},
		"label":          {"--out", filepath.Join(parent, "new"), "--run-label", "../old"},
	} {
		t.Run(name, func(t *testing.T) {
			deps := dependencies{loadConfig: func(string) (realtimelive.Config, error) {
				t.Fatal("unsafe run loaded protected config")
				return realtimelive.Config{}, nil
			}}
			args := append([]string{"--fixture", testFixture, "--run", "--config", config}, extra...)
			var stdout, stderr bytes.Buffer
			if code := runCLI(context.Background(), args, &stdout, &stderr, deps); code == 0 {
				t.Fatal("unsafe run succeeded")
			}
		})
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatal("overwrote previous failure")
	}
}

func TestProtectedConfigFailureHasProvenanceButNeverLeaksError(t *testing.T) {
	out, args := runArgs(t)
	deps := dependencies{loadConfig: func(string) (realtimelive.Config, error) {
		return realtimelive.Config{}, errors.New("secret-key wss://host/?secret=hidden")
	}, selectExpression: func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult {
		panic("selector after config failure")
	}}
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), args, &stdout, &stderr, deps); code == 0 {
		t.Fatal("bad config succeeded")
	}
	got := readJSON[summary](t, filepath.Join(out, "summary.json"))
	if got.Attempted != 0 || got.NotRun != 20 || got.Status != "config_failed" {
		t.Fatal(got)
	}
	if strings.Contains(stdout.String()+stderr.String(), "secret") || strings.Contains(stdout.String()+stderr.String(), "wss://") {
		t.Fatal("leaked config error")
	}
}

func TestLastCaseOutputFailureCannotReportSuccessfulExit(t *testing.T) {
	f := fixtureForTest(t)
	out, args := runArgs(t)
	calls := 0
	deps := fakeDependencies(t, func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult {
		i := calls
		calls++
		if calls == 20 {
			// A failed result write must not overwrite an existing record or
			// become exit 0 merely because the in-memory selections all passed.
			if err := os.WriteFile(filepath.Join(out, "E20.json"), []byte("keep collision evidence"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return realtimelive.ExpressionResult{Emotion: f.Cases[i].AcceptableEmotions[0], Reason: "completed", TotalMS: 1, RequestCount: 1}
	})
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), args, &stdout, &stderr, deps); code == 0 {
		t.Fatal("failed output reported success")
	}
	got := readJSON[summary](t, filepath.Join(out, "summary.json"))
	if got.Status != "output_failed" || got.Attempted != 20 || got.Passed != 20 {
		t.Fatal(got)
	}
	if raw, err := os.ReadFile(filepath.Join(out, "E20.json")); err != nil || string(raw) != "keep collision evidence" {
		t.Fatal("collision was overwritten")
	}
}

func TestAlreadyCancelledRunKeepsProvenanceWithoutConfigOrSelector(t *testing.T) {
	out, args := runArgs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := dependencies{
		loadConfig: func(string) (realtimelive.Config, error) { panic("config loaded after cancellation") },
		selectExpression: func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult {
			panic("selected after cancellation")
		},
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI(ctx, args, &stdout, &stderr, deps); code == 0 {
		t.Fatal("cancel reported success")
	}
	got := readJSON[summary](t, filepath.Join(out, "summary.json"))
	if got.Status != "cancelled" || got.Attempted != 0 || got.NotRun != 20 {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(out, "metadata.json")); err != nil {
		t.Fatal(err)
	}
}
