// Package speaker assigns anonymous, session-only labels to qualified embeddings.
// It does not infer names or persist audio, embeddings, or identities.
package speaker

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrInvalidConfig      = errors.New("speaker: invalid configuration")
	ErrScope              = errors.New("speaker: observation scope mismatch")
	ErrInvalidObservation = errors.New("speaker: invalid observation")
	ErrClosed             = errors.New("speaker: tracker closed")
)

// Scope is fixed for the entire lifetime of a Tracker. A new session, stream
// generation, or embedding model requires a new Tracker.
type Scope struct {
	SessionID        string
	StreamGeneration uint64
	ModelRevision    string
}

type Config struct {
	Scope               Scope
	Dimension           int
	MinSamples          int64
	MatchThreshold      float64
	NewSpeakerThreshold float64
	Margin              float64
	MaxSpeakers         int
}

// Observation describes an absolute, half-open sample interval [start, end).
// SingleSpeaker and Complete must come from the caller's quality assessment;
// the tracker cannot establish either fact from an embedding.
type Observation struct {
	Scope         Scope
	StartSample   int64
	EndSample     int64
	Embedding     []float32
	SingleSpeaker bool
	Complete      bool
}

type State string

const (
	StateTemporary State = "temporary"
	StateUncertain State = "uncertain"
	StateUnknown   State = "unknown"
)

// Result contains no embedding or persistent identity. Revision increases on
// every accepted observation, including uncertain or unknown observations.
// Keep the originating Scope alongside this result when checking IsCurrent.
type Result struct {
	State       State
	Label       string
	Revision    uint64
	StartSample int64
	EndSample   int64
}

// Tracker is owned serially by its caller; it is not safe for concurrent use.
// Templates are normalized copies of the first adequate observation for each
// label, and are never adapted by subsequent matches.
type Tracker struct {
	config    Config
	templates [][]float64
	last      Result
	closed    bool
}

func NewTracker(cfg Config) (*Tracker, error) {
	if cfg.Scope.SessionID == "" || cfg.Scope.ModelRevision == "" ||
		cfg.Dimension < 1 || cfg.Dimension > 1024 || cfg.MinSamples <= 0 ||
		cfg.MaxSpeakers < 1 || cfg.MaxSpeakers > 8 ||
		!finite(cfg.MatchThreshold) || !finite(cfg.NewSpeakerThreshold) || !finite(cfg.Margin) ||
		cfg.NewSpeakerThreshold < -1 || cfg.MatchThreshold > 1 ||
		cfg.NewSpeakerThreshold >= cfg.MatchThreshold || cfg.Margin < 0 || cfg.Margin > 2 {
		return nil, ErrInvalidConfig
	}
	return &Tracker{config: cfg}, nil
}

// Observe rejects malformed, wrong-scope, overlapping, and late observations
// without changing any state. Valid but unqualified observations advance the
// interval/revision watermark and return uncertain without allocating a label.
func (t *Tracker) Observe(obs Observation) (Result, error) {
	if t.closed {
		return Result{}, ErrClosed
	}
	if obs.Scope != t.config.Scope {
		return Result{}, ErrScope
	}
	if obs.StartSample < 0 || obs.EndSample <= obs.StartSample ||
		obs.StartSample < t.last.EndSample {
		return Result{}, fmt.Errorf("%w: sample interval", ErrInvalidObservation)
	}
	vector, err := normalize(obs.Embedding, t.config.Dimension)
	if err != nil {
		return Result{}, err
	}
	result := Result{State: StateUncertain, Revision: t.last.Revision + 1,
		StartSample: obs.StartSample, EndSample: obs.EndSample}
	if obs.EndSample-obs.StartSample >= t.config.MinSamples && obs.SingleSpeaker && obs.Complete {
		result.State, result.Label = t.classify(vector)
	}
	t.last = result
	return result, nil
}

func (t *Tracker) classify(vector []float64) (State, string) {
	best, second, bestIndex := math.Inf(-1), math.Inf(-1), -1
	for i, template := range t.templates {
		score := cosine(vector, template)
		if score > best {
			second, best, bestIndex = best, score, i
		} else if score > second {
			second = score
		}
	}
	if bestIndex >= 0 && best >= t.config.MatchThreshold &&
		(len(t.templates) == 1 || best-second >= t.config.Margin) {
		return StateTemporary, guestLabel(bestIndex)
	}
	// Equality with the new-speaker threshold is deliberately a gray zone.
	if best < t.config.NewSpeakerThreshold {
		if len(t.templates) == t.config.MaxSpeakers {
			return StateUnknown, ""
		}
		t.templates = append(t.templates, vector)
		return StateTemporary, guestLabel(len(t.templates) - 1)
	}
	return StateUncertain, ""
}

// IsCurrent validates the latest result against its original immutable scope.
// An accepted later observation or Close invalidates prior results. The caller
// must supply the originating scope, rather than relabeling an old result with a
// new session's scope. This is a lifecycle check, not an authentication token.
func (t *Tracker) IsCurrent(scope Scope, result Result) bool {
	return !t.closed && scope == t.config.Scope && result.Revision != 0 && result == t.last
}

// Close clears template values, releases their references, and permanently
// rejects observations and results. Create a new Tracker to start fresh labels.
func (t *Tracker) Close() {
	for _, template := range t.templates {
		clear(template)
	}
	t.templates = nil
	t.last = Result{}
	t.closed = true
}

func normalize(input []float32, dimension int) ([]float64, error) {
	if len(input) != dimension {
		return nil, fmt.Errorf("%w: embedding dimension", ErrInvalidObservation)
	}
	// Float64 accumulation safely covers up to 1024 finite float32 components,
	// including their squares; tiny float32 vectors also remain nonzero here.
	vector := make([]float64, dimension)
	var squaredNorm float64
	for i, component := range input {
		value := float64(component)
		if !finite(value) {
			return nil, fmt.Errorf("%w: nonfinite embedding", ErrInvalidObservation)
		}
		vector[i] = value
		squaredNorm += value * value
	}
	if squaredNorm == 0 || !finite(squaredNorm) {
		return nil, fmt.Errorf("%w: embedding norm", ErrInvalidObservation)
	}
	norm := math.Sqrt(squaredNorm)
	for i := range vector {
		vector[i] /= norm
	}
	return vector, nil
}

func cosine(a, b []float64) float64 {
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return math.Max(-1, math.Min(1, dot))
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func guestLabel(index int) string { return fmt.Sprintf("guest_%d", index+1) }
