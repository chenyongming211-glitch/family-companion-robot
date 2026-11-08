// face-expression-check validates the frozen synthetic scene protocol and,
// only with --run, evaluates the production silent expression selector.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"familyrobot/internal/deviceprofile"
	"familyrobot/internal/realtimelive"
)

const defaultFixture = "docs/verification/fixtures/2026-11-08-expression-scenes.json"

// These are protocol validation values, not another expression selector.
var canonicalEmotions = []string{"neutral", "happy", "loving", "embarrassed", "confident", "delicious", "sad", "crying", "sleepy", "silly", "angry", "surprised", "winking", "relaxed", "confused"}
var expectedFaults = []string{"unknown_emotion", "missing_tool", "conflicting_tools", "provider_error", "timeout", "cancel_before_begin", "cancel_after_begin", "late_after_playback_window", "result_after_playback_finished", "stale_connection", "new_reply_during_old_drain"}
var sourcePattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
var labelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type fixture struct {
	SchemaVersion     int      `json:"schema_version"`
	Protocol          string   `json:"protocol"`
	Registration      string   `json:"registration"`
	RobotName         string   `json:"robot_name"`
	SelectorTimeoutMS int64    `json:"selector_timeout_ms"`
	MaxRequests       int      `json:"max_generation_requests_per_case"`
	NormalCaseRule    string   `json:"normal_case_rule"`
	Cases             []scene  `json:"cases"`
	FaultCases        []string `json:"fault_cases"`
	VoiceCaseIDs      []string `json:"voice_comparison_case_ids"`
	VoiceOrder        []string `json:"voice_comparison_order"`
	MeasurementNotes  string   `json:"measurement_notes"`
}

type scene struct {
	ID                 string         `json:"id"`
	Request            string         `json:"request"`
	PlayedContext      *playedHistory `json:"played_context,omitempty"`
	AcceptableEmotions []string       `json:"acceptable_emotions"`
}

type playedHistory struct {
	RecentTurns []playedTurn `json:"recent_turns"`
}
type playedTurn struct {
	User            string `json:"user"`
	AssistantPlayed string `json:"assistant_played"`
}

type dependencies struct {
	loadConfig       func(string) (realtimelive.Config, error)
	selectExpression func(context.Context, realtimelive.Config, string, realtimelive.ExpressionInput) realtimelive.ExpressionResult
}

type runMetadata struct {
	SchemaVersion     int      `json:"schema_version"`
	Protocol          string   `json:"protocol"`
	FixtureSHA256     string   `json:"fixture_sha256"`
	SourceSHA         string   `json:"source_sha,omitempty"`
	SourceStatus      string   `json:"source_status"`
	Version           string   `json:"version"`
	RunLabel          string   `json:"run_label,omitempty"`
	StartedAt         string   `json:"started_at"`
	RobotName         string   `json:"robot_name"`
	SelectorTimeoutMS int64    `json:"selector_timeout_ms"`
	MaxRequests       int      `json:"max_generation_requests_per_case"`
	NormalCases       int      `json:"normal_cases"`
	FaultCases        []string `json:"fault_cases_not_executed"`
	VoiceCaseIDs      []string `json:"voice_comparison_case_ids_not_executed"`
	VoiceOrder        []string `json:"voice_comparison_order_not_executed"`
}

type caseRecord struct {
	ID                 string                         `json:"id"`
	Status             string                         `json:"status"`
	NotRunReason       string                         `json:"not_run_reason,omitempty"`
	AcceptableEmotions []string                       `json:"acceptable_emotions"`
	Result             *realtimelive.ExpressionResult `json:"result,omitempty"`
	SelectorCompleted  bool                           `json:"selector_completed"`
	Canonical          bool                           `json:"canonical"`
	Acceptable         bool                           `json:"acceptable"`
	TimingValid        bool                           `json:"timing_valid"`
	WithinDeadline     bool                           `json:"within_deadline"`
	ObservedTotalMS    int64                          `json:"observed_total_ms"`
	Passed             bool                           `json:"passed"`
	Failures           []string                       `json:"failures,omitempty"`
}

