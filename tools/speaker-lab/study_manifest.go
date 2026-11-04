//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

type studyCase struct {
	ID           string `json:"id"`
	Speaker      string `json:"speaker"`
	Role         string `json:"role"`
	Path         string `json:"path"`
	Samples      int    `json:"samples"`
	SHA          string `json:"sha256"`
	RecordingSHA string `json:"recording_sha256"`
	TextSHA      string `json:"text_sha256"`
}

func (c studyCase) fixture() fixtureCase {
	return fixtureCase{ID: c.ID, Speaker: c.Speaker, Split: c.Role, Path: c.Path, Samples: c.Samples, SHA: c.SHA, TextSHA: c.TextSHA}
}

type studyManifest struct {
	SourceKind         string      `json:"source_kind"`
	SourceID           string      `json:"source_id"`
	SourceURL          string      `json:"source_url"`
	LicenseURL         string      `json:"license_url"`
	SingleSpeakerBasis string      `json:"single_speaker_basis"`
	SampleRate         int         `json:"sample_rate"`
	AuditSHA           string      `json:"quality_audit_sha256"`
	Cases              []studyCase `json:"cases"`
}

func studySpeaker(s string) (string, int) {
	for _, x := range []struct {
		prefix string
		count  int
	}{{"fixture", 50}, {"calibration", 10}, {"unknown", 5}} {
		n, e := strconv.Atoi(strings.TrimPrefix(s, x.prefix+"_"))
		if e == nil && n >= 1 && n <= x.count && s == fmt.Sprintf("%s_%d", x.prefix, n) {
			if x.prefix == "fixture" {
				return "known", n
			}
			return x.prefix, n
		}
	}
	return "", 0
}
func loadBoundAudit(path, expected string) (qualityAudit, error) {
	var a qualityAudit
	b, e := boundedFile(path, studyMaxBytes)
	if e != nil {
		return a, e
	}
	defer clear(b)
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != expected {
		return a, errProtocol
	}
	var raw struct {
		SourceKind           string            `json:"source_kind"`
		ModelSHA             string            `json:"model_sha256"`
		ManifestSHA          string            `json:"manifest_sha256"`
		Config               json.RawMessage   `json:"config"`
		Cases                []json.RawMessage `json:"cases"`
		HumanAcceptance      bool              `json:"human_acceptance"`
		ProductionAcceptance bool              `json:"production_acceptance"`
		FeatureEnabled       bool              `json:"feature_enabled"`
	}
	if strictHeader(b, &raw) != nil || raw.SourceKind != "public_human" || raw.ModelSHA != sileroSHA || !shaPattern.MatchString(raw.ManifestSHA) || raw.HumanAcceptance || raw.ProductionAcceptance || raw.FeatureEnabled || len(raw.Cases) < 1 || len(raw.Cases) > 2048 || strictHeader(raw.Config, &a.Config) != nil || a.Config != fixedVADConfig() {
		return a, errProtocol
	}
	a.SourceKind, a.ModelSHA, a.ManifestSHA = raw.SourceKind, raw.ModelSHA, raw.ManifestSHA
	ids, hashes := map[string]bool{}, map[string]bool{}
	for _, r := range raw.Cases {
		var c auditCase
		if strictHeader(r, &c) != nil || !tokenPattern.MatchString(c.ID) || ids[c.ID] || !shaPattern.MatchString(c.SHA) || hashes[c.SHA] || c.Samples < 1 || c.Samples > maxSamples || c.SpeechSamples < 0 || c.SpeechSamples > c.Samples || math.IsNaN(c.RMS) || math.IsInf(c.RMS, 0) || c.RMS < 0 || c.RMS > 1 || c.ClippedSamples < 0 || c.ClippedSamples > c.Samples {
			return a, errProtocol
		}
		ids[c.ID], hashes[c.SHA] = true, true
		a.Cases = append(a.Cases, c)
	}
	return a, nil
}
func loadStudyManifest(path, auditPath string) (studyManifest, qualityAudit, string, error) {
	var m studyManifest
	var a qualityAudit
	b, e := boundedFile(path, studyMaxBytes)
	if e != nil {
		return m, a, "", e
	}
	defer clear(b)
	var raw struct {
		SourceKind         string            `json:"source_kind"`
		SourceID           string            `json:"source_id"`
		SourceURL          string            `json:"source_url"`
		LicenseURL         string            `json:"license_url"`
		SingleSpeakerBasis string            `json:"single_speaker_basis"`
		SampleRate         int               `json:"sample_rate"`
		AuditSHA           string            `json:"quality_audit_sha256"`
		Cases              []json.RawMessage `json:"cases"`
	}
	if strictHeader(b, &raw) != nil || raw.SourceKind != "public_human" || !tokenPattern.MatchString(raw.SourceID) || !httpsReference(raw.SourceURL) || !httpsReference(raw.LicenseURL) || raw.SingleSpeakerBasis != "source_annotations" || raw.SampleRate != 16000 || !shaPattern.MatchString(raw.AuditSHA) || len(raw.Cases) < 1 || len(raw.Cases) > 2048 {
		return m, a, "", errProtocol
	}
	m = studyManifest{SourceKind: raw.SourceKind, SourceID: raw.SourceID, SourceURL: raw.SourceURL, LicenseURL: raw.LicenseURL, SingleSpeakerBasis: raw.SingleSpeakerBasis, SampleRate: raw.SampleRate, AuditSHA: raw.AuditSHA}
	a, e = loadBoundAudit(auditPath, m.AuditSHA)
	if e != nil {
		return m, a, "", e
	}
	audit := map[string]auditCase{}
	for _, c := range a.Cases {
		audit[c.ID] = c
	}
	ids, paths, hashes, recordings := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	textRoles := map[string]string{}
	personTexts := map[string]bool{}
	counts, total, last := map[string][2]int{}, map[string]int{}, map[string]int{}
	for _, r := range raw.Cases {
		var c studyCase
		if strictHeader(r, &c) != nil || !tokenPattern.MatchString(c.ID) || ids[c.ID] || !filepath.IsAbs(c.Path) || filepath.Clean(c.Path) != c.Path || !noSymlinks(c.Path) || paths[c.Path] || c.Samples < 32000 || c.Samples > maxSamples || !shaPattern.MatchString(c.SHA) || hashes[c.SHA] || !shaPattern.MatchString(c.RecordingSHA) || recordings[c.RecordingSHA] || !shaPattern.MatchString(c.TextSHA) {
			return m, a, "", errProtocol
		}
		group, _ := studySpeaker(c.Speaker)
		enroll := c.Role == group+"_enrollment"
		query := (group == "known" || group == "unknown") && c.Role == group+"_evaluation" || group == "calibration" && c.Role == "calibration_query"
		if group == "" || !enroll && !query || group == "unknown" && enroll {
			return m, a, "", errProtocol
		}
		ac, ok := audit[c.ID]
		if !ok || ac.SHA != c.SHA || ac.Samples != c.Samples {
			return m, a, "", errProtocol
		}
		both := ac.RMS >= minRMS && ac.SpeechSamples >= 16000 && c.Samples >= minQualitySamples
		if old, ok := textRoles[c.TextSHA]; ok && old != c.Role {
			return m, a, "", errProtocol
		}
		pt := c.Speaker + ":" + c.TextSHA
		if personTexts[pt] {
			return m, a, "", errProtocol
		}
		n := counts[c.Speaker]
		if enroll {
			if !both || total[c.Speaker] >= 20*sampleRate {
				return m, a, "", errProtocol
			}
			last[c.Speaker] = c.Samples
			total[c.Speaker] += c.Samples
			n[0]++
		} else {
			n[1]++
		}
		counts[c.Speaker] = n
		ids[c.ID], paths[c.Path], hashes[c.SHA], recordings[c.RecordingSHA], personTexts[pt] = true, true, true, true, true
		textRoles[c.TextSHA] = c.Role
		m.Cases = append(m.Cases, c)
	}
	if len(counts) != 65 {
		return m, a, "", errProtocol
	}
	for _, group := range []struct {
		prefix string
		n      int
	}{{"fixture", 50}, {"calibration", 10}, {"unknown", 5}} {
		for i := 1; i <= group.n; i++ {
			s := fmt.Sprintf("%s_%d", group.prefix, i)
			n, ok := counts[s]
			if !ok || n[1] < 2 {
				return m, a, "", errProtocol
			}
			if group.prefix == "unknown" {
				if n[0] != 0 || n[1] > 5 {
					return m, a, "", errProtocol
				}
				continue
			}
			if n[0] < 2 || total[s] < 320000 || total[s] > 448000 || total[s]-last[s] >= 320000 {
				return m, a, "", errProtocol
			}
			if group.prefix == "calibration" {
				qualifiedQueries := 0
				for _, c := range m.Cases {
					if c.Speaker == s && c.Role == "calibration_query" {
						ac := audit[c.ID]
						if ac.RMS >= minRMS && ac.SpeechSamples >= 16000 {
							qualifiedQueries++
						}
					}
				}
				if qualifiedQueries < 2 {
					return m, a, "", errProtocol
				}
			}
		}
	}
	h := sha256.Sum256(b)
	return m, a, hex.EncodeToString(h[:]), nil
}
