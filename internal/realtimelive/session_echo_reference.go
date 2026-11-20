package realtimelive

import (
	"familyrobot/internal/realtimestate"
	"time"
)

// The device reports 24 kHz samples. The microphone and the decoded playback
// reference both use 16 kHz PCM16. Keep three seconds per active session.
const echoReferenceSamples = 3 * 16000

func (s *Session) rememberEchoPCMLocked(rt *liveTurn, pcm []byte) {
	if rt != s.output || !s.speechPlayingLocked() {
		return
	}
	start := (rt.turn.Sent - realtimestate.PacketSamples) * 2 / 3
	if start < 0 || len(pcm) != int(realtimestate.PacketSamples*2/3)*2 {
		s.echoPCM = nil // Never join noncontiguous or partial audio.
		return
	}
	if s.echoPCM == nil || s.echoPCM.End != start {
		s.echoPCM = realtimestate.NewAudioRing(echoReferenceSamples)
		s.echoPCM.End = start
	}
	_ = s.echoPCM.Append(pcm) // The aligned frame size and capacity are fixed above.
}

// Return an owned copy near the input capture, never audio merely queued for
// the device. The first hardware receipt can come after the microphone onset:
// VAD reported up to 560 ms of prior audio in the observed capture.
func (s *Session) capturedEchoReferenceLocked(p *pendingInput) []byte {
	rt := s.output
	if p == nil || p.retired || !p.ended || !p.overlapped || p.outputAtStart != rt || p.overlapSamples <= 0 ||
		s.echoPCM == nil || !s.speechPlayingLocked() || !freshPlaybackProgress(rt, time.Now()) ||
		p.overlapSamples > rt.turn.Completed {
		return nil
	}
	captured := int64(len(p.pcm) / 2)
	if captured <= 0 {
		return nil
	}
	const before = 12000 // 750 ms including VAD lookback and receipt timing.
	const after = 8000   // 500 ms from the end estimated by the microphone length.
	anchor := p.overlapSamples * 2 / 3
	start := max(int64(0), anchor-before, s.echoPCM.End-echoReferenceSamples)
	end := min(rt.turn.Completed*2/3, anchor+captured+after, s.echoPCM.End)
	return s.echoPCM.Slice(start, end)
}
