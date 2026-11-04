//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditRangesClipMerge(t *testing.T) {
	if n := speechUnion([]speechRange{{-10, 20}, {10, 40}, {40, 50}, {95, 120}}, 100); n != 55 {
		t.Fatalf("union=%d", n)
	}
	if n := speechUnion(nil, 100); n != 0 {
		t.Fatal(n)
	}
}
func TestAuditManifestIsolation(t *testing.T) {
	m := evaluationFixture()
	m.Cases = m.Cases[:1]
	m.Cases[0].Split = "evaluation"
	m.Cases[0].Speaker = "fixture_80"
	p := filepath.Join(t.TempDir(), "m.json")
	b, _ := json.Marshal(m)
	os.WriteFile(p, b, 0600)
	if _, _, e := loadQualityManifest(p); e != nil {
		t.Fatal(e)
	}
	if _, _, e := loadEvaluationManifest(p); e == nil {
		t.Fatal("ordinary limits expanded")
	}
	m.Cases[0].Split = "enrollment"
	b, _ = json.Marshal(m)
	os.WriteFile(p, b, 0600)
	if _, _, e := loadQualityManifest(p); e == nil {
		t.Fatal("audit accepted enrollment")
	}
}
func TestAuditStatisticsAndPrivacy(t *testing.T) {
	s := signalStats([]byte{0, 128, 255, 127, 0, 0})
	if s.ClippedSamples != 2 || s.RMS <= 0 {
		t.Fatal(s)
	}
	r := newQualityAudit("digest", "input")
	r.Cases = []auditCase{{ID: "case_1", SHA: "sha", Samples: 3, SpeechSamples: 2, RMS: .1}}
	b, _ := json.Marshal(r)
	for _, forbidden := range []string{"speaker", "path", "text", "vector"} {
		if bytes.Contains(b, []byte(`"`+forbidden+`"`)) {
			t.Fatalf("leak %s", b)
		}
	}
	if r.Config != fixedVADConfig() || r.ModelSHA != "digest" || r.ManifestSHA != "input" {
		t.Fatal(r)
	}
}
func TestAuditCancelledBeforeNative(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := runQualityAudit(ctx, "/does-not-exist", sileroSHA, "/does-not-exist"); e == nil {
		t.Fatal("accepted cancellation")
	}
}

type retainingVAD struct {
	accepted                [][]float32
	segments                []*sherpa.SpeechSegment
	retained                [][]float32
	resets, clears, flushes int
	ctxCancel               func()
}

func (d *retainingVAD) AcceptWaveform(v []float32) {
	d.accepted = append(d.accepted, v)
	if d.ctxCancel != nil {
		d.ctxCancel()
	}
}
func (d *retainingVAD) IsEmpty() bool { return len(d.segments) == 0 }
func (d *retainingVAD) Front() *sherpa.SpeechSegment {
	s := d.segments[0]
	d.retained = append(d.retained, s.Samples)
	return s
}
func (d *retainingVAD) Pop()   { d.segments = d.segments[1:] }
func (d *retainingVAD) Reset() { d.resets++ }
func (d *retainingVAD) Clear() { d.clears++ }
func (d *retainingVAD) Flush() {
	d.flushes++
	d.segments = []*sherpa.SpeechSegment{{Start: -10, Samples: make([]float32, 30)}, {Start: 10, Samples: make([]float32, 40)}, {Start: 95, Samples: make([]float32, 20)}}
	for _, s := range d.segments {
		for i := range s.Samples {
			s.Samples[i] = .25
		}
	}
}
func TestAuditDetectorOwnershipResetAndCancel(t *testing.T) {
	d := &retainingVAD{}
	n, e := auditPCM(context.Background(), make([]byte, 200), d)
	if e != nil || n != 55 || d.resets != 2 || d.clears != 2 || d.flushes != 1 {
		t.Fatal(n, e, d)
	}
	for _, v := range append(d.accepted, d.retained...) {
		for _, x := range v {
			if x != 0 {
				t.Fatal("retained sample")
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	d = &retainingVAD{ctxCancel: cancel}
	if _, e := auditPCM(ctx, sessionPCM(32000), d); e == nil || d.resets != 2 || d.clears != 2 {
		t.Fatal("cancel cleanup", e, d)
	}
	for _, v := range d.accepted {
		for _, x := range v {
			if x != 0 {
				t.Fatal("cancel retained samples")
			}
		}
	}
}
func TestRealVADOptIn(t *testing.T) {
	model, wav, exe := os.Getenv("SPEAKER_LAB_TEST_VAD_MODEL"), os.Getenv("SPEAKER_LAB_TEST_WAV"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if model == "" || wav == "" || exe == "" {
		t.Skip("explicit VAD model+synthetic WAV+binary opt-in not set")
	}
	b, e := os.ReadFile(wav)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(b)
	h := sha256.Sum256(b)
	pcm, e := readWAV(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	defer clear(pcm)
	m := evaluationFixture()
	m.Cases = m.Cases[:1]
	m.Cases[0].Path = wav
	m.Cases[0].Samples = len(pcm) / 2
	m.Cases[0].SHA = hex.EncodeToString(h[:])
	m.Cases[0].Split = "evaluation"
	mb, _ := json.Marshal(m)
	p := filepath.Join(t.TempDir(), "quality.json")
	os.WriteFile(p, mb, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "quality-audit", "--model", model, "--sha256", sileroSHA, "--manifest", p)
	out, e := cmd.Output()
	if e != nil {
		t.Fatal("native audit", e)
	}
	ap := filepath.Join(t.TempDir(), "audit.json")
	os.WriteFile(ap, out, 0600)
	hash := sha256.Sum256(out)
	a, e := loadBoundAudit(ap, hex.EncodeToString(hash[:]))
	if e != nil || len(a.Cases) != 1 || a.Cases[0].Samples != len(pcm)/2 {
		t.Fatal(a, e)
	}
}
