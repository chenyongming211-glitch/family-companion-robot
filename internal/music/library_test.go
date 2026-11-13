package music

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
			if _, e = c.Resolve(context.Background(), ts[0]); e != nil {
				t.Fatal(e)
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

func TestLibraryAcceptedMetadataRoundtrip(t *testing.T) {
	data := libraryFixture(t, "mp3")
	root := t.TempDir()
	l, _ := NewLibrary(root, "a")
	l = l.WithAccessCheck(func() bool { return true })
	for _, text := range []string{"<", "𠮷"} {
		title := strings.Repeat(text, 500)
		artist := strings.Repeat(text, 2000)
		v, e := l.Import(context.Background(), title, artist, "mp3", bytes.NewReader(data))
		if e != nil {
			t.Fatal(e)
		}
		reopened, _ := NewLibrary(root, "a")
		reopened = reopened.WithAccessCheck(func() bool { return true })
		rows, e := reopened.List()
		if e != nil || len(rows) != 1 || rows[0].Title != title || rows[0].Artist != artist {
			t.Fatalf("roundtrip: %v rows=%d", e, len(rows))
		}
		if _, e = New().WithLibrary(reopened, func() bool { return true }).Resolve(context.Background(), v.track()); e != nil {
			t.Fatal(e)
		}
		if e = reopened.Delete(v.ID); e != nil {
			t.Fatal(e)
		}
	}
}
func libraryFixture(t *testing.T, format string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture."+format)
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=1.2", "-y", path).CombinedOutput(); e != nil {
		t.Fatalf("fixture %v %s", e, b)
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestLibraryRejectsFalseFLACDuration(t *testing.T) {
	data := libraryFixture(t, "flac")
	if string(data[:4]) != "fLaC" {
		t.Fatal("not flac")
	} // STREAMINFO packed 36-bit total_samples starts at byte 21 low nibble.
	total := uint64(data[21]&15)<<32 | uint64(binary.BigEndian.Uint32(data[22:26]))
	total *= 2
	data[21] = (data[21] & 240) | byte(total>>32)
	binary.BigEndian.PutUint32(data[22:26], uint32(total))
	l, _ := NewLibrary(t.TempDir(), "a")
	l = l.WithAccessCheck(func() bool { return true })
	if _, e := l.Import(context.Background(), "x", "y", "flac", bytes.NewReader(data)); e == nil {
		t.Fatal("false decoded duration committed")
	}
	rows, e := l.List()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
func TestLibraryCatalogCancellationAndNonblockingFacts(t *testing.T) {
	l, _ := NewLibrary(t.TempDir(), "a")
	l = l.WithAccessCheck(func() bool { return true })
	v, e := l.Import(context.Background(), "x", "y", "mp3", bytes.NewReader(libraryFixture(t, "mp3")))
	if e != nil {
		t.Fatal(e)
	}
	c := New().WithLibrary(l, func() bool { return true })
	if _, e = c.Resolve(context.Background(), v.track()); e != nil {
		t.Fatal(e)
	}
	if New().WithLibrary(l, func() bool { return true }).CanAccessTrack(v.track()) {
		t.Fatal("facts crossed catalog scope")
	}
	for _, operation := range []string{"search", "resolve", "facts"} {
		t.Run(operation, func(t *testing.T) {
			reader := &blockedImportReader{entered: make(chan struct{}), release: make(chan struct{})}
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, _ = l.Import(context.Background(), "pending", "upload", "mp3", reader)
			}()
			<-reader.entered
			go func() { time.Sleep(150 * time.Millisecond); close(reader.release) }()
			defer func() { <-finished }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			start := time.Now()
			if operation == "facts" {
				if !c.CanAccessTrack(v.track()) {
					t.Fatal("valid scope denied")
				}
				if time.Since(start) > 75*time.Millisecond {
					t.Fatal("facts blocked on import lock")
				}
				return
			}
			if operation == "search" {
				_, e = c.Search(ctx, Query{Title: "x", Artist: "y"})
			} else {
				_, e = c.Resolve(ctx, v.track())
			}
			if !errors.Is(e, context.DeadlineExceeded) {
				t.Fatalf("caller deadline discarded: %v", e)
			}
			if time.Since(start) > 100*time.Millisecond {
				t.Fatal("cancellation delayed")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = c.Search(ctx, Query{Title: "x"}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e = c.Resolve(ctx, v.track()); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestLibraryAttachedCoverAndVideoRejection(t *testing.T) {
	dir := t.TempDir()
	cover := filepath.Join(dir, "cover.ppm")
	pixels := bytes.Repeat([]byte{255, 0, 0}, 16*16)
	if e := os.WriteFile(cover, append([]byte("P6\n16 16\n255\n"), pixels...), 0600); e != nil {
		t.Fatal(e)
	}
	output := filepath.Join(dir, "covered.mp3")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=1.2", "-i", cover, "-map", "0:a", "-map", "1:v", "-c:a", "libmp3lame", "-c:v", "mjpeg", "-disposition:v", "attached_pic", "-y", output).CombinedOutput(); e != nil {
		t.Fatalf("cover fixture %v %s", e, b)
	}
	data, _ := os.ReadFile(output)
	l, _ := NewLibrary(t.TempDir(), "a")
	l = l.WithAccessCheck(func() bool { return true })
	v, e := l.Import(context.Background(), "x", "y", "mp3", bytes.NewReader(data))
	if e != nil {
		t.Fatal("embedded cover rejected", e)
	}
	r, e := New().WithLibrary(l, func() bool { return true }).Resolve(context.Background(), v.track())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e = New().WithLibrary(l, func() bool { return true }).Stream(ctx, r, func([]byte) error { return nil }); e != nil {
		t.Fatal(e)
	}
	video := filepath.Join(dir, "video.mkv")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=1.2", "-f", "lavfi", "-i", "color=size=16x16:duration=1.2", "-c:a", "flac", "-c:v", "ffv1", "-y", video).CombinedOutput(); e != nil {
		t.Fatalf("video fixture %v %s", e, b)
	}
	data, _ = os.ReadFile(video)
	if _, e = l.Import(context.Background(), "x", "y", "flac", bytes.NewReader(data)); e == nil {
		t.Fatal("video container accepted")
	}
}

type blockedImportReader struct{ entered, release chan struct{} }

func (r *blockedImportReader) Read([]byte) (int, error) {
	close(r.entered)
	<-r.release
	return 0, io.EOF
}
