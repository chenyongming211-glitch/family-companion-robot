//go:build linux

package main

import (
	"context"
	"reflect"
	"testing"
)

func TestEvidenceScenesDeterministicNumericInterleaving(t *testing.T) {
	m := backendManifest{}
	cs := []embeddedCase{}
	for _, s := range []string{"validation_10", "validation_unknown_2", "validation_2", "validation_unknown_1", "validation_1"} {
		for _, id := range []string{"z", "a", "m"} {
			role := "validation_query"
			if s == "validation_unknown_1" || s == "validation_unknown_2" {
				role = "validation_unknown"
			}
			m.Cases = append(m.Cases, backendCase{ID: s + "_" + id, Speaker: s, Role: role, Samples: 16000})
			cs = append(cs, embeddedCase{quality: "qualified", vector: []float32{1, 0}})
		}
	}
	for _, tc := range []struct {
		name   string
		prefix []string
	}{{"paired_bursts", []string{"validation_1_a", "validation_1_m", "validation_unknown_1_a", "validation_unknown_1_m", "validation_2_a", "validation_2_m", "validation_unknown_2_a"}}, {"alternating", []string{"validation_1_a", "validation_unknown_1_a", "validation_2_a", "validation_unknown_2_a", "validation_10_a"}}} {
		events, e := buildEvidenceScene(m, cs, "validation", 50, 50, tc.name)
		if e != nil {
			t.Fatal(e)
		}
		seen := map[int]bool{}
		names := []string{}
		for i, event := range events {
			if seen[event.Index] {
				t.Fatal("duplicate")
			}
			seen[event.Index] = true
			names = append(names, m.Cases[event.Index].ID)
			if i > 0 && event.Chunk.Start-events[i-1].Chunk.End != 4000 {
				t.Fatal("gap")
			}
			if event.Chunk.ID != uint64(i+1) {
				t.Fatal("IDs")
			}
		}
		if len(seen) != len(m.Cases) || !reflect.DeepEqual(names[:len(tc.prefix)], tc.prefix) {
			t.Fatal(tc.name, names)
		}
	}
	if _, e := buildEvidenceScene(m, cs, "validation", 50, 50, "future"); e == nil {
		t.Fatal("unsupported scene")
	}
}
func TestEvidenceReplayNoFutureTruthOrGroupLeak(t *testing.T) {
	p := evidencePolicy{ContinuityStatus: "fitted", ContinuityFloor: .5}
	events := []evidenceReplayEvent{{Index: 0, Chunk: evidenceChunk{Stream: "group0", ID: 1, Start: 0, End: 16000, Qualified: true, Vector: []float32{1, 0}}}, {Index: 1, Chunk: evidenceChunk{Stream: "group0", ID: 2, Start: 20000, End: 36000, Qualified: true, Vector: []float32{1, 1}}}, {Index: 2, Chunk: evidenceChunk{Stream: "group1", ID: 1, Start: 0, End: 16000, Qualified: true, Vector: []float32{1, 0}}}}
	templates := [][]float32{{1, 0}, {0, 1}}
	before, stats, windows, e := replayEvidence(context.Background(), events, templates, 2, p, true)
	if e != nil {
		t.Fatal(e)
	}
	if stats.Accumulations != 1 || stats.Queries != 3 || len(windows[1]) != 2 || len(windows[2]) != 1 {
		t.Fatal(stats, windows)
	}
	prefix, _, _, e := replayEvidence(context.Background(), events[:1], templates, 2, p, true)
	if e != nil || !reflect.DeepEqual(prefix[0], before[0]) {
		t.Fatal("future affected past", e)
	}
	// Relabeling is possible only in this posthoc structure; replay has no truth input.
	labels := []string{"same", "different", "same"}
	if evidenceMixedWindows(windows, labels) != 1 {
		t.Fatal("posthoc mixed count")
	}
	labels[1] = "same"
	if evidenceMixedWindows(windows, labels) != 0 {
		t.Fatal("posthoc labels")
	}
	after, _, _, e := replayEvidence(context.Background(), events, templates, 2, p, true)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("posthoc scoring altered decisions")
	}
	for _, status := range []string{"disabled_insufficient_training", "disabled_no_safe_threshold", "unsupported"} {
		p.ContinuityStatus = status
		p.ContinuityFloor = 0
		got, s, _, e := replayEvidence(context.Background(), events, templates, 2, p, true)
		base, _, _, _ := replayEvidence(context.Background(), events, templates, 2, p, false)
		if e != nil || s.Accumulations != 0 || !reflect.DeepEqual(got, base) {
			t.Fatal("disabled floor used", status, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, e := replayEvidence(ctx, events, templates, 2, p, true); e == nil {
		t.Fatal("cancel ignored")
	}
}
