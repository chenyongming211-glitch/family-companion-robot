package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

func TestWakeOnlyInterruptionLeavesSlotForActualTask(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	detect := func(mode string) {
		h.device.send(t, map[string]any{"type": "listen", "state": "detect", "text": "嗨小伴"})
		h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": mode})
		p.want(t, "session.update") // Barrier after device-originated wake metadata.
	}
	detect("realtime")
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "wake-input"})
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "wake-input", "transcript": "嗨，小伴！"})
	wantSTT(t, h.device)
	toolParent(t, p, "greeting", 20)
	readStart(t, h.device)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	fresh, _ := h.model(t)
	const task = "嗨小伴，请完整读这篇文章直到结尾"
	fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "real-input"})
	fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "real-input", "transcript": task})
	wantSTT(t, h.device)
	toolParent(t, fresh, "real-task", 20)
	start := readStart(t, h.device)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	next, _ := h.model(t)
	out := localResult(t, next, "get-real", "conversation_get_paused_task", "{}")
	if out["available"] != true || out["original_request"] != task || out["playback_id"] != start["playback_id"] {
		t.Fatalf("real task was excluded or attached to greeting: %v", out)
	}
}

func TestLateWakeEvidenceReleasesOnlyWakePause(t *testing.T) {
	for _, detectLate := range []bool{false, true} {
		h := newPlaybackHarness(t)
		h.s.responseID = ""
		ctx := context.Background()
		detect := func() {
			data, _ := json.Marshal(map[string]any{"type": "listen", "state": "detect", "text": "嗨小伴"})
			if err := h.s.onDevice(ctx, deviceEvent{kind: websocket.TextMessage, data: data}); err != nil {
				t.Fatal(err)
			}
		}
		if !detectLate {
			detect()
		}
		h.s.commitOrigin("wake")
		if detectLate {
			h.s.transcribeOrigin("wake", "嗨，小伴！")
		}
		if err := h.s.onModel(ctx, qwen.Event{Type: "response.created", Response: qwen.Response{ID: "greeting"}}); err != nil {
			t.Fatal(err)
		}
		if err := h.s.beginPlayback(); err != nil {
			t.Fatal(err)
		}
		h.s.started = true
		h.s.pausePlayback()
		if detectLate {
			detect()
		} else {
			h.s.transcribeOrigin("wake", "嗨，小伴！")
		}
		if h.s.paused != nil {
			t.Fatal("late matching wake evidence retained a wake-only pause")
		}
	}
}
