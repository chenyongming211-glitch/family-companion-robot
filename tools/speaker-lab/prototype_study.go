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

type prototypeMethod struct {
	Name               string             `json:"method"`
	Status             string             `json:"status"`
	Prototypes         []prototypeSummary `json:"prototype_summary"`
	TwoPrototypePeople int                `json:"two_prototype_people"`
	FallbackPeople     int                `json:"fallback_single_people"`
	PrototypeMS        float64            `json:"prototype_construction_ms"`
	CalibrationMS      float64            `json:"calibration_ms"`
	ScoringMS          float64            `json:"scoring_ms"`
	TotalMS            float64            `json:"method_total_ms"`
	Pools              []backendPool      `json:"pools"`
}
type prototypeReport struct {
	evaluationMetadata
	Condition                  string              `json:"condition"`
	Methods                    []prototypeMethod   `json:"methods"`
	Enrollment                 []backendEnrollment `json:"enrollment_summary"`
	Population                 string              `json:"population"`
	NewBlindTest               bool                `json:"new_blind_test"`
	QueryAccumulation          bool                `json:"query_accumulation"`
	HouseholdAssessment        string              `json:"household_assessment"`
	EndToEndLatencyAssessment  string              `json:"end_to_end_latency_assessment"`
	Frontend                   string              `json:"frontend"`
	OfficialFeatureEquivalence string              `json:"official_feature_equivalence"`
	FusionBasis                string              `json:"fusion_basis"`
	AuditSHAs                  []string            `json:"quality_audit_sha256"`
	VADModelSHA                string              `json:"vad_model_sha256"`
	VADConfig                  vadConfiguration    `json:"vad_config"`
	Provider                   string              `json:"provider"`
	Threads                    int                 `json:"threads"`
	SampleRate                 int                 `json:"sample_rate"`
	AudioSeconds               float64             `json:"audio_seconds"`
	ColdStartMS                float64             `json:"cold_start_ms"`
	WorkerCPUms                float64             `json:"worker_cpu_ms"`
	WorkerMaxRSSKiB            int64               `json:"worker_max_rss_kib"`
	ParentMaxRSSKiB            int64               `json:"parent_max_rss_kib"`
	Roles                      []backendRoleCount  `json:"role_quality_counts"`
	TotalMS                    float64             `json:"analysis_total_ms"`
}

