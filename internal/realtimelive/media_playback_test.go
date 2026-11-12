package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/music"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Isolated source fixtures exercise the real route and Session output path.
// No model service, music network, persistent song data or hardware is used.
type mediaPlaybackSource struct {
	duration time.Duration
	stream   func(context.Context, func([]byte) error) (int64, error)
}

func (f *mediaPlaybackSource) Search(context.Context, music.Query) ([]music.Track, error) {
	return []music.Track{{ID: 7, Title: "黄昏", Artist: "周传雄", Duration: f.duration}}, nil
}
func (f *mediaPlaybackSource) Resolve(_ context.Context, row music.Track) (music.Resolved, error) {
	return music.Resolved{Track: row}, nil
}
func (f *mediaPlaybackSource) Stream(ctx context.Context, _ music.Resolved, emit func([]byte) error) (int64, error) {
	return f.stream(ctx, emit)
}
func mediaPlaybackSession(t *testing.T, source *mediaPlaybackSource) (*Session, *websocket.Conn) {
	t.Helper()
	s, c := expressionSession(t)
	s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi", SessionID: "media-test"}
	s.cfg.Music = music.New()
	s.music = &sessionMusic{source: source}
	prepareMusicPlan(t, s, "play", "黄昏", "周传雄")
	s.generate = func(context.Context, int64, string, []byte, func(string, []byte) error) error {
		return errors.New("ordinary_generator_must_not_supply_music")
	}
	return s, c
}
func startMediaReply(t *testing.T, s *Session, rt *liveTurn) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() { s.reply(rt, nil); close(done) }()
	t.Cleanup(func() {
		s.cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("reply not reaped")
		}
	})
	return done
}
func awaitMediaReply(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reply did not finish")
	}
}
func mediaReceipt(t *testing.T, s *Session, rt *liveTurn, n int64, state string) {
	t.Helper()
	w := s.wire.(*xiaozhiWire)
	raw, _ := json.Marshal(map[string]any{"type": "playback", "state": state, "session_id": w.scope, "playback_id": rt.turn.ID, "sample_rate": 24000, "played_samples": n, "valid": true, "basis": "i2s_dma", "interrupted": false})
	event, e := w.control(raw)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.receiveReceipt(event); e != nil {
		t.Fatal(e)
	}
}
func readMediaMessage(t *testing.T, c *websocket.Conn) (int, []byte, map[string]any) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	kind, b, e := c.ReadMessage()
	if e != nil {
		t.Fatal(e)
	}
	var event map[string]any
	if kind == websocket.TextMessage && json.Unmarshal(b, &event) != nil {
		t.Fatal("bad event")
	}
	return kind, b, event
}
func TestMediaReplyPhysicalCompletionNoASRExpressionOrText(t *testing.T) {
	for _, pcm := range []bool{true, false} {
		t.Run(fmt.Sprint(pcm), func(t *testing.T) {
			deadlineSeen := make(chan time.Duration, 1)
			source := &mediaPlaybackSource{duration: 3 * time.Second, stream: func(ctx context.Context, emit func([]byte) error) (int64, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					return 0, errors.New("missing_shared_deadline")
				}
				deadlineSeen <- time.Until(deadline)
				for range 25 {
					if e := emit(make([]byte, 2880)); e != nil {
						return 0, e
					}
				}
				return 25 * 1440, nil
			}}
			s, c := mediaPlaybackSession(t, source)
			s.wire.(*xiaozhiWire).pcm = pcm
			var expressions atomic.Int32
			s.selectExpression = func(context.Context, ExpressionInput) ExpressionResult {
				expressions.Add(1)
				return ExpressionResult{Emotion: "happy"}
			}
			rt := musicRequest(s, "播放周传雄的黄昏")
			done := startMediaReply(t, s, rt)
			frames := int64(0)
			for {
				kind, b, event := readMediaMessage(t, c)
				if kind == websocket.BinaryMessage {
					if pcm && len(b) != 2880 {
						t.Fatal("PCM frame changed", len(b))
					}
					frames++
					mediaReceipt(t, s, rt, frames*1440, "progress")
					continue
				}
				if event["type"] == "expression" {
					t.Fatal("music expression sent")
				}
				if event["type"] == "tts" && event["state"] == "stop" {
					break
				}
			}
			awaitMediaReply(t, done)
			select {
			case remaining := <-deadlineSeen:
				if remaining <= 30*time.Second || remaining > 33*time.Second {
					t.Fatal("deadline", remaining)
				}
			default:
				t.Fatal("music source not used")
			}
			want := int64(25)
			if !pcm {
				want++
			} // Existing Opus lookahead remains unchanged.
			s.mu.Lock()
			if frames != want || rt.turn.Sent != want*1440 || !rt.turn.Ended || rt.turn.PlaybackFinal || rt.turn.Generated != "" || rt.asr != nil || rt.expressionStarted || rt.expressionSent {
				t.Error("media state", frames, rt.turn, rt.asr, rt.expressionStarted)
			}
			s.mu.Unlock()
			if expressions.Load() != 0 {
				t.Fatal("extra expression planner", expressions.Load())
			}
			mediaReceipt(t, s, rt, frames*1440, "finished")
			s.mu.Lock()
			final := rt.turn.PlaybackFinal && rt.turn.PlaybackBasis == "i2s_dma"
			s.mu.Unlock()
			if !final {
				t.Fatal("physical completion not recorded")
			}
			next := musicRequest(s, "你好")
			history, e := s.settle(next.ctx, next)
			if e != nil || strings.Contains(history, "黄昏") {
				t.Fatal("media leaked into spoken context", history, e)
			}
		})
	}
}
func TestMediaReplyBoundedProducerCancelAndNextTurn(t *testing.T) {
	var emitted atomic.Int32
	started := make(chan struct{})
	reaped := make(chan struct{})
	source := &mediaPlaybackSource{duration: time.Minute, stream: func(ctx context.Context, emit func([]byte) error) (int64, error) {
		close(started)
		defer close(reaped)
		for i := 0; i < 1000; i++ {
			if e := emit(make([]byte, 2880)); e != nil {
				return int64(i) * 1440, e
			}
			emitted.Add(1)
		}
		return 1000 * 1440, nil
	}}
	s, client := mediaPlaybackSession(t, source)
	rt := musicRequest(s, "播放黄昏")
	done := startMediaReply(t, s, rt)
	select {
	case <-started:
	case <-done:
		t.Fatal("source was not used")
	case <-time.After(time.Second):
		t.Fatal("source did not start")
	}
	limit := time.Now().Add(time.Second)
	for emitted.Load() < 21 && time.Now().Before(limit) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	// Ten wire frames, one consumer-held frame, ten queued frames. The next
	// synchronous source callback must block rather than buffer the whole song.
	if got := emitted.Load(); got != 21 {
		t.Fatalf("unbounded media producer: completed emits=%d", got)
	}
	s.mu.Lock()
	sent := rt.turn.Sent
	asr := rt.asr
	s.mu.Unlock()
	if sent != 14400 || asr != nil {
		t.Fatal("credit/ASR", sent, asr)
	}
	if e := s.interrupt(); e != nil {
		t.Fatal(e)
	}
	awaitMediaReply(t, done)
	select {
	case <-reaped:
	case <-time.After(time.Second):
		t.Fatal("blocked source not canceled")
	}
	if ok, e := s.binary(rt, make([]byte, 2880)); ok || e != nil {
		t.Fatal("late media escaped seal", ok, e)
	}
	mediaReceipt(t, s, rt, sent, "finished")
	next := musicRequest(s, "下一首")
	if _, e := s.settle(next.ctx, next); e != nil {
		t.Fatal("next turn held after drain", e)
	}
	// A canceled music producer cannot poison the next ordinary reply/ASR.
	s.cfg.Music = nil
	s.generate = func(_ context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		if e := emit("pcm", make([]byte, 2880)); e != nil {
			return e
		}
		return emit("text", []byte("你好。"))
	}
	nextDone := startMediaReply(t, s, next)
	for {
		_, _, event := readMediaMessage(t, client)
		if event["type"] == "tts" && event["state"] == "stop" && event["playback_id"] == float64(next.turn.ID) {
			break
		}
	}
	awaitMediaReply(t, nextDone)
	s.mu.Lock()
	nextASR, generated, nextSent := next.asr, next.turn.Generated, next.turn.Sent
	s.mu.Unlock()
	if nextASR == nil || nextASR.InputSamples() != 960 || generated != "你好。" || nextSent != 1440 {
		t.Fatal("ordinary reply after music lost audio/text/ASR", nextASR, generated, nextSent)
	}
}
func TestMediaReplySourceFailureDrainsAndReaps(t *testing.T) {
	source := &mediaPlaybackSource{duration: time.Second, stream: func(ctx context.Context, emit func([]byte) error) (int64, error) {
		if e := emit(make([]byte, 2880)); e != nil {
			return 0, e
		}
		return 1440, errors.New("isolated_source_failure")
	}}
	s, c := mediaPlaybackSession(t, source)
	rt := musicRequest(s, "播放黄昏")
	done := startMediaReply(t, s, rt)
	for {
		_, _, event := readMediaMessage(t, c)
		if event["reason"] == "drain" {
			break
		}
	}
	awaitMediaReply(t, done)
	s.mu.Lock()
	cut, sent, ended := rt.turn.Cut, rt.turn.Sent, rt.turn.Ended
	s.mu.Unlock()
	if !cut || sent != 1440 || ended || s.ctx.Err() != nil {
		t.Fatal("failed source claimed completion/closed Session", cut, sent, ended, s.ctx.Err())
	}
	mediaReceipt(t, s, rt, sent, "finished")
	next := musicRequest(s, "你好")
	if _, e := s.settle(next.ctx, next); e != nil {
		t.Fatal(e)
	}
}
func TestMediaMemorySnapshotExcludesEntireTurn(t *testing.T) {
	s := &Session{ctx: context.Background(), memory: &sessionMemory{}, rec: recorder{owner: Ownership{SessionID: strings.Repeat("a", 32)}}}
	media := s.addTurn()
	media.media = true
	media.turn.User = "播放黄昏"
	media.turn.Generated = "不应保存的歌词"
	media.turn.Sent = 1440
	media.turn.Completed = 1440
	ordinary := s.addTurn()
	ordinary.turn.User = "你好"
	s.memory.mu.Lock()
	snapshot := s.memorySnapshotLocked()
	s.memory.mu.Unlock()
	if len(snapshot.Turns) != 1 || snapshot.Turns[0].User != "你好" {
		t.Fatal("media memory persisted", snapshot)
	}
}
func TestMediaReplyRejectsInvalidDurationBeforeSource(t *testing.T) {
	for _, duration := range []time.Duration{0, -time.Second, time.Millisecond, 21 * time.Minute} {
		t.Run(duration.String(), func(t *testing.T) {
			var calls atomic.Int32
			source := &mediaPlaybackSource{duration: duration, stream: func(context.Context, func([]byte) error) (int64, error) { calls.Add(1); return 0, nil }}
			s, _ := mediaPlaybackSession(t, source)
			rt := musicRequest(s, "播放黄昏")
			done := startMediaReply(t, s, rt)
			awaitMediaReply(t, done)
			if calls.Load() != 0 {
				t.Fatal("invalid duration started source", calls.Load())
			}
			s.mu.Lock()
			cut, begun, sent := rt.turn.Cut, rt.turn.Begun, rt.turn.Sent
			s.mu.Unlock()
			if !cut || begun || sent != 0 {
				t.Fatal("invalid media was not rejected before playback", cut, begun, sent)
			}
		})
	}
}

