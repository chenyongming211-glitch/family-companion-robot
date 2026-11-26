package realtimelive

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const inputPCMLimit = 16000 * 2 * 25
const inputItemLimit = 64
const inputTextLimit = 4096

// Only the current input may retain PCM. Retired entries are bounded item-ID
// tombstones so late provider events cannot be reassigned to the next capture.
// Session.mu owns all fields. Observer PCM is a different owned allocation.
type pendingInput struct {
	id                                    int64
	item                                  string
	start                                 int64
	speakerStartValid                     bool
	ended, hadPreview, finalized, retired bool
	stoppedAt                             time.Time
	pcm                                   []byte
	outputAtStart                         *liveTurn
	overlapped                            bool   // Device speech was playing when VAD opened this capture.
	playbackSeen                          bool   // Device speech was observed playing at some point during this capture.
	overlapSamples                        int64  // First confirmed playhead observed during this capture.
	weakPreview                           string // At most eight normalized runes until corroborated.
	matchedEchoPreview                    bool   // An earlier preview matched this capture's playing output.
	lastHeldReason                        string
	speakerState                          string
	speakerLabel                          string
	speakerReason                         string
	carryFrom                             *pendingInput
	carryText                             string
	carryPCM                              []byte
}

func carriedCompareRunes(text string) []rune {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return []rune(strings.Trim(b.String(), "嗯呃啊哦噢诶欸"))
}

func joinCarriedText(prefix, suffix string) (string, bool) {
	prefix = strings.TrimSpace(prefix)
	suffix = strings.TrimSpace(suffix)
	if prefix == "" {
		return suffix, false
	}
	if suffix == "" {
		return prefix, false
	}
	trimmed := strings.TrimRight(prefix, "。，、！？；：,.!?;: ")
	if trimmed == "" {
		return suffix, false
	}
	// When the user pauses and restarts the sentence from the beginning
	// ("查一下明天呃。" -> "查一下明天杭州的天气。"), keep only the full restarted
	// sentence (and its audio) instead of stuttering the repeated opening prefix.
	prefixRunes := carriedCompareRunes(trimmed)
	suffixRunes := carriedCompareRunes(suffix)
	if len(prefixRunes) >= 3 && len(suffixRunes) >= len(prefixRunes) &&
		string(suffixRunes[:len(prefixRunes)]) == string(prefixRunes) {
		return suffix, true
	}
	return trimmed + "，" + suffix, false
}

func retireInputLocked(p *pendingInput) {
	if p == nil {
		return
	}
	if p.carryFrom != nil {
		retireInputLocked(p.carryFrom)
		p.carryFrom = nil
	}
	clear(p.pcm)
	p.pcm = nil
	clear(p.carryPCM)
	p.carryPCM = nil
	p.carryText = ""
	p.outputAtStart = nil
	p.weakPreview = ""
	p.retired = true
}

func inputItem(event map[string]any) (string, bool) {
	id, ok := event["item_id"].(string)
	if !ok || id == "" || len(id) > 256 || !utf8.ValidString(id) {
		return "", false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", false
		}
	}
	return id, true
}

func inputText(value any) (text string, bounded, meaningful bool) {
	text, ok := value.(string)
	if !ok || len(text) > inputTextLimit*utf8.UTFMax || !utf8.ValidString(text) || utf8.RuneCountInString(text) > inputTextLimit {
		return "", false, false
	}
	text = strings.TrimSpace(text)
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return text, true, true
		}
	}
	return text, true, false
}

func (s *Session) discardInputLocked() {
	retireInputLocked(s.lastInput)
	s.lastInput = nil
}

func (s *Session) closeInputs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discardInputLocked()
	for _, p := range s.items {
		retireInputLocked(p)
	}
	clear(s.items)
	s.itemOrder = nil
}

func (s *Session) activeOutputLocked() bool {
	rt := s.output
	return rt != nil && !rt.turn.Cut && !rt.turn.PlaybackFinal && !(rt.turn.Sent > 0 && rt.turn.Completed == rt.turn.Sent && closed(rt.done))
}

