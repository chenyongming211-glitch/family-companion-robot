//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func backendSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func backendFixture(t *testing.T) (backendManifest, qualityAudit) {
	t.Helper()
	m := backendManifest{SourceKind: "public_human", SourceID: "corpus", SourceURL: "https://example.org/source", LicenseURL: "https://example.org/license", SingleSpeakerBasis: "source_annotations", Condition: "device03", SampleRate: 16000}
	a := newQualityAudit(sileroSHA, fmt.Sprintf("%064d", 1))
	dir := t.TempDir()
	seq := 0
	for _, g := range []struct {
		prefix string
		n      int
	}{{"train", 10}, {"validation", 50}, {"validation_unknown", 5}, {"cohort", 30}, {"test", 4}, {"test_unknown", 20}} {
		for n := 1; n <= g.n; n++ {
			roles := []string{g.prefix + "_enrollment", g.prefix + "_enrollment", g.prefix + "_enrollment", g.prefix + "_query", g.prefix + "_query"}
			if g.prefix == "cohort" {
				roles = []string{"cohort", "cohort"}
			}
			if g.prefix == "validation_unknown" || g.prefix == "test_unknown" {
				roles = []string{g.prefix, g.prefix}
			}
			for _, role := range roles {
				seq++
				samples := 112000
				wav := wavBytes(samples)
				binary.LittleEndian.PutUint16(wav[44:], uint16(seq))
				p := filepath.Join(dir, fmt.Sprintf("c%d.wav", seq))
				if e := os.WriteFile(p, wav, 0600); e != nil {
					t.Fatal(e)
				}
				h := backendSHA(wav)
				c := backendCase{ID: fmt.Sprintf("case_%d", seq), Speaker: fmt.Sprintf("%s_%d", g.prefix, n), Role: role, Path: p, Samples: samples, SHA: h, RecordingSHA: h, SourceGroupSHA: h, TextSHA: h}
				if role == "cohort" {
					c.TextSHA = ""
				}
				m.Cases = append(m.Cases, c)
				a.Cases = append(a.Cases, auditCase{ID: c.ID, SHA: h, Samples: samples, SpeechSamples: 96000, RMS: .1})
			}
		}
	}
	return m, a
}
func writeBackend(t *testing.T, m backendManifest, a qualityAudit) string {
	t.Helper()
	d := t.TempDir()
	ab, _ := json.Marshal(a)
	ap := filepath.Join(d, "audit.json")
	os.WriteFile(ap, ab, 0600)
	m.Audits = []backendAuditRef{{Path: ap, SHA: backendSHA(ab)}}
	mb, _ := json.Marshal(m)
	mp := filepath.Join(d, "manifest.json")
	os.WriteFile(mp, mb, 0600)
	return mp
}
func TestBackendManifestBindingRolesFirstCrossing(t *testing.T) {
	m, a := backendFixture(t)
	p := writeBackend(t, m, a)
	loaded, audits, h, e := loadBackendManifest(p)
	if e != nil || len(loaded.Cases) != len(m.Cases) || len(audits) != len(a.Cases) || len(h) != 64 {
		t.Fatal("valid manifest refused", e)
	}
	for name, mutate := range map[string]func(*backendManifest, *qualityAudit){
		"role": func(m *backendManifest, a *qualityAudit) { m.Cases[0].Role = "train_query" }, "prefix": func(m *backendManifest, a *qualityAudit) { m.Cases[0].Speaker = "train_01" }, "samples": func(m *backendManifest, a *qualityAudit) { m.Cases[0].Samples-- }, "audit sha": func(m *backendManifest, a *qualityAudit) { a.Cases[0].SHA = fmt.Sprintf("%064d", 99) }, "config": func(m *backendManifest, a *qualityAudit) { a.Config.Threads = 2 }, "enrollment vad": func(m *backendManifest, a *qualityAudit) { a.Cases[0].SpeechSamples = 0 }, "enrollment energy": func(m *backendManifest, a *qualityAudit) { a.Cases[0].RMS = 0 }, "repeat text": func(m *backendManifest, a *qualityAudit) { m.Cases[3].TextSHA = m.Cases[0].TextSHA }, "repeat source": func(m *backendManifest, a *qualityAudit) { m.Cases[3].SourceGroupSHA = m.Cases[0].SourceGroupSHA }, "crossrole source": func(m *backendManifest, a *qualityAudit) { m.Cases[5].SourceGroupSHA = m.Cases[0].SourceGroupSHA }, "duplicate sha": func(m *backendManifest, a *qualityAudit) { m.Cases[1].SHA = m.Cases[0].SHA }, "duplicate id": func(m *backendManifest, a *qualityAudit) { m.Cases[1].ID = m.Cases[0].ID }, "duplicate path": func(m *backendManifest, a *qualityAudit) { m.Cases[1].Path = m.Cases[0].Path }, "missing cohort": func(m *backendManifest, a *qualityAudit) { m.Cases = m.Cases[:300] }, "condition": func(m *backendManifest, a *qualityAudit) { m.Condition = "near" }, "first crossing": func(m *backendManifest, a *qualityAudit) { m.Cases[3].Role = "train_enrollment" },
	} {
		t.Run(name, func(t *testing.T) {
			cm, ca := m, a
			cm.Cases = append([]backendCase(nil), m.Cases...)
			ca.Cases = append([]auditCase(nil), a.Cases...)
			mutate(&cm, &ca)
			if _, _, _, e := loadBackendManifest(writeBackend(t, cm, ca)); e == nil {
				t.Fatal("accepted invalid")
			}
		})
	}
	// Audit IDs are local: the binding key is SHA. Repeated text between distinct people is permitted.
	a.Cases[5].ID = a.Cases[0].ID // inside one audit must still be refused.
	if _, _, _, e := loadBackendManifest(writeBackend(t, m, a)); e == nil {
		t.Fatal("duplicate audit ID")
	}
}
func TestBackendStrictSchemaAndBounds(t *testing.T) {
	m, a := backendFixture(t)
	p := writeBackend(t, m, a)
	b, _ := os.ReadFile(p)
	for name, bad := range map[string][]byte{"unknown": bytes.Replace(b, []byte(`"condition":`), []byte(`"extra":0,"condition":`), 1), "duplicate": bytes.Replace(b, []byte(`"condition":`), []byte(`"condition":"device03","condition":`), 1), "null": bytes.Replace(b, []byte(`"sample_rate":16000`), []byte(`"sample_rate":null`), 1), "missing": bytes.Replace(b, []byte(`"sample_rate":16000,`), nil, 1), "case unknown": bytes.Replace(b, []byte(`"role":`), []byte(`"extra":0,"role":`), 1), "null row": bytes.Replace(b, []byte(`"cases":[`), []byte(`"cases":[null,`), 1), "oversize": bytes.Repeat([]byte(" "), 4*1024*1024+1)} {
		t.Run(name, func(t *testing.T) {
			os.WriteFile(p, bad, 0600)
			if _, _, _, e := loadBackendManifest(p); e == nil {
				t.Fatal("accepted bad schema")
			}
		})
	}
	p = writeBackend(t, m, a)
	os.WriteFile(m.Cases[0].Path, []byte("bad wav"), 0600)
	if _, _, _, e := loadBackendManifest(p); e == nil {
		t.Fatal("bad WAV accepted")
	}
}
func TestBackendAuditSHAKeyAndEnergyOnlyQueries(t *testing.T) {
	m, a := backendFixture(t)
	a.Cases[3].RMS = 0
	a.Cases[3].SpeechSamples = 0
	a.Cases[4].SpeechSamples = 0
	// Same text may occur for different people, never within one person.
	m.Cases[5].TextSHA = m.Cases[0].TextSHA
	p := writeBackend(t, m, a)
	if _, _, _, e := loadBackendManifest(p); e != nil {
		t.Fatal("query quality or cross-person text", e)
	}
	// Two bound audits may reuse IDs; WAV SHA cannot repeat across them.
	first, second := a, a
	first.Cases = append([]auditCase(nil), a.Cases[:100]...)
	second.Cases = append([]auditCase(nil), a.Cases[100:]...)
	second.Cases[0].ID = first.Cases[0].ID
	d := t.TempDir()
	m.Audits = nil
	for i, x := range []qualityAudit{first, second} {
		ab, _ := json.Marshal(x)
		p := filepath.Join(d, fmt.Sprintf("a%d.json", i))
		os.WriteFile(p, ab, 0600)
		m.Audits = append(m.Audits, backendAuditRef{Path: p, SHA: backendSHA(ab)})
	}
	mb, _ := json.Marshal(m)
	p = filepath.Join(d, "manifest.json")
	os.WriteFile(p, mb, 0600)
	if _, _, _, e := loadBackendManifest(p); e != nil {
		t.Fatal("SHA keyed audits", e)
	}
	m.Audits = append(m.Audits, m.Audits[0])
	mb, _ = json.Marshal(m)
	os.WriteFile(p, mb, 0600)
	if _, _, _, e := loadBackendManifest(p); e == nil {
		t.Fatal("duplicate audit")
	}
}
func TestBackendAdditionalBoundsAndBindings(t *testing.T) {
	m, a := backendFixture(t)
	for name, mutate := range map[string]func(*backendManifest, *qualityAudit){"recording duplicate": func(m *backendManifest, a *qualityAudit) { m.Cases[1].RecordingSHA = m.Cases[0].RecordingSHA }, "missing audit coverage": func(m *backendManifest, a *qualityAudit) { a.Cases = a.Cases[1:] }, "audit samples": func(m *backendManifest, a *qualityAudit) { a.Cases[0].Samples-- }, "enrollment over 28s": func(m *backendManifest, a *qualityAudit) { m.Cases[3].Role = "train_enrollment" }, "cohort source duplicate": func(m *backendManifest, a *qualityAudit) {
		for i, c := range m.Cases {
			if c.Role == "cohort" {
				m.Cases[i+1].SourceGroupSHA = c.SourceGroupSHA
				break
			}
		}
	}, "cohort VAD": func(m *backendManifest, a *qualityAudit) {
		for i, c := range m.Cases {
			if c.Role == "cohort" {
				a.Cases[i].SpeechSamples = 0
				break
			}
		}
	}, "unknown more than 5": func(m *backendManifest, a *qualityAudit) {
		for i := range m.Cases {
			if m.Cases[i].Speaker == "test_unknown_2" || m.Cases[i].Speaker == "test_unknown_3" {
				m.Cases[i].Speaker = "test_unknown_1"
			}
		}
	}, "case cap": func(m *backendManifest, a *qualityAudit) { m.Cases = make([]backendCase, 4097) }, "test gap": func(m *backendManifest, a *qualityAudit) {
		for i := range m.Cases {
			if m.Cases[i].Speaker == "test_2" {
				m.Cases[i].Speaker = "test_5"
			}
		}
	}} {
		t.Run(name, func(t *testing.T) {
			cm, ca := m, a
			cm.Cases = append([]backendCase(nil), m.Cases...)
			ca.Cases = append([]auditCase(nil), a.Cases...)
			mutate(&cm, &ca)
			if _, _, _, e := loadBackendManifest(writeBackend(t, cm, ca)); e == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	p := writeBackend(t, m, a)
	mb, _ := os.ReadFile(p)
	var header map[string]any
	json.Unmarshal(mb, &header)
	ref := header["audits"].([]any)[0]
	header["audits"] = []any{ref, ref, ref, ref, ref}
	b, _ := json.Marshal(header)
	os.WriteFile(p, b, 0600)
	if _, _, _, e := loadBackendManifest(p); e == nil {
		t.Fatal("audit cap")
	}
	link := filepath.Join(t.TempDir(), "link.wav")
	os.Symlink(m.Cases[0].Path, link)
	m.Cases[0].Path = link
	if _, _, _, e := loadBackendManifest(writeBackend(t, m, a)); e == nil {
		t.Fatal("symlink")
	}
}

// Read-only input contract gate. It never starts a worker or extracts identity.
func TestBackendFrozenManifestOptIn(t *testing.T) {
	p := os.Getenv("SPEAKER_LAB_BACKEND_MANIFEST")
	if p == "" {
		t.Skip("explicit backend input-contract opt-in not set")
	}
	m, a, h, e := loadBackendManifest(p)
	if e != nil {
		t.Fatal("frozen backend input invalid", e)
	}
	people := map[string]bool{}
	for _, c := range m.Cases {
		people[c.Speaker] = true
	}
	t.Logf("condition=%s cases=%d people=%d bound_audit_cases=%d manifest_sha256=%s", m.Condition, len(m.Cases), len(people), len(a), h)
}
