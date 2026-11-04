//go:build linux

package main

import (
	"math"
	"sort"
)

type backendDistribution struct {
	Mean    float64 `json:"mean"`
	Std     float64 `json:"std"`
	Floored bool    `json:"floored"`
}
type backendQuery struct {
	Qualified  bool
	Own, Group int
	Speaker    string
	Scores     []float64
}
type backendCounts struct {
	Total            int `json:"known_total"`
	Qualified        int `json:"known_qualified"`
	Correct          int `json:"known_correct"`
	Wrong            int `json:"known_wrong"`
	Rejected         int `json:"known_rejected"`
	UnknownTotal     int `json:"unknown_total"`
	UnknownQualified int `json:"unknown_qualified"`
	UnknownAccepted  int `json:"unknown_false_accept"`
	UnknownRejected  int `json:"unknown_rejected"`
}
type backendPolicy struct {
	Status     string        `json:"status"`
	Threshold  float64       `json:"match_threshold"`
	Margin     float64       `json:"margin"`
	Budget     int           `json:"unknown_budget"`
	Target     float64       `json:"unknown_target"`
	Validation backendCounts `json:"validation"`
}
type backendPair struct {
	Score, Duration float64
	Same            bool
}
type backendFitResult struct {
	Status           string     `json:"status"`
	Weights          [3]float64 `json:"weights_score_duration_intercept"`
	Mean             [2]float64 `json:"training_mean"`
	Std              [2]float64 `json:"training_population_std"`
	StdFloors        int        `json:"std_floor_count"`
	Iterations       int        `json:"iterations"`
	ObjectiveInitial float64    `json:"objective_initial"`
	ObjectiveFinal   float64    `json:"objective_final"`
	SamePairs        int        `json:"same_pairs"`
	DifferentPairs   int        `json:"different_pairs"`
}
type backendROCPoint struct {
	Threshold float64 `json:"threshold"`
	FAR       float64 `json:"false_accept_rate"`
	FRR       float64 `json:"false_reject_rate"`
	TPR       float64 `json:"true_positive_rate"`
}
type backendROCResult struct {
	Status             string            `json:"status"`
	PositivePairs      int               `json:"positive_pairs"`
	NegativePairs      int               `json:"negative_pairs"`
	PairedObservations bool              `json:"paired_not_independent_observations"`
	EER                float64           `json:"linear_interpolated_eer"`
	MinDCF             float64           `json:"normalized_min_dcf_p01_cmiss1_cfa1"`
	MinCost            float64           `json:"minimum_raw_cost_p01_cmiss1_cfa1"`
	Points             []backendROCPoint `json:"roc_det_points"`
}

func backendFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
func backendStats(x []float64) (backendDistribution, error) {
	var r backendDistribution
	if len(x) < 10 {
		return r, errProtocol
	}
	v := append([]float64(nil), x...)
	defer clear(v)
	for _, s := range v {
		if !backendFinite(s) {
			return r, errProtocol
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(v)))
	v = v[:10]
	for _, s := range v {
		r.Mean += s / 10
	}
	for _, s := range v {
		r.Std += (s - r.Mean) * (s - r.Mean) / 10
	}
	r.Std = math.Sqrt(r.Std)
	if r.Std < 1e-6 {
		r.Std = 1e-6
		r.Floored = true
	}
	if !backendFinite(r.Mean) || !backendFinite(r.Std) {
		return backendDistribution{}, errProtocol
	}
	return r, nil
}
func backendASNorm(s float64, a, b backendDistribution) (float64, error) {
	if !backendFinite(s) || !backendFinite(a.Mean) || !backendFinite(b.Mean) || !backendFinite(a.Std) || !backendFinite(b.Std) || a.Std < 1e-6 || b.Std < 1e-6 {
		return 0, errProtocol
	}
	v := .5 * ((s-a.Mean)/a.Std + (s-b.Mean)/b.Std)
	if !backendFinite(v) {
		return 0, errProtocol
	}
	return v, nil
}
func backendGroups(n, k int) [][]int {
	if n < 1 || k < 1 || k > n {
		return nil
	}
	var r [][]int
	for start := 0; start < n; start += k {
		g := make([]int, k)
		for i := range g {
			g[i] = (start + i) % n
		}
		r = append(r, g)
	}
	return r
}
func backendContains(x []int, v int) bool {
	for _, i := range x {
		if i == v {
			return true
		}
	}
	return false
}
func backendBest(q backendQuery) (best, second float64, index int) {
	best, second, index = math.Inf(-1), math.Inf(-1), -1
	for i, s := range q.Scores {
		if !backendFinite(s) {
			return math.Inf(-1), math.Inf(-1), -1
		}
		if s > best {
			second, best, index = best, s, i
		} else if s > second {
			second = s
		}
	}
	return
}
func backendAccept(q backendQuery, t, m float64) bool {
	if !q.Qualified || !backendFinite(t) || !backendFinite(m) {
		return false
	}
	best, second, i := backendBest(q)
	return i >= 0 && best >= t && (len(q.Scores) == 1 || best != second && best-second >= m)
}
func backendCount(k, u []backendQuery, t, m float64, run bool) backendCounts {
	r := backendCounts{Total: len(k), UnknownTotal: len(u)}
	for _, q := range k {
		if q.Qualified {
			r.Qualified++
		}
		if !run {
			continue
		}
		if !backendAccept(q, t, m) {
			r.Rejected++
			continue
		}
		_, _, i := backendBest(q)
		if i == q.Own {
			r.Correct++
		} else {
			r.Wrong++
		}
	}
	for _, q := range u {
		if q.Qualified {
			r.UnknownQualified++
		}
		if !run {
			continue
		}
		if backendAccept(q, t, m) {
			r.UnknownAccepted++
		} else {
			r.UnknownRejected++
		}
	}
	return r
}
func backendCalibrate(k, u []backendQuery, margins []float64, target float64) backendPolicy {
	r := backendPolicy{Status: "failed_insufficient_validation", Target: target, Validation: backendCount(k, u, 0, 0, false)}
	if len(k) == 0 || len(u) == 0 || !backendFinite(target) || target < 0 || target > 1 {
		return r
	}
	r.Budget = int(math.Floor(float64(len(u)) * target))
	var thresholds []float64
	for _, qs := range [][]backendQuery{k, u} {
		for _, q := range qs {
			if !q.Qualified {
				continue
			}
			b, _, i := backendBest(q)
			if i < 0 {
				r.Status = "failed_invalid_scores"
				return r
			}
			thresholds = append(thresholds, b, math.Nextafter(b, math.Inf(1)))
		}
	}
	if len(thresholds) == 0 {
		r.Status = "failed_no_qualified_validation"
		return r
	}
	sort.Float64s(thresholds)
	thresholds = append(thresholds, math.Nextafter(thresholds[0], math.Inf(-1)))
	bestSet := false
	single := false
	for _, qs := range [][]backendQuery{k, u} {
		for _, q := range qs {
			if q.Qualified && len(q.Scores) == 1 {
				single = true
			}
		}
	}
	for _, m := range margins {
		if !backendFinite(m) || m < 0 {
			continue
		}
		if single {
			m = 0
		}
		for _, t := range thresholds {
			if !backendFinite(t) {
				continue
			}
			c := backendCount(k, u, t, m, true)
			if c.Wrong > 0 || c.UnknownAccepted > r.Budget {
				continue
			}
			if !bestSet || c.Correct > r.Validation.Correct || c.Correct == r.Validation.Correct && (c.UnknownAccepted < r.Validation.UnknownAccepted || c.UnknownAccepted == r.Validation.UnknownAccepted && (t > r.Threshold || t == r.Threshold && m > r.Margin)) {
				r.Status = "calibrated"
				r.Threshold, r.Margin, r.Validation = t, m, c
				bestSet = true
			}
		}
	}
	return r
}
func backendSoftplus(x float64) float64 {
	if x > 0 {
		return x + math.Log1p(math.Exp(-x))
	}
	return math.Log1p(math.Exp(x))
}
func backendTransform(f backendFitResult, s, d float64) float64 {
	return f.Weights[0]*(s-f.Mean[0])/f.Std[0] + f.Weights[1]*(d-f.Mean[1])/f.Std[1] + f.Weights[2]
}

