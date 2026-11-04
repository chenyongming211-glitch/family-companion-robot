//go:build linux

package main

import (
	"encoding/json"
	"math"
	"testing"
)

func closeBackend(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-7 {
		t.Fatalf("got %.12g want %.12g", got, want)
	}
}
func TestBackendASNormPopulationTop10AndFloor(t *testing.T) {
	x := []float64{-100, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s, e := backendStats(x)
	if e != nil {
		t.Fatal(e)
	}
	closeBackend(t, s.Mean, 5.5)
	closeBackend(t, s.Std, math.Sqrt(8.25))
	if s.Floored {
		t.Fatal("unexpected floor")
	}
	s, e = backendStats([]float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	if e != nil || !s.Floored {
		t.Fatal("missing floor", e)
	}
	closeBackend(t, s.Std, 1e-6)
	v, e := backendASNorm(.7, backendDistribution{Mean: .2, Std: .1}, backendDistribution{Mean: .3, Std: .2})
	if e != nil {
		t.Fatal(e)
	}
	closeBackend(t, v, 3.5)
	for _, bad := range [][]float64{{1}, {1, 2, 3, 4, 5, 6, 7, 8, 9, math.NaN()}} {
		if _, e := backendStats(bad); e == nil {
			t.Fatal("invalid stats")
		}
	}
}
func TestBackendGroupsWrapAndCountOnce(t *testing.T) {
	gs := backendGroups(50, 3)
	if len(gs) != 17 || len(gs[16]) != 3 || gs[16][0] != 48 || gs[16][1] != 49 || gs[16][2] != 0 {
		t.Fatal(gs)
	}
	for i := 0; i < 50; i++ {
		if !backendContains(gs[i/3], i) {
			t.Fatal("own group missing", i)
		}
	}
}
func TestBackendTiedAndSingleCandidate(t *testing.T) {
	q := backendQuery{Qualified: true, Own: 0, Scores: []float64{.8, .8}}
	if backendAccept(q, 0, 0) {
		t.Fatal("tie accepted")
	}
	q.Scores = []float64{.8}
	if !backendAccept(q, .8, 100) {
		t.Fatal("single candidate margin")
	}
	q.Qualified = false
	if backendAccept(q, 0, 0) {
		t.Fatal("quality accepted")
	}
}
func TestBackendThresholdBudgetWrongAndTieObjective(t *testing.T) {
	known := []backendQuery{{Qualified: true, Own: 0, Scores: []float64{.8, .3}}, {Qualified: true, Own: 0, Scores: []float64{.2, .7}}, {Own: 0}}
	unknown := []backendQuery{{Qualified: true, Own: -1, Scores: []float64{.7, .1}}, {Own: -1}}
	p := backendCalibrate(known, unknown, []float64{0, .1}, .01)
	if p.Status != "calibrated" || p.Budget != 0 || p.Validation.Correct != 1 || p.Validation.Wrong != 0 || p.Validation.UnknownAccepted != 0 {
		t.Fatalf("%+v", p)
	}
	if p.Threshold != .8 || p.Margin != .1 {
		t.Fatalf("tie objective %+v", p)
	}
	p = backendCalibrate(nil, nil, []float64{0}, .01)
	if p.Status == "calibrated" {
		t.Fatal("empty fit")
	}
	c := backendCount(known, unknown, 0, 0, false)
	if c.Total != 3 || c.Qualified != 2 || c.UnknownTotal != 2 || c.UnknownQualified != 1 || c.Correct+c.Wrong+c.Rejected != 0 {
		t.Fatal(c)
	}
	c = backendCount(known, unknown, .8, 0, true)
	if c.Correct != 1 || c.Rejected != 2 || c.UnknownRejected != 2 {
		t.Fatal(c)
	}
}
func TestBackendQMFTrainOnlyStableConvergenceAndFailure(t *testing.T) {
	for _, x := range []float64{-1000, 0, 1000} {
		v := backendSoftplus(x)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatal(v)
		}
	}
	closeBackend(t, backendSoftplus(1000), 1000)
	pairs := []backendPair{{Score: 2, Duration: 1, Same: true}, {Score: 3, Duration: 2, Same: true}, {Score: -2, Duration: 1}, {Score: -3, Duration: 2}}
	fit := backendFit(pairs)
	if fit.Status != "fitted" || fit.Weights[0] <= 0 || fit.Iterations < 1 || fit.ObjectiveFinal >= fit.ObjectiveInitial {
		t.Fatalf("%+v", fit)
	}
	closeBackend(t, fit.Mean[0], 0)
	closeBackend(t, fit.Mean[1], 1.5)
	a := backendTransform(fit, 2, 1)
	b := backendTransform(fit, -2, 1)
	if a <= b {
		t.Fatal("rank changed")
	}
	bad := backendFit([]backendPair{{Score: 1, Same: true}})
	if bad.Status == "fitted" {
		t.Fatal("one class fit")
	}
	bad = backendFit([]backendPair{{Score: -2, Same: true}, {Score: 2}})
	if bad.Status == "fitted" {
		t.Fatal("negative slope fit")
	}
	bad = backendFit([]backendPair{{Score: math.Inf(1), Same: true}, {Score: 2}})
	if bad.Status == "fitted" {
		t.Fatal("nonfinite fit")
	}
}
func TestBackendPairedROCInterpolatedEER(t *testing.T) {
	r := backendROC([]float64{.9, .8}, []float64{.2, .1})
	if r.Status != "diagnostic" || r.PositivePairs != 2 || r.NegativePairs != 2 || !r.PairedObservations {
		t.Fatal(r)
	}
	closeBackend(t, r.EER, 0)
	closeBackend(t, r.MinDCF, 0)
	r = backendROC([]float64{.5}, []float64{.5})
	closeBackend(t, r.EER, .5)
	closeBackend(t, r.MinDCF, 1)
}
func TestBackendSingleCandidateCalibrationReportsZeroMarginAfterQualityFailure(t *testing.T) {
	k := []backendQuery{{Own: 0}, {Qualified: true, Own: 0, Scores: []float64{.9}}}
	u := []backendQuery{{Qualified: true, Own: -1, Scores: []float64{.1}}}
	p := backendCalibrate(k, u, []float64{0, 1}, .01)
	if p.Status != "calibrated" || p.Margin != 0 || p.Validation.Correct != 1 {
		t.Fatal(p)
	}
}
func TestBackendThresholdUsesNextafterBoundaryAndBudgetFivePercent(t *testing.T) {
	k := []backendQuery{{Qualified: true, Own: 0, Scores: []float64{.8, .1}}}
	u := make([]backendQuery, 25)
	for i := range u {
		u[i] = backendQuery{Qualified: true, Own: -1, Scores: []float64{.8, .1}}
	}
	p := backendCalibrate(k, u, []float64{0}, .05)
	if p.Budget != 1 || p.Validation.Correct != 0 || p.Validation.UnknownAccepted != 0 || p.Threshold != math.Nextafter(.8, math.Inf(1)) {
		t.Fatal(p)
	}
	u[0].Scores = []float64{.9, .1}
	for i := 1; i < 25; i++ {
		u[i].Scores = []float64{.1, 0}
	}
	p = backendCalibrate(k, u, []float64{0}, .05)
	if p.Budget != 1 || p.Validation.Correct != 1 || p.Validation.UnknownAccepted != 1 {
		t.Fatal(p)
	}
}

func TestBackendFailedFitStillMarshalsFiniteJSON(t *testing.T) {
	p := []backendPair{{Score: math.MaxFloat64, Same: true}, {Score: -math.MaxFloat64}}
	f := backendFit(p)
	if f.Status == "fitted" {
		t.Fatal("extreme invalid stats fitted")
	}
	if _, e := json.Marshal(f); e != nil {
		t.Fatal("nonfinite failed-fit output", e)
	}
}
