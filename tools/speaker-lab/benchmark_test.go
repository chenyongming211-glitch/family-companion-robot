//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func benchmarkFixtures() []embeddedCase {
	var out []embeddedCase
	for s := 0; s < 3; s++ {
		for n := 0; n < 5; n++ {
			v := []float32{0, 0, 0}
			v[s] = 1
			split := "holdout"
			if n < 2 {
				split = "calibration"
			}
			out = append(out, embeddedCase{fixtureCase: fixtureCase{ID: string(rune('a' + s*5 + n)), Speaker: "fixture_" + string(rune('1'+s)), Split: split, Samples: 32000}, vector: v})
		}
	}
	return out
}
func TestCalibrationAndAnonymousSequence(t *testing.T) {
	cases := benchmarkFixtures()
	cal := calibrate(cases)
	if cal.Status != "separable" || cal.SameMin != 1 || cal.DifferentMax != 0 || cal.NewThreshold != .25 || cal.MatchThreshold != .5 || cal.Margin != .125 {
		t.Fatalf("bad calibration: %+v", cal)
	}
	// The excluded third voice must not influence fitting.
	for i := 10; i < 12; i++ {
		cases[i].vector = []float32{1, 0, 0}
	}
	if got := calibrate(cases); got != cal {
		t.Fatal("third voice affected thresholds")
	}
	r, e := analyzeBenchmark(cases, testSHA, 3)
	if e != nil || r.Sequence.Stable != 18 || r.Sequence.Uncertain != 0 || r.Capacity.Unknown != 6 || r.Capacity.KnownReacquired != 12 {
		t.Fatalf("unexpected heldout behavior: %+v %v", r, e)
	}
	b, _ := json.Marshal(r)
	for _, secret := range []string{"vector", "path", "text_sha256", "speaker\"", "embedding"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("report exposes %s", secret)
		}
	}
}
func TestCalibrationRefusesNonseparableAndInvalidVectors(t *testing.T) {
	cases := benchmarkFixtures()
	cases[5].vector = []float32{1, 0, 0}
	r, e := analyzeBenchmark(cases, testSHA, 3)
	if e != nil || r.Calibration.Status != "failed_nonseparable" || r.Sequence.Status != "not_run" {
		t.Fatalf("fallback thresholds used: %+v %v", r, e)
	}
	cases = benchmarkFixtures()
	cases[0].vector = []float32{float32(math.NaN()), 0, 0}
	if _, e = analyzeBenchmark(cases, testSHA, 3); e == nil {
		t.Fatal("invalid vector accepted")
	}
}
func TestBenchmarkQualityGate(t *testing.T) {
	pcm := make([]byte, 48000)
	for i := 0; i < len(pcm); i += 2 {
		pcm[i] = 0
		pcm[i+1] = 16
	}
	if qualityReason(pcm) != "qualified" {
		t.Fatal("bounded signal rejected")
	}
	if qualityReason(pcm[:47998]) != "short" {
		t.Fatal("short accepted")
	}
	clear(pcm)
	if qualityReason(pcm) != "silence" {
		t.Fatal("silence accepted")
	}
}
func testManifest(t *testing.T) (fixtureManifest, string) {
	t.Helper()
	m := fixtureManifest{SourceKind: "synthetic_tts", Model: "test_tts", SampleRate: 16000, SingleSpeakerOnly: true}
	for i, c := range benchmarkFixtures() {
		c.fixtureCase.Path = filepath.Join(t.TempDir(), "secret.wav")
		c.SHA = strings.Repeat("a", 64)
		c.TextSHA = strings.Repeat("b", 64)
		if c.Split == "holdout" {
			c.TextSHA = strings.Repeat("c", 64)
		}
		m.Cases = append(m.Cases, c.fixtureCase)
		_ = i
	}
	p := filepath.Join(t.TempDir(), "manifest.json")
	return m, p
}
func writeManifest(t *testing.T, m fixtureManifest, p string) {
	t.Helper()
	b, _ := json.Marshal(m)
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestManifestBoundariesAndSplitIsolation(t *testing.T) {
	m, p := testManifest(t)
	tests := []struct {
		name   string
		mutate func(*fixtureManifest)
	}{
		{"human", func(m *fixtureManifest) { m.HumanAcceptance = true }},
		{"source", func(m *fixtureManifest) { m.SourceKind = "recording" }},
		{"rate", func(m *fixtureManifest) { m.SampleRate = 48000 }},
		{"no_single_voice_provenance", func(m *fixtureManifest) { m.SingleSpeakerOnly = false }},
		{"duplicate", func(m *fixtureManifest) { m.Cases[1].ID = m.Cases[0].ID }},
		{"repeated_text", func(m *fixtureManifest) { m.Cases[2].TextSHA = m.Cases[0].TextSHA }},
		{"repeated_audio", func(m *fixtureManifest) { m.Cases[2].SHA = m.Cases[0].SHA }},
		{"voice_name", func(m *fixtureManifest) { m.Cases[0].Speaker = "secret-person" }},
		{"too_many", func(m *fixtureManifest) {
			for len(m.Cases) < 129 {
				m.Cases = append(m.Cases, m.Cases[0])
			}
		}},
	}
	// Valid audio hashes must be unique across splits, including across voices.
	for i := range m.Cases {
		h := sha256.Sum256([]byte(m.Cases[i].ID))
		m.Cases[i].SHA = hex.EncodeToString(h[:])
	}
	writeManifest(t, m, p)
	if _, _, e := loadManifest(p); e != nil {
		t.Fatal(e)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copyM := m
			copyM.Cases = append([]fixtureCase(nil), m.Cases...)
			tt.mutate(&copyM)
			writeManifest(t, copyM, p)
			if _, _, e := loadManifest(p); e == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	writeManifest(t, m, p)
	b, _ := os.ReadFile(p)
	b = bytes.Replace(b, []byte(`"source_kind":`), []byte(`"unknown":1,"source_kind":`), 1)
	os.WriteFile(p, b, 0600)
	if _, _, e := loadManifest(p); e == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestFixtureHashAndSampleMetadata(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secret.wav")
	b := wavBytes(32000)
	os.WriteFile(p, b, 0600)
	h := sha256.Sum256(b)
	c := fixtureCase{Path: p, Samples: 32000, SHA: hex.EncodeToString(h[:])}
	pcm, e := loadFixture(c)
	if e != nil || len(pcm) != 64000 {
		t.Fatalf("valid fixture failed: %v", e)
	}
	c.Samples--
	if _, e = loadFixture(c); e == nil {
		t.Fatal("sample mismatch accepted")
	}
	c.Samples++
	c.SHA = testSHA
	if _, e = loadFixture(c); e == nil {
		t.Fatal("hash mismatch accepted")
	}
	link := p + "-link"
	os.Symlink(p, link)
	c.Path = link
	if _, e = loadFixture(c); e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestBenchmarkCLIErrorPrivacy(t *testing.T) {
	var out, errs bytes.Buffer
	if runCLI(context.Background(), []string{"benchmark", "--model", "secret-model", "--sha256", testSHA, "--manifest", "secret-manifest"}, &out, &errs) == 0 {
		t.Fatal("invalid benchmark accepted")
	}
	if out.Len() != 0 || strings.Contains(errs.String(), "secret") || !strings.Contains(errs.String(), "benchmark_failed") {
		t.Fatal("unsafe or missing error")
	}
}
