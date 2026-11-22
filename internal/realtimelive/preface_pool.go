package realtimelive

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
)

// A slow turn leaves the user listening to nothing for two to nine seconds
// (measured 2026-11-22: 2.7s on a memory turn, 6.1s with search, 8.1s on one
// stalled answer). The previous attempt filled that gap with a model-generated
// line, which costs about 0.6s to produce its first packet and therefore cannot
// speak before the 2.2s mark. A pre-synthesized pool has no generation step at
// all, so it can answer at 0.5s -- the range a person answers in.
//
// The pool is audio, so the one mistake that ruins it is a different speaker.
// Everything below is a gate against that, not a preference:
//
//   - the manifest names the voice it was synthesized with, and a pool recorded
//     for another voice is refused outright
//   - the sample rate, channel count and sample format must be the ones the
//     playback path carries (24000, mono, s16le). Resampling on the fly is the
//     other way to make one voice sound like two: a rate mismatch shifts pitch
//   - every clip must be a whole number of playback packets, so a short phrase
//     never ends in zero padding the answer would have to wait behind
//
// The synthesis side lives in tools/synth_preface_pool.py, which uses the same
// realtime endpoint and the same voice as production on purpose.

const (
	prefacePoolRate     = 24000
	prefacePoolChannels = 1
	prefacePoolPacket   = 2880 // 60ms s16le mono at 24k, the playback packet size
	// How many clips back the chooser refuses to repeat. Six out of fifteen is
	// enough that a listener does not hear a rotation, and short enough that a
	// tier with five lines still has room to choose.
	prefaceRecentKeep = 6
)

// prefaceLine is one clip in the pool.
type prefaceLine struct {
	ID    string `json:"id"`
	Tier  int    `json:"tier"`
	Text  string `json:"text"`
	MS    int    `json:"ms"`
	Bytes int    `json:"bytes"`
	SHA   string `json:"sha256"`
	pcm   []byte
}

type prefaceManifest struct {
	Voice         string        `json:"voice"`
	Model         string        `json:"model"`
	SampleRate    int           `json:"sample_rate"`
	SampleFormat  string        `json:"sample_format"`
	Channels      int           `json:"channels"`
	PacketBytes   int           `json:"packet_bytes"`
	SeamSilenceMS int           `json:"seam_silence_ms"`
	Lines         []prefaceLine `json:"lines"`
}

type PrefacePool struct {
	voice string
	lines []prefaceLine

	mu     sync.Mutex
	recent []string
	rnd    *rand.Rand
}

// LoadPrefacePool reads dir/manifest.json and the clips it names. voice is the
// voice this process will speak in: a pool recorded for someone else is refused
// rather than played, because a wrong voice is worse than a missing fill.
func LoadPrefacePool(dir, voice string) (*PrefacePool, error) {
	if dir == "" {
		return nil, errors.New("preface_pool_no_dir")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("preface_pool_manifest: %w", err)
	}
	var m prefaceManifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("preface_pool_manifest: %w", err)
	}
	if m.Voice == "" {
		return nil, errors.New("preface_pool_voice_unnamed")
	}
	if m.Voice != voice {
		return nil, fmt.Errorf("preface_pool_voice_mismatch: pool=%s live=%s", m.Voice, voice)
	}
	if m.SampleRate != prefacePoolRate || m.Channels != prefacePoolChannels || m.SampleFormat != "s16le" {
		return nil, fmt.Errorf("preface_pool_format: %d/%d/%s", m.SampleRate, m.Channels, m.SampleFormat)
	}
	if m.PacketBytes != prefacePoolPacket {
		return nil, fmt.Errorf("preface_pool_packet: %d", m.PacketBytes)
	}
	if len(m.Lines) == 0 {
		return nil, errors.New("preface_pool_empty")
	}
	p := &PrefacePool{voice: m.Voice, rnd: rand.New(rand.NewSource(rand.Int63()))}
	for _, line := range m.Lines {
		if line.Tier < 1 || line.Tier > 3 {
			return nil, fmt.Errorf("preface_pool_tier: %s=%d", line.ID, line.Tier)
		}
		pcm, e := os.ReadFile(filepath.Join(dir, line.ID+".pcm"))
		if e != nil {
			return nil, fmt.Errorf("preface_pool_clip %s: %w", line.ID, e)
		}
		if len(pcm) != line.Bytes || len(pcm) == 0 || len(pcm)%prefacePoolPacket != 0 {
			return nil, fmt.Errorf("preface_pool_clip_size: %s have=%d want=%d", line.ID, len(pcm), line.Bytes)
		}
		line.pcm = pcm
		p.lines = append(p.lines, line)
	}
	return p, nil
}

// Lines reports how many clips were loaded, for the startup log.
func (p *PrefacePool) Lines() int {
	if p == nil {
		return 0
	}
	return len(p.lines)
}

// Pick chooses a clip of the given tier, avoiding the last few it returned.
// A tier that has been exhausted forgets its history rather than refusing to
// speak: repetition is a smaller failure than silence.
func (p *PrefacePool) Pick(tier int) *prefaceLine {
	if p == nil || len(p.lines) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pick := func(skipRecent bool) *prefaceLine {
		var candidates []prefaceLine
		for _, line := range p.lines {
			if line.Tier != tier {
				continue
			}
			if skipRecent && containsString(p.recent, line.ID) {
				continue
			}
			candidates = append(candidates, line)
		}
		if len(candidates) == 0 {
			return nil
		}
		line := candidates[p.rnd.Intn(len(candidates))]
		return &line
	}
	line := pick(true)
	if line == nil {
		line = pick(false)
	}
	if line == nil {
		return nil
	}
	p.recent = append(p.recent, line.ID)
	if len(p.recent) > prefaceRecentKeep {
		p.recent = p.recent[len(p.recent)-prefaceRecentKeep:]
	}
	return line
}

// Tier returns how long an acknowledgement may be at this point in the wait.
// The answer may take the floor at any seam, so a clip that is too long is a
// delay in front of the answer, not a filler behind it. That only becomes
// affordable once the answer is already known to be far away.
func prefaceTier(elapsedMS int64) int {
	switch {
	case elapsedMS < 1200:
		return 1
	case elapsedMS < 2600:
		return 2
	default:
		return 3
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
