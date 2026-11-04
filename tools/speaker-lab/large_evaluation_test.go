//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func largeFixture(speakers, clips int) evaluationManifest {
	m := evaluationFixture()
	m.Cases = nil
	for s := 1; s <= speakers; s++ {
		for n := 0; n < clips; n++ {
			split := "evaluation"
			if n == 0 {
				split = "enrollment"
			} else if n < 4 {
				split = "calibration"
			}
			digest := func(kind string) string {
				h := sha256.Sum256([]byte(fmt.Sprintf("%s_%d_%d", kind, s, n)))
				return hex.EncodeToString(h[:])
			}
			m.Cases = append(m.Cases, evaluationCase{ID: fmt.Sprintf("case_%d_%d", s, n), Speaker: fmt.Sprintf("fixture_%d", s), Split: split, Path: fmt.Sprintf("/private/corpus/%d_%d.wav", s, n), Samples: 32000, SHA: digest("wav"), RecordingSHA: digest("recording"), TextSHA: digest("text")})
		}
	}
	return m
}
func largeVectors() []embeddedCase {
	var out []embeddedCase
	for _, c := range largeFixture(50, 8).Cases {
		var s int
		fmt.Sscanf(c.Speaker, "fixture_%d", &s)
		v := make([]float32, 50)
		v[s-1] = 1
		out = append(out, embeddedCase{fixtureCase: c.fixture(), quality: "qualified", vector: v})
	}
	return out
}
func TestLargeManifestLimitsAndCompatibility(t *testing.T) {
	p := filepath.Join(t.TempDir(), "manifest.json")
	write := func(m evaluationManifest) {
		t.Helper()
		b, e := json.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(largeFixture(50, 8))
	if m, sha, e := loadLargeEvaluationManifest(p); e != nil || len(m.Cases) != 400 || len(sha) != 64 {
		t.Fatalf("large manifest rejected: %v", e)
	}
	if _, _, e := loadEvaluationManifest(p); e == nil {
		t.Fatal("ordinary evaluate accepted50 people")
	}
	mutations := map[string]func(*evaluationManifest){
		"51 speakers": func(m *evaluationManifest) { *m = largeFixture(51, 8) },
		"49 speakers": func(m *evaluationManifest) { *m = largeFixture(49, 8) },
		"2049 cases":  func(m *evaluationManifest) { *m = largeFixture(50, 41); m.Cases = m.Cases[:2049] },
		"fixture51": func(m *evaluationManifest) {
			for i := range m.Cases {
				if m.Cases[i].Speaker == "fixture_50" {
					m.Cases[i].Speaker = "fixture_51"
				}
			}
		},
		"fixture00":          func(m *evaluationManifest) { m.Cases[0].Speaker = "fixture_00" },
		"missing enrollment": func(m *evaluationManifest) { m.Cases[0].Split = "evaluation" },
		"only2 calibration":  func(m *evaluationManifest) { m.Cases[1].Split = "evaluation" },
		"only3 evaluation":   func(m *evaluationManifest) { m.Cases[4].Split = "calibration" },
		"recording leakage":  func(m *evaluationManifest) { m.Cases[4].RecordingSHA = m.Cases[1].RecordingSHA },
		"text leakage":       func(m *evaluationManifest) { m.Cases[4].TextSHA = m.Cases[1].TextSHA },
		"duplicate audio":    func(m *evaluationManifest) { m.Cases[4].SHA = m.Cases[1].SHA },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := largeFixture(50, 8)
			mutate(&m)
			write(m)
			if _, _, e := loadLargeEvaluationManifest(p); e == nil {
				t.Fatal("invalid large manifest accepted")
			}
		})
	}
	write(largeFixture(50, 40))
	if _, _, e := loadLargeEvaluationManifest(p); e != nil {
		t.Fatalf("valid2000 cases rejected %v", e)
	}
	b := bytes.Repeat([]byte(" "), 2*1024*1024+1)
	os.WriteFile(p, b, 0600)
	if _, _, e := loadLargeEvaluationManifest(p); e == nil {
		t.Fatal("over2MiB accepted")
	}
	write(largeFixture(50, 8))
	original, _ := os.ReadFile(p)
	for name, b := range map[string][]byte{
		"duplicate header": bytes.Replace(original, []byte(`"source_kind":`), []byte(`"source_id":"extra","source_kind":`), 1),
		"duplicate case":   bytes.Replace(original, []byte(`"recording_sha256":`), []byte(`"id":"extra","recording_sha256":`), 1),
		"null":             bytes.Replace(original, []byte(`"source_kind":"public_human"`), []byte(`"source_kind":null`), 1),
		"missing":          bytes.Replace(original, []byte(`"source_kind":"public_human",`), nil, 1),
		"unknown":          bytes.Replace(original, []byte(`"source_kind":`), []byte(`"extra":1,"source_kind":`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			os.WriteFile(p, b, 0600)
			if _, _, e := loadLargeEvaluationManifest(p); e == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
}
func TestLargeEmpiricalFARBoundaries(t *testing.T) {
	tests := []struct {
		name              string
		scores            []float64
		target, threshold float64
		accepted          int
		status            string
	}{
		{"ties", []float64{.9, .8, .8, .1}, .5, math.Nextafter(.8, math.Inf(1)), 1, "calibrated"},
		{"zero budget", []float64{.9, .8}, .001, math.Nextafter(.9, math.Inf(1)), 0, "calibrated"},
		{"minimum", []float64{-.5, 0, .5, 1}, 1, -1, 4, "calibrated"},
		{"unreachable", []float64{1, 1, .5}, .1, 0, 0, "unreachable"},
		{"empty", nil, .01, 0, 0, "failed_insufficient_calibration"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := fitEmpiricalFAR(tc.scores, tc.target)
			if r.Status != tc.status || r.MatchThreshold != tc.threshold || r.AcceptedDifferent != tc.accepted {
				t.Fatalf("got%+v", r)
			}
			if r.Status == "calibrated" && r.EmpiricalFAR > tc.target {
				t.Fatal("FAR budget exceeded")
			}
		})
	}
	scores := make([]float64, 1000)
	for i := range scores {
		scores[i] = float64(i) / 1000
	}
	r := fitEmpiricalFAR(scores, .001)
	if r.AcceptedDifferent != 1 || r.EmpiricalFAR != .001 || r.MatchThreshold != math.Nextafter(.998, math.Inf(1)) {
		t.Fatalf("integer budget wrong %+v", r)
	}
}
func TestLargeRulesScoringAndFrozenCalibration(t *testing.T) {
	a := largeVectors()
	r, e := analyzeLargeEvaluation(a, 50)
	if e != nil || r.Calibration.FittedSpeakers != 50 || r.Calibration.FittedCases != 150 || r.Calibration.SamePairs != 150 || r.Calibration.DifferentPairs != 11025 || len(r.Rules) != 4 {
		t.Fatalf("all calibration pairs missing %+v %v", r.Calibration, e)
	}
	for i, rule := range r.Rules {
		if rule.Status != "evaluated" || len(rule.Pools) != 2 {
			t.Fatalf("rule%d %+v", i, rule)
		}
		for _, p := range rule.Pools {
			if p.Total != 200 || p.Correct != 200 || p.Wrong != 0 || p.Rejected != 0 || p.Accuracy != 1 || p.EffectiveRate != 1 || p.Pairs.SameAccepted != 200 || p.Pairs.DifferentAccepted != 0 || p.Pairs.DifferentRejected != 200*(p.Candidates-1) {
				t.Fatalf("bad pool %+v", p)
			}
		}
	}
	if r.Rules[0].MatchThreshold != .682987 || r.Rules[0].Margin != .030229 {
		t.Fatal("current session rule drift")
	}
	if len(r.Rules[0].Pools[1].Groups) != 10 || r.Rules[0].Pools[1].Groups[0].Group != 1 || r.Rules[0].Pools[1].Groups[9].Group != 10 {
		t.Fatal("missing anonymous groups")
	}
	frozen := append([]largeRuleResult(nil), r.Rules...)
	// fixture1 evaluation misassigned to fixture6: wrong at50, rejected in fixed group1.
	a[4].vector = append([]float32(nil), a[5*8].vector...)
	a[5].quality = "short"
	a[5].vector = nil
	// Tie between fixture1 and fixture2 must never silently choose first.
	a[6].vector = make([]float32, 50)
	a[6].vector[0], a[6].vector[1] = 1, 1
	r, e = analyzeLargeEvaluation(a, 50)
	if e != nil {
		t.Fatal(e)
	}
	for i, rule := range r.Rules {
		if rule.MatchThreshold != frozen[i].MatchThreshold || rule.Margin != frozen[i].Margin || rule.AcceptedDifferent != frozen[i].AcceptedDifferent {
			t.Fatal("evaluation changed thresholds")
		}
		p, q := rule.Pools[0], rule.Pools[1]
		if p.Total != 200 || p.Correct != 197 || p.Wrong != 1 || p.Rejected != 2 || q.Correct != 197 || q.Wrong != 0 || q.Rejected != 3 || p.Accuracy != 197.0/200 {
			t.Fatalf("wrong/tie/quality denominator %+v %+v", p, q)
		}
	}
	if r.Top1[0].Correct != 197 || r.Top1[0].Tied != 1 || r.Top1[0].QualityRejected != 1 || r.Top1[0].Total != 200 {
		t.Fatalf("Top1 %+v", r.Top1)
	}
	// Additional calibration is included rather than truncated to first3.
	a = largeVectors()
	extra := a[1]
	extra.vector = append([]float32(nil), a[8].vector...)
	a = append(a, extra)
	r, e = analyzeLargeEvaluation(a, 50)
	if e != nil || r.Calibration.FittedCases != 151 || r.Calibration.SamePairs != 153 || r.Calibration.DifferentPairs != 11172 || r.Rules[1].Status != "not_run" || r.Calibration.Status != "failed_nonseparable" || r.Rules[0].Status != "evaluated" {
		t.Fatalf("extra calibration ignored %+v %v", r, e)
	}
}
func TestLargeQualifiedMinimumsAndFirstEnrollment(t *testing.T) {
	for _, index := range []int{0, 1} {
		a := largeVectors()
		a[index].quality = "silence"
		a[index].vector = nil
		r, e := analyzeLargeEvaluation(a, 50)
		if e != nil {
			t.Fatal(e)
		}
		for _, rule := range r.Rules {
			if rule.Status != "not_run" {
				t.Fatal("missing qualified input scored")
			}
		}
	}
	a := largeVectors()
	extra := a[0]
	extra.vector = append([]float32(nil), a[8].vector...)
	a = append(a, extra)
	a[4].vector = extra.vector
	r, e := analyzeLargeEvaluation(a, 50)
	if e != nil || r.Rules[0].Pools[0].Wrong != 1 {
		t.Fatal("later enrollment replaced first")
	}
	for _, v := range [][]float32{nil, {1, 0}, make([]float32, 50), {float32(math.NaN())}} {
		a = largeVectors()
		a[4].vector = v
		if _, e := analyzeLargeEvaluation(a, 50); e == nil {
			t.Fatal("invalid qualified vector accepted")
		}
	}
}
func TestLargeReportPrivacyAndCLI(t *testing.T) {
	r := newLargeEvaluationReport(testSHA)
	var e error
	r.largeEvaluationAnalysis, e = analyzeLargeEvaluation(largeVectors(), 50)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"vector", "embedding", "path", "fixture_", "source_url", "license_url", "source_id", "recording_sha256", "text_sha256", "\"cases\":"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("unsafe report contains%s", secret)
		}
	}
	if r.HumanAcceptance || r.ProductionAcceptance || r.FeatureEnabled || r.SegmentationAssessment != "unavailable" || r.OverlapAssessment != "unavailable" || r.OpenSetAssessment != "unavailable" {
		t.Fatal("unsafe acceptance")
	}
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"evaluate-large"}, "usage"},
		{[]string{"evaluate-large", "--model", "/secret/model", "--sha256", testSHA, "--manifest", "/secret/missing.json"}, "evaluate_large_failed"},
		{[]string{"evaluate-large", "--model", "/secret/model", "--sha256", testSHA, "--manifest", "/secret/missing.json", "extra"}, "usage"},
	} {
		var out, errOut bytes.Buffer
		if runCLI(context.Background(), tc.args, &out, &errOut) != 1 || out.Len() != 0 || strings.TrimSpace(errOut.String()) != fmt.Sprintf(`{"error":%q}`, tc.code) {
			t.Fatalf("unsafe CLI %q %q", out.String(), errOut.String())
		}
	}
	// Old report has no new fields. Its schema stays byte-for-byte identical.
	old := newEvaluationReport(testSHA)
	old.evaluationAnalysis, _ = analyzeEvaluation(evaluationVectors(), 3)
	oldBytes, _ := json.Marshal(old)
	var fields map[string]json.RawMessage
	json.Unmarshal(oldBytes, &fields)
	want := []string{"source_kind", "human_acceptance", "production_acceptance", "feature_enabled", "segmentation_assessment", "overlap_assessment", "open_set_assessment", "single_speaker_basis", "model_sha256", "manifest_sha256", "dimension", "case_count", "qualified_cases", "rejected_short", "rejected_silence", "native_inference", "wall_ipc_inference", "calibration", "evaluation"}
	if len(fields) != len(want) {
		t.Fatalf("old JSON schema changed%d", len(fields))
	}
	for _, k := range want {
		if _, ok := fields[k]; !ok {
			t.Fatal("old field missing", k)
		}
	}
	if !reflect.DeepEqual(old.Calibration, fitEvaluation(evaluationVectors(), groupCases(evaluationVectors()))) {
		t.Fatal("old calibration changed")
	}
}