type summary struct {
	Metadata             runMetadata      `json:"metadata"`
	Status               string           `json:"status"`
	RequiredCases        int              `json:"required_cases"`
	Attempted            int              `json:"attempted"`
	Passed               int              `json:"passed"`
	Failed               int              `json:"failed"`
	NotRun               int              `json:"not_run"`
	RequestCount         int              `json:"request_count"`
	MaxRequestViolations int              `json:"max_request_violations"`
	ReasonCounts         map[string]int   `json:"reason_counts"`
	DefaultReasons       map[string]int   `json:"default_reasons"`
	FailureCounts        map[string]int   `json:"failure_counts"`
	UsageTotals          map[string]int64 `json:"usage_totals,omitempty"`
	UsageObservedCases   int              `json:"usage_observed_cases"`
	UsageMetricCases     map[string]int   `json:"usage_metric_cases,omitempty"`
	ConnectLatency       *latencySummary  `json:"connect_latency_ms,omitempty"`
	TotalLatency         *latencySummary  `json:"total_latency_ms,omitempty"`
	PlaybackWindow       string           `json:"playback_window"`
	PhysicalStatus       string           `json:"physical_status"`
	VoiceComparison      string           `json:"voice_comparison"`
	FaultCases           string           `json:"fault_cases"`
	Cases                []caseRecord     `json:"cases"`
}

