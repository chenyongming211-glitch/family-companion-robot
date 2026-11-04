//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func backendSynthetic(m backendManifest) []embeddedCase {
	ids := map[string]int{}
	var cs []embeddedCase
	for _, c := range m.Cases {
		n, ok := ids[c.Speaker]
		if !ok {
			n = len(ids)
			ids[c.Speaker] = n
		}
		v := make([]float32, 128)
		v[n] = 1
		cs = append(cs, embeddedCase{fixtureCase: fixtureCase{ID: c.ID, Speaker: c.Speaker}, vector: v, quality: "qualified"})
	}
	return cs
}
func TestBackendStudySharedScoresRankPrivacyAndPersonMacro(t *testing.T) {
	m, a := backendFixture(t)
	audit := map[string]auditCase{}
	for _, c := range a.Cases {
		audit[c.SHA] = c
	}
	cs := backendSynthetic(m)
	cs[3].quality = "silence"
	ac := audit[m.Cases[4].SHA]
	ac.SpeechSamples = 0
	audit[m.Cases[4].SHA] = ac
	methods, summary, e := analyzeBackend(m, audit, cs, 128)
	if e != nil || len(methods) != 3 || len(summary) == 0 {
		t.Fatal("missing analysis", e)
	}
	for _, method := range methods {
		if method.Status != "evaluated" {
			t.Fatal(method.Name, method.Status, method.Fit)
		}
		if len(method.Pools) != 3 {
			t.Fatal("pool layout", method.Name)
		}
		for _, pool := range method.Pools {
			if pool.Split == "test" && pool.Candidates != 4 {
				t.Fatal("wrong test pool")
			}
			if pool.Split == "validation" && pool.Candidates != 4 && pool.Candidates != 50 {
				t.Fatal("wrong validation pool")
			}
			if len(pool.Rules) < 2 {
				t.Fatal("missing targets")
			}
			if pool.Top1.Correct != pool.Top1.Qualified {
				t.Fatal("rank/energy gate changed", pool.Top1)
			}
			if pool.Split == "test" && len(pool.Rules[0].People) != 24 {
				t.Fatal("missing people")
			}
			for _, rule := range pool.Rules {
				if rule.Status != "evaluated" || rule.Counts.Wrong != 0 {
					t.Fatal("rule", rule)
				}
			}
		}
	}
	// All views share extraction ownership; analysis must neither clear nor mutate vectors.
	if cs[0].vector[0] != 1 {
		t.Fatal("cleared borrowed vector")
	}
	b, e := json.Marshal(methods)
	if e != nil {
		t.Fatal("nonfinite JSON", e)
	}
	for _, private := range []string{m.Cases[0].Path, m.Cases[0].SHA, "source_group", "recording_sha", "vector", "text_sha"} {
		if bytes.Contains(b, []byte(private)) {
			t.Fatal("private data exposed")
		}
	}
	r0 := methods[0].Pools[1]
	if r0.Split != "validation" || r0.Candidates != 50 || len(r0.Rules) != 4 {
		t.Fatal("R0 fixed diagnostics missing")
	}
}
func TestBackendStudyFitFailurePreservesDenominators(t *testing.T) {
	m, a := backendFixture(t)
	audit := map[string]auditCase{}
	for _, c := range a.Cases {
		audit[c.SHA] = c
	}
	cs := backendSynthetic(m)
	for i, c := range m.Cases {
		if c.Role == "train_query" {
			cs[i].quality = "silence"
			cs[i].vector = nil
		}
	}
	methods, _, e := analyzeBackend(m, audit, cs, 128)
	if e != nil {
		t.Fatal(e)
	}
	r := methods[2]
	if r.Status != "not_run" {
		t.Fatal("fit failure accepted")
	}
	for _, p := range r.Pools {
		if p.Top1.Total == 0 || p.Top1.Correct != 0 {
			t.Fatal(p.Top1)
		}
		for _, rule := range p.Rules {
			c := rule.Counts
			if rule.Status != "not_run" || c.Total == 0 || c.UnknownTotal == 0 || c.Correct+c.Wrong+c.Rejected+c.UnknownAccepted+c.UnknownRejected != 0 {
				t.Fatal(rule)
			}
		}
	}
}
func TestBackendStudyPerPersonMacroUnequalQueries(t *testing.T) {
	k := []backendQuery{{Qualified: true, Own: 0, Speaker: "test_1", Scores: []float64{1, 0}}, {Qualified: true, Own: 1, Speaker: "test_2", Scores: []float64{1, 0}}, {Qualified: true, Own: 1, Speaker: "test_2", Scores: []float64{1, 0}}, {Qualified: true, Own: 1, Speaker: "test_2", Scores: []float64{1, 0}}}
	u := []backendQuery{{Qualified: true, Own: -1, Speaker: "test_unknown_1", Scores: []float64{1, 0}}, {Own: -1, Speaker: "test_unknown_2"}}
	people, macro := backendPeople(k, u, .5, 0, true)
	if len(people) != 4 {
		t.Fatal(people)
	}
	closeBackend(t, macro.CorrectAccept, .5)
	closeBackend(t, macro.Top1All, .5)
	if macro.UnknownPeopleWithFalseAccept != 1 {
		t.Fatal(macro)
	}
}
func TestBackendCLIIndependentAndOldLimit(t *testing.T) {
	for _, args := range [][]string{{"backend-study"}, {"backend-study", "--model", "/missing", "--sha256", fmt.Sprintf("%064d", 1), "--manifest", "/missing"}, {"backend-study", "--model", "/missing", "--sha256", fmt.Sprintf("%064d", 1), "--manifest", "/missing", "--audit", "/missing"}} {
		var out, err bytes.Buffer
		if code := runCLI(context.Background(), args, &out, &err); code == 0 || out.Len() != 0 {
			t.Fatal(code)
		}
	}
	if maxManifestBytes != 128*1024 || studyMaxBytes != 2*1024*1024 {
		t.Fatal("old bounds expanded")
	}
}
func TestBackendR0MatchesPriorBFixedPolicyAndTop1(t *testing.T) {
	m, a := backendFixture(t)
	cs := backendSynthetic(m)
	audit := map[string]auditCase{}
	for _, c := range a.Cases {
		audit[c.SHA] = c
	}
	var oldM studyManifest
	var oldCases []embeddedCase
	for i, c := range m.Cases {
		g, n := backendSpeaker(c.Speaker)
		prefix, role := "", ""
		switch g {
		case "validation":
			prefix = "fixture"
			if c.Role == "validation_enrollment" {
				role = "known_enrollment"
			} else {
				role = "known_evaluation"
			}
		case "train":
			prefix = "calibration"
			if c.Role == "train_enrollment" {
				role = "calibration_enrollment"
			} else {
				role = "calibration_query"
			}
		case "validation_unknown":
			prefix = "unknown"
			role = "unknown_evaluation"
		default:
			continue
		}
		speaker := fmt.Sprintf("%s_%d", prefix, n)
		oldM.Cases = append(oldM.Cases, studyCase{ID: c.ID, Speaker: speaker, Role: role, Path: c.Path, Samples: c.Samples, SHA: c.SHA, RecordingSHA: c.RecordingSHA, TextSHA: c.TextSHA})
		item := cs[i]
		item.Speaker = speaker
		oldCases = append(oldCases, item)
	}
	old, e := analyzeStudy(oldM, a, oldCases, 128)
	if e != nil {
		t.Fatal(e)
	}
	now, _, e := analyzeBackend(m, audit, cs, 128)
	if e != nil {
		t.Fatal(e)
	}
	var prior studyView
	for _, v := range old {
		if v.Enrollment == "multi" && v.Quality == "energy" {
			prior = v
		}
	}
	pool := now[0].Pools[1]
	if pool.Top1.Correct != prior.Top1[0].Correct || pool.Top1.Total != prior.Top1[0].Total || pool.Top1.Qualified != prior.Top1[0].QualifiedTotal {
		t.Fatal("raw top1 changed")
	}
	for i := 0; i < 2; i++ {
		got, want := pool.Rules[2+i].Counts, prior.Rules[i].Known[0]
		unk := prior.Rules[i].Unknown[0]
		if got.Correct != want.Correct || got.Wrong != want.Wrong || got.Rejected != want.Rejected || got.UnknownAccepted != unk.FalseAccept || got.UnknownRejected != unk.Rejected {
			t.Fatalf("fixed R0 mismatch got%+v known%+v unknown%+v", got, want, unk)
		}
	}
}
func TestBackendGroupAssignmentNumericWrapAndQueriesCountOnce(t *testing.T) {
	m := backendManifest{}
	var cs []embeddedCase
	scores := map[int][]float64{}
	for i := 1; i <= 50; i++ {
		m.Cases = append(m.Cases, backendCase{Speaker: fmt.Sprintf("validation_%d", i), Role: "validation_query"})
		cs = append(cs, embeddedCase{quality: "qualified"})
		s := make([]float64, 50)
		s[i-1] = 1
		scores[len(cs)-1] = s
	}
	for i := 1; i <= 5; i++ {
		m.Cases = append(m.Cases, backendCase{Speaker: fmt.Sprintf("validation_unknown_%d", i), Role: "validation_unknown"})
		cs = append(cs, embeddedCase{quality: "qualified"})
		scores[len(cs)-1] = make([]float64, 50)
	}
	k, u, n := backendPoolQueries(m, cs, scores, "validation", 50, 3)
	if len(k) != 50 || len(u) != 5 || n != 17 || k[49].Group != 16 || k[49].Own != 1 || u[4].Group != 4 {
		t.Fatal("wrong mapping", n, k[49], u[4])
	}
	for _, q := range k {
		if !backendAccept(q, .5, 0) {
			t.Fatal("lost own template", q)
		}
	}
}
