package realtimelive

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func diagnosticOwner() Ownership {
	return Ownership{DeviceID: "dev_" + strings.Repeat("a", 32), SessionID: strings.Repeat("b", 32), ClientType: "xiaozhi"}
}

func diagnosticRows(t *testing.T, dir string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "audio-diagnostics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err, string(line))
		}
		rows = append(rows, row)
	}
	return rows
}

func stopDiagnostics(t *testing.T, d *audioDiagnostics) {
	t.Helper()
	d.stop()
	select {
	case <-d.done:
	case <-time.After(time.Second):
		t.Fatal("diagnostics did not stop")
	}
}

func TestAudioDiagnosticsCountersAndPrivacy(t *testing.T) {
	dir := t.TempDir()
	d := newAudioDiagnostics(diagnosticOwner(), dir, time.Second)
	pcm := make([]byte, 640)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 1000)
	}
	d.received(40)
	d.decoded(pcm)
	d.queued(len(pcm)/2, 1)
	d.dequeued(len(pcm) / 2)
	started := d.appendBegin()
	d.appendEnd(len(pcm)/2, started, false)
	d.control("listen_start", "realtime", nil)
	d.control("abort", "", "Bearer never-log-this")
	d.observed("generated", map[string]any{"id": int64(1), "sent": int64(1440), "text": "private generated text"})
	go d.run()
	stopDiagnostics(t, d)
	rows := diagnosticRows(t, dir)
	var stats map[string]any
	for _, row := range rows {
		if row["event"] == "audio_stats" {
			stats = row
		}
	}
	if stats == nil || stats["rx_frames"] != float64(1) || stats["rx_bytes"] != float64(40) || stats["decoded_samples"] != float64(320) || stats["appended_samples"] != float64(320) || stats["pcm_rms"] != float64(1000) {
		t.Fatal("lost pipeline counters", stats)
	}
	if rows[len(rows)-1]["event"] != "diagnostics_stopped" {
		t.Fatal("missing final status", rows)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "audio-diagnostics.jsonl"))
	if strings.Contains(string(raw), "never-log-this") || strings.Contains(string(raw), "private generated") {
		t.Fatal("private input leaked")
	}
	st, _ := os.Stat(filepath.Join(dir, "audio-diagnostics.jsonl"))
	if st.Mode().Perm() != 0600 {
		t.Fatal("diagnostic permissions", st.Mode())
	}
}

type diagnosticBlockedWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *diagnosticBlockedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}
func (w *diagnosticBlockedWriter) Close() error { return nil }

type diagnosticGatedFile struct {
	inner            io.WriteCloser
	entered, release chan struct{}
	once             sync.Once
}

func (w *diagnosticGatedFile) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return w.inner.Write(p)
}
func (w *diagnosticGatedFile) Close() error { return w.inner.Close() }

func TestAudioDiagnosticsDeadlineAccountsForQueuedEvents(t *testing.T) {
	dir := t.TempDir()
	d := newAudioDiagnostics(diagnosticOwner(), dir, 30*time.Millisecond)
	open := d.open
	entered, release := make(chan struct{}), make(chan struct{})
	d.open = func() (io.WriteCloser, error) {
		f, e := open()
		if e != nil {
			return nil, e
		}
		return &diagnosticGatedFile{inner: f, entered: entered, release: release}, nil
	}
	go d.run()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("writer not started")
	}
	for i := 0; i < 100; i++ {
		d.control("abort", "", nil)
	}
	accepted := len(d.events)
	if accepted == 0 {
		close(release)
		stopDiagnostics(t, d)
		t.Fatal("no event entered test window")
	}
	time.Sleep(40 * time.Millisecond)
	close(release)
	select {
	case <-d.done:
	case <-time.After(time.Second):
		t.Fatal("deadline did not stop")
	}
	rows := diagnosticRows(t, dir)
	logged := 0
	for _, r := range rows {
		if r["event"] == "device_control" {
			logged++
		}
	}
	last := rows[len(rows)-1]
	if last["reason"] != "deadline" || logged+int(last["dropped_events"].(float64)) != accepted {
		t.Fatal("queued events vanished without accounting", accepted, logged, last)
	}
}

