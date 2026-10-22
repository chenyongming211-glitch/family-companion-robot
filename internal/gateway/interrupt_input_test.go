package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"familyrobot/internal/audio"
	"github.com/gorilla/websocket"
)

// Qwen can send response.done/cancelled before speech_started. The reconnect
// must retain the same uncommitted prefix regardless of which event arrives first.
func TestInterruptedInputReplayAcrossCancellationOrders(t *testing.T) {
	for _, tc := range []struct {
		name, mode, trigger string
		prefixFrames        uint32
		replay              bool
	}{
		{"cancel-before-vad-realtime", "realtime", "cancelled", 2, true},
		{"cancel-before-vad-auto", "auto", "cancelled", 2, true},
		{"cancel-bounded-replay", "realtime", "cancelled", 36, true},
		{"vad-before-cancel", "realtime", "vad", 2, true},
		{"failed-response", "realtime", "failed", 2, false},
		{"manual-cancel", "manual", "cancelled", 2, false},
		{"explicit-device-abort", "realtime", "abort", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newToolHarness(t, false)
			old, _ := h.model(t)
			h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": tc.mode})
			if tc.mode != "auto" {
				old.want(t, "session.update")
			}
			encoder, err := audio.NewEncoderAtRate(16000)
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			send := func(seed uint32) {
				// A deterministic noise pattern makes the two Opus-decoded inputs
				// distinguishable; silence cannot detect the loss of a real prefix.
				pcm := make([]byte, 1920)
				for i := 0; i < len(pcm); i += 2 {
					seed = 1664525*seed + 1013904223
					binary.LittleEndian.PutUint16(pcm[i:], uint16(seed>>16))
				}
				packet, err := encoder.Encode(pcm)
				if err != nil {
					t.Fatal(err)
				}
				if err := h.device.c.WriteMessage(websocket.BinaryMessage, packet); err != nil {
					t.Fatal(err)
				}
			}
			readAudio := func(p *toolPeer) []byte {
				v := p.want(t, "input_audio_buffer.append")
				pcm, err := base64.StdEncoding.DecodeString(v["audio"].(string))
				if err != nil {
					t.Fatal(err)
				}
				return pcm
			}
			// An earlier committed request must not be replayed into the new turn.
			send(123)
			readAudio(old)
			old.send(t, map[string]any{"type": "input_audio_buffer.committed"})
			old.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "count to fifty"})
			h.device.want(t, "stt")
			toolParent(t, old, "old-reply", 500)
			if start := h.device.want(t, "tts"); start["state"] != "start" {
				t.Fatal(start)
			}
			var prefix []byte
			for i := uint32(0); i < tc.prefixFrames; i++ {
				send(17 + i)
				prefix = append(prefix, readAudio(old)...)
			}
			// The existing replay limit is two seconds of 16kHz mono16.
			if len(prefix) > 64000 {
				prefix = prefix[len(prefix)-64000:]
			}
			switch tc.trigger {
			case "vad":
				old.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
			case "abort":
				h.device.send(t, map[string]any{"type": "abort"})
			default:
				old.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "old-reply", "status": tc.trigger}})
			}
			for {
				ev := h.device.read(t)
				if ev.kind == websocket.BinaryMessage {
					continue // Audio sent before the stop is allowed.
				}
				if ev.body["type"] != "tts" || ev.body["state"] != "stop" || ev.body["reason"] != "interrupted" {
					t.Fatalf("expected interrupted stop before reconnect handshake: %+v", ev)
				}
				break
			}
			fresh, _ := h.model(t) // Handshake was deliberately held until after stop.
			send(999)
			first := readAudio(fresh)
			if tc.replay {
				if !bytes.Equal(first, prefix) {
					t.Fatal("reconnect lost the uncommitted speech prefix when cancellation arrived before VAD")
				}
				if bytes.Equal(readAudio(fresh), prefix) {
					t.Fatal("reconnect duplicated the prefix instead of forwarding the next input frame")
				}
			} else if len(first) != 1920 || bytes.Equal(first, prefix) {
				t.Fatal("an explicit abort, failed response or manual cancellation replayed abandoned input")
			}
		})
	}
}
