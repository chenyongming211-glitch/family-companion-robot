//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func evaluationFixture() evaluationManifest {
	m := evaluationManifest{SourceKind: "public_human", SourceID: "public_corpus", SourceURL: "https://example.org/corpus", LicenseURL: "https://example.org/license", SingleSpeakerBasis: "source_annotations", SampleRate: 16000}
	for s := 0; s < 3; s++ {
		for n, split := range []string{"enrollment", "calibration", "calibration", "evaluation"} {
			digest := func(kind string) string {
				h := sha256.Sum256([]byte(fmt.Sprintf("%s_%d_%d", kind, s, n)))
				return hex.EncodeToString(h[:])
			}
			m.Cases = append(m.Cases, evaluationCase{ID: fmt.Sprintf("case_%d_%d", s, n), Speaker: fmt.Sprintf("fixture_%d", s+1), Split: split, Path: fmt.Sprintf("/private/corpus/%d_%d.wav", s, n), Samples: 32000, SHA: digest("wav"), RecordingSHA: digest("recording"), TextSHA: digest("text")})
		}
	}
	return m
}
func evaluationVectors() []embeddedCase {
	var out []embeddedCase
	for _, c := range evaluationFixture().Cases {
		v := make([]float32, 3)
		v[int(c.Speaker[len(c.Speaker)-1]-'1')] = 1
		out = append(out, embeddedCase{fixtureCase: c.fixture(), vector: v, quality: "qualified"})
	}
	return out
}
func TestEvaluationManifestStrictValidation(t *testing.T) {
	original, _ := json.Marshal(evaluationFixture())
	p := filepath.Join(t.TempDir(), "manifest.json")
	write := func(b []byte) {
		t.Helper()
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(original)
	if _, digest, e := loadEvaluationManifest(p); e != nil || len(digest) != 64 {
		t.Fatalf("valid manifest: %v", e)
	}
	mutations := map[string]func(map[string]any){
		"synthetic":        func(m map[string]any) { m["source_kind"] = "synthetic_tts" },
		"source token":     func(m map[string]any) { m["source_id"] = "corpus name" },
		"http":             func(m map[string]any) { m["source_url"] = "http://example.org" },
		"credentials":      func(m map[string]any) { m["license_url"] = "https://user:pass@example.org/license" },
		"relative URL":     func(m map[string]any) { m["source_url"] = "/corpus" },
		"missing host":     func(m map[string]any) { m["source_url"] = "https:///corpus" },
		"basis":            func(m map[string]any) { m["single_speaker_basis"] = "declared" },
		"rate":             func(m map[string]any) { m["sample_rate"] = 8000 },
		"unknown":          func(m map[string]any) { m["extra"] = false },
		"missing":          func(m map[string]any) { delete(m, "license_url") },
		"null":             func(m map[string]any) { m["source_id"] = nil },
		"too few speakers": func(m map[string]any) { m["cases"] = m["cases"].([]any)[:8] },
		"too many cases": func(m map[string]any) {
			for len(m["cases"].([]any)) < 129 {
				m["cases"] = append(m["cases"].([]any), m["cases"].([]any)[0])
			}
		},
	}
	caseMutations := map[string]func(map[string]any, []any){
		"speaker":              func(c map[string]any, _ []any) { c["speaker"] = "real_name" },
		"split":                func(c map[string]any, _ []any) { c["split"] = "holdout" },
		"missing case field":   func(c map[string]any, _ []any) { delete(c, "recording_sha256") },
		"null case field":      func(c map[string]any, _ []any) { c["text_sha256"] = nil },
		"unknown case field":   func(c map[string]any, _ []any) { c["extra"] = 0 },
		"relative path":        func(c map[string]any, _ []any) { c["path"] = "one.wav" },
		"unclean path":         func(c map[string]any, _ []any) { c["path"] = "/private/../one.wav" },
		"oversize clip":        func(c map[string]any, _ []any) { c["samples"] = 128001 },
		"zero samples":         func(c map[string]any, _ []any) { c["samples"] = 0 },
		"bad digest":           func(c map[string]any, _ []any) { c["sha256"] = "invalid" },
		"bad recording digest": func(c map[string]any, _ []any) { c["recording_sha256"] = "invalid" },
		"bad text digest":      func(c map[string]any, _ []any) { c["text_sha256"] = "invalid" },
		"id duplicate":         func(c map[string]any, a []any) { c["id"] = a[1].(map[string]any)["id"] },
		"path duplicate":       func(c map[string]any, a []any) { c["path"] = a[1].(map[string]any)["path"] },
		"audio duplicate":      func(c map[string]any, a []any) { c["sha256"] = a[4].(map[string]any)["sha256"] },
		"audio leak":           func(c map[string]any, a []any) { c["sha256"] = a[1].(map[string]any)["sha256"] },
		"recording leak":       func(c map[string]any, a []any) { c["recording_sha256"] = a[1].(map[string]any)["recording_sha256"] },
		"text leak":            func(c map[string]any, a []any) { c["text_sha256"] = a[1].(map[string]any)["text_sha256"] },
		"missing enrollment":   func(c map[string]any, _ []any) { c["split"] = "calibration" },
	}
	for name, mutate := range caseMutations {
		mutations[name] = func(m map[string]any) { a := m["cases"].([]any); mutate(a[0].(map[string]any), a) }
	}
	mutations["missing calibration"] = func(m map[string]any) { m["cases"].([]any)[1].(map[string]any)["split"] = "evaluation" }
	mutations["missing evaluation"] = func(m map[string]any) { m["cases"].([]any)[3].(map[string]any)["split"] = "enrollment" }
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			json.Unmarshal(original, &m)
			mutate(m)
			b, _ := json.Marshal(m)
			write(b)
			if _, _, e := loadEvaluationManifest(p); e == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	for name, b := range map[string][]byte{
		"duplicate header": bytes.Replace(original, []byte(`"source_kind":`), []byte(`"source_id":"other","source_kind":`), 1),
		"duplicate case":   bytes.Replace(original, []byte(`"recording_sha256":`), []byte(`"id":"duplicate","recording_sha256":`), 1),
		"trailing":         append(append([]byte(nil), original...), []byte(` {}`)...),
		"oversize":         bytes.Repeat([]byte(" "), maxManifestBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			write(b)
			if _, _, e := loadEvaluationManifest(p); e == nil {
				t.Fatal("accepted invalid JSON")
			}
		})
	}
	// Repeated text/recording inside one split is allowed, but not repeated audio.
	m := evaluationFixture()
	m.Cases[2].RecordingSHA = m.Cases[1].RecordingSHA
	m.Cases[2].TextSHA = m.Cases[1].TextSHA
	b, _ := json.Marshal(m)
	write(b)
	if _, _, e := loadEvaluationManifest(p); e != nil {
		t.Fatal("same-split annotations rejected", e)
	}
}
func TestEvaluationAllCalibrationAndFrozenThreshold(t *testing.T) {
	a := evaluationVectors()
	r, e := analyzeEvaluation(a, 3)
	if e != nil || r.Calibration.Status != "separable" || r.Calibration.FittedSpeakers != 3 || r.Calibration.FittedCases != 6 || r.Calibration.SamePairs != 3 || r.Calibration.DifferentPairs != 12 || r.Evaluation.Correct != 3 || r.Evaluation.Total != 3 || r.Evaluation.EffectiveRate != 1 || r.Evaluation.Accuracy != 1 {
		t.Fatalf("unexpected result %+v %v", r, e)
	}
	frozen := r.Calibration
	a[3].vector = []float32{0, 1, 0}
	a[7].vector = []float32{1, 1, 1}
	a[11].quality = "short"
	a[11].vector = nil
	r, e = analyzeEvaluation(a, 3)
	if e != nil || r.Calibration != frozen || r.Evaluation.Wrong != 1 || r.Evaluation.Rejected != 2 || r.Evaluation.Correct != 0 || r.Evaluation.Total != 3 {
		t.Fatalf("evaluation leaked into fitting or bad scoring %+v %v", r, e)
	}
	// Extra calibration clips are not truncated to two per person.
	a = evaluationVectors()
	extra := a[1]
	extra.vector = []float32{0, 1, 0}
	a = append(a, extra)
	r, e = analyzeEvaluation(a, 3)
	if e != nil || r.Calibration.FittedCases != 7 || r.Calibration.SamePairs != 5 || r.Calibration.DifferentPairs != 16 || r.Calibration.Status != "failed_nonseparable" {
		t.Fatalf("extra calibration ignored %+v %v", r, e)
	}
	// The third speaker calibration must affect fitting; old calibrate ignores it.
	a = evaluationVectors()
	a[9].vector = []float32{1, 0, 0}
	r, e = analyzeEvaluation(a, 3)
	if e != nil || r.Calibration.Status != "failed_nonseparable" || r.Evaluation.Status != "not_run" || r.Evaluation.Correct != 0 {
		t.Fatalf("nonseparable calibration scored %+v %v", r, e)
	}
}
func TestEvaluationEnrollmentAndInvalidVectors(t *testing.T) {
	a := evaluationVectors()
	a[0].quality = "silence"
	a[0].vector = nil
	r, e := analyzeEvaluation(a, 3)
	if e != nil || r.Evaluation.Status != "not_run" || r.Calibration.Status != "failed_missing_enrollment" {
		t.Fatalf("missing enrollment accepted %+v %v", r, e)
	}
	a = evaluationVectors()
	a[1].quality = "short"
	a[1].vector = nil
	r, e = analyzeEvaluation(a, 3)
	if e != nil || r.Calibration.Status != "failed_insufficient_calibration" || r.Evaluation.Status != "not_run" {
		t.Fatalf("insufficient calibration accepted %+v %v", r, e)
	}
	for _, v := range [][]float32{nil, {0, 0, 0}, {1, 0}, {float32(math.NaN()), 0, 0}, {float32(math.Inf(1)), 0, 0}} {
		a = evaluationVectors()
		a[3].vector = v
		if _, e := analyzeEvaluation(a, 3); e == nil {
			t.Fatal("invalid vector accepted")
		}
	}
	// First qualified enrollment stays frozen even if later enrollment is closer.
	a = evaluationVectors()
	extra := a[0]
	extra.vector = []float32{0, 1, 0}
	a = append(a, extra)
	a[3].vector = []float32{0, 1, 0}
	r, e = analyzeEvaluation(a, 3)
	if e != nil || r.Evaluation.Wrong != 1 {
		t.Fatalf("later enrollment replaced first %+v %v", r, e)
	}
}
func TestEvaluationReportPrivacy(t *testing.T) {
	r := newEvaluationReport(testSHA)
	r.ManifestSHA = strings.Repeat("b", 64)
	var e error
	r.evaluationAnalysis, e = analyzeEvaluation(evaluationVectors(), 3)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"vector", "embedding", "path", "fixture_", "source_url", "license_url", "source_id", "recording_sha256", "text_sha256", "\"cases\":"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("unsafe report contains %s", secret)
		}
	}
	if r.SourceKind != "public_human" || r.HumanAcceptance || r.ProductionAcceptance || r.FeatureEnabled || r.SegmentationAssessment != "unavailable" || r.OverlapAssessment != "unavailable" || r.OpenSetAssessment != "unavailable" {
		t.Fatalf("unsafe acceptance %+v", r)
	}
}
