//go:build linux

package main

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func evidenceTestAccumulator(t *testing.T, floor float64) *evidenceAccumulator {
	t.Helper()
	a, e := newEvidenceAccumulator(2, floor)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func evidenceTestChunk(id uint64, start, end int64, v ...float32) evidenceChunk {
	return evidenceChunk{Stream: "stream", ID: id, Start: start, End: end, Qualified: true, Vector: v}
}
func evidencePush(t *testing.T, a *evidenceAccumulator, c evidenceChunk) evidenceAccumulated {
	t.Helper()
	r, e := a.Push(c)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func evidenceNearVector(t *testing.T, v []float32, w ...float64) {
	t.Helper()
	if len(v) != len(w) {
		t.Fatal(v, w)
	}
	for i := range v {
		observed, expected := float64(v[i]), w[i]
		if math.IsNaN(observed) || math.IsInf(observed, 0) || math.IsNaN(expected) || math.IsInf(expected, 0) || math.Abs(observed-expected) > 1e-6 {
			t.Fatal(v, w)
		}
	}
}
func TestEvidenceAccumulatorWeightedNormalizedAndImmediate(t *testing.T) {
	a := evidenceTestAccumulator(t, 0)
	r := evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 10, 0))
	if r.Chunks != 1 || r.Samples != 1000 || r.Reason != "initial" {
		t.Fatal(r)
	}
	evidenceNearVector(t, r.Vector, 1, 0)
	r = evidencePush(t, a, evidenceTestChunk(2, 1000, 4000, 0, 2))
	if r.Chunks != 2 || r.Samples != 4000 || r.Reason != "accumulated" {
		t.Fatal(r)
	}
	evidenceNearVector(t, r.Vector, 1/math.Sqrt(10), 3/math.Sqrt(10))
}
func TestEvidenceAccumulatorAllPairsBlocksCentroidDrift(t *testing.T) {
	a := evidenceTestAccumulator(t, .7)
	// 0, 40, 60 degrees: 60 passes the 20-degree centroid and 40,
	// but fails the retained 0-degree segment (.5 < .7).
	evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 1, 0))
	evidencePush(t, a, evidenceTestChunk(2, 1000, 2000, float32(math.Cos(40*math.Pi/180)), float32(math.Sin(40*math.Pi/180))))
	r := evidencePush(t, a, evidenceTestChunk(3, 2000, 3000, .5, float32(math.Sqrt(3)/2)))
	if r.Chunks != 1 || r.Reason != "continuity_mismatch" {
		t.Fatal(r)
	}
	evidenceNearVector(t, r.Vector, .5, math.Sqrt(3)/2)
}
func TestEvidenceAccumulatorInclusiveLimitsAndRestarts(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		chunks       []evidenceChunk
		counts       []int
	}{
		{"audio", "sample_limit", []evidenceChunk{evidenceTestChunk(1, 0, 64000, 1, 0), evidenceTestChunk(2, 64000, 128000, 1, 0), evidenceTestChunk(3, 128000, 128001, 1, 0)}, []int{1, 2, 1}},
		{"count", "chunk_limit", []evidenceChunk{evidenceTestChunk(1, 0, 1, 1, 0), evidenceTestChunk(2, 1, 2, 1, 0), evidenceTestChunk(3, 2, 3, 1, 0), evidenceTestChunk(4, 3, 4, 1, 0), evidenceTestChunk(5, 4, 5, 1, 0)}, []int{1, 2, 3, 4, 1}},
		{"gap", "gap_limit", []evidenceChunk{evidenceTestChunk(1, 0, 1000, 1, 0), evidenceTestChunk(2, 25000, 26000, 1, 0), evidenceTestChunk(3, 50001, 51001, 1, 0)}, []int{1, 2, 1}},
		{"span", "span_limit", []evidenceChunk{evidenceTestChunk(1, 0, 40000, 1, 0), evidenceTestChunk(2, 64000, 104000, 1, 0), evidenceTestChunk(3, 128000, 151999, 1, 0), evidenceTestChunk(4, 175999, 192000, 1, 0)}, []int{1, 2, 3, 4}},
		{"span over", "span_limit", []evidenceChunk{evidenceTestChunk(1, 0, 40000, 1, 0), evidenceTestChunk(2, 64000, 104000, 1, 0), evidenceTestChunk(3, 128000, 151999, 1, 0), evidenceTestChunk(4, 175999, 192001, 1, 0)}, []int{1, 2, 3, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := evidenceTestAccumulator(t, .5)
			var r evidenceAccumulated
			for i, c := range tc.chunks {
				r = evidencePush(t, a, c)
				if r.Chunks != tc.counts[i] {
					t.Fatal(i, r)
				}
			}
			if tc.counts[len(tc.counts)-1] == 1 && r.Reason != tc.reason {
				t.Fatal(r)
			}
		})
	}
}
func TestEvidenceAccumulatorBoundariesSuppressOrRestart(t *testing.T) {
	for _, flag := range []string{"stream_change", "discontinuity", "overlap", "unqualified"} {
		t.Run(flag, func(t *testing.T) {
			a := evidenceTestAccumulator(t, .5)
			evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 1, 0))
			c := evidenceTestChunk(2, 1000, 2000, 1, 0)
			switch flag {
			case "stream_change":
				c.Stream = "other"
				c.ID = 1
				c.Start = 0
				c.End = 1000
			case "discontinuity":
				c.Discontinuity = true
			case "overlap":
				c.Overlap = true
				c.Vector = nil
			case "unqualified":
				c.Qualified = false
				c.Vector = nil
			}
			r := evidencePush(t, a, c)
			if r.Reason != flag {
				t.Fatal(r)
			}
			if flag == "overlap" || flag == "unqualified" {
				if r.Vector != nil || r.Chunks != 0 || r.Samples != 0 {
					t.Fatal(r)
				}
				c = evidenceTestChunk(3, 2000, 3000, 1, 0)
				r = evidencePush(t, a, c)
			}
			if r.Chunks != 1 {
				t.Fatal(r)
			}
		})
	}
}
func TestEvidenceAccumulatorProtocolErrorsClear(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*evidenceChunk)
	}{
		{"duplicate id", func(c *evidenceChunk) { c.ID = 1 }}, {"retrograde id", func(c *evidenceChunk) { c.ID = 0 }},
		{"crossing time", func(c *evidenceChunk) { c.Start = 999 }}, {"retrograde time", func(c *evidenceChunk) { c.Start = 0; c.End = 1 }},
		{"negative", func(c *evidenceChunk) { c.Start = -1 }}, {"empty time", func(c *evidenceChunk) { c.End = c.Start }},
		{"large chunk", func(c *evidenceChunk) { c.End = c.Start + 128001 }}, {"empty stream", func(c *evidenceChunk) { c.Stream = "" }},
		{"dimension", func(c *evidenceChunk) { c.Vector = []float32{1} }}, {"zero", func(c *evidenceChunk) { c.Vector = []float32{0, 0} }},
		{"nan", func(c *evidenceChunk) { c.Vector[0] = float32(math.NaN()) }}, {"inf", func(c *evidenceChunk) { c.Vector[1] = float32(math.Inf(1)) }},
		{"suppressed nan", func(c *evidenceChunk) { c.Qualified = false; c.Vector[0] = float32(math.NaN()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := evidenceTestAccumulator(t, -1)
			evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 1, 0))
			owned := evidenceOwnedVectors(reflect.ValueOf(a))
			c := evidenceTestChunk(2, 1000, 2000, 1, 0)
			tc.edit(&c)
			r, e := a.Push(c)
			if !errors.Is(e, errProtocol) || r.Vector != nil || r.Chunks != 0 {
				t.Fatal(r, e)
			}
			evidenceAssertZero(t, owned)
			if _, e = a.Push(evidenceTestChunk(1, 0, 1000, 1, 0)); !errors.Is(e, errProtocol) {
				t.Fatal("error allowed stale replay", e)
			}
			r = evidencePush(t, a, evidenceTestChunk(2, 1000, 2000, 1, 0))
			if r.Chunks != 1 {
				t.Fatal(r)
			}
		})
	}
}
func TestEvidenceAccumulatorCancellationAndConstructor(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), -1.01, 1.01} {
		if _, e := newEvidenceAccumulator(2, f); !errors.Is(e, errProtocol) {
			t.Fatal(f, e)
		}
	}
	for _, d := range []int{0, -1, maxDimension + 1} {
		if _, e := newEvidenceAccumulator(d, .5); !errors.Is(e, errProtocol) {
			t.Fatal(d, e)
		}
	}
	a := evidenceTestAccumulator(t, -1)
	evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 1, 0))
	owned := evidenceOwnedVectors(reflect.ValueOf(a))
	r, e := a.Push(evidenceTestChunk(2, 1000, 2000, -1, 0))
	if !errors.Is(e, errProtocol) || r.Vector != nil {
		t.Fatal(r, e)
	}
	evidenceAssertZero(t, owned)
}

