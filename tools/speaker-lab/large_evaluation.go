//go:build linux

package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const largeSpeakers = 50
const largeSessionMatch = .682987
const largeSessionMargin = .030229

func largeSpeaker(s string) bool {
	if !strings.HasPrefix(s, "fixture_") {
		return false
	}
	n, e := strconv.Atoi(strings.TrimPrefix(s, "fixture_"))
	return e == nil && n >= 1 && n <= largeSpeakers && s == fmt.Sprintf("fixture_%d", n)
}
func loadLargeEvaluationManifest(path string) (evaluationManifest, string, error) {
	return loadEvaluationManifestWithLimits(path, evaluationLimits{2 * 1024 * 1024, 400, 2048, 50, 50, [3]int{1, 3, 4}, largeSpeaker})
}

type empiricalFAR struct {
	Status            string  `json:"status"`
	MatchThreshold    float64 `json:"match_threshold"`
	Target            float64 `json:"target"`
	DifferentPairs    int     `json:"different_pairs"`
	AcceptedDifferent int     `json:"accepted_different"`
	EmpiricalFAR      float64 `json:"empirical_far"`
}

// Match accepts score >= threshold. Exclude the boundary with Nextafter so ties
// are never partly admitted. Evaluation scores never enter this calculation.
func fitEmpiricalFAR(scores []float64, target float64) empiricalFAR {
	r := empiricalFAR{Status: "failed_insufficient_calibration", Target: target, DifferentPairs: len(scores)}
	if len(scores) == 0 || math.IsNaN(target) || target < 0 || target > 1 {
		return r
	}
	ordered := append([]float64(nil), scores...)
	defer clear(ordered)
	for _, s := range ordered {
		if math.IsNaN(s) || math.IsInf(s, 0) || s < -1 || s > 1 {
			r.Status = "failed_invalid_calibration"
			return r
		}
	}
	sort.Float64s(ordered)
	budget := int(math.Floor(float64(len(ordered)) * target))
	threshold := -1.0
	if budget < len(ordered) {
		threshold = math.Nextafter(ordered[len(ordered)-budget-1], math.Inf(1))
	}
	if threshold > 1 {
		r.Status = "unreachable"
		return r
	}
	r.Status = "calibrated"
	r.MatchThreshold = threshold
	for _, s := range scores {
		if s >= threshold {
			r.AcceptedDifferent++
		}
	}
	r.EmpiricalFAR = float64(r.AcceptedDifferent) / float64(len(scores))
	// Recount rather than assuming budget clips are admitted: ties may reduce it.
	if r.EmpiricalFAR > target {
		r.Status = "unreachable"
	}
	return r
}

type evaluationPairScores struct {
	SameAccepted      int `json:"same_accepted"`
	SameRejected      int `json:"same_rejected"`
	DifferentAccepted int `json:"different_accepted"`
	DifferentRejected int `json:"different_rejected"`
}
type largeGroupScores struct {
	Group int `json:"group"`
	evaluationScores
}
type largePoolScores struct {
	Candidates int `json:"candidates"`
	GroupCount int `json:"group_count"`
	evaluationScores
	Pairs  evaluationPairScores `json:"evaluation_template_pairs"`
	Groups []largeGroupScores   `json:"groups,omitempty"`
}
type largeTop1 struct {
	Candidates      int    `json:"candidates"`
	Status          string `json:"status"`
	Total           int    `json:"total"`
	Correct         int    `json:"correct"`
	Tied            int    `json:"tied"`
	QualityRejected int    `json:"quality_rejected"`
}
type largeRuleResult struct {
	Name                 string            `json:"name"`
	Status               string            `json:"status"`
	CalibrationStatus    string            `json:"calibration_status"`
	MatchThreshold       float64           `json:"match_threshold"`
	Margin               float64           `json:"margin"`
	TargetCalibrationFAR *float64          `json:"target_calibration_far,omitempty"`
	DifferentPairs       int               `json:"calibration_different_pairs"`
	AcceptedDifferent    int               `json:"calibration_accepted_different"`
	EmpiricalFAR         float64           `json:"calibration_empirical_far"`
	Pools                []largePoolScores `json:"pools"`
}
type largeEvaluationAnalysis struct {
	Calibration evaluationCalibration `json:"calibration"`
	Rules       []largeRuleResult     `json:"rules"`
	Top1        []largeTop1           `json:"top1_diagnostic"`
}

