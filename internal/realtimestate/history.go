package realtimestate

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var digits = map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
var units = map[rune]int{'十': 10, '百': 100, '千': 1000}

func numberRune(c rune) bool {
	_, d := digits[c]
	_, u := units[c]
	return d || u || c >= '0' && c <= '9' || c >= '０' && c <= '９'
}
func NumberKey(raw string) [2]string {
	raw = norm.NFKC.String(raw)
	runes := []rune(raw)
	ascii := len(runes) > 0
	for _, c := range runes {
		if c < '0' || c > '9' {
			ascii = false
			break
		}
	}
	if ascii {
		return [2]string{"cardinal", raw}
	}
	var b strings.Builder
	allDigits := true
	for _, c := range runes {
		n, ok := digits[c]
		if !ok {
			allDigits = false
			break
		}
		b.WriteByte(byte('0' + n))
	}
	if allDigits {
		kind := "digit_sequence"
		if len(runes) == 1 {
			kind = "cardinal"
		}
		return [2]string{kind, b.String()}
	}
	total, pending, previous := 0, -1, 10000
	for _, c := range runes {
		if n, ok := digits[c]; ok {
			if pending != -1 && pending != 0 {
				return [2]string{"literal", raw}
			}
			pending = n
		} else if unit, ok := units[c]; ok && unit < previous {
			n := pending
			if n == -1 {
				n = 1
			}
			total += n * unit
			pending = -1
			previous = unit
		} else {
			return [2]string{"literal", raw}
		}
	}
	if pending > 0 && previous >= 100 && !(len(runes) >= 2 && (runes[len(runes)-2] == '零' || runes[len(runes)-2] == '〇')) {
		return [2]string{"literal", raw}
	}
	if pending > 0 {
		total += pending
	}
	return [2]string{"cardinal", strconv.Itoa(total)}
}

type token struct {
	key [2]string
	end int
}

func Tokens(text string) []token {
	runes := []rune(text)
	result := []token{}
	for i := 0; i < len(runes); {
		if numberRune(runes[i]) {
			end := i + 1
			for end < len(runes) && numberRune(runes[end]) {
				end++
			}
			result = append(result, token{NumberKey(string(runes[i:end])), end})
			i = end
			continue
		}
		for _, c := range cases.Fold().String(norm.NFKC.String(string(runes[i]))) {
			if !ignored(c) {
				result = append(result, token{[2]string{"char", string(c)}, i + 1})
			}
		}
		i++
	}
	return result
}
func EquivalentPrefix(generated, observed string) *string {
	draft, heard := Tokens(generated), Tokens(observed)
	if len(heard) == 0 || len(heard) > len(draft) {
		return nil
	}
	for i := range heard {
		if draft[i].key != heard[i].key {
			return nil
		}
	}
	end := draft[len(heard)-1].end
	runes := []rune(generated)
	for end < len(runes) && ignored(runes[end]) {
		end++
	}
	result := string(runes[:end])
	return &result
}

type ObservedWord struct {
	Text    string `json:"text"`
	BeginMS any    `json:"begin_ms"`
	EndMS   any    `json:"end_ms"`
}

func Observations(turn *Turn, marginMS ...float64) ([]ObservedWord, string) {
	margin := 120.0
	if len(marginMS) > 0 {
		margin = marginMS[0]
	}
	selected := []ObservedWord{}
	previous, expected, length := 0.0, 1, 0
	boundary := "no_recognition"
	playedMS, sentMS := float64(turn.Completed)/24, float64(turn.Sent)/24
	if turn.Tracker == nil {
		return selected, boundary
	}
	for _, id := range sortedSentences(turn.Tracker.Sentences) {
		sentence := turn.Tracker.Sentences[id]
		if id != expected || sentence["sentence_end"] != true {
			return selected, "recognition_gap"
		}
		expected++
		for _, raw := range list(sentence["words"]) {
			word := object(raw)
			start, sok := numeric(word["begin_time"])
			end, eok := numeric(word["end_time"])
			text, tok := word["text"].(string)
			if !sok || !eok || !(previous <= start && start <= end && end <= sentMS) || !tok {
				return selected, "invalid_timing"
			}
			guard := playedMS - margin
			if guard < 0 {
				guard = 0
			}
			if end > guard {
				return selected, "playback_guard"
			}
			if length+len([]rune(text)) > 2048 || len(selected) >= 512 {
				return selected, "context_limit"
			}
			length += len([]rune(text))
			selected = append(selected, ObservedWord{text, word["begin_time"], word["end_time"]})
			previous = end
			boundary = "conservative_end"
		}
	}
	return selected, boundary
}

type Evidence struct {
	SpokenText           *string        `json:"spoken_text"`
	ObservedText         *string        `json:"observed_text"`
	TextSource           string         `json:"text_source"`
	Alignment            string         `json:"alignment"`
	AlignedPrefix        *string        `json:"aligned_prefix"`
	CompletedMS          pythonFloat    `json:"completed_ms"`
	SentMS               pythonFloat    `json:"sent_ms"`
	Boundary             string         `json:"boundary"`
	ExactStopKnown       bool           `json:"exact_stop_known"`
	WordEndMS            any            `json:"word_end_ms"`
	TailWords            []ObservedWord `json:"tail_words"`
	MarginMS             int            `json:"margin_ms"`
	ClaimScope           string         `json:"claim_scope"`
	RecognitionMayDiffer bool           `json:"recognition_may_differ"`
}

func PlaybackEvidence(turn *Turn) Evidence {
	words, boundary := Observations(turn)
	var b strings.Builder
	for _, w := range words {
		b.WriteString(w.Text)
	}
	observed := optional(strings.TrimFunc(b.String(), func(c rune) bool { return unicode.IsSpace(c) || (c >= 0x1c && c <= 0x1f) }))
	var aligned *string
	if observed != nil {
		aligned = EquivalentPrefix(turn.Generated, *observed)
	}
	complete := turn.Ended && turn.Sent > 0 && turn.Completed == turn.Sent
	spoken := aligned
	if spoken == nil {
		spoken = observed
	}
	source := "none"
	if observed != nil {
		source = "output_asr"
	}
	if complete {
		spoken = optional(turn.Generated)
		source = "native_transcript_full_playback"
		boundary = "complete_response"
	}
	alignment := "unmatched"
	if aligned != nil {
		alignment = "equivalent_prefix"
	}
	var wordEnd any
	if len(words) > 0 {
		wordEnd = words[len(words)-1].EndMS
	}
	tail := words
	if len(tail) > 6 {
		tail = tail[len(tail)-6:]
	}
	return Evidence{spoken, observed, source, alignment, aligned, pythonFloat(float64(turn.Completed) / 24), pythonFloat(float64(turn.Sent) / 24), boundary, complete, wordEnd, tail, 120, "this_response_only", true}
}
func optional(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}
