package realtimestate

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
)

func ignored(c rune) bool { return unicode.In(c, unicode.P, unicode.Z, unicode.S) }
func Normalized(text string) string {
	var b strings.Builder
	for _, c := range text {
		if !ignored(c) {
			b.WriteString(cases.Fold().String(string(c)))
		}
	}
	return b.String()
}
func SelectPrefix(sentences []map[string]any, generated string, playedMS, marginMS float64, totalAudioMS *float64) map[string]any {
	unknown := func() map[string]any { return map[string]any{"status": "unknown", "prefix": nil} }
	if math.IsNaN(playedMS) || math.IsInf(playedMS, 0) || playedMS < 0 || marginMS < 0 {
		return unknown()
	}
	words := []map[string]any{}
	previous := 0.0
	for _, sentence := range sentences {
		if sentence["sentence_end"] != true {
			continue
		}
		for _, raw := range list(sentence["words"]) {
			word := object(raw)
			begin, bok := numeric(word["begin_time"])
			end, eok := numeric(word["end_time"])
			_, tok := word["text"].(string)
			if !bok || !eok || !(previous <= begin && begin <= end) || !tok || (totalAudioMS != nil && end > *totalAudioMS) {
				return unknown()
			}
			previous = end
			words = append(words, word)
		}
	}
	selected := []map[string]any{}
	var text strings.Builder
	for _, word := range words {
		end, _ := numeric(word["end_time"])
		if end > math.Max(0, playedMS-marginMS) {
			break
		}
		selected = append(selected, word)
		text.WriteString(word["text"].(string))
	}
	recognized := Normalized(text.String())
	if recognized == "" || !strings.HasPrefix(Normalized(generated), recognized) {
		return unknown()
	}
	count, endIndex := 0, 0
	runes := []rune(generated)
	length := len([]rune(recognized))
	for index, char := range runes {
		count += len([]rune(Normalized(string(char))))
		if count == length {
			endIndex = index + 1
			break
		}
	}
	for endIndex < len(runes) && Normalized(string(runes[endIndex])) == "" {
		endIndex++
	}
	return map[string]any{"status": "candidate", "prefix": string(runes[:endIndex]), "word_end_ms": selected[len(selected)-1]["end_time"], "margin_ms": marginMS, "selected_words": len(selected), "played_ms": playedMS}
}

type Arrival struct {
	ReceivedMS float64
	Sentences  map[int]map[string]any
}
type PrefixTracker struct {
	Generated    string
	TotalAudioMS float64
	Sentences    map[int]map[string]any
	Arrivals     []Arrival
}

func NewPrefixTracker(generated string, totalAudioMS float64) *PrefixTracker {
	return &PrefixTracker{Generated: generated, TotalAudioMS: totalAudioMS, Sentences: map[int]map[string]any{}, Arrivals: []Arrival{}}
}
func (p *PrefixTracker) Add(sentence map[string]any, receivedMS float64) {
	id, ok := strictInt(sentence["sentence_id"])
	if sentence["sentence_end"] != true || !ok || id < 1 || id > 256 {
		return
	}
	if p.Sentences == nil {
		p.Sentences = map[int]map[string]any{}
	}
	p.Sentences[int(id)] = sentence
	snapshot := make(map[int]map[string]any, len(p.Sentences))
	for k, v := range p.Sentences {
		snapshot[k] = v
	}
	p.Arrivals = append(p.Arrivals, Arrival{receivedMS, snapshot})
}
func (p *PrefixTracker) Ordered(values ...map[int]map[string]any) []map[string]any {
	source := p.Sentences
	if len(values) > 0 {
		source = values[0]
	}
	result := []map[string]any{}
	for i := 1; i <= len(source); i++ {
		sentence, ok := source[i]
		if !ok {
			return []map[string]any{}
		}
		result = append(result, sentence)
	}
	return result
}
func (p *PrefixTracker) Candidate(playedMS float64) map[string]any {
	return SelectPrefix(p.Ordered(), p.Generated, playedMS, 120, &p.TotalAudioMS)
}
func (p *PrefixTracker) Covers(playedMS float64, values ...map[int]map[string]any) bool {
	ordered := p.Ordered(values...)
	result := SelectPrefix(ordered, p.Generated, playedMS, 120, &p.TotalAudioMS)
	maximum := 0.0
	for _, sentence := range ordered {
		for _, raw := range list(sentence["words"]) {
			n, _ := numeric(object(raw)["end_time"])
			if n > maximum {
				maximum = n
			}
		}
	}
	return result["status"] == "candidate" && maximum >= playedMS-120
}
func (p *PrefixTracker) FirstUsableMS(playedMS float64) *float64 {
	for _, arrival := range p.Arrivals {
		if p.Covers(playedMS, arrival.Sentences) {
			n := arrival.ReceivedMS
			return &n
		}
	}
	return nil
}
func sortedSentences(values map[int]map[string]any) []int {
	keys := make([]int, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// ValidateReceipt retains the older probe's receipt contract (accepted need not be packet-aligned).
func ValidateReceipt(receipt map[string]any, identity any, sentSamples int64) (float64, error) {
	completed, cok := strictInt(receipt["completed_samples"])
	accepted, aok := strictInt(receipt["accepted_samples"])
	typ := receipt["type"]
	if (typ != "cut" && typ != "played") || receipt["id"] != identity || receipt["basis"] != "CoreAudio_dataPlayedBack" || receipt["microphone"] != false || !cok || !aok || !(0 <= completed && completed <= accepted && accepted <= sentSamples) || completed%1440 != 0 {
		return 0, errors.New("invalid_playback_receipt")
	}
	return float64(completed) * 1000 / 24000, nil
}