// Analysis borrows a single extraction. Enrollment is consumed in manifest
// order; queries never enter construction, and training never fits a policy.
func analyzePrototypeStudy(ctx context.Context, m backendManifest, a map[string]auditCase, cases []embeddedCase, dimension int) (prototypeReport, error) {
	started := time.Now()
	r := prototypeReport{evaluationMetadata: newEvaluationReport("").evaluationMetadata, Condition: m.Condition, Population: "seen_development_regression", HouseholdAssessment: "unavailable", EndToEndLatencyAssessment: "unavailable", Frontend: "sherpa_native", OfficialFeatureEquivalence: "unverified", FusionBasis: "qualified_segment_equal_weight_normalized_vectors"}
	r.SourceKind = m.SourceKind
	r.SingleSpeakerBasis = m.SingleSpeakerBasis
	if m.SourceKind == "synthetic_tts" {
		r.Population = "synthetic_engineering_fixture"
	} else if m.SourceKind == "public_human" {
		r.OpenSetAssessment = "limited_public_unknown_cohort"
	} else {
		return r, errProtocol
	}
	if e := ctx.Err(); e != nil {
		return r, e
	}
	if len(cases) != len(m.Cases) || dimension < 1 || dimension > maxDimension || m.SampleRate != sampleRate || (m.Condition != "device03" && m.Condition != "device06") {
		return r, errProtocol
	}
	r.Dimension = dimension
	r.CaseCount = len(cases)
	r.SampleRate = sampleRate
	bySpeaker := map[string][]evidenceLabeledVector{}
	summaries := map[string]backendEnrollment{}
	roles := map[string]backendRoleCount{}
	ids := map[string]bool{}
	testN := 0
	for i, c := range m.Cases {
		if e := ctx.Err(); e != nil {
			return r, e
		}
		ac, ok := a[c.SHA]
		group, person := backendSpeaker(c.Speaker)
		limit := map[string]int{"train": 10, "validation": 50, "test": 5, "validation_unknown": 5, "test_unknown": 20, "cohort": 30}[group]
		roleOK := c.Role == group+"_enrollment" || c.Role == group+"_query"
		if group == "validation_unknown" || group == "test_unknown" || group == "cohort" {
			roleOK = c.Role == group
		}
		if !ok || ac.Samples != c.Samples || c.Samples < 1 || c.Samples > maxSamples || ac.SpeechSamples < 0 || ac.SpeechSamples > c.Samples || person < 1 || person > limit || !roleOK || c.ID == "" || ids[c.ID] {
			return r, errProtocol
		}
		ids[c.ID] = true
		if group == "test" {
			testN = max(testN, person)
		}
		energy := cases[i].quality == "qualified"
		switch cases[i].quality {
		case "qualified":
			if !validVector(cases[i].vector, dimension) {
				return r, errProtocol
			}
			r.QualifiedCases++
		case "short":
			r.RejectedShort++
		case "silence":
			r.RejectedSilence++
		default:
			return r, errProtocol
		}
		r.AudioSeconds += float64(c.Samples) / sampleRate
		role := roles[c.Role]
		role.Role = c.Role
		role.Total++
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
		if strings.HasSuffix(c.Role, "_enrollment") {
			if !energy || !vad {
				return r, errProtocol
			}
			bySpeaker[c.Speaker] = append(bySpeaker[c.Speaker], evidenceLabeledVector{Speaker: c.Speaker, ID: c.ID, Vector: cases[i].vector})
			summary := summaries[c.Speaker]
			summary.Speaker = c.Speaker
			summary.Cases++
			summary.RawSamples += c.Samples
			summary.SpeechSamples += ac.SpeechSamples
			summaries[c.Speaker] = summary
		}
	}
	if testN < 1 || testN > 5 {
		return r, errProtocol
	}
	// Pool helpers require every candidate's enrollment and bounded identity IDs.
	for group, n := range map[string]int{"validation": 50, "test": testN} {
		for i := 1; i <= n; i++ {
			if len(bySpeaker[fmt.Sprintf("%s_%d", group, i)]) == 0 {
				return r, errProtocol
			}
		}
	}
	var speakers []string
	for speaker := range bySpeaker {
		speakers = append(speakers, speaker)
	}
	sort.Slice(speakers, func(i, j int) bool { return backendSpeakerLess(speakers[i], speakers[j]) })
	for _, speaker := range speakers {
		r.Enrollment = append(r.Enrollment, summaries[speaker])
	}
	for _, role := range roles {
		r.Roles = append(r.Roles, role)
	}
	sort.Slice(r.Roles, func(i, j int) bool { return r.Roles[i].Role < r.Roles[j].Role })
	for _, name := range []string{"B_single_template", "P2_two_prototypes"} {
		method, e := analyzePrototypeMethod(ctx, m, cases, dimension, speakers, bySpeaker, testN, name)
		if e != nil {
			return r, e
		}
		r.Methods = append(r.Methods, method)
	}
	r.TotalMS = backendElapsed(started)
	return r, nil
}
func analyzePrototypeMethod(ctx context.Context, m backendManifest, cases []embeddedCase, dimension int, speakers []string, bySpeaker map[string][]evidenceLabeledVector, testN int, name string) (prototypeMethod, error) {
	started := time.Now()
	method := prototypeMethod{Name: name, Status: "evaluated"}
	templates := map[string][][]float32{}
	defer func() {
		for _, p := range templates {
			for _, v := range p {
				clear(v)
			}
		}
	}()
	construction := time.Now()
	for _, speaker := range speakers {
		if e := ctx.Err(); e != nil {
			return method, e
		}
		clips := bySpeaker[speaker]
		var p [][]float32
		var summary prototypeSummary
		var e error
		if name == "P2_two_prototypes" {
			p, summary, e = buildEnrollmentPrototypes(clips, dimension)
		} else {
			original := make([][]float32, len(clips))
			for i, c := range clips {
				original[i] = c.Vector
			}
			var v []float32
			v, e = fuseEnrollment(original, dimension)
			if e == nil {
				p = [][]float32{v}
			}
			summary = prototypeSummary{Speaker: speaker, Status: "single_template", Reason: "original_B", Cases: len(clips), ClusterSizes: []int{len(clips)}}
		}
		if e != nil {
			return method, e
		}
		templates[speaker] = p
		method.Prototypes = append(method.Prototypes, summary)
		if summary.Status == "two_prototypes" {
			method.TwoPrototypePeople++
		}
		if summary.Status == "fallback_single" {
			method.FallbackPeople++
		}
	}
	method.PrototypeMS = backendElapsed(construction)
	scores := map[int][]float64{}
	defer func() {
		for _, v := range scores {
			clear(v)
		}
	}()
	scoring := time.Now()
	for i, c := range m.Cases {
		if e := ctx.Err(); e != nil {
			return method, e
		}
		if cases[i].quality != "qualified" || (c.Role != "validation_query" && c.Role != "validation_unknown" && c.Role != "test_query" && c.Role != "test_unknown") {
			continue
		}
		group, _ := backendSpeaker(c.Speaker)
		group = strings.TrimSuffix(group, "_unknown")
		n := 50
		if group == "test" {
			n = testN
		}
		values := make([]float64, n)
		scores[i] = values
		for j := range values {
			score, e := prototypeScore(cases[i].vector, templates[fmt.Sprintf("%s_%d", group, j+1)], dimension)
			if e != nil {
				return method, e
			}
			values[j] = score
		}
	}
	method.ScoringMS = backendElapsed(scoring)
	clearQueries := func(queries []backendQuery) {
		for _, q := range queries {
			clear(q.Scores)
		}
	}
	for _, shape := range []struct {
		split   string
		n, size int
	}{{"validation", 50, testN}, {"validation", 50, 50}, {"test", testN, testN}} {
		if e := ctx.Err(); e != nil {
			return method, e
		}
		k, u, groups := backendPoolQueries(m, cases, scores, shape.split, shape.n, shape.size)
		defer clearQueries(k)
		defer clearQueries(u)
		vk, vu, _ := backendPoolQueries(m, cases, scores, "validation", 50, shape.size)
		defer clearQueries(vk)
		defer clearQueries(vu)
		calibration := time.Now()
		policy := backendCalibrate(vk, vu, []float64{0, .01, .030229, .05, .1}, .01)
		method.CalibrationMS += backendElapsed(calibration)
		scoring = time.Now()
		pool := backendPool{Split: shape.split, Candidates: shape.size, GroupCount: groups, Top1: backendTop(k, true), PairDiagnostic: backendPoolROC(k, u, true)}
		pool.Rules = append(pool.Rules, backendRuleResult("unknown_target_0.01", policy, k, u, groups, true))
		method.ScoringMS += backendElapsed(scoring)
		method.Pools = append(method.Pools, pool)
	}
	method.TotalMS = backendElapsed(started)
	return method, nil
}
func runPrototypeStudy(ctx context.Context, model, sha, manifest string) (prototypeReport, error) {
	return runPrototypeStudyWithStart(ctx, model, sha, manifest, startWorker)
}

