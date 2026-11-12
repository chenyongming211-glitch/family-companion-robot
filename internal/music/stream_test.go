package music

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixtureOnce sync.Once
var fixtureMP3 []byte
var fixtureErr error

func syntheticMP3(t *testing.T) []byte {
	t.Helper()
	fixtureOnce.Do(func() {
		fixtureMP3, fixtureErr = exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=2", "-ac", "2", "-codec:a", "libmp3lame", "-b:a", "128k", "-f", "mp3", "pipe:1").Output()
	})
	if fixtureErr != nil {
		t.Fatalf("synthetic MP3 generation: %v", fixtureErr)
	}
	return fixtureMP3
}
func streamFixture(data []byte, declared int64, duration time.Duration) (*Catalog, Resolved) {
	c := New()
	c.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), ContentLength: declared, Request: req}, nil
	})}
	t := Track{ID: 1, Title: "synthetic", Artist: "test", Duration: duration}
	return c, Resolved{Track: t, track: t, url: "https://music.126.net/test.mp3", size: declared}
}
func boundedStreamContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestStreamRejectsMissingOrOverlongCallerDeadlineBeforeIO(t *testing.T) {
	for _, name := range []string{"missing", "overlong"} {
		t.Run(name, func(t *testing.T) {
			c, r := streamFixture(nil, 100, 2*time.Second)
			ctx := context.Background()
			if name == "overlong" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, r.Track.Duration+31*time.Second)
				defer cancel()
			}
			networkCalled, childCalled := false, false
			c.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				networkCalled = true
				return nil, errors.New("unexpected network")
			})
			c.command = func(context.Context) *exec.Cmd { childCalled = true; return nil }
			n, err := c.Stream(ctx, r, func([]byte) error { return nil })
			if !errors.Is(err, ErrInvalid) || n != 0 || networkCalled || childCalled {
				t.Fatalf("deadline accepted: samples=%d err=%v network=%v child=%v", n, err, networkCalled, childCalled)
			}
		})
	}
}

func TestStreamAlreadyCanceledCallerReturnsCauseBeforeDeadlineValidation(t *testing.T) {
	c, r := streamFixture(nil, 100, 2*time.Second)
	ctx, cancel := context.WithCancelCause(context.Background())
	want := errors.New("caller stopped")
	cancel(want)
	c.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled caller reached network")
		return nil, nil
	})
	c.command = func(context.Context) *exec.Cmd { t.Fatal("canceled caller started child"); return nil }
	if n, err := c.Stream(ctx, r, func([]byte) error { return nil }); n != 0 || !errors.Is(err, want) {
		t.Fatalf("cancellation cause lost: samples=%d err=%v", n, err)
	}
}

