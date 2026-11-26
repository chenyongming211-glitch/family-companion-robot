// Package speakerlive carries bounded, memory-only speaker observations to an
// isolated Linux helper. A helper failure permanently ends that observer.
package speakerlive

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

const (
	sampleRate   = 16000
	maxSamples   = 8 * sampleRate
	maxPCMBytes  = maxSamples * 2
	maxLineBytes = 4096
)

var (
	errUnavailable = errors.New("speaker observer unavailable")
	tokenPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// reasonPattern bounds the readable shape of a reason token so an unknown
	// one can be dropped without opening IPC to arbitrary helper text.
	reasonPattern = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)
)

// Config contains fixed cloud paths. The helper verifies the model digest.
// A zero Lifetime follows the owning Session's lifetime instead of imposing a
// fixed wall-clock cap. A zero MaxClips processes clips until the Session ends.
type Config struct {
	Binary, Model, SHA256 string
	Lifetime              time.Duration
	MaxClips              int
}

// Job is a complete mono 16 kHz signed little-endian PCM span. Coordinates are
// source samples; nonzero starts and gaps are allowed.
type Job struct {
	ID, Start, End int64
	PCM            []byte
}

// Result contains no audio, feature vectors, paths, or user text. ElapsedMS is
// wall time spent transporting and observing this request.
type Result struct {
	ID, Start, End       int64
	State, Label, Reason string
	ElapsedMS            int64
	AudioMS              int64
}

type observerLimits struct {
	startup, request, lifetime time.Duration
	maxClips                   int
}

// requestTimeout bounds one observation end to end. It must cover a cold model
// start after a worker restart (up to ten seconds) plus one inference and one
// queued neighbour, because the helper serves clips serially.
const requestTimeout = 15 * time.Second

func limitsFor(cfg Config) observerLimits {
	return observerLimits{startup: 15 * time.Second, request: requestTimeout, lifetime: cfg.Lifetime, maxClips: cfg.MaxClips}
}

type helperEvent struct {
	Event      string `json:"event"`
	ID         int64  `json:"id"`
	Label      string `json:"label"`
	DurationMS int64  `json:"duration_ms"`
	Evidence   string `json:"evidence"`
	Reason     string `json:"reason,omitempty"`
}
type helperOutput struct {
	event helperEvent
	err   error
}

// Observer owns one helper, one running request, and at most one queued copy.
// Callbacks run on its background worker and must return promptly. Close waits
// for any in-flight callback and for the child to be killed and reaped.
type Observer struct {
	ctx                        context.Context
	cancel                     context.CancelFunc
	cmd                        *exec.Cmd
	input                      io.WriteCloser
	output                     io.ReadCloser
	jobs                       chan Job
	events                     chan helperOutput
	readerDone, waitDone, done chan struct{}
	emit                       func(Result)
	limits                     observerLimits
	session, sha               string
	mu                         sync.Mutex
	closed                     bool
	lastID, lastEnd            int64
	accepted                   int
	stopOnce                   sync.Once
}

// New validates configuration and starts the helper without waiting for model
// loading. Readiness and its 15-second deadline belong to the background worker.
func New(ctx context.Context, session string, cfg Config, emit func(Result)) (*Observer, error) {
	return newObserver(ctx, session, cfg, emit, limitsFor(cfg))
}
func newObserver(parent context.Context, session string, cfg Config, emit func(Result), limits observerLimits) (*Observer, error) {
	if parent == nil || parent.Err() != nil || !tokenPattern.MatchString(session) || !shaPattern.MatchString(cfg.SHA256) || !validFile(cfg.Binary, true) || !validFile(cfg.Model, false) {
		return nil, errUnavailable
	}
	if limits.startup <= 0 || limits.startup > 15*time.Second || limits.request <= 0 || limits.request > requestTimeout || limits.lifetime < 0 || limits.maxClips < 0 {
		return nil, errUnavailable
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.Command(cfg.Binary, "session", "--model", cfg.Model, "--sha256", cfg.SHA256, "--declared-single-speaker", "--realtime-test")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	// A nil Stderr is /dev/null, with no copying goroutine or retained child text.
	if configureProcess(cmd) != nil {
		cancel()
		return nil, errUnavailable
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errUnavailable
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		cancel()
		return nil, errUnavailable
	}
	if cmd.Start() != nil {
		_ = input.Close()
		_ = output.Close()
		cancel()
		return nil, errUnavailable
	}
	o := &Observer{ctx: ctx, cancel: cancel, cmd: cmd, input: input, output: output, jobs: make(chan Job, 1), events: make(chan helperOutput, 1), readerDone: make(chan struct{}), waitDone: make(chan struct{}), done: make(chan struct{}), emit: emit, limits: limits, session: session, sha: cfg.SHA256}
	go func() { _ = cmd.Wait(); close(o.waitDone) }()
	interrupt := context.AfterFunc(ctx, o.stop)
	go o.readOutput()
	go func() { defer interrupt(); o.run() }()
	return o, nil
}
func validFile(path string, executable bool) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && (!executable || info.Mode().Perm()&0111 != 0)
}

