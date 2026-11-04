//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func studyFixture(t *testing.T) (studyManifest, qualityAudit) {
	t.Helper()
	m := studyManifest{SourceKind: "public_human", SourceID: "public_corpus", SourceURL: "https://3dspeaker.github.io/", LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/", SingleSpeakerBasis: "source_annotations", SampleRate: 16000}
	a := newQualityAudit(sileroSHA, fmt.Sprintf("%064d", 1))
	dir := t.TempDir()
	for n := 1; n <= 65; n++ {
		speaker := fmt.Sprintf("fixture_%d", n)
		roles := []string{"known_enrollment", "known_enrollment", "known_enrollment", "known_evaluation", "known_evaluation"}
		if n > 50 {
			speaker = fmt.Sprintf("calibration_%d", n-50)
			roles = []string{"calibration_enrollment", "calibration_enrollment", "calibration_enrollment", "calibration_query", "calibration_query"}
		}
		if n > 60 {
			speaker = fmt.Sprintf("unknown_%d", n-60)
			roles = []string{"unknown_evaluation", "unknown_evaluation"}
		}
		for i, role := range roles {
			id := fmt.Sprintf("case_%d_%d", n, i)
			h := sha256.Sum256([]byte(id))
			sha := hex.EncodeToString(h[:])
			p := filepath.Join(dir, id+".wav")
			os.WriteFile(p, []byte("placeholder"), 0600)
			c := studyCase{ID: id, Speaker: speaker, Role: role, Path: p, Samples: 112000, SHA: sha, RecordingSHA: sha, TextSHA: sha}
			m.Cases = append(m.Cases, c)
			a.Cases = append(a.Cases, auditCase{ID: id, SHA: sha, Samples: c.Samples, SpeechSamples: 96000, RMS: .1})
		}
	}
	return m, a
}
func writeStudy(t *testing.T, m studyManifest, a qualityAudit) (string, string) {
	t.Helper()
	dir := t.TempDir()
	ap := filepath.Join(dir, "audit.json")
	ab, _ := json.Marshal(a)
	os.WriteFile(ap, ab, 0600)
	h := sha256.Sum256(ab)
	m.AuditSHA = hex.EncodeToString(h[:])
	mp := filepath.Join(dir, "manifest.json")
	mb, _ := json.Marshal(m)
	os.WriteFile(mp, mb, 0600)
	return mp, ap
}
func TestStudyManifestBindingAndRoles(t *testing.T) {
	m, a := studyFixture(t)
	mp, ap := writeStudy(t, m, a)
	if _, _, _, e := loadStudyManifest(mp, ap); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*studyManifest, *qualityAudit){func(m *studyManifest, a *qualityAudit) { m.Cases[0].Role = "known_evaluation" }, func(m *studyManifest, a *qualityAudit) { m.Cases[0].Speaker = "calibration_1" }, func(m *studyManifest, a *qualityAudit) { m.Cases[0].Samples = 32000 }, func(m *studyManifest, a *qualityAudit) { m.Cases[3].TextSHA = m.Cases[0].TextSHA }, func(m *studyManifest, a *qualityAudit) { m.Cases[3].RecordingSHA = m.Cases[0].RecordingSHA }, func(m *studyManifest, a *qualityAudit) { a.Cases[0].SpeechSamples = 0 }, func(m *studyManifest, a *qualityAudit) { a.Config.WindowSize = 256 }, func(m *studyManifest, a *qualityAudit) { a.ModelSHA = fmt.Sprintf("%064d", 2) }, func(m *studyManifest, a *qualityAudit) { a.Cases[0].SHA = fmt.Sprintf("%064d", 2) }, func(m *studyManifest, a *qualityAudit) { a.Cases[253].SpeechSamples = 0 }} {
		copyM, copyA := m, a
		copyM.Cases = append([]studyCase(nil), m.Cases...)
		copyA.Cases = append([]auditCase(nil), a.Cases...)
		mutate(&copyM, &copyA)
		mp, ap = writeStudy(t, copyM, copyA)
		if _, _, _, e := loadStudyManifest(mp, ap); e == nil {
			t.Fatal("accepted invalid study")
		}
	}
}
func TestStudyRetainsQualityFailedKnownQueries(t *testing.T) {
	m, a := studyFixture(t)
	a.Cases[3].SpeechSamples = 0
	a.Cases[3].RMS = 0
	a.Cases[4].SpeechSamples = 0
	a.Cases[4].RMS = 0
	mp, ap := writeStudy(t, m, a)
	if _, _, _, e := loadStudyManifest(mp, ap); e != nil {
		t.Fatal("lost denominator", e)
	}
}
func TestStudyRejectsCalibrationEvaluationAlias(t *testing.T) {
	m, a := studyFixture(t)
	c := m.Cases[253]
	c.Role = "calibration_evaluation"
	c.ID = "case_alias"
	h := sha256.Sum256([]byte(c.ID))
	c.SHA = hex.EncodeToString(h[:])
	c.TextSHA, c.RecordingSHA = c.SHA, c.SHA
	c.Path = filepath.Join(t.TempDir(), "alias.wav")
	os.WriteFile(c.Path, []byte("placeholder"), 0600)
	m.Cases = append(m.Cases, c)
	a.Cases = append(a.Cases, auditCase{ID: c.ID, SHA: c.SHA, Samples: c.Samples, SpeechSamples: 96000, RMS: .1})
	mp, ap := writeStudy(t, m, a)
	if _, _, _, e := loadStudyManifest(mp, ap); e == nil {
		t.Fatal("accepted undeclared calibration_evaluation role")
	}
}

