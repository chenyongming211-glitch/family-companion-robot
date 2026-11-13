package music

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func IsPrivateTrack(t Track) bool { return t.ID >= privateIDMin && t.ID < privateIDMax }
func (c *Catalog) WithLibrary(l *Library, allowed func() bool) *Catalog {
	v := *c
	v.library = l
	v.allowed = allowed
	v.privateFacts = &privateFacts{tracks: make(map[int64]Track)}
	return &v
}
func (c *Catalog) access() error {
	if c.library != nil && (c.allowed == nil || !c.allowed() || c.library.check() != nil) {
		return ErrLibraryForbidden
	}
	return nil
}
func (c *Catalog) scopedLibrary() *Library {
	return c.library.WithAccessCheck(func() bool { return c.access() == nil && c.library.check() == nil })
}
func (c *Catalog) searchLibrary(ctx context.Context, q Query) ([]Track, error) {
	if e := c.access(); e != nil {
		return nil, e
	}
	ts, e := c.scopedLibrary().list(ctx)
	if e != nil {
		return nil, e
	}
	out := []Track{}
	for _, t := range ts {
		if matchesTitle(t.Title, q.Title) && matchesArtist(t.Artist, q.Artist) {
			out = append(out, t.track())
		}
	}
	if e = c.access(); e != nil {
		return nil, e
	}
	if e := matchedResults(q, out); e != nil {
		return nil, e
	}
	if ctx.Err() != nil {
		return nil, sourceError(ctx)
	}
	return out, nil
}
func (c *Catalog) resolveLibrary(ctx context.Context, t Track) (Resolved, error) {
	if c.library == nil {
		return Resolved{}, ErrInvalid
	}
	l := c.scopedLibrary()
	r, done, e := l.locked(ctx)
	if e != nil {
		return Resolved{}, e
	}
	defer done()
	v, e := l.read(r, t.ID)
	if e != nil {
		return Resolved{}, e
	}
	if t != v.track() {
		return Resolved{}, ErrInvalid
	}
	if e = c.access(); e != nil {
		return Resolved{}, e
	}
	if ctx.Err() != nil {
		return Resolved{}, sourceError(ctx)
	}
	c.rememberPrivate(t)
	return Resolved{Track: t, track: t, size: v.Bytes, library: c.library, format: v.Format}, nil
}
func (c *Catalog) streamLibrary(ctx context.Context, v Resolved, emit func([]byte) error) (int64, error) {
	if c.library == nil || v.library != c.library || v.Track != v.track || emit == nil {
		return 0, ErrInvalid
	}
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(time.Now().Add(v.Track.Duration+30*time.Second)) {
		return 0, ErrInvalid
	}
	l := c.scopedLibrary()
	r, done, e := l.locked(ctx)
	if e != nil {
		return 0, e
	}
	t, e := l.read(r, v.Track.ID)
	if e != nil {
		done()
		return 0, e
	}
	if t.track() != v.track || t.Bytes != v.size || t.Format != v.format {
		done()
		return 0, ErrInvalid
	}
	f, e := r.OpenFile(entryName(l.family, t.ID)+"/audio", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	done()
	if e != nil {
		return 0, ErrSource
	}
	defer f.Close()
	run, cancel := context.WithTimeout(ctx, v.Track.Duration+30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(run, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "pipe", "-f", t.Format, "-i", "pipe:0", "-vn", "-ac", "1", "-ar", "24000", "-af", "volume=-6dB", "-f", "s16le", "pipe:1")
	return c.decode(run, ctx, cancel, v, f, cmd, emit)
}

var ErrAmbiguous = errors.New("music_ambiguous")

func matchedResults(q Query, tracks []Track) error {
	if q.Title != "" && q.Artist == "" && len(tracks) > 1 {
		first := normalizeName(tracks[0].Artist)
		for _, t := range tracks[1:] {
			if normalizeName(t.Artist) != first {
				return ErrAmbiguous
			}
		}
	}
	return nil
}

// CanAccessTrack checks only live scoped authorization for cached facts. It never
// waits on storage; deletion is revalidated by Resolve/Stream, outside Session.mu.
func (c *Catalog) CanAccessTrack(t Track) bool {
	if !IsPrivateTrack(t) {
		return true
	}
	if c.library == nil || c.access() != nil {
		return false
	}
	if !validTrack(t) || c.privateFacts == nil {
		return false
	}
	c.privateFacts.mu.Lock()
	defer c.privateFacts.mu.Unlock()
	return c.privateFacts.tracks[t.ID] == t
}

type privateFacts struct {
	mu     sync.Mutex
	tracks map[int64]Track
}

func (c *Catalog) rememberPrivate(t Track) {
	if c.privateFacts == nil {
		return
	}
	c.privateFacts.mu.Lock()
	defer c.privateFacts.mu.Unlock()
	if len(c.privateFacts.tracks) >= MaxLibraryTracks {
		clear(c.privateFacts.tracks)
	}
	c.privateFacts.tracks[t.ID] = t
}
