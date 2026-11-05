//go:build linux

package main

import (
	"context"
	"fmt"
	"sort"
	"syscall"
	"time"
)

type evidenceReplayEvent struct {
	Index int
	Chunk evidenceChunk
}
type evidenceReplaySummary struct {
	Queries             int            `json:"queries"`
	Qualified           int            `json:"qualified_queries"`
	Accumulations       int            `json:"accumulations"`
	Reasons             map[string]int `json:"window_reasons"`
	QualifiedSamples    int64          `json:"input_qualified_samples"`
	EvidenceSamplesSum  int64          `json:"returned_evidence_samples_sum"`
	EvidenceSamplesMax  int            `json:"returned_evidence_samples_max"`
	MixedSpeakerWindows int            `json:"posthoc_mixed_speaker_windows"`
	FusionCPUms         float64        `json:"fusion_cpu_ms"`
	ProcessingMS        float64        `json:"processing_ms"`
}

// Candidate membership is assigned by the original backend protocol. Labels are
// used here only to construct the preregistered order; no boundary oracle is set.
func buildEvidenceScene(m backendManifest, cases []embeddedCase, split string, n, size int, scenario string) ([]evidenceReplayEvent, error) {
	if len(cases) != len(m.Cases) || n < 1 || size < 1 || size > n {
		return nil, errProtocol
	}
	burst := 1
	switch scenario {
	case "paired_bursts":
		burst = 2
	case "alternating":
	default:
		return nil, errProtocol
	}
	known, unknown, groups := backendPoolQueries(m, cases, nil, split, n, size)
	byGroup := make([]map[string][]int, groups)
	for i := range byGroup {
		byGroup[i] = map[string][]int{}
	}
	ki, ui := 0, 0
	for i, c := range m.Cases {
		group := -1
		if c.Role == split+"_query" {
			group = known[ki].Group
			ki++
		} else if c.Role == split+"_unknown" {
			group = unknown[ui].Group
			ui++
		}
		if group >= 0 {
			byGroup[group][c.Speaker] = append(byGroup[group][c.Speaker], i)
		}
	}
	var events []evidenceReplayEvent
	for group, people := range byGroup {
		var ks, us []string
		for speaker, indices := range people {
			sort.Slice(indices, func(i, j int) bool { return m.Cases[indices[i]].ID < m.Cases[indices[j]].ID })
			if m.Cases[indices[0]].Role == split+"_unknown" {
				us = append(us, speaker)
			} else {
				ks = append(ks, speaker)
			}
		}
		sort.Slice(ks, func(i, j int) bool { return backendSpeakerLess(ks[i], ks[j]) })
		sort.Slice(us, func(i, j int) bool { return backendSpeakerLess(us[i], us[j]) })
		order := make([]string, 0, len(people))
		for i := 0; i < max(len(ks), len(us)); i++ {
			if i < len(ks) {
				order = append(order, ks[i])
			}
			if i < len(us) {
				order = append(order, us[i])
			}
		}
		offsets := map[string]int{}
		var coordinate int64
		var id uint64
		for {
			progressed := false
			for _, speaker := range order {
				indices := people[speaker]
				for j := 0; j < burst && offsets[speaker] < len(indices); j++ {
					index := indices[offsets[speaker]]
					offsets[speaker]++
					progressed = true
					id++
					samples := m.Cases[index].Samples
					if samples < 1 || samples > 128000 {
						return nil, errProtocol
					}
					chunk := evidenceChunk{Stream: fmt.Sprintf("group_%d", group), ID: id, Start: coordinate, End: coordinate + int64(samples), Qualified: cases[index].quality == "qualified", Vector: cases[index].vector}
					events = append(events, evidenceReplayEvent{Index: index, Chunk: chunk})
					coordinate = chunk.End + 4000
				}
			}
			if !progressed {
				break
			}
		}
	}
	return events, nil
}

// Replay owns transient fusion vectors and returns only score arrays and source
// indices. It cannot inspect speaker labels, enrollment identities or future audio.
func replayEvidence(ctx context.Context, events []evidenceReplayEvent, templates [][]float32, dimension int, policy evidencePolicy, accumulate bool) (map[int][]float64, evidenceReplaySummary, map[int][]int, error) {
	started := time.Now()
	summary := evidenceReplaySummary{Reasons: map[string]int{}}
	scores := map[int][]float64{}
	windows := map[int][]int{}
	fail := func(e error) (map[int][]float64, evidenceReplaySummary, map[int][]int, error) {
		for _, v := range scores {
			clear(v)
		}
		return nil, summary, nil, e
	}
	if dimension < 1 || len(templates) < 1 {
		return fail(errProtocol)
	}
	for _, v := range templates {
		if !validVector(v, dimension) {
			return fail(errProtocol)
		}
	}
	use := accumulate && policy.ContinuityStatus == "fitted"
	var accumulator *evidenceAccumulator
	if use {
		var e error
		accumulator, e = newEvidenceAccumulator(dimension, policy.ContinuityFloor)
		if e != nil {
			return fail(e)
		}
		defer accumulator.Reset()
	}
	retained := []int{}
	seen := map[int]bool{}
	for _, event := range events {
		if e := ctx.Err(); e != nil {
			return fail(e)
		}
		if event.Index < 0 || seen[event.Index] {
			return fail(errProtocol)
		}
		seen[event.Index] = true
		chunk := event.Chunk
		summary.Queries++
		var value evidenceAccumulated
		if use {
			var before, after syscall.Rusage
			if syscall.Getrusage(syscall.RUSAGE_SELF, &before) != nil {
				return fail(errWorker)
			}
			var e error
			value, e = accumulator.Push(chunk)
			if syscall.Getrusage(syscall.RUSAGE_SELF, &after) != nil {
				clear(value.Vector)
				return fail(errWorker)
			}
			summary.FusionCPUms += float64(after.Utime.Nano()+after.Stime.Nano()-before.Utime.Nano()-before.Stime.Nano()) / 1e6
			if e != nil {
				return fail(e)
			}
		} else {
			value.Reason = "single_segment"
			if chunk.Qualified && !chunk.Overlap {
				if !validVector(chunk.Vector, dimension) {
					return fail(errProtocol)
				}
				value.Vector = chunk.Vector
				value.Chunks = 1
				value.Samples = int(chunk.End - chunk.Start)
			}
		}
		summary.Reasons[value.Reason]++
		if value.Chunks == 0 {
			retained = nil
			continue
		}
		summary.Qualified++
		summary.QualifiedSamples += chunk.End - chunk.Start
		summary.EvidenceSamplesSum += int64(value.Samples)
		summary.EvidenceSamplesMax = max(summary.EvidenceSamplesMax, value.Samples)
		if value.Chunks > 1 {
			summary.Accumulations++
			retained = append(retained, event.Index)
		} else {
			retained = []int{event.Index}
		}
		windows[event.Index] = append([]int(nil), retained...)
		row := make([]float64, len(templates))
		for i, template := range templates {
			row[i] = similarity(value.Vector, template)
		}
		scores[event.Index] = row
		if use {
			clear(value.Vector)
		}
	}
	summary.ProcessingMS = backendElapsed(started)
	return scores, summary, windows, nil
}

// Ground truth is read only after all decisions have been produced. This value
// is report-only; callers must never use it for reset or calibration.
func evidenceMixedWindows(windows map[int][]int, labels []string) int {
	mixed := 0
	for _, indices := range windows {
		if len(indices) < 2 {
			continue
		}
		for _, index := range indices[1:] {
			if labels[index] != labels[indices[0]] {
				mixed++
				break
			}
		}
	}
	return mixed
}
