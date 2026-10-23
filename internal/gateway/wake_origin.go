package gateway

import (
	"errors"
	"time"
)

// Only the pre-listen device cache can open this handshake. It is committed
// under manual segmentation before business audio enters the native buffer.
// No ASR spelling, wake label, or first-user-turn guess establishes provenance.
const maxWakeInputBytes = 64000 // Same bound as the interruption replay tail.
const wakeHandoffTimeout = 8 * time.Second

type wakeHandoff struct {
	manual                         bool
	commitPending, responsePending bool
	inputID                        string
	origin                         requestOrigin
	queued                         []byte
	stopPending                    bool
	deadline                       time.Time
}

func (s *session) wakeOnly(origin requestOrigin) bool { return origin.wake }

func (s *session) forwardInput(pcm []byte) error {
	if s.inputActive {
		s.lastActivity = time.Now()
	}
	s.inputBytes += len(pcm)
	s.inputTail = append(s.inputTail, pcm...)
	if len(s.inputTail) > maxWakeInputBytes {
		s.inputTail = append([]byte(nil), s.inputTail[len(s.inputTail)-maxWakeInputBytes:]...)
	}
	return s.appendAudio(pcm)
}
func (s *session) receiveInput(pcm []byte) error {
	if !s.wake.deadline.IsZero() {
		if len(s.wake.queued)+len(pcm) > maxWakeInputBytes {
			return errors.New("wake business input exceeded replay bound")
		}
		s.wake.queued = append(s.wake.queued, pcm...)
		return nil
	}
	if !s.listenStarted {
		if s.inputBytes+len(pcm) > maxWakeInputBytes {
			return errors.New("pre-listen cache exceeded limit")
		}
		if !s.wake.manual {
			if err := s.model.SetMode("manual"); err != nil {
				return err
			}
			s.wake.manual = true
		}
	}
	return s.forwardInput(pcm)
}
func (s *session) detectWake() error {
	if s.listenStarted || !s.wake.manual || !s.wake.deadline.IsZero() || s.wake.inputID != "" {
		return nil
	}
	if s.inputBytes < 3200 {
		return errors.New("wake cache too short to commit")
	}
	s.wake.commitPending = true
	s.wake.deadline = time.Now().Add(wakeHandoffTimeout)
	return s.model.Send(map[string]any{"type": "input_audio_buffer.commit"})
}

// Wait for both the explicit commit ACK and the first device start. In manual
// mode user capture takes precedence; an unsolicited greeting would overlap it.
func (s *session) advanceWake() error {
	if s.wake.commitPending || !s.listenStarted || s.wake.deadline.IsZero() || s.wake.responsePending {
		return nil
	}
	if s.mode == "manual" {
		s.awaitingResponse = false
		return s.finishWake()
	}
	s.wake.responsePending = true
	s.wake.origin = s.currentInput
	s.awaitingResponse = true
	return s.model.Send(map[string]any{"type": "response.create"})
}
func (s *session) finishWake() error {
	queued, stop := s.wake.queued, s.wake.stopPending
	s.wake.queued = nil
	s.wake.stopPending = false
	s.wake.deadline = time.Time{}
	if s.mode != "manual" {
		if err := s.model.SetMode(s.mode); err != nil {
			return err
		}
	}
	s.wake.manual = false
	if len(queued) > 0 {
		if err := s.forwardInput(queued); err != nil {
			return err
		}
	}
	if stop {
		return s.stopInput()
	}
	return nil
}
func (s *session) stopInput() error {
	if s.inputActive {
		s.lastActivity = time.Now()
	}
	s.inputActive = false
	if !s.wake.deadline.IsZero() {
		s.wake.stopPending = true
		return nil
	}
	if s.mode == "manual" && s.inputBytes >= 3200 && !s.manualCommit {
		s.manualCommit = true
		s.awaitingResponse = true
		return s.model.Send(map[string]any{"type": "input_audio_buffer.commit"})
	}
	return nil
}
