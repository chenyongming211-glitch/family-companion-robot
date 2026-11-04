//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const backendMaxBytes = 4 * 1024 * 1024

func backendSpeaker(s string) (string, int) {
	for _, p := range []string{"validation_unknown", "test_unknown", "validation", "cohort", "train", "test"} {
		if !strings.HasPrefix(s, p+"_") {
			continue
		}
		n, e := strconv.Atoi(strings.TrimPrefix(s, p+"_"))
		if e == nil && n >= 1 && n <= 240 && s == fmt.Sprintf("%s_%d", p, n) {
			return p, n
		}
	}
	return "", 0
}

type backendCase struct {
	ID             string `json:"id"`
	Speaker        string `json:"speaker"`
	Role           string `json:"role"`
	Path           string `json:"path"`
	Samples        int    `json:"samples"`
	SHA            string `json:"sha256"`
	RecordingSHA   string `json:"recording_sha256"`
	SourceGroupSHA string `json:"source_group_sha256"`
	TextSHA        string `json:"text_sha256"`
}
type backendAuditRef struct {
	Path string `json:"path"`
	SHA  string `json:"sha256"`
}
type backendManifest struct {
	SourceKind         string            `json:"source_kind"`
	SourceID           string            `json:"source_id"`
	SourceURL          string            `json:"source_url"`
	LicenseURL         string            `json:"license_url"`
	SingleSpeakerBasis string            `json:"single_speaker_basis"`
	Condition          string            `json:"condition"`
	SampleRate         int               `json:"sample_rate"`
	Audits             []backendAuditRef `json:"audits"`
	Cases              []backendCase     `json:"cases"`
}

