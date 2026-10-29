package realtimelive

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const diagnosticWindow = 5 * time.Minute

var diagnosticDevicePattern = regexp.MustCompile(`^dev_[a-f0-9]{32}$`)

// Counters observe the existing path. They never authorize input or cancel audio.
// A blocked diagnostic writer is deliberately outside Session.wg: shutdown and
// voice must not wait for optional file I/O. Only this bounded worker owns its file.
type audioDiagnostics struct {
	owner                                                                           Ownership
	start                                                                           time.Time
	duration, interval                                                              time.Duration
	byteLimit                                                                       int
	open                                                                            func() (io.WriteCloser, error)
	active                                                                          atomic.Bool
	stopOnce                                                                        sync.Once
	stopped, done                                                                   chan struct{}
	events                                                                          chan map[string]any
	eventMu                                                                         sync.Mutex // Admission/termination only; never held for I/O.
	dropped                                                                         atomic.Uint64
	rxFrames, rxBytes, decodedFrames, decodedSamples                                atomic.Uint64
	queuedSamples, dequeuedSamples, appendAttempts, appendedFrames, appendedSamples atomic.Uint64
	decodeErrors, appendErrors, queueHigh                                           atomic.Uint64
	lastRX, lastPCM, lastAppend, appendStarted, maxRXGap, maxAppendTime             atomic.Int64
	mode                                                                            atomic.Int32
	energyMu                                                                        sync.Mutex
	energy, energySamples                                                           uint64
	peak                                                                            int
}

