//go:build linux

package main

import (
	"math"
	"sort"
)

type prototypeSummary struct {
	Speaker      string `json:"anonymous_speaker"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	Cases        int    `json:"cases"`
	Iterations   int    `json:"iterations"`
	ClusterSizes []int  `json:"cluster_sizes"`
}

// Inputs are borrowed. Only normalized copies and centers are owned here;
// successful outputs transfer ownership, and every temporary is cleared.
func buildEnrollmentPrototypes(clips []evidenceLabeledVector, dimension int) ([][]float32, prototypeSummary, error) {
	s := prototypeSummary{Cases: len(clips)}
	if dimension < 1 || dimension > maxDimension || len(clips) == 0 {
		return nil, s, errProtocol
	}
	original := make([][]float32, len(clips))
	ids := map[string]bool{}
	s.Speaker = clips[0].Speaker
	for i, c := range clips {
		if c.Speaker == "" || c.Speaker != s.Speaker || c.ID == "" || ids[c.ID] || !validVector(c.Vector, dimension) {
			return nil, s, errProtocol
		}
		ids[c.ID] = true
		original[i] = c.Vector
	}
	fallback := func(reason string) ([][]float32, prototypeSummary, error) {
		s.Status = "fallback_single"
		s.Reason = reason
		s.ClusterSizes = []int{len(clips)}
		v, e := fuseEnrollment(original, dimension)
		if e != nil {
			return nil, s, e
		}
		return [][]float32{v}, s, nil
	}
	if len(clips) < 4 {
		return fallback("insufficient_segments")
	}
	sorted := make([]evidenceLabeledVector, 0, len(clips))
	defer func() {
		for _, c := range sorted {
			clear(c.Vector)
		}
	}()
	for _, c := range clips {
		v, e := fuseEnrollment([][]float32{c.Vector}, dimension)
		if e != nil {
			return fallback("numeric_normalization_failure")
		}
		sorted = append(sorted, evidenceLabeledVector{Speaker: c.Speaker, ID: c.ID, Vector: v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	seedA, seedB := 0, 1
	least := math.Inf(1)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			score := similarity(sorted[i].Vector, sorted[j].Vector)
			if score < least {
				least = score
				seedA, seedB = i, j
			}
		}
	}
	if !backendFinite(least) || least >= 1 {
		return fallback("coincident_seeds")
	}
	centers := [][]float32{append([]float32(nil), sorted[seedA].Vector...), append([]float32(nil), sorted[seedB].Vector...)}
	defer func() {
		for _, v := range centers {
			clear(v)
		}
	}()
	previous := make([]int, len(sorted))
	for i := range previous {
		previous[i] = -1
	}
	defer clear(previous)
	for iteration := 1; iteration <= 10; iteration++ {
		s.Iterations = iteration
		members := [2][][]float32{}
		unchanged := true
		for i, c := range sorted {
			cluster := 0
			if similarity(c.Vector, centers[1]) > similarity(c.Vector, centers[0]) {
				cluster = 1
			}
			if previous[i] != cluster {
				unchanged = false
			}
			previous[i] = cluster
			members[cluster] = append(members[cluster], c.Vector)
		}
		if len(members[0]) < 2 || len(members[1]) < 2 {
			return fallback("empty_or_singleton_cluster")
		}
		next := make([][]float32, 0, 2)
		for _, cluster := range members {
			v, e := fuseEnrollment(cluster, dimension)
			if e != nil {
				for _, owned := range next {
					clear(owned)
				}
				return fallback("numeric_fusion_failure")
			}
			next = append(next, v)
		}
		for _, v := range centers {
			clear(v)
		}
		centers = next
		s.ClusterSizes = []int{len(members[0]), len(members[1])}
		s.Reason = "iteration_limit"
		if unchanged {
			s.Reason = "stable"
		}
		if unchanged || iteration == 10 {
			s.Status = "two_prototypes"
			result := centers
			centers = nil
			return result, s, nil
		}
	}
	return nil, s, errProtocol
}

// One independently extracted query scores against at most two enrollment
// centers. Every vector is checked before any cosine or max operation.
func prototypeScore(query []float32, prototypes [][]float32, dimension int) (float64, error) {
	if !validVector(query, dimension) || len(prototypes) < 1 || len(prototypes) > 2 {
		return 0, errProtocol
	}
	for _, v := range prototypes {
		if !validVector(v, dimension) {
			return 0, errProtocol
		}
	}
	best := -1.0
	for _, v := range prototypes {
		score := similarity(query, v)
		if !backendFinite(score) {
			return 0, errProtocol
		}
		best = math.Max(best, score)
	}
	return best, nil
}
