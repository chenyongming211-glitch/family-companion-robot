package gateway

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Wake identity comes from the device's detect control, never a hardcoded name
// or a substring in a real instruction. Keep only one bounded phrase per device
// connection; it survives upstream-only reconnects just like playback identity.
func (s *session) rememberWakePhrase(text string) {
	s.wakePhrase = ""
	if utf8.RuneCountInString(text) <= 128 {
		s.wakePhrase = normalizeWakePhrase(text)
	}
	s.discardWakePause()
}

func normalizeWakePhrase(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
}

func (s *session) wakeOnly(origin requestOrigin) bool {
	return origin.known && !origin.truncated && s.wakePhrase != "" &&
		normalizeWakePhrase(origin.text) == s.wakePhrase
}

func (s *session) discardWakePause() {
	if s.paused != nil && s.wakeOnly(s.paused.playback.origin) {
		s.tracePlayback("wake_pause_discarded", &s.paused.playback)
		s.paused = nil
	}
}
