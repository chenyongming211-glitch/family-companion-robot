//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"syscall"
	"time"

	"familyrobot/internal/speaker"
)

const minQualitySamples = 24000
const minRMS = 0.005
const maxManifestBytes = 128 * 1024

var fixtureSpeakerPattern = regexp.MustCompile(`^fixture_[1-8]$`)

type fixtureCase struct {
	ID      string `json:"id"`
	Speaker string `json:"speaker"`
	Split   string `json:"split"`
	Path    string `json:"path"`
	Samples int    `json:"samples"`
	SHA     string `json:"sha256"`
	TextSHA string `json:"text_sha256"`
}
type fixtureManifest struct {
	SourceKind        string        `json:"source_kind"`
	Model             string        `json:"model"`
	SampleRate        int           `json:"sample_rate"`
	HumanAcceptance   bool          `json:"human_acceptance"`
	SingleSpeakerOnly bool          `json:"single_speaker_only"`
	Cases             []fixtureCase `json:"cases"`
}
type embeddedCase struct {
	fixtureCase
	vector  []float32
	quality string
}

// boundedFile retains one verified, non-symlink regular inode for reading.
func boundedFile(path string, limit int64) ([]byte, error) {
	if !noSymlinks(path) {
		return nil, errProtocol
	}
	before, e := os.Lstat(path)
	if e != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > limit {
		return nil, errProtocol
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, errProtocol
	}
	f := os.NewFile(uintptr(fd), "fixture")
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !os.SameFile(before, st) || !st.Mode().IsRegular() {
		return nil, errProtocol
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit || int64(len(b)) != st.Size() {
		clear(b)
		return nil, errProtocol
	}
	return b, nil
}
func loadManifest(path string) (fixtureManifest, string, error) {
	var m fixtureManifest
	b, e := boundedFile(path, maxManifestBytes)
	if e != nil {
		return m, "", e
	}
	defer clear(b)
	var raw struct {
		SourceKind        string            `json:"source_kind"`
		Model             string            `json:"model"`
		SampleRate        int               `json:"sample_rate"`
		HumanAcceptance   bool              `json:"human_acceptance"`
		SingleSpeakerOnly bool              `json:"single_speaker_only"`
		Cases             []json.RawMessage `json:"cases"`
	}
	if strictHeader(b, &raw) != nil || raw.SourceKind != "synthetic_tts" || !tokenPattern.MatchString(raw.Model) || raw.SampleRate != sampleRate || raw.HumanAcceptance || !raw.SingleSpeakerOnly || len(raw.Cases) < 6 || len(raw.Cases) > 128 {
		return m, "", errProtocol
	}
	m = fixtureManifest{SourceKind: raw.SourceKind, Model: raw.Model, SampleRate: raw.SampleRate, SingleSpeakerOnly: true}
	ids := map[string]bool{}
	paths := map[string]bool{}
	audioSplit := map[string]string{}
	textSplit := map[string]string{}
	counts := map[string][2]int{}
	for _, r := range raw.Cases {
		var c fixtureCase
		if strictHeader(r, &c) != nil || !tokenPattern.MatchString(c.ID) || ids[c.ID] || !fixtureSpeakerPattern.MatchString(c.Speaker) || c.Samples < 1 || c.Samples > maxSamples || !shaPattern.MatchString(c.SHA) || !shaPattern.MatchString(c.TextSHA) || paths[c.Path] {
			return fixtureManifest{}, "", errProtocol
		}
		if c.Split != "calibration" && c.Split != "holdout" {
			return fixtureManifest{}, "", errProtocol
		}
		if old, ok := audioSplit[c.SHA]; ok && old != c.Split {
			return fixtureManifest{}, "", errProtocol
		}
		if old, ok := textSplit[c.TextSHA]; ok && old != c.Split {
			return fixtureManifest{}, "", errProtocol
		}
		ids[c.ID] = true
		paths[c.Path] = true
		audioSplit[c.SHA] = c.Split
		textSplit[c.TextSHA] = c.Split
		n := counts[c.Speaker]
		if c.Split == "calibration" {
			n[0]++
		} else {
			n[1]++
		}
		counts[c.Speaker] = n
		m.Cases = append(m.Cases, c)
	}
	if len(counts) < 3 || len(counts) > 8 {
		return fixtureManifest{}, "", errProtocol
	}
	for _, n := range counts {
		if n[0] < 2 || n[1] < 1 {
			return fixtureManifest{}, "", errProtocol
		}
	}
	h := sha256.Sum256(b)
	return m, hex.EncodeToString(h[:]), nil
}
func loadFixture(c fixtureCase) ([]byte, error) {
	b, e := boundedFile(c.Path, maxPCMBytes+65536)
	if e != nil {
		return nil, e
	}
	defer clear(b)
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != c.SHA {
		return nil, errProtocol
	}
	pcm, e := readWAV(bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	if len(pcm)/2 != c.Samples {
		clear(pcm)
		return nil, errProtocol
	}
	return pcm, nil
}

// This gate detects only duration and low energy, never overlap or speech.
func qualityReason(pcm []byte) string {
	if len(pcm)/2 < minQualitySamples {
		return "short"
	}
	var energy float64
	for i := 0; i < len(pcm); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
		energy += v * v
	}
	if math.Sqrt(energy/float64(len(pcm)/2)) < minRMS {
		return "silence"
	}
	return "qualified"
}

type calibrationResult struct {
	Status         string  `json:"status"`
	FittedVoices   int     `json:"fitted_voices"`
	FittedCases    int     `json:"fitted_cases"`
	SameMin        float64 `json:"same_min"`
	SameMax        float64 `json:"same_max"`
	DifferentMin   float64 `json:"different_min"`
	DifferentMax   float64 `json:"different_max"`
	NewThreshold   float64 `json:"new_speaker_threshold"`
	MatchThreshold float64 `json:"match_threshold"`
	Margin         float64 `json:"margin"`
}

func groupCases(cases []embeddedCase) []string {
	var groups []string
	seen := map[string]bool{}
	for _, c := range cases {
		if !seen[c.Speaker] {
			seen[c.Speaker] = true
			groups = append(groups, c.Speaker)
		}
	}
	return groups
}
func similarity(a, b []float32) float64 {
	var dot, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		aa += x * x
		bb += y * y
	}
	return math.Max(-1, math.Min(1, dot/math.Sqrt(aa*bb)))
}
func calibrate(cases []embeddedCase) calibrationResult {
	r := calibrationResult{Status: "failed_insufficient_calibration"}
	groups := groupCases(cases)
	if len(groups) < 2 {
		return r
	}
	var fit []embeddedCase
	counts := map[string]int{}
	for _, c := range cases {
		if c.Split == "calibration" && (c.Speaker == groups[0] || c.Speaker == groups[1]) && counts[c.Speaker] < 2 {
			if c.quality != "" && c.quality != "qualified" {
				return r
			}
			counts[c.Speaker]++
			fit = append(fit, c)
		}
	}
	if len(fit) != 4 {
		return r
	}
	r.FittedVoices = 2
	r.FittedCases = 4
	r.SameMin = 1
	r.SameMax = -1
	r.DifferentMin = 1
	r.DifferentMax = -1
	for i := range fit {
		for j := 0; j < i; j++ {
			v := similarity(fit[i].vector, fit[j].vector)
			if fit[i].Speaker == fit[j].Speaker {
				r.SameMin = math.Min(r.SameMin, v)
				r.SameMax = math.Max(r.SameMax, v)
			} else {
				r.DifferentMin = math.Min(r.DifferentMin, v)
				r.DifferentMax = math.Max(r.DifferentMax, v)
			}
		}
	}
	gap := r.SameMin - r.DifferentMax
	if gap <= 0 {
		r.Status = "failed_nonseparable"
		return r
	}
	r.Status = "separable"
	r.NewThreshold = r.DifferentMax + gap/4
	r.MatchThreshold = r.DifferentMax + gap/2
	r.Margin = gap / 8
	return r
}

