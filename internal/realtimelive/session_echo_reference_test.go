package realtimelive

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"familyrobot/internal/realtimeaudio"
)

// A real device reply calls release after encoding and feeds device I²S
// receipts back separately. Exercise both boundaries and never substitute sent.
func TestEchoReferenceUsesOnlyConfirmedAudioFromCurrentCapture(t *testing.T) {
	s, c := newPCMSession(t)
	rt := s.addTurn()
	rt.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	rt.asr = &OutputASR{queue: make(chan []byte, 72), Done: make(chan struct{}), cancel: func() {}}
	defer close(rt.asr.Done)
	convert, wantConvert := realtimeaudio.NewResampler(), realtimeaudio.NewResampler()
	var all []byte
	var p *pendingInput
	const totalFrames = 70
	for frame := 1; frame <= totalFrames; frame++ {
		packet := make([]byte, 2880)
		for j := 0; j < 1440; j++ {
			binary.LittleEndian.PutUint16(packet[2*j:], uint16(frame*71+j))
		}
		ok, e := s.release(rt, packet, nil, convert)
		if !ok || e != nil {
			t.Fatal("wire release", e)
		}
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatal(err)
		}
		expected, e := wantConvert.Process(packet)
		if e != nil {
			t.Fatal(e)
		}
		all = append(all, expected...)
		if frame <= 62 {
			mediaReceipt(t, s, rt, int64(frame*1440), "progress")
			if frame == 52 {
				p = &pendingInput{overlapped: true, outputAtStart: rt, ended: true, pcm: make([]byte, 16000*2)}
				p.observePlayback(rt, time.Now())
			}
		}
	}
	// 62 confirmed frames; the latest eight were sent but never acknowledged.
	// A stale or unrelated capture must not borrow any part of this reply.
	got := s.capturedEchoReferenceLocked(p)
	// The field trace reported VAD start 560 ms late. That earlier audio
	// must still be eligible alongside a normal device receipt interval.
	start := 52*960 - 12000
	if !bytes.Equal(got, all[2*start:2*62*960]) {
		t.Fatalf("wrong confirmed 16 kHz window, bytes=%d", len(got))
	}
	got[0] ^= 0xff
	if !bytes.Equal(s.capturedEchoReferenceLocked(p), all[2*start:2*62*960]) {
		t.Fatal("returned snapshot aliases playback buffer")
	}
	p.ended = false
	if s.capturedEchoReferenceLocked(p) != nil {
		t.Fatal("unfinished capture supplied an acoustic comparison")
	}
	p.ended = true
	// A delayed final may have more played audio available than can possibly
	// belong to this capture. Clip it at the capture length plus timing margin.
	p.overlapSamples = 32 * 1440
	if got := s.capturedEchoReferenceLocked(p); !bytes.Equal(got, all[(totalFrames*960-48000)*2:(32*960+16000+8000)*2]) {
		t.Fatalf("reference continued beyond the captured utterance: %d bytes", len(got))
	}
	saved := rt.lastPlaybackProgress
	rt.lastPlaybackProgress = saved.Add(-time.Second)
	if s.capturedEchoReferenceLocked(p) != nil {
		t.Fatal("stale receipt supplied a reference")
	}
	rt.lastPlaybackProgress = saved
	// A new turn on the same device cannot supply the previous turn's reference.
	next := s.addTurn()
	if s.capturedEchoReferenceLocked(p) != nil || s.echoPCM != nil {
		t.Fatal("old reference survived turn replacement")
	}
	// A late finalized receipt for the previous turn must not clear the next buffer.
	next.turn.Begun = true
	next.turn.Sent = 1440
	s.rememberEchoPCMLocked(next, make([]byte, 1920))
	if s.echoPCM == nil {
		t.Fatal("current turn did not start its reference")
	}
	if e := s.send(map[string]any{"type": "end", "id": rt.turn.ID}); e != nil {
		t.Fatal(e)
	}
	rt.turn.Ended = true
	mediaReceipt(t, s, rt, totalFrames*1440, "finished")
	if s.echoPCM == nil {
		t.Fatal("late finished receipt discarded new turn's reference")
	}
}

func TestEchoReferenceReleasedAfterStopAndBoundedForLongAnswers(t *testing.T) {
	for _, finish := range []bool{true, false} {
		s, c := newPCMSession(t)
		rt := s.addTurn()
		rt.turn.Begun = true
		if e := s.send(map[string]any{"type": "begin", "id": rt.turn.ID}); e != nil {
			t.Fatal(e)
		}
		if _, _, e := c.ReadMessage(); e != nil {
			t.Fatal(e)
		}
		for frame := 0; frame < 70; frame++ {
			ok, e := s.binary(rt, make([]byte, 2880))
			if !ok || e != nil {
				t.Fatal(e)
			}
			if _, _, e := c.ReadMessage(); e != nil {
				t.Fatal(e)
			}
			s.rememberEchoPCMLocked(rt, make([]byte, 1920))
		}
		if s.echoPCM == nil || s.echoPCM.Slice(0, 1) != nil || len(s.echoPCM.Slice(s.echoPCM.End-48000, s.echoPCM.End)) != 96000 {
			t.Fatal("three-second bound")
		}
		if finish {
			rt.turn.Ended = true
			if e := s.send(map[string]any{"type": "end", "id": rt.turn.ID}); e != nil {
				t.Fatal(e)
			}
			mediaReceipt(t, s, rt, rt.turn.Sent, "finished")
		} else {
			if e := s.interruptFrom(rt, "device_abort"); e != nil {
				t.Fatal(e)
			}
		}
		if s.echoPCM != nil {
			t.Fatal("completed or interrupted turn retained PCM")
		}
	}
}
