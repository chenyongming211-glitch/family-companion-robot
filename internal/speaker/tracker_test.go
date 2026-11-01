package speaker

import (
	"errors"
	"math"
	"testing"
)

func testConfig() Config {
	return Config{
		Scope:     Scope{SessionID: "session-a", StreamGeneration: 1, ModelRevision: "model-v1"},
		Dimension: 3, MinSamples: 100, MaxSpeakers: 8,
		MatchThreshold: 0.8, NewSpeakerThreshold: 0.3, Margin: 0.1,
	}
}

func newTestTracker(t *testing.T, cfg Config) *Tracker {
	t.Helper()
	tracker, err := NewTracker(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return tracker
}

func observation(cfg Config, start int64, vector ...float32) Observation {
	return Observation{Scope: cfg.Scope, StartSample: start, EndSample: start + 100,
		Embedding: vector, SingleSpeaker: true, Complete: true}
}

func observe(t *testing.T, tracker *Tracker, obs Observation, state State, label string) Result {
	t.Helper()
	result, err := tracker.Observe(obs)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != state || result.Label != label || result.Revision == 0 ||
		result.StartSample != obs.StartSample || result.EndSample != obs.EndSample {
		t.Fatalf("unexpected result: %+v", result)
	}
	return result
}

func TestTrackerAnonymousABA(t *testing.T) {
	cfg := testConfig()
	tracker := newTestTracker(t, cfg)
	a := observe(t, tracker, observation(cfg, 0, 2, 0, 0), StateTemporary, "guest_1")
	b := observe(t, tracker, observation(cfg, 100, 0, 3, 0), StateTemporary, "guest_2")
	a2 := observe(t, tracker, observation(cfg, 200, 1, 0, 0), StateTemporary, "guest_1")
	if !(a.Revision < b.Revision && b.Revision < a2.Revision) {
		t.Fatal("accepted observations must have increasing revisions")
	}
}

func TestTrackerGrayZoneAndAmbiguousCandidates(t *testing.T) {
	t.Run("gray does not allocate", func(t *testing.T) {
		cfg := testConfig()
		tracker := newTestTracker(t, cfg)
		observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
		observe(t, tracker, observation(cfg, 100, 0.6, 0.8, 0), StateUncertain, "")
		observe(t, tracker, observation(cfg, 200, 0, 1, 0), StateTemporary, "guest_2")
	})
	t.Run("runner up requires margin", func(t *testing.T) {
		cfg := testConfig()
		cfg.NewSpeakerThreshold = 0.5
		cfg.MatchThreshold = 0.7
		tracker := newTestTracker(t, cfg)
		observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
		observe(t, tracker, observation(cfg, 100, 0, 1, 0), StateTemporary, "guest_2")
		observe(t, tracker, observation(cfg, 200, 1, 1, 0), StateUncertain, "")
		observe(t, tracker, observation(cfg, 300, 1, 0, 0), StateTemporary, "guest_1")
	})
	t.Run("one candidate has no runner up", func(t *testing.T) {
		cfg := testConfig()
		cfg.Margin = 2
		tracker := newTestTracker(t, cfg)
		observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
		observe(t, tracker, observation(cfg, 100, 1, 0, 0), StateTemporary, "guest_1")
	})
}

func TestTrackerInsufficientQualityDoesNotAllocate(t *testing.T) {
	for _, kind := range []string{"short", "mixed", "incomplete"} {
		t.Run(kind, func(t *testing.T) {
			cfg := testConfig()
			tracker := newTestTracker(t, cfg)
			obs := observation(cfg, 0, 0, 1, 0)
			switch kind {
			case "short":
				obs.EndSample = 99
			case "mixed":
				obs.SingleSpeaker = false
			case "incomplete":
				obs.Complete = false
			}
			observe(t, tracker, obs, StateUncertain, "")
			observe(t, tracker, observation(cfg, 100, 1, 0, 0), StateTemporary, "guest_1")
		})
	}
}

func TestTrackerRejectsInvalidObservationsWithoutMutation(t *testing.T) {
	cases := map[string]func(*Observation){
		"session":           func(o *Observation) { o.Scope.SessionID = "other" },
		"generation":        func(o *Observation) { o.Scope.StreamGeneration++ },
		"model":             func(o *Observation) { o.Scope.ModelRevision = "other" },
		"negative start":    func(o *Observation) { o.StartSample = -1 },
		"empty span":        func(o *Observation) { o.EndSample = o.StartSample },
		"reversed span":     func(o *Observation) { o.EndSample = o.StartSample - 1 },
		"late span":         func(o *Observation) { o.StartSample, o.EndSample = 0, 100 },
		"overlapping span":  func(o *Observation) { o.StartSample = 99 },
		"empty vector":      func(o *Observation) { o.Embedding = nil },
		"short vector":      func(o *Observation) { o.Embedding = []float32{1, 0} },
		"long vector":       func(o *Observation) { o.Embedding = []float32{1, 0, 0, 0} },
		"zero vector":       func(o *Observation) { o.Embedding = []float32{0, 0, 0} },
		"nan":               func(o *Observation) { o.Embedding[0] = float32(math.NaN()) },
		"positive infinity": func(o *Observation) { o.Embedding[0] = float32(math.Inf(1)) },
		"negative infinity": func(o *Observation) { o.Embedding[0] = float32(math.Inf(-1)) },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			tracker := newTestTracker(t, cfg)
			initial := observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
			bad := observation(cfg, 1000, 0, 1, 0)
			corrupt(&bad)
			result, err := tracker.Observe(bad)
			if err == nil || result != (Result{}) {
				t.Fatalf("invalid observation must return error and empty result: %+v, %v", result, err)
			}
			if !tracker.IsCurrent(cfg.Scope, initial) {
				t.Fatal("rejected observation invalidated the current result")
			}
			// A malformed far-future observation must not advance the watermark or revision.
			next := observe(t, tracker, observation(cfg, 100, 0, 1, 0), StateTemporary, "guest_2")
			if next.Revision != initial.Revision+1 {
				t.Fatal("rejected observation changed revision")
			}
		})
	}
}

