//go:build linux

package main

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func prototypeClips(v ...[]float32) []evidenceLabeledVector {
	c := make([]evidenceLabeledVector, len(v))
	for i := range c {
		c[i] = evidenceLabeledVector{Speaker: "person", ID: fmt.Sprintf("clip_%03d", i), Vector: v[i]}
	}
	return c
}
func clearPrototypeTest(v [][]float32) {
	for _, x := range v {
		clear(x)
	}
}
func TestPrototypeTwoModesPermutationAndOwnership(t *testing.T) {
	clips := prototypeClips([]float32{1, 0}, []float32{1, .01}, []float32{0, 1}, []float32{.01, 1})
	before := make([][]float32, len(clips))
	for i, c := range clips {
		before[i] = append([]float32(nil), c.Vector...)
	}
	p, s, e := buildEnrollmentPrototypes(clips, 2)
	if e != nil || s.Status != "two_prototypes" || s.Reason != "stable" || len(p) != 2 || !reflect.DeepEqual(s.ClusterSizes, []int{2, 2}) || s.Iterations < 1 || s.Iterations > 10 {
		t.Fatalf("two modes: %+v %v", s, e)
	}
	defer clearPrototypeTest(p)
	query := []float32{1, 0}
	score, e := prototypeScore(query, p, 2)
	mean, e2 := fuseEnrollment(before, 2)
	if e != nil || e2 != nil || !backendFinite(score) || score <= similarity(query, mean)+.2 {
		t.Fatal("max cosine gain", score, e, e2)
	}
	clear(mean)
	// All permutations must use sorted IDs and the same farthest-pair tie order.
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 40; i++ {
		perm := append([]evidenceLabeledVector(nil), clips...)
		rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
		pp, ss, e := buildEnrollmentPrototypes(perm, 2)
		if e != nil || !reflect.DeepEqual(ss, s) || !reflect.DeepEqual(pp, p) {
			t.Fatal("permutation changed prototypes", ss, e)
		}
		clearPrototypeTest(pp)
	}
	for i, c := range clips {
		if !reflect.DeepEqual(c.Vector, before[i]) {
			t.Fatal("borrowed input changed")
		}
	}
	p[0][0] = .123
	if reflect.DeepEqual(p[0], p[1]) || !reflect.DeepEqual(clips[0].Vector, before[0]) {
		t.Fatal("returned vector aliases input or other center")
	}
}
func TestPrototypeFallbackMatchesOriginalBExactly(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		clips        []evidenceLabeledVector
	}{
		{"one", "insufficient_segments", prototypeClips([]float32{2, 1})},
		{"three", "insufficient_segments", prototypeClips([]float32{1, .2}, []float32{.7, .5}, []float32{.1, 1})},
		{"identical", "coincident_seeds", prototypeClips([]float32{1, 0}, []float32{2, 0}, []float32{3, 0}, []float32{4, 0})},
		{"singleton", "empty_or_singleton_cluster", prototypeClips([]float32{1, 0}, []float32{1, .01}, []float32{1, -.01}, []float32{0, 1})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vectors := make([][]float32, len(tc.clips))
			for i, c := range tc.clips {
				vectors[i] = c.Vector
			}
			b, e := fuseEnrollment(vectors, 2)
			if e != nil {
				t.Fatal(e)
			}
			defer clear(b)
			p, s, e := buildEnrollmentPrototypes(tc.clips, 2)
			if e != nil || len(p) != 1 || s.Status != "fallback_single" || s.Reason != tc.reason || !reflect.DeepEqual(s.ClusterSizes, []int{len(tc.clips)}) || !reflect.DeepEqual(p[0], b) {
				t.Fatalf("fallback must be original B: %+v %v", s, e)
			}
			clearPrototypeTest(p)
			if !validVector(vectors[0], 2) {
				t.Fatal("clear corrupted borrowed vectors")
			}
		})
	}
	// Non-associative float32 accumulation: fallback preserves manifest order,
	// even when ID order differs. It must not fuse already-normalized copies.
	clips := prototypeClips([]float32{.012345, 1}, []float32{1, .345678}, []float32{.765432, .987654})
	clips[0].ID = "z"
	clips[1].ID = "a"
	clips[2].ID = "m"
	b, _ := fuseEnrollment([][]float32{clips[0].Vector, clips[1].Vector, clips[2].Vector}, 2)
	defer clear(b)
	p, _, e := buildEnrollmentPrototypes(clips, 2)
	if e != nil || !reflect.DeepEqual(p[0], b) {
		t.Fatal("fallback changed original accumulation order")
	}
	clearPrototypeTest(p)
}
func TestPrototypeValidationAndNegativeDirections(t *testing.T) {
	good := prototypeClips([]float32{-1, 0}, []float32{-1, -.01}, []float32{0, -1}, []float32{-.01, -1})
	p, _, e := buildEnrollmentPrototypes(good, 2)
	if e != nil {
		t.Fatal("negative finite coordinates valid", e)
	}
	defer clearPrototypeTest(p)
	for _, tc := range []struct {
		name  string
		clips []evidenceLabeledVector
		dim   int
	}{
		{"empty", nil, 2}, {"negative dimension", good, -1}, {"zero dimension", good, 0}, {"too large", good, maxDimension + 1},
		{"dimension mismatch", prototypeClips([]float32{1}), 2}, {"zero", prototypeClips([]float32{0, 0}), 2},
		{"nan", prototypeClips([]float32{float32(math.NaN()), 1}), 2}, {"inf", prototypeClips([]float32{1, float32(math.Inf(1))}), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, e := buildEnrollmentPrototypes(tc.clips, tc.dim)
			if e != errProtocol || p != nil {
				t.Fatal("invalid accepted", e)
			}
		})
	}
	for _, mutate := range []func([]evidenceLabeledVector){func(c []evidenceLabeledVector) { c[1].Speaker = "other" }, func(c []evidenceLabeledVector) { c[1].ID = c[0].ID }, func(c []evidenceLabeledVector) { c[0].ID = "" }, func(c []evidenceLabeledVector) { c[0].Speaker = "" }} {
		c := append([]evidenceLabeledVector(nil), good...)
		mutate(c)
		out, _, e := buildEnrollmentPrototypes(c, 2)
		if e != errProtocol || out != nil {
			t.Fatal("mixed or duplicate accepted")
		}
	}
	// Original B cancellation is an error, never select a surviving segment.
	out, _, e := buildEnrollmentPrototypes(prototypeClips([]float32{1, 0}, []float32{-1, 0}), 2)
	if e != errProtocol || out != nil {
		t.Fatal("zero B not rejected")
	}
	for _, tc := range []struct {
		q   []float32
		p   [][]float32
		dim int
	}{
		{[]float32{1, 0}, nil, 2}, {[]float32{1, 0}, [][]float32{{1, 0}, {0, 1}, {1, 1}}, 2},
		{[]float32{1, 0}, [][]float32{{1, 0}, {float32(math.NaN()), 0}}, 2},
		{[]float32{0, 0}, p, 2}, {[]float32{1}, p, 2}, {[]float32{1, 0}, p, -1},
	} {
		if _, e := prototypeScore(tc.q, tc.p, tc.dim); e != errProtocol {
			t.Fatal("invalid max cosine accepted")
		}
	}
	score, e := prototypeScore([]float32{1, 0}, [][]float32{{-1, 0}, {-.5, -.5}}, 2)
	if e != nil || score >= 0 || !backendFinite(score) {
		t.Fatal("negative max clipped", score, e)
	}
}
func TestPrototypeIterationCapAndTieDeterminism(t *testing.T) {
	// Symmetric directions produce exact seed and assignment ties.
	clips := prototypeClips([]float32{1, 0}, []float32{0, 1}, []float32{-1, 0}, []float32{0, 1})
	p, s, e := buildEnrollmentPrototypes(clips, 2)
	if e != nil || s.Reason != "empty_or_singleton_cluster" || s.Iterations != 1 || len(p) != 1 {
		t.Fatal("tie must choose cluster zero", s, e)
	}
	clearPrototypeTest(p)
	rng := rand.New(rand.NewSource(71))
	limits := 0
	for fixture := 0; fixture < 200; fixture++ {
		var vectors [][]float32
		for i := 0; i < 80; i++ {
			v := make([]float32, 8)
			for j := range v {
				v[j] = float32(rng.NormFloat64())
			}
			vectors = append(vectors, v)
		}
		p, s, e := buildEnrollmentPrototypes(prototypeClips(vectors...), 8)
		if e != nil || s.Iterations < 1 || s.Iterations > 10 {
			t.Fatal("iteration bound", s, e)
		}
		if s.Reason == "iteration_limit" {
			limits++
		}
		sum := 0
		for _, n := range s.ClusterSizes {
			sum += n
		}
		if sum != len(vectors) {
			t.Fatal("segments dropped", s)
		}
		for _, v := range p {
			if !validVector(v, 8) {
				t.Fatal("nonfinite center")
			}
		}
		clearPrototypeTest(p)
	}
	if limits == 0 {
		t.Fatal("fixture did not exercise exact ten-round termination")
	}
}

func TestPrototypeStableSeedAndAssignmentTies(t *testing.T) {
	clips := prototypeClips([]float32{1, 0}, []float32{1, 0}, []float32{-1, 0}, []float32{-1, 0}, []float32{0, 1}, []float32{0, 1})
	p, s, e := buildEnrollmentPrototypes(clips, 2)
	if e != nil || s.Status != "two_prototypes" || !reflect.DeepEqual(s.ClusterSizes, []int{4, 2}) || p[0][0] <= 0 || p[0][1] <= 0 || !reflect.DeepEqual(p[1], []float32{-1, 0}) {
		t.Fatal("lexicographic seed and cluster-zero ties", s, e)
	}
	defer clearPrototypeTest(p)
	for i, j := 0, len(clips)-1; i < j; i, j = i+1, j-1 {
		clips[i], clips[j] = clips[j], clips[i]
	}
	pp, ss, e := buildEnrollmentPrototypes(clips, 2)
	if e != nil || !reflect.DeepEqual(ss, s) || !reflect.DeepEqual(pp, p) {
		t.Fatal("tied seeds changed under permutation")
	}
	clearPrototypeTest(pp)
}