// The injectable starter preserves real extraction/IPC ownership in numeric
// engineering tests; the public entry point always uses the native worker.
func runPrototypeStudyWithStart(ctx context.Context, model, sha, manifest string, start evaluationWorkerStart) (prototypeReport, error) {
	var r prototypeReport
	m, a, digest, e := loadBackendManifest(manifest)
	if e != nil {
		return r, e
	}
	em := evaluationManifest{}
	for _, c := range m.Cases {
		em.Cases = append(em.Cases, evaluationCase{ID: c.ID, Speaker: c.Speaker, Split: "evaluation", Path: c.Path, Samples: c.Samples, SHA: c.SHA, RecordingSHA: c.RecordingSHA, TextSHA: c.TextSHA})
	}
	ex, e := extractEvaluation(ctx, em, model, sha, start)
	if e != nil {
		return r, e
	}
	defer ex.clearVectors()
	r, e = analyzePrototypeStudy(ctx, m, a, ex.cases, ex.dimension)
	if e != nil {
		return r, e
	}
	r.ModelSHA = sha
	r.ManifestSHA = digest
	r.Native = ex.native
	r.Wall = ex.wall
	r.ColdStartMS = ex.coldMS
	r.WorkerCPUms = ex.cpuMS
	r.WorkerMaxRSSKiB = ex.workerRSS
	r.Provider = "cpu"
	r.Threads = 1
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
