//go:build linux

package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"syscall"
	"time"
)

type backendTop1 struct {
	Status            string  `json:"status"`
	Total             int     `json:"total"`
	Qualified         int     `json:"qualified_total"`
	Correct           int     `json:"correct"`
	AllAccuracy       float64 `json:"all_accuracy"`
	QualifiedAccuracy float64 `json:"qualified_accuracy"`
}
type backendPerson struct {
	Speaker string        `json:"anonymous_speaker"`
	Counts  backendCounts `json:"counts"`
	Top1    backendTop1   `json:"top1_diagnostic"`
}
type backendMacro struct {
	KnownPeople                  int     `json:"known_people"`
	QualifiedKnownPeople         int     `json:"qualified_known_people"`
	UnknownPeople                int     `json:"unknown_people"`
	CorrectAccept                float64 `json:"equal_person_correct_accept_all"`
	CorrectAcceptQualified       float64 `json:"equal_person_correct_accept_qualified"`
	Top1All                      float64 `json:"equal_person_top1_all"`
	Top1Qualified                float64 `json:"equal_person_top1_qualified"`
	UnknownPeopleWithFalseAccept int     `json:"unknown_people_with_any_false_accept"`
}
type backendGroup struct {
	Group  int           `json:"group"`
	Counts backendCounts `json:"counts"`
}
type backendRule struct {
	Name        string          `json:"name"`
	Status      string          `json:"status"`
	Calibration backendPolicy   `json:"validation_calibration"`
	Counts      backendCounts   `json:"counts"`
	Groups      []backendGroup  `json:"groups"`
	People      []backendPerson `json:"people"`
	Macro       backendMacro    `json:"macro"`
}
type backendPool struct {
	Split          string           `json:"split"`
	Candidates     int              `json:"candidates"`
	GroupCount     int              `json:"group_count"`
	Rules          []backendRule    `json:"rules"`
	Top1           backendTop1      `json:"top1_diagnostic"`
	PairDiagnostic backendROCResult `json:"template_pair_diagnostic"`
}
type backendMethod struct {
	Name                     string           `json:"method"`
	Status                   string           `json:"status"`
	Fit                      backendFitResult `json:"qmf_fit"`
	TrainingPeople           int              `json:"training_people"`
	TrainingQueries          int              `json:"training_queries"`
	TrainingQualifiedQueries int              `json:"training_qualified_queries"`
	CohortStatsMS            float64          `json:"cohort_statistics_ms"`
	FitMS                    float64          `json:"fitting_ms"`
	CalibrationMS            float64          `json:"threshold_calibration_ms"`
	ScoringMS                float64          `json:"scoring_ms"`
	TotalMS                  float64          `json:"total_ms"`
	ReusedR1Stats            bool             `json:"reused_r1_cohort_statistics"`
	StdFloors                int              `json:"asnorm_std_floor_count"`
	Pools                    []backendPool    `json:"pools"`
}
type backendEnrollment struct {
	Speaker       string `json:"anonymous_speaker"`
	Cases         int    `json:"cases"`
	RawSamples    int    `json:"raw_samples"`
	SpeechSamples int    `json:"speech_samples"`
}
type backendRoleCount struct {
	Role            string `json:"role"`
	Total           int    `json:"total"`
	EnergyQualified int    `json:"energy_qualified"`
	VADQualified    int    `json:"vad_qualified"`
	BothQualified   int    `json:"both_qualified"`
}
type backendReport struct {
	evaluationMetadata
	Condition           string              `json:"condition"`
	AuditSHAs           []string            `json:"quality_audit_sha256"`
	VADModelSHA         string              `json:"vad_model_sha256"`
	VADConfig           vadConfiguration    `json:"vad_config"`
	Provider            string              `json:"provider"`
	Threads             int                 `json:"threads"`
	SampleRate          int                 `json:"sample_rate"`
	AudioSeconds        float64             `json:"audio_seconds"`
	ColdStartMS         float64             `json:"cold_start_ms"`
	WorkerCPUms         float64             `json:"worker_cpu_ms"`
	WorkerMaxRSSKiB     int64               `json:"worker_max_rss_kib"`
	ParentMaxRSSKiB     int64               `json:"parent_max_rss_kib"`
	Roles               []backendRoleCount  `json:"role_quality_counts"`
	Enrollment          []backendEnrollment `json:"enrollment_summary"`
	Methods             []backendMethod     `json:"methods"`
	TotalMS             float64             `json:"backend_total_ms"`
	ReplayAssessment    string              `json:"replay_assessment"`
	HouseholdAssessment string              `json:"household_assessment"`
}