func TestMediaExpressionGuardRejectsLateReady(t *testing.T) {
	s, _ := expressionSession(t)
	var calls atomic.Int32
	s.selectExpression = func(context.Context, ExpressionInput) ExpressionResult {
		calls.Add(1)
		return ExpressionResult{Emotion: "happy"}
	}
	rt := musicRequest(s, "播放黄昏")
	s.mu.Lock()
	rt.media = true
	s.mu.Unlock()
	s.expressionContextReady(rt, `{"recent_turns":[]}`)
	s.wg.Wait()
	if calls.Load() != 0 {
		t.Fatal("media started expression planner", calls.Load())
	}
}

func TestMediaRoutingGuidanceUsesOrdinarySpeechWithoutFalseMusic(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			var streams, expressions atomic.Int32
			source := &mediaPlaybackSource{duration: time.Second, stream: func(context.Context, func([]byte) error) (int64, error) { streams.Add(1); return 0, nil }}
			s, c := mediaPlaybackSession(t, source)
			outcome := "needs_clarification"
			if unavailable {
				s.cfg.URL = "ws://127.0.0.1:1"
				outcome = "planner_unavailable"
			} else {
				prepareMusicPlan(t, s, "clarify", "", "")
			}
			s.selectExpression = func(context.Context, ExpressionInput) ExpressionResult {
				expressions.Add(1)
				return ExpressionResult{Emotion: "neutral"}
			}
			s.generate = func(ctx context.Context, _ int64, prompt string, _ []byte, emit func(string, []byte) error) error {
				if !strings.Contains(prompt, outcome) {
					t.Error("music fact missing from ordinary prompt", outcome)
				}
				if _, deadline := ctx.Deadline(); deadline {
					t.Error("ordinary generation unexpectedly has media deadline")
				}
				if e := emit("pcm", make([]byte, 2880)); e != nil {
					return e
				}
				return emit("text", []byte("请告诉我歌名。"))
			}
			rt := musicRequest(s, "放首歌")
			done := startMediaReply(t, s, rt)
			for {
				_, _, event := readMediaMessage(t, c)
				if event["type"] == "tts" && event["state"] == "stop" {
					break
				}
			}
			awaitMediaReply(t, done)
			s.wg.Wait()
			s.mu.Lock()
			generated, asr, media := rt.turn.Generated, rt.asr, rt.media
			s.mu.Unlock()
			if generated != "请告诉我歌名。" || asr == nil || media || streams.Load() != 0 {
				t.Fatal("ordinary speech changed", generated, asr, media, streams.Load())
			}
			if !unavailable && expressions.Load() != 0 {
				t.Fatal("handled music started unrelated expression planner")
			}
		})
	}
}
