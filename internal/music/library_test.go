package music

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLibraryLifecycle(t *testing.T) {
	for _, format := range []string{"mp3", "flac"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture."+format)
			if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2", "-y", path).CombinedOutput(); e != nil {
				t.Fatalf("fixture: %v %s", e, b)
			}
			data, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			root := t.TempDir()
			base, e := NewLibrary(root, "family-a")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = base.List(); !errors.Is(e, ErrLibraryForbidden) {
				t.Fatal(e)
			}
			var allowed atomic.Bool
			allowed.Store(true)
			l := base.WithAccessCheck(allowed.Load)
			track, e := l.Import(context.Background(), "歌曲", "歌手", format, bytes.NewReader(data))
			if e != nil {
				t.Fatal(e)
			}
			if !IsPrivateTrack(track.track()) {
				t.Fatal(track)
			}
			reopened, _ := NewLibrary(root, "family-a")
			reopened = reopened.WithAccessCheck(allowed.Load)
			list, e := reopened.List()
			if e != nil || len(list) != 1 {
				t.Fatalf("restart %v %v", list, e)
			}
			other, _ := NewLibrary(root, "family-b")
			other = other.WithAccessCheck(func() bool { return true })
			if e = other.Delete(track.ID); e == nil {
				t.Fatal("cross-family delete")
			}
			c := New().WithLibrary(l, allowed.Load)
			ts, e := c.Search(context.Background(), Query{Title: "歌曲", Artist: "歌手"})
			if e != nil || len(ts) != 1 {
				t.Fatalf("search %v %v", ts, e)
			}
			if !c.CanAccessTrack(ts[0]) {
				t.Fatal("private facts unavailable")
			}
			allowed.Store(false)
			if c.CanAccessTrack(ts[0]) {
				t.Fatal("private facts leaked")
			}
			allowed.Store(true)
			r, e := c.Resolve(context.Background(), ts[0])
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			n, e := c.Stream(ctx, r, func([]byte) error { return nil })
			if e != nil || n < 24000 {
				t.Fatalf("stream %d %v", n, e)
			}
			if _, e = New().WithLibrary(other, func() bool { return true }).Stream(ctx, r, func([]byte) error { return nil }); !errors.Is(e, ErrInvalid) {
				t.Fatal("capability crossed library", e)
			}
			_, e = c.Stream(ctx, r, func([]byte) error { allowed.Store(false); return nil })
			if !errors.Is(e, ErrLibraryForbidden) {
				t.Fatal("revocation", e)
			}
			allowed.Store(true)
			if e = l.Delete(track.ID); e != nil {
				t.Fatal(e)
			}
			list, e = l.List()
			if e != nil || len(list) != 0 {
				t.Fatal(list, e)
			}
			if _, e = l.Import(context.Background(), "x", "y", format, bytes.NewBufferString("corrupt")); e == nil {
				t.Fatal("corrupt accepted")
			}
			list, e = l.List()
			if e != nil || len(list) != 0 {
				t.Fatal("partial upload", list, e)
			}
		})
	}
}
func TestLibrarySymlinkAndCancellation(t *testing.T) {
	root := t.TempDir()
	l, e := NewLibrary(root, "a")
	if e != nil {
		t.Fatal(e)
	}
	l = l.WithAccessCheck(func() bool { return true })
	if e = os.Symlink(t.TempDir(), filepath.Join(root, "a", "4503599627370496")); e != nil {
		t.Fatal(e)
	}
	if _, e = l.List(); !errors.Is(e, ErrSource) {
		t.Fatal(e)
	}
	if _, e = NewLibrary(root, "../bad"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	os.Remove(filepath.Join(root, "a", "4503599627370496"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = l.Import(ctx, "x", "y", "mp3", bytes.NewReader([]byte("abc"))); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestMatchedResultsAmbiguous(t *testing.T) {
	if !errors.Is(matchedResults(Query{Title: "歌曲"}, []Track{{Artist: "甲"}, {Artist: "乙"}}), ErrAmbiguous) {
		t.Fatal("ambiguity accepted")
	}
	if matchedResults(Query{Title: "歌曲", Artist: "甲"}, []Track{{Artist: "甲"}, {Artist: "乙"}}) != nil {
		t.Fatal("explicit artist")
	}
}

func TestLibraryGlobalQuotaAndReaderFailure(t *testing.T) {
	root := t.TempDir()
	l, _ := NewLibrary(root, "a")
	l = l.WithAccessCheck(func() bool { return true })
	other, _ := NewLibrary(root, "b")
	// Sparse staging data in another family counts toward the root budget.
	if e := os.Mkdir(filepath.Join(root, "b", ".stage-existing"), 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(root, "b", ".stage-existing", "audio"), os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.Truncate(MaxLibraryBytes)
	f.Close()
	if _, e = l.Import(context.Background(), "x", "y", "mp3", bytes.NewReader([]byte("x"))); !errors.Is(e, ErrLibraryFull) {
		t.Fatal(e)
	}
	list, e := other.WithAccessCheck(func() bool { return true }).List()
	if e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
	os.RemoveAll(filepath.Join(root, "b", ".stage-existing"))
	if _, e = l.Import(context.Background(), "x", "y", "mp3", failingReader{}); e == nil {
		t.Fatal("reader error accepted")
	}
	entries, _ := os.ReadDir(filepath.Join(root, "a"))
	if len(entries) != 0 {
		t.Fatal("orphan stage")
	}
}

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) { return 0, errors.New("multipart rejected") }

func TestLibraryConcurrentWritersAndCountLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.mp3")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=1.2", "-y", path).CombinedOutput(); e != nil {
		t.Fatalf("fixture %v %s", e, b)
	}
	data, _ := os.ReadFile(path)
	root := t.TempDir()
	a, _ := NewLibrary(root, "a")
	b, _ := NewLibrary(root, "b")
	a = a.WithAccessCheck(func() bool { return true })
	b = b.WithAccessCheck(func() bool { return true })
	result := make(chan error, 2)
	for _, l := range []*Library{a, b} {
		go func(l *Library) {
			_, e := l.Import(context.Background(), "x", "y", "mp3", bytes.NewReader(data))
			result <- e
		}(l)
	}
	for range 2 {
		if e := <-result; e != nil {
			t.Fatal(e)
		}
	}
	// Count cap is global even when all tracks belong to a different family.
	for i := 0; i < 98; i++ {
		if e := os.Mkdir(filepath.Join(root, "b", fmt.Sprint(".stage-", i)), 0700); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := a.Import(context.Background(), "x", "y", "mp3", bytes.NewReader(data)); !errors.Is(e, ErrLibraryFull) {
		t.Fatal(e)
	}
}