func backendElapsed(t time.Time) float64 { return float64(time.Since(t).Microseconds()) / 1000 }
func backendTop(k []backendQuery, run bool) backendTop1 {
	r := backendTop1{Status: "not_run", Total: len(k)}
	if run {
		r.Status = "diagnostic"
	}
	for _, q := range k {
		if !q.Qualified {
			continue
		}
		r.Qualified++
		if run {
			b, s, i := backendBest(q)
			if i == q.Own && (len(q.Scores) == 1 || b != s) {
				r.Correct++
			}
		}
	}
	if run && r.Total > 0 {
		r.AllAccuracy = float64(r.Correct) / float64(r.Total)
	}
	if run && r.Qualified > 0 {
		r.QualifiedAccuracy = float64(r.Correct) / float64(r.Qualified)
	}
	return r
}
func backendSpeakerLess(a, b string) bool {
	ap, ai := backendSpeaker(a)
	bp, bi := backendSpeaker(b)
	if ap == bp {
		return ai < bi
	}
	return ap < bp
}
func backendPeople(k, u []backendQuery, t, m float64, run bool) ([]backendPerson, backendMacro) {
	byKnown, byUnknown := map[string][]backendQuery{}, map[string][]backendQuery{}
	for _, q := range k {
		byKnown[q.Speaker] = append(byKnown[q.Speaker], q)
	}
	for _, q := range u {
		byUnknown[q.Speaker] = append(byUnknown[q.Speaker], q)
	}
	var names []string
	for s := range byKnown {
		names = append(names, s)
	}
	for s := range byUnknown {
		names = append(names, s)
	}
	sort.Slice(names, func(i, j int) bool { return backendSpeakerLess(names[i], names[j]) })
	macro := backendMacro{KnownPeople: len(byKnown), UnknownPeople: len(byUnknown)}
	var people []backendPerson
	for _, s := range names {
		ks, us := byKnown[s], byUnknown[s]
		c := backendCount(ks, us, t, m, run)
		top := backendTop(ks, run)
		people = append(people, backendPerson{Speaker: s, Counts: c, Top1: top})
		if len(ks) > 0 {
			if run {
				macro.CorrectAccept += float64(c.Correct) / float64(c.Total)
				macro.Top1All += top.AllAccuracy
			}
			if c.Qualified > 0 {
				macro.QualifiedKnownPeople++
				if run {
					macro.CorrectAcceptQualified += float64(c.Correct) / float64(c.Qualified)
					macro.Top1Qualified += top.QualifiedAccuracy
				}
			}
		}
		if run && c.UnknownAccepted > 0 {
			macro.UnknownPeopleWithFalseAccept++
		}
	}
	if macro.KnownPeople > 0 {
		macro.CorrectAccept /= float64(macro.KnownPeople)
		macro.Top1All /= float64(macro.KnownPeople)
	}
	if macro.QualifiedKnownPeople > 0 {
		macro.CorrectAcceptQualified /= float64(macro.QualifiedKnownPeople)
		macro.Top1Qualified /= float64(macro.QualifiedKnownPeople)
	}
	return people, macro
}
func backendRuleResult(name string, p backendPolicy, k, u []backendQuery, groups int, run bool) backendRule {
	run = run && p.Status == "calibrated"
	r := backendRule{Name: name, Status: "not_run", Calibration: p, Counts: backendCount(k, u, p.Threshold, p.Margin, run)}
	if run {
		r.Status = "evaluated"
	}
	r.People, r.Macro = backendPeople(k, u, p.Threshold, p.Margin, run)
	for g := 0; g < groups; g++ {
		var ks, us []backendQuery
		for _, q := range k {
			if q.Group == g {
				ks = append(ks, q)
			}
		}
		for _, q := range u {
			if q.Group == g {
				us = append(us, q)
			}
		}
		r.Groups = append(r.Groups, backendGroup{Group: g + 1, Counts: backendCount(ks, us, p.Threshold, p.Margin, run)})
	}
	return r
}