// Audit IDs are scoped to each audit; WAV SHA is the cross-audit binding key.
// Return values contain private paths only internally, never in the report.
func loadBackendManifest(path string) (backendManifest, map[string]auditCase, string, error) {
	var m backendManifest
	fail := func() (backendManifest, map[string]auditCase, string, error) {
		return backendManifest{}, nil, "", errProtocol
	}
	b, e := boundedFile(path, backendMaxBytes)
	if e != nil {
		return fail()
	}
	defer clear(b)
	var raw struct {
		SourceKind         string            `json:"source_kind"`
		SourceID           string            `json:"source_id"`
		SourceURL          string            `json:"source_url"`
		LicenseURL         string            `json:"license_url"`
		SingleSpeakerBasis string            `json:"single_speaker_basis"`
		Condition          string            `json:"condition"`
		SampleRate         int               `json:"sample_rate"`
		Audits             []json.RawMessage `json:"audits"`
		Cases              []json.RawMessage `json:"cases"`
	}
	if strictHeader(b, &raw) != nil || raw.SourceKind != "public_human" || !tokenPattern.MatchString(raw.SourceID) || !httpsReference(raw.SourceURL) || !httpsReference(raw.LicenseURL) || raw.SingleSpeakerBasis != "source_annotations" || raw.SampleRate != sampleRate || (raw.Condition != "device03" && raw.Condition != "device06") || len(raw.Audits) < 1 || len(raw.Audits) > 4 || len(raw.Cases) < 1 || len(raw.Cases) > 4096 {
		return fail()
	}
	m = backendManifest{SourceKind: raw.SourceKind, SourceID: raw.SourceID, SourceURL: raw.SourceURL, LicenseURL: raw.LicenseURL, SingleSpeakerBasis: raw.SingleSpeakerBasis, SampleRate: raw.SampleRate, Condition: raw.Condition}
	audit := map[string]auditCase{}
	aps, ahs := map[string]bool{}, map[string]bool{}
	for _, r := range raw.Audits {
		var ref backendAuditRef
		if strictHeader(r, &ref) != nil || !filepath.IsAbs(ref.Path) || filepath.Clean(ref.Path) != ref.Path || aps[ref.Path] || !shaPattern.MatchString(ref.SHA) || ahs[ref.SHA] {
			return fail()
		}
		a, e := loadBoundAudit(ref.Path, ref.SHA)
		if e != nil {
			return fail()
		}
		for _, c := range a.Cases {
			if _, ok := audit[c.SHA]; ok {
				return fail()
			}
			audit[c.SHA] = c
		}
		aps[ref.Path], ahs[ref.SHA] = true, true
		m.Audits = append(m.Audits, ref)
	}
	ids, paths, hashes, recordings, personTexts, sourceGroups := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	counts, total, last := map[string][2]int{}, map[string]int{}, map[string]int{}
	for _, r := range raw.Cases {
		var c backendCase
		if strictHeader(r, &c) != nil || !tokenPattern.MatchString(c.ID) || ids[c.ID] || !filepath.IsAbs(c.Path) || filepath.Clean(c.Path) != c.Path || paths[c.Path] || c.Samples < 32000 || c.Samples > maxSamples || !shaPattern.MatchString(c.SHA) || hashes[c.SHA] || !shaPattern.MatchString(c.RecordingSHA) || recordings[c.RecordingSHA] || !shaPattern.MatchString(c.SourceGroupSHA) || sourceGroups[c.SourceGroupSHA] {
			return fail()
		}
		g, _ := backendSpeaker(c.Speaker)
		enroll := c.Role == g+"_enrollment"
		query := c.Role == g+"_query"
		if g == "cohort" {
			enroll = c.Role == "cohort"
			query = false
		} else if strings.HasSuffix(g, "_unknown") {
			enroll = false
			query = c.Role == g
		}
		if g == "" || (!enroll && !query) || (g != "cohort" || c.TextSHA != "") && !shaPattern.MatchString(c.TextSHA) {
			return fail()
		}
		ac, ok := audit[c.SHA]
		if !ok || ac.Samples != c.Samples {
			return fail()
		}
		pt := c.Speaker + ":" + c.TextSHA
		if c.TextSHA != "" && personTexts[pt] {
			return fail()
		}
		n := counts[c.Speaker]
		if enroll {
			if ac.RMS < minRMS || ac.SpeechSamples < 16000 || c.Samples < minQualitySamples {
				return fail()
			}
			if g != "cohort" && total[c.Speaker] >= 320000 {
				return fail()
			}
			n[0]++
			last[c.Speaker] = c.Samples
			total[c.Speaker] += c.Samples
		} else {
			n[1]++
		}
		pcm, e := loadFixture(fixtureCase{ID: c.ID, Path: c.Path, Samples: c.Samples, SHA: c.SHA})
		if e != nil {
			return fail()
		}
		clear(pcm)
		counts[c.Speaker] = n
		ids[c.ID], paths[c.Path], hashes[c.SHA], recordings[c.RecordingSHA], sourceGroups[c.SourceGroupSHA] = true, true, true, true, true
		if c.TextSHA != "" {
			personTexts[pt] = true
		}
		m.Cases = append(m.Cases, c)
	}
	if len(counts) > 240 {
		return fail()
	}
	sizes := map[string]int{}
	for s, n := range counts {
		g, i := backendSpeaker(s)
		if i > sizes[g] {
			sizes[g] = i
		}
		if g == "cohort" {
			if n[0] != 2 || n[1] != 0 {
				return fail()
			}
			continue
		}
		if n[1] < 2 {
			return fail()
		}
		if strings.HasSuffix(g, "_unknown") {
			if n[0] != 0 || n[1] > 5 {
				return fail()
			}
			continue
		}
		if n[0] < 2 || total[s] < 320000 || total[s] > 448000 || total[s]-last[s] >= 320000 {
			return fail()
		}
	}
	if sizes["train"] != 10 || sizes["validation"] != 50 || sizes["validation_unknown"] != 5 || sizes["cohort"] != 30 || sizes["test"] < 1 || sizes["test"] > 5 || sizes["test_unknown"] != 20 {
		return fail()
	}
	for g, n := range sizes {
		for i := 1; i <= n; i++ {
			if _, ok := counts[fmt.Sprintf("%s_%d", g, i)]; !ok {
				return fail()
			}
		}
	}
	h := sha256.Sum256(b)
	return m, audit, hex.EncodeToString(h[:]), nil
}