// Offer copies only accepted, bounded data. It never waits for the child or runs
// the callback. A full queue does not advance accepted IDs or coordinates.
func (o *Observer) Offer(job Job) bool {
	if job.ID <= 0 || job.Start < 0 || job.End <= job.Start || job.End-job.Start > maxSamples || len(job.PCM) > maxPCMBytes || int64(len(job.PCM)) != (job.End-job.Start)*2 {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.ctx.Err() != nil || (o.limits.maxClips > 0 && o.accepted >= o.limits.maxClips) || job.ID <= o.lastID || job.Start < o.lastEnd || len(o.jobs) == cap(o.jobs) {
		return false
	}
	job.PCM = append([]byte(nil), job.PCM...)
	o.jobs <- job
	o.lastID, o.lastEnd = job.ID, job.End
	o.accepted++
	return true
}

// Close is idempotent. Cancellation interrupts blocked pipe I/O, kills the
// process group, reaps the child, clears queued PCM, and ends callbacks.
func (o *Observer) Close() {
	if o == nil {
		return
	}
	o.cancel()
	o.stop()
	<-o.done
}
func (o *Observer) stop() {
	o.stopOnce.Do(func() { killProcess(o.cmd); _ = o.input.Close(); _ = o.output.Close() })
}
func (o *Observer) drainLocked() []Job {
	var jobs []Job
	for {
		select {
		case job := <-o.jobs:
			clear(job.PCM)
			job.PCM = nil
			jobs = append(jobs, job)
		default:
			return jobs
		}
	}
}
func (o *Observer) run() {
	defer close(o.done)
	defer func() {
		o.cancel()
		o.stop()
		<-o.waitDone
		<-o.readerDone
		o.mu.Lock()
		o.closed = true
		o.drainLocked()
		o.mu.Unlock()
	}()
	timeout := o.ctx
	var cancel context.CancelFunc
	var lifetimeDone <-chan struct{}
	if o.limits.lifetime > 0 {
		timeout, cancel = context.WithTimeout(o.ctx, o.limits.lifetime)
		defer cancel()
		lifetimeDone = timeout.Done()
	}
	event, err := o.next(timeout, o.limits.startup)
	if err != nil || event.Event != "ready" {
		o.stop()
		o.fail(nil, "startup_failed")
		return
	}
	for count := 0; ; {
		select {
		case <-o.ctx.Done():
			return
		case <-lifetimeDone:
			o.stop()
			o.fail(nil, "lifetime_expired")
			return
		case <-o.events:
			o.stop()
			o.fail(nil, "helper_exited")
			return // unsolicited output or EOF
		case job := <-o.jobs:
			if o.ctx.Err() != nil {
				clear(job.PCM)
				return
			}
			result, err := o.request(timeout, job)
			if err != nil {
				o.stop()
				o.fail(&job, "request_failed")
				return
			}
			o.deliver(result)
			count++
			if o.limits.maxClips > 0 && count == o.limits.maxClips {
				o.stop()
				o.fail(nil, "clip_limit")
				return
			}
		}
	}
}
func (o *Observer) deliver(result Result) {
	if o.emit != nil && o.ctx.Err() == nil {
		o.emit(result)
	}
}
func (o *Observer) fail(current *Job, reason string) {
	o.mu.Lock()
	o.closed = true
	jobs := o.drainLocked()
	o.mu.Unlock()
	if current != nil {
		clear(current.PCM)
		current.PCM = nil
		jobs = append([]Job{*current}, jobs...)
	}
	if len(jobs) == 0 {
		o.deliver(Result{State: "unavailable", Reason: reason})
		return
	}
	for _, job := range jobs {
		o.deliver(Result{ID: job.ID, Start: job.Start, End: job.End, State: "unavailable", Reason: reason})
	}
}
func (o *Observer) next(parent context.Context, timeout time.Duration) (helperEvent, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return helperEvent{}, errUnavailable
	case item, ok := <-o.events:
		if !ok || item.err != nil {
			return helperEvent{}, errUnavailable
		}
		return item.event, nil
	}
}

type requestHeader struct {
	Version    int    `json:"version"`
	ID         int64  `json:"id"`
	Session    string `json:"session"`
	Scope      string `json:"scope"`
	Generation int    `json:"generation"`
	Start      int64  `json:"start"`
	End        int64  `json:"end"`
	ModelSHA   string `json:"model_sha256"`
	SampleRate int    `json:"sample_rate"`
	PCMBytes   int    `json:"pcm_bytes"`
}

