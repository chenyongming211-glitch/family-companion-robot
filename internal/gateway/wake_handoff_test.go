package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"familyrobot/internal/audio"
	"github.com/gorilla/websocket"
	"testing"
	"time"
)

func wakePCM(t *testing.T, h *toolHarness) func() {
	t.Helper()
	enc, e := audio.NewEncoderAtRate(16000)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(enc.Close)
	return func() {
		pcm := bytes.Repeat([]byte{31, 17}, 960)
		packet, e := enc.Encode(pcm)
		if e != nil {
			t.Fatal(e)
		}
		if e = h.device.c.WriteMessage(websocket.BinaryMessage, packet); e != nil {
			t.Fatal(e)
		}
	}
}
func wantPCM(t *testing.T, p *toolPeer) []byte {
	t.Helper()
	b, e := base64.StdEncoding.DecodeString(p.want(t, "input_audio_buffer.append")["audio"].(string))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func beginCachedWake(t *testing.T, h *toolHarness, p *toolPeer) func() {
	t.Helper()
	send := wakePCM(t, h)
	send()
	if p.want(t, "session.update")["session"].(map[string]any)["turn_detection"] != nil {
		t.Fatal("wake cache must be isolated from automatic VAD")
	}
	wantPCM(t, p)
	send()
	wantPCM(t, p)
	h.device.send(t, map[string]any{"type": "listen", "state": "detect", "text": "你好小智"})
	p.want(t, "input_audio_buffer.commit")
	return send
}
func TestWakeHandoffIdentityAndEarlyBusiness(t *testing.T) {
	for _, ackFirst := range []bool{false, true} {
		for _, stt := range []string{"你好，小志。", "你好小知啊", "任意转写"} {
			t.Run(stt+map[bool]string{true: "/ack-first", false: "/start-first"}[ackFirst], func(t *testing.T) {
				h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
				p, _ := h.model(t)
				send := beginCachedWake(t, h, p)
				ack := func() { p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "wake-1"}) }
				if ackFirst {
					ack()
				}
				h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "realtime"})
				send() // Business audio while the explicit wake handoff is pending.
				if !ackFirst {
					ack()
				}
				p.want(t, "response.create")
				p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "wake-1", "transcript": stt})
				wantSTT(t, h.device)
				toolParent(t, p, "greeting", 30)
				if p.want(t, "session.update")["session"].(map[string]any)["turn_detection"] == nil {
					t.Fatal("business VAD not restored")
				}
				queued := wantPCM(t, p)
				if len(queued) != 1920 {
					t.Fatal("queued business prefix lost")
				}
				readStart(t, h.device)
				h.device.send(t, map[string]any{"type": "abort"})
				readStop(t, h.device)
				fresh, _ := h.model(t)

				const task = "你好小智，请从一完整数到五十"
				fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "business"})
				fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "business", "transcript": task})
				wantSTT(t, h.device)
				toolParent(t, fresh, "business-response", 30)
				readStart(t, h.device)
				h.device.send(t, map[string]any{"type": "abort"})
				readStop(t, h.device)
				next, _ := h.model(t)
				if got := localResult(t, next, "real-task", "conversation_get_paused_task", "{}"); got["original_request"] != task {
					t.Fatalf("real instruction lost: %v", got)
				}
			})
		}
	}
}
func TestWakeHandoffNoAudioDetectCannotClaimBusiness(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	h.device.send(t, map[string]any{"type": "listen", "state": "detect", "text": "你好小智"})
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "realtime"})
	p.want(t, "session.update")
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "real"})
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "real", "transcript": "你好小智"})
	wantSTT(t, h.device)
	toolParent(t, p, "real", 20)
	readStart(t, h.device)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	fresh, _ := h.model(t)
	if got := localResult(t, fresh, "get", "conversation_get_paused_task", "{}"); got["available"] != true {
		t.Fatal("metadata alone consumed a real input", got)
	}
}
func TestWakeHandoffDuplicateAckDoesNotRecreateOrFlushTwice(t *testing.T) {
	h := newToolHarness(t, false)
	p, _ := h.model(t)
	send := beginCachedWake(t, h, p)
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "realtime"})
	h.device.send(t, map[string]any{"type": "listen", "state": "detect", "text": "other-label"})
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "wake"})
	p.want(t, "response.create")
	send()
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "wake"})
	toolParent(t, p, "greeting", 1)
	p.want(t, "session.update")
	wantPCM(t, p)
	p.quiet(t, 80*time.Millisecond)
}
func TestWakeHandoffManualStopKeepsBusinessCapture(t *testing.T) {
	h := newToolHarness(t, false)
	p, _ := h.model(t)
	send := beginCachedWake(t, h, p)
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "manual"})
	send()
	send()
	h.device.send(t, map[string]any{"type": "listen", "state": "stop"})
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "wake"})
	// Manual mode does not create an unsolicited greeting over the user's held capture.
	total := 0
	for total < 3840 {
		total += len(wantPCM(t, p))
	}
	if total != 3840 {
		t.Fatal("queued manual utterance truncated")
	}
	p.want(t, "input_audio_buffer.commit")
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "user"})
	p.want(t, "response.create")
}

