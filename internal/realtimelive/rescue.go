package realtimelive

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Search turns are slow in a way no prompt can fix: the provider's search
// plugin runs before the model is allowed to speak, so the first sound arrives
// 3.8-4.6s after the user stops.
//
// So the server covers the wait itself purely by elapsed time, without any
// keyword list guessing whether the model will search. After the main session
// opens, if no audio has arrived by rescueDelay, it synthesizes one short line
// in the background and holds playback until rescueSpeakAt (and at least
// rescueSpeakAfterOpen after the main session opened). If the model decides not
// to search, its first audio packet arrives within ~1.2-1.8s and cancels the
// cover before it ever speaks; if the model decides to search, the cover speaks
// right at the hold boundary while the search finishes.
const (
	rescueDelay          = 1150 * time.Millisecond
	rescueOpenMinWait    = 650 * time.Millisecond
	rescueSpeakAt        = 1850 * time.Millisecond
	rescueSpeakAfterOpen = 1350 * time.Millisecond
	rescueNoSearchDelay  = 1200 * time.Millisecond
	rescueDeadline       = 7 * time.Second
	// A rescue line is filler, not an answer. Anything longer means the model
	// started answering, which would then be repeated by the real answer. It
	// also has to stay short for a second reason: it plays before the answer,
	// so every extra second is a second the answer is held back.
	rescueMaxRunes = 9
	rescueMaxMS    = 1800
	rescueRate     = 24000
)

// rescueWait decides how long after the main session opens to wait before
// synthesizing a cover line in the background. It uses no keyword lists: when
// search is enabled, synthesis starts at rescueDelay from speech_stop (and at
// least rescueOpenMinWait after the main session opened). When search is
// disabled by config, it waits at least rescueNoSearchDelay after Open.
func rescueWait(stoppedAt, preparedAt time.Time, configuredSearch *bool) time.Duration {
	elapsed := preparedAt.Sub(stoppedAt)
	if configuredSearch != nil && *configuredSearch {
		return max(rescueDelay-elapsed, rescueOpenMinWait)
	}
	return max(rescueSpeakAt-elapsed, rescueNoSearchDelay)
}

// rescueSpeakHold keeps an early-synthesized cover line from speaking before
// both rescueSpeakAt (since speech_stop) and rescueSpeakAfterOpen (since the
// main session opened), so any non-search answer (~1.2-1.8s) wins outright and
// cancels the cover.
func rescueSpeakHold(stoppedAt, preparedAt, now time.Time) time.Duration {
	waitStop := rescueSpeakAt - now.Sub(stoppedAt)
	waitOpen := rescueSpeakAfterOpen - now.Sub(preparedAt)
	if wait := max(waitStop, waitOpen); wait > 0 {
		return wait
	}
	return 0
}

// The prompt asks for a line, not a phrase from a list. The wording rules are
// the same ones the product prompt uses for openings: own words, no template,
// no promise about how long it will take.
const rescuePrompt = "你在陪家里人说话。用户刚说了一句话，你正要查一下或者想一下才能答上来。" +
	"现在只做一件事：说一句很短的过渡话，让用户知道你在处理这件事。\n" +
	"要求：\n" +
	"1. 用日常口语，不超过六个字。\n" +
	"2. 这一句是这一轮现想的说法，不要套用现成的客套话，不要每次都用同一个句式。\n" +
	"3. 不要复述用户要你做的事：用户要你推荐，就别说“我帮你挑”“我给你推荐”；说更泛的说法。" +
	"这句话之后紧接着就要说真正的回答，重复一遍会让人听见同一句说两次。\n" +
	"4. 不要回答问题本身，不要说具体内容、数字或结论。\n" +
	"5. 不要用问句，不要承诺多久能给结果。\n" +
	"6. 只说这一句就停，不要接着往下说。"

// rescueLine asks for one opening line and returns its audio and transcript.
// Search is switched off: the line must not wait on a second round of lookup.
func rescueLine(ctx context.Context, cfg Config, userText string) ([]byte, string, error) {
	if cfg.URL == "" || cfg.Key == "" || cfg.Voice == "" {
		return nil, "", errors.New("rescue_config_incomplete")
	}
	off := false
	base := Config{URL: cfg.URL, Key: cfg.Key, Voice: cfg.Voice, Model: cfg.Model, Search: &off, Synthetic: true, MemoryOnly: true}
	callCtx, cancel := context.WithTimeout(ctx, rescueDeadline)
	defer cancel()
	n := NewNative(base, "")
	if err := n.Open(callCtx, rescuePrompt, false); err != nil {
		return nil, "", err
	}
	defer n.Close()
	var pcm []byte
	said := ""
	if err := n.StreamText(callCtx, "用户刚说："+userText, func(kind string, value []byte) error {
		switch kind {
		case "pcm":
			pcm = append(pcm, value...)
		case "text":
			said = string(value)
		}
		return nil
	}); err != nil {
		return nil, said, err
	}
	return pcm, said, nil
}

// answerClock records when the answer's first audio arrived. A cover line has
// to yield the moment that happens: playing the rest of it would put filler in
// front of an answer that is already ready to speak.
type answerClock struct {
	mu   sync.Mutex
	when time.Time
}

func (c *answerClock) arrive() {
	c.mu.Lock()
	if c.when.IsZero() {
		c.when = time.Now()
	}
	c.mu.Unlock()
}

func (c *answerClock) at() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.when
}

func (c *answerClock) arrived() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.when.IsZero()
}

// rescueResult carries the audio together with what it says. The text is
// recorded with the turn so a cover line can be checked without listening.
type rescueResult struct {
	pcm  []byte
	said string
}

// rescueAudit checks physical bounds and question punctuation on the generated
// cover line, without maintaining a hard-coded prefix/suffix keyword list.
func rescueAudit(said string, pcm []byte) (bool, string) {
	text := strings.TrimSpace(said)
	switch {
	case text == "":
		return false, "empty_text"
	case len(pcm) == 0:
		return false, "empty_audio"
	case utf8.RuneCountInString(text) > rescueMaxRunes:
		return false, "too_many_words"
	case strings.ContainsAny(text, "?？"):
		return false, "question"
	}
	if len(pcm)/2 > rescueMaxMS*rescueRate/1000 {
		return false, "too_long"
	}
	return true, ""
}

func rescueAcceptable(said string, pcm []byte) bool {
	ok, _ := rescueAudit(said, pcm)
	return ok
}
