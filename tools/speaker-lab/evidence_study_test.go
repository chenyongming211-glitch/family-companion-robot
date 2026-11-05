//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func evidenceStudyFixture() (backendManifest, map[string]auditCase, []embeddedCase) {
	m := backendManifest{SourceKind: "synthetic_tts", Condition: "device03", SampleRate: 16000}
	a := map[string]auditCase{}
	cs := []embeddedCase{}
	person := 0
	for _, g := range []struct {
		prefix string
		n      int
	}{{"train", 3}, {"validation", 50}, {"validation_unknown", 5}, {"test", 4}, {"test_unknown", 20}} {
		for n := 1; n <= g.n; n++ {
			s := fmt.Sprintf("%s_%d", g.prefix, n)
			roles := []string{g.prefix + "_enrollment", g.prefix + "_enrollment", g.prefix + "_query", g.prefix + "_query"}
			if strings.Contains(g.prefix, "unknown") {
				roles = []string{g.prefix, g.prefix}
			}
			for _, role := range roles {
				id := fmt.Sprintf("case_%04d", len(cs))
				v := make([]float32, 128)
				v[person] = 1
				c := backendCase{ID: id, Speaker: s, Role: role, Samples: 16000, SHA: id, Path: "/private/secret.wav"}
				m.Cases = append(m.Cases, c)
				a[id] = auditCase{Samples: 16000, SpeechSamples: 16000}
				cs = append(cs, embeddedCase{fixtureCase: fixtureCase{ID: id, Speaker: s}, quality: "qualified", vector: v})
			}
			person++
		}
	}
	return m, a, cs
}
func TestEvidenceAnalysisFourArmsDenominatorsParityPrivacy(t *testing.T) {
	m, a, cs := evidenceStudyFixture()
	for i, c := range m.Cases {
		if c.Role == "validation_query" {
			cs[i].quality = "silence"
			cs[i].vector = nil
			break
		}
	}
	r, e := analyzeEvidence(context.Background(), m, a, cs, 128)
	if e != nil {
		t.Fatal(e)
	}
	if r.Dimension != 128 || r.CaseCount != len(cs) || r.QualifiedCases != len(cs)-1 || r.RejectedSilence != 1 {
		t.Fatal("internal analysis extraction summary", r.Dimension, r.CaseCount, r.QualifiedCases, r.RejectedSilence)
	}
	if r.Policy.DiagnosticStatus != "fitted" || r.Policy.ContinuityStatus != "fitted" || r.Policy.EnrollmentPeople != 3 || r.Policy.QueryPeople != 3 {
		t.Fatal(r.Policy)
	}
	want := []string{"B_baseline", "B_enrollment_diagnostic", "B_accumulation", "B_combined_diagnostic"}
	if len(r.Arms) != 4 || len(r.Diagnostics) != 57 {
		t.Fatal("layout", len(r.Diagnostics))
	}
	for i, arm := range r.Arms {
		if arm.Name != want[i] || len(arm.Scenarios) != 2 {
			t.Fatal(arm.Name)
		}
		for _, scene := range arm.Scenarios {
			if len(scene.Pools) != 3 {
				t.Fatal("pools")
			}
			p := scene.Pools[1]
			if p.Top1.Total != 100 || p.Top1.Qualified != 99 || len(p.Rules) < 1 || p.Rules[0].Counts.Total != 100 || p.Rules[0].Counts.UnknownTotal != 10 {
				t.Fatal("denominator", p)
			}
		}
	}
	for _, pair := range [][2]int{{0, 1}, {2, 3}} {
		for j := 0; j < 2; j++ {
			if !reflect.DeepEqual(r.Arms[pair[0]].Scenarios[j].Pools, r.Arms[pair[1]].Scenarios[j].Pools) {
				t.Fatal("diagnostics changed scores")
			}
		}
	}
	// Baseline must preserve original B fixed policy, candidate grouping and Top1 semantics.
	templates := map[string][][]float32{}
	for i, c := range m.Cases {
		if strings.HasSuffix(c.Role, "_enrollment") {
			templates[c.Speaker] = append(templates[c.Speaker], cs[i].vector)
		}
	}
	scores := map[int][]float64{}
	for i, c := range m.Cases {
		if c.Role != "validation_query" && c.Role != "validation_unknown" || cs[i].quality != "qualified" {
			continue
		}
		for j := 1; j <= 50; j++ {
			v, _ := fuseEnrollment(templates[fmt.Sprintf("validation_%d", j)], 128)
			scores[i] = append(scores[i], similarity(cs[i].vector, v))
			clear(v)
		}
	}
	k, u, groups := backendPoolQueries(m, cs, scores, "validation", 50, 50)
	p := r.Arms[0].Scenarios[0].Pools[1]
	if !reflect.DeepEqual(p.Top1, backendTop(k, true)) || p.GroupCount != groups {
		t.Fatal("baseline top/group parity")
	}
	for i, threshold := range []float64{largeSessionMatch, .4499944095418535} {
		if !reflect.DeepEqual(p.Rules[i+1].Counts, backendCount(k, u, threshold, largeSessionMargin, true)) {
			t.Fatal("fixed baseline parity")
		}
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"/private/", "secret.wav", `"vector"`, "recording_sha", "text_sha", "source_group"} {
		if bytes.Contains(b, []byte(s)) {
			t.Fatal("privacy", s)
		}
	}
	if r.SourceKind != "synthetic_tts" || r.NewBlindTest || r.EvidenceBasis != "constructed_sequence" || r.HouseholdAssessment != "unavailable" || r.EndToEndLatencyAssessment != "unavailable" {
		t.Fatal("truthful flags")
	}
	if cs[0].vector[0] != 1 {
		t.Fatal("borrowed vectors mutated")
	}
}
func TestEvidenceAnalysisTrainOnlyPolicyAndDisabledFallback(t *testing.T) {
	m, a, cs := evidenceStudyFixture()
	first, e := analyzeEvidence(context.Background(), m, a, cs, 128)
	if e != nil {
		t.Fatal(e)
	}
	for i, c := range m.Cases {
		if strings.HasPrefix(c.Role, "validation") || strings.HasPrefix(c.Role, "test") {
			if cs[i].quality == "qualified" {
				clear(cs[i].vector)
				cs[i].vector[127] = 1
			}
		}
	}
	changed, e := analyzeEvidence(context.Background(), m, a, cs, 128)
	if e != nil || !reflect.DeepEqual(first.Policy, changed.Policy) {
		t.Fatal("heldout leakage", e)
	}
	for i, c := range m.Cases {
		if c.Role == "train_query" {
			cs[i].quality = "short"
			cs[i].vector = nil
		}
	}
	disabled, e := analyzeEvidence(context.Background(), m, a, cs, 128)
	if e != nil || disabled.Policy.ContinuityStatus == "fitted" {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		for j, base := range disabled.Arms[0].Scenarios[i].Pools {
			got := disabled.Arms[2].Scenarios[i].Pools[j]
			if !reflect.DeepEqual(base.Top1, got.Top1) || !reflect.DeepEqual(base.Rules[0], got.Rules[0]) {
				t.Fatal("disabled accumulation changed decisions")
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := analyzeEvidence(ctx, m, a, cs, 128); e == nil {
		t.Fatal("analysis cancel")
	}
}
func TestEvidenceCLIExplicitFixedErrorsNoEcho(t *testing.T) {
	sha := fmt.Sprintf("%064d", 1)
	for _, tc := range []struct {
		args []string
		code string
	}{{nil, "usage"}, {[]string{"evidence-study"}, "usage"}, {[]string{"evidence-study", "--model", "relative", "--sha256", sha, "--manifest", "/private/missing"}, "usage"}, {[]string{"evidence-study", "--model", "/private/model", "--sha256", sha, "--manifest", "/private/missing", "--audit", "/private/leak"}, "usage"}, {[]string{"evidence-study", "--model", "/private/model", "--sha256", sha, "--manifest", "/private/missing"}, "evidence_study_failed"}} {
		var out, err bytes.Buffer
		if runCLI(context.Background(), tc.args, &out, &err) != 1 || out.Len() != 0 || err.String() != fmt.Sprintf("{\"error\":\"%s\"}\n", tc.code) {
			t.Fatal(tc.args, out.String(), err.String())
		}
	}
}

// TestRealEvidenceStudyOptIn exercises native extraction without pretending that
// repeated synthetic voices are independent public-human regression subjects.
func TestRealEvidenceStudyOptIn(t *testing.T) {
	manifest, model, sha, binary := os.Getenv("SPEAKER_LAB_TEST_EVIDENCE_FIXTURE"), os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if manifest == "" || model == "" || sha == "" || binary == "" {
		t.Skip("explicit synthetic fixture+model+digest+binary opt-in not set")
	}
	for _, p := range []string{manifest, model, binary} {
		if !filepath.IsAbs(p) {
			t.Fatal("explicit absolute inputs required")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	r, extracted, worker, socket := nativeEvidenceFixture(t, ctx, manifest, model, sha, binary)
	defer extracted.clearVectors()
	if r.SourceKind != "synthetic_tts" || r.SingleSpeakerBasis != "synthetic_fixture_declared_single_speaker" || r.Population != "synthetic_engineering_fixture" || r.NewBlindTest || r.HumanAcceptance || r.ProductionAcceptance || r.FeatureEnabled || r.HouseholdAssessment != "unavailable" || r.EvidenceBasis != "constructed_sequence" || r.EndToEndLatencyAssessment != "unavailable" {
		t.Fatal("native fixture must retain truthful engineering metadata")
	}
	if extracted.dimension != 512 || extracted.qualified < 6 || r.Dimension != extracted.dimension || r.QualifiedCases != r.CaseCount || len(r.Arms) != 4 || len(r.Diagnostics) != 57 {
		t.Fatal("native extraction/analysis coverage missing")
	}
	for _, pair := range [][2]int{{0, 1}, {2, 3}} {
		for scene := 0; scene < 2; scene++ {
			if !reflect.DeepEqual(r.Arms[pair[0]].Scenarios[scene].Pools, r.Arms[pair[1]].Scenarios[scene].Pools) {
				t.Fatal("diagnostic changed native identity results")
			}
		}
	}
	merges := 0
	for _, scene := range r.Arms[2].Scenarios {
		for _, replay := range scene.Replay {
			merges += replay.Accumulations
			if replay.EvidenceSamplesMax > 128000 {
				t.Fatal("native accumulated window exceeded sample bound")
			}
			if r.Policy.ContinuityStatus != "fitted" && replay.Accumulations != 0 {
				t.Fatal("disabled native policy accumulated")
			}
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{manifest, model, binary, `"vector"`, `"embedding"`, `"path"`, `"recording_sha256"`, `"text_sha256"`} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("private native data in anonymous report")
		}
	}
	if worker == nil || worker.cmd.ProcessState == nil {
		t.Fatal("native worker not reaped")
	}
	select {
	case <-worker.done:
	default:
		t.Fatal("native worker still running")
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Fatal("native worker socket not removed")
	}
	// Clear explicitly, rather than relying on process exit to drop native vectors.
	extracted.clearVectors()
	for _, c := range extracted.cases {
		for _, v := range c.vector {
			if v != 0 {
				t.Fatal("native extracted vectors retained")
			}
		}
	}
	t.Logf("native=%d dimension=%d synthetic_reused_cases=%d diagnostic=%s continuity=%s observed_accumulations=%d (synthetic engineering coverage only)", extracted.qualified, extracted.dimension, r.CaseCount, r.Policy.DiagnosticStatus, r.Policy.ContinuityStatus, merges)
}

func nativeEvidenceFixture(t *testing.T, ctx context.Context, manifest, model, sha, binary string) (evidenceReport, evaluationExtraction, *workerProcess, string) {
	t.Helper()
	fixture, _, err := loadManifest(manifest)
	if err != nil || fixture.SourceKind != "synthetic_tts" || fixture.HumanAcceptance || !fixture.SingleSpeakerOnly {
		t.Fatal("strict synthetic fixture required")
	}
	em := evaluationManifest{SourceKind: "synthetic_tts", SampleRate: sampleRate}
	for _, c := range fixture.Cases {
		em.Cases = append(em.Cases, evaluationCase{ID: c.ID, Speaker: c.Speaker, Split: "evaluation", Path: c.Path, Samples: c.Samples, SHA: c.SHA, TextSHA: c.TextSHA})
	}
	var worker *workerProcess
	var socket string
	starts := 0
	ex, err := extractEvaluation(ctx, em, model, sha, func(ctx context.Context, model, sha string) (*workerProcess, error) {
		starts++
		w, e := startWorkerBinary(ctx, binary, model, sha)
		if e == nil {
			worker = w
			socket = w.conn.RemoteAddr().String()
		}
		return w, e
	})
	if err != nil {
		t.Fatal("native synthetic extraction failed")
	}
	t.Cleanup(ex.clearVectors)
	if starts != 1 || ex.qualified != len(fixture.Cases) {
		t.Fatal("native extraction did not process each qualified fixture once")
	}
	voices := map[string][]embeddedCase{}
	var voiceNames []string
	for _, c := range ex.cases {
		if _, ok := voices[c.Speaker]; !ok {
			voiceNames = append(voiceNames, c.Speaker)
		}
		voices[c.Speaker] = append(voices[c.Speaker], c)
	}
	if len(voiceNames) < 3 {
		t.Fatal("native fixture needs at least three synthetic voices")
	}
	// Reuse the explicitly synthetic native fixtures across the fixed pool layout.
	// This is a pipeline/ownership check, never an accuracy or role-disjoint study.
	m, a, cs := evidenceStudyFixture()
	m.SingleSpeakerBasis = "synthetic_fixture_declared_single_speaker"
	assigned := map[string]int{}
	clips := map[string]int{}
	for i, c := range m.Cases {
		voice, ok := assigned[c.Speaker]
		if !ok {
			voice = len(assigned) % len(voiceNames)
			assigned[c.Speaker] = voice
		}
		choices := voices[voiceNames[voice]]
		source := choices[clips[c.Speaker]%len(choices)]
		clips[c.Speaker]++
		clear(cs[i].vector)
		cs[i] = embeddedCase{fixtureCase: fixtureCase{ID: c.ID, Speaker: c.Speaker}, quality: source.quality, vector: append([]float32(nil), source.vector...)}
		m.Cases[i].Samples = source.Samples
		a[c.SHA] = auditCase{Samples: source.Samples} // No synthetic VAD speech-duration assertion.
	}
	defer func() {
		for i := range cs {
			clear(cs[i].vector)
		}
	}()
	r, err := analyzeEvidence(ctx, m, a, cs, ex.dimension)
	if err != nil {
		t.Fatal("native synthetic evidence analysis failed")
	}
	return r, ex, worker, socket
}
