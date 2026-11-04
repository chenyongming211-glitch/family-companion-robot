//go:build linux

package main

import (
	"context"
	"fmt"
	"math"
	"syscall"
)

// Every returned template is a new allocation owned by this caller. Input
// vectors remain owned exclusively by evaluationExtraction and are never cleared
// through the aliasing quality/scoring views.
func fuseEnrollment(vectors [][]float32, dimension int) ([]float32, error) {
	if len(vectors) == 0 {
		return nil, errProtocol
	}
	sum := make([]float32, dimension)
	ok := false
	defer func() {
		if !ok {
			clear(sum)
		}
	}()
	for _, v := range vectors {
		if !validVector(v, dimension) {
			return nil, errProtocol
		}
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		norm = math.Sqrt(norm)
		for i, x := range v {
			sum[i] += float32(float64(x) / norm / float64(len(vectors)))
		}
	}
	if !validVector(sum, dimension) {
		return nil, errProtocol
	}
	var norm float64
	for _, x := range sum {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	for i := range sum {
		sum[i] = float32(float64(sum[i]) / norm)
	}
	ok = true
	return sum, nil
}

type studyTop1 struct {
	largeTop1
	QualifiedTotal    int     `json:"qualified_total"`
	QualifiedAccuracy float64 `json:"qualified_accuracy"`
	AllAccuracy       float64 `json:"all_accuracy"`
}
type unknownCounts struct {
	Status                   string  `json:"status"`
	Total                    int     `json:"total"`
	QualifiedTotal           int     `json:"qualified_total"`
	FalseAccept              int     `json:"false_accept"`
	Rejected                 int     `json:"rejected"`
	FalseAcceptRate          float64 `json:"false_accept_rate"`
	QualifiedFalseAcceptRate float64 `json:"qualified_false_accept_rate"`
}
type unknownGroup struct {
	Group int `json:"group"`
	unknownCounts
}
type unknownPool struct {
	Candidates int `json:"candidates"`
	GroupCount int `json:"group_count"`
	unknownCounts
	Groups []unknownGroup `json:"groups,omitempty"`
}
type studyRule struct {
	Name              string            `json:"name"`
	Status            string            `json:"status"`
	CalibrationStatus string            `json:"calibration_status"`
	Threshold         float64           `json:"match_threshold"`
	Margin            float64           `json:"margin"`
	DifferentPairs    int               `json:"calibration_different_pairs"`
	SamePairs         int               `json:"calibration_same_pairs"`
	Budget            int               `json:"calibration_budget"`
	AcceptedDifferent int               `json:"calibration_accepted_different"`
	EmpiricalFAR      float64           `json:"calibration_empirical_far"`
	Known             []largePoolScores `json:"known"`
	Unknown           []unknownPool     `json:"unknown"`
}
type studyView struct {
	Enrollment                  string      `json:"enrollment"`
	Quality                     string      `json:"quality"`
	CalibrationSpeakers         int         `json:"calibration_speakers"`
	CalibrationQueries          int         `json:"calibration_queries"`
	CalibrationQualifiedQueries int         `json:"calibration_qualified_queries"`
	CalibrationQualityRejected  int         `json:"calibration_quality_rejected"`
	CalibrationStatus           string      `json:"calibration_status"`
	Rules                       []studyRule `json:"rules"`
	Top1                        []studyTop1 `json:"top1_diagnostic"`
}
type studyEnrollment struct {
	Speaker       string `json:"speaker"`
	Cases         int    `json:"cases"`
	RawSamples    int    `json:"raw_samples"`
	SpeechSamples int    `json:"speech_samples"`
	FirstID       string `json:"first_id"`
}

func unknownRates(c *unknownCounts) {
	if c.Total > 0 {
		c.FalseAcceptRate = float64(c.FalseAccept) / float64(c.Total)
	}
	if c.QualifiedTotal > 0 {
		c.QualifiedFalseAcceptRate = float64(c.FalseAccept) / float64(c.QualifiedTotal)
	}
}
func scoreStudyUnknown(cases []embeddedCase, groups []string, templates map[string][]float32, size int, threshold, margin float64) unknownPool {
	r := unknownPool{Candidates: size, GroupCount: 1, unknownCounts: unknownCounts{Status: "evaluated"}}
	if size == 5 {
		r.GroupCount = 5
		for i := 1; i <= 5; i++ {
			r.Groups = append(r.Groups, unknownGroup{Group: i, unknownCounts: unknownCounts{Status: "evaluated"}})
		}
	}
	for _, c := range cases {
		_, n := studySpeaker(c.Speaker)
		index := n - 1
		candidates := groups
		if size == 5 {
			candidates = groups[index*5 : index*5+5]
		}
		r.Total++
		var g *unknownCounts
		if size == 5 {
			g = &r.Groups[index].unknownCounts
			g.Total++
		}
		accepted := false
		if c.quality == "qualified" {
			r.QualifiedTotal++
			if g != nil {
				g.QualifiedTotal++
			}
			best, second, _ := bestLarge(c, candidates, templates)
			accepted = best >= threshold && best-second >= margin && best != second
		}
		if accepted {
			r.FalseAccept++
			if g != nil {
				g.FalseAccept++
			}
		} else {
			r.Rejected++
			if g != nil {
				g.Rejected++
			}
		}
	}
	unknownRates(&r.unknownCounts)
	for i := range r.Groups {
		unknownRates(&r.Groups[i].unknownCounts)
	}
	return r
}
func analyzeStudy(m studyManifest, a qualityAudit, cases []embeddedCase, dimension int) ([]studyView, error) {
	if len(cases) != len(m.Cases) {
		return nil, errProtocol
	}
	audits := map[string]auditCase{}
	for _, x := range a.Cases {
		audits[x.ID] = x
	}
	groups := make([]string, 50)
	calGroups := make([]string, 10)
	for i := range groups {
		groups[i] = fmt.Sprintf("fixture_%d", i+1)
	}
	for i := range calGroups {
		calGroups[i] = fmt.Sprintf("calibration_%d", i+1)
	}
	var result []studyView
	for _, mode := range []string{"short", "multi"} {
		templates := map[string][]float32{}
		bySpeaker := map[string][][]float32{}
		for i, c := range m.Cases {
			if c.Role == "known_enrollment" || c.Role == "calibration_enrollment" {
				if !validVector(cases[i].vector, dimension) {
					return nil, errProtocol
				}
				if mode == "multi" || len(bySpeaker[c.Speaker]) == 0 {
					bySpeaker[c.Speaker] = append(bySpeaker[c.Speaker], cases[i].vector)
				}
			}
		}
		// This function owns only newly fused templates. Clear exactly once even
		// when an invalid template causes an early return.
		e := func() error {
			defer func() {
				for _, v := range templates {
					clear(v)
				}
			}()
			for s, vectors := range bySpeaker {
				v, e := fuseEnrollment(vectors, dimension)
				if e != nil {
					return e
				}
				templates[s] = v
			}
			for _, s := range append(append([]string(nil), groups...), calGroups...) {
				if templates[s] == nil {
					return errProtocol
				}
			}
			for _, quality := range []string{"energy", "vad"} {
				view := studyView{Enrollment: mode, Quality: quality, CalibrationSpeakers: 10, CalibrationStatus: "calibrated"}
				var known, unknown, queries []embeddedCase
				counts := map[string]int{}
				for i, c := range m.Cases {
					item := cases[i]
					ac, ok := audits[c.ID]
					if !ok || ac.SHA != c.SHA || ac.Samples != c.Samples {
						return errProtocol
					}
					qualified := ac.Samples >= minQualitySamples && ac.RMS >= minRMS
					if quality == "vad" {
						qualified = ac.SpeechSamples >= 16000
					}
					item.quality = "rejected"
					if qualified {
						if !validVector(item.vector, dimension) {
							return errProtocol
						}
						item.quality = "qualified"
					}
					item.Split = "evaluation"
					switch c.Role {
					case "known_evaluation":
						known = append(known, item)
					case "unknown_evaluation":
						unknown = append(unknown, item)
					case "calibration_query":
						view.CalibrationQueries++
						if qualified {
							queries = append(queries, item)
							counts[c.Speaker]++
						}
					}
				}
				view.CalibrationQualifiedQueries = len(queries)
				view.CalibrationQualityRejected = view.CalibrationQueries - len(queries)
				for _, s := range calGroups {
					if counts[s] < 2 {
						view.CalibrationStatus = "failed_insufficient_calibration"
					}
				}
				var negatives []float64
				same := 0
				for _, q := range queries {
					for _, s := range calGroups {
						if q.Speaker == s {
							same++
						} else {
							negatives = append(negatives, similarity(q.vector, templates[s]))
						}
					}
				}
				for index, name := range []string{"current_session", "legacy_base_far001", "condition_far001", "condition_far01"} {
					rule := studyRule{Name: name, Status: "not_run", CalibrationStatus: view.CalibrationStatus, Margin: largeSessionMargin, DifferentPairs: len(negatives), SamePairs: same, Known: newLargePools(len(known)), Unknown: []unknownPool{{Candidates: 50, GroupCount: 1, unknownCounts: unknownCounts{Status: "not_run", Total: len(unknown)}}, {Candidates: 5, GroupCount: 5, unknownCounts: unknownCounts{Status: "not_run", Total: len(unknown)}}}}
					switch index {
					case 0:
						rule.Threshold = largeSessionMatch
						rule.CalibrationStatus = "fixed_session"
					case 1:
						rule.Threshold = .4499944095418535
						rule.CalibrationStatus = "fixed_legacy_base_reference"
					default:
						target := .001
						if index == 3 {
							target = .01
						}
						rule.Budget = int(math.Floor(float64(len(negatives)) * target))
						fit := fitEmpiricalFAR(negatives, target)
						rule.CalibrationStatus = fit.Status
						rule.Threshold = fit.MatchThreshold
					}
					if view.CalibrationStatus == "calibrated" && (index < 2 || rule.CalibrationStatus == "calibrated") {
						rule.Status = "evaluated"
						for _, s := range negatives {
							if s >= rule.Threshold {
								rule.AcceptedDifferent++
							}
						}
						if len(negatives) > 0 {
							rule.EmpiricalFAR = float64(rule.AcceptedDifferent) / float64(len(negatives))
						}
						for p, size := range []int{50, 5} {
							rule.Known[p] = scoreLargePool(known, groups, templates, size, rule.Threshold, rule.Margin)
							rule.Unknown[p] = scoreStudyUnknown(unknown, groups, templates, size, rule.Threshold, rule.Margin)
						}
					}
					view.Rules = append(view.Rules, rule)
				}
				for _, size := range []int{50, 5} {
					top := studyTop1{largeTop1: diagnoseLargeTop1(known, groups, templates, size)}
					top.QualifiedTotal = top.Total - top.QualityRejected
					if top.Total > 0 {
						top.AllAccuracy = float64(top.Correct) / float64(top.Total)
					}
					if top.QualifiedTotal > 0 {
						top.QualifiedAccuracy = float64(top.Correct) / float64(top.QualifiedTotal)
					}
					view.Top1 = append(view.Top1, top)
				}
				clear(negatives)
				result = append(result, view)
			}
			return nil
		}()
		if e != nil {
			return nil, e
		}
	}
	return result, nil
}

type studyReport struct {
	evaluationMetadata
	AuditSHA        string            `json:"quality_audit_sha256"`
	VADModelSHA     string            `json:"vad_model_sha256"`
	VADConfig       vadConfiguration  `json:"vad_config"`
	Provider        string            `json:"provider"`
	Threads         int               `json:"threads"`
	SampleRate      int               `json:"sample_rate"`
	AudioSeconds    float64           `json:"audio_seconds"`
	ColdStartMS     float64           `json:"cold_start_ms"`
	WorkerCPUms     float64           `json:"worker_cpu_ms"`
	WorkerMaxRSSKiB int64             `json:"worker_max_rss_kib"`
	ParentMaxRSSKiB int64             `json:"parent_max_rss_kib"`
	Enrollment      []studyEnrollment `json:"enrollment_summary"`
	Views           []studyView       `json:"views"`
}

func runEnrollmentStudy(ctx context.Context, model, sha, manifest, auditPath string) (studyReport, error) {
	r := studyReport{evaluationMetadata: newEvaluationReport(sha).evaluationMetadata, Provider: "cpu", Threads: 1, SampleRate: 16000, VADModelSHA: sileroSHA, VADConfig: fixedVADConfig()}
	m, a, digest, e := loadStudyManifest(manifest, auditPath)
	if e != nil {
		return r, e
	}
	r.ManifestSHA, r.AuditSHA, r.CaseCount = digest, m.AuditSHA, len(m.Cases)
	em := evaluationManifest{}
	audit := map[string]auditCase{}
	for _, c := range a.Cases {
		audit[c.ID] = c
	}
	summaries := map[string]int{}
	for _, c := range m.Cases {
		r.AudioSeconds += float64(c.Samples) / sampleRate
		em.Cases = append(em.Cases, evaluationCase{ID: c.ID, Speaker: c.Speaker, Split: "evaluation", Path: c.Path, Samples: c.Samples, SHA: c.SHA, RecordingSHA: c.RecordingSHA, TextSHA: c.TextSHA})
		if c.Role == "known_enrollment" || c.Role == "calibration_enrollment" {
			index, ok := summaries[c.Speaker]
			if !ok {
				index = len(r.Enrollment)
				summaries[c.Speaker] = index
				r.Enrollment = append(r.Enrollment, studyEnrollment{Speaker: c.Speaker, FirstID: c.ID})
			}
			s := &r.Enrollment[index]
			s.Cases++
			s.RawSamples += c.Samples
			s.SpeechSamples += audit[c.ID].SpeechSamples
		}
	}
	ex, e := extractEvaluationWithGate(ctx, em, model, sha, startWorker, func(c evaluationCase, pcm []byte) bool { return audit[c.ID].SpeechSamples >= 16000 })
	if e != nil {
		return r, e
	}
	defer ex.clearVectors()
	r.Dimension, r.QualifiedCases, r.RejectedShort, r.RejectedSilence = ex.dimension, ex.qualified, ex.short, ex.silence
	r.Native, r.Wall, r.ColdStartMS, r.WorkerCPUms, r.WorkerMaxRSSKiB = ex.native, ex.wall, ex.coldMS, ex.cpuMS, ex.workerRSS
	r.Views, e = analyzeStudy(m, a, ex.cases, ex.dimension)
	if e != nil {
		return r, e
	}
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return r, errWorker
	}
	r.ParentMaxRSSKiB = usage.Maxrss
	return r, nil
}
