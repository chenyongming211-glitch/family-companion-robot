package realtimelive

import (
	"context"
	"sync"
	"time"
)

// The acknowledgement is no longer generated. It is a pool of pre-synthesized
// clips (see preface_pool.go), which is what lets it answer at 0.5s instead of
// 2.2s: generating a line cost about 0.6s to its first packet, so the previous
// design could not help a turn that was only going to take 1.5s.
//
// Filling the gap is not the same as covering it, and the difference is whether
// the answer is delayed. So the pool speaks in short clips with a seam between
// them, and before each clip it asks whether the answer has already taken the
// floor. If it has, the pool stops immediately: the clips already queued play
// out, and the answer follows them. The worst case a clip can add to a fast
// answer is one clip length, measured at 230-410ms for tier 1.
const (
	// A person answers in 0.2-0.5s. Below 0.5s the answer is usually already
	// speaking; above it, the silence has become something the user notices.
	prefaceFireDelay = 500 * time.Millisecond
	// Past this the clips stop. They are not the answer, and a listener who has
	// heard five thinking noises is already waiting, not being fooled.
	prefaceMaxCover = 5 * time.Second
	prefaceMaxClips = 6
)

// prefaceGate decides who owns the front of this turn's audio: the pool or the
// answer. Both sides take the same lock, so a clip can never be queued behind
// the answer's first packet -- which would make the robot say "嗯" after it had
// already answered.
type prefaceGate struct {
	mu       sync.Mutex
	answered bool
}

// answer takes the floor with the answer's first packet. Everything the pool
// queued before this call plays first, which is the intended overlap.
func (g *prefaceGate) answer(write func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answered = true
	return write()
}

// clip writes one clip, unless the answer got there first. The whole clip is
// queued under the lock so there is no window in which the pool believes it
// still owns the floor and the answer is already writing.
func (g *prefaceGate) clip(write func() error) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.answered {
		return false, nil
	}
	if err := write(); err != nil {
		return false, err
	}
	return true, nil
}

func (g *prefaceGate) taken() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.answered
}

func (s *Session) recordPreface(rt *liveTurn, fields map[string]any) {
	fields["id"] = rt.turn.ID
	// Observation must never break audio: a failed audit write is dropped here
	// rather than cancelling the turn.
	_ = s.record("preface", fields)
}

// prefaceAllowed reports whether this turn's answer speaks with cfg.Voice.
//
// Only route C does: it answers through the realtime endpoint and takes its
// voice from session.update. Routes A and B answer in the CosyVoice voice
// (longanyang) instead, while these clips are synthesized with cfg.Voice
// through the realtime endpoint. Prefacing there would put two different
// people into one answer, which is worse than the silence it was hiding.
func (s *Session) prefaceAllowed() bool {
	return s.voiceRoute != "A" && s.voiceRoute != "B"
}

// speakPreface writes acknowledgement clips into this turn's audio stream while
// the answer is still being produced. It gives up silently whenever the answer
// speaks first, the turn is cancelled, or the budget runs out.
func (s *Session) speakPreface(ctx context.Context, rt *liveTurn, gate *prefaceGate, pool *PrefacePool, encodeFrame func([]byte) ([]byte, error), markPrepared func(), out chan<- encodedPacket) {
	if pool == nil {
		return
	}
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

	outcome := "spoken"
	clips, samples, firstClipMS := 0, 0, int64(-1)
	var said []string
	defer func() {
		s.recordPreface(rt, map[string]any{
			"outcome":       outcome,
			"clips":         clips,
			"spoken_ms":     samples * 1000 / prefacePoolRate,
			"first_clip_ms": firstClipMS,
			"voice":         pool.voice,
			"lines":         said,
		})
	}()

	for {
		elapsed := time.Since(rt.stoppedAt).Milliseconds()
		if elapsed >= prefaceMaxCover.Milliseconds() || clips >= prefaceMaxClips {
			outcome = "budget"
			return
		}
		line := pool.Pick(prefaceTier(elapsed))
		if line == nil {
			outcome = "no_clip"
			return
		}
		pcm := line.pcm
		ok, err := gate.clip(func() error {
			for off := 0; off < len(pcm); off += prefacePoolPacket {
				packet, e := encodeFrame(pcm[off : off+prefacePoolPacket])
				if e != nil {
					return e
				}
				select {
				case out <- encodedPacket{packet: packet, preface: true}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			outcome = "error"
			return
		}
		if !ok {
			outcome = "cancelled_by_answer"
			return
		}
		if firstClipMS < 0 {
			firstClipMS = elapsed
		}
		// The first clip is the point where the consumer may start playing: it
		// is this turn's opening audio, not a separate response.
		markPrepared()
		clips++
		samples += len(pcm) / 2
		said = append(said, line.Text)

		// Let the clip play out before deciding whether another is needed. This
		// is what keeps the pool at most one clip ahead of what is audible, and
		// therefore the most the answer can be delayed by.
		wait := time.NewTimer(time.Duration(line.MS) * time.Millisecond)
		select {
		case <-wait.C:
		case <-ctx.Done():
			wait.Stop()
			return
		}
	}
}