// Numeric speaker order is part of the frozen grouping protocol. The last
// validation group wraps for candidates only, never duplicating query records.
func backendPoolQueries(m backendManifest, cases []embeddedCase, scores map[int][]float64, split string, n, size int) ([]backendQuery, []backendQuery, int) {
	gs := backendGroups(n, size)
	var known, unknown []backendQuery
	for i, c := range m.Cases {
		if c.Role != split+"_query" && c.Role != split+"_unknown" {
			continue
		}
		_, person := backendSpeaker(c.Speaker)
		group := (person - 1) / size
		own := person - 1
		if c.Role == split+"_unknown" {
			group = (person - 1) % len(gs)
			own = -1
		}
		q := backendQuery{Qualified: cases[i].quality == "qualified", Own: -1, Group: group, Speaker: c.Speaker}
		for j, index := range gs[group] {
			if index == own {
				q.Own = j
			}
			if q.Qualified && len(scores[i]) == n {
				q.Scores = append(q.Scores, scores[i][index])
			}
		}
		if own < 0 {
			unknown = append(unknown, q)
		} else {
			known = append(known, q)
		}
	}
	return known, unknown, len(gs)
}
func backendPoolROC(k, u []backendQuery, run bool) backendROCResult {
	positive, negative := 0, 0
	var p, n []float64
	defer func() { clear(p); clear(n) }()
	for _, q := range k {
		if !q.Qualified {
			continue
		}
		positive++
		negative += max(0, len(q.Scores)-1)
		if run {
			for i, s := range q.Scores {
				if i == q.Own {
					p = append(p, s)
				} else {
					n = append(n, s)
				}
			}
		}
	}
	for _, q := range u {
		if !q.Qualified {
			continue
		}
		negative += len(q.Scores)
		if run {
			n = append(n, q.Scores...)
		}
	}
	if !run {
		return backendROCResult{Status: "not_run", PositivePairs: positive, NegativePairs: negative, PairedObservations: true}
	}
	return backendROC(p, n)
}

