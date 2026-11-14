package realtimelive

import (
	"time"
	"unicode"
)

// Nine runes are enough to reject inputs over eight and to compare the bounded
// opening of generated text. Generated text is only an echo suspicion reference,
// never evidence that those words were played or a source of memory evidence.
func echoOpeningRunes(text string) []rune {
	opening := make([]rune, 0, 9)
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		opening = append(opening, unicode.ToLower(r))
		if len(opening) == 9 {
			break
		}
	}
	return opening
}

// Session.mu owns the input binding, playback ledger and progress timestamp.
func (s *Session) suspectedOpeningEchoLocked(p *pendingInput, text string, now time.Time) bool {
	if p == nil || p.hadPreview || s.ownerErr != nil || s.rec.owner.ClientType != "xiaozhi" || !diagnosticDevicePattern.MatchString(s.rec.owner.DeviceID) {
		return false
	}
	if _, device := s.wire.(*xiaozhiWire); !device {
		return false
	}
	rt := s.output
	if rt == nil || rt != p.outputAtStart || rt.media || !s.activeOutputLocked() || rt.turn.PlaybackBasis != "i2s_dma" || rt.turn.Completed <= 0 || rt.turn.Completed > 36000 || rt.lastPlaybackProgress.IsZero() {
		return false
	}
	age := now.Sub(rt.lastPlaybackProgress)
	if age < 0 || age > 500*time.Millisecond {
		return false
	}
	input, opening := echoOpeningRunes(text), echoOpeningRunes(rt.turn.Generated)
	if len(input) == 0 || len(input) > 8 || len(input) > len(opening) {
		return false
	}
	for i, r := range input {
		if r != opening[i] {
			return false
		}
	}
	return true
}
