package realtimelive

import (
	"familyrobot/internal/realtimestate"
	"strings"
	"time"
	"unicode"
)

// Nine runes distinguish a short recognition fragment (at most eight) from a
// longer utterance. These references are never promoted to played-text evidence.
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

func (s *Session) speechPlayingLocked() bool {
	_, device := s.wire.(*xiaozhiWire)
	rt := s.output
	return device && s.ownerErr == nil && s.rec.owner.ClientType == "xiaozhi" &&
		diagnosticDevicePattern.MatchString(s.rec.owner.DeviceID) && s.activeOutputLocked() &&
		!rt.media && rt.turn.Begun && rt.turn.Sent > 0
}

func freshPlaybackProgress(rt *liveTurn, now time.Time) bool {
	if rt == nil || rt.turn.PlaybackBasis != "i2s_dma" || rt.turn.Completed <= 0 || rt.lastPlaybackProgress.IsZero() {
		return false
	}
	age := now.Sub(rt.lastPlaybackProgress)
	return age >= 0 && age <= 500*time.Millisecond
}

// Freeze only positive hardware playback actually observed during this capture.
// Sent packets, zero progress and receipts arriving after VAD stop are insufficient.
func (p *pendingInput) observePlayback(output *liveTurn, now time.Time) {
	if p.overlapSamples == 0 && p.overlapped && output == p.outputAtStart && freshPlaybackProgress(output, now) {
		p.overlapSamples = output.turn.Completed
	}
}

// Compare only text close to playback. The draft is a bounded opening hint.
// Mid-answer, word timings from OutputASR must already fall within real I2S
// progress; matching arbitrary future words in a story would block real people.
func (s *Session) suspectedOpeningEchoLocked(p *pendingInput, text string, now time.Time) bool {
	rt := s.output
	if p == nil || p.hadPreview || !p.overlapped || (p.ended && p.overlapSamples == 0) || !s.speechPlayingLocked() || rt != p.outputAtStart ||
		!freshPlaybackProgress(rt, now) {
		return false
	}
	input := echoOpeningRunes(text)
	if len(input) == 0 || len(input) > 8 {
		return false
	}
	if rt.turn.Completed <= 36000 {
		if rt.rescueText != "" && strings.Contains(string(echoOpeningRunes(rt.rescueText)), string(input)) {
			return true
		}
		return strings.Contains(string(echoOpeningRunes(rt.turn.PlaybackTranscript())), string(input))
	}
	if rt.turn.Tracker == nil || len(rt.turn.Tracker.Sentences) == 0 {
		return false
	}
	words, _ := realtimestate.Observations(rt.turn, 0)
	var recent strings.Builder
	since := float64(rt.turn.Completed)/24 - 1000
	for _, word := range words {
		end, ok := timestamp(word.EndMS)
		if ok && end >= since {
			recent.WriteString(word.Text)
		}
	}
	heard := strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, recent.String())
	return strings.Contains(heard, string(input))
}

type inputDecision struct {
	allow  bool
	reason string
	match  bool
}

func (p *pendingInput) shortEchoCapture() bool {
	return p.matchedEchoPreview && !p.hadPreview && p.ended && p.overlapped && p.overlapSamples > 0 &&
		len(p.pcm) > 0 && len(p.pcm) <= 32000 // up to one second at 16 kHz, PCM16
}

// In very short Mandarin ASR fragments, repeated syllables and trailing
// questioning/aspect particles can all come from one uncertain syllable.
// Keep this as an overlap-only confidence hint, not transcript normalization.
func hasDistinctSpeech(runes []rune) bool {
	if len(runes) < 2 {
		return false
	}
	for len(runes) > 1 && strings.ContainsRune("了吗呢啊呀吧啦嘛哦", runes[len(runes)-1]) {
		runes = runes[:len(runes)-1]
	}
	for _, r := range runes[1:] {
		if r != runes[0] {
			return true
		}
	}
	return false
}

// Session.mu owns the input and output. A first short preview while audio is
// playing is ambiguous. Only a later extension adds recognition content; a
// repeated unstable preview or a final label alone does not establish a speaker.
func (s *Session) playbackInputLocked(p *pendingInput, text, stable string, final bool, now time.Time) inputDecision {
	if p.hadPreview || !p.overlapped {
		return inputDecision{allow: true, reason: "ordinary_input"}
	}
	runes := echoOpeningRunes(text)
	normalized := string(runes)
	shortAudio := len(p.pcm) > 0 && len(p.pcm) <= 32000 // one second of 16 kHz PCM16
	if (!final || shortAudio) && s.suspectedOpeningEchoLocked(p, text, now) {
		if !final && len(runes) <= 2 {
			p.weakPreview = normalized
		}
		return inputDecision{reason: "suspected_opening_echo", match: true}
	}
	// Direct short stop requests remain usable. An echo matching the audible
	// reference was tested above, including when the robot itself says "停".
	switch normalized {
	case "停", "停止", "停下", "等等", "等下", "等一下", "停一下":
		return inputDecision{allow: true, reason: "stop_request"}
	}
	if final {
		// The observed false final followed a weak preview at playback onset.
		// Preserve novel short questions later in a story if there is no
		// matching recent playback evidence.
		if p.overlapSamples > 0 && p.overlapSamples <= 36000 && len(runes) <= 8 && shortAudio && !hasDistinctSpeech(runes) {
			return inputDecision{reason: "uncorroborated_short_input"}
		}
		return inputDecision{allow: true, reason: "final_content"}
	}
	qualified := hasDistinctSpeech(runes)
	if len(runes) <= 3 || len(runes) <= 8 && !qualified {
		stableContent := qualified && hasDistinctSpeech(echoOpeningRunes(stable))
		if stableContent && len(runes) == 3 {
			return inputDecision{allow: true, reason: "stable_content"}
		}
		if stableContent && p.weakPreview != "" && normalized != p.weakPreview && strings.HasPrefix(normalized, p.weakPreview) {
			return inputDecision{allow: true, reason: "preview_advanced"}
		}
		p.weakPreview = normalized
		return inputDecision{reason: "weak_preview"}
	}
	return inputDecision{allow: true, reason: "preview_content"}
}

// Holding a preview is observable without persisting either transcript. Keep
// the snapshot under the decision lock so cancellation cannot change its IDs.
func (s *Session) inputDecisionFieldsLocked(p *pendingInput, result inputDecision, source string, chars int, now time.Time) map[string]any {
	fields := map[string]any{"id": p.id, "source": source, "reason": result.reason, "chars": chars, "item_scope": "current", "confirmed": result.allow}
	if rt := p.outputAtStart; rt != nil {
		var age any
		if !rt.lastPlaybackProgress.IsZero() {
			age = now.Sub(rt.lastPlaybackProgress).Milliseconds()
		}
		fields["opening_echo_check"] = map[string]any{
			"output_id": rt.turn.ID, "same_output": s.output == rt,
			"active_output": s.activeOutputLocked(), "overlapped": p.overlapped, "capture_played_samples": p.overlapSamples,
			"reference_present": rt.turn.PlaybackTranscript() != "" || rt.rescueText != "", "matched": result.match,
			"completed": rt.turn.Completed, "progress_age_ms": age,
		}
	}
	return fields
}