func TestLargeReportOmitsLegacyScoringAndPreservesRuleDefinitions(t *testing.T) {
	r := newLargeEvaluationReport(testSHA)
	var e error
	r.largeEvaluationAnalysis, e = analyzeLargeEvaluation(largeVectors(), 50)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(r)
	var fields map[string]json.RawMessage
	json.Unmarshal(b, &fields)
	if _, ok := fields["evaluation"]; ok {
		t.Fatal("large report contains unrelated zero-valued legacy evaluation")
	}
}
func TestLargeFailurePreservesRuleDefinitions(t *testing.T) {
	a := largeVectors()
	a[1].quality = "short"
	a[1].vector = nil
	analysis, e := analyzeLargeEvaluation(a, 50)
	if e != nil {
		t.Fatal(e)
	}
	if analysis.Rules[0].MatchThreshold != .682987 || analysis.Rules[0].Margin != .030229 || analysis.Rules[2].TargetCalibrationFAR == nil || *analysis.Rules[2].TargetCalibrationFAR != .001 || analysis.Rules[3].TargetCalibrationFAR == nil || *analysis.Rules[3].TargetCalibrationFAR != .01 {
		t.Fatal("failed calibration lost preregistered rules")
	}
}

// A child exercises real bounded IPC and process reaping without native models.
type largeTestExtractor struct {
	calls int
	hang  bool
}

