//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/url"
	"path/filepath"
	"syscall"
	"time"
)

// The public corpus has a separate schema and never enters synthetic benchmark.
type evaluationCase struct {
	ID           string `json:"id"`
	Speaker      string `json:"speaker"`
	Split        string `json:"split"`
	Path         string `json:"path"`
	Samples      int    `json:"samples"`
	SHA          string `json:"sha256"`
	RecordingSHA string `json:"recording_sha256"`
	TextSHA      string `json:"text_sha256"`
}

func (c evaluationCase) fixture() fixtureCase {
	return fixtureCase{ID: c.ID, Speaker: c.Speaker, Split: c.Split, Path: c.Path, Samples: c.Samples, SHA: c.SHA, TextSHA: c.TextSHA}
}

type evaluationManifest struct {
	SourceKind         string           `json:"source_kind"`
	SourceID           string           `json:"source_id"`
	SourceURL          string           `json:"source_url"`
	LicenseURL         string           `json:"license_url"`
	SingleSpeakerBasis string           `json:"single_speaker_basis"`
	SampleRate         int              `json:"sample_rate"`
	Cases              []evaluationCase `json:"cases"`
}

func httpsReference(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Opaque == ""
}

type evaluationLimits struct {
	manifestBytes                                int64
	minCases, maxCases, minSpeakers, maxSpeakers int
	minSplits                                    [3]int
	validSpeaker                                 func(string) bool
}

func loadEvaluationManifest(path string) (evaluationManifest, string, error) {
	return loadEvaluationManifestWithLimits(path, evaluationLimits{maxManifestBytes, 12, 128, 3, 8, [3]int{1, 2, 1}, fixtureSpeakerPattern.MatchString})
}

// Limits belong to the explicit command, never global or user-supplied flags.
func loadEvaluationManifestWithLimits(path string, limits evaluationLimits) (evaluationManifest, string, error) {
	var m evaluationManifest
	b, e := boundedFile(path, limits.manifestBytes)
	if e != nil {
		return m, "", e
	}
	defer clear(b)
	// Decode each case separately so strictHeader can reject duplicate/missing keys.
	var raw struct {
		SourceKind         string            `json:"source_kind"`
		SourceID           string            `json:"source_id"`
		SourceURL          string            `json:"source_url"`
		LicenseURL         string            `json:"license_url"`
		SingleSpeakerBasis string            `json:"single_speaker_basis"`
		SampleRate         int               `json:"sample_rate"`
		Cases              []json.RawMessage `json:"cases"`
	}
	if strictHeader(b, &raw) != nil || raw.SourceKind != "public_human" || !tokenPattern.MatchString(raw.SourceID) || !httpsReference(raw.SourceURL) || !httpsReference(raw.LicenseURL) || raw.SingleSpeakerBasis != "source_annotations" || raw.SampleRate != sampleRate || len(raw.Cases) < limits.minCases || len(raw.Cases) > limits.maxCases {
		return m, "", errProtocol
	}
	m = evaluationManifest{SourceKind: raw.SourceKind, SourceID: raw.SourceID, SourceURL: raw.SourceURL, LicenseURL: raw.LicenseURL, SingleSpeakerBasis: raw.SingleSpeakerBasis, SampleRate: raw.SampleRate}
	ids, paths, audio := map[string]bool{}, map[string]bool{}, map[string]bool{}
	recordings, texts := map[string]string{}, map[string]string{}
	counts := map[string][3]int{}
	for _, r := range raw.Cases {
		var c evaluationCase
		if strictHeader(r, &c) != nil || !tokenPattern.MatchString(c.ID) || ids[c.ID] || !limits.validSpeaker(c.Speaker) || !filepath.IsAbs(c.Path) || filepath.Clean(c.Path) != c.Path || paths[c.Path] || c.Samples < 1 || c.Samples > maxSamples || !shaPattern.MatchString(c.SHA) || audio[c.SHA] || !shaPattern.MatchString(c.RecordingSHA) || !shaPattern.MatchString(c.TextSHA) {
			return evaluationManifest{}, "", errProtocol
		}
		var index int
		switch c.Split {
		case "enrollment":
			index = 0
		case "calibration":
			index = 1
		case "evaluation":
			index = 2
		default:
			return evaluationManifest{}, "", errProtocol
		}
		if old, ok := recordings[c.RecordingSHA]; ok && old != c.Split {
			return evaluationManifest{}, "", errProtocol
		}
		if old, ok := texts[c.TextSHA]; ok && old != c.Split {
			return evaluationManifest{}, "", errProtocol
		}
		ids[c.ID], paths[c.Path], audio[c.SHA] = true, true, true
		recordings[c.RecordingSHA], texts[c.TextSHA] = c.Split, c.Split
		n := counts[c.Speaker]
		n[index]++
		counts[c.Speaker] = n
		m.Cases = append(m.Cases, c)
	}
	if len(counts) < limits.minSpeakers || len(counts) > limits.maxSpeakers {
		return evaluationManifest{}, "", errProtocol
	}
	for _, n := range counts {
		if n[0] < limits.minSplits[0] || n[1] < limits.minSplits[1] || n[2] < limits.minSplits[2] {
			return evaluationManifest{}, "", errProtocol
		}
	}
	h := sha256.Sum256(b)
	return m, hex.EncodeToString(h[:]), nil
}