type latencySummary struct {
	Samples int     `json:"samples"`
	Min     int64   `json:"min"`
	Median  float64 `json:"median"`
	P95     int64   `json:"p95"`
	Max     int64   `json:"max"`
	Mean    float64 `json:"mean"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(runCLI(ctx, os.Args[1:], os.Stdout, os.Stderr, dependencies{
		loadConfig:       realtimelive.LoadConfig,
		selectExpression: realtimelive.SelectExpression,
	}))
}

func loadFixture(path string) (fixture, []byte, error) {
	var f fixture
	file, err := os.Open(path)
	if err != nil {
		return f, nil, errors.New("fixture_unreadable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || !utf8.Valid(raw) {
		return f, nil, errors.New("fixture_invalid")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return f, nil, errors.New("fixture_invalid")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return f, nil, errors.New("fixture_invalid")
	}
	if f.SchemaVersion != 1 || f.Protocol != "expression-v1.0" || f.SelectorTimeoutMS != realtimelive.ExpressionSelectionBudget.Milliseconds() || f.MaxRequests != 1 || len(f.Cases) != 20 || f.Registration != "before first expression model evaluation" || f.NormalCaseRule == "" || f.MeasurementNotes == "" {
		return f, nil, errors.New("fixture_protocol_mismatch")
	}
	f.RobotName, err = deviceprofile.NormalizeName(f.RobotName)
	if err != nil {
		return f, nil, errors.New("fixture_name_invalid")
	}
	if !slices.Equal(f.FaultCases, expectedFaults) || !slices.Equal(f.VoiceCaseIDs, []string{"E01", "E07", "E15", "E17"}) || !slices.Equal(f.VoiceOrder, []string{"off", "on", "on", "off"}) {
		return f, nil, errors.New("fixture_metadata_mismatch")
	}
	for i, c := range f.Cases {
		if c.ID != fmt.Sprintf("E%02d", i+1) || strings.TrimSpace(c.Request) == "" || len(c.Request) > 16384 || len(c.AcceptableEmotions) == 0 {
			return f, nil, errors.New("fixture_case_invalid")
		}
		seen := map[string]bool{}
		for _, emotion := range c.AcceptableEmotions {
			if !slices.Contains(canonicalEmotions, emotion) || seen[emotion] {
				return f, nil, errors.New("fixture_emotion_invalid")
			}
			seen[emotion] = true
		}
		if c.PlayedContext != nil {
			if c.PlayedContext.RecentTurns == nil || len(c.PlayedContext.RecentTurns) > 16 {
				return f, nil, errors.New("fixture_history_invalid")
			}
			for _, turn := range c.PlayedContext.RecentTurns {
				if strings.TrimSpace(turn.User) == "" || strings.TrimSpace(turn.AssistantPlayed) == "" || utf8.RuneCountInString(turn.User) > 1024 || utf8.RuneCountInString(turn.AssistantPlayed) > 2048 {
					return f, nil, errors.New("fixture_history_invalid")
				}
			}
		}
	}
	return f, raw, nil
}

func expressionInput(f fixture, c scene) realtimelive.ExpressionInput {
	// The frozen fixture calls this assistant_played; the production projection
	// requires assistant_spoken_prefix. Preserve the exact evidenced text.
	type turn struct {
		User      string `json:"user"`
		Assistant string `json:"assistant_spoken_prefix"`
	}
	history := struct {
		RecentTurns []turn `json:"recent_turns"`
	}{RecentTurns: []turn{}}
	if c.PlayedContext != nil {
		for _, t := range c.PlayedContext.RecentTurns {
			history.RecentTurns = append(history.RecentTurns, turn{t.User, t.AssistantPlayed})
		}
	}
	raw, _ := json.Marshal(history) // Only valid UTF-8 strings and slices.
	return realtimelive.ExpressionInput{CurrentTranscript: c.Request, DeviceName: f.RobotName, PlayedContext: string(raw)}
}

func cleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != string(os.PathSeparator) && !strings.ContainsRune(path, 0)
}

func safeParents(path string) bool {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if parent == filepath.Dir(parent) {
			return true
		}
	}
}

func safeRunPaths(config, out string) bool {
	if !cleanAbsolute(config) || !cleanAbsolute(out) || !safeParents(config) || !safeParents(out) {
		return false
	}
	info, err := os.Lstat(config)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	_, err = os.Lstat(out)
	return os.IsNotExist(err)
}

func writePrivate(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(raw)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, append(raw, '\n'))
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("face-expression-check", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // Never echo arbitrary arguments or protected errors.
	fixturePath := flags.String("fixture", defaultFixture, "frozen synthetic fixture")
	run := flags.Bool("run", false, "explicitly enable provider inference")
	configPath := flags.String("config", "", "protected cloud config, only read with --run")
	out := flags.String("out", "", "fresh absolute private output directory")
	source := flags.String("source-sha", "", "optional exact 40-hex source commit")
	version := flags.String("version", "face-expression-check-v1", "validation tool version")
	label := flags.String("run-label", "", "optional run provenance label")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, "Default: validate fixture only; no config read, inference or writes.\nFlags: --fixture PATH [--run --config ABSOLUTE_PROTECTED_FILE --out ABSOLUTE_NEW_DIRECTORY] [--source-sha 40HEX] [--version LABEL] [--run-label LABEL]\nPlayback window and physical DMA are not measured; paired voice/fault metadata is not executed.")
			return 0
		}
		fmt.Fprintln(stderr, "invalid_arguments")
		return 2
	}
	if flags.NArg() != 0 || (*source != "" && !sourcePattern.MatchString(*source)) || !labelPattern.MatchString(*version) || (*label != "" && !labelPattern.MatchString(*label)) || (*out != "" && !cleanAbsolute(*out)) || (*configPath != "" && !cleanAbsolute(*configPath)) {
		fmt.Fprintln(stderr, "invalid_arguments")
		return 2
	}
	f, raw, err := loadFixture(*fixturePath)
	if err != nil {
		fmt.Fprintln(stderr, "fixture_validation_failed")
		return 2
	}
	digest := sha256.Sum256(raw)
	meta := runMetadata{SchemaVersion: 1, Protocol: f.Protocol, FixtureSHA256: hex.EncodeToString(digest[:]), SourceSHA: strings.ToLower(*source), SourceStatus: "not_provided", Version: *version, RunLabel: *label, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), RobotName: f.RobotName, SelectorTimeoutMS: f.SelectorTimeoutMS, MaxRequests: 1, NormalCases: len(f.Cases), FaultCases: f.FaultCases, VoiceCaseIDs: f.VoiceCaseIDs, VoiceOrder: f.VoiceOrder}
	if meta.SourceSHA != "" {
		meta.SourceStatus = "provided"
	}
	if !*run {
		plan := map[string]any{"mode": "validate_only", "normal_cases": len(f.Cases), "fixture_sha256": meta.FixtureSHA256, "selector_timeout_ms": meta.SelectorTimeoutMS, "max_generation_requests_per_case": 1, "plan": "explicit --run calls the production SelectExpression once per normal case, sequentially, without retries", "playback_window": "not_measured", "physical_status": "not_measured", "fault_cases": "not_executed", "voice_comparison": "not_executed", "metadata": meta}
		if json.NewEncoder(stdout).Encode(plan) != nil {
			return 1
		}
		return 0
	}
	if !safeRunPaths(*configPath, *out) {
		fmt.Fprintln(stderr, "unsafe_run_paths")
		return 2
	}
	if deps.loadConfig == nil || deps.selectExpression == nil {
		fmt.Fprintln(stderr, "missing_dependencies")
		return 2
	}
	if err := os.Mkdir(*out, 0700); err != nil {
		fmt.Fprintln(stderr, "output_creation_failed")
		return 1
	}
	records := make([]caseRecord, len(f.Cases))
	for i, c := range f.Cases {
		records[i] = caseRecord{ID: c.ID, Status: "pending", AcceptableEmotions: c.AcceptableEmotions}
	}
	// Fixture and source provenance must be durable before config loading or any
	// selector call. Never replace a previous attempt, including failed runs.
	if writePrivate(filepath.Join(*out, "fixture.json"), raw) != nil || writeJSON(filepath.Join(*out, "metadata.json"), meta) != nil || writeJSON(filepath.Join(*out, "summary-000.json"), aggregate(meta, records, "in_progress")) != nil {
		fmt.Fprintln(stderr, "provenance_write_failed")
		return 1
	}
	finish := func(status, reason string) int {
		for i := range records {
			if records[i].Status == "pending" {
				records[i].Status, records[i].NotRunReason = "not_run", reason
			}
		}
		final := aggregate(meta, records, status)
		if writeJSON(filepath.Join(*out, "summary.json"), final) != nil {
			fmt.Fprintln(stderr, "summary_write_failed")
			return 1
		}
		if json.NewEncoder(stdout).Encode(final) != nil {
			return 1
		}
		if status != "completed" || final.Passed != len(f.Cases) {
			return 1
		}
		return 0
	}
	if ctx.Err() != nil {
		return finish("cancelled", "cancelled")
	}
	cfg, err := deps.loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, "protected_config_load_failed")
		return finish("config_failed", "config_failed")
	}
	// These preregistered inputs are synthetic; tag the production private audit
	// accurately. This flag does not select another provider or execution path.
	cfg.Synthetic = true
	for i, c := range f.Cases {
		if ctx.Err() != nil {
			return finish("cancelled", "cancelled")
		}
		dir := filepath.Join(*out, c.ID+"-selector")
		attempt := map[string]any{"id": c.ID, "state": "started", "started_at": time.Now().UTC().Format(time.RFC3339Nano), "fixture_sha256": meta.FixtureSHA256, "source_sha": meta.SourceSHA, "version": meta.Version, "selector_timeout_ms": meta.SelectorTimeoutMS, "max_generation_requests": 1}
		if os.Mkdir(dir, 0700) != nil || writeJSON(filepath.Join(*out, c.ID+"-attempt.json"), attempt) != nil {
			fmt.Fprintln(stderr, "attempt_write_failed")
			return finish("output_failed", "output_failed")
		}
		start := time.Now()
		result := deps.selectExpression(ctx, cfg, dir, expressionInput(f, c))
		records[i] = evaluate(c, result, time.Since(start), ctx.Err() != nil)
		if ctx.Err() != nil {
			for j := i + 1; j < len(records); j++ {
				records[j].Status, records[j].NotRunReason = "not_run", "cancelled"
			}
		}
		if writeJSON(filepath.Join(*out, c.ID+".json"), records[i]) != nil || writeJSON(filepath.Join(*out, fmt.Sprintf("summary-%03d.json", i+1)), aggregate(meta, records, "in_progress")) != nil {
			fmt.Fprintln(stderr, "case_write_failed")
			return finish("output_failed", "output_failed")
		}
	}
	if ctx.Err() != nil {
		return finish("cancelled", "cancelled")
	}
	status := "completed"
	for _, c := range records {
		if !c.Passed {
			status = "failed"
			break
		}
	}
	return finish(status, "not_attempted")
}

func evaluate(c scene, result realtimelive.ExpressionResult, observed time.Duration, cancelled bool) caseRecord {
	// The production API exposes bounded reason codes, never raw provider errors.
	// Preserve only that contract if a future implementation violates the boundary.
	if !reasonPattern.MatchString(result.Reason) {
		result.Reason = "unrecognized_reason"
	}
	usage := map[string]int64{}
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens", "cached_tokens"} {
		if n, ok := result.Usage[key]; ok && n >= 0 {
			usage[key] = n
		}
	}
	result.Usage = usage
	r := caseRecord{ID: c.ID, Status: "attempted", AcceptableEmotions: c.AcceptableEmotions, Result: &result, SelectorCompleted: result.Reason == "completed", Canonical: slices.Contains(canonicalEmotions, result.Emotion), Acceptable: slices.Contains(c.AcceptableEmotions, result.Emotion), TimingValid: result.TotalMS >= 0 && result.ConnectMS >= 0 && result.ConnectMS <= result.TotalMS, ObservedTotalMS: observed.Milliseconds()}
	r.WithinDeadline = r.TimingValid && result.TotalMS < realtimelive.ExpressionSelectionBudget.Milliseconds() && observed < realtimelive.ExpressionSelectionBudget
	if !r.SelectorCompleted {
		r.Failures = append(r.Failures, "selector_"+result.Reason)
	}
	if !r.Canonical {
		r.Failures = append(r.Failures, "noncanonical_emotion")
	}
	if !r.Acceptable {
		r.Failures = append(r.Failures, "semantic_mismatch")
	}
	if !r.TimingValid {
		r.Failures = append(r.Failures, "invalid_timing")
	}
	if !r.WithinDeadline {
		r.Failures = append(r.Failures, "deadline")
	}
	if result.RequestCount != 1 {
		r.Failures = append(r.Failures, "request_count")
	}
	if cancelled {
		r.Failures = append(r.Failures, "cancelled")
	}
	r.Passed = len(r.Failures) == 0
	return r
}

func aggregate(meta runMetadata, records []caseRecord, status string) summary {
	s := summary{Metadata: meta, Status: status, RequiredCases: len(records), Cases: records, ReasonCounts: map[string]int{}, DefaultReasons: map[string]int{}, FailureCounts: map[string]int{}, UsageTotals: map[string]int64{}, UsageMetricCases: map[string]int{}, PlaybackWindow: "not_measured", PhysicalStatus: "not_measured", VoiceComparison: "not_executed", FaultCases: "not_executed"}
	var connect, total []int64
	for _, c := range records {
		if c.Result == nil {
			s.NotRun++
			continue
		}
		s.Attempted++
		if c.Passed {
			s.Passed++
		} else {
			s.Failed++
		}
		r := c.Result
		s.RequestCount += r.RequestCount
		if r.RequestCount > meta.MaxRequests {
			s.MaxRequestViolations++
		}
		s.ReasonCounts[r.Reason]++
		if r.Reason != "completed" {
			s.DefaultReasons[r.Reason]++
		}
		for _, reason := range c.Failures {
			s.FailureCounts[reason]++
		}
		if len(r.Usage) > 0 {
			s.UsageObservedCases++
		}
		for key, n := range r.Usage {
			s.UsageTotals[key] += n
			s.UsageMetricCases[key]++
		}
		if c.TimingValid {
			connect = append(connect, r.ConnectMS)
			total = append(total, r.TotalMS)
		}
	}
	s.ConnectLatency, s.TotalLatency = latency(connect), latency(total)
	return s
}

func latency(values []int64) *latencySummary {
	if len(values) == 0 {
		return nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	sum := float64(0)
	for _, n := range values {
		sum += float64(n)
	}
	middle := len(values) / 2
	median := float64(values[middle])
	if len(values)%2 == 0 {
		median = (float64(values[middle-1]) + median) / 2
	}
	return &latencySummary{Samples: len(values), Min: values[0], Median: median, P95: values[int(math.Ceil(float64(len(values))*.95))-1], Max: values[len(values)-1], Mean: sum / float64(len(values))}
}
