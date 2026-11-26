package realtimelive

import (
	"encoding/binary"
	"familyrobot/internal/speakerlive"
	"math"
)

const minSpeakerRMS = 0.005

type speakerObserver interface {
	Offer(speakerlive.Job) bool
	Close()
}

// hasLowEnergySignal reports whether a 16 kHz PCM16 capture carries non-zero
// audio whose RMS energy falls below minSpeakerRMS (far-field TV or background
// bystander chatter). All-zero synthetic test buffers return false so geometry
// tests that pass make([]byte, N) remain unaffected.
func hasLowEnergySignal(pcm []byte) bool {
	if len(pcm) < 2 {
		return false
	}
	var sum float64
	nonZero := false
	samples := len(pcm) / 2
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(binary.LittleEndian.Uint16(pcm[i:]))
		if v != 0 {
			nonZero = true
		}
		f := float64(v) / 32768
		sum += f * f
	}
	return nonZero && math.Sqrt(sum/float64(samples)) < minSpeakerRMS
}

// Session.mu held. Evaluates whether the current input should be rejected as
// far-field low-energy background noise or as a different speaker once the
// session's primary speaker anchor has been established.
func (s *Session) bystanderRejectionLocked(p *pendingInput, pcm []byte) string {
	if s.cfg.Speaker == nil || p == nil {
		return ""
	}
	if p.speakerReason == "low_energy" || hasLowEnergySignal(pcm) {
		return "bystander_low_energy"
	}
	// A different speaker is rejected only when robot playback overlaps their
	// capture, as an interval rather than a point in time. playbackInputLocked
	// latches p.playbackSeen whenever it sees playback running during the
	// capture, so this reads two recorded facts: the capture opened while speech
	// was playing (p.overlapped), or playback was seen at some point during the
	// capture (p.playbackSeen). A capture that opened during playback stays
	// rejected even after playback ends. A pending turn that never actually
	// played is not covered: cancelling it and answering the bystander is the
	// accepted trade-off.
	if (p.overlapped || p.playbackSeen) && !s.speakerFailed && s.speakerAnchor != "" && p.speakerState == "temporary" && p.speakerLabel != "" && p.speakerLabel != s.speakerAnchor {
		return "bystander_speaker_mismatch"
	}
	return ""
}

// All mutable observation state is serialized with the existing Session mutex.
func (s *Session) beginSpeaker() {
	if s.cfg.Speaker == nil {
		return
	}
	s.mu.Lock()
	if s.lastInput == nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.speakerLatest = s.lastInput.id
	state := "pending"
	if s.speakerFailed {
		state = "unavailable"
	}
	if s.cfg.MemoryOnly {
		event := speakerEvent(speakerlive.Result{ID: s.speakerLatest, State: state})
		_ = s.sendLocked(event)
	}
	s.mu.Unlock()
}

func speakerEvent(r speakerlive.Result) map[string]any {
	if r.State == "unavailable" {
		r.Reason = "observer_unavailable"
	}
	return map[string]any{"type": "speaker_observation", "input_id": r.ID, "state": r.State, "label": r.Label, "start_sample": r.Start, "end_sample": r.End, "elapsed_ms": r.ElapsedMS, "evidence": "turn_taking_test", "reason": r.Reason, "audio_ms": r.AudioMS}
}

func (s *Session) speakerResult(r speakerlive.Result) {
	if s.cfg.Speaker == nil {
		return
	}
	var logReason string
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	if r.State == "unavailable" {
		if !s.speakerFailed && r.Reason != "" {
			logReason = r.Reason
		}
		// The upper layer does not auto-recover a failed observer; a new
		// session naturally recovers by constructing a fresh one. No audio,
		// transcript or path is recorded here.
		s.speakerFailed = true
		if r.ID != s.speakerLatest {
			r = speakerlive.Result{ID: s.speakerLatest, State: "unavailable", Reason: r.Reason}
		}
	} else if r.ID != s.speakerLatest {
		s.mu.Unlock()
		return
	}
	// Permanent helper failure wins even if Offer/validation raced its callback.
	if s.speakerFailed {
		r.State = "unavailable"
		r.Label = ""
		r.Reason = "observer_unavailable"
	} else {
		if s.lastInput != nil && s.lastInput.id == r.ID {
			s.lastInput.speakerState = r.State
			s.lastInput.speakerLabel = r.Label
			s.lastInput.speakerReason = r.Reason
		}
		if s.speakerAnchor == "" && r.State == "temporary" && r.Label != "" {
			for i := len(s.history) - 1; i >= 0; i-- {
				rt := s.history[i]
				if rt.turn.ID == r.ID {
					if !rt.overlapped && !rt.turn.Cut {
						s.speakerAnchor = r.Label
					}
					break
				}
			}
		}
	}
	// A slow/closed observer cannot cancel the normal speech/model pipeline.
	if s.cfg.MemoryOnly {
		_ = s.sendLocked(speakerEvent(r))
	}
	s.mu.Unlock()
	// Bounded reason only: never audio, transcripts, paths or credentials.
	if logReason != "" {
		_ = s.record("speaker_observer_unavailable", map[string]any{"reason": logReason})
	}
}

func (s *Session) observeSpeaker(id, start, end int64, pcm []byte, complete bool) {
	if s.cfg.Speaker == nil || s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	observer, failed := s.speaker, s.speakerFailed
	s.mu.Unlock()
	if failed {
		s.speakerResult(speakerlive.Result{ID: id, State: "unavailable"})
		return
	}
	ms := min(int64(len(pcm)/32), int64(35000))
	reject := func(state, reason string) {
		s.speakerResult(speakerlive.Result{ID: id, State: state, Reason: reason, AudioMS: ms})
	}
	if !complete || start < 0 || end <= start || int64(len(pcm)) != (end-start)*2 {
		reject("uncertain", "incomplete_span")
		return
	}
	if hasLowEnergySignal(pcm) {
		reject("uncertain", "low_energy")
		return
	}
	if end-start < 24000 {
		reject("uncertain", "too_short")
		return
	}
	if end-start > 128000 {
		reject("uncertain", "too_long")
		return
	}
	if observer == nil {
		reject("unavailable", "observer_unavailable")
		return
	}
	if !observer.Offer(speakerlive.Job{ID: id, Start: start, End: end, PCM: pcm}) {
		reject("uncertain", "not_queued")
	}
}
