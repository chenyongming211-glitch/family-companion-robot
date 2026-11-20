package voicecompare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/gorilla/websocket"
)

var errTaskRejected = errors.New("provider rejected task")

type ttsFailure struct {
	category string
	cause    error
}

func (e ttsFailure) Error() string { return e.cause.Error() }
func (e ttsFailure) Unwrap() error { return e.cause }

// TTSFailureCategory is safe for durable logs. Provider payloads and the text
// being synthesized may appear in the underlying errors.
func TTSFailureCategory(err error) string {
	var failure ttsFailure
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errTaskRejected):
		return "provider_rejected"
	case errors.As(err, &failure):
		return failure.category
	default:
		return "protocol_or_transport"
	}
}

// Isolated punctuation or pictographs produce no words to align and can be
// rejected by the provider. Filter only whole fragments with no spoken text.
func ttsDecorationOnly(text string) bool {
	return strings.TrimFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("，。！？；,.!?;～~…*“”‘’「」『』【】（）()", r)
	}) == ""
}

func splitClauses(text string) []string {
	// CosyVoice omits these pictographs even inside a spoken fragment. Remove
	// them before submission so the strict provider text check still holds.
	// Result.Text separately retains the full model reply.
	spoken := strings.Map(func(r rune) rune {
		if r == '✨' || r >= 0x1F300 && r <= 0x1FAFF && unicode.Is(unicode.So, r) {
			return -1
		}
		return r
	}, text)
	var parts []string
	var b strings.Builder
	n := 0
	runes := []rune(spoken)
	for i, r := range runes {
		b.WriteRune(r)
		n++
		numericSeparator := (r == '.' || r == ',') && i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1])
		punctuationEnd := strings.ContainsRune("，。！？；,.!?;～~\n", r) && !numericSeparator
		ellipsisEnd := r == '…' && (i+1 == len(runes) || runes[i+1] != '…')
		if punctuationEnd || ellipsisEnd || n == 64 {
			if !ttsDecorationOnly(b.String()) {
				parts = append(parts, b.String())
			}
			b.Reset()
			n = 0
		}
	}
	if !ttsDecorationOnly(b.String()) {
		parts = append(parts, b.String())
	}
	return parts
}

func (c Client) tts(ctx context.Context, text string, maxBytes int) (pcm []byte, words []Word, call Call, err error) {
	// Each clause has <=64 runes (<=128 billable characters): v3 Flash <=¥0.0128.
	call, err = c.call(ctx, "tts", .015, func(usage *map[string]any) error {
		s, e := c.dial(ctx, c.InferenceURL)
		if e != nil {
			return e
		}
		defer s.close()
		id := taskID()
		if e = s.send(taskCommand("run-task", id, map[string]any{"task_group": "audio", "task": "tts", "function": "SpeechSynthesizer", "model": c.TTSModel, "parameters": map[string]any{"text_type": "PlainText", "format": "pcm", "sample_rate": 24000, "word_timestamp_enabled": true, "voice": c.Voice}, "input": map[string]any{}})); e != nil {
			return e
		}
		ev, e := s.taskEvent(id, usage)
		if e != nil {
			return e
		}
		if ev.Header.Event != "task-started" {
			return errors.New("missing task-started")
		}
		if e = s.send(taskCommand("continue-task", id, map[string]any{"input": map[string]any{"text": text}})); e != nil {
			return e
		}
		if e = s.send(taskCommand("finish-task", id, map[string]any{"input": map[string]any{}})); e != nil {
			return e
		}
		started, ended, coarse := false, false, false
		index := -1
		for {
			ev, e = s.taskEvent(id, usage)
			if e != nil {
				return e
			}
			if ev.Header.Event == "task-finished" {
				if len(pcm) == 0 {
					return ttsFailure{"audio_invalid", errors.New("missing TTS audio")}
				}
				if !ended || len(words) == 0 && !coarse {
					return errors.New("incomplete TTS terminal")
				}
				return nil
			}
			if ev.Header.Event != "result-generated" {
				return errors.New("unexpected TTS event")
			}
			var out struct {
				Type     string `json:"type"`
				Original string `json:"original_text"`
				Sentence struct {
					Index *int            `json:"index"`
					Words []timestampWord `json:"words"`
				} `json:"sentence"`
			}
			if json.Unmarshal(ev.Payload.Output, &out) != nil || out.Sentence.Index == nil || *out.Sentence.Index < 0 || ended {
				return errors.New("invalid TTS sentence")
			}
			if out.Type == "sentence-begin" {
				if started {
					return errors.New("unexpected second TTS sentence")
				}
				// Begin/stream events may carry changing provisional timestamps.
				// Only sentence-end words are validated and used for playback mapping.
				if out.Original != text {
					return ttsFailure{"source_text_mismatch", fmt.Errorf("TTS original text mismatch: requested=%q received=%q", text, out.Original)}
				}
				started = true
				index = *out.Sentence.Index
				continue
			}
			if !started || *out.Sentence.Index != index || out.Original != "" && out.Original != text {
				return errors.New("TTS sentence identity mismatch")
			}
			switch out.Type {
			case "sentence-synthesis":
				kind, b, e := s.read()
				if e != nil {
					return e
				}
				if kind != websocket.BinaryMessage || len(b) == 0 || len(b)%2 != 0 || len(pcm)+len(b) > maxBytes {
					return ttsFailure{"audio_invalid", errors.New("missing or invalid TTS audio frame")}
				}
				pcm = append(pcm, b...)
			case "sentence-end":
				if out.Original != text {
					return errors.New("missing final sentence text")
				}
				words, e = validateWords(text, out.Sentence.Words, int64(len(pcm)/48))
				if e != nil && canUseTaskBoundary(text, out.Sentence.Words, int64(len(pcm)/48)) {
					// Preserve completed audio without inventing source-word positions.
					// Run records the exact task PCM boundary separately from Words.
					words, e, coarse = nil, nil, true
				}
				if e != nil {
					return ttsFailure{"timestamp_invalid", e}
				}
				ended = true
			default:
				return errors.New("unexpected TTS output")
			}
		}
	})
	return
}