func TestTrackerRejectsInvalidConfig(t *testing.T) {
	cases := map[string]func(*Config){
		"session":             func(c *Config) { c.Scope.SessionID = "" },
		"model":               func(c *Config) { c.Scope.ModelRevision = "" },
		"zero dimension":      func(c *Config) { c.Dimension = 0 },
		"negative dimension":  func(c *Config) { c.Dimension = -1 },
		"dimension limit":     func(c *Config) { c.Dimension = 1025 },
		"zero minimum":        func(c *Config) { c.MinSamples = 0 },
		"negative minimum":    func(c *Config) { c.MinSamples = -1 },
		"zero capacity":       func(c *Config) { c.MaxSpeakers = 0 },
		"negative capacity":   func(c *Config) { c.MaxSpeakers = -1 },
		"capacity limit":      func(c *Config) { c.MaxSpeakers = 9 },
		"equal thresholds":    func(c *Config) { c.NewSpeakerThreshold = c.MatchThreshold },
		"reversed thresholds": func(c *Config) { c.NewSpeakerThreshold = 0.9 },
		"low threshold":       func(c *Config) { c.NewSpeakerThreshold = -1.1 },
		"high threshold":      func(c *Config) { c.MatchThreshold = 1.1 },
		"nan new":             func(c *Config) { c.NewSpeakerThreshold = math.NaN() },
		"infinite new":        func(c *Config) { c.NewSpeakerThreshold = math.Inf(-1) },
		"nan match":           func(c *Config) { c.MatchThreshold = math.NaN() },
		"infinite match":      func(c *Config) { c.MatchThreshold = math.Inf(1) },
		"negative margin":     func(c *Config) { c.Margin = -0.1 },
		"margin limit":        func(c *Config) { c.Margin = 2.1 },
		"nan margin":          func(c *Config) { c.Margin = math.NaN() },
		"infinite margin":     func(c *Config) { c.Margin = math.Inf(1) },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			corrupt(&cfg)
			tracker, err := NewTracker(cfg)
			if !errors.Is(err, ErrInvalidConfig) || tracker != nil {
				t.Fatalf("invalid config accepted: %v", err)
			}
		})
	}
	// Explicit upper bounds and threshold endpoints are accepted.
	cfg := testConfig()
	cfg.Dimension, cfg.MaxSpeakers = 1024, 8
	cfg.NewSpeakerThreshold, cfg.MatchThreshold, cfg.Margin = -1, 1, 0
	newTestTracker(t, cfg)
}

