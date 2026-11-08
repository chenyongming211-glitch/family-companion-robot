package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/protocol"
	"familyrobot/internal/realtimestate"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func expressionSession(t *testing.T) (*Session, *websocket.Conn) {
	t.Helper()
	server, client := testSocket(t)
	p, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err = p.WithName("团子")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(server, Config{ExpressionsEnabled: true, ASRURL: "ws://127.0.0.1:1", Synthetic: true}, t.TempDir(), p)
	w := newXiaozhiWire(server, 1, 60, "authenticated-scope")
	w.expression = true
	w.pcm = true
	s.wire = w
	t.Cleanup(func() {
		s.cancel()
		s.wg.Wait()
		s.mu.Lock()
		history := append([]*liveTurn(nil), s.history...)
		s.mu.Unlock()
		for _, rt := range history {
			if rt.asr != nil {
				rt.asr.Close()
			}
		}
	})
	return s, client
}

func expressionTranscript(s *Session, rt *liveTurn, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt.turn.User = text
	rt.transcriptOnce.Do(func() { close(rt.transcriptReady) })
	s.maybeStartExpressionLocked(rt)
}

func waitExpression(t *testing.T, s *Session, rt *liveTurn) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ready := rt.expressionPending != nil || rt.expressionSent
		s.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expression result not prepared")
}

func TestExpressionPreparedOrderStartEmotionAudio(t *testing.T) {
	s, c := expressionSession(t)
	var calls atomic.Int32
	s.selectExpression = func(_ context.Context, input ExpressionInput) ExpressionResult {
		calls.Add(1)
		if input.DeviceName != "团子" || input.CurrentTranscript != "好消息" || input.PlayedContext != `{"recent_turns":[]}` {
			t.Error(input)
		}
		return ExpressionResult{Emotion: "happy", Reason: "completed", RequestCount: 1}
	}
	rt := s.addTurn()
	expressionTranscript(s, rt, "好消息")
	s.expressionContextReady(rt, `{"recent_turns":[]}`)
	waitExpression(t, s, rt)
	// Results prepared before begin are held, with no L1 wire message.
	if err := s.send(map[string]any{"type": "ready"}); err != nil {
		t.Fatal(err)
	}
	if e := readFaceEvent(t, c); e["type"] != "hello" {
		t.Fatal(e)
	}
	s.generate = func(_ context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		return emit("pcm", make([]byte, 2880))
	}
	done := make(chan struct{})
	go func() { s.reply(rt, nil); close(done) }()
	start := readFaceEvent(t, c)
	emotion := readFaceEvent(t, c)
	if start["type"] != "tts" || start["state"] != "start" || emotion["type"] != "llm" || emotion["emotion"] != "happy" || emotion["playback_id"] != float64(rt.turn.ID) || emotion["session_id"] != "authenticated-scope" || len(emotion) != 4 {
		t.Fatal(start, emotion)
	}
	kind, _, err := c.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		t.Fatal(kind, err)
	}
	<-done
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestExpressionBlockedSelectorDoesNotDelayVoice(t *testing.T) {
	s, c := expressionSession(t)
	started := make(chan struct{})
	stopped := make(chan struct{})
	s.selectExpression = func(ctx context.Context, _ ExpressionInput) ExpressionResult {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ExpressionResult{Emotion: "neutral", Reason: "cancelled"}
	}
	s.generate = func(_ context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		return emit("pcm", make([]byte, 2880))
	}
	rt := s.addTurn()
	expressionTranscript(s, rt, "普通问题")
	done := make(chan struct{})
	go func() { s.reply(rt, nil); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("selector not started")
	}
	start := readFaceEvent(t, c)
	if start["type"] != "tts" {
		t.Fatal(start)
	}
	c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	kind, _, err := c.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		t.Fatal("voice waited for selector", kind, err)
	}
	<-done
	s.cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("selector cancellation leaked")
	}
}

