package gateway

import (
	"testing"
	"time"

	"familyrobot/internal/audio"
	"github.com/gorilla/websocket"
)

const testIdleTimeout = 400 * time.Millisecond

func newIdleHarness(t *testing.T) (*toolHarness, *toolPeer) {
	t.Helper()
	h := newToolHarness(t, false, func(cfg *Config) { cfg.IdleTimeout = testIdleTimeout })
	p, _ := h.model(t)
	return h, p
}

// Keep transport traffic flowing, just as the firmware does while listening.
// Only an actual connection close satisfies the idle expectation.
func idleTraffic(t *testing.T, p *toolPeer, d time.Duration, send func()) bool {
	t.Helper()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	end := time.NewTimer(d)
	defer end.Stop()
	for {
		select {
		case ev := <-p.in:
			if ev.err != nil {
				return true
			}
			t.Fatalf("unexpected device event during idle observation: %+v", ev)
		case <-tick.C:
			send()
		case <-end.C:
			return false
		}
	}
}

func silenceSender(t *testing.T, p *toolPeer) func() {
	t.Helper()
	enc, err := audio.NewEncoderAtRate(16000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(enc.Close)
	packet, err := enc.Encode(make([]byte, 1920))
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		// A write may race the expected close; the reader is authoritative.
		p.c.WriteMessage(websocket.BinaryMessage, packet)
	}
}

func TestIdleSilenceAndAutomaticControlsExpire(t *testing.T) {
	for _, traffic := range []string{"silence", "automatic-controls"} {
		t.Run(traffic, func(t *testing.T) {
			h, _ := newIdleHarness(t)
			send := silenceSender(t, h.device)
			if traffic == "automatic-controls" {
				send = func() {
					h.device.c.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "auto"})
					h.device.c.WriteJSON(map[string]any{"type": "iot"})
				}
			}
			if !idleTraffic(t, h.device, 3*testIdleTimeout, send) {
				t.Fatal("silent transport traffic kept the conversation open past its idle deadline")
			}
		})
	}
}

func TestIdleActiveCaptureThenSilenceExpires(t *testing.T) {
	for _, mode := range []string{"auto", "manual"} {
		t.Run(mode, func(t *testing.T) {
			h, model := newIdleHarness(t)
			if mode == "manual" {
				h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": mode})
				model.want(t, "session.update")
			} else {
				model.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
				// This ordered model event gives a wire barrier after speech_started.
				model.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "synthetic speech"})
				h.device.want(t, "stt")
			}
			send := silenceSender(t, h.device)
			if idleTraffic(t, h.device, 3*testIdleTimeout, send) {
				t.Fatal("conversation closed during active capture")
			}
			if mode == "manual" {
				h.device.send(t, map[string]any{"type": "listen", "state": "stop"})
			} else {
				model.send(t, map[string]any{"type": "input_audio_buffer.speech_stopped"})
			}
			if idleTraffic(t, h.device, 2*testIdleTimeout, send) {
				return
			}
			t.Fatal("silence after capture did not expire the idle conversation")
		})
	}
}

func TestIdleAssistantPlaybackGetsFullWindow(t *testing.T) {
	h, model := newIdleHarness(t)
	toolParent(t, model, "long-reply", 20)
	toolDone(t, model, "long-reply")
	h.device.want(t, "tts")
	frames := 0
	for {
		ev := h.device.read(t)
		if ev.kind == websocket.BinaryMessage {
			frames++
		} else if ev.body["type"] == "tts" && ev.body["state"] == "stop" {
			break
		} else {
			t.Fatalf("unexpected playback event: %+v", ev)
		}
	}
	if frames != 20 {
		t.Fatalf("playback ended after %d frames, want 20", frames)
	}
	send := silenceSender(t, h.device)
	if idleTraffic(t, h.device, testIdleTimeout/2, send) {
		t.Fatal("idle window was not restarted after the assistant finished")
	}
	if !idleTraffic(t, h.device, 2*testIdleTimeout, send) {
		t.Fatal("post-reply silence did not close the conversation")
	}
}