// The returned templates below are owned here; extracted vectors are borrowed
// read-only and retain a single owner in evaluationExtraction.
func analyzeBackend(m backendManifest, a map[string]auditCase, cases []embeddedCase, dimension int) ([]backendMethod, []backendEnrollment, error) {
	if len(cases) != len(m.Cases) || dimension < 1 {
		return nil, nil, errProtocol
	}
	started := time.Now()
	bySpeaker := map[string][][]float32{}
	templates := map[string][]float32{}
	defer func() {
		for _, v := range templates {
			clear(v)
		}
	}()
	summaries := map[string]backendEnrollment{}
	testN := 0
	for i, c := range m.Cases {
		ac, ok := a[c.SHA]
		if !ok || ac.Samples != c.Samples {
			return nil, nil, errProtocol
		}
		if cases[i].quality == "qualified" && !validVector(cases[i].vector, dimension) {
			return nil, nil, errProtocol
		}
		g, n := backendSpeaker(c.Speaker)
		if g == "test" && n > testN {
			testN = n
		}
		if strings.HasSuffix(c.Role, "_enrollment") || c.Role == "cohort" {
			if cases[i].quality != "qualified" || ac.SpeechSamples < 16000 {
				return nil, nil, errProtocol
			}
			bySpeaker[c.Speaker] = append(bySpeaker[c.Speaker], cases[i].vector)
			s := summaries[c.Speaker]
			s.Speaker = c.Speaker
			s.Cases++
			s.RawSamples += c.Samples
			s.SpeechSamples += ac.SpeechSamples
			summaries[c.Speaker] = s
		}
	}
	for s, v := range bySpeaker {
		f, e := fuseEnrollment(v, dimension)
		if e != nil {
			return nil, nil, e
		}
		templates[s] = f
	}
	var summary []backendEnrollment
	for _, s := range summaries {
		summary = append(summary, s)
	}
	sort.Slice(summary, func(i, j int) bool { return backendSpeakerLess(summary[i].Speaker, summary[j].Speaker) })
	for g, n := range map[string]int{"cohort": 30, "train": 10, "validation": 50, "test": testN} {
		for i := 1; i <= n; i++ {
			if templates[fmt.Sprintf("%s_%d", g, i)] == nil {
				return nil, nil, errProtocol
			}
		}
	}
	if testN < 1 || testN > 5 {
		return nil, nil, errProtocol
	}
	templateStats, queryStats := map[string]backendDistribution{}, map[int]backendDistribution{}
	floors := 0
	statsStarted := time.Now()
	cohortStats := func(v []float32) (backendDistribution, error) {
		scores := make([]float64, 30)
		defer clear(scores)
		for i := range scores {
			scores[i] = similarity(v, templates[fmt.Sprintf("cohort_%d", i+1)])
		}
		return backendStats(scores)
	}
	// Sorted template traversal makes reported counters and floating reductions
	// reproducible even though Go map iteration order is randomized.
	for _, s := range summary {
		if strings.HasPrefix(s.Speaker, "cohort_") {
			continue
		}
		v, e := cohortStats(templates[s.Speaker])
		if e != nil {
			return nil, nil, e
		}
		templateStats[s.Speaker] = v
		if v.Floored {
			floors++
		}
	}
	for i, c := range m.Cases {
		if strings.HasSuffix(c.Role, "_enrollment") || c.Role == "cohort" || cases[i].quality != "qualified" {
			continue
		}
		v, e := cohortStats(cases[i].vector)
		if e != nil {
			return nil, nil, e
		}
		queryStats[i] = v
		if v.Floored {
			floors++
		}
	}
	statsMS := backendElapsed(statsStarted)
	sharedMS := backendElapsed(started)
	var methods []backendMethod
	var r1Scores map[int][]float64
	var fit backendFitResult
	defer func() {
		for _, v := range r1Scores {
			clear(v)
		}
	}()
	for methodIndex, name := range []string{"R0", "R1", "R2"} {
		methodStart := time.Now()
		method := backendMethod{Name: name, Status: "evaluated", TrainingPeople: 10}
		if methodIndex == 0 {
			method.Fit.Status = "not_applicable"
		}
		if methodIndex == 1 {
			method.CohortStatsMS = statsMS
			method.StdFloors = floors
			method.Fit.Status = "not_applicable"
		}
		if methodIndex == 2 {
			method.ReusedR1Stats = true
			method.StdFloors = floors
			fitStart := time.Now()
			var pairs []backendPair
			for i, c := range m.Cases {
				if c.Role != "train_query" {
					continue
				}
				method.TrainingQueries++
				if cases[i].quality != "qualified" {
					continue
				}
				method.TrainingQualifiedQueries++
				duration := math.Log1p(float64(a[c.SHA].SpeechSamples) / sampleRate)
				_, person := backendSpeaker(c.Speaker)
				for j, s := range r1Scores[i] {
					pairs = append(pairs, backendPair{Score: s, Duration: duration, Same: j == person-1})
				}
			}
			fit = backendFit(pairs)
			clear(pairs)
			method.Fit = fit
			method.FitMS = backendElapsed(fitStart)
			if fit.Status != "fitted" {
				method.Status = "not_run"
			}
		}
		scoreStart := time.Now()
		scores := map[int][]float64{}
		for i, c := range m.Cases {
			if strings.HasSuffix(c.Role, "_enrollment") || c.Role == "cohort" || cases[i].quality != "qualified" {
				continue
			}
			g, _ := backendSpeaker(c.Speaker)
			g = strings.TrimSuffix(g, "_unknown")
			n := 10
			if g == "validation" {
				n = 50
			} else if g == "test" {
				n = testN
			}
			out := make([]float64, n)
			scores[i] = out
			for j := range out {
				speaker := fmt.Sprintf("%s_%d", g, j+1)
				if methodIndex == 2 {
					if method.Status == "evaluated" {
						out[j] = backendTransform(fit, r1Scores[i][j], math.Log1p(float64(a[c.SHA].SpeechSamples)/sampleRate))
					}
					continue
				}
				s := similarity(cases[i].vector, templates[speaker])
				if !backendFinite(s) {
					return nil, nil, errProtocol
				}
				if methodIndex == 1 {
					var e error
					s, e = backendASNorm(s, templateStats[speaker], queryStats[i])
					if e != nil {
						return nil, nil, e
					}
				}
				out[j] = s
			}
			if methodIndex == 2 && method.Status == "evaluated" {
				for j, v := range out {
					if !backendFinite(v) {
						return nil, nil, errProtocol
					}
					for k := 0; k < j; k++ {
						before := r1Scores[i][j] - r1Scores[i][k]
						after := v - out[k]
						if before > 0 && after <= 0 || before < 0 && after >= 0 || before == 0 && after != 0 {
							return nil, nil, errProtocol
						}
					}
				}
			}
		}
		method.ScoringMS = backendElapsed(scoreStart)
		for _, p := range []struct {
			split   string
			n, size int
		}{{"validation", 50, testN}, {"validation", 50, 50}, {"test", testN, testN}} {
			k, u, groups := backendPoolQueries(m, cases, scores, p.split, p.n, p.size)
			pool := backendPool{Split: p.split, Candidates: p.size, GroupCount: groups, Top1: backendTop(k, method.Status == "evaluated"), PairDiagnostic: backendPoolROC(k, u, method.Status == "evaluated")}
			vk, vu, _ := backendPoolQueries(m, cases, scores, "validation", 50, p.size)
			margins := []float64{0, .1, .25, .5, 1}
			if methodIndex == 0 {
				margins = []float64{0, .01, .030229, .05, .1}
			}
			for _, target := range []float64{.01, .05} {
				calStart := time.Now()
				policy := backendPolicy{Status: "not_run", Target: target, Budget: int(math.Floor(float64(len(vu)) * target)), Validation: backendCount(vk, vu, 0, 0, false)}
				if method.Status == "evaluated" {
					policy = backendCalibrate(vk, vu, margins, target)
				}
				method.CalibrationMS += backendElapsed(calStart)
				ruleStart := time.Now()
				pool.Rules = append(pool.Rules, backendRuleResult(fmt.Sprintf("unknown_target_%.2f", target), policy, k, u, groups, method.Status == "evaluated"))
				method.ScoringMS += backendElapsed(ruleStart)
			}
			if methodIndex == 0 && p.split == "validation" && p.size == 50 {
				for _, fixed := range []struct {
					name string
					t    float64
				}{{"fixed_current_session", largeSessionMatch}, {"fixed_legacy_base_far001", .4499944095418535}} {
					policy := backendPolicy{Status: "calibrated", Threshold: fixed.t, Margin: largeSessionMargin, Validation: backendCount(k, u, fixed.t, largeSessionMargin, true)}
					pool.Rules = append(pool.Rules, backendRuleResult(fixed.name, policy, k, u, groups, true))
				}
			}
			method.Pools = append(method.Pools, pool)
		}
		method.TotalMS = backendElapsed(methodStart)
		if methodIndex == 1 {
			method.TotalMS += sharedMS
		}
		methods = append(methods, method)
		if methodIndex == 1 {
			r1Scores = scores
		} else {
			for _, v := range scores {
				clear(v)
			}
		}
	}
	return methods, summary, nil
}
func runBackendStudy(ctx context.Context, model, sha, manifest string) (backendReport, error) {
	r := backendReport{evaluationMetadata: newEvaluationReport(sha).evaluationMetadata, Provider: "cpu", Threads: 1, SampleRate: sampleRate, VADModelSHA: sileroSHA, VADConfig: fixedVADConfig(), ReplayAssessment: "unavailable", HouseholdAssessment: "unavailable"}
	r.OpenSetAssessment = "limited_public_unknown_cohort"
	m, a, digest, e := loadBackendManifest(manifest)
	if e != nil {
		return r, e
	}
	r.Condition = m.Condition
	r.ManifestSHA, r.CaseCount = digest, len(m.Cases)
	for _, ref := range m.Audits {
		r.AuditSHAs = append(r.AuditSHAs, ref.SHA)
	}
	em := evaluationManifest{}
	for _, c := range m.Cases {
		r.AudioSeconds += float64(c.Samples) / sampleRate
		em.Cases = append(em.Cases, evaluationCase{ID: c.ID, Speaker: c.Speaker, Split: "evaluation", Path: c.Path, Samples: c.Samples, SHA: c.SHA, RecordingSHA: c.RecordingSHA, TextSHA: c.TextSHA})
	}
	ex, e := extractEvaluation(ctx, em, model, sha, startWorker)
	if e != nil {
		return r, e
	}
	defer ex.clearVectors()
	r.Dimension, r.QualifiedCases, r.RejectedShort, r.RejectedSilence = ex.dimension, ex.qualified, ex.short, ex.silence
	r.Native, r.Wall, r.ColdStartMS, r.WorkerCPUms, r.WorkerMaxRSSKiB = ex.native, ex.wall, ex.coldMS, ex.cpuMS, ex.workerRSS
	roleCounts := map[string]backendRoleCount{}
	for i, c := range m.Cases {
		v := roleCounts[c.Role]
		v.Role = c.Role
		v.Total++
		energy := ex.cases[i].quality == "qualified"
		vad := a[c.SHA].SpeechSamples >= 16000
		if energy {
			v.EnergyQualified++
		}
		if vad {
			v.VADQualified++
		}
		if energy && vad {
			v.BothQualified++
		}
		roleCounts[c.Role] = v
	}
	for _, v := range roleCounts {
		r.Roles = append(r.Roles, v)
	}
	sort.Slice(r.Roles, func(i, j int) bool { return r.Roles[i].Role < r.Roles[j].Role })
	started := time.Now()
	r.Methods, r.Enrollment, e = analyzeBackend(m, a, ex.cases, ex.dimension)
	r.TotalMS = backendElapsed(started)
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