func TestStreamCallerDeadlineReleasesBlockedConsumerAndReapsProcess(t *testing.T) {
	data := syntheticMP3(t)
	c, r := streamFixture(data, int64(len(data)), 2*time.Second)
	var child *exec.Cmd
	c.command = func(ctx context.Context) *exec.Cmd { child = ffmpegCommand(ctx); return child }
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.Stream(ctx, r, func([]byte) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("no backpressured callback before deadline")
	}
	select {
	case err := <-done:
		t.Fatalf("consumer returned before caller deadline: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != context.DeadlineExceeded {
			t.Fatalf("caller deadline not observed: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("deadline did not release consumer")
	}
	if child == nil || child.ProcessState == nil {
		t.Fatal("Stream returned without waiting for child")
	}
}

func TestStreamDecodesMP3ToBounded24KMonoPCM(t *testing.T) {
	data := syntheticMP3(t)
	c, r := streamFixture(data, int64(len(data)), 2*time.Second)
	var total int64
	var pcm bytes.Buffer
	n, err := c.Stream(boundedStreamContext(t), r, func(b []byte) error {
		if len(b) == 0 || len(b) > 2880 || len(b)%2 != 0 {
			t.Fatalf("chunk length %d", len(b))
		}
		total += int64(len(b) / 2)
		pcm.Write(b)
		return nil
	})
	if err != nil || n != total || n < 48000 || n > 54000 {
		t.Fatalf("samples=%d total=%d err=%v", n, total, err)
	}
	// Compare actual samples to an independent fixed-format reference decode;
	// this covers sample rate, channels and the required -6dB gain.
	ref := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "mp3", "-i", "pipe:0", "-ac", "1", "-ar", "24000", "-af", "volume=-6dB", "-f", "s16le", "pipe:1")
	ref.Stdin = bytes.NewReader(data)
	want, e := ref.Output()
	if e != nil || !bytes.Equal(pcm.Bytes(), want) {
		t.Fatalf("PCM differs from mono24k -6dB reference: %v", e)
	}
}
func TestStreamRejectsTruncatedCorruptAndDeceptiveSources(t *testing.T) {
	data := syntheticMP3(t)
	for _, tc := range []struct {
		name     string
		data     []byte
		size     int64
		duration time.Duration
	}{
		{"truncated", data[:len(data)/2], int64(len(data)), 2 * time.Second},
		{"extra bytes", append(append([]byte(nil), data...), 1), int64(len(data)), 2 * time.Second},
		{"corrupt", []byte("not MP3"), 7, 2 * time.Second},
		{"too short", data, int64(len(data)), 10 * time.Second},
		{"too long", data, int64(len(data)), time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, r := streamFixture(tc.data, tc.size, tc.duration)
			_, err := c.Stream(boundedStreamContext(t), r, func([]byte) error { return nil })
			if !errors.Is(err, ErrSource) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
func TestStreamRejectsForgedAndModifiedResolved(t *testing.T) {
	c := New()
	if _, e := c.Stream(context.Background(), Resolved{}, func([]byte) error { return nil }); !errors.Is(e, ErrInvalid) {
		t.Fatalf("forged: %v", e)
	}
	data := syntheticMP3(t)
	c, r := streamFixture(data, int64(len(data)), 2*time.Second)
	r.Track.Duration = time.Minute
	if _, e := c.Stream(boundedStreamContext(t), r, func([]byte) error { return nil }); !errors.Is(e, ErrInvalid) {
		t.Fatalf("modified: %v", e)
	}
	_, r = streamFixture(data, int64(len(data)), 2*time.Second)
	if _, e := c.Stream(boundedStreamContext(t), r, nil); !errors.Is(e, ErrInvalid) {
		t.Fatalf("nil consumer: %v", e)
	}
}
func TestStreamConsumerBackpressureCancelAndProcessReap(t *testing.T) {
	data := syntheticMP3(t)
	c, r := streamFixture(data, int64(len(data)), 2*time.Second)
	var child *exec.Cmd
	c.command = func(ctx context.Context) *exec.Cmd { child = ffmpegCommand(ctx); return child }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.Stream(ctx, r, func([]byte) error { close(entered); <-release; return ctx.Err() })
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no chunk")
	}
	select {
	case err := <-done:
		t.Fatalf("ignored backpressure: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	cancel()
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not return")
	}
	if child == nil || child.ProcessState == nil || !child.ProcessState.Exited() && child.ProcessState.Success() {
		t.Fatal("child not waited")
	}
	for _, arg := range child.Args {
		if strings.Contains(arg, "https:") {
			t.Fatal("URL reached ffmpeg")
		}
	}
	joined := strings.Join(child.Args, " ")
	if !strings.Contains(joined, "-protocol_whitelist pipe") || !strings.Contains(joined, "-f mp3 -i pipe:0") {
		t.Fatal("unrestricted ffmpeg input")
	}
}
func TestStreamConsumerErrorIsReturnedAfterReap(t *testing.T) {
	data := syntheticMP3(t)
	c, r := streamFixture(data, int64(len(data)), 2*time.Second)
	want := errors.New("consumer stopped")
	var cmd *exec.Cmd
	c.command = func(ctx context.Context) *exec.Cmd { cmd = ffmpegCommand(ctx); return cmd }
	_, e := c.Stream(boundedStreamContext(t), r, func([]byte) error { return want })
	if !errors.Is(e, want) || cmd.ProcessState == nil {
		t.Fatalf("consumer failure not reaped: %v", e)
	}
}

type stalledBody struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newStalledBody() *stalledBody {
	return &stalledBody{started: make(chan struct{}), closed: make(chan struct{})}
}
func (b *stalledBody) Read([]byte) (int, error) {
	b.startOnce.Do(func() { close(b.started) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *stalledBody) Close() error { b.closeOnce.Do(func() { close(b.closed) }); return nil }

func TestStreamCancellationClosesStalledNetworkAndReapsProcess(t *testing.T) {
	c, r := streamFixture(nil, 100, 2*time.Second)
	body := newStalledBody()
	var cmd *exec.Cmd
	c.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, ContentLength: -1, Request: req}, nil
	})
	c.command = func(ctx context.Context) *exec.Cmd { cmd = ffmpegCommand(ctx); return cmd }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := c.Stream(ctx, r, func([]byte) error { return nil }); done <- e }()
	select {
	case <-body.started:
	case <-time.After(3 * time.Second):
		t.Fatal("not reading")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatalf("cancel: %v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stalled network leaked")
	}
	if cmd.ProcessState == nil {
		t.Fatal("process not waited")
	}
}

func TestReadWatchdogClosesStalledBody(t *testing.T) {
	if idleTimeout != 10*time.Second {
		t.Fatal("network idle limit changed")
	}
	body := newStalledBody()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := progressReader{body: body, cancel: cancel, timeout: 20 * time.Millisecond}
	_, e := r.Read(make([]byte, 1))
	if e == nil || ctx.Err() != context.Canceled {
		t.Fatalf("watchdog failed: %v", e)
	}
}

func TestStreamRejectsHTTPSizeTypeAndEncodingBeforeDecoder(t *testing.T) {
	for _, tc := range []struct {
		name, header, value string
		length              int64
	}{
		{"size mismatch", "Content-Type", "audio/mpeg", 1},
		{"wrong type", "Content-Type", "text/html", 100},
		{"encoding", "Content-Encoding", "gzip", 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, r := streamFixture(nil, 100, time.Second)
			called := false
			c.command = func(context.Context) *exec.Cmd { called = true; return nil }
			c.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{tc.header: []string{tc.value}}, Body: io.NopCloser(strings.NewReader("")), ContentLength: tc.length, Request: req}, nil
			})
			_, e := c.Stream(boundedStreamContext(t), r, func([]byte) error { return nil })
			if !errors.Is(e, ErrSource) || called {
				t.Fatalf("unsafe HTTP accepted: %v", e)
			}
		})
	}
}

func TestStderrIsBoundedAndConsumerErrorsAreSanitized(t *testing.T) {
	var stderr boundedStderr
	p := bytes.Repeat([]byte("x"), 1<<20)
	if n, e := stderr.Write(p); e != nil || n != len(p) || stderr.bytes != 4096 || !stderr.nonempty() {
		t.Fatal("stderr not bounded")
	}
	data := syntheticMP3(t)
	c, r := streamFixture(data, int64(len(data)), 2*time.Second)
	want := errors.New("https://secret.invalid/source-body")
	_, e := c.Stream(boundedStreamContext(t), r, func([]byte) error { return want })
	if e.Error() != "music_source" || !errors.Is(e, want) {
		t.Fatalf("consumer error escaped: %v", e)
	}
}
