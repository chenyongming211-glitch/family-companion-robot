package music

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os/exec"
	"sync"
	"time"
)

func ffmpegCommand(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
		"-protocol_whitelist", "pipe", "-f", "mp3", "-i", "pipe:0", "-vn", "-ac", "1", "-ar", "24000",
		"-af", "volume=-6dB", "-f", "s16le", "pipe:1")
}

// Stream returns decoded PCM sample count, never proof of device playback.
// emit is synchronous, must honor the caller's context while blocked, and must
// copy the chunk if retaining it. There is no decoded PCM queue in this package.
func (c *Catalog) Stream(ctx context.Context, r Resolved, emit func([]byte) error) (int64, error) {
	u, err := url.Parse(r.url)
	if emit == nil || !validTrack(r.Track) || r.Track != r.track || r.size <= 0 || r.size > maxSourceBytes || err != nil || !validURL(u, false) {
		return 0, ErrInvalid
	}
	if ctx.Err() != nil {
		return 0, sourceError(ctx)
	}
	runCtx, cancel := context.WithTimeout(ctx, r.Track.Duration+30*time.Second)
	defer cancel()
	// A preparation deadline ends after headers, without imposing a 15-second
	// deadline on the subsequent complete-song body.
	preparation := time.AfterFunc(prepareTimeout, cancel)
	resp, err := c.get(runCtx, r.url, false)
	preparation.Stop()
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || (resp.ContentLength >= 0 && resp.ContentLength != r.size) {
		return 0, ErrSource
	}
	if typ := resp.Header.Get("Content-Type"); typ != "" {
		media, _, e := mime.ParseMediaType(typ)
		if e != nil || (media != "audio/mpeg" && media != "audio/mp3" && media != "application/octet-stream") {
			return 0, ErrSource
		}
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return 0, ErrSource
	}
	cmd := c.command(runCtx)
	var stderr boundedStderr
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, ErrSource
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return 0, ErrSource
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return 0, ErrSource
	}
	// Closing the request on cancellation also wakes a currently blocked Read.
	closeBody := context.AfterFunc(runCtx, func() { resp.Body.Close() })
	defer closeBody()
	type feedResult struct {
		bytes int64
		err   error
	}
	fed := make(chan feedResult, 1)
	go func() {
		reader := &progressReader{body: resp.Body, cancel: cancel, timeout: idleTimeout}
		n, e := io.CopyBuffer(stdin, io.LimitReader(reader, r.size+1), make([]byte, 32<<10))
		stdin.Close()
		fed <- feedResult{n, e}
	}()
	var samples int64
	var decodeErr, errorConsumer error
	buf := make([]byte, 2880)
	maxSamples := int64((r.Track.Duration + 250*time.Millisecond) * 24000 / time.Second)
	for {
		n, e := io.ReadFull(stdout, buf)
		if n > 0 {
			if n%2 != 0 {
				decodeErr = ErrSource
				break
			}
			samples += int64(n / 2)
			if samples > maxSamples {
				decodeErr = ErrSource
				break
			}
			if runCtx.Err() != nil {
				decodeErr = sourceError(runCtx)
				break
			}
			if err = emit(buf[:n]); err != nil {
				errorConsumer = err
				break
			}
		}
		if e == io.EOF || e == io.ErrUnexpectedEOF {
			break
		}
		if e != nil {
			decodeErr = ErrSource
			break
		}
	}
	// Kill on every early exit; drain both producer and child before returning.
	if decodeErr != nil || errorConsumer != nil {
		cancel()
		resp.Body.Close()
		stdout.Close()
	}
	feed := <-fed
	waitErr := cmd.Wait()
	if errorConsumer != nil {
		return samples, sourceFailure{errorConsumer}
	}
	if ctx.Err() != nil {
		return samples, sourceError(ctx)
	}
	if runCtx.Err() != nil {
		return samples, sourceError(runCtx)
	}
	if decodeErr != nil || waitErr != nil || feed.err != nil || feed.bytes != r.size || stderr.nonempty() {
		return samples, ErrSource
	}
	minSamples := int64((r.Track.Duration - 250*time.Millisecond) * 24000 / time.Second)
	if samples < minSamples {
		return samples, ErrSource
	}
	return samples, nil
}

// The watchdog runs only while waiting for the network, so consumer backpressure
// cannot be mistaken for a stalled source. Closing the body releases the Read.
type progressReader struct {
	body    io.ReadCloser
	cancel  context.CancelFunc
	timeout time.Duration
}

func (r *progressReader) Read(p []byte) (int, error) {
	timer := time.AfterFunc(r.timeout, func() { r.cancel(); r.body.Close() })
	n, e := r.body.Read(p)
	timer.Stop()
	return n, e
}

// Drain all stderr without retaining source details or unbounded diagnostics.
type boundedStderr struct {
	mu    sync.Mutex
	bytes int
	data  [4096]byte
}

func (s *boundedStderr) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bytes < len(s.data) {
		s.bytes += copy(s.data[s.bytes:], p)
	}
	return len(p), nil
}
func (s *boundedStderr) nonempty() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.bytes > 0 }