func TestWakeHandoffMissingAckIdentityFailsClosed(t *testing.T) {
	h := newToolHarness(t, false)
	p, _ := h.model(t)
	beginCachedWake(t, h, p)
	p.send(t, map[string]any{"type": "input_audio_buffer.committed"})
	if h.device.want(t, "alert")["type"] != "alert" {
		t.Fatal("missing identity accepted")
	}
}
func TestWakeHandoffLateSttBeforeAckIsRetained(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	beginCachedWake(t, h, p)
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "wake", "transcript": "你好，小志。"})
	wantSTT(t, h.device)
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "realtime"})
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "wake"})
	p.want(t, "response.create")
	toolParent(t, p, "greeting", 10)
	p.want(t, "session.update")
	readStart(t, h.device)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	fresh, _ := h.model(t)
	if got := localResult(t, fresh, "get", "conversation_get_paused_task", "{}"); got["available"] != false {
		t.Fatal(got)
	}
}
func TestWakeHandoffTimeoutClosesSession(t *testing.T) {
	h := newToolHarness(t, false)
	p, _ := h.model(t)
	beginCachedWake(t, h, p)
	started := time.Now()
	for {
		select {
		case ev := <-h.device.in:
			if ev.err != nil {
				if time.Since(started) < wakeHandoffTimeout-500*time.Millisecond {
					t.Fatal("premature close")
				}
				return
			}
		case <-time.After(wakeHandoffTimeout + 3*time.Second):
			t.Fatal("wake handshake never closed")
		}
	}
}

func TestWakeHandoffNativeInterruptionReplaysCompleteBusinessPrefix(t *testing.T) {
	for _, trigger := range []string{"vad", "cancelled"} {
		t.Run(trigger, func(t *testing.T) {
			h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
			p, _ := h.model(t)
			beginCachedWake(t, h, p)
			enc, e := audio.NewEncoderAtRate(16000)
			if e != nil {
				t.Fatal(e)
			}
			defer enc.Close()
			sendNoise := func(seed uint32) {
				pcm := make([]byte, 1920)
				for i := 0; i < len(pcm); i += 2 {
					seed = 1664525*seed + 1013904223
					binary.LittleEndian.PutUint16(pcm[i:], uint16(seed>>16))
				}
				packet, e := enc.Encode(pcm)
				if e != nil {
					t.Fatal(e)
				}
				if e = h.device.c.WriteMessage(websocket.BinaryMessage, packet); e != nil {
					t.Fatal(e)
				}
			}
			for i := uint32(0); i < 33; i++ {
				sendNoise(i + 17)
			}
			// start is ordered after all business frames on the device socket, so the
			// response cannot be requested until that entire near-limit prefix is queued.
			h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "realtime"})
			p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "wake"})
			p.want(t, "response.create")
			toolParent(t, p, "greeting", 100)
			p.want(t, "session.update")
			prefix := wantPCM(t, p)
			if len(prefix) != 33*1920 {
				t.Fatalf("queued prefix length=%d", len(prefix))
			}
			readStart(t, h.device)
			if trigger == "vad" {
				p.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
			} else {
				p.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "greeting", "status": "cancelled"}})
			}
			readStop(t, h.device)
			fresh, _ := h.model(t)
			if got := wantPCM(t, fresh); !bytes.Equal(got, prefix) {
				t.Fatal("wake handoff lost or duplicated queued business on native interruption")
			}
			sendNoise(991)
			if next := wantPCM(t, fresh); len(next) != 1920 || bytes.Equal(next, prefix) {
				t.Fatal("new input was not forwarded exactly once")
			}
		})
	}
}