// Keep views of owned numeric backing arrays to inspect zeroing after release.
func evidenceOwnedVectors(v reflect.Value) []reflect.Value {
	if v.Kind() == reflect.Pointer {
		return evidenceOwnedVectors(v.Elem())
	}
	var out []reflect.Value
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			out = append(out, evidenceOwnedVectors(v.Field(i))...)
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Float32 || v.Type().Elem().Kind() == reflect.Float64 {
			if v.Len() > 0 {
				return []reflect.Value{v.Slice(0, v.Len())}
			}
		}
		for i := 0; i < v.Len(); i++ {
			out = append(out, evidenceOwnedVectors(v.Index(i))...)
		}
	}
	return out
}
func evidenceAssertZero(t *testing.T, views []reflect.Value) {
	t.Helper()
	if len(views) == 0 {
		t.Fatal("no retained numeric vectors found")
	}
	for _, v := range views {
		for i := 0; i < v.Len(); i++ {
			if v.Index(i).Float() != 0 {
				t.Fatal("owned vector was not cleared")
			}
		}
	}
}
func TestEvidenceAccumulatorIndependentOwnershipAndResetZeroing(t *testing.T) {
	a := evidenceTestAccumulator(t, 0)
	input := []float32{10, 0}
	r := evidencePush(t, a, evidenceTestChunk(1, 0, 1000, input...))
	owned := evidenceOwnedVectors(reflect.ValueOf(a))
	input[0] = 0
	input[1] = 10
	r.Vector[0] = .25
	next := evidencePush(t, a, evidenceTestChunk(2, 1000, 2000, 1, 0))
	evidenceNearVector(t, next.Vector, 1, 0)
	if r.Vector[0] != .25 {
		t.Fatal("mutated old result")
	}
	old := append([]float32(nil), next.Vector...)
	owned = append(owned, evidenceOwnedVectors(reflect.ValueOf(a))...)
	a.Reset()
	evidenceAssertZero(t, owned)
	if !reflect.DeepEqual(next.Vector, old) || input[1] != 10 {
		t.Fatal("reset mutated caller vectors")
	}
	r = evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 0, 1))
	if r.Chunks != 1 {
		t.Fatal(r)
	}
	a.Reset()
	a.Reset()
}
func TestEvidenceAccumulatorRestartClearsOldVectors(t *testing.T) {
	for _, reason := range []string{"continuity", "stream", "quality", "discontinuity", "gap"} {
		t.Run(reason, func(t *testing.T) {
			a := evidenceTestAccumulator(t, .5)
			evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 1, 0))
			owned := evidenceOwnedVectors(reflect.ValueOf(a))
			c := evidenceTestChunk(2, 1000, 2000, 1, 0)
			switch reason {
			case "continuity":
				c.Vector = []float32{0, 1}
			case "stream":
				c.Stream = "new"
			case "quality":
				c.Qualified = false
				c.Vector = nil
			case "discontinuity":
				c.Discontinuity = true
			case "gap":
				c.Start = 25001
				c.End = 26001
			}
			evidencePush(t, a, c)
			evidenceAssertZero(t, owned)
		})
	}
}

func TestEvidenceAccumulatorsIndependentAndSuppressedSequence(t *testing.T) {
	a := evidenceTestAccumulator(t, .5)
	b := evidenceTestAccumulator(t, .5)
	evidencePush(t, a, evidenceTestChunk(1, 0, 1000, 1, 0))
	evidencePush(t, b, evidenceTestChunk(1, 0, 1000, 0, 1))
	a.Reset()
	r := evidencePush(t, b, evidenceTestChunk(2, 1000, 2000, 0, 1))
	if r.Chunks != 2 {
		t.Fatal(r)
	}
	evidenceNearVector(t, r.Vector, 0, 1)
	c := evidenceTestChunk(3, 2000, 3000)
	c.Qualified = false
	evidencePush(t, b, c)
	if _, e := b.Push(c); !errors.Is(e, errProtocol) {
		t.Fatal("suppressed event allowed replay", e)
	}
	r = evidencePush(t, b, evidenceTestChunk(4, 3000, 4000, 0, 1))
	if r.Chunks != 1 {
		t.Fatal(r)
	}
}