func TestAudioDiagnosticsBlockedWriterDoesNotBlockVoice(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	d := newAudioDiagnostics(diagnosticOwner(), t.TempDir(), time.Second)
	w := &diagnosticBlockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	d.open = func() (io.WriteCloser, error) { return w, nil }
	s.diag = d
	go d.run()
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	defer func() { close(w.release); stopDiagnostics(t, d); s.cancel() }()
	for i := 0; i < 1000; i++ {
		d.control("abort", "", "wake_word_detected")
	}
	if d.dropped.Load() == 0 {
		t.Fatal("full diagnostic queue did not count drops")
	}
	rt := s.addTurn()
	rt.turn.Begun = true
	if ok, err := s.binary(rt, []byte{1, 2}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := client.ReadMessage(); err != nil {
		t.Fatal("diagnostic file blocked voice", err)
	}
}

func TestAudioDiagnosticsExpiryAndBudget(t *testing.T) {
	t.Run("expiry", func(t *testing.T) {
		dir := t.TempDir()
		d := newAudioDiagnostics(diagnosticOwner(), dir, 30*time.Millisecond)
		d.interval = 5 * time.Millisecond
		go d.run()
		select {
		case <-d.done:
		case <-time.After(time.Second):
			t.Fatal("expiry failed")
		}
		rows := diagnosticRows(t, dir)
		if rows[len(rows)-1]["reason"] != "deadline" {
			t.Fatal(rows)
		}
		count := 0
		for _, r := range rows {
			if r["event"] == "audio_stats" {
				count++
			}
		}
		if count < 2 {
			t.Fatal("no periodic statistics during input silence", rows)
		}
		before := d.rxFrames.Load()
		d.received(40)
		if d.rxFrames.Load() != before {
			t.Fatal("collection continued after expiry")
		}
	})
	t.Run("budget", func(t *testing.T) {
		dir := t.TempDir()
		d := newAudioDiagnostics(diagnosticOwner(), dir, time.Second)
		d.byteLimit = 2048
		d.interval = time.Millisecond
		go d.run()
		select {
		case <-d.done:
		case <-time.After(2 * time.Second):
			t.Fatal("budget failed")
		}
		st, _ := os.Stat(filepath.Join(dir, "audio-diagnostics.jsonl"))
		if st.Size() > 2048 {
			t.Fatal("exceeded byte budget", st.Size())
		}
		rows := diagnosticRows(t, dir)
		if rows[len(rows)-1]["reason"] != "budget" {
			t.Fatal(rows)
		}
	})
}

func TestAudioDiagnosticsExclusiveFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "existing")
	os.WriteFile(target, []byte("unchanged"), 0600)
	if err := os.Symlink(target, filepath.Join(dir, "audio-diagnostics.jsonl")); err != nil {
		t.Fatal(err)
	}
	d := newAudioDiagnostics(diagnosticOwner(), dir, time.Second)
	go d.run()
	select {
	case <-d.done:
	case <-time.After(time.Second):
		t.Fatal("open failure hung")
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "unchanged" || d.enabled() {
		t.Fatal("existing file changed or diagnostics active")
	}
}

func TestAudioDiagnosticsOwnershipAndSingleClaim(t *testing.T) {
	cfg := Config{DiagnosticsDevice: diagnosticOwner().DeviceID, DiagnosticsDuration: time.Second}
	s := NewServer(cfg, nil, t.TempDir(), "diagnostic-test")
	makeSession := func(owner Ownership) *Session { return &Session{rec: recorder{owner: owner}, directory: t.TempDir()} }
	other := diagnosticOwner()
	other.DeviceID = "dev_" + strings.Repeat("c", 32)
	wrong := makeSession(other)
	s.attachDiagnostics(wrong)
	if wrong.diag != nil {
		t.Fatal("other device selected")
	}
	mac := diagnosticOwner()
	mac.ClientType = "mac_native"
	wrong = makeSession(mac)
	s.attachDiagnostics(wrong)
	if wrong.diag != nil {
		t.Fatal("Mac selected")
	}
	first := makeSession(diagnosticOwner())
	s.attachDiagnostics(first)
	if first.diag == nil {
		t.Fatal("target not selected")
	}
	second := makeSession(diagnosticOwner())
	s.attachDiagnostics(second)
	if second.diag != nil {
		t.Fatal("second session selected")
	}
	if s.cfg.DiagnosticsDuration != time.Second {
		t.Fatal("duration changed")
	}
	disabled := NewServer(Config{}, nil, t.TempDir(), "off")
	plain := makeSession(diagnosticOwner())
	disabled.attachDiagnostics(plain)
	if plain.diag != nil {
		t.Fatal("default enabled")
	}
	for _, bad := range []Config{{DiagnosticsDevice: "untrusted"}, {DiagnosticsDevice: diagnosticOwner().DeviceID, DiagnosticsDuration: 6 * time.Minute}} {
		if NewServer(bad, nil, t.TempDir(), "bad").InitializationError() == nil {
			t.Fatal("invalid diagnostics accepted")
		}
	}
}

