package realtimestate

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
)

// ConfirmedSegment is a complete response or an independently confirmed short
// clause. Times use the decoded, delivered 24kHz playback timeline.
type ConfirmedSegment struct {
	Text           string
	BeginMS, EndMS float64
}

// PlayedSegments contains bounded, independently confirmed response text.
// PlayedSamples is the canonical receipt count, not an ASR-derived estimate.
// Complete retains PlaybackEvidence's whole-playback rule. Unknown is true for
// partial playback, or when an absent/oversize transcript cannot be included.
type PlayedSegments struct {
	Segments      []ConfirmedSegment
	PlayedSamples int64
	Complete      bool
	Unknown       bool
}

// ConfirmedPlayback extracts evidence without mutating the turn or its tracker.
// The caller owns serialization, as with the other Turn state operations.
func ConfirmedPlayback(turn *Turn) PlayedSegments {
	result := PlayedSegments{Unknown: true}
	if turn == nil || turn.Sent < 0 || turn.Completed <= 0 || turn.Completed > turn.Sent {
		return result
	}
	result.PlayedSamples = turn.Completed
	result.Complete = turn.Ended && turn.Sent > 0 && turn.Completed == turn.Sent && (turn.PlaybackBasis != "i2s_dma" || turn.PlaybackFinal)
	// The observation contract caps text at 2048 runes. Do not truncate a clause
	// or infer anchor uniqueness from only a prefix of a larger transcript.
	if utf8.RuneCountInString(turn.Generated) > 2048 || turn.Generated == "" {
		return result
	}
	if result.Complete {
		result.Segments = []ConfirmedSegment{{turn.Generated, 0, float64(turn.Sent) / 24}}
		result.Unknown = false
		return result
	}

	words, _ := Observations(turn)
	draft := playbackClauses(turn.Generated, turn.Ended)
	observed := observedPlaybackClauses(turn, words)
	// Both sides must identify a whole clause exactly once. In particular, a
	// unique substring is not enough: "吃药" cannot anchor "不要吃药".
	positions := make(map[string]int, len(draft))
	for i, clause := range draft {
		if _, exists := positions[clause.key]; exists {
			positions[clause.key] = -1
		} else {
			positions[clause.key] = i
		}
	}
	counts := make(map[string]int, len(observed))
	for _, clause := range observed {
		counts[clause.key]++
	}
	type anchor struct{ observed, draft int }
	anchors := []anchor{}
	for i, clause := range observed {
		if position, exists := positions[clause.key]; exists && position >= 0 && counts[clause.key] == 1 {
			anchors = append(anchors, anchor{i, position})
		}
	}
	// Reject every anchor participating in an inversion. Independent clauses
	// before and after a crossed pair can still be kept without choosing an order.
	crossed := make([]bool, len(anchors))
	for i, a := range anchors {
		for j := i + 1; j < len(anchors); j++ {
			if a.draft > anchors[j].draft {
				crossed[i], crossed[j] = true, true
			}
		}
	}
	for i, a := range anchors {
		if !crossed[i] {
			clause := observed[a.observed]
			result.Segments = append(result.Segments, ConfirmedSegment{draft[a.draft].text, clause.beginMS, clause.endMS})
		}
	}
	return result
}

type playbackClause struct {
	text, key      string
	start, end     int // byte offsets within the original text
	beginMS, endMS float64
}

// A clause needs punctuation, or a final transcript/sentence boundary. A word
// selected immediately before the playback guard is not a clause boundary.
func playbackClauses(text string, final bool) []playbackClause {
	clauses := []playbackClause{}
	appendClause := func(start, end int) {
		clause := strings.TrimSpace(text[start:end])
		if key := playbackClauseKey(clause); key != "" {
			clauses = append(clauses, playbackClause{text: clause, key: key, start: start, end: end})
		}
	}
	start, punctuated := 0, false
	for position, c := range text {
		if punctuated && !playbackSpeechPunctuation(c) && !unicode.IsSpace(c) {
			appendClause(start, position)
			start, punctuated = position, false
		}
		if strings.ContainsRune("。！？；，、：.!?;,:\n\r", c) && !playbackNumericSeparator(text, position, c) {
			punctuated = true
		}
	}
	if punctuated || final {
		appendClause(start, len(text))
	}
	return clauses
}

