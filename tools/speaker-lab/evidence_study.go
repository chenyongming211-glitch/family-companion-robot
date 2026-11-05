//go:build linux

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"
)

type evidenceScenario struct {
	CalibrationMS float64                 `json:"calibration_ms"`
	ScoringMS     float64                 `json:"scoring_ms"`
	Name          string                  `json:"scenario"`
	Pools         []backendPool           `json:"pools"`
	Replay        []evidenceReplaySummary `json:"replay_by_pool"`
}
type evidenceArm struct {
	EnrollmentDiagnostic bool               `json:"enrollment_diagnostic_enabled"`
	Accumulation         bool               `json:"accumulation_requested"`
	Name                 string             `json:"arm"`
	Scenarios            []evidenceScenario `json:"scenarios"`
}
type evidenceReport struct {
	evaluationMetadata
	Condition                 string               `json:"condition"`
	Policy                    evidencePolicy       `json:"policy"`
	Diagnostics               []evidenceDiagnostic `json:"enrollment_diagnostics"`
	Enrollment                []backendEnrollment  `json:"enrollment_summary"`
	Arms                      []evidenceArm        `json:"arms"`
	EvidenceBasis             string               `json:"sequence_evidence_basis"`
	Population                string               `json:"population"`
	NewBlindTest              bool                 `json:"new_blind_test"`
	HouseholdAssessment       string               `json:"household_assessment"`
	EndToEndLatencyAssessment string               `json:"end_to_end_latency_assessment"`
	FusionBasis               string               `json:"fusion_basis"`
	AuditSHAs                 []string             `json:"quality_audit_sha256"`
	VADModelSHA               string               `json:"vad_model_sha256"`
	VADConfig                 vadConfiguration     `json:"vad_config"`
	Provider                  string               `json:"provider"`
	Threads                   int                  `json:"threads"`
	SampleRate                int                  `json:"sample_rate"`
	AudioSeconds              float64              `json:"audio_seconds"`
	ColdStartMS               float64              `json:"cold_start_ms"`
	WorkerCPUms               float64              `json:"worker_cpu_ms"`
	WorkerMaxRSSKiB           int64                `json:"worker_max_rss_kib"`
	ParentMaxRSSKiB           int64                `json:"parent_max_rss_kib"`
	Roles                     []backendRoleCount   `json:"role_quality_counts"`
	TotalMS                   float64              `json:"analysis_total_ms"`
}

