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
	if p := s.lastInput; p != nil {
		clear(p.pcm)
		p.pcm = nil
		p.retired = true
	}
	s.lastInput = nil
}

func (s *Session) closeInputs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discardInputLocked()
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
	s.discardInputLocked()
	end := s.ring.End
	pos, valid := timestamp(event["audio_start_ms"])
	start := max(int64(0), end-8000)
	if valid && pos >= 0 && pos*16 <= float64(end) {
		start = int64(pos * 16)
	}
	s.seq++
	p := &pendingInput{id: s.seq, item: item, start: start, speakerStartValid: valid && pos >= 0 && pos*16 <= float64(s.providerEnd)}
	s.lastInput = p
	s.items[item] = p
	s.itemOrder = append(s.itemOrder, item)
	if len(s.itemOrder) > inputItemLimit {
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
	end := s.ring.End
	pos, valid := timestamp(event["audio_end_ms"])
	if valid && pos > 0 {
		end = min(end, int64(pos*16))
	}
	pcm := s.ring.Slice(p.start, end)
	complete := p.speakerStartValid && valid && pos > 0 && pos*16 <= float64(s.providerEnd) && int64(pos*16) == end
	accepted := pcm != nil && len(pcm) <= inputPCMLimit
	if accepted {
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
	p.hadPreview = true
	s.mu.Unlock()
	if err := s.interruptInput(nil, "provider_asr_preview", p); err != nil {
		return err
	}
	return s.record("input_preview", map[string]any{"id": p.id, "chars": chars, "item_scope": "current", "confirmed": true})
}

func (s *Session) finishInput(event map[string]any) error {
	item, validItem := inputItem(event)
	if !validItem {
		return nil
	}
	text, bounded, meaningful := inputText(event["transcript"])
	s.mu.Lock()
	p := s.lastInput
	if s.ctx.Err() != nil || p == nil || p.item != item || !p.ended || p.retired || p.finalized {
		s.mu.Unlock()
		return nil
	}
	p.finalized = true
	if !bounded || !meaningful {
		hadPreview := p.hadPreview
		s.discardInputLocked()
		s.mu.Unlock()
		reason := "empty_transcript"
		if !bounded {
			reason = "invalid_transcript"
		}
		return s.record("input_rejected", map[string]any{"id": p.id, "reason": reason, "had_preview": hadPreview, "item_scope": "current"})
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
	pcm := p.pcm
	p.pcm = nil
	s.discardInputLocked()
	rt := s.activateTurnLocked(p.id, p.stoppedAt)
	rt.turn.User = text
	rt.transcriptOnce.Do(func() { close(rt.transcriptReady) })
	s.mu.Unlock()
	if err := s.record("user_text", map[string]any{"id": rt.turn.ID, "text": text}); err != nil {
		clear(pcm)
		close(rt.done)
		return err
	}
	s.spawn(func() { defer close(rt.done); defer clear(pcm); s.reply(rt, pcm) })
	return nil
}
