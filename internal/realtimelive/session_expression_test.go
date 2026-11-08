package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/protocol"
	"familyrobot/internal/realtimestate"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestExpressionFinalProviderTranscriptAdmitsThroughListener(t *testing.T) {
	r := newAdmissionRig(t)
	selected := make(chan ExpressionInput, 2)
	r.s.selectExpression = func(_ context.Context, input ExpressionInput) ExpressionResult {
		selected <- input
		return ExpressionResult{Emotion: "neutral", Reason: "completed"}
	}
	r.start(t, "current-item")
	r.stop(t, "current-item")
	r.event(t, "input_audio_buffer.committed", "current-item", nil)
	r.noReply(t)
	r.final(t, "current-item", "迟到的原话")
	select {
	case input := <-selected:
		if input.CurrentTranscript != "迟到的原话" {
			t.Fatal(input)
		}
	case <-time.After(time.Second):
		t.Fatal("final ASR did not trigger selector after admission")
	}
	r.final(t, "current-item", "重复原话")
	r.sync(t)
	select {
	case <-selected:
		t.Fatal("duplicate final ASR triggered a second selection")
	default:
	}
}

func TestSessionScreenExpressionsReachMainModelOnlyAfterNegotiation(t *testing.T) {
	base, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{base.Name(), "团子"} {
		product := base
		if name != base.Name() {
			product, err = base.WithName(name)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, tc := range []struct {
			name                string
			enabled, face, want bool
		}{
			{"negotiated", true, true, true},
			{"legacy", true, false, false},
			{"disabled", false, true, false},
			{"state_only", true, false, false},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				server := NewServer(Config{ExpressionsEnabled: tc.enabled}, product, t.TempDir(), "test")
				sessions := make(chan *Session, 1)
				server.runDevice = func(s *Session) {
					defer s.cancel()
					sessions <- s
					s.send(map[string]any{"type": "ready"})
				}
				h := httptest.NewServer(server.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
				defer h.Close()
				c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				hello := protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true, "face_expression_v1": tc.face, "face_state_v1": tc.name == "state_only"}}
				if err := c.WriteJSON(hello); err != nil {
					t.Fatal(err)
				}
				if got := object(readFaceEvent(t, c)["features"])["face_expression_v1"] == true; got != tc.want {
					t.Fatal("wrong negotiated capability", got)
				}
				var s *Session
				select {
				case s = <-sessions:
				case <-time.After(time.Second):
					t.Fatal("authenticated session not created")
				}
				facts := s.sessionFacts()
				emotions, factPresent := facts["screen_expression_emotions"].([]string)
				if tc.want && (!factPresent || !reflect.DeepEqual(emotions, expressionEmotions[:]) || len(emotions) != 15) {
					t.Fatalf("main model lacks canonical screen emotion facts: %v", facts)
				}
				if !tc.want && facts["screen_expression_emotions"] != nil {
					t.Fatal("unnegotiated session acquired screen emotions", facts)
				}
				context := `{"recent_turns":[]}`
				now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
				snapshot, err := s.product.Snapshot(facts, context, true)
				if err != nil {
					t.Fatal(err)
				}
				prompt, err := s.product.InstructionsFor(facts, context, true, now)
				if err != nil {
					t.Fatal(err)
				}
				if object(snapshot["product"])["name"] != name || !strings.Contains(prompt, `"name":"`+name+`"`) || !strings.Contains(prompt, "这个入口只有语音输入。") {
					t.Fatal("screen output rewrote product identity or audio input facts", snapshot, prompt)
				}
				cap := object(object(snapshot["capabilities"])["screen_expressions"])
				if tc.want {
					if cap["status"] != "available" {
						t.Fatal("negotiated screen capability unavailable", cap)
					}
					description, _ := cap["description"].(string)
					for _, term := range []string{"15种主情绪表情", "平静", "开心", "关爱", "害羞", "自信", "馋嘴", "难过", "哭泣", "困倦", "搞怪", "生气", "惊讶", "眨眼", "放松", "疑惑", "L0系统状态", "L2微动作", "不计入"} {
						if !strings.Contains(description, term) || !strings.Contains(prompt, term) {
							t.Fatalf("screen capability missing natural fact %q: %v", term, cap)
						}
					}
					for _, emotion := range expressionEmotions {
						if !strings.Contains(description, "（"+emotion+"）") {
							t.Fatal("screen capability lost canonical emotion", emotion, cap)
						}
					}
					for _, term := range []string{"屏幕输出", "选用合适表情", "不是给手机发送", "不要承诺一次回复依次展示全部"} {
						if !strings.Contains(prompt, term) {
							t.Fatal("main prompt missing screen boundary", term, prompt)
						}
					}
					// Facts are per-session copies, not a mutable alias of the selector enum.
					emotions[0] = "mutated"
					if !reflect.DeepEqual(s.sessionFacts()["screen_expression_emotions"], expressionEmotions[:]) || expressionEmotions[0] != "neutral" {
						t.Fatal("screen facts changed the canonical selector values")
					}
				} else {
					frozenSnapshot, _ := s.product.Snapshot(s.cfg.facts(), context, true)
					frozenPrompt, _ := s.product.InstructionsFor(s.cfg.facts(), context, true, now)
					if len(cap) != 0 || !reflect.DeepEqual(snapshot, frozenSnapshot) || prompt != frozenPrompt {
						t.Fatal("unnegotiated identity or prompt changed", snapshot, prompt)
					}
				}
				if _, exists := s.cfg.facts()["screen_expression_emotions"]; exists {
					t.Fatal("shared config acquired another session's screen facts")
				}
			})
		}
	}
}

func TestScreenExpressionFactsRejectMacAndUnownedWire(t *testing.T) {
	for _, mode := range []string{"mac", "unowned", "wrong_client_type", "invalid_owner"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := expressionSession(t)
			switch mode {
			case "mac":
				s.wire = &macWire{ws: s.ws}
				s.rec.owner = Ownership{DeviceID: "device", ClientType: "xiaozhi"}
			case "wrong_client_type":
				s.rec.owner = Ownership{DeviceID: "device", ClientType: "mac"}
			case "invalid_owner":
				s.rec.owner = Ownership{DeviceID: "device", ClientType: "xiaozhi"}
				s.ownerErr = context.Canceled
			}
			if _, exists := s.sessionFacts()["screen_expression_emotions"]; exists {
				t.Fatal("unauthenticated/non-device wire acquired screen emotion facts")
			}
		})
	}
}
