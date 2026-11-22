package realtimelive

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// errPrefaceLost means the answer produced its first packet first. It stops the
// acknowledgement stream without being treated as a failure.
var errPrefaceLost = errors.New("preface_lost_to_answer")

// A turn that needs search, memory selection or a slow upstream reply leaves the
// user listening to nothing for two to nine seconds. Measured 2026-11-22:
// 6148ms and 2033ms on search turns, 2671ms on memory turns, 8127ms on a turn
// whose answer was fourteen characters. Only the first two shapes can be guessed
// from the transcript, so the acknowledgement is fired on elapsed time instead:
// if the answer has not produced audio by prefaceFireDelay after the user
// stopped speaking, it never will be fast.
//
// The acknowledgement is not a second turn. It is the first second of this
// turn's audio, written into the same channel so the ledger, the generation
// guard and the playback credit window keep seeing one continuous stream.
const (
	// An acknowledgement costs about 0.6s to produce its first packet, so it only
	// pays off when the answer would have taken longer than that. Firing at the
	// normal-turn median (1.1s, measured) made it fire on ordinary turns and, since
	// the answer waits behind it, delayed them. 2.2s sits above the ordinary tail
	// and below the turns this exists for (2.7s memory, 6.1s search, 8.1s stall).
	prefaceFireDelay = 2200 * time.Millisecond
	// An acknowledgement longer than six seconds is no longer an
	// acknowledgement: the answer would have arrived behind it.
	prefaceMaxSamples = 24000 * 6
	// Generation is bounded so a stuck preface cannot hold the answer back.
	prefaceBudget = 5 * time.Second
	// How many spoken lines are carried forward as lines to avoid.
	prefaceRecall = 4
)

// The words must be the model's, not a fixed phrase: a canned line is obvious by
// the third turn. What must not vary is the claim. The previous version passed
// the search switch in and told the model it was going online, which made every
// acknowledgement a promise to search: asked its own name, the robot answered
// "I'll go look that up". The switch says search is allowed this turn, not that
// a search is happening, so nothing may be claimed about it. Say only that you
// are getting to it, in your own words.
const prefaceInstructions = "你是家里的语音陪伴助手，正在准备回答用户刚说的话。先随口应一句，让用户知道你已经听到了、马上就说。" +
	"要求：只说一句话，十五字以内；口语，像真人顺口应的那种；" +
	"只表达「我在，马上说」，不要碰到问题的任何内容，不要给答案，也不要猜答案的方向；" +
	"不要说去查、去搜、去网上看、去翻资料这类话，因为你并不知道自己会不会去查；" +
	"不要提「AI」「助手」「模型」这些词；不要用「好的」「收到」这种客服腔；" +
	"不要用「我这就…」「让我…一下」这种句式，也不要每句都以「我」开头。"

// The user's words are here so the acknowledgement can fit the moment, not so it
// can answer them. The lines already used go in as a concrete list: a rule like
// "say it differently each time" is something the model cannot check, whereas a
// list of the exact sentences to avoid is.
func prefaceInput(user string, avoid []string) string {
	var b strings.Builder
	b.WriteString("用户刚说：")
	b.WriteString(user)
	b.WriteString("\n你先应一声，马上就说。")
	if len(avoid) > 0 {
		b.WriteString("\n你已经说过下面这些，这次必须换一种完全不同的说法，不要只换几个字或换个语序：")
		for _, line := range avoid {
			b.WriteString("\n- ")
			b.WriteString(line)
		}
	}
	return b.String()
}

// rememberPreface keeps the last few lines so the next one can be told to avoid
// them. Four is enough to cover the repeats a listener notices and short enough
// to stay out of the way of the audio path.
func (s *Session) rememberPreface(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.prefaceSaid {
		if old == text {
			return
		}
	}
	s.prefaceSaid = append(s.prefaceSaid, text)
	if len(s.prefaceSaid) > prefaceRecall {
		s.prefaceSaid = s.prefaceSaid[len(s.prefaceSaid)-prefaceRecall:]
	}
}

// prefaceOrder decides who owns the front of the turn: the acknowledgement or
// the answer. Whoever has a packet ready first wins.
//
// The earlier version took the front when the timer fired, before generating
// anything, so every turn that triggered waited for a whole acknowledgement
// request before its answer could be queued: asked "what is your name", the
// answer arrived at 1.5s but the user heard the first packet at 2.5s, because a
// promise to look it up was being generated in front of it. Now the front is
// taken when the first packet is actually ready, so an answer that shows up
// during generation still speaks first and drops the acknowledgement entirely.
const (
	orderIdle = iota
	orderPreface
	orderAnswer
)

type prefaceOrder struct {
	mu      sync.Mutex
	state   int
	claimed bool
	// flushed closes once the acknowledgement has written its last packet. Only
	// the answer reads it, and only when the acknowledgement got there first.
	flushed chan struct{}
}

func newPrefaceOrder() *prefaceOrder {
	return &prefaceOrder{flushed: make(chan struct{})}
}

// claim reserves this turn's one attempt. It says nothing about who speaks: the
// answer may still take the front while the request is in flight.
func (o *prefaceOrder) claim() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.claimed || o.state != orderIdle {
		return false
	}
	o.claimed = true
	return true
}