func newAudioDiagnostics(owner Ownership, dir string, duration time.Duration) *audioDiagnostics {
	d := &audioDiagnostics{owner: owner, start: time.Now(), duration: duration, interval: time.Second,
		byteLimit: 1 << 20, stopped: make(chan struct{}), done: make(chan struct{}), events: make(chan map[string]any, 128)}
	d.active.Store(true)
	d.open = func() (io.WriteCloser, error) {
		return os.OpenFile(filepath.Join(dir, "audio-diagnostics.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	return d
}

func (d *audioDiagnostics) enabled() bool {
	if d == nil || !d.active.Load() {
		return false
	}
	if time.Since(d.start) >= d.duration {
		d.active.Store(false)
		return false
	}
	return true
}
func (d *audioDiagnostics) elapsedNS() int64 { return time.Since(d.start).Nanoseconds() + 1 }
func maxDiagnostic(v *atomic.Int64, n int64) {
	for old := v.Load(); n > old; old = v.Load() {
		if v.CompareAndSwap(old, n) {
			return
		}
	}
}
func (d *audioDiagnostics) received(n int) {
	if !d.enabled() {
		return
	}
	now := d.elapsedNS()
	last := d.lastRX.Swap(now)
	if last > 0 {
		maxDiagnostic(&d.maxRXGap, now-last)
	}
	d.rxFrames.Add(1)
	d.rxBytes.Add(uint64(n))
}
func (d *audioDiagnostics) decodeFailed() {
	if d.enabled() {
		d.decodeErrors.Add(1)
	}
}
func (d *audioDiagnostics) decoded(pcm []byte) {
	if !d.enabled() || len(pcm) == 0 {
		return
	}
	d.decodedFrames.Add(1)
	d.decodedSamples.Add(uint64(len(pcm) / 2))
	d.lastPCM.Store(d.elapsedNS())
	var energy uint64
	peak := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		n := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		energy += uint64(int64(n) * int64(n))
		if n < 0 {
			n = -n
		}
		if n > peak {
			peak = n
		}
	}
	d.energyMu.Lock()
	d.energy += energy
	d.energySamples += uint64(len(pcm) / 2)
	if peak > d.peak {
		d.peak = peak
	}
	d.energyMu.Unlock()
}
func (d *audioDiagnostics) queued(samples, depth int) {
	if !d.enabled() {
		return
	}
	d.queuedSamples.Add(uint64(samples))
	for old := d.queueHigh.Load(); uint64(depth) > old; old = d.queueHigh.Load() {
		if d.queueHigh.CompareAndSwap(old, uint64(depth)) {
			break
		}
	}
}
func (d *audioDiagnostics) dequeued(samples int) {
	if d.enabled() {
		d.dequeuedSamples.Add(uint64(samples))
	}
}
func (d *audioDiagnostics) appendBegin() int64 {
	if !d.enabled() {
		return 0
	}
	n := d.elapsedNS()
	d.appendAttempts.Add(1)
	d.appendStarted.Store(n)
	return n
}
func (d *audioDiagnostics) appendEnd(samples int, started int64, failed bool) {
	if d == nil || started == 0 {
		return
	}
	d.appendStarted.Store(0)
	if !d.enabled() {
		return
	}
	maxDiagnostic(&d.maxAppendTime, d.elapsedNS()-started)
	if failed {
		d.appendErrors.Add(1)
		return
	}
	d.appendedFrames.Add(1)
	d.appendedSamples.Add(uint64(samples))
	d.lastAppend.Store(d.elapsedNS())
}
func (d *audioDiagnostics) note(event string, values map[string]any) {
	if d == nil {
		return
	}
	d.eventMu.Lock()
	defer d.eventMu.Unlock()
	if !d.enabled() {
		return
	}
	values["event"] = event
	values["elapsed_ms"] = time.Since(d.start).Milliseconds()
	values["at"] = float64(time.Now().UnixNano()) / 1e9
	select {
	case d.events <- values:
	default:
		d.dropped.Add(1)
	}
}
func diagnosticReason(reason any) string {
	if reason == nil {
		return "missing"
	}
	s, ok := reason.(string)
	if !ok {
		return "other"
	}
	switch s {
	case "wake_word_detected", "none":
		return s
	case "":
		return "missing"
	default:
		return "other"
	}
}
func (d *audioDiagnostics) control(control, mode string, reason any) {
	if !d.enabled() {
		return
	}
	v := map[string]any{"control": control}
	switch control {
	case "listen_start":
		if mode == "realtime" {
			d.mode.Store(2)
		} else if mode == "auto" {
			d.mode.Store(1)
		} else {
			return
		}
		v["reported_mode"] = mode
	case "listen_stop", "listen_detect":
	case "abort":
		v["reported_reason"] = diagnosticReason(reason)
	default:
		return
	}
	d.note("device_control", v)
}
func (d *audioDiagnostics) interrupt(source string, id int64, active bool) {
	if !d.enabled() {
		return
	}
	switch source {
	case "provider_vad_start", "provider_vad_stop", "device_abort", "generation_error":
	default:
		source = "unspecified"
	}
	d.note("interrupt_request", map[string]any{"source": source, "id": id, "active_output": active})
}

// Explicit fields only: never copy provider/device objects, text or error strings.
func (d *audioDiagnostics) observed(event string, fields map[string]any) {
	if !d.enabled() {
		return
	}
	switch event {
	case "speech_started", "input_segment", "user_text", "first_packet", "generated", "generated_interrupted", "playback", "playback_rejected", "drain_timeout", "turn_error", "input_failed", "transport_closed", "closed":
	default:
		return
	}
	v := map[string]any{}
	for _, key := range []string{"id", "sample", "received_end", "samples", "sent", "completed", "accepted", "drain_ms", "speech_stop_to_packet_ms", "close_code", "turns"} {
		x := fields[key]
		switch x.(type) {
		case int, int64, uint64, float64:
			v[key] = x
		}
	}
	if text, ok := fields["text"].(string); ok && event == "user_text" {
		v["transcript_present"] = strings.TrimSpace(text) != ""
		v["transcript_runes"] = utf8.RuneCountInString(text)
	}
	if terminal, ok := fields["terminal"].(string); ok && (terminal == "played" || terminal == "drained") {
		v["terminal"] = terminal
	}
	if valid, ok := fields["provider_timestamp"].(bool); ok {
		v["provider_timestamp"] = valid
	}
	d.note(event, v)
}
func (d *audioDiagnostics) stats() map[string]any {
	now := d.elapsedNS()
	age := func(n int64) int64 {
		if n == 0 {
			return -1
		}
		return (now - n) / int64(time.Millisecond)
	}
	d.energyMu.Lock()
	energy, n, peak := d.energy, d.energySamples, d.peak
	d.energy, d.energySamples, d.peak = 0, 0, 0
	d.energyMu.Unlock()
	rms := 0
	if n > 0 {
		rms = int(math.Sqrt(float64(energy) / float64(n)))
	}
	mode := map[int32]string{0: "unknown", 1: "auto", 2: "realtime"}[d.mode.Load()]
	queued, dequeued := d.queuedSamples.Load(), d.dequeuedSamples.Load()
	pending := uint64(0)
	if queued > dequeued {
		pending = queued - dequeued
	}
	return map[string]any{"event": "audio_stats", "rx_frames": d.rxFrames.Load(), "rx_bytes": d.rxBytes.Load(),
		"decoded_frames": d.decodedFrames.Load(), "decoded_samples": d.decodedSamples.Load(), "decode_errors": d.decodeErrors.Load(),
		"queued_samples": queued, "dequeued_samples": dequeued, "queue_pending_samples": pending, "queue_high_frames": d.queueHigh.Load(),
		"append_attempts": d.appendAttempts.Load(), "appended_frames": d.appendedFrames.Load(), "appended_samples": d.appendedSamples.Load(), "append_errors": d.appendErrors.Load(),
		"rx_age_ms": age(d.lastRX.Load()), "pcm_age_ms": age(d.lastPCM.Load()), "append_ok_age_ms": age(d.lastAppend.Load()), "append_in_flight_ms": age(d.appendStarted.Load()),
		"max_rx_gap_ms": d.maxRXGap.Load() / int64(time.Millisecond), "max_append_ms": d.maxAppendTime.Load() / int64(time.Millisecond),
		"pcm_rms": rms, "pcm_peak": peak, "pcm_window_samples": n, "reported_listen_mode": mode, "dropped_events": d.dropped.Load()}
}
func (d *audioDiagnostics) stop() {
	if d != nil {
		d.eventMu.Lock()
		defer d.eventMu.Unlock()
		d.active.Store(false)
		d.stopOnce.Do(func() { close(d.stopped) })
	}
}

// Account for accepted events left unwritten at a deadline or writer failure.
// eventMu makes the cutoff atomic with queue admission, not with audio counters.
func (d *audioDiagnostics) discardPending(extra uint64) {
	d.eventMu.Lock()
	defer d.eventMu.Unlock()
	d.active.Store(false)
	pending := len(d.events)
	for i := 0; i < pending; i++ {
		<-d.events
	}
	d.dropped.Add(uint64(pending) + extra)
}
func (d *audioDiagnostics) run() {
	defer close(d.done)
	defer d.active.Store(false)
	out, err := d.open()
	if err != nil {
		d.discardPending(0)
		return
	}
	defer out.Close()
	written := 0
	write := func(row map[string]any, reserve bool) bool {
		row["schema_version"] = 1
		row["device_id"] = d.owner.DeviceID
		row["session_id"] = d.owner.SessionID
		if _, ok := row["at"]; !ok {
			row["at"] = float64(time.Now().UnixNano()) / 1e9
		}
		if _, ok := row["elapsed_ms"]; !ok {
			row["elapsed_ms"] = time.Since(d.start).Milliseconds()
		}
		b, e := json.Marshal(row)
		if e != nil {
			return false
		}
		b = append(b, '\n')
		limit := d.byteLimit
		if reserve {
			limit -= 512
		}
		if written+len(b) > limit {
			return false
		}
		n, e := out.Write(b)
		written += n
		return e == nil && n == len(b)
	}
	if !write(map[string]any{"event": "diagnostics_started", "duration_ms": d.duration.Milliseconds()}, true) {
		d.discardPending(0)
		return
	}
	tick := time.NewTicker(d.interval)
	defer tick.Stop()
	remaining := time.Until(d.start.Add(d.duration))
	if remaining < 0 {
		remaining = 0
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	reason := "session_closed"
	finish := func() {
		write(map[string]any{"event": "diagnostics_stopped", "reason": reason, "dropped_events": d.dropped.Load()}, false)
	}
	for {
		select {
		case event := <-d.events:
			if !write(event, true) {
				d.discardPending(1)
				reason = "budget_or_write_failed"
				finish()
				return
			}
		case <-tick.C:
			if !write(d.stats(), true) {
				d.discardPending(0)
				reason = "budget"
				finish()
				return
			}
		case <-timer.C:
			reason = "deadline"
			d.discardPending(0)
			write(d.stats(), true)
			finish()
			return
		case <-d.stopped:
			for {
				select {
				case event := <-d.events:
					if !write(event, true) {
						d.discardPending(1)
						reason = "budget"
						finish()
						return
					}
				default:
					if !write(d.stats(), true) {
						reason = "budget"
					}
					finish()
					return
				}
			}
		}
	}
}

func (s *Server) attachDiagnostics(session *Session) {
	owner := session.rec.owner
	if session.ownerErr != nil || owner.ClientType != "xiaozhi" || owner.DeviceID == "" || owner.DeviceID != s.cfg.DiagnosticsDevice {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diagnosticClaimed || s.initErr != nil {
		return
	}
	s.diagnosticClaimed = true
	session.diag = newAudioDiagnostics(owner, session.directory, s.cfg.DiagnosticsDuration)
	s.diagnosticCapture = session.diag
	if w, ok := session.wire.(*xiaozhiWire); ok {
		w.diag = session.diag
	}
}