func playbackSpeechPunctuation(c rune) bool {
	return strings.ContainsRune("。！？；，、：.!?;,:\"", c) || unicode.In(c, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf)
}

// Chinese full stops/enumeration marks always end a clause. Dot/comma guards
// apply only between decimal digits; colon guards also cover literal Chinese
// number tokens in ratios/times. Spaces do not change the separator's literal
// value, and no numeric spelling or punctuation variant is made equivalent.
func playbackNumericSeparator(text string, position int, c rune) bool {
	if !strings.ContainsRune(".,:．，：", c) || position == 0 {
		return false
	}
	previous, _ := utf8.DecodeLastRuneInString(strings.TrimSpace(text[:position]))
	after := strings.TrimSpace(text[position+utf8.RuneLen(c):])
	next, width := utf8.DecodeRuneInString(after)
	if c == ':' || c == '：' {
		if strings.ContainsRune("+-−＋－负正", next) {
			next, _ = utf8.DecodeRuneInString(strings.TrimSpace(after[width:]))
		}
		return (unicode.IsDigit(previous) || numberRune(previous)) && (unicode.IsDigit(next) || numberRune(next))
	}
	return unicode.IsDigit(previous) && unicode.IsDigit(next)
}

// Ignore speech punctuation/case, but retain signs, percentages, fractions and
// separators inside numbers. The older prefix normalization deliberately has a
// different contract and must not be used to erase these distinctions here.
func playbackClauseKey(text string) string {
	var key strings.Builder
	for position, c := range text {
		if unicode.IsSpace(c) || playbackSpeechPunctuation(c) && !playbackNumericSeparator(text, position, c) {
			continue
		}
		key.WriteString(cases.Fold().String(string(c)))
	}
	return key.String()
}

// OutputASR stores punctuation separately from word text. Only validated speech
// separators may create boundaries; this field must never supply missing words
// or numeric symbols. Its glyph is replaced by a forced boundary so metadata
// between numeric words cannot construct a ratio, decimal, or other value.
func playbackASRBoundary(value any) string {
	punctuation, ok := value.(string)
	if !ok || len(punctuation) > 32 {
		return ""
	}
	boundary := false
	for _, c := range punctuation {
		if unicode.IsSpace(c) || unicode.In(c, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf) || c == '"' {
			continue
		}
		switch c {
		case '.', '。', ',', '，', '、', ':', '：', ';', '；', '!', '！', '?', '？':
			boundary = true
		default:
			return ""
		}
	}
	if boundary {
		return "\n"
	}
	return ""
}

func observedPlaybackClauses(turn *Turn, words []ObservedWord) []playbackClause {
	clauses := []playbackClause{}
	if turn.Tracker == nil || len(words) == 0 {
		return clauses
	}
	offset := 0
	for _, id := range sortedSentences(turn.Tracker.Sentences) {
		if offset == len(words) {
			break
		}
		// Observations already validated this contiguous prefix. Only a fully
		// selected ASR sentence may supply a punctuation-free clause boundary.
		rawWords := list(turn.Tracker.Sentences[id]["words"])
		count := len(rawWords)
		available := len(words) - offset
		final := count <= available
		if count > available {
			count = available
		}
		type wordSpan struct {
			start, end     int
			beginMS, endMS float64
		}
		spans := make([]wordSpan, 0, count)
		var text strings.Builder
		for i, word := range words[offset : offset+count] {
			start := text.Len()
			text.WriteString(word.Text)
			text.WriteString(playbackASRBoundary(object(rawWords[i])["punctuation"]))
			begin, _ := numeric(word.BeginMS)
			end, _ := numeric(word.EndMS)
			spans = append(spans, wordSpan{start, text.Len(), begin, end})
		}
		for _, clause := range playbackClauses(text.String(), final) {
			first, timed := true, true
			for _, span := range spans {
				if span.end > clause.start && span.start < clause.end {
					if span.endMS <= span.beginMS {
						timed = false
					}
					if first {
						clause.beginMS, first = span.beginMS, false
					}
					clause.endMS = span.endMS
				}
			}
			if timed && !first {
				clauses = append(clauses, clause)
			}
		}
		offset += count
		if !final {
			break
		}
	}
	return clauses
}
