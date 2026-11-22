package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// The pool is generated at runtime, in the voice the process is configured to
// speak in, using the same realtime client the answer uses.
//
// That is the whole point of doing it here instead of shipping a fixed set of
// files: the voice is configuration (QWEN_VOICE, and eventually a per-agent
// choice), so a pool baked for one voice becomes the wrong speaker the moment
// anyone changes it -- robot answers in one voice, clears its throat in another.
// Synthesizing through NewNative means the clips are produced by exactly the
// same session.update -- same endpoint, same voice field, same 24k s16le output
// -- that will speak the answer. A TTS endpoint is a different speaker and is
// deliberately not used.
const (
	// Read the line, do not respond to it. The realtime model is conversational
	// and will otherwise answer "我想想啊" instead of saying it.
	prefaceRepeater = "你是一台复读机。用户给你一句话，你只用同样的语气把这句话原样念一遍，" +
		"一个字都不加，一个字都不减，不要回应它的内容，不要解释，不要加语气词。"
	prefaceSynthAttempts = 3
	prefaceSynthTimeout  = 25 * time.Second
)

// The level is matched to how the answer speaks, measured on the same voice:
// the answer peaks near -10 dBFS with voiced RMS near -21 dBFS. Normalising a
// clip to full scale instead puts it several dB above the answer -- a hum with
// no pauses has no quiet frames to pull its mean down -- and the gap reads as a
// second, louder person leaning in.
const (
	prefaceTargetVoicedRMS = -19.0
	prefaceMaxPeak         = -9.0
	prefaceSilenceFloor    = -42.0
	prefaceEdgePadMS       = 12
	prefaceFadeMS          = 6
)

// SynthPrefacePool generates a pool for cfg.Voice into dir and returns it.
// dir is expected to be dedicated to one voice.
func SynthPrefacePool(ctx context.Context, cfg Config, dir string, lines []prefaceLineSpec) (*PrefacePool, error) {
	if cfg.URL == "" || cfg.Key == "" || cfg.Voice == "" {
		return nil, errors.New("preface_synth_config_incomplete")
	}
	if len(lines) == 0 {
		return nil, errors.New("preface_synth_no_lines")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("preface_synth_dir: %w", err)
	}
	// Synthesis must not read or write production turn records: these are not
	// conversations. Synthetic labels the audit entry and MemoryOnly keeps it
	// out of the filesystem entirely -- Synthetic alone still writes the intent
	// file, which would litter the working directory of a running service.
	base := Config{URL: cfg.URL, Key: cfg.Key, Voice: cfg.Voice, Model: cfg.Model, Synthetic: true, MemoryOnly: true}

	m := prefaceManifest{
		Voice: cfg.Voice, Model: cfg.Model,
		SampleRate: prefacePoolRate, SampleFormat: "s16le", Channels: prefacePoolChannels,
		PacketBytes: prefacePoolPacket, SeamSilenceMS: prefaceSeamSilenceMS,
	}
	var rejected []string
	for _, spec := range lines {
		pcm, said, err := synthLine(ctx, base, spec.Text)
		if err != nil {
			return nil, fmt.Errorf("preface_synth %s: %w", spec.ID, err)
		}
		clip := preparePrefaceClip(pcm)
		if len(clip) == 0 {
			rejected = append(rejected, spec.ID+":empty")
			continue
		}
		if err = os.WriteFile(filepath.Join(dir, spec.ID+".pcm"), clip, 0o600); err != nil {
			return nil, fmt.Errorf("preface_synth write %s: %w", spec.ID, err)
		}
		m.Lines = append(m.Lines, prefaceLine{
			ID: spec.ID, Tier: spec.Tier, Text: spec.Text,
			MS: len(clip) * 1000 / (prefacePoolRate * 2), Bytes: len(clip),
		})
		_ = said
	}
	if len(m.Lines) == 0 {
		return nil, errors.New("preface_synth_empty")
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), append(raw, '\n'), 0o600); err != nil {
		return nil, fmt.Errorf("preface_synth manifest: %w", err)
	}
	return LoadPrefacePool(dir, cfg.Voice)
}

// synthLine reads one line out loud and returns its PCM.
func synthLine(ctx context.Context, cfg Config, text string) ([]byte, string, error) {
	var lastErr error
	for attempt := 0; attempt < prefaceSynthAttempts; attempt++ {
		pcm, said, err := synthOnce(ctx, cfg, text)
		if err != nil {
			lastErr = err
			continue
		}
		if prefaceSaidMatches(text, said) {
			return pcm, said, nil
		}
		// A clip that says something else is worse than no clip: it would put
		// words in the robot's mouth that nobody wrote.
		lastErr = fmt.Errorf("preface_synth drifted: want %q got %q", text, said)
	}
	return nil, "", lastErr
}

