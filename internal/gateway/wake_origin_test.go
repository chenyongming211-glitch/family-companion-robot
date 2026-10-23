package gateway

import (
	"testing"
	"time"
)

func TestWakeOriginSurvivesLateTranscriptionAndBusinessCommit(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.generation = 3
	s.wake.inputID = "wake"
	s.currentInput = requestOrigin{generation: 3, itemID: "wake", wake: true}
	s.inputCommitted = true
	s.responseOrigin = s.currentInput
	if err := s.beginPlayback(); err != nil {
		t.Fatal(err)
	}
	s.started = true
	s.commitOrigin("business")
	s.transcribeOrigin("wake", "你好，小志。")
	if !s.activePlayback.origin.wake || !s.activePlayback.origin.known {
		t.Fatal("late STT changed source")
	}
	if s.currentInput.itemID != "business" || s.currentInput.wake {
		t.Fatal("late wake STT stole business identity")
	}
	s.pausePlayback()
	if s.paused != nil {
		t.Fatal("greeting became a task")
	}
	s.transcribeOrigin("business", "你好小智，请从一数到五十")
	s.responseOrigin = s.currentInput
	if err := s.beginPlayback(); err != nil {
		t.Fatal(err)
	}
	s.pausePlayback()
	if s.paused == nil || s.paused.playback.origin.wake {
		t.Fatal("business request excluded")
	}
}
func TestWakeQueueAndDeadlineAreBounded(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.wake.deadline = time.Now().Add(time.Second)
	data := make([]byte, maxWakeInputBytes)
	if err := s.receiveInput(data); err != nil {
		t.Fatal(err)
	}
	if err := s.receiveInput([]byte{0, 0}); err == nil {
		t.Fatal("accepted business prefix exceeding replay capacity")
	}
	if len(s.wake.queued) != maxWakeInputBytes || s.inputBytes != 0 || len(s.inputTail) != 0 {
		t.Fatal("wake ACK queue corrupted committed/replay accounting")
	}
}
func TestWakeProvenanceDoesNotRequireTranscription(t *testing.T) {
	if !(&session{}).wakeOnly(requestOrigin{wake: true}) {
		t.Fatal("ASR availability used as source")
	}
	if (&session{}).wakeOnly(requestOrigin{text: "你好小智", known: true}) {
		t.Fatal("text used as provenance")
	}
}