type sequenceCase struct {
	Index    int           `json:"index"`
	Fixture  int           `json:"fixture"`
	Round    int           `json:"round"`
	State    speaker.State `json:"state"`
	Label    string        `json:"label,omitempty"`
	Expected string        `json:"expected"`
	Correct  bool          `json:"correct"`
}
type sequenceResult struct {
	Status          string         `json:"status"`
	Cases           []sequenceCase `json:"cases"`
	Stable          int            `json:"stable"`
	Uncertain       int            `json:"uncertain"`
	Unknown         int            `json:"unknown"`
	WrongKnown      int            `json:"wrong_known"`
	KnownReacquired int            `json:"known_reacquired"`
}
type scoreRange struct {
	Count int     `json:"count"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}
type benchmarkAnalysis struct {
	Calibration      calibrationResult `json:"calibration"`
	Sequence         sequenceResult    `json:"anonymous_sequence"`
	Capacity         sequenceResult    `json:"capacity_two"`
	HoldoutSame      scoreRange        `json:"holdout_same_voice"`
	HoldoutDifferent scoreRange        `json:"holdout_different_voice"`
}

func addScore(r *scoreRange, v float64) {
	if r.Count == 0 {
		r.Min = v
		r.Max = v
	} else {
		r.Min = math.Min(r.Min, v)
		r.Max = math.Max(r.Max, v)
	}
	r.Count++
}
func analyzeBenchmark(cases []embeddedCase, sha string, dimension int) (benchmarkAnalysis, error) {
	r := benchmarkAnalysis{Sequence: sequenceResult{Status: "not_run"}, Capacity: sequenceResult{Status: "not_run"}}
	for _, c := range cases {
		if c.quality == "" || c.quality == "qualified" {
			if !validVector(c.vector, dimension) {
				return r, errProtocol
			}
		}
	}
	r.Calibration = calibrate(cases)
	for i, c := range cases {
		if c.Split != "holdout" || len(c.vector) == 0 {
			continue
		}
		for j := 0; j < i; j++ {
			d := cases[j]
			if d.Split != "holdout" || len(d.vector) == 0 {
				continue
			}
			v := similarity(c.vector, d.vector)
			if c.Speaker == d.Speaker {
				addScore(&r.HoldoutSame, v)
			} else {
				addScore(&r.HoldoutDifferent, v)
			}
		}
	}
	if r.Calibration.Status != "separable" {
		return r, nil
	}
	var e error
	r.Sequence, e = runSequence(cases, sha, dimension, r.Calibration, 8)
	if e != nil {
		return r, e
	}
	r.Capacity, e = runSequence(cases, sha, dimension, r.Calibration, 2)
	return r, e
}
func runSequence(cases []embeddedCase, sha string, dimension int, cal calibrationResult, capacity int) (sequenceResult, error) {
	r := sequenceResult{Status: "evaluated"}
	groups := groupCases(cases)
	scope := speaker.Scope{SessionID: "synthetic_benchmark", StreamGeneration: 1, ModelRevision: sha}
	tracker, e := speaker.NewTracker(speaker.Config{Scope: scope, Dimension: dimension, MinSamples: minQualitySamples, MatchThreshold: cal.MatchThreshold, NewSpeakerThreshold: cal.NewThreshold, Margin: cal.Margin, MaxSpeakers: capacity})
	if e != nil {
		return r, e
	}
	defer tracker.Close()
	var end int64
	observe := func(c embeddedCase) (speaker.Result, error) {
		start := end
		end += int64(c.Samples)
		return tracker.Observe(speaker.Observation{Scope: scope, StartSample: start, EndSample: end, Embedding: c.vector, Complete: true, SingleSpeaker: c.quality == "" || c.quality == "qualified"})
	}
	fixtureLabels := map[string]string{}
	labelOwners := map[string]string{}
	for _, g := range groups[:2] {
		for _, c := range cases {
			if c.Speaker == g && c.Split == "calibration" {
				result, err := observe(c)
				if err != nil {
					return r, err
				}
				if result.State != speaker.StateTemporary || labelOwners[result.Label] != "" {
					return r, errProtocol
				}
				fixtureLabels[g] = result.Label
				labelOwners[result.Label] = g
				break
			}
		}
	}
	// Only held-out cases drive evaluation; two complete passes test reacquisition.
	var held [][]embeddedCase
	maxCount := 0
	for _, g := range groups {
		var a []embeddedCase
		for _, c := range cases {
			if c.Speaker == g && c.Split == "holdout" {
				a = append(a, c)
			}
		}
		held = append(held, a)
		if len(a) > maxCount {
			maxCount = len(a)
		}
	}
	for round := 1; round <= 2; round++ {
		for n := 0; n < maxCount; n++ {
			for g, a := range held {
				if n >= len(a) {
					continue
				}
				c := a[n]
				expected := fixtureLabels[c.Speaker]
				if expected == "" {
					expected = "new_guest"
				}
				if capacity == 2 && g >= 2 {
					expected = "unknown"
				}
				if len(c.vector) == 0 {
					r.Uncertain++
					r.Cases = append(r.Cases, sequenceCase{Index: len(r.Cases) + 1, Fixture: g + 1, Round: round, State: speaker.StateUncertain, Expected: expected})
					continue
				}
				result, e := observe(c)
				if e != nil {
					return r, e
				}
				// Assign the reference mapping only for a genuinely new label. A
				// rejected first sample reserves no ordinal; a known fixture that
				// later splits keeps its original reference label and scores wrong.
				if result.State == speaker.StateTemporary {
					if expected == "new_guest" && labelOwners[result.Label] == "" {
						fixtureLabels[c.Speaker] = result.Label
						expected = result.Label
					}
					// Retain ownership even for an incorrect split, so a different
					// fixture cannot subsequently claim that already-created label.
					if labelOwners[result.Label] == "" {
						labelOwners[result.Label] = c.Speaker
					}
				}
				correct := string(result.State) == expected || result.State == speaker.StateTemporary && result.Label == expected
				row := sequenceCase{Index: len(r.Cases) + 1, Fixture: g + 1, Round: round, State: result.State, Label: result.Label, Expected: expected, Correct: correct}
				r.Cases = append(r.Cases, row)
				switch result.State {
				case speaker.StateUncertain:
					r.Uncertain++
				case speaker.StateUnknown:
					r.Unknown++
				case speaker.StateTemporary:
					if correct {
						r.Stable++
						if g < 2 {
							r.KnownReacquired++
						}
					} else {
						r.WrongKnown++
					}
				}
			}
		}
	}
	return r, nil
}

type timingSummary struct {
	FirstMS      float64 `json:"first_ms"`
	WarmMedianMS float64 `json:"warm_median_ms"`
	WarmP95MS    float64 `json:"warm_p95_ms"`
	WarmCount    int     `json:"warm_count"`
}

func summarizeTimes(a []float64) timingSummary {
	if len(a) == 0 {
		return timingSummary{}
	}
	r := timingSummary{FirstMS: a[0], WarmCount: len(a) - 1}
	if len(a) == 1 {
		return r
	}
	s := append([]float64(nil), a[1:]...)
	sort.Float64s(s)
	n := len(s)
	r.WarmMedianMS = s[n/2]
	if n%2 == 0 {
		r.WarmMedianMS = (s[n/2-1] + s[n/2]) / 2
	}
	r.WarmP95MS = s[int(math.Ceil(.95*float64(n)))-1]
	return r
}

type benchmarkReport struct {
	SourceKind             string        `json:"source_kind"`
	HumanAcceptance        bool          `json:"human_acceptance"`
	ProductionAcceptance   bool          `json:"production_acceptance"`
	FeatureEnabled         bool          `json:"feature_enabled"`
	OverlapAssessment      string        `json:"overlap_assessment"`
	SegmentationAssessment string        `json:"segmentation_assessment"`
	SingleSpeakerBasis     string        `json:"single_speaker_basis"`
	Evidence               string        `json:"quality_evidence"`
	ModelSHA               string        `json:"model_sha256"`
	ManifestSHA            string        `json:"manifest_sha256"`
	Dimension              int           `json:"dimension"`
	CaseCount              int           `json:"case_count"`
	QualifiedCases         int           `json:"qualified_cases"`
	RejectedShort          int           `json:"rejected_short"`
	RejectedSilence        int           `json:"rejected_silence"`
	Provider               string        `json:"provider"`
	Threads                int           `json:"threads"`
	SampleRate             int           `json:"sample_rate"`
	MinSamples             int           `json:"min_samples"`
	MinRMS                 float64       `json:"min_rms"`
	ColdStartMS            float64       `json:"cold_start_ms"`
	Native                 timingSummary `json:"native_inference"`
	Wall                   timingSummary `json:"wall_ipc_inference"`
	WorkerCPUms            float64       `json:"worker_cpu_ms"`
	WorkerMaxRSSKiB        int64         `json:"worker_max_rss_kib"`
	benchmarkAnalysis
}

func runBenchmark(ctx context.Context, model, sha, manifest string) (benchmarkReport, error) {
	r := benchmarkReport{SourceKind: "synthetic", OverlapAssessment: "unavailable", SegmentationAssessment: "unavailable", SingleSpeakerBasis: "synthetic_fixture_declaration_only", Evidence: "engineering_and_synthetic_only", ModelSHA: sha, Provider: "cpu", Threads: 1, SampleRate: sampleRate, MinSamples: minQualitySamples, MinRMS: minRMS}
	m, digest, e := loadManifest(manifest)
	if e != nil {
		return r, e
	}
	r.ManifestSHA = digest
	r.CaseCount = len(m.Cases)
	var cases []embeddedCase
	defer func() {
		for i := range cases {
			clear(cases[i].vector)
		}
	}()
	w, e := startWorker(ctx, model, sha)
	if e != nil {
		return r, e
	}
	defer w.Close()
	r.Dimension = w.dimension
	r.ColdStartMS = float64(w.coldStart.Microseconds()) / 1000
	var native, wall []float64
	var requestID uint64
	var end uint64
	for _, c := range m.Cases {
		pcm, e := loadFixture(c)
		if e != nil {
			return r, e
		}
		quality := qualityReason(pcm)
		item := embeddedCase{fixtureCase: c, quality: quality}
		if quality != "qualified" {
			clear(pcm)
			if quality == "short" {
				r.RejectedShort++
			} else {
				r.RejectedSilence++
			}
			cases = append(cases, item)
			continue
		}
		requestID++
		h := requestHeader{Version: 1, ID: requestID, Session: "synthetic_benchmark", Scope: "fixture", Generation: 1, Start: end, End: end + uint64(c.Samples), ModelSHA: sha, SampleRate: sampleRate, PCMBytes: len(pcm)}
		end = h.End
		started := time.Now()
		v, us, e := w.request(ctx, h, pcm)
		elapsed := float64(time.Since(started).Microseconds()) / 1000
		clear(pcm)
		if e != nil {
			return r, e
		}
		item.vector = v
		cases = append(cases, item)
		r.QualifiedCases++
		native = append(native, float64(us)/1000)
		wall = append(wall, elapsed)
	}
	// Reap before reading resource accounting, including the initial model load.
	w.Close()
	if w.cmd.ProcessState == nil {
		return r, errWorker
	}
	usage, ok := w.cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		return r, errWorker
	}
	r.WorkerCPUms = float64(usage.Utime.Sec+usage.Stime.Sec)*1000 + float64(usage.Utime.Usec+usage.Stime.Usec)/1000
	r.WorkerMaxRSSKiB = usage.Maxrss
	r.Native = summarizeTimes(native)
	r.Wall = summarizeTimes(wall)
	r.benchmarkAnalysis, e = analyzeBenchmark(cases, sha, w.dimension)
	return r, e
}