func TestTrackerCapacityNeverForcesAssignment(t *testing.T) {
	cfg := testConfig()
	cfg.MaxSpeakers = 2
	tracker := newTestTracker(t, cfg)
	observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
	observe(t, tracker, observation(cfg, 100, 0, 1, 0), StateTemporary, "guest_2")
	observe(t, tracker, observation(cfg, 200, 0, 0, 1), StateUnknown, "")
	observe(t, tracker, observation(cfg, 300, 0.6, 0, 0.8), StateUncertain, "")
	observe(t, tracker, observation(cfg, 400, 1, 0, 0), StateTemporary, "guest_1")
}

func TestTrackerCopiesInputAndKeepsFirstTemplate(t *testing.T) {
	cfg := testConfig()
	tracker := newTestTracker(t, cfg)
	input := observation(cfg, 0, 1, 0, 0)
	observe(t, tracker, input, StateTemporary, "guest_1")
	input.Embedding[0], input.Embedding[1] = 0, 1
	observe(t, tracker, observation(cfg, 100, 1, 0, 0), StateTemporary, "guest_1")
	observe(t, tracker, observation(cfg, 200, 4, 3, 0), StateTemporary, "guest_1")
	// This is close to the last match, but only gray against the original template.
	observe(t, tracker, observation(cfg, 300, 0.6, 0.8, 0), StateUncertain, "")
	observe(t, tracker, observation(cfg, 400, 0, 1, 0), StateTemporary, "guest_2")
}

func TestTrackerStaleResultsAndClose(t *testing.T) {
	cfg := testConfig()
	tracker := newTestTracker(t, cfg)
	first := observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
	if !tracker.IsCurrent(cfg.Scope, first) || tracker.IsCurrent(cfg.Scope, Result{}) {
		t.Fatal("unexpected initial result validity")
	}
	for _, mutate := range []func(*Scope){
		func(s *Scope) { s.SessionID = "other" },
		func(s *Scope) { s.StreamGeneration++ },
		func(s *Scope) { s.ModelRevision = "other" },
	} {
		scope := cfg.Scope
		mutate(&scope)
		if tracker.IsCurrent(scope, first) {
			t.Fatal("wrong scope accepted a result")
		}
	}
	second := observe(t, tracker, observation(cfg, 100, 1, 0, 0), StateTemporary, "guest_1")
	if tracker.IsCurrent(cfg.Scope, first) || !tracker.IsCurrent(cfg.Scope, second) {
		t.Fatal("stale result accepted")
	}
	forged := second
	forged.Label = "guest_8"
	if tracker.IsCurrent(cfg.Scope, forged) {
		t.Fatal("altered result accepted")
	}
	tracker.Close()
	tracker.Close()
	if tracker.IsCurrent(cfg.Scope, second) {
		t.Fatal("closed tracker accepts prior result")
	}
	if result, err := tracker.Observe(observation(cfg, 200, 1, 0, 0)); !errors.Is(err, ErrClosed) || result != (Result{}) {
		t.Fatal("closed tracker accepted observation")
	}
	// Inspect lifecycle cleanup, without publishing any vectors in results.
	if len(tracker.templates) != 0 {
		t.Fatal("close retained templates")
	}
	cfg.Scope.SessionID = "session-b"
	next := newTestTracker(t, cfg)
	observe(t, next, observation(cfg, 0, 0, 1, 0), StateTemporary, "guest_1")
	if next.IsCurrent(testConfig().Scope, first) {
		t.Fatal("new session accepted an old-session result")
	}
}