// analyzeEvidence accepts already extracted vectors, including explicitly tagged
// synthetic fixtures. The public CLI always uses the unchanged strict loader.
func analyzeEvidence(ctx context.Context, m backendManifest, audit map[string]auditCase, cases []embeddedCase, dimension int) (evidenceReport, error) {
	started := time.Now()
	r := evidenceReport{evaluationMetadata: newEvaluationReport("").evaluationMetadata, Condition: m.Condition, EvidenceBasis: "constructed_sequence", Population: "seen_development_regression", HouseholdAssessment: "unavailable", EndToEndLatencyAssessment: "unavailable", FusionBasis: "qualified_segment_normalized_sample_weighted_vectors"}
	r.SourceKind = m.SourceKind
	r.SingleSpeakerBasis = m.SingleSpeakerBasis
	if m.SourceKind == "synthetic_tts" {
		r.Population = "synthetic_engineering_fixture"
	}
	if m.SourceKind == "public_human" {
		r.OpenSetAssessment = "limited_public_unknown_cohort"
	}
	if e := ctx.Err(); e != nil {
		return r, e
	}
	if len(cases) != len(m.Cases) || dimension < 1 || dimension > maxDimension {
		return r, errProtocol
	}
	r.Dimension = dimension
	r.CaseCount = len(cases)
	r.SampleRate = sampleRate
	bySpeaker := map[string][]evidenceLabeledVector{}
	summaries := map[string]backendEnrollment{}
	templates := map[string][]float32{}
	defer func() {
		for _, v := range templates {
			clear(v)
		}
	}()
	var trainEnrollment, trainQuery []evidenceLabeledVector
	labels := make([]string, len(cases))
	testN := 0
	roles := map[string]backendRoleCount{}
	for i, c := range m.Cases {
		if e := ctx.Err(); e != nil {
			return r, e
		}
		ac, ok := audit[c.SHA]
		group, person := backendSpeaker(c.Speaker)
		if !ok || ac.Samples != c.Samples || group == "" || c.Samples < 1 || c.Samples > 128000 {
			return r, errProtocol
		}
		if cases[i].quality == "qualified" && !validVector(cases[i].vector, dimension) {
			return r, errProtocol
		}
		if group == "test" {
			testN = max(testN, person)
		}
		labels[i] = c.Speaker
		role := roles[c.Role]
		role.Role = c.Role
		role.Total++
		energy := cases[i].quality == "qualified"
		switch cases[i].quality {
		case "qualified":
			r.QualifiedCases++
		case "short":
			r.RejectedShort++
		case "silence":
			r.RejectedSilence++
		default:
			return r, errProtocol
		}
		r.AudioSeconds += float64(c.Samples) / sampleRate
		vad := ac.SpeechSamples >= 16000
		if energy {
			role.EnergyQualified++
		}
		if vad {
			role.VADQualified++
		}
		if energy && vad {
			role.BothQualified++
		}
		roles[c.Role] = role
		clip := evidenceLabeledVector{Speaker: c.Speaker, ID: c.ID, Vector: cases[i].vector}
		if strings.HasSuffix(c.Role, "_enrollment") {
			if !energy {
				return r, errProtocol
			}
			bySpeaker[c.Speaker] = append(bySpeaker[c.Speaker], clip)
			summary := summaries[c.Speaker]
			summary.Speaker = c.Speaker
			summary.Cases++
			summary.RawSamples += c.Samples
			summary.SpeechSamples += ac.SpeechSamples
			summaries[c.Speaker] = summary
		}
		if c.Role == "train_enrollment" && energy {
			trainEnrollment = append(trainEnrollment, clip)
		}
		if c.Role == "train_query" && energy {
			trainQuery = append(trainQuery, clip)
		}
	}
	if testN < 1 || testN > 5 {
		return r, errProtocol
	}
	var e error
	r.Policy, e = fitEvidencePolicy(trainEnrollment, trainQuery, dimension)
	if e != nil {
		return r, e
	}
	var speakers []string
	for s := range bySpeaker {
		speakers = append(speakers, s)
	}
	sort.Slice(speakers, func(i, j int) bool { return backendSpeakerLess(speakers[i], speakers[j]) })
	for _, speaker := range speakers {
		if e := ctx.Err(); e != nil {
			return r, e
		}
		clips := bySpeaker[speaker]
		vectors := make([][]float32, len(clips))
		for i, c := range clips {
			vectors[i] = c.Vector
		}
		v, e := fuseEnrollment(vectors, dimension)
		if e != nil {
			return r, e
		}
		templates[speaker] = v
		diagnostic, e := diagnoseEvidenceEnrollment(speaker, clips, dimension, r.Policy)
		if e != nil {
			return r, e
		}
		r.Diagnostics = append(r.Diagnostics, diagnostic)
		r.Enrollment = append(r.Enrollment, summaries[speaker])
	}
	for _, v := range roles {
		r.Roles = append(r.Roles, v)
	}
	sort.Slice(r.Roles, func(i, j int) bool { return r.Roles[i].Role < r.Roles[j].Role })
	for _, name := range []string{"B_baseline", "B_enrollment_diagnostic", "B_accumulation", "B_combined_diagnostic"} {
		accumulate := name == "B_accumulation" || name == "B_combined_diagnostic"
		arm := evidenceArm{Name: name, EnrollmentDiagnostic: name == "B_enrollment_diagnostic" || name == "B_combined_diagnostic", Accumulation: accumulate}
		for _, scenario := range []string{"paired_bursts", "alternating"} {
			scene := evidenceScenario{Name: scenario}
			validationKnown := map[int][]backendQuery{}
			validationUnknown := map[int][]backendQuery{}
			for _, shape := range []struct {
				split   string
				n, size int
			}{{"validation", 50, testN}, {"validation", 50, 50}, {"test", testN, testN}} {
				if e := ctx.Err(); e != nil {
					return r, e
				}
				candidates := make([][]float32, shape.n)
				for i := range candidates {
					v := templates[fmt.Sprintf("%s_%d", shape.split, i+1)]
					if v == nil {
						return r, errProtocol
					}
					candidates[i] = v
				}
				events, e := buildEvidenceScene(m, cases, shape.split, shape.n, shape.size, scenario)
				if e != nil {
					return r, e
				}
				scores, summary, windows, e := replayEvidence(ctx, events, candidates, dimension, r.Policy, accumulate)
				if e != nil {
					return r, e
				}
				k, u, groups := backendPoolQueries(m, cases, scores, shape.split, shape.n, shape.size)
				for _, v := range scores {
					clear(v)
				}
				if shape.split == "validation" {
					validationKnown[shape.size] = k
					validationUnknown[shape.size] = u
				}
				vk, vu := validationKnown[shape.size], validationUnknown[shape.size]
				calibrationStart := time.Now()
				policy := backendCalibrate(vk, vu, []float64{0, .01, .030229, .05, .1}, .01)
				scene.CalibrationMS += backendElapsed(calibrationStart)
				scoringStart := time.Now()
				pool := backendPool{Split: shape.split, Candidates: shape.size, GroupCount: groups, Top1: backendTop(k, true), PairDiagnostic: backendPoolROC(k, u, true)}
				pool.Rules = append(pool.Rules, backendRuleResult("unknown_target_0.01", policy, k, u, groups, true))
				if !accumulate && shape.split == "validation" && shape.size == 50 {
					for _, fixed := range []struct {
						name      string
						threshold float64
					}{{"fixed_current_session", largeSessionMatch}, {"fixed_legacy_base_far001", .4499944095418535}} {
						p := backendPolicy{Status: "calibrated", Threshold: fixed.threshold, Margin: largeSessionMargin, Validation: backendCount(k, u, fixed.threshold, largeSessionMargin, true)}
						pool.Rules = append(pool.Rules, backendRuleResult(fixed.name, p, k, u, groups, true))
					}
				}
				scene.ScoringMS += backendElapsed(scoringStart)
				// Truth is consulted only after replay, calibration and scoring are final.
				summary.MixedSpeakerWindows = evidenceMixedWindows(windows, labels)
				scene.Pools = append(scene.Pools, pool)
				scene.Replay = append(scene.Replay, summary)
			}
			arm.Scenarios = append(arm.Scenarios, scene)
		}
		r.Arms = append(r.Arms, arm)
	}
	r.TotalMS = backendElapsed(started)
	return r, nil
}
func runEvidenceStudy(ctx context.Context, model, sha, manifest string) (evidenceReport, error) {
	var r evidenceReport
	m, a, digest, e := loadBackendManifest(manifest)
	if e != nil {
		return r, e
	}
	em := evaluationManifest{}
	for _, c := range m.Cases {
		em.Cases = append(em.Cases, evaluationCase{ID: c.ID, Speaker: c.Speaker, Split: "evaluation", Path: c.Path, Samples: c.Samples, SHA: c.SHA, RecordingSHA: c.RecordingSHA, TextSHA: c.TextSHA})
	}
	ex, e := extractEvaluation(ctx, em, model, sha, startWorker)
	if e != nil {
		return r, e
	}
	defer ex.clearVectors()
	r, e = analyzeEvidence(ctx, m, a, ex.cases, ex.dimension)
	if e != nil {
		return r, e
	}
	r.ModelSHA = sha
	r.ManifestSHA = digest
	r.Dimension = ex.dimension
	r.CaseCount = len(m.Cases)
	r.QualifiedCases = ex.qualified
	r.RejectedShort = ex.short
	r.RejectedSilence = ex.silence
	r.Native = ex.native
	r.Wall = ex.wall
	r.ColdStartMS = ex.coldMS
	r.WorkerCPUms = ex.cpuMS
	r.WorkerMaxRSSKiB = ex.workerRSS
	r.Provider = "cpu"
	r.Threads = 1
	r.SampleRate = sampleRate
	r.VADModelSHA = sileroSHA
	r.VADConfig = fixedVADConfig()
	for _, ref := range m.Audits {
		r.AuditSHAs = append(r.AuditSHAs, ref.SHA)
	}
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return r, errWorker
	}
	r.ParentMaxRSSKiB = usage.Maxrss
	return r, nil
}