type evaluationCalibration struct {
	Status         string  `json:"status"`
	FittedSpeakers int     `json:"fitted_speakers"`
	FittedCases    int     `json:"fitted_cases"`
	SamePairs      int     `json:"same_pairs"`
	DifferentPairs int     `json:"different_pairs"`
	SameMin        float64 `json:"same_min"`
	SameMax        float64 `json:"same_max"`
	DifferentMin   float64 `json:"different_min"`
	DifferentMax   float64 `json:"different_max"`
	MatchThreshold float64 `json:"match_threshold"`
	Margin         float64 `json:"margin"`
}
type evaluationScores struct {
	Status        string  `json:"status"`
	Total         int     `json:"total"`
	Correct       int     `json:"correct_matches"`
	Wrong         int     `json:"wrong_matches"`
	Rejected      int     `json:"rejected"`
	Accuracy      float64 `json:"accuracy"`
	EffectiveRate float64 `json:"effective_rate"`
}
type evaluationAnalysis struct {
	Calibration evaluationCalibration `json:"calibration"`
	Evaluation  evaluationScores      `json:"evaluation"`
}

// All qualified calibration clips and all unordered pairs participate. No
// enrollment or evaluation vector affects the frozen threshold or margin.
func fitEvaluation(cases []embeddedCase, groups []string) evaluationCalibration {
	return fitEvaluationMinimum(cases, groups, 2)
}
func fitEvaluationMinimum(cases []embeddedCase, groups []string, minimum int) evaluationCalibration {
	r := evaluationCalibration{Status: "failed_insufficient_calibration"}
	var fit []embeddedCase
	counts := map[string]int{}
	for _, c := range cases {
		if c.Split == "calibration" && c.quality == "qualified" {
			fit = append(fit, c)
			counts[c.Speaker]++
		}
	}
	r.FittedCases = len(fit)
	r.FittedSpeakers = len(counts)
	for _, g := range groups {
		if counts[g] < minimum {
			return r
		}
	}
	if len(groups) < 3 {
		return r
	}
	r.SameMin, r.DifferentMin = 1, 1
	r.SameMax, r.DifferentMax = -1, -1
	for i, c := range fit {
		for j := 0; j < i; j++ {
			v := similarity(c.vector, fit[j].vector)
			if c.Speaker == fit[j].Speaker {
				r.SamePairs++
				r.SameMin = math.Min(r.SameMin, v)
				r.SameMax = math.Max(r.SameMax, v)
			} else {
				r.DifferentPairs++
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
	r.MatchThreshold = r.DifferentMax + gap/2
	r.Margin = gap / 8
	return r
}
func analyzeEvaluation(cases []embeddedCase, dimension int) (evaluationAnalysis, error) {
	r := evaluationAnalysis{Evaluation: evaluationScores{Status: "not_run"}}
	groups := groupCases(cases)
	templates := map[string][]float32{}
	for _, c := range cases {
		if c.Split == "evaluation" {
			r.Evaluation.Total++
		}
		if c.quality != "qualified" {
			continue
		}
		if !validVector(c.vector, dimension) {
			return r, errProtocol
		}
		if c.Split == "enrollment" && templates[c.Speaker] == nil {
			templates[c.Speaker] = c.vector
		}
	}
	for _, g := range groups {
		if templates[g] == nil {
			r.Calibration.Status = "failed_missing_enrollment"
			return r, nil
		}
	}
	r.Calibration = fitEvaluation(cases, groups)
	if r.Calibration.Status != "separable" {
		return r, nil
	}
	r.Evaluation.Status = "evaluated"
	for _, c := range cases {
		if c.Split != "evaluation" {
			continue
		}
		if c.quality != "qualified" {
			r.Evaluation.Rejected++
			continue
		}
		best, second := math.Inf(-1), math.Inf(-1)
		owner := ""
		for _, g := range groups {
			score := similarity(c.vector, templates[g])
			if score > best {
				second, best, owner = best, score, g
			} else if score > second {
				second = score
			}
		}
		// Ties and insufficient evidence are refusals, never silently attributed.
		if best < r.Calibration.MatchThreshold || best-second < r.Calibration.Margin || best == second {
			r.Evaluation.Rejected++
		} else if owner == c.Speaker {
			r.Evaluation.Correct++
		} else {
			r.Evaluation.Wrong++
		}
	}
	if r.Evaluation.Total > 0 {
		r.Evaluation.Accuracy = float64(r.Evaluation.Correct) / float64(r.Evaluation.Total)
		r.Evaluation.EffectiveRate = float64(r.Evaluation.Correct+r.Evaluation.Wrong) / float64(r.Evaluation.Total)
	}
	return r, nil
}

type evaluationMetadata struct {
	SourceKind             string        `json:"source_kind"`
	HumanAcceptance        bool          `json:"human_acceptance"`
	ProductionAcceptance   bool          `json:"production_acceptance"`
	FeatureEnabled         bool          `json:"feature_enabled"`
	SegmentationAssessment string        `json:"segmentation_assessment"`
	OverlapAssessment      string        `json:"overlap_assessment"`
	OpenSetAssessment      string        `json:"open_set_assessment"`
	SingleSpeakerBasis     string        `json:"single_speaker_basis"`
	ModelSHA               string        `json:"model_sha256"`
	ManifestSHA            string        `json:"manifest_sha256"`
	Dimension              int           `json:"dimension"`
	CaseCount              int           `json:"case_count"`
	QualifiedCases         int           `json:"qualified_cases"`
	RejectedShort          int           `json:"rejected_short"`
	RejectedSilence        int           `json:"rejected_silence"`
	Native                 timingSummary `json:"native_inference"`
	Wall                   timingSummary `json:"wall_ipc_inference"`
}

type evaluationReport struct {
	evaluationMetadata
	evaluationAnalysis
}

func newEvaluationReport(sha string) evaluationReport {
	return evaluationReport{evaluationMetadata: evaluationMetadata{SourceKind: "public_human", ModelSHA: sha, SingleSpeakerBasis: "source_annotations", SegmentationAssessment: "unavailable", OverlapAssessment: "unavailable", OpenSetAssessment: "unavailable"}}
}

// Successful extraction transfers vector ownership to the caller. Errors clear
// every accumulated vector here; PCM is cleared before advancing or returning.
type evaluationExtraction struct {
	cases                                []embeddedCase
	dimension, qualified, short, silence int
	native, wall                         timingSummary
	coldMS, cpuMS                        float64
	workerRSS                            int64
}

func (r *evaluationExtraction) clearVectors() {
	for i := range r.cases {
		clear(r.cases[i].vector)
	}
}

type evaluationWorkerStart func(context.Context, string, string) (*workerProcess, error)

func extractEvaluation(ctx context.Context, m evaluationManifest, model, sha string, start evaluationWorkerStart) (r evaluationExtraction, err error) {
	defer func() {
		if err != nil {
			r.clearVectors()
			r.cases = nil
		}
	}()
	w, e := start(ctx, model, sha)
	if e != nil {
		return r, e
	}
	defer w.Close()
	r.dimension = w.dimension
	r.coldMS = float64(w.coldStart.Microseconds()) / 1000
	var native, wall []float64
	var id, end uint64
	for _, c := range m.Cases {
		if ctx.Err() != nil {
			return r, errWorker
		}
		pcm, e := loadFixture(c.fixture())
		if e != nil {
			return r, e
		}
		quality := qualityReason(pcm)
		item := embeddedCase{fixtureCase: c.fixture(), quality: quality}
		if quality != "qualified" {
			clear(pcm)
			if quality == "short" {
				r.short++
			} else {
				r.silence++
			}
			r.cases = append(r.cases, item)
			continue
		}
		id++
		h := requestHeader{Version: 1, ID: id, Session: "public_evaluation", Scope: "fixture", Generation: 1, Start: end, End: end + uint64(c.Samples), ModelSHA: sha, SampleRate: sampleRate, PCMBytes: len(pcm)}
		end = h.End
		started := time.Now()
		v, us, e := w.request(ctx, h, pcm)
		elapsed := float64(time.Since(started).Microseconds()) / 1000
		clear(pcm)
		if e != nil {
			clear(v)
			return r, e
		}
		item.vector = v
		r.cases = append(r.cases, item)
		r.qualified++
		if c.Split == "evaluation" {
			native = append(native, float64(us)/1000)
			wall = append(wall, elapsed)
		}
	}
	w.Close()
	if w.cmd.ProcessState == nil {
		return r, errWorker
	}
	usage, ok := w.cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		return r, errWorker
	}
	r.cpuMS = float64(usage.Utime.Sec+usage.Stime.Sec)*1000 + float64(usage.Utime.Usec+usage.Stime.Usec)/1000
	r.workerRSS = usage.Maxrss
	r.native = summarizeTimes(native)
	r.wall = summarizeTimes(wall)
	return r, nil
}
func runEvaluation(ctx context.Context, model, sha, manifest string) (evaluationReport, error) {
	r := newEvaluationReport(sha)
	m, digest, e := loadEvaluationManifest(manifest)
	if e != nil {
		return r, e
	}
	r.ManifestSHA = digest
	r.CaseCount = len(m.Cases)
	extracted, e := extractEvaluation(ctx, m, model, sha, startWorker)
	if e != nil {
		return r, e
	}
	defer extracted.clearVectors()
	r.Dimension = extracted.dimension
	r.QualifiedCases = extracted.qualified
	r.RejectedShort = extracted.short
	r.RejectedSilence = extracted.silence
	r.Native = extracted.native
	r.Wall = extracted.wall
	r.evaluationAnalysis, e = analyzeEvaluation(extracted.cases, extracted.dimension)
	return r, e
}