func TestExpressionLateResultOnceAfterNormalSeal(t *testing.T) {
	s, c := expressionSession(t)
	gate := make(chan struct{})
	s.selectExpression = func(context.Context, ExpressionInput) ExpressionResult {
		<-gate
		return ExpressionResult{Emotion: "loving", Reason: "completed"}
	}
	rt := s.addTurn()
	expressionTranscript(s, rt, "陪我聊聊")
	s.expressionContextReady(rt, `{"recent_turns":[]}`)
	s.mu.Lock()
	rt.turn.Begun = true
	rt.turn.Ended = true
	if err := s.sendLocked(map[string]any{"type": "begin", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.sendLocked(map[string]any{"type": "end", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	s.mu.Unlock()
	readFaceEvent(t, c)
	readFaceEvent(t, c)
	close(gate)
	e := readFaceEvent(t, c)
	if e["emotion"] != "loving" {
		t.Fatal(e)
	}
	s.expressionContextReady(rt, `{"recent_turns":[]}`)
	expressionTranscript(s, rt, "再来的重复转写")
	if err := s.send(map[string]any{"type": "ready"}); err != nil {
		t.Fatal(err)
	}
	if e = readFaceEvent(t, c); e["type"] != "hello" {
		t.Fatal("duplicate expression", e)
	}
}

func TestExpressionRetiredResultsNeverSendAndCancelWork(t *testing.T) {
	for _, mode := range []string{"cancel", "new-turn", "close", "playback-final"} {
		t.Run(mode, func(t *testing.T) {
			s, c := expressionSession(t)
			started := make(chan struct{})
			returned := make(chan struct{})
			gate := make(chan struct{})
			s.selectExpression = func(ctx context.Context, _ ExpressionInput) ExpressionResult {
				close(started)
				<-ctx.Done()
				<-gate
				close(returned)
				return ExpressionResult{Emotion: "happy", Reason: "completed"}
			}
			rt := s.addTurn()
			expressionTranscript(s, rt, "问题")
			s.expressionContextReady(rt, `{"recent_turns":[]}`)
			<-started
			s.mu.Lock()
			rt.turn.Begun = true
			s.sendLocked(map[string]any{"type": "begin", "id": rt.turn.ID})
			s.mu.Unlock()
			readFaceEvent(t, c)
			switch mode {
			case "cancel":
				if err := s.interrupt(); err != nil {
					t.Fatal(err)
				}
				readFaceEvent(t, c)
				if err := s.receiveReceipt(map[string]any{"id": json.Number("1"), "played_samples": json.Number("0"), "basis": "i2s_dma", "valid": true, "interrupted": false, "state": "finished"}); err != nil {
					t.Fatal(err)
				}
			case "new-turn":
				s.addTurn()
			case "close":
				s.cancel()
			case "playback-final":
				s.mu.Lock()
				rt.turn.Ended = true
				s.sendLocked(map[string]any{"type": "end", "id": rt.turn.ID})
				s.mu.Unlock()
				readFaceEvent(t, c)
				if err := s.receiveReceipt(map[string]any{"id": json.Number("1"), "played_samples": json.Number("0"), "basis": "i2s_dma", "valid": true, "interrupted": false, "state": "finished"}); err != nil {
					t.Fatal(err)
				}
			}
			close(gate)
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("retired selector not cancelled")
			}
			s.wg.Wait()
			// A marker proves no retired result was queued; no timeout poisons the socket.
			s.mu.Lock()
			err := s.wire.sendEvent(map[string]any{"type": "ready"})
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if e := readFaceEvent(t, c); e["type"] != "hello" {
				t.Fatal("old result escaped", e)
			}
		})
	}
}

func TestExpressionContextAndTranscriptReadinessAreEventDriven(t *testing.T) {
	for _, order := range []string{"transcript-first", "context-first", "missing-transcript"} {
		t.Run(order, func(t *testing.T) {
			s, _ := expressionSession(t)
			var calls atomic.Int32
			s.selectExpression = func(context.Context, ExpressionInput) ExpressionResult {
				calls.Add(1)
				return ExpressionResult{Emotion: "neutral", Reason: "completed"}
			}
			rt := s.addTurn()
			if order == "transcript-first" {
				expressionTranscript(s, rt, "问题")
			}
			s.expressionContextReady(rt, `{"recent_turns":[]}`)
			if order == "context-first" {
				expressionTranscript(s, rt, "问题")
			}
			if order == "missing-transcript" {
				close(rt.done)
				s.wg.Wait()
				if calls.Load() != 0 {
					t.Fatal(calls.Load())
				}
				return
			}
			waitExpression(t, s, rt)
			s.expressionContextReady(rt, `{"recent_turns":[]}`)
			expressionTranscript(s, rt, "重复")
			if calls.Load() != 1 {
				t.Fatal(calls.Load())
			}
		})
	}
}

func TestExpressionUnnegotiatedSessionsHaveZeroCallsAndMessages(t *testing.T) {
	for _, mode := range []string{"legacy", "disabled", "face-state-only", "mac"} {
		t.Run(mode, func(t *testing.T) {
			s, c := expressionSession(t)
			var calls atomic.Int32
			s.selectExpression = func(context.Context, ExpressionInput) ExpressionResult {
				calls.Add(1)
				return ExpressionResult{Emotion: "happy", Reason: "completed"}
			}
			w := s.wire.(*xiaozhiWire)
			switch mode {
			case "legacy":
				w.expression = false
			case "disabled":
				s.cfg.ExpressionsEnabled = false
			case "face-state-only":
				w.expression = false
				w.face = true
			case "mac":
				s.wire = &macWire{ws: s.ws}
			}
			rt := s.addTurn()
			expressionTranscript(s, rt, "问题")
			s.expressionContextReady(rt, `{"recent_turns":[]}`)
			s.wg.Wait()
			if calls.Load() != 0 {
				t.Fatal(calls.Load())
			}
			if err := s.send(map[string]any{"type": "ready"}); err != nil {
				t.Fatal(err)
			}
			if e := readFaceEvent(t, c); e["type"] != "hello" && e["type"] != "ready" {
				t.Fatal(e)
			}
		})
	}
}

func TestExpressionNegotiationIndependentAndGatedByFlag(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, capability := range []bool{false, true} {
			for _, state := range []bool{false, true} {
				t.Run(strings.Join([]string{map[bool]string{true: "enabled", false: "disabled"}[enabled], map[bool]string{true: "expression", false: "legacy"}[capability], map[bool]string{true: "state", false: "no-state"}[state]}, "/"), func(t *testing.T) {
					s := NewServer(Config{ExpressionsEnabled: enabled}, nil, t.TempDir(), "test")
					s.runDevice = func(session *Session) { session.send(map[string]any{"type": "ready"}) }
					h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
					defer h.Close()
					c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}})
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					hello := protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true, "face_expression_v1": capability, "face_state_v1": state}}
					c.WriteJSON(hello)
					event := readFaceEvent(t, c)
					features := object(event["features"])
					if (features["face_expression_v1"] == true) != (enabled && capability) || (features["face_state_v1"] == true) != state {
						t.Fatal(event)
					}
				})
			}
		}
	}
}

