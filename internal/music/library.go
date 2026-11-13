package music

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ErrLibraryFull = errors.New("music_library_full")
var ErrLibraryForbidden = errors.New("music_library_forbidden")

const privateIDMin int64 = 1 << 52
const privateIDMax int64 = 1 << 53
const MaxLibraryBytes int64 = 300 << 20
const MaxLibraryTracks = 100
const MaxLibraryTrackBytes int64 = 30 << 20
const libraryMaxBytes = MaxLibraryBytes
const libraryMaxFile = MaxLibraryTrackBytes

type LibraryTrack struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Artist     string    `json:"artist"`
	DurationMs int64     `json:"durationMs"`
	Bytes      int64     `json:"bytes"`
	Format     string    `json:"format"`
	Bitrate    int64     `json:"bitrate"`
	SampleRate int       `json:"sampleRate"`
	Channels   int       `json:"channels"`
	AddedAt    time.Time `json:"addedAt"`
}

func (t LibraryTrack) track() Track {
	return Track{t.ID, t.Title, t.Artist, time.Duration(t.DurationMs) * time.Millisecond}
}

type Library struct {
	root, family string
	allowed      func() bool
}

func NewLibrary(root, family string) (*Library, error) {
	if family == "" || family == "." || family == ".." || strings.ContainsAny(family, "/\\") || !validText(family, 200) {
		return nil, ErrInvalid
	}
	abs, e := filepath.Abs(root)
	if e != nil {
		return nil, ErrInvalid
	}
	if e = os.MkdirAll(abs, 0700); e != nil {
		return nil, ErrSource
	}
	st, e := os.Lstat(abs)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSource
	}
	if e = os.Chmod(abs, 0700); e != nil {
		return nil, ErrSource
	}
	l := &Library{root: abs, family: family}
	r, e := os.OpenRoot(abs)
	if e != nil {
		return nil, ErrSource
	}
	defer r.Close()
	if e = r.Mkdir(family, 0700); e != nil && !errors.Is(e, os.ErrExist) {
		return nil, ErrSource
	}
	st, e = r.Lstat(family)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSource
	}
	if e = r.Chmod(family, 0700); e != nil {
		return nil, ErrSource
	}
	return l, nil
}
func (l *Library) WithAccessCheck(a func() bool) *Library { v := *l; v.allowed = a; return &v }
func (l *Library) check() error {
	if l == nil || l.allowed == nil || !l.allowed() {
		return ErrLibraryForbidden
	}
	return nil
}
func (l *Library) locked(ctx context.Context) (*os.Root, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, prepareTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return nil, nil, sourceError(ctx)
	}
	if e := l.check(); e != nil {
		return nil, nil, e
	}
	stRoot, rootErr := os.Lstat(l.root)
	if rootErr != nil || !stRoot.IsDir() || stRoot.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrSource
	}
	r, e := os.OpenRoot(l.root)
	if e != nil {
		return nil, nil, ErrSource
	}
	f, e := r.OpenFile(".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		r.Close()
		return nil, nil, ErrSource
	}
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
			f.Close()
			r.Close()
			return nil, nil, ErrSource
		}
		select {
		case <-ctx.Done():
			f.Close()
			r.Close()
			return nil, nil, sourceError(ctx)
		case <-time.After(20 * time.Millisecond):
		}
	}
	done := func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close(); r.Close() }
	st, e := r.Lstat(l.family)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		done()
		return nil, nil, ErrSource
	}
	if e = l.check(); e != nil {
		done()
		return nil, nil, e
	}
	return r, done, nil
}
func entryName(f string, id int64) string { return f + "/" + strconv.FormatInt(id, 10) }
func (l *Library) read(r *os.Root, id int64) (LibraryTrack, error) {
	var t LibraryTrack
	if id < privateIDMin || id >= privateIDMax {
		return t, ErrInvalid
	}
	name := entryName(l.family, id)
	for _, p := range []string{name, name + "/meta.json", name + "/audio"} {
		s, e := r.Lstat(p)
		if e != nil || s.Mode()&os.ModeSymlink != 0 || (p == name && !s.IsDir()) || (p != name && !s.Mode().IsRegular()) {
			return t, ErrSource
		}
	}
	f, e := r.OpenFile(name+"/meta.json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return t, ErrSource
	}
	defer f.Close()
	if json.NewDecoder(io.LimitReader(f, 8192)).Decode(&t) != nil || t.ID != id || !validTrack(t.track()) || t.Bytes <= 0 || t.Bytes > libraryMaxFile || (t.Format != "mp3" && t.Format != "flac") {
		return LibraryTrack{}, ErrSource
	}
	s, e := r.Stat(name + "/audio")
	if e != nil || !s.Mode().IsRegular() || s.Size() != t.Bytes {
		return LibraryTrack{}, ErrSource
	}
	return t, nil
}
func (l *Library) List() ([]LibraryTrack, error) {
	r, done, e := l.locked(context.Background())
	if e != nil {
		return nil, e
	}
	defer done()
	f, e := r.Open(l.family)
	if e != nil {
		return nil, ErrSource
	}
	defer f.Close()
	entries, e := f.ReadDir(-1)
	if e != nil {
		return nil, ErrSource
	}
	out := []LibraryTrack{}
	for _, v := range entries {
		if strings.HasPrefix(v.Name(), ".stage-") {
			continue
		}
		id, e := strconv.ParseInt(v.Name(), 10, 64)
		if e != nil {
			return nil, ErrSource
		}
		t, e := l.read(r, id)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	if e = l.check(); e != nil {
		return nil, e
	}
	return out, nil
}
func (l *Library) Delete(id int64) error {
	r, done, e := l.locked(context.Background())
	if e != nil {
		return e
	}
	defer done()
	if _, e = l.read(r, id); e != nil {
		return e
	}
	if e = l.check(); e != nil {
		return e
	}
	if e = r.RemoveAll(entryName(l.family, id)); e != nil {
		return ErrSource
	}
	return nil
}

// All writers share this lock, so staging bytes cannot race the global quota.
func usage(r *os.Root) (int64, int, error) {
	var bytes int64
	var count int
	var walk func(string) error
	walk = func(p string) error {
		f, e := r.Open(p)
		if e != nil {
			return e
		}
		es, e := f.ReadDir(-1)
		f.Close()
		if e != nil {
			return e
		}
		for _, v := range es {
			n := v.Name()
			if p == "." && n == ".lock" {
				continue
			}
			path := n
			if p != "." {
				path = p + "/" + n
			}
			s, e := r.Lstat(path)
			if e != nil {
				return e
			}
			if s.Mode()&os.ModeSymlink != 0 {
				return ErrSource
			}
			if s.IsDir() {
				if p != "." {
					count++
				}
				if e = walk(path); e != nil {
					return e
				}
			} else if s.Mode().IsRegular() {
				if n == "audio" {
					bytes += s.Size()
				}
			} else {
				return ErrSource
			}
		}
		return nil
	}
	e := walk(".")
	return bytes, count, e
}
func (l *Library) Import(ctx context.Context, title, artist, format string, input io.Reader) (LibraryTrack, error) {
	var z LibraryTrack
	title = strings.TrimSpace(title)
	artist = strings.TrimSpace(artist)
	if title == "" || artist == "" || !validText(title, 500) || !validText(artist, 2000) || (format != "mp3" && format != "flac") || input == nil {
		return z, ErrInvalid
	}
	r, done, e := l.locked(ctx)
	if e != nil {
		return z, e
	}
	defer done()
	used, count, e := usage(r)
	if e != nil {
		return z, ErrSource
	}
	if count >= MaxLibraryTracks || used >= libraryMaxBytes {
		return z, ErrLibraryFull
	}
	var b [8]byte
	if _, e = rand.Read(b[:]); e != nil {
		return z, ErrSource
	}
	id := int64(binary.LittleEndian.Uint64(b[:])&((1<<52)-1)) + privateIDMin
	stage := l.family + "/.stage-" + strconv.FormatInt(id, 10)
	if e = r.Mkdir(stage, 0700); e != nil {
		return z, ErrSource
	}
	defer r.RemoveAll(stage)
	f, e := r.OpenFile(stage+"/audio", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return z, ErrSource
	}
	limit := libraryMaxFile
	if libraryMaxBytes-used < limit {
		limit = libraryMaxBytes - used
	}
	n, e := io.Copy(f, io.LimitReader(&contextReader{ctx, input}, limit+1))
	syncErr := f.Sync()
	f.Close()
	if e != nil {
		return z, sourceError(ctx)
	}
	if syncErr != nil {
		return z, ErrSource
	}
	if ctx.Err() != nil {
		return z, sourceError(ctx)
	}
	if n > limit {
		if limit < libraryMaxFile {
			return z, ErrLibraryFull
		}
		return z, ErrInvalid
	}
	if n == 0 {
		return z, ErrInvalid
	}
	t, e := validateMedia(ctx, r, stage+"/audio", format)
	if e != nil {
		return z, e
	}
	t.ID = id
	t.Title = title
	t.Artist = artist
	t.Bytes = n
	t.Format = format
	t.AddedAt = time.Now().UTC()
	data, _ := json.Marshal(t)
	m, e := r.OpenFile(stage+"/meta.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return z, ErrSource
	}
	_, e = m.Write(data)
	if e == nil {
		e = m.Sync()
	}
	m.Close()
	if e != nil {
		return z, ErrSource
	}
	if e = l.check(); e != nil {
		return z, e
	}
	if ctx.Err() != nil {
		return z, sourceError(ctx)
	}
	if e = r.Rename(stage, entryName(l.family, id)); e != nil {
		return z, ErrSource
	}
	return t, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func validateMedia(ctx context.Context, root *os.Root, path, format string) (LibraryTrack, error) {
	var z LibraryTrack
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return z, ErrSource
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-f", format, "-show_streams", "-show_format", "-of", "json", "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	var out boundedBuffer
	cmd.Stdout = &out
	var stderr boundedStderr
	cmd.Stderr = &stderr
	if cmd.Run() != nil || stderr.nonempty() || out.overflow {
		return z, sourceError(ctx)
	}
	var p struct {
		Streams []struct {
			CodecName  string `json:"codec_name"`
			CodecType  string `json:"codec_type"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		}
		Format struct {
			Duration   string `json:"duration"`
			BitRate    string `json:"bit_rate"`
			FormatName string `json:"format_name"`
		}
	}
	if json.Unmarshal(out.b, &p) != nil || len(p.Streams) != 1 {
		return z, ErrInvalid
	}
	s := p.Streams[0]
	seconds, e := strconv.ParseFloat(p.Format.Duration, 64)
	rate, _ := strconv.Atoi(s.SampleRate)
	if e != nil || !(seconds >= 1 && seconds <= 1200) || s.CodecName != format || s.CodecType != "audio" || p.Format.FormatName != format || (s.Channels != 1 && s.Channels != 2) || rate < 8000 || rate > 192000 {
		return z, ErrInvalid
	}
	cmd = exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-v", "error", "-xerror", "-protocol_whitelist", "pipe", "-f", format, "-i", "pipe:0", "-map", "0:a:0", "-f", "null", "-")
	if _, e = f.Seek(0, 0); e != nil {
		return z, ErrSource
	}
	cmd.Stdin = f
	cmd.Stdout = io.Discard
	var decodeErr boundedStderr
	cmd.Stderr = &decodeErr
	if cmd.Run() != nil || decodeErr.nonempty() {
		return z, sourceError(ctx)
	}
	z.DurationMs = int64(seconds * 1000)
	z.SampleRate = rate
	z.Channels = s.Channels
	z.Bitrate, _ = strconv.ParseInt(p.Format.BitRate, 10, 64)
	return z, nil
}

type boundedBuffer struct {
	b        []byte
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.b)+n > 65536 {
		b.overflow = true
		return n, nil
	}
	b.b = append(b.b, p...)
	return n, nil
}