func (s *Session) startInput(event map[string]any) error {
	item, validItem := inputItem(event)
	if !validItem {
		return nil
	}
	s.mu.Lock()
	if s.ctx.Err() != nil || s.items[item] != nil {
		s.mu.Unlock()
		return nil
	}
	end := s.ring.End
	pos, valid := timestamp(event["audio_start_ms"])
	start := max(int64(0), end-8000)
	if valid && pos >= 0 && pos*16 <= float64(end) {
		start = int64(pos * 16)
	}
	now := time.Now()
	var carryFrom *pendingInput
	var carryText string
	var carryPCM []byte
	if prev := s.lastInput; prev != nil && prev.ended && !prev.finalized && !prev.retired &&
		prev.hadPreview && len(prev.pcm) > 0 && start > prev.start && now.Sub(prev.stoppedAt) <= 2*time.Second {
		if prev.carryFrom != nil {
			retireInputLocked(prev.carryFrom)
			prev.carryFrom = nil
		}
		carryText = prev.carryText
		carryPCM = prev.carryPCM
		prev.carryText = ""
		prev.carryPCM = nil
		carryFrom = prev
		s.lastInput = nil
	} else {
		s.discardInputLocked()
	}
	s.seq++
	p := &pendingInput{
		id: s.seq, item: item, start: start,
		speakerStartValid: valid && pos >= 0 && pos*16 <= float64(s.providerEnd),
		outputAtStart:     s.output, overlapped: s.speechPlayingLocked(),
		carryFrom: carryFrom, carryText: carryText, carryPCM: carryPCM,
	}
	p.observePlayback(s.output, now)
	s.lastInput = p
	s.items[item] = p
	s.itemOrder = append(s.itemOrder, item)
	if len(s.itemOrder) > inputItemLimit {
		evicted := s.items[s.itemOrder[0]]
		if evicted != nil && evicted == p.carryFrom {
			p.carryFrom = nil
		}
		retireInputLocked(evicted)
		delete(s.items, s.itemOrder[0])
		s.itemOrder = s.itemOrder[1:]
	}
	listening := !s.activeOutputLocked()
	var err error
	if listening {
		err = s.sendLocked(map[string]any{"type": "status", "code": "listening"})
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.beginSpeaker()
	return s.record("speech_started", map[string]any{"id": p.id, "sample": start, "received_end": end, "provider_timestamp": valid, "item_scope": "current"})
}

func (s *Session) stopInput(event map[string]any) error {
	item, validItem := inputItem(event)
	if !validItem {
		return nil
	}
	s.mu.Lock()
	p := s.lastInput
	if s.ctx.Err() != nil || p == nil || p.item != item || p.ended || p.retired {
		s.mu.Unlock()
		return nil
	}
	p.ended = true
	p.stoppedAt = time.Now()
	p.observePlayback(s.output, p.stoppedAt)
	end := s.ring.End
	pos, valid := timestamp(event["audio_end_ms"])
	if valid && pos > 0 {
		end = min(end, int64(pos*16))
	}
	pcm := s.ring.Slice(p.start, end)
	complete := p.speakerStartValid && valid && pos > 0 && pos*16 <= float64(s.providerEnd) && int64(pos*16) == end
	accepted := pcm != nil && len(pcm) <= inputPCMLimit
	if accepted {
		carriedBytes := len(p.carryPCM)
		if p.carryFrom != nil {
			carriedBytes += len(p.carryFrom.pcm)
		}
		if len(pcm)+carriedBytes > inputPCMLimit {
			if p.carryFrom != nil {
				retireInputLocked(p.carryFrom)
				p.carryFrom = nil
			}
			clear(p.carryPCM)
			p.carryPCM = nil
			p.carryText = ""
		}
		// The observer can retain its PCM asynchronously; pending-input disposal
		// must never clear or reuse that allocation.
		p.pcm = append([]byte(nil), pcm...)
	} else {
		s.discardInputLocked()
	}
	idle := !s.activeOutputLocked()
	s.mu.Unlock()
	s.observeSpeaker(p.id, p.start, end, pcm, complete)
	if !accepted {
		if err := s.record("input_rejected", map[string]any{"id": p.id, "reason": "input_too_long_or_incomplete", "had_preview": p.hadPreview, "samples": len(pcm) / 2, "item_scope": "current"}); err != nil {
			return err
		}
		if idle {
			return s.send(map[string]any{"type": "status", "code": "input_too_long"})
		}
		return nil
	}
	return s.record("input_segment", map[string]any{"id": p.id, "samples": len(pcm) / 2, "item_scope": "current", "awaiting_final": true})
}

func (s *Session) previewInput(event map[string]any) error {
	item, validItem := inputItem(event)
	if !validItem {
		return nil
	}
	meaningful, chars := false, 0
	preview := ""
	for _, field := range []string{"text", "stash"} {
		if value, present := event[field]; present {
			text, bounded, hasText := inputText(value)
			if !bounded {
				return nil
			}
			if hasText {
				meaningful = true
				chars = max(chars, utf8.RuneCountInString(text))
			}
			preview += text
		}
	}
	if !meaningful {
		return nil
	}
	s.mu.Lock()
	p := s.lastInput
	if s.ctx.Err() != nil || p == nil || p.item != item || p.retired || p.hadPreview || p.finalized {
		s.mu.Unlock()
		return nil
	}
	// The provider's text and stash can be stable and unstable continuations.
	// Compare their complete content; a matching field cannot hide new words.
	now := time.Now()
	if s.speechPlayingLocked() && s.output == p.outputAtStart {
		p.playbackSeen = true
	}
	if !p.ended {
		p.observePlayback(s.output, now)
	}
	stable, _ := event["text"].(string)
	decision := s.playbackInputLocked(p, preview, stable, false, now)
	if decision.match {
		p.matchedEchoPreview = true
	}
	if decision.allow && p.overlapped && s.cfg.Speaker != nil {
		captured := p.pcm
		if !p.ended {
			captured = s.ring.Slice(p.start, s.ring.End)
		}
		if reason := s.bystanderRejectionLocked(p, captured); reason != "" {
			decision = inputDecision{reason: reason}
		}
	}
	// Once VAD has sealed a short capture, a revised preview adds no new
	// audio. Wait for its final before cutting a previously matching output.
	if decision.allow && decision.reason != "stop_request" && p.shortEchoCapture() &&
		s.output == p.outputAtStart && s.speechPlayingLocked() && len(echoOpeningRunes(preview)) <= 8 {
		decision = inputDecision{reason: "awaiting_echo_check"}
	}
	fields := s.inputDecisionFieldsLocked(p, decision, "provider_asr_preview", chars, now)
	if !decision.allow {
		record := p.lastHeldReason != decision.reason
		p.lastHeldReason = decision.reason
		s.mu.Unlock()
		if record {
			return s.record("input_held", fields)
		}
		return nil
	}
	p.hadPreview = true
	s.mu.Unlock()
	if err := s.interruptInput(nil, "provider_asr_preview", p); err != nil {
		return err
	}
	return s.record("input_preview", fields)
}

func (s *Session) finishInput(event map[string]any) error {
	item, validItem := inputItem(event)
	if !validItem {
		return nil
	}
	text, bounded, meaningful := inputText(event["transcript"])
	s.mu.Lock()
	p := s.lastInput
	carrying := false
	if p != nil && p.carryFrom != nil && p.carryFrom.item == item {
		p = p.carryFrom
		carrying = true
	}
	if s.ctx.Err() != nil || p == nil || p.item != item || !p.ended || p.retired || p.finalized {
		s.mu.Unlock()
		return nil
	}
	p.finalized = true
	if !carrying && p.carryFrom != nil {
		retireInputLocked(p.carryFrom)
		p.carryFrom = nil
	}
	if !carrying && bounded && !meaningful && p.carryText != "" {
		// A trailing breath or cough after a validated first clause produces an
		// empty second transcript. Promote the already-validated first clause
		// instead of dropping the user's whole sentence.
		text = p.carryText
		meaningful = true
		p.hadPreview = true
		p.carryText = ""
		if len(p.carryPCM) > 0 {
			clear(p.pcm)
			p.pcm = p.carryPCM
			p.carryPCM = nil
		}
	}
	if !bounded || !meaningful {
		hadPreview := p.hadPreview
		if carrying {
			if s.lastInput != nil && s.lastInput.carryFrom == p {
				s.lastInput.carryFrom = nil
			}
			retireInputLocked(p)
		} else {
			s.discardInputLocked()
		}
		s.mu.Unlock()
		reason := "empty_transcript"
		if !bounded {
			reason = "invalid_transcript"
		}
		return s.record("input_rejected", map[string]any{"id": p.id, "reason": reason, "had_preview": hadPreview, "item_scope": "current"})
	}
	now := time.Now()
	if s.speechPlayingLocked() && s.output == p.outputAtStart {
		p.playbackSeen = true
	}
	decision := s.playbackInputLocked(p, text, text, true, now)
	if decision.allow {
		if reason := s.bystanderRejectionLocked(p, p.pcm); reason != "" {
			decision = inputDecision{reason: reason}
		}
	}
	audioCheck := ""
	referenceMS := 0
	if decision.allow && decision.reason != "stop_request" && p.shortEchoCapture() {
		audioCheck = "unavailable"
		if reference := s.capturedEchoReferenceLocked(p); len(reference) != 0 {
			referenceMS = len(reference) / 32 // 16 kHz mono PCM16
			// The output or a new VAD item can retire p and wipe its microphone
			// buffer while the comparison runs. Keep a private bounded copy.
			mic := append([]byte(nil), p.pcm...)
			s.mu.Unlock()
			matched := matchingPlaybackAudio(mic, reference)
			clear(mic)
			clear(reference)
			s.mu.Lock()
			stillValid := s.lastInput == p || (carrying && s.lastInput != nil && s.lastInput.carryFrom == p)
			if s.ctx.Err() != nil || !stillValid || p.retired {
				s.mu.Unlock()
				return nil
			}
			now = time.Now()
			audioCheck = "no_match"
			if matched && s.output == p.outputAtStart {
				audioCheck = "matched"
				decision = inputDecision{reason: "confirmed_playback_echo"}
			}
		}
	}
	fields := s.inputDecisionFieldsLocked(p, decision, "provider_asr_final", utf8.RuneCountInString(text), now)
	if audioCheck != "" {
		fields["playback_audio_check"] = audioCheck
		fields["playback_reference_ms"] = referenceMS
	}
	fields["samples"] = len(p.pcm) / 2
	fields["had_preview"] = p.hadPreview
	if !decision.allow {
		if carrying {
			if s.lastInput != nil && s.lastInput.carryFrom == p {
				s.lastInput.carryFrom = nil
			}
			retireInputLocked(p)
		} else {
			s.discardInputLocked()
		}
		s.mu.Unlock()
		return s.record("input_rejected", fields)
	}
	if carrying {
		cur := s.lastInput
		if cur == nil || cur.retired || cur.carryFrom != p {
			retireInputLocked(p)
			s.mu.Unlock()
			return nil
		}
		cur.carryFrom = nil
		name := ""
		if s.product != nil {
			name = s.product.Name()
		}
		if !memoryClearCommand(text, name) {
			joined, restarted := joinCarriedText(cur.carryText, text)
			cur.carryText = joined
			if restarted {
				clear(cur.carryPCM)
				cur.carryPCM = nil
			}
			if len(cur.carryPCM)+len(p.pcm)+len(cur.pcm) <= inputPCMLimit {
				cur.carryPCM = append(cur.carryPCM, p.pcm...)
			}
		}
		intoID := cur.id
		retireInputLocked(p)
		s.mu.Unlock()
		return s.record("input_carried", map[string]any{"id": p.id, "into_id": intoID, "text": text, "input_decision": fields})
	}
	s.mu.Unlock()
	if err := s.interruptInput(nil, "provider_asr_final", p); err != nil {
		return err
	}
	s.mu.Lock()
	if s.ctx.Err() != nil || s.lastInput != p || p.retired {
		s.mu.Unlock()
		return nil
	}
	if s.output != nil && s.output != p.outputAtStart {
		s.discardInputLocked()
		s.mu.Unlock()
		return nil
	}
	name := ""
	if s.product != nil {
		name = s.product.Name()
	}
	if p.carryText != "" && !memoryClearCommand(p.carryText, name) && !memoryClearCommand(text, name) {
		var restarted bool
		text, restarted = joinCarriedText(p.carryText, text)
		if !restarted && len(p.carryPCM) > 0 && len(p.carryPCM)+len(p.pcm) <= inputPCMLimit {
			p.pcm = append(p.carryPCM, p.pcm...)
		}
	}
	clear(p.carryPCM)
	p.carryPCM = nil
	p.carryText = ""
	if n := len(s.history); n > 0 {
		prev := s.history[n-1]
		if prev.turn.ID >= s.memoryFloor && !prev.media && prev.turn.User != "" &&
			!prev.turn.Begun && prev.turn.Sent == 0 && prev.turn.Completed == 0 && prev.turn.Cut &&
			!prev.stoppedAt.IsZero() && p.stoppedAt.Sub(prev.stoppedAt) <= 3*time.Second &&
			!memoryClearCommand(prev.turn.User, name) && !memoryClearCommand(text, name) {
			text, _ = joinCarriedText(prev.turn.User, text)
			s.history = s.history[:n-1]
		}
	}
	pcm := p.pcm
	p.pcm = nil
	overlapped := p.overlapped
	speakerState, speakerLabel := p.speakerState, p.speakerLabel
	s.discardInputLocked()
	rt := s.activateTurnLocked(p.id, p.stoppedAt)
	rt.overlapped = overlapped
	rt.turn.User = text
	if !overlapped && !s.speakerFailed && s.speakerAnchor == "" && speakerState == "temporary" && speakerLabel != "" {
		s.speakerAnchor = speakerLabel
	}
	rt.transcriptOnce.Do(func() { close(rt.transcriptReady) })
	s.mu.Unlock()
	if err := s.record("user_text", map[string]any{"id": rt.turn.ID, "text": text, "input_decision": fields}); err != nil {
		clear(pcm)
		close(rt.done)
		return err
	}
	s.spawn(func() { defer close(rt.done); defer clear(pcm); s.reply(rt, pcm) })
	return nil
}
