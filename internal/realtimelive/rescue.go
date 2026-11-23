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
// 3.8-4.6s after the user stops. Measured 2026-11-23, the model had already
// written an opening line in the transcript for one of those turns -- the audio
// was dropped when the search interrupted it. Telling the model to say
// something first cannot help, because it has nothing to speak into yet.
//
// So the server covers the wait itself. If no audio has arrived after
// rescueDelay, it asks for one short line, synthesized through the same
// endpoint and the same voice as the answer, and plays that while the search
// finishes. The line is written for this turn only; nothing is cached or
// reused. If the answer's first packet arrives first, the line is dropped
// before it is ever spoken.
const (
	rescueDelay    = 1500 * time.Millisecond
	rescueDeadline = 7 * time.Second
	// A rescue line is filler, not an answer. Anything longer means the model
	// started answering, which would then be repeated by the real answer. It
	// also has to stay short for a second reason: it plays before the answer,
	// so every extra second is a second the answer is held back.
	rescueMaxRunes = 9
	rescueMaxMS    = 1800
	rescueRate     = 24000
)

// Spoken turns often carry no punctuation, so a question has to be recognised by
// its shape. Rejecting a good line only costs silence; playing a question or a
// half answer makes the user hear the same thing twice, which is worse.
var rescueQuestionWords = []string{"？", "?", "你想", "你要", "你是", "哪一", "哪个", "什么", "怎么", "吗", "呢", "要不要", "是不是"}

// The prompt asks for a line, not a phrase from a list. The wording rules are
// the same ones the product prompt uses for openings: own words, no template,
// no promise about how long it will take.
const rescuePrompt = "你在陪家里人说话。用户刚说了一句话，你正要查一下或者想一下才能答上来。" +
	"现在只做一件事：说一句很短的过渡话，让用户知道你在处理这件事。\n" +
	"要求：\n" +
	"1. 用日常口语，不超过六个字。\n" +
	"2. 这一句是这一轮现想的说法，不要套用现成的客套话，不要每次都用同一个句式。\n" +
	"3. 不要回答问题本身，不要说具体内容、数字或结论。\n" +
	"4. 不要用问句，不要承诺多久能给结果。\n" +
	"5. 只说这一句就停，不要接着往下说。"

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

// rescueAcceptable rejects anything that is not a short statement. A line that
// asks a question or runs long is the model answering instead of covering, and
// playing it would make the user hear the answer twice.
func rescueAcceptable(said string, pcm []byte) bool {
	text := strings.TrimSpace(said)
	if text == "" || len(pcm) == 0 {
		return false
	}
	if utf8.RuneCountInString(text) > rescueMaxRunes {
		return false
	}
	for _, word := range rescueQuestionWords {
		if strings.Contains(text, word) {
			return false
		}
	}
	return len(pcm)/2 <= rescueMaxMS*rescueRate/1000
}