func TestExpressionWireValidatesBindingAndAllowsNormalSealedTail(t *testing.T) {
	s, c := expressionSession(t)
	w := s.wire.(*xiaozhiWire)
	send := func(id int64, emotion string) error {
		return w.sendEvent(map[string]any{"type": "expression", "id": id, "emotion": emotion, "session_id": "untrusted-scope"})
	}
	if err := send(1, "happy"); err == nil {
		t.Fatal("unbegun expression accepted")
	}
	w.sendEvent(map[string]any{"type": "begin", "id": int64(1)})
	readFaceEvent(t, c)
	for _, name := range []string{"laughing", "thinking", "sleep", "", "Happy"} {
		if err := send(1, name); err == nil {
			t.Fatal("noncanonical accepted", name)
		}
	}
	if err := send(2, "happy"); err == nil {
		t.Fatal("future expression accepted")
	}
	w.sendEvent(map[string]any{"type": "end", "id": int64(1)})
	readFaceEvent(t, c)
	if err := send(1, "neutral"); err != nil {
		t.Fatal("normal seal is not physical finish", err)
	}
	e := readFaceEvent(t, c)
	if len(e) != 4 || e["session_id"] != "authenticated-scope" || e["playback_id"] != float64(1) || e["emotion"] != "neutral" {
		t.Fatal(e)
	}
	w.sendEvent(map[string]any{"type": "drain", "id": int64(1), "sent": int64(0)})
	readFaceEvent(t, c)
	if err := send(1, "happy"); err == nil {
		t.Fatal("draining expression accepted")
	}
	w.expression = false
	if err := send(1, "happy"); err == nil {
		t.Fatal("unnegotiated expression accepted")
	}
}

