package realtimelive

import "familyrobot/internal/speakerlive"

type speakerObserver interface {
	Offer(speakerlive.Job) bool
	Close()
}

// Candidates are experimental observations, never authorization or model facts.
// All mutable observation state is serialized with the existing Session mutex.
func (s *Session) beginSpeaker() {
	if s.cfg.Speaker == nil {
		return
	}
	s.mu.Lock()
	s.speakerLatest = s.seq + 1
	state := "pending"
	if s.speakerFailed {
		state = "unavailable"
	}
	event := speakerEvent(speakerlive.Result{ID: s.speakerLatest, State: state})
	_ = s.sendLocked(event)
	s.mu.Unlock()
}
func speakerEvent(r speakerlive.Result) map[string]any {
	return map[string]any{"type": "speaker_observation", "input_id": r.ID, "state": r.State, "label": r.Label, "start_sample": r.Start, "end_sample": r.End, "elapsed_ms": r.ElapsedMS, "evidence": "turn_taking_test"}
}
func (s *Session) speakerResult(r speakerlive.Result) {
	if s.cfg.Speaker == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	if r.State == "unavailable" {
		s.speakerFailed = true
		if r.ID != s.speakerLatest {
			r = speakerlive.Result{ID: s.speakerLatest, State: "unavailable"}
		}
	} else if r.ID != s.speakerLatest {
		return
	}
	// Permanent helper failure wins even if Offer/validation raced its callback.
	if s.speakerFailed {
		r.State = "unavailable"
		r.Label = ""
	}
	// A slow/closed observer cannot cancel the normal speech/model pipeline.
	_ = s.sendLocked(speakerEvent(r))
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
	if !complete || start < 0 || end <= start || end-start < 24000 || end-start > 128000 || int64(len(pcm)) != (end-start)*2 {
		s.speakerResult(speakerlive.Result{ID: id, State: "uncertain"})
		return
	}
	if observer == nil {
		s.speakerResult(speakerlive.Result{ID: id, State: "unavailable"})
		return
	}
	if !observer.Offer(speakerlive.Job{ID: id, Start: start, End: end, PCM: pcm}) {
		s.speakerResult(speakerlive.Result{ID: id, State: "uncertain"})
	}
}
