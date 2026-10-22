package gateway

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
)

func TestLongModelAudioBurstDrainsAtDeviceCadence(t *testing.T) {
	h := newPlaybackHarness(t)
	// A fast model produces 30 seconds before the device has played any.
	// Exercise the actual event handler, Opus encoder and WebSocket output.
	const frames = 500
	delta := base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes*10))
	for i := 0; i < frames/10; i++ {
		if err := h.s.onModel(context.Background(), qwen.Event{
			Type: "response.audio.delta", ResponseID: "test-response", Delta: delta,
		}); err != nil {
			t.Fatalf("30-second model burst rejected after %d frames: %v", i*10, err)
		}
	}
	if err := h.s.onModel(context.Background(), qwen.Event{
		Type: "response.done", Response: qwen.Response{ID: "test-response", Status: "completed"},
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Unix(100, 0)
	for i := 0; i < frames; i++ {
		want := []string{"audio"}
		if i == 0 {
			want = []string{"start", "audio"}
		}
		expectPlayback(t, h.play(t, start.Add(time.Duration(i)*60*time.Millisecond)), want...)
		if h.s.responseID == "" {
			t.Fatalf("reply ended at frame %d of %d", i, frames)
		}
	}
	expectPlayback(t, h.play(t, start.Add(30*time.Second-time.Nanosecond)))
	expectPlayback(t, h.play(t, start.Add(30*time.Second)), "stop")
	if h.s.pending.Len() != 0 || h.s.responseID != "" {
		t.Fatal("completed long reply retained playback state")
	}
}

func TestModelAudioBurstRemainsBounded(t *testing.T) {
	h := newPlaybackHarness(t)
	delta := base64.StdEncoding.EncodeToString(make([]byte, 24000*2*12))
	for i := 0; i < 10; i++ {
		if err := h.s.onModel(context.Background(), qwen.Event{
			Type: "response.audio.delta", ResponseID: "test-response", Delta: delta,
		}); err != nil {
			t.Fatalf("bounded 120-second burst rejected at chunk %d: %v", i, err)
		}
	}
	before := h.s.pending.Len()
	if err := h.s.onModel(context.Background(), qwen.Event{
		Type: "response.audio.delta", ResponseID: "test-response",
		Delta: base64.StdEncoding.EncodeToString([]byte{1, 2}),
	}); err == nil {
		t.Fatal("accepted audio beyond the finite 120-second pending limit")
	}
	if h.s.pending.Len() != before {
		t.Fatal("overflow changed previously accepted audio")
	}
}

func TestInterruptDiscardsLongModelBurst(t *testing.T) {
	for _, source := range []string{"device", "model"} {
		t.Run(source, func(t *testing.T) { testInterrupt(t, source, 500) })
	}
}