func TestTrackerExtremeFiniteVectorsAndSampleBounds(t *testing.T) {
	cfg := testConfig()
	tracker := newTestTracker(t, cfg)
	observe(t, tracker, observation(cfg, 0, math.MaxFloat32, 0, 0), StateTemporary, "guest_1")
	observe(t, tracker, observation(cfg, 100, math.SmallestNonzeroFloat32, 0, 0), StateTemporary, "guest_1")
	last := observation(cfg, math.MaxInt64-100, 1, 0, 0)
	observe(t, tracker, last, StateTemporary, "guest_1")
}

func TestTrackerAcceptedUncertainAdvancesWatermark(t *testing.T) {
	for _, kind := range []string{"short", "mixed", "incomplete", "gray"} {
		t.Run(kind, func(t *testing.T) {
			cfg := testConfig()
			tracker := newTestTracker(t, cfg)
			first := observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
			obs := observation(cfg, 200, 0.6, 0.8, 0)
			switch kind {
			case "short":
				obs.EndSample--
			case "mixed":
				obs.SingleSpeaker = false
			case "incomplete":
				obs.Complete = false
			}
			current := observe(t, tracker, obs, StateUncertain, "")
			if tracker.IsCurrent(cfg.Scope, first) || !tracker.IsCurrent(cfg.Scope, current) {
				t.Fatal("accepted uncertain observation did not advance result revision")
			}
			if _, err := tracker.Observe(observation(cfg, 100, 0, 1, 0)); !errors.Is(err, ErrInvalidObservation) {
				t.Fatal("late observation preceding uncertainty accepted")
			}
			observe(t, tracker, observation(cfg, 300, 0, 1, 0), StateTemporary, "guest_2")
		})
	}
}

func TestTrackerThresholdAndMarginBoundaries(t *testing.T) {
	t.Run("new threshold equality is uncertain", func(t *testing.T) {
		cfg := testConfig()
		cfg.NewSpeakerThreshold = 0
		tracker := newTestTracker(t, cfg)
		observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
		observe(t, tracker, observation(cfg, 100, 0, 1, 0), StateUncertain, "")
		observe(t, tracker, observation(cfg, 200, -1, 0, 0), StateTemporary, "guest_2")
	})
	t.Run("match and margin equality are accepted", func(t *testing.T) {
		cfg := testConfig()
		cfg.MatchThreshold, cfg.Margin = 1, 2
		tracker := newTestTracker(t, cfg)
		observe(t, tracker, observation(cfg, 0, 1, 0, 0), StateTemporary, "guest_1")
		observe(t, tracker, observation(cfg, 100, -1, 0, 0), StateTemporary, "guest_2")
		observe(t, tracker, observation(cfg, 200, 1, 0, 0), StateTemporary, "guest_1")
	})
	t.Run("eight speakers and full dimension", func(t *testing.T) {
		cfg := testConfig()
		cfg.Dimension = 1024
		tracker := newTestTracker(t, cfg)
		for i := 0; i < 9; i++ {
			vector := make([]float32, cfg.Dimension)
			vector[i] = math.MaxFloat32
			if i < 8 {
				observe(t, tracker, observation(cfg, int64(i)*100, vector...), StateTemporary, guestLabel(i))
			} else {
				observe(t, tracker, observation(cfg, int64(i)*100, vector...), StateUnknown, "")
			}
		}
	})
}
