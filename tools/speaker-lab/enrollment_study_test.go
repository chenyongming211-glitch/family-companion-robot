//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStudyFusionNormalizesEveryClip(t *testing.T) {
	v, e := fuseEnrollment([][]float32{{10, 0}, {0, 1}}, 2)
	if e != nil || math.Abs(float64(v[0]-v[1])) > 1e-6 {
		t.Fatal(v, e)
	}
	if _, e = fuseEnrollment([][]float32{{1, 0}, {-1, 0}}, 2); e == nil {
		t.Fatal("accepted zero sum")
	}
	if _, e = fuseEnrollment([][]float32{{0, 0}}, 2); e == nil {
		t.Fatal("accepted invalid")
	}
}
func TestStudyAllViewsPoliciesAndDenominators(t *testing.T) {
	m, a := studyFixture(t)
	var cases []embeddedCase
	for i, c := range m.Cases {
		v := make([]float32, 65)
		group, number := studySpeaker(c.Speaker)
		offset := 0
		if group == "calibration" {
			offset = 50
		}
		if group == "unknown" {
			offset = 60
		}
		v[offset+number-1] = 1
		cases = append(cases, embeddedCase{fixtureCase: c.fixture(), vector: v, quality: "qualified"})
		_ = i
	}
	a.Cases[3].SpeechSamples = 0
	a.Cases[3].RMS = 0
	cases[3].quality = "silence"
	r, e := analyzeStudy(m, a, cases, 65)
	if e != nil || len(r) != 4 {
		t.Fatal(len(r), e)
	}
	for _, v := range r {
		if len(v.Rules) != 4 || len(v.Top1) != 2 {
			t.Fatal(v)
		}
		for _, rule := range v.Rules {
			if rule.Status != "evaluated" || len(rule.Known) != 2 || len(rule.Unknown) != 2 {
				t.Fatal(rule)
			}
			if rule.Known[0].Total != 100 || rule.Known[0].Rejected != 1 || rule.Known[1].Total != 100 || rule.Unknown[0].Total != 10 || rule.Unknown[1].Total != 10 {
				t.Fatal(rule)
			}
			if rule.DifferentPairs != 180 {
				t.Fatal(rule.DifferentPairs)
			}
		}
		if v.Top1[0].QualityRejected != 1 || v.Top1[0].Correct != 99 || v.Top1[0].Total != 100 {
			t.Fatal(v.Top1)
		}
	}
}
func TestStudyUnknownUsesOneFixedGroup(t *testing.T) {
	groups := make([]string, 50)
	templates := map[string][]float32{}
	for i := range groups {
		groups[i] = fmt.Sprintf("fixture_%d", i+1)
		v := make([]float32, 50)
		v[i] = 1
		templates[groups[i]] = v
	}
	v := make([]float32, 50)
	v[0] = 1
	cases := []embeddedCase{{fixtureCase: fixtureCase{Speaker: "unknown_1"}, vector: v, quality: "qualified"}, {fixtureCase: fixtureCase{Speaker: "unknown_2"}, vector: v, quality: "qualified"}, {fixtureCase: fixtureCase{Speaker: "unknown_3"}, quality: "rejected"}}
	r := scoreStudyUnknown(cases, groups, templates, 5, .5, .03)
	if r.Total != 3 || r.FalseAccept != 1 || r.Rejected != 2 || r.QualifiedTotal != 2 || len(r.Groups) != 5 || r.Groups[0].Total != 1 || r.Groups[1].Total != 1 || r.Groups[2].Total != 1 {
		t.Fatal(r)
	}
	r = scoreStudyUnknown(cases, groups, templates, 50, .5, .03)
	if r.Total != 3 || r.FalseAccept != 2 || r.Rejected != 1 {
		t.Fatal(r)
	}
}
func TestStudyExtractionUnionKeepsEnergyAndVAD(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "quiet.wav")
	b := wavBytes(32000)
	os.WriteFile(p, b, 0600)
	h := sha256.Sum256(b)
	c := evaluationCase{ID: "one", Speaker: "fixture_1", Split: "evaluation", Path: p, Samples: 32000, SHA: hex.EncodeToString(h[:])}
	socketDir := filepath.Join(dir, "worker")
	os.Mkdir(socketDir, 0700)
	socket := filepath.Join(socketDir, "worker.sock")
	start := func(ctx context.Context, _, _ string) (*workerProcess, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLargeExtractionHelper$")
		cmd.Env = append(os.Environ(), "SPEAKER_LAB_LARGE_HELPER=success", "SPEAKER_LAB_HELPER_SOCKET="+socket)
		w, e := launchWorker(ctx, cmd, socket, testSHA, time.Second)
		if e == nil {
			w.cleanup = func() { cleanupWorkerDirectory(socketDir, socket, w.socketIdentity) }
		}
		return w, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m := evaluationManifest{Cases: []evaluationCase{c, c}}
	calls := 0
	ex, e := extractEvaluationWithGate(ctx, m, "unused", testSHA, start, func(c evaluationCase, pcm []byte) bool { calls++; return calls == 1 })
	if e != nil || ex.qualified != 1 || ex.silence != 1 || len(ex.cases) != 2 || ex.cases[0].quality != "silence" || len(ex.cases[0].vector) != 2 || len(ex.cases[1].vector) != 0 {
		t.Fatal(ex, e)
	}
	v := ex.cases[0].vector
	ex.clearVectors()
	if !zeroFloats(v) {
		t.Fatal("union vector retained")
	}
}
func zeroFloats(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

func TestStudyQualityViewsAndCalibrationAreIndependent(t *testing.T) {
	m, a := studyFixture(t)
	var cases []embeddedCase
	for _, c := range m.Cases {
		v := make([]float32, 65)
		group, n := studySpeaker(c.Speaker)
		offset := 0
		if group == "calibration" {
			offset = 50
		}
		if group == "unknown" {
			offset = 60
		}
		v[offset+n-1] = 1
		cases = append(cases, embeddedCase{fixtureCase: c.fixture(), vector: v, quality: "qualified"})
	}
	// The first query has speech despite low energy; the other has energy but no
	// estimated speech. Both embeddings exist under the extraction union.
	a.Cases[3].RMS = 0
	a.Cases[4].SpeechSamples = 0
	cases[3].vector = append([]float32(nil), cases[8].vector...)
	first, e := analyzeStudy(m, a, cases, 65)
	if e != nil {
		t.Fatal(e)
	}
	for _, view := range first {
		if view.Top1[0].QualityRejected != 1 {
			t.Fatal(view)
		}
		want := 99
		if view.Quality == "vad" {
			want = 98
		}
		if view.Top1[0].Correct != want {
			t.Fatal("quality views merged", view.Top1)
		}
	}
	for i, c := range m.Cases {
		if c.Role == "known_evaluation" || c.Role == "unknown_evaluation" {
			cases[i].vector = append([]float32(nil), cases[0].vector...)
		}
	}
	second, e := analyzeStudy(m, a, cases, 65)
	if e != nil {
		t.Fatal(e)
	}
	for i := range first {
		for j := range first[i].Rules {
			if first[i].Rules[j].Threshold != second[i].Rules[j].Threshold || first[i].Rules[j].DifferentPairs != second[i].Rules[j].DifferentPairs {
				t.Fatal("evaluation fitted threshold")
			}
		}
	}
	// Tied calibration impostors at 1 cannot be excluded by a threshold in [-1,1].
	for i, c := range m.Cases {
		if c.Role == "calibration_enrollment" || c.Role == "calibration_query" {
			cases[i].vector = append([]float32(nil), cases[0].vector...)
		}
	}
	unreachable, e := analyzeStudy(m, a, cases, 65)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range unreachable {
		for _, r := range v.Rules[2:] {
			if r.Status != "not_run" || r.CalibrationStatus != "unreachable" {
				t.Fatal(r)
			}
		}
	}
}

func TestStudyCalibrationRetainsQualityDenominator(t *testing.T) {
	m, a := studyFixture(t)
	var cases []embeddedCase
	for _, c := range m.Cases {
		v := make([]float32, 65)
		group, n := studySpeaker(c.Speaker)
		offset := 0
		if group == "calibration" {
			offset = 50
		}
		if group == "unknown" {
			offset = 60
		}
		v[offset+n-1] = 1
		cases = append(cases, embeddedCase{fixtureCase: c.fixture(), vector: v, quality: "qualified"})
	}
	a.Cases[253].SpeechSamples = 0
	views, e := analyzeStudy(m, a, cases, 65)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range views {
		if v.CalibrationQueries != 20 {
			t.Fatal("calibration denominator lost", v.CalibrationQueries)
		}
	}
}

func TestStudyUnreachableUnknownDenominators(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed_%v", mixed), func(t *testing.T) {
			m, a := studyFixture(t)
			var cases []embeddedCase
			for i, c := range m.Cases {
				v := make([]float32, 65)
				group, n := studySpeaker(c.Speaker)
				offset := 0
				if group == "calibration" {
					offset = 50
				}
				if group == "unknown" {
					offset = 60
				}
				v[offset+n-1] = 1
				if group == "calibration" {
					clear(v)
					v[50] = 1
				}
				cases = append(cases, embeddedCase{fixtureCase: c.fixture(), vector: v, quality: "qualified"})
				if mixed && group == "unknown" && i%2 == 0 {
					a.Cases[i].RMS = 0
					a.Cases[i].SpeechSamples = 0
				}
			}
			views, e := analyzeStudy(m, a, cases, 65)
			if e != nil {
				t.Fatal(e)
			}
			qualified := 10
			if mixed {
				qualified = 5
			}
			for _, view := range views {
				for _, r := range view.Rules[2:] {
					if r.Status != "not_run" || r.CalibrationStatus != "unreachable" {
						t.Fatal(r)
					}
					for _, p := range r.Unknown {
						if p.Status != "not_run" || p.Total != 10 || p.QualifiedTotal != qualified || p.FalseAccept != 0 || p.Rejected != 0 {
							t.Fatalf("unscored denominators %+v", p)
						}
						if p.Candidates == 5 {
							if len(p.Groups) != 5 {
								t.Fatal("missing fixed groups")
							}
							total, q := 0, 0
							for i, g := range p.Groups {
								wantQ := 2
								if mixed {
									wantQ = 1
								}
								if g.Group != i+1 || g.Total != 2 || g.QualifiedTotal != wantQ || g.Status != "not_run" || g.FalseAccept != 0 || g.Rejected != 0 {
									t.Fatalf("unscored group %+v", g)
								}
								total += g.Total
								q += g.QualifiedTotal
							}
							if total != p.Total || q != p.QualifiedTotal {
								t.Fatal("group denominator inflation")
							}
						}
					}
				}
				// Fixed policies still score exactly one decision per clip, including quality
				// rejects; grouping must preserve totals when calibration policies cannot run.
				for _, r := range view.Rules[:2] {
					for _, p := range r.Unknown {
						if p.Status != "evaluated" || p.Total != 10 || p.QualifiedTotal != qualified || p.FalseAccept+p.Rejected != 10 {
							t.Fatal("evaluated denominator inflation", p)
						}
						if p.Candidates == 5 {
							total, q, decisions := 0, 0, 0
							for _, g := range p.Groups {
								total += g.Total
								q += g.QualifiedTotal
								decisions += g.FalseAccept + g.Rejected
							}
							if total != 10 || q != qualified || decisions != 10 {
								t.Fatal("evaluated group inflation")
							}
						}
					}
				}
			}
		})
	}
}
func TestStudyObservationScopeKeepsLegacyMetadata(t *testing.T) {
	r, _ := runEnrollmentStudy(context.Background(), "/missing-model", testSHA, "/missing-manifest", "/missing-audit")
	if r.OpenSetAssessment != "limited_public_unknown_cohort" || r.HumanAcceptance || r.ProductionAcceptance || r.FeatureEnabled {
		t.Fatal("study observation scope", r.evaluationMetadata)
	}
	for _, m := range []evaluationMetadata{newEvaluationReport(testSHA).evaluationMetadata, newLargeEvaluationReport(testSHA).evaluationMetadata} {
		if m.OpenSetAssessment != "unavailable" || m.HumanAcceptance || m.ProductionAcceptance || m.FeatureEnabled {
			t.Fatal("legacy metadata changed", m)
		}
	}
}
