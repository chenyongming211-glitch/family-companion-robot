package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
)

// Same scenarios as Qwen's sealed output queue, through our real event handler,
// PCM buffer, Opus encoder and WebSocket transport. No hardware ACK is simulated.
func TestSealedReplyRejectsLateOutput(t *testing.T) {
	h := newPlaybackHarness(t)
	h.append(t, make([]byte, audio.FrameBytes))
	h.s.transcript, h.s.draftTranscript = "原回复", "原回复"
	done := qwen.Event{Type: "response.done", Response: qwen.Response{ID: "test-response", Status: "completed"}}
	if err := h.s.onModel(context.Background(), done); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []qwen.Event{
		{Type: "response.audio.delta", ResponseID: "test-response", Delta: base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))},
		{Type: "response.audio_transcript.delta", ResponseID: "test-response", Delta: "迟到"},
		{Type: "response.audio_transcript.done", ResponseID: "test-response", Transcript: "迟到全文"},
	} {
		if err := h.s.onModel(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	if h.s.pending.Len() != audio.FrameBytes || h.s.transcript != "原回复" || h.s.draftTranscript != "原回复" {
		t.Fatal("late output changed a sealed reply")
	}
	start := time.Unix(100, 0)
	expectPlayback(t, h.play(t, start), "start", "sentence_start", "audio")
	expectPlayback(t, h.play(t, start.Add(60*time.Millisecond)), "stop")
}

func TestSealedReplyIgnoresContradictoryDuplicateDone(t *testing.T) {
	h := newPlaybackHarness(t)
	h.s.responseDone = true
	h.append(t, make([]byte, audio.FrameBytes))
	// A cancellation replay cannot invalidate already sealed output. A new VAD
	// speech event still follows the existing interruption path independently.
	if err := h.s.onModel(context.Background(), qwen.Event{Type: "response.done", Response: qwen.Response{ID: "test-response", Status: "cancelled"}}); err != nil {
		t.Fatal(err)
	}
	if !h.s.responseDone || h.s.pending.Len() != audio.FrameBytes {
		t.Fatal("duplicate done invalidated sealed output")
	}
}

func TestRetiredReplyCannotReopenPlayback(t *testing.T) {
	h := newPlaybackHarness(t)
	h.append(t, make([]byte, audio.FrameBytes))
	h.s.responseDone = true
	start := time.Unix(100, 0)
	expectPlayback(t, h.play(t, start), "start", "audio")
	expectPlayback(t, h.play(t, start.Add(60*time.Millisecond)), "stop")
	for _, ev := range []qwen.Event{
		{Type: "response.created", Response: qwen.Response{ID: "test-response"}},
		{Type: "response.audio.delta", ResponseID: "test-response", Delta: base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))},
		{Type: "response.done", Response: qwen.Response{ID: "test-response", Status: "completed"}},
	} {
		if err := h.s.onModel(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	if h.s.responseID != "" || h.s.pending.Len() != 0 {
		t.Fatal("retired response reopened playback")
	}
	expectPlayback(t, h.play(t, start.Add(time.Second)))
	// The tombstone must not suppress a genuinely new reply.
	if err := h.s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "new-response"}}); err != nil {
		t.Fatal(err)
	}
	if h.s.responseID != "new-response" {
		t.Fatal("new response rejected")
	}
}

func TestTransportHistoryDoesNotClaimHardwarePlayback(t *testing.T) {
	h := newPlaybackHarness(t)
	h.s.transcript = "完整生成的回复"
	h.s.responseDone = true
	h.append(t, make([]byte, audio.FrameBytes))
	start := time.Unix(100, 0)
	expectPlayback(t, h.play(t, start), "start", "sentence_start", "audio")
	expectPlayback(t, h.play(t, start.Add(60*time.Millisecond)), "stop")
	if len(h.s.hist.Turns) != 1 {
		t.Fatal("missing transport history")
	}
	turn := h.s.hist.Turns[0]
	if turn.Delivery != "transport_sent" || turn.Text != "" || turn.GeneratedText != "完整生成的回复" {
		t.Fatalf("transport was recorded as heard words: %+v", turn)
	}
}

func TestRetiredReplyWindowIsBounded(t *testing.T) {
	h := newPlaybackHarness(t)
	// Empty sealed outputs still consume a completion identity, without making
	// an audible block. Exercise bounded eviction through normal finalization.
	for i := 0; i < 300; i++ {
		h.s.responseID = fmt.Sprintf("reply-%d", i)
		h.s.responseDone = true
		expectPlayback(t, h.play(t, time.Unix(int64(100+i), 0)))
	}
	// The recent window is protected while its oldest entries can expire.
	if err := h.s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "reply-299"}}); err != nil {
		t.Fatal(err)
	}
	if h.s.responseID != "" {
		t.Fatal("recent retired identity reopened")
	}
	if err := h.s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "reply-0"}}); err != nil {
		t.Fatal(err)
	}
	if h.s.responseID != "reply-0" {
		t.Fatal("retirement window grew without eviction")
	}
}