func scoreRates(s *evaluationScores) {
	if s.Total > 0 {
		s.Accuracy = float64(s.Correct) / float64(s.Total)
		s.EffectiveRate = float64(s.Correct+s.Wrong) / float64(s.Total)
	}
}
func newLargePools(total int) []largePoolScores {
	pools := []largePoolScores{{Candidates: 50, GroupCount: 1}, {Candidates: 5, GroupCount: 10}}
	for i := range pools {
		pools[i].evaluationScores = evaluationScores{Status: "not_run", Total: total}
	}
	return pools
}
func analyzeLargeEvaluation(cases []embeddedCase, dimension int) (largeEvaluationAnalysis, error) {
	r := largeEvaluationAnalysis{}
	groups := make([]string, 50)
	for i := range groups {
		groups[i] = fmt.Sprintf("fixture_%d", i+1)
	}
	templates := map[string][]float32{}
	seen := map[string]bool{}
	total := 0
	for _, c := range cases {
		if !largeSpeaker(c.Speaker) {
			return r, errProtocol
		}
		seen[c.Speaker] = true
		if c.Split == "evaluation" {
			total++
		} else if c.Split != "enrollment" && c.Split != "calibration" {
			return r, errProtocol
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
	if len(seen) != 50 {
		return r, errProtocol
	}
	for _, name := range []string{"current_session", "strict_separation", "calibration_far_001", "calibration_far_01"} {
		r.Rules = append(r.Rules, largeRuleResult{Name: name, Status: "not_run", Pools: newLargePools(total)})
	}
	r.Rules[0].MatchThreshold = largeSessionMatch
	r.Rules[0].Margin = largeSessionMargin
	for i, target := range []float64{.001, .01} {
		r.Rules[i+2].TargetCalibrationFAR = new(float64)
		*r.Rules[i+2].TargetCalibrationFAR = target
		r.Rules[i+2].Margin = largeSessionMargin
	}
	r.Top1 = []largeTop1{{Candidates: 50, Status: "not_run", Total: total}, {Candidates: 5, Status: "not_run", Total: total}}
	r.Calibration = fitEvaluationMinimum(cases, groups, 3)
	for _, g := range groups {
		if templates[g] == nil {
			r.Calibration.Status = "failed_missing_enrollment"
			break
		}
	}
	for i := range r.Rules {
		r.Rules[i].CalibrationStatus = r.Calibration.Status
	}
	if r.Calibration.Status == "failed_missing_enrollment" || r.Calibration.Status == "failed_insufficient_calibration" {
		return r, nil
	}
	// All qualified calibration clips contribute, including later clips.
	var fit []embeddedCase
	for _, c := range cases {
		if c.Split == "calibration" && c.quality == "qualified" {
			fit = append(fit, c)
		}
	}
	var negatives []float64
	for i, c := range fit {
		for j := 0; j < i; j++ {
			if c.Speaker != fit[j].Speaker {
				negatives = append(negatives, similarity(c.vector, fit[j].vector))
			}
		}
	}
	defer clear(negatives)
	current := &r.Rules[0]
	current.Status = "evaluated"
	current.CalibrationStatus = "fixed_session"
	current.MatchThreshold = largeSessionMatch
	current.Margin = largeSessionMargin
	strict := &r.Rules[1]
	if r.Calibration.Status == "separable" {
		strict.Status = "evaluated"
		strict.MatchThreshold = r.Calibration.MatchThreshold
		strict.Margin = r.Calibration.Margin
	}
	for i, target := range []float64{.001, .01} {
		far := fitEmpiricalFAR(negatives, target)
		rule := &r.Rules[i+2]
		rule.TargetCalibrationFAR = new(float64)
		*rule.TargetCalibrationFAR = target
		rule.CalibrationStatus = far.Status
		rule.MatchThreshold = far.MatchThreshold
		rule.Margin = largeSessionMargin
		if far.Status == "calibrated" {
			rule.Status = "evaluated"
		}
	}
	for i := range r.Rules {
		rule := &r.Rules[i]
		rule.DifferentPairs = len(negatives)
		if rule.Status != "evaluated" {
			continue
		}
		for _, v := range negatives {
			if v >= rule.MatchThreshold {
				rule.AcceptedDifferent++
			}
		}
		rule.EmpiricalFAR = float64(rule.AcceptedDifferent) / float64(len(negatives))
		for p := range rule.Pools {
			rule.Pools[p] = scoreLargePool(cases, groups, templates, rule.Pools[p].Candidates, rule.MatchThreshold, rule.Margin)
		}
	}
	for p := range r.Top1 {
		r.Top1[p] = diagnoseLargeTop1(cases, groups, templates, r.Top1[p].Candidates)
	}
	return r, nil
}

func candidateGroups(groups []string, speaker string, size int) ([]string, int) {
	if size == 50 {
		return groups, 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(speaker, "fixture_"))
	index := (n - 1) / 5
	return groups[index*5 : index*5+5], index
}
func bestLarge(c embeddedCase, candidates []string, templates map[string][]float32) (best, second float64, owner string) {
	best, second = math.Inf(-1), math.Inf(-1)
	for _, g := range candidates {
		score := similarity(c.vector, templates[g])
		if score > best {
			second, best, owner = best, score, g
		} else if score > second {
			second = score
		}
	}
	return
}
func scoreLargePool(cases []embeddedCase, groups []string, templates map[string][]float32, size int, threshold, margin float64) largePoolScores {
	r := largePoolScores{Candidates: size, GroupCount: 50 / size, evaluationScores: evaluationScores{Status: "evaluated"}}
	counts := make([]largeGroupScores, 50/size)
	for i := range counts {
		counts[i] = largeGroupScores{Group: i + 1, evaluationScores: evaluationScores{Status: "evaluated"}}
	}
	for _, c := range cases {
		if c.Split != "evaluation" {
			continue
		}
		candidates, index := candidateGroups(groups, c.Speaker, size)
		s := &counts[index].evaluationScores
		s.Total++
		if c.quality != "qualified" {
			s.Rejected++
			r.Pairs.SameRejected++
			r.Pairs.DifferentRejected += size - 1
			continue
		}
		for _, g := range candidates {
			accepted := similarity(c.vector, templates[g]) >= threshold
			if g == c.Speaker {
				if accepted {
					r.Pairs.SameAccepted++
				} else {
					r.Pairs.SameRejected++
				}
			} else {
				if accepted {
					r.Pairs.DifferentAccepted++
				} else {
					r.Pairs.DifferentRejected++
				}
			}
		}
		best, second, owner := bestLarge(c, candidates, templates)
		if best < threshold || best-second < margin || best == second {
			s.Rejected++
		} else if owner == c.Speaker {
			s.Correct++
		} else {
			s.Wrong++
		}
	}
	for i := range counts {
		s := &counts[i].evaluationScores
		scoreRates(s)
		r.Total += s.Total
		r.Correct += s.Correct
		r.Wrong += s.Wrong
		r.Rejected += s.Rejected
	}
	scoreRates(&r.evaluationScores)
	if size == 5 {
		r.Groups = counts
	}
	return r
}
func diagnoseLargeTop1(cases []embeddedCase, groups []string, templates map[string][]float32, size int) largeTop1 {
	r := largeTop1{Candidates: size, Status: "diagnostic_only"}
	for _, c := range cases {
		if c.Split != "evaluation" {
			continue
		}
		r.Total++
		if c.quality != "qualified" {
			r.QualityRejected++
			continue
		}
		candidates, _ := candidateGroups(groups, c.Speaker, size)
		best, second, owner := bestLarge(c, candidates, templates)
		if best == second {
			r.Tied++
		} else if owner == c.Speaker {
			r.Correct++
		}
	}
	return r
}

type largeEvaluationReport struct {
	evaluationMetadata
	Speakers         int     `json:"speakers"`
	EnrollmentCases  int     `json:"enrollment_cases"`
	CalibrationCases int     `json:"calibration_cases"`
	EvaluationCases  int     `json:"evaluation_cases"`
	AudioSeconds     float64 `json:"audio_seconds"`
	Provider         string  `json:"provider"`
	Threads          int     `json:"threads"`
	SampleRate       int     `json:"sample_rate"`
	MinSamples       int     `json:"min_samples"`
	MinRMS           float64 `json:"min_rms"`
	ColdStartMS      float64 `json:"cold_start_ms"`
	WorkerCPUms      float64 `json:"worker_cpu_ms"`
	WorkerMaxRSSKiB  int64   `json:"worker_max_rss_kib"`
	ParentMaxRSSKiB  int64   `json:"parent_max_rss_kib"`
	largeEvaluationAnalysis
}

func newLargeEvaluationReport(sha string) largeEvaluationReport {
	return largeEvaluationReport{evaluationMetadata: newEvaluationReport(sha).evaluationMetadata, Provider: "cpu", Threads: 1, SampleRate: sampleRate, MinSamples: minQualitySamples, MinRMS: minRMS}
}
func runLargeEvaluation(ctx context.Context, model, sha, manifest string) (largeEvaluationReport, error) {
	r := newLargeEvaluationReport(sha)
	m, digest, e := loadLargeEvaluationManifest(manifest)
	if e != nil {
		return r, e
	}
	r.ManifestSHA = digest
	r.CaseCount = len(m.Cases)
	r.Speakers = 50
	for _, c := range m.Cases {
		r.AudioSeconds += float64(c.Samples) / sampleRate
		switch c.Split {
		case "enrollment":
			r.EnrollmentCases++
		case "calibration":
			r.CalibrationCases++
		case "evaluation":
			r.EvaluationCases++
		}
	}
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
	r.ColdStartMS = extracted.coldMS
	r.WorkerCPUms = extracted.cpuMS
	r.WorkerMaxRSSKiB = extracted.workerRSS
	r.largeEvaluationAnalysis, e = analyzeLargeEvaluation(extracted.cases, extracted.dimension)
	var usage syscall.Rusage
	if e == nil {
		if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
			return r, errWorker
		}
		r.ParentMaxRSSKiB = usage.Maxrss
	}
	return r, e
}
