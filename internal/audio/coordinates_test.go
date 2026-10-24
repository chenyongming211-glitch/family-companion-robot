package audio

import "testing"

func TestRateSpecificDecoderAndLookahead(t *testing.T) {
	enc, err := NewEncoderAtRate(24000)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	lookahead, err := enc.LookaheadSamples()
	if err != nil || lookahead <= 0 || lookahead >= 1440 {
		t.Fatalf("lookahead=%d err=%v", lookahead, err)
	}
	packet, err := enc.Encode(make([]byte, FrameBytes))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewDecoderAtRate(24000)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	pcm, err := dec.Decode(packet)
	if err != nil || len(pcm) != FrameBytes {
		t.Fatalf("24k decode bytes=%d err=%v", len(pcm), err)
	}
	if _, err := NewDecoderAtRate(44100); err == nil {
		t.Fatal("accepted unsupported rate")
	}
	enc.Close()
	if _, err := enc.LookaheadSamples(); err == nil {
		t.Fatal("closed encoder returned lookahead")
	}
}