// This opt-in validates frozen input binding only. It never loads an embedding
// model or performs identity inference and can be used before the scoring gate.
func TestStudyFrozenInputOptIn(t *testing.T) {
	manifest, audit := os.Getenv("SPEAKER_LAB_TEST_STUDY_MANIFEST"), os.Getenv("SPEAKER_LAB_TEST_STUDY_AUDIT")
	if manifest == "" || audit == "" {
		t.Skip("explicit study manifest+audit validation opt-in not set")
	}
	m, _, digest, e := loadStudyManifest(manifest, audit)
	if e != nil {
		t.Fatal("invalid frozen study input")
	}
	if len(digest) != 64 || len(m.Cases) < 1 {
		t.Fatal("invalid study metadata")
	}
}

func TestBoundAuditStrictSchemaAndDigest(t *testing.T) {
	a := newQualityAudit(sileroSHA, fmt.Sprintf("%064d", 1))
	a.Cases = []auditCase{{ID: "case_1", SHA: fmt.Sprintf("%064d", 2), Samples: 32000, SpeechSamples: 16000, RMS: .1}}
	original, _ := json.Marshal(a)
	p := filepath.Join(t.TempDir(), "audit.json")
	check := func(b []byte, wantOK bool) {
		t.Helper()
		os.WriteFile(p, b, 0600)
		h := sha256.Sum256(b)
		_, e := loadBoundAudit(p, hex.EncodeToString(h[:]))
		if (e == nil) != wantOK {
			t.Fatalf("audit validity=%v wanted=%v", e, wantOK)
		}
	}
	check(original, true)
	if _, e := loadBoundAudit(p, fmt.Sprintf("%064d", 3)); e == nil {
		t.Fatal("accepted unbound audit")
	}
	for name, mutate := range map[string]func(map[string]any){
		"missing flag": func(m map[string]any) { delete(m, "human_acceptance") }, "true flag": func(m map[string]any) { m["feature_enabled"] = true }, "null flag": func(m map[string]any) { m["production_acceptance"] = nil }, "unknown": func(m map[string]any) { m["extra"] = 1 }, "bad source": func(m map[string]any) { m["source_kind"] = "synthetic_tts" }, "bad manifest sha": func(m map[string]any) { m["manifest_sha256"] = "bad" }, "missing config": func(m map[string]any) { delete(m["config"].(map[string]any), "threshold") }, "unknown config": func(m map[string]any) { m["config"].(map[string]any)["extra"] = 1 }, "bad config": func(m map[string]any) { m["config"].(map[string]any)["threads"] = 2 }, "empty cases": func(m map[string]any) { m["cases"] = []any{} }, "null case": func(m map[string]any) { m["cases"] = []any{nil} }, "duplicate case": func(m map[string]any) { m["cases"] = append(m["cases"].([]any), m["cases"].([]any)[0]) }, "speech outside clip": func(m map[string]any) { m["cases"].([]any)[0].(map[string]any)["speech_samples"] = 32001 }, "bad rms": func(m map[string]any) { m["cases"].([]any)[0].(map[string]any)["rms"] = -.1 }, "bad clipped": func(m map[string]any) { m["cases"].([]any)[0].(map[string]any)["clipped_samples"] = 32001 }, "missing row": func(m map[string]any) { delete(m["cases"].([]any)[0].(map[string]any), "sha256") },
	} {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			json.Unmarshal(original, &m)
			mutate(m)
			b, _ := json.Marshal(m)
			check(b, false)
		})
	}
	for name, b := range map[string][]byte{"duplicate config": bytes.Replace(original, []byte(`"threshold":`), []byte(`"threshold":0.5,"threshold":`), 1), "duplicate row": bytes.Replace(original, []byte(`"speech_samples":`), []byte(`"samples":32000,"speech_samples":`), 1), "trailing": append(append([]byte(nil), original...), []byte(` {}`)...), "oversize": bytes.Repeat([]byte(" "), studyMaxBytes+1)} {
		t.Run(name, func(t *testing.T) { check(b, false) })
	}
}
func TestStudyStrictNewSchemaAndSymlink(t *testing.T) {
	m, a := studyFixture(t)
	mp, ap := writeStudy(t, m, a)
	b, _ := os.ReadFile(mp)
	for name, invalid := range map[string][]byte{"unknown": bytes.Replace(b, []byte(`"source_kind":`), []byte(`"extra":0,"source_kind":`), 1), "duplicate header": bytes.Replace(b, []byte(`"source_kind":`), []byte(`"source_kind":"public_human","source_kind":`), 1), "missing": bytes.Replace(b, []byte(`"sample_rate":16000,`), nil, 1), "null": bytes.Replace(b, []byte(`"sample_rate":16000`), []byte(`"sample_rate":null`), 1), "old split": bytes.Replace(b, []byte(`"role":`), []byte(`"split":`), 1), "duplicate role": bytes.Replace(b, []byte(`"role":`), []byte(`"role":"known_enrollment","role":`), 1), "unknown case": bytes.Replace(b, []byte(`"recording_sha256":`), []byte(`"extra":0,"recording_sha256":`), 1)} {
		t.Run(name, func(t *testing.T) {
			os.WriteFile(mp, invalid, 0600)
			if _, _, _, e := loadStudyManifest(mp, ap); e == nil {
				t.Fatal("accepted invalid schema")
			}
		})
	}
	link := filepath.Join(t.TempDir(), "link.wav")
	os.Symlink(m.Cases[0].Path, link)
	m.Cases[0].Path = link
	mp, ap = writeStudy(t, m, a)
	if _, _, _, e := loadStudyManifest(mp, ap); e == nil {
		t.Fatal("accepted symlink audio")
	}
}
