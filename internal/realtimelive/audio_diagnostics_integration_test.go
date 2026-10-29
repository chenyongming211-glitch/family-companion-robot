package realtimelive

import (
	"encoding/base64"
	"familyrobot/internal/protocol"
	"familyrobot/internal/realtimeaudio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAudioDiagnosticsAuthenticatedAudioPipeline(t *testing.T) {
	testAudioDiagnosticsPipeline(t, false)
}
func TestAudioDiagnosticsSessionShutdownWithBlockedWriter(t *testing.T) {
	testAudioDiagnosticsPipeline(t, true)
}
func testAudioDiagnosticsPipeline(t *testing.T, blocked bool) {
	appended := make(chan int, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		for i := 0; i < 2; i++ {
			var request map[string]any
			if c.ReadJSON(&request) != nil {
				return
			}
			cfg := map[string]any{}
			if i == 0 {
				cfg["enable_search"] = false
			}
			c.WriteJSON(map[string]any{"type": "session.updated", "session": cfg})
		}
		for {
			var request map[string]any
			if c.ReadJSON(&request) != nil {
				return
			}
			if request["type"] == "input_audio_buffer.append" {
				pcm, err := base64.StdEncoding.DecodeString(request["audio"].(string))
				if err != nil {
					return
				}
				appended <- len(pcm) / 2
			}
		}
	}))
	defer provider.Close()
	app := NewServer(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http"), DiagnosticsDevice: diagnosticOwner().DeviceID, DiagnosticsDuration: 5 * time.Second}, nil, t.TempDir(), "diagnostic-pipeline")
	device, err := app.devices.BindXiaozhi(testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	app.cfg.DiagnosticsDevice = device.ID
	entered := make(chan *Session, 1)
	sessionDone := make(chan struct{})
	writerEntered, writerRelease := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseWriter := func() { releaseOnce.Do(func() { close(writerRelease) }) }
	app.runDevice = func(s *Session) {
		if s.diag != nil {
			s.diag.interval = 10 * time.Millisecond
			if blocked {
				open := s.diag.open
				s.diag.open = func() (io.WriteCloser, error) {
					f, e := open()
					if e != nil {
						return nil, e
					}
					return &diagnosticGatedFile{inner: f, entered: writerEntered, release: writerRelease}, nil
				}
			}
		}
		entered <- s
		s.Run()
		close(sessionDone)
	}
	h := httptest.NewServer(app.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	defer releaseWriter()
	c, _, err := deviceDial(h.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deviceHello(t, c)
	s := nextSession(t, entered)
	if s.diag == nil {
		t.Fatal("authenticated target did not activate diagnostics")
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hello protocol.Hello
	if err := c.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	codec, err := realtimeaudio.New(16000, 960)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	packet, err := codec.Encode(make([]byte, 1920))
	if err != nil {
		t.Fatal(err)
	}
	// The first packet belongs to wake upload, not the user's business audio.
	c.WriteMessage(websocket.BinaryMessage, packet)
	c.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "realtime", "session_id": hello.SessionID})
	for i := 0; i < 3; i++ {
		if err := c.WriteMessage(websocket.BinaryMessage, packet); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		select {
		case n := <-appended:
			if n != 960 {
				t.Fatal("wrong PCM count", n)
			}
		case <-time.After(time.Second):
			t.Fatal("upstream write missing")
		}
	}
	deadline := time.Now().Add(time.Second)
	for s.diag.appendedSamples.Load() != 2880 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.diag.appendedSamples.Load() != 2880 {
		t.Fatal("successful upstream writes were not observed")
	}
	// Leave the transport open with no new audio: periodic age records must still
	// reveal the input gap instead of relying on another packet to emit a log.
	time.Sleep(35 * time.Millisecond)
	c.WriteJSON(map[string]any{"type": "abort", "reason": "wake_word_detected", "session_id": hello.SessionID})
	c.WriteJSON(map[string]any{"type": "listen", "state": "stop", "session_id": hello.SessionID})
	if blocked {
		select {
		case <-writerEntered:
		case <-time.After(time.Second):
			t.Fatal("diagnostic writer was not blocked")
		}
		select {
		case <-sessionDone:
		case <-time.After(time.Second):
			releaseWriter()
			t.Fatal("optional diagnostic I/O blocked Session.Run shutdown")
		}
		releaseWriter()
	}
	select {
	case <-s.diag.done:
	case <-time.After(time.Second):
		t.Fatal("diagnostics did not finish with the session")
	}
	rows := diagnosticRows(t, s.directory)
	stats := 0
	source := false
	correctCounters := false
	for _, row := range rows {
		if row["session_id"] != hello.SessionID || row["device_id"] != device.ID {
			t.Fatal("diagnostic ownership lost", row)
		}
		if row["event"] == "audio_stats" {
			stats++
			if row["rx_frames"] == float64(4) && row["decoded_frames"] == float64(3) && row["queued_samples"] == float64(2880) && row["appended_samples"] == float64(2880) && row["rx_age_ms"].(float64) > 10 {
				correctCounters = true
			}
		}
		if row["event"] == "interrupt_request" && row["source"] == "device_abort" && row["active_output"] == false {
			source = true
		}
	}
	minStats := 2
	if blocked {
		minStats = 1
	}
	if stats < minStats || !correctCounters || !source {
		t.Fatal("pipeline, input gap, or no-op abort evidence missing", rows)
	}
}