func TestExpressionNeutralFallbackConsumesSingleSelection(t *testing.T) {
	s, c := expressionSession(t)
	var calls atomic.Int32
	s.selectExpression = func(context.Context, ExpressionInput) ExpressionResult {
		calls.Add(1)
		return ExpressionResult{Emotion: "laughing", Reason: "provider-fallback"}
	}
	rt := s.addTurn()
	expressionTranscript(s, rt, "问题")
	s.expressionContextReady(rt, `{"recent_turns":[]}`)
	waitExpression(t, s, rt)
	s.mu.Lock()
	rt.turn.Begun = true
	s.sendLocked(map[string]any{"type": "begin", "id": rt.turn.ID})
	err := s.sendExpressionLocked(rt)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	readFaceEvent(t, c)
	if e := readFaceEvent(t, c); e["emotion"] != "neutral" {
		t.Fatal(e)
	}
	s.expressionContextReady(rt, `{"recent_turns":[]}`)
	expressionTranscript(s, rt, "不同文字也不能重选")
	s.mu.Lock()
	err = s.sendExpressionLocked(rt)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s.send(map[string]any{"type": "ready"})
	if e := readFaceEvent(t, c); e["type"] != "hello" || calls.Load() != 1 {
		t.Fatal(e, calls.Load())
	}
}

func TestExpressionLateProviderTranscriptTriggersThroughListener(t *testing.T) {
	s, _ := expressionSession(t)
	provider, feed := testSocket(t)
	s.listener.conn = feed
	defer s.listener.Close()
	stop := context.AfterFunc(s.ctx, func() { feed.Close() })
	defer stop()
	selected := make(chan ExpressionInput, 1)
	s.selectExpression = func(_ context.Context, input ExpressionInput) ExpressionResult {
		selected <- input
		return ExpressionResult{Emotion: "neutral", Reason: "completed"}
	}
	rt := s.addTurn()
	s.items["current-item"] = rt
	history, err := s.settle(rt.ctx, rt)
	if err != nil {
		t.Fatal(err)
	}
	s.expressionContextReady(rt, history)
	done := make(chan struct{})
	go func() { defer close(done); s.listen() }()
	provider.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "current-item", "transcript": "迟到的原话"})
	select {
	case input := <-selected:
		if input.CurrentTranscript != "迟到的原话" {
			t.Fatal(input)
		}
	case <-time.After(time.Second):
		t.Fatal("final ASR after settle did not trigger selector")
	}
	provider.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "current-item", "transcript": "重复原话"})
	s.cancel()
	<-done
	select {
	case <-selected:
		t.Fatal("duplicate final ASR triggered a second selection")
	default:
	}
}