func canUseTaskBoundary(text string, raw []timestampWord, durationMS int64) bool {
	if len(raw) == 0 || len(raw) > 512 {
		return false
	}
	var spoken strings.Builder
	for _, w := range raw {
		spoken.WriteString(w.Text)
	}
	limit := max(len([]rune(text)), len([]rune(spoken.String())))
	lastIndex, lastTime := int64(0), int64(0)
	for _, w := range raw {
		if w.Text == "" || w.Begin == nil || w.End == nil || w.BeginIndex == nil || w.EndIndex == nil ||
			*w.BeginIndex < lastIndex || *w.EndIndex <= *w.BeginIndex || *w.EndIndex > int64(limit) ||
			*w.Begin < lastTime || *w.End <= *w.Begin || *w.End > durationMS {
			return false
		}
		lastIndex, lastTime = *w.EndIndex, *w.End
	}
	lexical := func(value string) []rune {
		return []rune(strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsPunct(r) || r == '～' || r == '~' {
				return -1
			}
			return r
		}, value))
	}
	source, reported := lexical(text), lexical(spoken.String())
	i, j := 0, 0
	for i < len(source) && j < len(reported) {
		if unicode.IsDigit(source[i]) {
			for i < len(source) && unicode.IsDigit(source[i]) {
				i++
			}
			start := j
			// Number pronunciation remains opaque: this grants only the whole
			// task's PCM boundary, never digit-level or numeric-value evidence.
			for j < len(reported) && (unicode.IsDigit(reported[j]) || strings.ContainsRune("零〇一二三四五六七八九十百千万亿两幺点负正", reported[j])) {
				j++
			}
			if j == start {
				return false
			}
		} else {
			if source[i] != reported[j] {
				return false
			}
			i++
			j++
		}
	}
	return i == len(source) && j == len(reported)
}

type timestampWord struct {
	Text       string `json:"text"`
	Begin      *int64 `json:"begin_time"`
	End        *int64 `json:"end_time"`
	BeginIndex *int64 `json:"begin_index"`
	EndIndex   *int64 `json:"end_index"`
}

func validateWords(text string, raw []timestampWord, durationMS int64) ([]Word, error) {
	if len(raw) == 0 || len(raw) > 512 {
		return nil, errors.New("missing or excessive TTS timestamps")
	}
	source := []rune(text)
	position, last := int64(0), int64(0)
	var words []Word
	ignorable := func(s []rune) bool {
		for _, r := range s {
			if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
				return false
			}
		}
		return true
	}
	for _, w := range raw {
		if w.Begin == nil || w.End == nil || w.BeginIndex == nil || w.EndIndex == nil {
			return nil, errors.New("missing timestamp field")
		}
		b, e := *w.BeginIndex, *w.EndIndex
		// Finishing a comma-ended task may label only its final comma as a period.
		// Preserve the source punctuation; lexical text and all timing checks stay exact.
		terminalFormat := false
		if b >= 0 && e == int64(len(source)) && e == b+1 {
			terminalFormat = strings.ContainsRune("，,", source[b]) && (w.Text == "。" || w.Text == ".") || strings.ContainsRune("～~", source[b]) && (w.Text == "～" || w.Text == "~")
		}
		if b < position || e <= b || e > int64(len(source)) || !ignorable(source[position:b]) || string(source[b:e]) != w.Text && !terminalFormat || *w.Begin < last || *w.End <= *w.Begin || *w.End > durationMS {
			detail, _ := json.Marshal(w)
			return nil, fmt.Errorf("invalid or incomplete TTS timestamps: duration_ms=%d position=%d last_end_ms=%d word=%s source=%q", durationMS, position, last, detail, text)
		}
		words = append(words, Word{Text: string(source[position:e]), BeginMS: *w.Begin, EndMS: *w.End})
		position, last = e, *w.End
	}
	for _, r := range source[position:] {
		// A trailing tone mark has no separate spoken word; retain it in the source.
		// Interior symbols still pass through the stricter gap check above.
		if r != '～' && r != '~' && !unicode.IsSpace(r) && !unicode.IsPunct(r) {
			return nil, errors.New("TTS timestamp text coverage missing")
		}
	}
	words[len(words)-1].Text += string(source[position:])
	return words, nil
}