func (o *Observer) request(parent context.Context, job Job) (Result, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, o.limits.request)
	defer cancel()
	written := make(chan error, 1)
	go func() {
		h := requestHeader{1, job.ID, o.session, "realtime-test", 1, job.Start, job.End, o.sha, sampleRate, len(job.PCM)}
		err := writeRequest(o.input, h, job.PCM)
		clear(job.PCM)
		written <- err
	}()
	select {
	case <-ctx.Done():
		o.stop()
		<-written
		return Result{}, errUnavailable
	case err := <-written:
		if err != nil {
			return Result{}, errUnavailable
		}
	}
	select {
	case <-ctx.Done():
		return Result{}, errUnavailable
	case item, ok := <-o.events:
		if !ok || item.err != nil {
			return Result{}, errUnavailable
		}
		e := item.event
		if e.Event != "observation" || e.ID != job.ID || e.DurationMS != (job.End-job.Start)*1000/sampleRate {
			return Result{}, errUnavailable
		}
		result := Result{ID: job.ID, Start: job.Start, End: job.End, State: e.Label, Reason: e.Reason, AudioMS: e.DurationMS, ElapsedMS: time.Since(started).Milliseconds()}
		if len(e.Label) == 7 && e.Label[:6] == "guest_" {
			result.State = "temporary"
			result.Label = e.Label
		}
		return result, nil
	}
}
func writeRequest(w io.Writer, h requestHeader, pcm []byte) error {
	header, err := json.Marshal(h)
	if err != nil || len(header) > maxLineBytes {
		return errUnavailable
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(header)))
	for _, part := range [][]byte{prefix[:], header, pcm} {
		for len(part) > 0 {
			n, err := w.Write(part)
			if err != nil {
				return errUnavailable
			}
			if n <= 0 {
				return errUnavailable
			}
			part = part[n:]
		}
	}
	return nil
}
func (o *Observer) readOutput() {
	defer close(o.readerDone)
	defer close(o.events)
	reader := bufio.NewReaderSize(o.output, maxLineBytes+2)
	for {
		line, err := reader.ReadSlice('\n')
		item := helperOutput{}
		if err != nil || len(line) > maxLineBytes+1 {
			item.err = errUnavailable
		} else {
			item.event, item.err = decodeEvent(line[:len(line)-1])
		}
		select {
		case <-o.ctx.Done():
			return
		case o.events <- item:
		}
		if item.err != nil {
			return
		}
	}
}

// Output is a flat, completely specified JSON object. Duplicate fields, nulls,
// unknown fields, trailing data, floats, and arbitrary helper text are rejected.
func decodeEvent(line []byte) (helperEvent, error) {
	var event helperEvent
	decoder := json.NewDecoder(bytes.NewReader(line))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return event, errUnavailable
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return event, errUnavailable
		}
		key, ok := token.(string)
		if !ok || len(key) > 11 {
			return event, errUnavailable
		}
		if _, exists := fields[key]; exists {
			return event, errUnavailable
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return event, errUnavailable
		}
		fields[key] = value
		if len(fields) > 6 {
			return event, errUnavailable
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return event, errUnavailable
	}
	if _, err = decoder.Token(); err != io.EOF {
		return event, errUnavailable
	}
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	if d.Decode(&event) != nil || event.Evidence != "turn_taking_test" {
		return event, errUnavailable
	}
	required := []string{"event", "evidence"}
	switch event.Event {
	case "ready", "closed":
	case "observation":
		required = append(required, "id", "label", "duration_ms")
		if _, present := fields["reason"]; present {
			required = append(required, "reason")
			// The reason is diagnostic metadata, not part of the event's
			// identity: helper and observer can be built from different
			// commits. An unreadable token still ends the observer, so no
			// arbitrary helper text crosses IPC; a readable token this build
			// does not know degrades to "no reason" for this clip instead of
			// tearing down voice filtering for the rest of the session.
			if !reasonPattern.MatchString(event.Reason) {
				return event, errUnavailable
			}
			if !validHelperReason(event.Label, event.Reason) {
				log.Printf("speaker observer: dropping unsupported reason %q for label %q", event.Reason, event.Label)
				event.Reason = ""
			}
		}
		if event.ID <= 0 || event.DurationMS < 0 || event.DurationMS > 8000 {
			return event, errUnavailable
		}
		if event.Label != "uncertain" && event.Label != "unknown" && !(len(event.Label) == 7 && event.Label[:6] == "guest_" && event.Label[6] >= '1' && event.Label[6] <= '8') {
			return event, errUnavailable
		}
	default:
		return event, errUnavailable
	}
	if len(fields) != len(required) {
		return event, errUnavailable
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return event, errUnavailable
		}
	}
	return event, nil
}

// Only fixed branch names cross IPC; no arbitrary worker/provider error text.
// The accepted set is duplicated on the helper side, so
// TestReasonProtocolMatchesSharedTable pins it against
// testdata/speaker_observation_protocol.json.
func validHelperReason(label, reason string) bool {
	if label == "uncertain" {
		// worker_unavailable is a per-clip degradation: the helper kept running
		// but could not produce a vector for this clip.
		return reason == "too_short" || reason == "low_energy" || reason == "match_uncertain" || reason == "worker_unavailable"
	}
	if label == "unknown" {
		return reason == "capacity"
	}
	return len(label) == 7 && label[:6] == "guest_" && label[6] >= '1' && label[6] <= '8' && reason == "candidate"
}