// Full-batch deterministic optimization, with an unpenalized intercept. Class
// masses each equal .5, independently of the number of nontarget pairs.
func backendFit(pairs []backendPair) (result backendFitResult) {
	defer func() {
		// Even a rejected extreme numeric fixture must produce valid JSON.
		finite := true
		for _, v := range result.Mean {
			finite = finite && backendFinite(v)
		}
		for _, v := range result.Std {
			finite = finite && backendFinite(v)
		}
		for _, v := range result.Weights {
			finite = finite && backendFinite(v)
		}
		finite = finite && backendFinite(result.ObjectiveInitial) && backendFinite(result.ObjectiveFinal)
		if !finite {
			result = backendFitResult{Status: "failed_nonfinite_training", SamePairs: result.SamePairs, DifferentPairs: result.DifferentPairs}
		}
	}()

	r := backendFitResult{Status: "failed_invalid_training"}
	if len(pairs) == 0 {
		return r
	}
	for _, p := range pairs {
		if !backendFinite(p.Score) || !backendFinite(p.Duration) {
			return r
		}
		if p.Same {
			r.SamePairs++
		} else {
			r.DifferentPairs++
		}
		r.Mean[0] += p.Score / float64(len(pairs))
		r.Mean[1] += p.Duration / float64(len(pairs))
	}
	if r.SamePairs == 0 || r.DifferentPairs == 0 {
		return r
	}
	for _, p := range pairs {
		r.Std[0] += (p.Score - r.Mean[0]) * (p.Score - r.Mean[0]) / float64(len(pairs))
		r.Std[1] += (p.Duration - r.Mean[1]) * (p.Duration - r.Mean[1]) / float64(len(pairs))
	}
	for i := range r.Std {
		r.Std[i] = math.Sqrt(r.Std[i])
		if r.Std[i] < 1e-6 {
			r.Std[i] = 1e-6
			r.StdFloors++
		}
		if !backendFinite(r.Mean[i]) || !backendFinite(r.Std[i]) {
			return r
		}
	}
	loss := func(w [3]float64) (float64, [3]float64) {
		value := .005 * (w[0]*w[0] + w[1]*w[1])
		grad := [3]float64{.01 * w[0], .01 * w[1], 0}
		for _, p := range pairs {
			x := [3]float64{(p.Score - r.Mean[0]) / r.Std[0], (p.Duration - r.Mean[1]) / r.Std[1], 1}
			z := w[0]*x[0] + w[1]*x[1] + w[2]
			sign, mass := -1., .5/float64(r.DifferentPairs)
			if p.Same {
				sign, mass = 1, .5/float64(r.SamePairs)
			}
			a := -sign * z
			value += mass * backendSoftplus(a)
			sig := 0.
			if a >= 0 {
				sig = 1 / (1 + math.Exp(-a))
			} else {
				v := math.Exp(a)
				sig = v / (1 + v)
			}
			for i := range grad {
				grad[i] += mass * (-sign) * sig * x[i]
			}
		}
		return value, grad
	}
	objective, grad := loss(r.Weights)
	if !backendFinite(objective) {
		return r
	}
	r.ObjectiveInitial, r.ObjectiveFinal = objective, objective
	r.Status = "failed_nonconverged"
	for iteration := 1; iteration <= 2000; iteration++ {
		step := 1.
		accepted := false
		var candidate [3]float64
		var next float64
		var nextGrad [3]float64
		for back := 0; back < 50; back++ {
			for i := range candidate {
				candidate[i] = r.Weights[i] - step*grad[i]
			}
			next, nextGrad = loss(candidate)
			if backendFinite(next) && next <= objective {
				accepted = true
				break
			}
			step *= .5
		}
		if !accepted {
			r.Status = "failed_line_search"
			return r
		}
		delta := objective - next
		r.Weights = candidate
		r.Iterations = iteration
		r.ObjectiveFinal = next
		objective, grad = next, nextGrad
		if delta <= 1e-8 {
			if r.Weights[0] <= 0 {
				r.Status = "failed_nonpositive_score_weight"
			} else {
				r.Status = "fitted"
			}
			return r
		}
	}
	return r
}

// The tied-score sweep includes all-accept and all-reject endpoints. EER is
// interpolated between adjacent empirical ROC/DET points, not a fitted policy.
func backendROC(p, n []float64) backendROCResult {
	r := backendROCResult{Status: "unavailable", PositivePairs: len(p), NegativePairs: len(n), PairedObservations: true}
	if len(p) == 0 || len(n) == 0 {
		return r
	}
	type labeled struct {
		s float64
		p bool
	}
	v := make([]labeled, 0, len(p)+len(n))
	defer clear(v)
	for _, s := range p {
		if !backendFinite(s) {
			return r
		}
		v = append(v, labeled{s, true})
	}
	for _, s := range n {
		if !backendFinite(s) {
			return r
		}
		v = append(v, labeled{s, false})
	}
	sort.Slice(v, func(i, j int) bool { return v[i].s > v[j].s })
	top := math.Nextafter(v[0].s, math.Inf(1))
	if !backendFinite(top) {
		return r
	}
	r.Status = "diagnostic"
	r.MinCost = .01
	r.Points = append(r.Points, backendROCPoint{Threshold: top, FRR: 1})
	tp, fp := 0, 0
	previousDiff := -1.
	previousEER := 1.
	for i := 0; i < len(v); {
		s := v[i].s
		for i < len(v) && v[i].s == s {
			if v[i].p {
				tp++
			} else {
				fp++
			}
			i++
		}
		far, frr := float64(fp)/float64(len(n)), 1-float64(tp)/float64(len(p))
		point := backendROCPoint{Threshold: s, FAR: far, FRR: frr, TPR: 1 - frr}
		r.Points = append(r.Points, point)
		dcf := .01*frr + .99*far
		if dcf < r.MinCost {
			r.MinCost = dcf
		}
		diff := far - frr
		if previousDiff <= 0 && diff >= 0 {
			alpha := -previousDiff / (diff - previousDiff)
			if diff == previousDiff {
				alpha = 0
			}
			r.EER = previousEER + alpha*(frr-previousEER)
		}
		previousDiff, previousEER = diff, frr
	}
	r.MinDCF = r.MinCost / .01
	return r
}
