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

func splitClauses(text string) []string {
	var parts []string
	var b strings.Builder
	n := 0
	runes := []rune(text)
	for i, r := range runes {
		b.WriteRune(r)
		n++
		numericSeparator := (r == '.' || r == ',') && i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1])
		if strings.ContainsRune("，。！？；,.!?;\n", r) && !numericSeparator || n == 64 {
			if strings.TrimSpace(b.String()) != "" {
				parts = append(parts, b.String())
			}
			b.Reset()
			n = 0
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		parts = append(parts, b.String())
	}
	return parts
}

func (c Client) tts(ctx context.Context, text string, maxBytes int) (pcm []byte, words []Word, call Call, err error) {
	// Each clause has <=64 runes (<=128 billable characters): v3 Flash <=¥0.0128.
	call, err = c.call(ctx, "tts", .02, func(usage *map[string]any) error {
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
		started, ended := false, false
		index := -1
		for {
			ev, e = s.taskEvent(id, usage)
			if e != nil {
				return e
			}
			if ev.Header.Event == "task-finished" {
				if !ended || len(pcm) == 0 || len(words) == 0 {
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
				if len(out.Sentence.Words) != 0 {
					return errors.New("unexpected early TTS word timestamps")
				}
				if out.Original != text {
					return fmt.Errorf("TTS original text mismatch: requested=%q received=%q", text, out.Original)
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
				if len(out.Sentence.Words) != 0 {
					return errors.New("unexpected early word timestamps")
				}
				kind, b, e := s.read()
				if e != nil {
					return e
				}
				if kind != websocket.BinaryMessage || len(b) == 0 || len(b)%2 != 0 || len(pcm)+len(b) > maxBytes {
					return errors.New("missing or invalid TTS audio frame")
				}
				pcm = append(pcm, b...)
			case "sentence-end":
				if out.Original != text {
					return errors.New("missing final sentence text")
				}
				words, e = validateWords(text, out.Sentence.Words, int64(len(pcm)/48))
				if e != nil {
					return e
				}
				ended = true
			default:
				return errors.New("unexpected TTS output")
			}
		}
	})
	return
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
		if b < position || e <= b || e > int64(len(source)) || !ignorable(source[position:b]) || string(source[b:e]) != w.Text || *w.Begin < last || *w.End <= *w.Begin || *w.End > durationMS {
			return nil, errors.New("invalid or incomplete TTS timestamps")
		}
		words = append(words, Word{Text: string(source[position:e]), BeginMS: *w.Begin, EndMS: *w.End})
		position, last = e, *w.End
	}
	if !ignorable(source[position:]) {
		return nil, errors.New("TTS timestamp text coverage missing")
	}
	words[len(words)-1].Text += string(source[position:])
	return words, nil
}
