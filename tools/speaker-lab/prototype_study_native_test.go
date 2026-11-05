//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This is an opt-in engineering proof. Reused synthetic waveforms are never
// relabeled as independent human identities or passed through the public loader.
func TestRealPrototypeStudyOptIn(t *testing.T) {
	fixturePath, model, sha, binary := os.Getenv("SPEAKER_LAB_TEST_PROTOTYPE_FIXTURE"), os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if fixturePath == "" || model == "" || sha == "" || binary == "" {
		t.Skip("explicit synthetic prototype fixture+model+digest+binary opt-in not set")
	}
	for _, p := range []string{fixturePath, model, binary} {
		if !filepath.IsAbs(p) {
			t.Fatal("absolute explicit inputs required")
		}
	}
	fixture, _, err := loadManifest(fixturePath)
	if err != nil || fixture.SourceKind != "synthetic_tts" || fixture.HumanAcceptance || !fixture.SingleSpeakerOnly {
		t.Fatal("strict synthetic fixture required")
	}
	em := evaluationManifest{SourceKind: "synthetic_tts", SampleRate: sampleRate}
	for _, c := range fixture.Cases {
		em.Cases = append(em.Cases, evaluationCase{ID: c.ID, Speaker: c.Speaker, Split: "evaluation", Path: c.Path, Samples: c.Samples, SHA: c.SHA, TextSHA: c.TextSHA})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var worker *workerProcess
	socket := ""
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
		t.Fatal("native synthetic extraction failed", err)
	}
	defer ex.clearVectors()
	if starts != 1 || ex.qualified != len(fixture.Cases) || ex.native.WarmCount != ex.qualified-1 || ex.dimension < 1 {
		t.Fatal("shared native extraction coverage")
	}
	// Select by declared source order and distinct waveform digest, never scores.
	sources := []embeddedCase{}
	waveSHAs := map[string]bool{}
	for i, c := range fixture.Cases {
		if !waveSHAs[c.SHA] {
			waveSHAs[c.SHA] = true
			sources = append(sources, ex.cases[i])
			if len(sources) == 2 {
				break
			}
		}
	}
	if len(sources) != 2 {
		t.Fatal("two distinct native waveforms required")
	}
	m, a, cs := evidenceStudyFixture()
	m.SingleSpeakerBasis = "synthetic_fixture_declared_single_speaker"
	defer func() {
		for _, c := range cs {
			clear(c.vector)
		}
	}()
	enrollOrdinal := map[string]int{}
	for i, c := range m.Cases {
		which := 0
		if strings.HasSuffix(c.Role, "_enrollment") {
			which = enrollOrdinal[c.Speaker] / 2 % 2
			enrollOrdinal[c.Speaker]++
		} else {
			which = i % 2
		}
		src := sources[which]
		clear(cs[i].vector)
		cs[i] = embeddedCase{fixtureCase: fixtureCase{ID: c.ID, Speaker: c.Speaker}, quality: "qualified", vector: append([]float32(nil), src.vector...)}
		m.Cases[i].Samples = src.Samples
		// Synthetic duration only; this is not a physical VAD speech claim.
		a[c.SHA] = auditCase{Samples: src.Samples, SpeechSamples: min(src.Samples, 16000)}
	}
	// Two identical native copies per mode give deterministic positive cluster
	// membership. Keep train_1 at two segments to exercise fallback in this report.
	initial := len(m.Cases)
	for i := 0; i < initial; i++ {
		c := m.Cases[i]
		if !strings.HasSuffix(c.Role, "_enrollment") || c.Speaker == "train_1" {
			continue
		}
		c.ID += "_mode2"
		c.SHA = c.ID
		src := sources[1]
		c.Samples = src.Samples
		m.Cases = append(m.Cases, c)
		cs = append(cs, embeddedCase{fixtureCase: fixtureCase{ID: c.ID, Speaker: c.Speaker}, quality: "qualified", vector: append([]float32(nil), src.vector...)})
		a[c.SHA] = auditCase{Samples: src.Samples, SpeechSamples: min(src.Samples, 16000)}
	}
	// Quality failures remain in the analysis denominators.
	rejectedKnown, rejectedUnknown := false, false
	for i, c := range m.Cases {
		if c.Role == "validation_query" && !rejectedKnown {
			clear(cs[i].vector)
			cs[i].vector = nil
			cs[i].quality = "silence"
			rejectedKnown = true
		}
		if c.Role == "test_unknown" && !rejectedUnknown {
			clear(cs[i].vector)
			cs[i].vector = nil
			cs[i].quality = "short"
			rejectedUnknown = true
		}
	}
	borrowed := snapshotNativeStudyVectors(t, cs)
	r, err := analyzePrototypeStudy(ctx, m, a, cs, ex.dimension)
	assertNativeStudyVectors(t, "positive analysis", cs, borrowed)
	if err != nil {
		t.Fatal("native internal prototype analysis", err)
	}
	if len(r.Methods) != 2 || r.Methods[0].Name != "B_single_template" || r.Methods[1].Name != "P2_two_prototypes" || len(r.Enrollment) != 57 {
		t.Fatal("native method layout")
	}
	if r.Methods[1].TwoPrototypePeople != 56 || r.Methods[1].FallbackPeople != 1 {
		t.Fatal("native fixture must exercise actual two-prototype identity")
	}
	if r.CaseCount != len(cs) || r.QualifiedCases != len(cs)-2 || r.RejectedSilence != 1 || r.RejectedShort != 1 {
		t.Fatal("native inclusive quality accounting")
	}
	for _, s := range r.Methods[1].Prototypes {
		if s.Speaker == "train_1" {
			if s.Status != "fallback_single" || s.Reason != "insufficient_segments" || s.Cases != 2 {
				t.Fatal("native fallback coverage")
			}
		} else if s.Status != "two_prototypes" || !reflect.DeepEqual(s.ClusterSizes, []int{2, 2}) || s.Cases != 4 {
			t.Fatal("native positive cluster membership")
		}
	}
	// Removing only the added enrollment copies exercises the complete original
	// B fallback, including all three pool/ROC/calibration/person objects.
	fallbackManifest := m
	fallbackManifest.Cases = m.Cases[:initial]
	fallbackBorrowed := snapshotNativeStudyVectors(t, cs[:initial])
	fr, e := analyzePrototypeStudy(ctx, fallbackManifest, a, cs[:initial], ex.dimension)
	assertNativeStudyVectors(t, "all-fallback analysis", cs[:initial], fallbackBorrowed)
	if e != nil || fr.Methods[1].TwoPrototypePeople != 0 || fr.Methods[1].FallbackPeople != 57 || !reflect.DeepEqual(fr.Methods[0].Pools, fr.Methods[1].Pools) {
		t.Fatal("native complete exact-B fallback pool parity", e)
	}
	if r.SourceKind != "synthetic_tts" || r.SingleSpeakerBasis != m.SingleSpeakerBasis || r.Population != "synthetic_engineering_fixture" || r.OpenSetAssessment != "unavailable" || r.NewBlindTest || r.QueryAccumulation || r.FeatureEnabled || r.HumanAcceptance || r.ProductionAcceptance || r.HouseholdAssessment != "unavailable" || r.EndToEndLatencyAssessment != "unavailable" {
		t.Fatal("truthful synthetic nonacceptance metadata")
	}
	for _, method := range r.Methods {
		if len(method.Pools) != 3 || len(method.Prototypes) != 57 {
			t.Fatal("native pools/prototype coverage")
		}
		for pi, p := range method.Pools {
			if len(p.Rules) != 1 {
				t.Fatal("rule layout")
			}
			c := p.Rules[0].Counts
			total, unknown := 100, 10
			if pi == 2 {
				total, unknown = 8, 40
			}
			if c.Total != total || c.Qualified != total-btoiNative(pi < 2) || c.UnknownTotal != unknown || c.UnknownQualified != unknown-btoiNative(pi == 2) {
				t.Fatal("inclusive denominator", c)
			}
			if p.Rules[0].Status == "evaluated" && (c.Correct+c.Wrong+c.Rejected != total || c.UnknownAccepted+c.UnknownRejected != unknown) {
				t.Fatal("decision accounting")
			}
		}
	}
	// Independently construct original B and compare each native identity's query
	// score with the prototype single-template scorer, preserving original order.
	for _, s := range r.Enrollment {
		var clips []evidenceLabeledVector
		var original [][]float32
		for i, c := range m.Cases {
			if c.Speaker == s.Speaker && strings.HasSuffix(c.Role, "_enrollment") {
				clips = append(clips, evidenceLabeledVector{Speaker: c.Speaker, ID: c.ID, Vector: cs[i].vector})
				original = append(original, cs[i].vector)
			}
		}
		func() {
			// Deferred cleanup also runs when an assertion or builder fails.
			want, e := fuseEnrollment(original, ex.dimension)
			defer clear(want)
			if e != nil {
				t.Fatal(e)
			}
			// Explicit fallback must remain bit-exact original B and own its output.
			p, summary, e := buildEnrollmentPrototypes(clips[:2], ex.dimension)
			defer clearNativeStudyVectors(p)
			fallback, e2 := fuseEnrollment(original[:2], ex.dimension)
			defer clear(fallback)
			if e != nil || e2 != nil || summary.Status != "fallback_single" || len(p) != 1 || !reflect.DeepEqual(p[0], fallback) {
				t.Fatal("native exact fallback")
			}
			for _, src := range sources {
				got, e := prototypeScore(src.vector, [][]float32{want}, ex.dimension)
				if e != nil || got != similarity(src.vector, want) {
					t.Fatal("native original B score parity")
				}
			}
			clearNativeStudyVectors(p)
			assertNativeStudyVectors(t, "cleared owned fallback", cs, borrowed)
			if s.Speaker != "train_1" {
				positive, summary, e := buildEnrollmentPrototypes(clips, ex.dimension)
				defer clearNativeStudyVectors(positive)
				if e != nil || summary.Status != "two_prototypes" || len(positive) != 2 || !reflect.DeepEqual(summary.ClusterSizes, []int{2, 2}) {
					t.Fatal("native owned positive prototypes")
				}
				clearNativeStudyVectors(positive)
				assertNativeStudyVectors(t, "cleared owned positive prototypes", cs, borrowed)
			}
		}()
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{fixturePath, model, binary, "/private/", `"vector"`, `"embedding"`, `"path"`, `"recording_sha256"`, `"text_sha256"`} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("private native data in report")
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
	if _, e := os.Lstat(socket); !os.IsNotExist(e) {
		t.Fatal("native socket retained")
	}
	// Extraction originals remain valid; actual analysis inputs were checked above.
	for _, src := range sources {
		if !validVector(src.vector, ex.dimension) {
			t.Fatal("analysis mutated borrowed native vector")
		}
	}
	ex.clearVectors()
	for _, c := range ex.cases {
		for _, v := range c.vector {
			if v != 0 {
				t.Fatal("native vector retained")
			}
		}
	}
	t.Logf("native=%d dimension=%d distinct_waveforms=%d synthetic_reused_cases=%d two_prototype_logical_identities=%d fallback=%d (engineering only; no human accuracy)", ex.qualified, ex.dimension, len(sources), r.CaseCount, r.Methods[1].TwoPrototypePeople, r.Methods[1].FallbackPeople)
}

func btoiNative(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Snapshot only in RAM, with cleanup registered before any subsequent assertion.
func snapshotNativeStudyVectors(t *testing.T, cases []embeddedCase) [][]float32 {
	t.Helper()
	snapshot := make([][]float32, len(cases))
	t.Cleanup(func() { clearNativeStudyVectors(snapshot) })
	for i, c := range cases {
		if c.vector != nil {
			snapshot[i] = append([]float32{}, c.vector...)
		}
	}
	return snapshot
}

func clearNativeStudyVectors(vectors [][]float32) {
	for _, v := range vectors {
		clear(v)
	}
}

func assertNativeStudyVectors(t *testing.T, phase string, cases []embeddedCase, snapshot [][]float32) {
	t.Helper()
	if len(cases) != len(snapshot) {
		t.Fatal(phase, "borrowed native vector count changed")
	}
	for i, c := range cases {
		if len(c.vector) != len(snapshot[i]) || (c.vector == nil) != (snapshot[i] == nil) {
			t.Fatal(phase, "borrowed native vector shape changed")
		}
		for j, v := range c.vector {
			if math.Float32bits(v) != math.Float32bits(snapshot[i][j]) {
				t.Fatal(phase, "borrowed native vector bits changed")
			}
		}
	}
}