func TestAudioDiagnosticsDeviceControlProvenance(t *testing.T) {
	dir := t.TempDir()
	d := newAudioDiagnostics(diagnosticOwner(), dir, time.Second)
	w := newXiaozhiWire(nil, 1, 60, "scope")
	w.diag = d
	if e, err := w.control([]byte(`{"type":"listen","state":"start","mode":"realtime","session_id":"scope"}`)); err != nil || e != nil {
		t.Fatal(e, err)
	}
	if e, err := w.control([]byte(`{"type":"abort","reason":"wake_word_detected","session_id":"scope"}`)); err != nil || e["type"] != "interrupt" {
		t.Fatal(e, err)
	}
	if _, err := w.control([]byte(`{"type":"abort","reason":"secret-value","session_id":"wrong"}`)); err == nil {
		t.Fatal("wrong scope accepted")
	}
	go d.run()
	stopDiagnostics(t, d)
	rows := diagnosticRows(t, dir)
	seen := false
	for _, row := range rows {
		if row["event"] == "device_control" && row["control"] == "abort" {
			seen = true
			if row["reported_reason"] != "wake_word_detected" {
				t.Fatal(row)
			}
		}
	}
	if !seen {
		t.Fatal("device source missing")
	}
}

func TestAudioDiagnosticsInterruptSources(t *testing.T) {
	for _, source := range []string{"provider_vad_start", "provider_vad_stop", "device_abort", "generation_error"} {
		t.Run(source, func(t *testing.T) {
			server, _ := testSocket(t)
			dir := t.TempDir()
			s := NewSession(server, Config{}, dir, nil)
			d := newAudioDiagnostics(diagnosticOwner(), dir, time.Second)
			s.diag = d
			rt := s.addTurn()
			if err := s.interruptFrom(nil, source); err != nil || !rt.turn.Cut {
				t.Fatal("cancellation changed", err)
			}
			if err := s.interruptFrom(nil, source); err != nil {
				t.Fatal(err)
			}
			go d.run()
			stopDiagnostics(t, d)
			s.cancel()
			rows := diagnosticRows(t, dir)
			requests := 0
			for _, row := range rows {
				if row["event"] == "interrupt_request" {
					requests++
					if row["source"] != source {
						t.Fatal(row)
					}
				}
			}
			if requests != 2 {
				t.Fatal("no-op interrupt request was lost", rows)
			}
		})
	}
}

func TestAudioDiagnosticsRetiredGenerationKeepsItsOwnIdentity(t *testing.T) {
	server, _ := testSocket(t)
	dir := t.TempDir()
	s := NewSession(server, Config{}, dir, nil)
	d := newAudioDiagnostics(diagnosticOwner(), dir, time.Second)
	s.diag = d
	old := s.addTurn()
	current := s.addTurn()
	if err := s.interruptFrom(old, "generation_error"); err != nil || current.turn.Cut {
		t.Fatal("old failure cancelled current output", err)
	}
	go d.run()
	stopDiagnostics(t, d)
	s.cancel()
	for _, row := range diagnosticRows(t, dir) {
		if row["event"] == "interrupt_request" {
			if row["id"] != float64(old.turn.ID) || row["active_output"] != false {
				t.Fatal("old failure attributed to new turn", row)
			}
			return
		}
	}
	t.Fatal("request missing")
}
