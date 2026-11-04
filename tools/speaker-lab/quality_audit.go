//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux/v2"
)

const studyMaxBytes = 2 * 1024 * 1024
const sileroSHA = "9e2449e1087496d8d4caba907f23e0bd3f78d91fa552479bb9c23ac09cbb1fd6"

type vadConfiguration struct {
	Provider   string  `json:"provider"`
	Threads    int     `json:"threads"`
	SampleRate int     `json:"sample_rate"`
	Threshold  float64 `json:"threshold"`
	WindowSize int     `json:"window_size"`
	MinSilence float64 `json:"min_silence_seconds"`
	MinSpeech  float64 `json:"min_speech_seconds"`
	MaxSpeech  float64 `json:"max_speech_seconds"`
	Buffer     float64 `json:"buffer_seconds"`
}

func fixedVADConfig() vadConfiguration {
	return vadConfiguration{"cpu", 1, 16000, .5, 512, .1, .25, 8, 10}
}

type auditCase struct {
	ID             string  `json:"id"`
	SHA            string  `json:"sha256"`
	Samples        int     `json:"samples"`
	SpeechSamples  int     `json:"speech_samples"`
	RMS            float64 `json:"rms"`
	ClippedSamples int     `json:"clipped_samples"`
}
type qualityAudit struct {
	SourceKind           string           `json:"source_kind"`
	ModelSHA             string           `json:"model_sha256"`
	ManifestSHA          string           `json:"manifest_sha256"`
	Config               vadConfiguration `json:"config"`
	Cases                []auditCase      `json:"cases"`
	HumanAcceptance      bool             `json:"human_acceptance"`
	ProductionAcceptance bool             `json:"production_acceptance"`
	FeatureEnabled       bool             `json:"feature_enabled"`
}

func newQualityAudit(sha, digest string) qualityAudit {
	return qualityAudit{SourceKind: "public_human", ModelSHA: sha, ManifestSHA: digest, Config: fixedVADConfig()}
}
func auditSpeaker(s string) bool {
	for i := 1; i <= 80; i++ {
		if s == fmt.Sprintf("fixture_%d", i) {
			return true
		}
	}
	return false
}
func loadQualityManifest(path string) (evaluationManifest, string, error) {
	m, d, e := loadEvaluationManifestWithLimits(path, evaluationLimits{studyMaxBytes, 1, 2048, 1, 80, [3]int{0, 0, 1}, auditSpeaker})
	if e == nil {
		for _, c := range m.Cases {
			if c.Split != "evaluation" {
				return evaluationManifest{}, "", errProtocol
			}
		}
	}
	return m, d, e
}

// These private range records contain offsets only. Their union avoids double
// counting SDK padding/overlap, and clips every segment to the original audio.
type speechRange struct{ start, end int64 }

func speechUnion(ranges []speechRange, samples int) int {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	var end, total int64
	for _, r := range ranges {
		a, b := max(int64(0), r.start), min(int64(samples), r.end)
		a = max(a, end)
		if b > a {
			total += b - a
			end = b
		}
	}
	return int(total)
}
func signalStats(pcm []byte) auditCase {
	r := auditCase{Samples: len(pcm) / 2}
	var sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(binary.LittleEndian.Uint16(pcm[i:]))
		f := float64(v) / 32768
		sum += f * f
		if v == 32767 || v == -32768 {
			r.ClippedSamples++
		}
	}
	if r.Samples > 0 {
		r.RMS = math.Sqrt(sum / float64(r.Samples))
	}
	return r
}

type auditDetector interface {
	AcceptWaveform([]float32)
	IsEmpty() bool
	Front() *sherpa.SpeechSegment
	Pop()
	Reset()
	Clear()
	Flush()
}

// The Go float buffer and each Front copy have a single owner and are cleared
// before return. Native SDK/OS internal copies are not claimed securely erased.
func auditPCM(ctx context.Context, pcm []byte, d auditDetector) (int, error) {
	d.Reset()
	d.Clear()
	defer d.Reset()
	defer d.Clear()
	samples := make([]float32, len(pcm)/2)
	defer clear(samples)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
	}
	var ranges []speechRange
	drain := func() {
		for !d.IsEmpty() {
			s := d.Front()
			ranges = append(ranges, speechRange{s.Start, s.Start + int64(len(s.Samples))})
			clear(s.Samples)
			d.Pop()
		}
	}
	for offset := 0; offset < len(samples); offset += 512 {
		if ctx.Err() != nil {
			return 0, errWorker
		}
		d.AcceptWaveform(samples[offset:min(offset+512, len(samples))])
		drain()
	}
	d.Flush()
	drain()
	if ctx.Err() != nil {
		return 0, errWorker
	}
	return speechUnion(ranges, len(samples)), nil
}
func runQualityAudit(ctx context.Context, model, sha, manifest string) (qualityAudit, error) {
	r := newQualityAudit(sha, "")
	if ctx.Err() != nil {
		return r, errWorker
	}
	if sha != sileroSHA {
		return r, errModel
	}
	m, digest, e := loadQualityManifest(manifest)
	if e != nil {
		return r, e
	}
	r.ManifestSHA = digest
	// No enrollment/calibration is permitted in this signal-only command.
	for _, c := range m.Cases {
		if c.Split != "evaluation" {
			return r, errProtocol
		}
	}
	f, e := openModel(model, sha)
	if e != nil {
		return r, e
	}
	defer f.Close()
	cfg := sherpa.VadModelConfig{SileroVad: sherpa.SileroVadModelConfig{Model: fmt.Sprintf("/proc/self/fd/%d", f.Fd()), Threshold: .5, MinSilenceDuration: .1, MinSpeechDuration: .25, WindowSize: 512, MaxSpeechDuration: 8}, SampleRate: 16000, NumThreads: 1, Provider: "cpu"}
	vad := sherpa.NewVoiceActivityDetector(&cfg, 10)
	if vad == nil {
		return r, errModel
	}
	defer sherpa.DeleteVoiceActivityDetector(vad)
	for _, c := range m.Cases {
		if ctx.Err() != nil {
			return r, errWorker
		}
		pcm, e := loadFixture(c.fixture())
		if e != nil {
			return r, e
		}
		item := signalStats(pcm)
		item.ID, item.SHA = c.ID, c.SHA
		item.SpeechSamples, e = auditPCM(ctx, pcm, vad)
		clear(pcm)
		if e != nil {
			return r, e
		}
		r.Cases = append(r.Cases, item)
	}
	return r, nil
}