// begin takes the front for the acknowledgement. Called with the first packet in
// hand, never before: an answer that arrives during generation must outrank it.
func (o *prefaceOrder) begin() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != orderIdle {
		return false
	}
	o.state = orderPreface
	return true
}

// finish hands the front over, whether the acknowledgement spoke or gave up.
func (o *prefaceOrder) finish() {
	o.mu.Lock()
	if o.state == orderPreface {
		o.state = orderAnswer
	}
	o.mu.Unlock()
	close(o.flushed)
}

// answerPacket reports whether the answer may write its first packet now. When
// the acknowledgement is already speaking it waits for the handover; the encoded
// channel is FIFO, so that one wait is enough for the whole answer.
func (o *prefaceOrder) answerPacket(ctx context.Context) (bool, error) {
	o.mu.Lock()
	wait := o.state == orderPreface
	if o.state == orderIdle {
		o.state = orderAnswer
	}
	o.mu.Unlock()
	if !wait {
		return true, nil
	}
	select {
	case <-o.flushed:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (s *Session) recordPreface(rt *liveTurn, fields map[string]any) {
	fields["id"] = rt.turn.ID
	// Observation must never break audio: a failed audit write is dropped here
	// rather than cancelling the turn.
	_ = s.record("preface", fields)
}

// speakPreface writes one short acknowledgement into this turn's audio stream
// when the answer is late. It gives up silently whenever the answer speaks
// first, the turn is cancelled, or there is no transcript to answer yet.
func (s *Session) speakPreface(ctx context.Context, rt *liveTurn, order *prefaceOrder, encodeFrame func([]byte) ([]byte, error), markPrepared func(), out chan<- encodedPacket) {
	delay := time.Until(rt.stoppedAt.Add(prefaceFireDelay))
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return
	}
	if !order.claim() {
		s.recordPreface(rt, map[string]any{"outcome": "cancelled_by_answer"})
		return
	}
	firedAfter := time.Since(rt.stoppedAt).Milliseconds()
	s.mu.Lock()
	user := strings.TrimSpace(rt.turn.User)
	avoid := append([]string(nil), s.prefaceSaid...)
	s.mu.Unlock()
	if user == "" {
		order.finish()
		s.recordPreface(rt, map[string]any{"outcome": "empty_input"})
		return
	}
	searchEnabled := s.cfg.Search != nil && *s.cfg.Search
	cfg := s.cfg
	cfg.Search = nil
	cfg.Tools = nil
	n := NewNative(cfg, s.directory)
	defer n.Close()
	budget, cancel := context.WithTimeout(ctx, prefaceBudget)
	defer cancel()
	started := time.Now()
	if e := n.Open(budget, prefaceInstructions, false); e != nil {
		order.finish()
		s.recordPreface(rt, map[string]any{"outcome": "error", "search_enabled": searchEnabled})
		return
	}
	var pending []byte
	samples, spoken, prepared, firstPCM := 0, "", false, int64(-1)
	// The front is taken with the first packet in hand, not before the request,
	// so an answer that arrives while this is generating still wins and the
	// acknowledgement is dropped whole rather than delaying it.
	begun, lost := false, false
	defer func() {
		if begun {
			order.finish()
		}
	}()
	emit := func(kind string, value []byte) error {
		if kind == "text" {
			// The transcript is evidence for the log, not this turn's reply
			// text: rt.turn.Generated stays the answer.
			spoken = string(value)
			return nil
		}
		if samples >= prefaceMaxSamples {
			return nil
		}
		pending = append(pending, value...)
		for len(pending) >= 2880 {
			if !begun {
				if !order.begin() {
					lost = true
					return errPrefaceLost
				}
				begun = true
			}
			packet, e := encodeFrame(pending[:2880])
			if e != nil {
				return e
			}
			if !prepared {
				prepared = true
				firstPCM = time.Since(started).Milliseconds()
				markPrepared()
			}
			select {
			case out <- encodedPacket{packet: packet, preface: true}:
			case <-ctx.Done():
				return ctx.Err()
			}
			pending = pending[2880:]
			samples += 1440
		}
		return nil
	}
	e := n.StreamText(budget, prefaceInput(user, avoid), emit)
	if len(pending) > 0 && e == nil {
		if !begun {
			if !order.begin() {
				lost = true
			} else {
				begun = true
			}
		}
		if begun && !lost {
			chunk := make([]byte, 2880)
			copy(chunk, pending)
			if packet, er := encodeFrame(chunk); er == nil {
				select {
				case out <- encodedPacket{packet: packet, preface: true}:
					samples += 1440
				case <-ctx.Done():
				}
			}
		}
	}
	// Losing to the answer is the designed outcome on fast turns, not a fault.
	if lost || errors.Is(e, errPrefaceLost) {
		s.recordPreface(rt, map[string]any{"outcome": "cancelled_by_answer", "fired_after_ms": firedAfter})
		return
	}
	outcome := "spoken"
	if samples == 0 {
		outcome = "silent"
	}
	if e != nil {
		outcome = "error"
	}
	if outcome == "spoken" {
		s.rememberPreface(spoken)
	}
	s.recordPreface(rt, map[string]any{"outcome": outcome, "search_enabled": searchEnabled, "text": spoken,
		"samples": samples, "first_pcm_ms": firstPCM, "fired_after_ms": firedAfter})
}
