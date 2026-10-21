package gateway

import (
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/protocol"
)

func (s *session) play() error {
	const frameDuration = 60 * time.Millisecond
	// A ticker stamp may have waited behind model or MCP I/O. Observe the
	// clock when processing playback instead of using that queued stamp.
	now := s.playbackNow()
	if s.responseID == "" || now.Before(s.nextAudio) {
		return nil
	}
	frame := s.pending.Pop(audio.FrameBytes, s.responseDone)
	if frame != nil {
		if !s.started {
			if err := s.tts("start", ""); err != nil {
				return err
			}
			s.started = true
			if s.transcript != "" {
				if err := s.tts("sentence_start", s.transcript); err != nil {
					return err
				}
			}
		}
		packet, err := s.encoder.Encode(frame)
		if err != nil {
			return err
		}
		wrapped, err := protocol.Wrap(s.version, packet, 0)
		if err != nil {
			return err
		}
		if err := s.writeAudio(wrapped); err != nil {
			return err
		}
		s.lastAudio = s.playbackNow()
		s.lastActivity = time.Now()
		// Preserve the original cadence through tick jitter. A full-frame
		// stall, including encoding or writing, starts a fresh timeline
		// from successful delivery instead of rapidly catching up.
		if s.nextAudio.IsZero() || s.lastAudio.Sub(s.nextAudio) >= frameDuration {
			s.nextAudio = s.lastAudio
		}
		s.nextAudio = s.nextAudio.Add(frameDuration)
		return nil
	}
	// Missing or incomplete PCM ends this timeline; resume at the next frame.
	s.nextAudio = time.Time{}
	if s.responseDone && s.pending.Len() == 0 {
		// Cadence deadlines may precede the actual write after tick jitter.
		// Let the final packet play for its full duration before stopping.
		if !s.lastAudio.IsZero() && now.Before(s.lastAudio.Add(frameDuration)) {
			return nil
		}
		// Keep a tool parent and its continuation in one firmware playback block.
		// An intermediate stop causes firmware to auto-send listen.start.
		s.toolPlayback = s.started && len(s.toolCalls) > 0
		if s.started && !s.toolPlayback {
			if err := s.tts("stop", ""); err != nil {
				return err
			}
		}
		// Transport delivery is known; actual hardware playback has no ACK.
		s.hist.add("assistant", s.transcript)
		s.hist.Interrupted = false
		s.responseID = ""
		if !s.toolPlayback {
			s.started = false
		}
		s.responseDone = false
		s.transcript = ""
		s.lastAudio = time.Time{}
		s.lastActivity = time.Now()
	}
	return nil
}
