//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func evidenceTraining() []evidenceLabeledVector {
	var v []evidenceLabeledVector
	for i := 0; i < 3; i++ {
		for j := 0; j < 2; j++ {
			x := make([]float32, 3)
			x[i] = float32(j + 1)
			v = append(v, evidenceLabeledVector{Speaker: fmt.Sprint(i), ID: fmt.Sprintf("c%d%d", i, j), Vector: x})
		}
	}
	return v
}
func TestEvidencePolicyFitsFloorsAndCounts(t *testing.T) {
	v := evidenceTraining()
	p, e := fitEvidencePolicy(v, v, 3)
	if e != nil || p.DiagnosticStatus != "fitted" || p.ContinuityStatus != "fitted" || p.DiagnosticFloor != 1 || p.ContinuityFloor != math.Nextafter(0, math.Inf(1)) || p.EnrollmentPeople != 3 || p.QueryPeople != 3 || p.EnrollmentSamePairs != 3 || p.QuerySamePairs != 3 || p.QueryDifferentPairs != 12 || p.QuerySamePassing != 3 {
		t.Fatal(p, e)
	}
}
func TestEvidencePolicyNearestRankNotMinimum(t *testing.T) {
	var v []evidenceLabeledVector
	for i := 0; i < 21; i++ {
		c := -1 + float64(i)/10
		v = append(v, evidenceLabeledVector{fmt.Sprint(i), fmt.Sprintf("a%d", i), []float32{1, 0, 0}}, evidenceLabeledVector{fmt.Sprint(i), fmt.Sprintf("b%d", i), []float32{float32(c), float32(math.Sqrt(1 - c*c)), 0}})
	}
	p, e := fitEvidencePolicy(v, evidenceTraining(), 3)
	if e != nil || p.EnrollmentSamePairs != 21 || math.IsNaN(p.DiagnosticFloor) || math.IsInf(p.DiagnosticFloor, 0) || math.Abs(p.DiagnosticFloor+.9) > 1e-6 {
		t.Fatal(p, e)
	}
}
func TestEvidencePolicyInsufficientIndependentAndNoSeparation(t *testing.T) {
	v := evidenceTraining()
	for _, tc := range []struct {
		name   string
		en, q  []evidenceLabeledVector
		ds, cs string
	}{
		{"both", nil, nil, "disabled_insufficient_training", "disabled_insufficient_training"},
		{"enrollment", v[:4], v, "disabled_insufficient_training", "fitted"},
		{"queries", v, v[:4], "fitted", "disabled_insufficient_training"},
		{"one segment", v[:5], v, "disabled_insufficient_training", "fitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, e := fitEvidencePolicy(tc.en, tc.q, 3)
			if e != nil || p.DiagnosticStatus != tc.ds || p.ContinuityStatus != tc.cs {
				t.Fatal(p, e)
			}
		})
	}
	for i := range v {
		v[i].Vector = []float32{1, 0, 0}
	}
	p, e := fitEvidencePolicy(v, v, 3)
	if e != nil || p.ContinuityStatus != "disabled_no_safe_threshold" || math.IsNaN(p.ContinuityFloor) || math.IsInf(p.ContinuityFloor, 0) || p.ContinuityFloor > 1 || p.QuerySamePassing != 0 {
		t.Fatal(p, e)
	}
}
func TestEvidencePolicyRejectsInvalidEvenWhenInsufficient(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    []evidenceLabeledVector
		dim  int
	}{
		{"dimension", nil, 0}, {"large dimension", nil, maxDimension + 1},
		{"nan", []evidenceLabeledVector{{"s", "id", []float32{float32(math.NaN()), 1}}}, 2},
		{"inf", []evidenceLabeledVector{{"s", "id", []float32{float32(math.Inf(1)), 1}}}, 2},
		{"zero", []evidenceLabeledVector{{"s", "id", []float32{0, 0}}}, 2},
		{"short", []evidenceLabeledVector{{"s", "id", []float32{1}}}, 2},
		{"speaker", []evidenceLabeledVector{{"", "id", []float32{1, 0}}}, 2},
		{"id", []evidenceLabeledVector{{"s", "", []float32{1, 0}}}, 2},
		{"duplicate", []evidenceLabeledVector{{"s", "id", []float32{1, 0}}, {"s", "id", []float32{1, 0}}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, query := range []bool{false, true} {
				var e error
				if query {
					_, e = fitEvidencePolicy(nil, tc.v, tc.dim)
				} else {
					_, e = fitEvidencePolicy(tc.v, nil, tc.dim)
				}
				if !errors.Is(e, errProtocol) {
					t.Fatal(e)
				}
			}
		})
	}
}
func TestEvidenceDiagnosticMedianSuspectsAndNoMutation(t *testing.T) {
	clips := []evidenceLabeledVector{{"s", "good1", []float32{10, 0}}, {"s", "good2", []float32{1, 0}}, {"s", "good3", []float32{1, 0}}, {"s", "suspect", []float32{0, 2}}}
	before := make([][]float32, len(clips))
	for i := range clips {
		before[i] = append([]float32(nil), clips[i].Vector...)
	}
	d, e := diagnoseEvidenceEnrollment("s", clips, 2, evidencePolicy{DiagnosticStatus: "fitted", DiagnosticFloor: .5})
	if e != nil || d.Status != "diagnosed" || d.Recommendation != "review_and_rerecord_suspect_segments" || d.Cases != 4 || !reflect.DeepEqual(d.SuspectCaseIDs, []string{"suspect"}) || !reflect.DeepEqual(d.SegmentMedianSimilarities, []float64{1, 1, 1, 0}) {
		t.Fatal(d, e)
	}
	for i := range clips {
		if !reflect.DeepEqual(before[i], clips[i].Vector) {
			t.Fatal("mutated input vectors")
		}
	}
	// With two comparisons, the middle pair must be averaged.
	d, e = diagnoseEvidenceEnrollment("s", clips[:3:3], 2, evidencePolicy{DiagnosticStatus: "fitted", DiagnosticFloor: 1})
	if e != nil || d.Recommendation != "no_consistency_outlier_detected" {
		t.Fatal(d, e)
	}
	d, e = diagnoseEvidenceEnrollment("s", []evidenceLabeledVector{clips[0], clips[1], clips[3]}, 2, evidencePolicy{DiagnosticStatus: "fitted", DiagnosticFloor: .6})
	if e != nil || !reflect.DeepEqual(d.SegmentMedianSimilarities, []float64{.5, .5, 0}) {
		t.Fatal(d, e)
	}
}
func TestEvidenceDiagnosticUnavailableAndProtocol(t *testing.T) {
	clips := []evidenceLabeledVector{{"s", "one", []float32{1, 0}}}
	for _, p := range []evidencePolicy{{}, {DiagnosticStatus: "fitted", DiagnosticFloor: .5}} {
		d, e := diagnoseEvidenceEnrollment("s", clips, 2, p)
		if e != nil || d.Status != "diagnostic_unavailable" || d.Recommendation != "diagnostic_unavailable" || len(d.SegmentMedianSimilarities) != 0 {
			t.Fatal(d, e)
		}
	}
	for _, p := range []evidencePolicy{{DiagnosticStatus: "fitted", DiagnosticFloor: math.NaN()}, {DiagnosticStatus: "fitted", DiagnosticFloor: 2}} {
		if _, e := diagnoseEvidenceEnrollment("s", clips, 2, p); !errors.Is(e, errProtocol) {
			t.Fatal(e)
		}
	}
	for _, tc := range []struct {
		s   string
		v   []evidenceLabeledVector
		dim int
	}{{"other", clips, 2}, {"", clips, 2}, {"s", clips, 3}, {"s", []evidenceLabeledVector{{"s", "bad", []float32{float32(math.NaN()), 0}}}, 2}} {
		if _, e := diagnoseEvidenceEnrollment(tc.s, tc.v, tc.dim, evidencePolicy{}); !errors.Is(e, errProtocol) {
			t.Fatal(e)
		}
	}
}
func TestEvidenceVectorsNeverJSON(t *testing.T) {
	for _, v := range []any{evidenceLabeledVector{Speaker: "s", ID: "i", Vector: []float32{.1234567}}, evidenceChunk{Vector: []float32{.1234567}}, evidenceAccumulated{Vector: []float32{.1234567}}} {
		b, e := json.Marshal(v)
		if e != nil || strings.Contains(strings.ToLower(string(b)), "vector") || strings.Contains(string(b), "1234567") {
			t.Fatal(string(b), e)
		}
	}
}

func TestEvidencePolicyTrainingOwnershipAndPassingCount(t *testing.T) {
	v := evidenceTraining()
	v[1].Vector = []float32{-1, 0, 0}
	before := make([][]float32, len(v))
	for i := range v {
		before[i] = append([]float32(nil), v[i].Vector...)
	}
	p, e := fitEvidencePolicy(v, v, 3)
	if e != nil || p.QuerySamePassing != 2 || p.ContinuityFloor != math.Nextafter(0, math.Inf(1)) || p.DiagnosticFloor != -1 {
		t.Fatal(p, e)
	}
	for i := range v {
		if !reflect.DeepEqual(before[i], v[i].Vector) {
			t.Fatal("training vector mutated")
		}
	}
}
