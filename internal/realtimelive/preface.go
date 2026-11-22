package realtimelive

import (
	"context"
	"strings"
	"sync"
	"time"
)

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
	// Normal turns start speaking at about 1.1s (p50 1086ms measured), so this
	// fires on the slow tail while leaving ordinary turns untouched.
	prefaceFireDelay = 1100 * time.Millisecond
	// An acknowledgement longer than six seconds is no longer an
	// acknowledgement: the answer would have arrived behind it.
	prefaceMaxSamples = 24000 * 6
	// Generation is bounded so a stuck preface cannot hold the answer back.
	prefaceBudget = 5 * time.Second
)

// The words must be the model's, not a fixed phrase: a canned line is obvious by
// the third turn. What must not vary is the claim, so searching is passed in and
// a turn without search may only say it is thinking, never that it is looking
// something up.
const prefaceInstructions = "你是家庭里的语音陪伴助手。现在你还给不出答案，先跟用户说一句很短的话，让他知道你在做什么。" +
	"要求：只说一句话；不超过十五个字；用口语；每次换个说法；不要给答案；不要复述用户的话；不要提「AI」「助手」「模型」这些词。"

func prefaceInput(user string, searching bool) string {
	doing := "需要想一想"
	if searching {
		doing = "要联网查一下"
	}
	return "用户刚说：" + user + "\n你现在" + doing + "，先用一句话回应。"
}

// prefaceOrder serialises the acknowledgement and the answer. Only the answer's
// first packet waits: the encoded channel is FIFO, so once that packet is queued
// behind the acknowledgement every later packet follows it.
type prefaceOrder struct {
	mu      sync.Mutex
	claimed bool
	answer  bool
	front   chan struct{}
}

func newPrefaceOrder() *prefaceOrder {
	o := &prefaceOrder{front: make(chan struct{}, 1)}
	o.front <- struct{}{}
	return o
}

// claim reports whether an acknowledgement may still take the front of the turn.
// It is false once the answer has produced audio: the answer is already in the
// user's ear and an acknowledgement after it would be nonsense.
func (o *prefaceOrder) claim() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.claimed || o.answer {
		return false
	}
	o.claimed = true
	return true
}

// answerPacket holds the answer's first packet until the acknowledgement has
// written its last one.
func (o *prefaceOrder) answerPacket(ctx context.Context) error {
	o.mu.Lock()
	first := !o.answer
	o.answer = true
	o.mu.Unlock()
	if !first {
		return nil
	}
	select {
	case <-o.front:
		o.front <- struct{}{}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *prefaceOrder) acquire(ctx context.Context) error {
	select {
	case <-o.front:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *prefaceOrder) release() { o.front <- struct{}{} }

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
	if e := order.acquire(ctx); e != nil {
		return
	}
	defer order.release()
	s.mu.Lock()
	user := strings.TrimSpace(rt.turn.User)
	s.mu.Unlock()
	if user == "" {
		s.recordPreface(rt, map[string]any{"outcome": "empty_input"})
		return
	}
	searching := s.cfg.Search != nil && *s.cfg.Search
	cfg := s.cfg
	cfg.Search = nil
	cfg.Tools = nil
	n := NewNative(cfg, s.directory)
	defer n.Close()
	budget, cancel := context.WithTimeout(ctx, prefaceBudget)
	defer cancel()
	started := time.Now()
	if e := n.Open(budget, prefaceInstructions, false); e != nil {
		s.recordPreface(rt, map[string]any{"outcome": "error", "searching": searching})
		return
	}
	var pending []byte
	samples, spoken, prepared, firstPCM := 0, "", false, int64(-1)
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
		if !prepared {
			prepared = true
			firstPCM = time.Since(started).Milliseconds()
			markPrepared()
		}
		pending = append(pending, value...)
		for len(pending) >= 2880 {
			packet, e := encodeFrame(pending[:2880])
			if e != nil {
				return e
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
	e := n.StreamText(budget, prefaceInput(user, searching), emit)
	if len(pending) > 0 && e == nil {
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
	outcome := "spoken"
	if samples == 0 {
		outcome = "silent"
	}
	if e != nil {
		outcome = "error"
	}
	s.recordPreface(rt, map[string]any{"outcome": outcome, "searching": searching, "text": spoken,
		"samples": samples, "first_pcm_ms": firstPCM, "fired_after_ms": firedAfter})
}
