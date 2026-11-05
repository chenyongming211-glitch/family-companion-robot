//go:build linux

package main

import (
	"math"
	"sort"
)

// Labels belong to training and scoring only, never to the accumulator.
type evidenceLabeledVector struct {
	Speaker string    `json:"speaker"`
	ID      string    `json:"id"`
	Vector  []float32 `json:"-"`
}
type evidencePolicy struct {
	DiagnosticStatus    string  `json:"diagnostic_status"`
	ContinuityStatus    string  `json:"continuity_status"`
	DiagnosticFloor     float64 `json:"diagnostic_floor"`
	ContinuityFloor     float64 `json:"continuity_floor"`
	EnrollmentPeople    int     `json:"enrollment_people"`
	QueryPeople         int     `json:"query_people"`
	EnrollmentSamePairs int     `json:"enrollment_same_pairs"`
	QuerySamePairs      int     `json:"query_same_pairs"`
	QueryDifferentPairs int     `json:"query_different_pairs"`
	QuerySamePassing    int     `json:"query_same_passing"`
}
type evidenceDiagnostic struct {
	Speaker                   string    `json:"speaker"`
	Status                    string    `json:"status"`
	Recommendation            string    `json:"recommendation"`
	Cases                     int       `json:"cases"`
	SuspectCaseIDs            []string  `json:"suspect_case_ids"`
	SegmentMedianSimilarities []float64 `json:"segment_median_similarities"`
}

// Validate every supplied training vector, even when there are too few people
// to fit a floor. Inputs remain caller-owned and are neither copied nor changed.
func evidenceTrainingCounts(v []evidenceLabeledVector, dimension int) (map[string]int, error) {
	if dimension < 1 || dimension > maxDimension {
		return nil, errProtocol
	}
	counts := map[string]int{}
	ids := map[string]bool{}
	for _, c := range v {
		if c.Speaker == "" || c.ID == "" || ids[c.ID] || !validVector(c.Vector, dimension) {
			return nil, errProtocol
		}
		ids[c.ID] = true
		counts[c.Speaker]++
	}
	return counts, nil
}
func evidenceEnoughTraining(counts map[string]int) bool {
	if len(counts) < 3 {
		return false
	}
	for _, n := range counts {
		if n < 2 {
			return false
		}
	}
	return true
}
func fitEvidencePolicy(enrollment, queries []evidenceLabeledVector, dimension int) (evidencePolicy, error) {
	ec, e := evidenceTrainingCounts(enrollment, dimension)
	if e != nil {
		return evidencePolicy{}, e
	}
	qc, e := evidenceTrainingCounts(queries, dimension)
	if e != nil {
		return evidencePolicy{}, e
	}
	p := evidencePolicy{DiagnosticStatus: "disabled_insufficient_training", ContinuityStatus: "disabled_insufficient_training", EnrollmentPeople: len(ec), QueryPeople: len(qc)}
	var sameEnrollment, sameQuery []float64
	defer func() { clear(sameEnrollment); clear(sameQuery) }()
	for i, c := range enrollment {
		for _, other := range enrollment[:i] {
			if c.Speaker == other.Speaker {
				sameEnrollment = append(sameEnrollment, similarity(c.Vector, other.Vector))
			}
		}
	}
	p.EnrollmentSamePairs = len(sameEnrollment)
	if evidenceEnoughTraining(ec) {
		sort.Float64s(sameEnrollment)
		index := max(0, int(math.Ceil(.05*float64(len(sameEnrollment))))-1)
		p.DiagnosticFloor = sameEnrollment[index]
		p.DiagnosticStatus = "fitted"
	}
	maxDifferent := -1.0
	for i, c := range queries {
		for _, other := range queries[:i] {
			cosine := similarity(c.Vector, other.Vector)
			if c.Speaker == other.Speaker {
				sameQuery = append(sameQuery, cosine)
			} else {
				p.QueryDifferentPairs++
				maxDifferent = math.Max(maxDifferent, cosine)
			}
		}
	}
	p.QuerySamePairs = len(sameQuery)
	if evidenceEnoughTraining(qc) {
		floor := math.Nextafter(maxDifferent, math.Inf(1))
		if floor > 1 {
			p.ContinuityStatus = "disabled_no_safe_threshold"
		} else {
			p.ContinuityStatus = "fitted"
			p.ContinuityFloor = floor
			for _, s := range sameQuery {
				if s >= floor {
					p.QuerySamePassing++
				}
			}
		}
	}
	return p, nil
}
func diagnoseEvidenceEnrollment(speaker string, clips []evidenceLabeledVector, dimension int, policy evidencePolicy) (evidenceDiagnostic, error) {
	if speaker == "" {
		return evidenceDiagnostic{}, errProtocol
	}
	if _, e := evidenceTrainingCounts(clips, dimension); e != nil {
		return evidenceDiagnostic{}, e
	}
	for _, c := range clips {
		if c.Speaker != speaker {
			return evidenceDiagnostic{}, errProtocol
		}
	}
	if policy.DiagnosticStatus == "fitted" && (!backendFinite(policy.DiagnosticFloor) || policy.DiagnosticFloor < -1 || policy.DiagnosticFloor > 1) {
		return evidenceDiagnostic{}, errProtocol
	}
	d := evidenceDiagnostic{Speaker: speaker, Cases: len(clips), Status: "diagnostic_unavailable", Recommendation: "diagnostic_unavailable", SuspectCaseIDs: []string{}, SegmentMedianSimilarities: []float64{}}
	if policy.DiagnosticStatus != "fitted" || len(clips) < 2 {
		return d, nil
	}
	d.Status = "diagnosed"
	d.Recommendation = "no_consistency_outlier_detected"
	scores := make([]float64, 0, len(clips)-1)
	defer clear(scores[:cap(scores)])
	for i, c := range clips {
		scores = scores[:0]
		for j, other := range clips {
			if j != i {
				scores = append(scores, similarity(c.Vector, other.Vector))
			}
		}
		sort.Float64s(scores)
		mid := len(scores) / 2
		median := scores[mid]
		if len(scores)%2 == 0 {
			median = (scores[mid-1] + scores[mid]) / 2
		}
		d.SegmentMedianSimilarities = append(d.SegmentMedianSimilarities, median)
		if median < policy.DiagnosticFloor {
			d.SuspectCaseIDs = append(d.SuspectCaseIDs, c.ID)
		}
	}
	if len(d.SuspectCaseIDs) > 0 {
		d.Recommendation = "review_and_rerecord_suspect_segments"
	}
	return d, nil
}
