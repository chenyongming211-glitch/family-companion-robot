package gateway

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
)

func TestNewResponseResetsEncoderForDeviceDecoderReset(t *testing.T) {
	enc, _ := audio.NewEncoder()
	s := session{encoder: enc}
	defer func() { s.encoder.Close() }()
	prior := bytes.Repeat([]byte{0, 32}, audio.FrameBytes/2)
	enc.Encode(prior)
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "next"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.encoder.Encode(make([]byte, audio.FrameBytes))
	fresh, _ := audio.NewEncoder()
	defer fresh.Close()
	expected, _ := fresh.Encode(make([]byte, audio.FrameBytes))
	if !bytes.Equal(got, expected) {
		t.Fatal("new response encoder retained previous audio state")
	}
}

func TestHistoryIsBoundedAndMarksLatestInterruption(t *testing.T) {
	var h history
	for i := 0; i < 20; i++ {
		h.add("user", strings.Repeat("字", 1000))
	}
	if len(h.Turns) != 12 || len([]rune(h.Turns[0].Text)) != 512 {
		t.Fatal("unbounded history")
	}
	h.Interrupted = true
	if !strings.Contains(h.context(), `"previous_reply_interrupted":true`) {
		t.Fatal("lost interrupted state")
	}
}