func synthOnce(ctx context.Context, cfg Config, text string) ([]byte, string, error) {
	callCtx, cancel := context.WithTimeout(ctx, prefaceSynthTimeout)
	defer cancel()
	n := NewNative(cfg, "")
	if err := n.Open(callCtx, prefaceRepeater, false); err != nil {
		return nil, "", err
	}
	defer n.Close()
	var pcm []byte
	said := ""
	err := n.StreamText(callCtx, "念："+text, func(kind string, value []byte) error {
		switch kind {
		case "pcm":
			pcm = append(pcm, value...)
		case "text":
			said = string(value)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if len(pcm) == 0 {
		return nil, said, errors.New("preface_synth_no_audio")
	}
	return pcm, said, nil
}

var prefacePunctuation = regexp.MustCompile(`[\s，。、？！…,.?!~\-—]`)

func prefaceSaidMatches(want, got string) bool {
	return prefacePunctuation.ReplaceAllString(want, "") == prefacePunctuation.ReplaceAllString(got, "")
}

// preparePrefaceClip turns raw model output into a clip that can be dropped into
// the answer's stream: silence trimmed, level matched, seams faded, and a breath
// of silence at the tail so two clips do not run together into one odd sentence.
func preparePrefaceClip(pcm []byte) []byte {
	samples := pcmToFloat(pcm)
	if len(samples) == 0 {
		return nil
	}
	floor := 32768.0 * math.Pow(10, prefaceSilenceFloor/20)
	first, last := -1, -1
	for i, v := range samples {
		if math.Abs(float64(v)) > floor {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return nil
	}
	pad := prefacePoolRate * prefaceEdgePadMS / 1000
	start := max(0, first-pad)
	end := min(len(samples), last+pad)
	samples = samples[start:end]

	level := voicedRMS(samples)
	if level <= 0 {
		return nil
	}
	scale := (32768.0 * math.Pow(10, prefaceTargetVoicedRMS/20)) / level
	for i := range samples {
		samples[i] *= float32(scale)
	}
	// Levelling by RMS can push a peak past the answer's; cap rather than clip.
	peak := float32(0)
	for _, v := range samples {
		if a := float32(math.Abs(float64(v))); a > peak {
			peak = a
		}
	}
	ceil := float32(32768.0 * math.Pow(10, prefaceMaxPeak/20))
	if peak > ceil {
		k := ceil / peak
		for i := range samples {
			samples[i] *= k
		}
	}

	fade := prefacePoolRate * prefaceFadeMS / 1000
	if len(samples) > 2*fade && fade > 0 {
		for i := 0; i < fade; i++ {
			r := float32(i) / float32(fade)
			samples[i] *= r
			samples[len(samples)-1-i] *= r
		}
	}
	seam := prefacePoolRate * prefaceSeamSilenceMS / 1000
	out := make([]byte, (len(samples)+seam)*2)
	for i, v := range samples {
		putInt16(out[i*2:], int32(clampInt16(v)))
	}
	// The tail is already zero-filled by make.
	if len(out)%prefacePoolPacket != 0 {
		out = append(out, make([]byte, prefacePoolPacket-len(out)%prefacePoolPacket)...)
	}
	return out
}

func pcmToFloat(pcm []byte) []float32 {
	n := len(pcm) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(int16(uint16(pcm[i*2]) | uint16(pcm[i*2+1])<<8))
	}
	return out
}

func putInt16(b []byte, v int32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func clampInt16(v float32) int32 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int32(math.Round(float64(v)))
}

// voicedRMS measures loudness over speech frames only, the way the answer was
// measured. Averaging the whole clip would include the lead-in and the tail and
// report a quiet clip, which is how level matching silently goes wrong.
func voicedRMS(samples []float32) float64 {
	const (
		window = 600 // 25ms at 24k
		hop    = 240 // 10ms
	)
	if len(samples) < window {
		return 0
	}
	var frames []float64
	maxRMS := 0.0
	for i := 0; i+window <= len(samples); i += hop {
		sum := 0.0
		for _, v := range samples[i : i+window] {
			sum += float64(v) * float64(v)
		}
		r := math.Sqrt(sum / window)
		frames = append(frames, r)
		if r > maxRMS {
			maxRMS = r
		}
	}
	if maxRMS <= 0 {
		return 0
	}
	threshold := 0.15 * maxRMS
	var kept []float64
	for _, r := range frames {
		if r > threshold {
			kept = append(kept, r*r)
		}
	}
	if len(kept) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range kept {
		sum += v
	}
	return math.Sqrt(sum / float64(len(kept)))
}

// prefaceSeamSilenceMS is the breath between clips, written into the audio so the
// caller only has to play clips in order.
const prefaceSeamSilenceMS = 250
