//go:build linux

package main

import "math"

type evidenceChunk struct {
	Stream        string    `json:"stream"`
	ID            uint64    `json:"id"`
	Start         int64     `json:"start"`
	End           int64     `json:"end"`
	Qualified     bool      `json:"qualified"`
	Overlap       bool      `json:"overlap"`
	Discontinuity bool      `json:"discontinuity"`
	Vector        []float32 `json:"-"`
}
type evidenceAccumulated struct {
	Vector  []float32 `json:"-"`
	Samples int       `json:"samples"`
	Chunks  int       `json:"chunks"`
	Reason  string    `json:"reason"`
}
type evidenceRetained struct {
	vector  []float32
	samples int
}

// This state owns only private vector copies. It has no identity, enrollment,
// recognition result, or truth-label input. A caller must serialize its use.
type evidenceAccumulator struct {
	dimension       int
	continuityFloor float64
	retained        []evidenceRetained
	samples         int
	firstStart      int64
	lastStream      string
	lastID          uint64
	lastEnd         int64
	haveLast        bool
}

func newEvidenceAccumulator(dimension int, continuityFloor float64) (*evidenceAccumulator, error) {
	if dimension < 1 || dimension > maxDimension || !backendFinite(continuityFloor) || continuityFloor < -1 || continuityFloor > 1 {
		return nil, errProtocol
	}
	return &evidenceAccumulator{dimension: dimension, continuityFloor: continuityFloor}, nil
}
func (a *evidenceAccumulator) clearWindow() {
	for i := range a.retained {
		clear(a.retained[i].vector)
		a.retained[i] = evidenceRetained{}
	}
	a.retained = nil
	a.samples = 0
	a.firstStart = 0
}

// Reset is also the cleanup operation when an experiment closes. It never
// clears caller inputs or vectors returned from previous Push calls.
func (a *evidenceAccumulator) Reset() {
	a.clearWindow()
	a.lastStream = ""
	a.lastID = 0
	a.lastEnd = 0
	a.haveLast = false
}
func (a *evidenceAccumulator) protocolError() (evidenceAccumulated, error) {
	// Keep the last accepted stream coordinate, so malformed events cannot make
	// an earlier event valid again. Explicit Reset starts a new sequence scope.
	a.clearWindow()
	return evidenceAccumulated{Reason: "bad_protocol"}, errProtocol
}
func (a *evidenceAccumulator) Push(chunk evidenceChunk) (evidenceAccumulated, error) {
	if chunk.Stream == "" || chunk.ID == 0 || chunk.Start < 0 || chunk.End <= chunk.Start || chunk.End-chunk.Start > 128000 {
		return a.protocolError()
	}
	sameStream := a.haveLast && chunk.Stream == a.lastStream
	if sameStream && (chunk.ID <= a.lastID || chunk.Start < a.lastEnd) {
		return a.protocolError()
	}
	// A suppressed event may have no extracted vector. If one was supplied,
	// validate it as rigorously as a qualified event; never hide NaN or Inf.
	if (len(chunk.Vector) > 0 || chunk.Qualified && !chunk.Overlap) && !validVector(chunk.Vector, a.dimension) {
		return a.protocolError()
	}
	reason := "initial"
	if a.haveLast && !sameStream {
		reason = "stream_change"
		a.clearWindow()
	}
	if chunk.Discontinuity {
		reason = "discontinuity"
		a.clearWindow()
	}
	if chunk.Overlap || !chunk.Qualified {
		a.clearWindow()
		a.lastStream = chunk.Stream
		a.lastID = chunk.ID
		a.lastEnd = chunk.End
		a.haveLast = true
		if chunk.Overlap {
			reason = "overlap"
		} else {
			reason = "unqualified"
		}
		return evidenceAccumulated{Reason: reason}, nil
	}
	samples := int(chunk.End - chunk.Start)
	if len(a.retained) > 0 {
		reason = "accumulated"
		switch {
		case chunk.Start-a.lastEnd > 24000:
			reason = "gap_limit"
		case chunk.End-a.firstStart > 192000:
			reason = "span_limit"
		case a.samples+samples > 128000:
			reason = "sample_limit"
		case len(a.retained) >= 4:
			reason = "chunk_limit"
		default:
			for _, old := range a.retained {
				if similarity(chunk.Vector, old.vector) < a.continuityFloor {
					reason = "continuity_mismatch"
					break
				}
			}
		}
		if reason != "accumulated" {
			a.clearWindow()
		}
	}
	if len(a.retained) == 0 {
		a.firstStart = chunk.Start
	}
	a.retained = append(a.retained, evidenceRetained{vector: append([]float32(nil), chunk.Vector...), samples: samples})
	a.samples += samples
	vector, e := a.fuse()
	if e != nil {
		return a.protocolError()
	}
	a.lastStream = chunk.Stream
	a.lastID = chunk.ID
	a.lastEnd = chunk.End
	a.haveLast = true
	return evidenceAccumulated{Vector: vector, Samples: a.samples, Chunks: len(a.retained), Reason: reason}, nil
}
func (a *evidenceAccumulator) fuse() ([]float32, error) {
	sum := make([]float64, a.dimension)
	defer clear(sum)
	for _, c := range a.retained {
		norm := 0.0
		for _, x := range c.vector {
			norm += float64(x) * float64(x)
		}
		norm = math.Sqrt(norm)
		weight := float64(c.samples) / float64(a.samples)
		for i, x := range c.vector {
			sum[i] += float64(x) / norm * weight
		}
	}
	norm := 0.0
	for _, x := range sum {
		norm += x * x
	}
	if norm <= 0 || !backendFinite(norm) {
		return nil, errProtocol
	}
	norm = math.Sqrt(norm)
	out := make([]float32, a.dimension)
	for i, x := range sum {
		out[i] = float32(x / norm)
	}
	if !validVector(out, a.dimension) {
		clear(out)
		return nil, errProtocol
	}
	return out, nil
}