func (x *largeTestExtractor) Dimension() int { return 2 }
func (x *largeTestExtractor) Close()         {}
func (x *largeTestExtractor) Extract([]byte) ([]float32, error) {
	x.calls++
	if x.hang && x.calls == 2 {
		time.Sleep(10 * time.Second)
	}
	return []float32{1, 0}, nil
}
func TestLargeExtractionHelper(t *testing.T) {
	mode := os.Getenv("SPEAKER_LAB_LARGE_HELPER")
	if mode == "" {
		return
	}
	l, e := listenPrivate(os.Getenv("SPEAKER_LAB_HELPER_SOCKET"))
	if e != nil {
		os.Exit(2)
	}
	defer l.Close()
	c, e := l.Accept()
	if e != nil {
		os.Exit(3)
	}
	defer c.Close()
	serveConnection(context.Background(), c, &largeTestExtractor{hang: mode == "hang"}, testSHA)
}
func TestLargeExtractionOwnershipCancellationAndLoadFailure(t *testing.T) {
	for _, mode := range []string{"success", "hang", "bad_digest", "quality"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "sle-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			p := filepath.Join(dir, "input.wav")
			b := wavBytes(32000)
			copy(b[44:], sessionPCM(32000))
			if e := os.WriteFile(p, b, 0600); e != nil {
				t.Fatal(e)
			}
			h := sha256.Sum256(b)
			c := evaluationCase{ID: "one", Speaker: "fixture_1", Split: "evaluation", Path: p, Samples: 32000, SHA: hex.EncodeToString(h[:])}
			m := evaluationManifest{Cases: []evaluationCase{c, c}}
			if mode == "bad_digest" {
				m.Cases[1].SHA = testSHA
			}
			if mode == "quality" {
				b = wavBytes(32000)
				os.WriteFile(p, b, 0600)
				h = sha256.Sum256(b)
				for i := range m.Cases {
					m.Cases[i].SHA = hex.EncodeToString(h[:])
				}
			}
			socketDir := filepath.Join(dir, "worker")
			os.Mkdir(socketDir, 0700)
			socket := filepath.Join(socketDir, "worker.sock")
			var worker *workerProcess
			start := func(ctx context.Context, _, _ string) (*workerProcess, error) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestLargeExtractionHelper$")
				cmd.Env = append(os.Environ(), "SPEAKER_LAB_LARGE_HELPER="+mode, "SPEAKER_LAB_HELPER_SOCKET="+socket)
				w, e := launchWorker(ctx, cmd, socket, testSHA, time.Second)
				if e == nil {
					worker = w
					w.cleanup = func() { cleanupWorkerDirectory(socketDir, socket, w.socketIdentity) }
				}
				return w, e
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if mode == "hang" {
				ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
				defer cancel()
			}
			extracted, e := extractEvaluation(ctx, m, "unused", testSHA, start)
			if mode == "success" {
				if e != nil || extracted.qualified != 2 || extracted.dimension != 2 || len(extracted.cases) != 2 || extracted.workerRSS <= 0 || extracted.cpuMS <= 0 {
					t.Fatalf("bad extraction %+v %v", extracted, e)
				}
				v := extracted.cases[0].vector
				extracted.clearVectors()
				if v[0] != 0 || v[1] != 0 {
					t.Fatal("caller did not clear owned vectors")
				}
			}
			if mode == "quality" && (e != nil || extracted.qualified != 0 || extracted.silence != 2) {
				t.Fatalf("quality gate wrong %+v %v", extracted, e)
			}
			if (mode == "hang" || mode == "bad_digest") && (e == nil || len(extracted.cases) != 0) {
				t.Fatal("error retained vectors")
			}
			if worker == nil || worker.cmd.ProcessState == nil {
				t.Fatal("worker not reaped")
			}
			if _, e := os.Lstat(socketDir); !os.IsNotExist(e) {
				t.Fatal("owned worker files not cleaned")
			}
		})
	}
}
func TestLargeExactLimitsAndEquality(t *testing.T) {
	p := filepath.Join(t.TempDir(), "manifest.json")
	m := largeFixture(50, 41)
	m.Cases = m.Cases[:2048]
	b, _ := json.Marshal(m)
	b = append(b, bytes.Repeat([]byte(" "), 2*1024*1024-len(b))...)
	os.WriteFile(p, b, 0600)
	if loaded, _, e := loadLargeEvaluationManifest(p); e != nil || len(loaded.Cases) != 2048 {
		t.Fatalf("exact2048/2MiB rejected %v", e)
	}
	a := largeVectors()
	groups := make([]string, 50)
	templates := map[string][]float32{}
	for i := range groups {
		groups[i] = fmt.Sprintf("fixture_%d", i+1)
		templates[groups[i]] = a[i*8].vector
	}
	pool := scoreLargePool(a, groups, templates, 50, 1, 0)
	if pool.Correct != 200 {
		t.Fatal("threshold equality rejected")
	}
}
