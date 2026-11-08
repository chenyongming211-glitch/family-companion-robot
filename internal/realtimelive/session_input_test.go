package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/speakerlive"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// These tests drive the production listener through a websocket. A ping/pong
// barrier proves all preceding events were handled before checking a no-op.
type admissionRig struct {
	s                 *Session
	provider          *websocket.Conn
	pong              chan string
	serial            int
	generated         chan int64
	primary, selector atomic.Int32
	events            chan map[string]any
	listenerDone      chan struct{}
	listenerErr       error
}

func newAdmissionRig(t *testing.T, configure ...func(*Session)) *admissionRig {
	t.Helper()
	s, client := expressionSession(t)
	s.rec.memoryOnly = true
	s.wire.(*xiaozhiWire).face = true
	provider, feed := testSocket(t)
	s.listener.conn = feed
	r := &admissionRig{s: s, provider: provider, pong: make(chan string, 1), generated: make(chan int64, 128), events: make(chan map[string]any, 256), listenerDone: make(chan struct{})}
	provider.SetPongHandler(func(data string) error { r.pong <- data; return nil })
	providerDone := make(chan struct{})
	go func() {
		defer close(providerDone)
		for {
			if _, _, err := provider.ReadMessage(); err != nil {
				return
			}
		}
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		for {
			kind, b, err := client.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage {
				var e map[string]any
				if json.Unmarshal(b, &e) == nil {
					r.events <- e
				}
			}
		}
	}()
	s.generate = func(ctx context.Context, id int64, prompt string, pcm []byte, emit func(string, []byte) error) error {
		r.primary.Add(1)
		r.generated <- id
		<-ctx.Done()
		return ctx.Err()
	}
	s.selectExpression = func(ctx context.Context, input ExpressionInput) ExpressionResult {
		r.selector.Add(1)
		return ExpressionResult{Emotion: "happy", Reason: "completed", RequestCount: 1}
	}
	pcm := make([]byte, 16000*2*4)
	for i := range pcm {
		pcm[i] = 42
	}
	if err := s.ring.Append(pcm); err != nil {
		t.Fatal(err)
	}
	for _, fn := range configure {
		fn(s)
	}
	go func() { defer close(r.listenerDone); r.listenerErr = s.listen() }()
	t.Cleanup(func() {
		s.cancel()
		s.listener.Close()
		provider.Close()
		client.Close()
		<-r.listenerDone
		<-providerDone
		<-clientDone
		s.wg.Wait()
	})
	return r
}
func (r *admissionRig) event(t *testing.T, typ, item string, extra map[string]any) {
	t.Helper()
	e := map[string]any{"type": typ, "item_id": item}
	for k, v := range extra {
		e[k] = v
	}
	if err := r.provider.WriteJSON(e); err != nil {
		t.Fatal(err)
	}
}
func (r *admissionRig) sync(t *testing.T) {
	t.Helper()
	r.serial++
	key := fmt.Sprint(r.serial)
	if err := r.provider.WriteControl(websocket.PingMessage, []byte(key), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-r.pong:
		if value != key {
			t.Fatal(value, key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener event barrier timed out")
	}
}
func (r *admissionRig) start(t *testing.T, item string) {
	r.event(t, "input_audio_buffer.speech_started", item, map[string]any{"audio_start_ms": 0})
}
func (r *admissionRig) stop(t *testing.T, item string) {
	r.event(t, "input_audio_buffer.speech_stopped", item, map[string]any{"audio_end_ms": 620})
}
func (r *admissionRig) final(t *testing.T, item string, text any) {
	r.event(t, "conversation.item.input_audio_transcription.completed", item, map[string]any{"transcript": text})
}
func (r *admissionRig) preview(t *testing.T, item, field string, text any) {
	r.event(t, "conversation.item.input_audio_transcription.delta", item, map[string]any{field: text})
}
func (r *admissionRig) expectGenerated(t *testing.T) int64 {
	t.Helper()
	select {
	case id := <-r.generated:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("valid finalized input did not generate")
		return 0
	}
}
func (r *admissionRig) noReply(t *testing.T) {
	t.Helper()
	r.sync(t)
	if got := r.primary.Load(); got != 0 {
		t.Fatalf("unadmitted input made %d primary calls", got)
	}
	if got := r.selector.Load(); got != 0 {
		t.Fatalf("unadmitted input made %d selector calls", got)
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if len(r.s.history) != 0 {
		t.Fatalf("unadmitted input entered history: %d", len(r.s.history))
	}
}

func TestInputAdmissionEmptySegmentPreservesContinuousOldOutput(t *testing.T) {
	r := newAdmissionRig(t)
	old := r.s.addTurn()
	expressionCtx, cancelExpression := context.WithCancel(old.ctx)
	r.s.mu.Lock()
	old.expressionCancel = cancelExpression
	old.expressionPending = &ExpressionResult{Emotion: "happy"}
	old.turn.Begun = true
	err := r.s.sendLocked(map[string]any{"type": "begin", "id": old.turn.ID})
	r.s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	stages := []func(){
		func() { r.start(t, "noise") },
		func() { r.stop(t, "noise") },
		func() { r.event(t, "input_audio_buffer.committed", "noise", nil) },
		func() { r.final(t, "noise", "") },
	}
	for i, stage := range stages {
		stage()
		r.sync(t)
		r.s.mu.Lock()
		cut, output, pending := old.turn.Cut, r.s.output, old.expressionPending
		r.s.mu.Unlock()
		if cut || output != old || pending == nil || expressionCtx.Err() != nil {
			t.Fatalf("noise stage %d retired old audio/expression", i)
		}
		if ok, err := r.s.binary(old, make([]byte, 2880)); err != nil || !ok {
			t.Fatalf("old audio stopped at stage %d: %v", i, err)
		}
	}
	if r.primary.Load() != 0 || r.selector.Load() != 0 {
		t.Fatal("empty final generated a reply")
	}
	for {
		select {
		case e := <-r.events:
			if e["type"] == "drain" || e["state"] == "listening" || e["state"] == "thinking" {
				t.Fatal("noise changed output state", e)
			}
		default:
			return
		}
	}
}

func TestInputAdmissionIdleEmptyThenValidIdentityQuestion(t *testing.T) {
	r := newAdmissionRig(t)
	r.start(t, "empty")
	r.stop(t, "empty")
	r.event(t, "input_audio_buffer.committed", "empty", nil)
	r.final(t, "empty", " \n。！？ ")
	r.noReply(t)
	r.start(t, "identity")
	r.stop(t, "identity")
	r.event(t, "input_audio_buffer.committed", "identity", nil)
	r.final(t, "identity", "你是谁，叫什么名字？")
	id := r.expectGenerated(t)
	r.sync(t)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.output == nil || r.s.output.turn.ID != id || r.s.output.turn.User != "你是谁，叫什么名字？" || len(r.s.history) != 1 {
		t.Fatal("valid identity request was filtered")
	}
}

func TestInputAdmissionPreviewCutsBeforeFinalOnce(t *testing.T) {
	for _, field := range []string{"text", "stash"} {
		t.Run(field, func(t *testing.T) {
			r := newAdmissionRig(t)
			old := r.s.addTurn()
			r.start(t, "barge")
			r.sync(t)
			r.s.mu.Lock()
			cut := old.turn.Cut
			r.s.mu.Unlock()
			if cut {
				t.Fatal("VAD alone cut old output")
			}
			r.preview(t, "barge", field, "等一下")
			r.sync(t)
			r.s.mu.Lock()
			cut, output := old.turn.Cut, r.s.output
			r.s.mu.Unlock()
			if !cut || output != nil {
				t.Fatal("meaningful streaming preview did not interrupt before final")
			}
			r.preview(t, "barge", field, "等一下")
			r.stop(t, "barge")
			r.event(t, "input_audio_buffer.committed", "barge", nil)
			r.sync(t)
			if r.primary.Load() != 0 || r.selector.Load() != 0 {
				t.Fatal("preview/stop started a new reply")
			}
			r.final(t, "barge", "等一下，再说一遍")
			id := r.expectGenerated(t)
			r.preview(t, "barge", field, "迟到片段")
			r.final(t, "barge", "重复转写")
			r.sync(t)
			if r.primary.Load() != 1 {
				t.Fatal("duplicate input generated again", r.primary.Load())
			}
			r.s.mu.Lock()
			defer r.s.mu.Unlock()
			if r.s.output == nil || r.s.output.turn.ID != id || r.s.output.turn.Cut || r.s.output.turn.User != "等一下，再说一遍" {
				t.Fatal("late/duplicate events changed accepted turn")
			}
		})
	}
}

func TestInputAdmissionRejectsUnscopedMalformedAndStaleEvents(t *testing.T) {
	r := newAdmissionRig(t)
	old := r.s.addTurn()
	r.start(t, "old")
	r.stop(t, "old")
	r.start(t, "current")
	for _, event := range []struct {
		item  string
		value any
	}{
		{"wrong", "你好"}, {"old", "旧内容"}, {"current", ""}, {"current", " \t"}, {"current", "……？！——"}, {"current", map[string]any{"text": "你好"}}, {"current", strings.Repeat("字", 4097)},
	} {
		r.preview(t, event.item, "stash", event.value)
	}
	r.event(t, "conversation.item.input_audio_transcription.delta", "current", map[string]any{"text": 42, "stash": "你好"})
	r.final(t, "old", "旧问题")
	r.event(t, "input_audio_buffer.committed", "old", nil)
	r.stop(t, "old")
	r.stop(t, "current")
	r.final(t, "wrong", "错问题")
	r.final(t, "current", 123)
	r.preview(t, "current", "text", "已拒绝输入的迟到预览")
	r.final(t, "current", "后来的重复必须无效")
	r.sync(t)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if old.turn.Cut || r.s.output != old || len(r.s.history) != 1 || r.primary.Load() != 0 || r.selector.Load() != 0 {
		t.Fatal("bad or stale item changed output/admitted input")
	}
}

func TestInputAdmissionPreviewRetractionDoesNotGenerateOrExecuteControls(t *testing.T) {
	r := newAdmissionRig(t)
	old := r.s.addTurn()
	r.s.memory = &sessionMemory{} // A preview must never reach memory routing.
	r.s.controls = &deviceControls{calls: make(chan *controlRequest, 1), ready: make(chan struct{})}
	r.start(t, "retract")
	r.preview(t, "retract", "stash", "请清空本设备对话记录，把音量调到50")
	r.sync(t)
	r.s.mu.Lock()
	cut := old.turn.Cut
	floor := r.s.memoryFloor
	r.s.mu.Unlock()
	if !cut {
		t.Fatal("meaningful preview failed to interrupt")
	}
	r.stop(t, "retract")
	r.final(t, "retract", "")
	r.sync(t)
	if r.primary.Load() != 0 || r.selector.Load() != 0 || len(r.s.controls.calls) != 0 {
		t.Fatal("retracted preview executed work")
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.memoryFloor != floor || len(r.s.history) != 1 {
		t.Fatal("preview changed memory")
	}
}

func TestInputAdmissionExplicitAbortInvalidatesPendingButAllowsNewCapture(t *testing.T) {
	r := newAdmissionRig(t)
	old := r.s.addTurn()
	r.start(t, "aborted")
	r.stop(t, "aborted")
	r.sync(t)
	if err := r.s.interruptFrom(nil, "device_abort"); err != nil {
		t.Fatal(err)
	}
	r.s.mu.Lock()
	cut := old.turn.Cut
	r.s.mu.Unlock()
	if !cut {
		t.Fatal("explicit abort did not cut immediately")
	}
	r.event(t, "input_audio_buffer.committed", "aborted", nil)
	r.final(t, "aborted", "不得复活")
	r.sync(t)
	if r.primary.Load() != 0 {
		t.Fatal("pre-abort input revived")
	}
	r.start(t, "new")
	r.stop(t, "new")
	r.final(t, "new", "新的问题")
	r.expectGenerated(t)
}

func TestInputAdmissionSupersessionAndIncompletePaths(t *testing.T) {
	r := newAdmissionRig(t)
	r.start(t, "first")
	r.stop(t, "first")
	r.start(t, "second")
	r.final(t, "first", "不得恢复旧输入")
	r.event(t, "input_audio_buffer.committed", "first", nil)
	r.final(t, "second", "结束前的最终事件")
	r.sync(t)
	if r.primary.Load() != 0 {
		t.Fatal("incomplete/superseded capture generated")
	}
	r.stop(t, "second")
	r.final(t, "second", "实际新问题")
	r.expectGenerated(t)
}

func TestInputAdmissionPendingWorkStaysBounded(t *testing.T) {
	r := newAdmissionRig(t)
	for i := 0; i < 130; i++ {
		item := fmt.Sprintf("missing-final-%d", i)
		r.start(t, item)
		r.stop(t, item)
		r.event(t, "input_audio_buffer.committed", item, nil)
	}
	r.noReply(t)
	r.s.mu.Lock()
	size := len(r.s.items)
	bytes := 0
	for _, input := range r.s.items {
		bytes += len(input.pcm)
	}
	r.s.mu.Unlock()
	if bytes > inputPCMLimit {
		t.Fatal("unbounded pending PCM", bytes)
	}
	if size > 64 {
		t.Fatal("unbounded provider input map", size)
	}
	r.final(t, "missing-final-0", "迟到的输入")
	r.sync(t)
	if r.primary.Load() != 0 {
		t.Fatal("expired item revived after supersession")
	}
	r.final(t, "missing-final-129", "最后一条真实输入")
	r.expectGenerated(t)
}

func TestInputAdmissionCanceledSessionCannotAdmitFinal(t *testing.T) {
	r := newAdmissionRig(t)
	r.start(t, "closing")
	r.stop(t, "closing")
	r.sync(t)
	r.s.cancel()
	r.final(t, "closing", "取消后不得回复")
	select {
	case <-r.listenerDone:
	case <-time.After(time.Second):
		t.Fatal("canceled listener did not finish")
	}
	r.s.mu.Lock()
	retained := r.s.lastInput != nil || len(r.s.items) != 0
	r.s.mu.Unlock()
	if retained {
		t.Fatal("session close retained pending input")
	}
	if r.primary.Load() != 0 {
		t.Fatal("closed/replaced session admitted a pending input")
	}
}

func TestInputAdmissionMissingCaptureAndMalformedIDsNeverGenerate(t *testing.T) {
	r := newAdmissionRig(t)
	for _, item := range []string{"", "bad id", strings.Repeat("x", 257)} {
		r.start(t, item)
		r.preview(t, item, "text", "你好")
		r.stop(t, item)
		r.final(t, item, "你好")
	}
	r.event(t, "input_audio_buffer.committed", "no-capture", nil)
	r.final(t, "no-capture", "不得只靠完成事件生成")
	r.noReply(t)
	for _, text := range []any{nil, 123, strings.Repeat("字", 4097), "👋……？！"} {
		item := fmt.Sprintf("invalid-final-%d", r.serial)
		r.start(t, item)
		r.stop(t, item)
		r.final(t, item, text)
		r.noReply(t)
	}
	r.start(t, "unicode-number")
	r.stop(t, "unicode-number")
	r.final(t, "unicode-number", "３")
	r.expectGenerated(t)
}

func TestInputAdmissionLateGenerationFailureDoesNotDiscardNewInput(t *testing.T) {
	r := newAdmissionRig(t)
	old := r.s.addTurn()
	r.start(t, "new-input")
	r.stop(t, "new-input")
	r.sync(t)
	r.s.turnError(old)
	r.final(t, "new-input", "继续这个新问题")
	id := r.expectGenerated(t)
	r.s.turnError(old)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.output == nil || r.s.output.turn.ID != id || r.s.output.turn.Cut {
		t.Fatal("late old failure canceled new input/output")
	}
}

func TestInputAdmissionProviderFailureDisposesPending(t *testing.T) {
	r := newAdmissionRig(t)
	r.start(t, "provider-failed")
	r.stop(t, "provider-failed")
	r.noReply(t)
	r.event(t, "error", "", map[string]any{"error": map[string]any{"message": "private-provider-detail"}})
	select {
	case <-r.listenerDone:
	case <-time.After(time.Second):
		t.Fatal("provider failure did not terminate listener")
	}
	if r.listenerErr == nil || r.listenerErr.Error() != "native_provider_error" {
		t.Fatal("provider failure was swallowed", r.listenerErr)
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.lastInput != nil || len(r.s.items) != 0 || r.primary.Load() != 0 {
		t.Fatal("failed provider retained/admitted a pending input")
	}
}

func TestInputAdmissionSpeakerCaptureIDsAndOwnedAudioSurviveRejection(t *testing.T) {
	observer := &channelSpeakerObserver{jobs: make(chan speakerlive.Job, 2)}
	wire := &speakerCaptureWire{}
	r := newAdmissionRig(t, func(s *Session) {
		s.cfg.Speaker = &speakerlive.Config{}
		s.speaker = observer
		s.providerEnd = 64000
		s.wire = wire
	})
	r.start(t, "rejected")
	r.event(t, "input_audio_buffer.speech_stopped", "rejected", map[string]any{"audio_end_ms": 2000})
	var first speakerlive.Job
	select {
	case first = <-observer.jobs:
	case <-time.After(time.Second):
		t.Fatal("speaker did not receive source audio")
	}
	r.final(t, "rejected", "")
	r.start(t, "superseded")
	r.start(t, "current")
	r.event(t, "input_audio_buffer.speech_stopped", "current", map[string]any{"audio_end_ms": 2000})
	var current speakerlive.Job
	select {
	case current = <-observer.jobs:
	case <-time.After(time.Second):
		t.Fatal("new speaker span not observed")
	}
	r.sync(t)
	r.s.mu.Lock()
	latest, count := r.s.speakerLatest, len(wire.events)
	r.s.mu.Unlock()
	if latest != current.ID || current.ID <= first.ID+1 {
		t.Fatal("rejected or superseded capture reused a speaker ID", first.ID, current.ID, latest)
	}
	r.s.speakerResult(speakerlive.Result{ID: first.ID, State: "temporary", Label: "old-person"})
	r.s.mu.Lock()
	after := len(wire.events)
	r.s.mu.Unlock()
	if after != count {
		t.Fatal("stale identity was published for the new capture")
	}
	if err := r.s.interruptFrom(nil, "device_abort"); err != nil {
		t.Fatal(err)
	}
	for _, job := range []speakerlive.Job{first, current} {
		for _, b := range job.PCM {
			if b != 42 {
				t.Fatal("pending cleanup cleared observer-owned source audio")
			}
		}
	}
	if r.primary.Load() != 0 {
		t.Fatal("rejected or superseded voice entered primary")
	}
}

func TestInputAdmissionPreviewAfterStopCutsBeforeFinal(t *testing.T) {
	for _, field := range []string{"text", "stash"} {
		t.Run(field, func(t *testing.T) {
			r := newAdmissionRig(t)
			old := r.s.addTurn()
			r.start(t, "staged")
			r.stop(t, "staged")
			r.sync(t)
			r.s.mu.Lock()
			cut, output := old.turn.Cut, r.s.output
			r.s.mu.Unlock()
			if cut || output != old {
				t.Fatal("VAD stop interrupted old output before recognition")
			}
			r.preview(t, "staged", field, "等一下")
			r.sync(t)
			r.s.mu.Lock()
			cut, output = old.turn.Cut, r.s.output
			r.s.mu.Unlock()
			if !cut || output != nil {
				t.Fatal("matching meaningful preview after stop did not interrupt before final")
			}
			if r.primary.Load() != 0 || r.selector.Load() != 0 {
				t.Fatal("staged preview started work before final")
			}
			r.final(t, "staged", "等一下，再讲一遍")
			id := r.expectGenerated(t)
			r.preview(t, "staged", field, "准入后的迟到预览")
			r.start(t, "new-capture")
			r.preview(t, "staged", field, "被新输入替换后的迟到预览")
			r.sync(t)
			r.s.mu.Lock()
			defer r.s.mu.Unlock()
			if r.s.output == nil || r.s.output.turn.ID != id || r.s.output.turn.Cut || r.primary.Load() != 1 {
				t.Fatal("retired preview cut the accepted output")
			}
		})
	}
}
