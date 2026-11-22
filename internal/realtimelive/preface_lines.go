package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// What the acknowledgement says is product content, not code. The defaults
// below are the wording this version ships with; a JSON file replaces them
// wholesale so the lines can be re-tuned without a build.
//
// Tier is the only structural requirement: it says how long a clip may be at
// that point in the wait. Tier 1 speaks at 0.5s and must clear the floor fast,
// because an answer arriving behind it waits for it. Tier 3 is only reached once
// the answer is already known to be seconds away.
type prefaceLineSpec struct {
	ID   string `json:"id"`
	Tier int    `json:"tier"`
	Text string `json:"text"`
}

func defaultPrefaceLines() []prefaceLineSpec {
	return []prefaceLineSpec{
		{"a01", 1, "嗯。"},
		{"a02", 1, "哎。"},
		{"a03", 1, "嗯？"},
		{"a04", 1, "诶。"},
		{"a05", 1, "唔。"},
		{"a06", 1, "哦。"},
		{"b01", 2, "我想想啊。"},
		{"b02", 2, "嗯，我理理。"},
		{"b03", 2, "让我想想啊。"},
		{"b04", 2, "我琢磨一下。"},
		{"b05", 2, "等一下啊。"},
		{"b06", 2, "我想一下怎么说。"},
		{"c01", 3, "你先别急，我理一理啊。"},
		{"c02", 3, "这个得慢慢想，马上就好。"},
		{"c03", 3, "嗯……我想清楚了就跟你说。"},
		{"c04", 3, "这个我好好想想，一会儿告诉你。"},
	}
}

// LoadPrefaceLines returns the lines to synthesize. path empty means the shipped
// defaults; a path must parse and be structurally sound, because a malformed
// list would otherwise synthesize a pool that fails at load time for a reason
// nobody can see.
func LoadPrefaceLines(path string) ([]prefaceLineSpec, error) {
	if path == "" {
		return defaultPrefaceLines(), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("preface_lines: %w", err)
	}
	var lines []prefaceLineSpec
	if err = json.Unmarshal(raw, &lines); err != nil {
		return nil, fmt.Errorf("preface_lines: %w", err)
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if line.ID == "" || line.Text == "" {
			return nil, fmt.Errorf("preface_lines: empty id or text")
		}
		if line.Tier < 1 || line.Tier > 3 {
			return nil, fmt.Errorf("preface_lines: %s has tier %d", line.ID, line.Tier)
		}
		if seen[line.ID] {
			return nil, fmt.Errorf("preface_lines: duplicate id %s", line.ID)
		}
		seen[line.ID] = true
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("preface_lines: empty")
	}
	return lines, nil
}

// PrefaceStore hands out the pool for a voice.
//
// It is a store rather than a single pool because the voice is configuration,
// not a build-time constant: change QWEN_VOICE and the acknowledgement must
// change with it, or the robot answers in one voice and clears its throat in
// another. Each voice keeps its own directory of clips, and a voice whose clips
// do not exist yet is synthesized on the fly and swapped in -- without a restart,
// so a device already connected gets the new voice's clips on its next turn.
type PrefaceStore struct {
	mu    sync.RWMutex
	pools map[string]*PrefacePool
	// synth serialises generation per voice so two sessions that both notice a
	// missing pool do not synthesize it twice at once.
	synthMu sync.Mutex
}

func NewPrefaceStore() *PrefaceStore {
	return &PrefaceStore{pools: map[string]*PrefacePool{}}
}

// For returns the pool for voice, or nil if there is none yet.
func (s *PrefaceStore) For(voice string) *PrefacePool {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pools[voice]
}

// Put installs a pool, replacing any previous one for the same voice. The swap
// is visible to the next turn, so a voice changed at runtime starts speaking its
// own clips without a restart.
func (s *PrefaceStore) Put(pool *PrefacePool) {
	if s == nil || pool == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pools[pool.voice] = pool
}

// Ensure returns the pool for cfg.Voice, loading it from root or synthesizing it
// when no clips exist yet. root holds one directory per voice, so switching the
// voice switches the clips and changing back reuses what was already generated.
// LoadCached adopts an existing pool only if it still speaks the wording now
// configured. It is the boot path: a cached pool is a cheap load, but adopting
// it blindly means an edited list keeps playing the old recording -- valid
// manifest, all clips present, and the only symptom is a sentence nobody wrote.
//
// The bool says whether the caller may skip synthesis. A voice directory that
// exists but no longer matches must be regenerated, not trusted.
func (s *PrefaceStore) LoadCached(root string, cfg Config, lines []prefaceLineSpec) (*PrefacePool, bool) {
	if s == nil || !validVoiceDirName(cfg.Voice) {
		return nil, false
	}
	dir := filepath.Join(root, cfg.Voice)
	pool, err := LoadPrefacePool(dir, cfg.Voice)
	if err != nil || !pool.speaks(lines) {
		return nil, false
	}
	s.Put(pool)
	return pool, true
}

func (s *PrefaceStore) Ensure(ctx context.Context, cfg Config, root string, lines []prefaceLineSpec) (*PrefacePool, error) {
	if s == nil {
		return nil, errors.New("preface_store_nil")
	}
	if pool := s.For(cfg.Voice); pool != nil {
		return pool, nil
	}
	if !validVoiceDirName(cfg.Voice) {
		return nil, fmt.Errorf("preface_voice_unsafe: %q", cfg.Voice)
	}
	// One synthesis per voice at a time: the boot path and any later caller can
	// race on the same missing directory otherwise, and both would pay for it.
	s.synthMu.Lock()
	defer s.synthMu.Unlock()
	if pool := s.For(cfg.Voice); pool != nil {
		return pool, nil
	}
	dir := filepath.Join(root, cfg.Voice)
	if pool, ok := s.LoadCached(root, cfg, lines); ok {
		return pool, nil
	}
	pool, err := SynthPrefacePool(ctx, cfg, dir, lines)
	if err != nil {
		return nil, err
	}
	s.Put(pool)
	return pool, nil
}

// validVoiceDirName keeps a voice name from escaping the pool root. Providers use
// short identifiers, and anything else is not a voice this store can cache.
func validVoiceDirName(voice string) bool {
	if voice == "" || len(voice) > 64 {
		return false
	}
	for _, r := range voice {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return voice != "." && voice != ".."
}

// Voices reports which voices currently have clips, for the startup log.
func (s *PrefaceStore) Voices() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.pools))
	for voice := range s.pools {
		out = append(out, voice)
	}
	return out
}
