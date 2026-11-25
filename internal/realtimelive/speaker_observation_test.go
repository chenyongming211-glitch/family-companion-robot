package realtimelive

import (
	"context"
	"encoding/binary"
	"errors"
	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/speakerlive"
	"os"
	"testing"
	"time"
)

type speakerCaptureWire struct{ events []map[string]any }

func (w *speakerCaptureWire) decodeAudio([]byte, *realtimeaudio.Codec) ([]byte, error) {
	return nil, nil
}
func (w *speakerCaptureWire) control([]byte) (map[string]any, error) { return nil, nil }
func (w *speakerCaptureWire) sendEvent(e map[string]any) error {
	w.events = append(w.events, e)
	return nil
}
func (w *speakerCaptureWire) sendAudio(int64, []byte) error { return nil }
func (w *speakerCaptureWire) normalizeReceipt(*realtimestate.Turn, map[string]any) (map[string]any, error) {
	return nil, nil
}

type speakerCaptureObserver struct {
	jobs   []speakerlive.Job
	accept bool
}

func (o *speakerCaptureObserver) Offer(j speakerlive.Job) bool {
	o.jobs = append(o.jobs, j)
	return o.accept
}
func (*speakerCaptureObserver) Close() {}
func observationSession(t *testing.T) (*Session, *speakerCaptureWire, *speakerCaptureObserver) {
	t.Helper()
	server, _ := testSocket(t)
	s := NewSession(server, Config{MemoryOnly: true, Speaker: &speakerlive.Config{}}, t.TempDir(), nil)
	w := &speakerCaptureWire{}
	o := &speakerCaptureObserver{accept: true}
	s.wire = w
	s.speaker = o
	t.Cleanup(s.cancel)
	return s, w, o
}
func TestSpeakerRequiresCompleteSourceSpan(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end int64
		size       int
		valid      bool
	}{
		{"missing_timestamp", 0, 32000, 64000, false},
		{"short", 0, 16000, 32000, true},
		{"partial", 0, 32000, 48000, true},
		{"too_long", 0, 128001, 256002, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, w, o := observationSession(t)
			s.speakerLatest = 1
			s.observeSpeaker(1, tc.start, tc.end, make([]byte, tc.size), tc.valid)
			if len(o.jobs) != 0 || len(w.events) != 1 || w.events[0]["state"] != "uncertain" {
				t.Fatalf("unsafe span accepted: %+v %+v", o.jobs, w.events)
			}
		})
	}
}
func TestSpeakerUsesExactSpanAndRejectsLateIdentity(t *testing.T) {
	s, w, o := observationSession(t)
	s.speakerLatest = 2
	s.observeSpeaker(2, 16000, 48000, make([]byte, 64000), true)
	if len(o.jobs) != 1 || o.jobs[0].Start != 16000 || o.jobs[0].End != 48000 || o.jobs[0].ID != 2 {
		t.Fatal(o.jobs)
	}
	s.speakerResult(speakerlive.Result{ID: 1, Start: 0, End: 32000, State: "temporary", Label: "guest_1"})
	if len(w.events) != 0 {
		t.Fatal("late identity replaced current input")
	}
	s.speakerResult(speakerlive.Result{ID: 2, Start: 16000, End: 48000, State: "temporary", Label: "guest_2"})
	if len(w.events) != 1 || w.events[0]["label"] != "guest_2" {
		t.Fatal(w.events)
	}
	s.cancel()
	s.speakerResult(speakerlive.Result{ID: 2, State: "unavailable"})
	if len(w.events) != 1 {
		t.Fatal("post-close identity emitted")
	}
}
func TestSpeakerDisabledNeverOffers(t *testing.T) {
	s, w, o := observationSession(t)
	s.cfg.Speaker = nil
	s.speakerLatest = 1
	s.observeSpeaker(1, 0, 32000, make([]byte, 64000), true)
	if len(o.jobs) != 0 || len(w.events) != 0 {
		t.Fatal("disabled speaker performed analysis")
	}
}
func TestSpeakerFailureDoesNotCancelConversation(t *testing.T) {
	s, w, _ := observationSession(t)
	s.speakerLatest = 3
	s.speakerResult(speakerlive.Result{State: "unavailable"})
	if s.ctx.Err() != nil || len(w.events) != 1 || w.events[0]["input_id"] != int64(3) {
		t.Fatal("speaker failure interrupted conversation")
	}
	// Session teardown remains independent from turn cancellation.
	ctx, cancel := context.WithCancel(s.ctx)
	cancel()
	if ctx.Err() == nil || s.ctx.Err() != nil {
		t.Fatal("bad cancellation isolation")
	}
}

func TestSpeakerLateFailureMarksObserverUnavailable(t *testing.T) {
	s, w, _ := observationSession(t)
	s.speakerLatest = 2
	s.speakerResult(speakerlive.Result{ID: 1, Start: 0, End: 32000, State: "unavailable"})
	if !s.speakerFailed || len(w.events) != 1 || w.events[0]["input_id"] != int64(2) || w.events[0]["state"] != "unavailable" {
		t.Fatal("helper death was mistaken for a discardable old identity", w.events)
	}
}

type channelSpeakerObserver struct{ jobs chan speakerlive.Job }

func (o *channelSpeakerObserver) Offer(job speakerlive.Job) bool { o.jobs <- job; return true }
func (*channelSpeakerObserver) Close()                           {}

func TestSpeakerListenerGeneratesWithoutAwaitingIdentity(t *testing.T) {
	s, _, _ := observationSession(t)
	product, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	s.product = product
	provider, source := testSocket(t)
	s.listener.conn = provider
	observer := &channelSpeakerObserver{jobs: make(chan speakerlive.Job, 1)}
	s.speaker = observer
	if err := s.ring.Append(make([]byte, 64000)); err != nil {
		t.Fatal(err)
	}
	s.providerEnd = 32000
	generated := make(chan struct{}, 1)
	s.generate = func(ctx context.Context, id int64, prompt string, pcm []byte, emit func(string, []byte) error) error {
		generated <- struct{}{}
		return errors.New("synthetic_stop")
	}
	done := make(chan error, 1)
	go func() { done <- s.listen() }()
	defer func() { s.cancel(); s.listener.Close(); <-done; s.wg.Wait() }()
	if err := source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "speaker-input", "audio_start_ms": 0}); err != nil {
		t.Fatal(err)
	}
	if err := source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "speaker-input", "audio_end_ms": 2000}); err != nil {
		t.Fatal(err)
	}
	select {
	case job := <-observer.jobs:
		if job.ID != 1 || job.Start != 0 || job.End != 32000 || len(job.PCM) != 64000 {
			t.Fatal(job)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("VAD span did not reach observer")
	}
	// Final ASR is required for voice admission; the fake speaker observer
	// intentionally never emits an identity result.
	if err := source.WriteJSON(map[string]any{"type": "input_audio_buffer.committed", "item_id": "speaker-input"}); err != nil {
		t.Fatal(err)
	}
	if err := source.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "speaker-input", "transcript": "讲个故事"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-generated:
	case <-time.After(2 * time.Second):
		t.Fatal("normal reply waited for identification")
	}
	if entries, err := os.ReadDir(s.directory); err != nil || len(entries) != 0 {
		t.Fatal("listener wrote private body", entries, err)
	}
}

func TestSpeakerTerminalFailureSurvivesShortInput(t *testing.T) {
	s, w, _ := observationSession(t)
	s.speakerLatest = 1
	s.speakerFailed = true
	s.observeSpeaker(1, 0, 16000, make([]byte, 32000), false)
	if len(w.events) != 1 || w.events[0]["state"] != "unavailable" {
		t.Fatal("terminal failure overwritten by uncertain", w.events)
	}
}

func TestSpeakerLongRejectedInputClearsPending(t *testing.T) {
	s, w, o := observationSession(t)
	provider, source := testSocket(t)
	s.listener.conn = provider
	if err := s.ring.Append(make([]byte, 416000*2)); err != nil {
		t.Fatal(err)
	}
	s.providerEnd = 416000
	done := make(chan error, 1)
	go func() { done <- s.listen() }()
	_ = source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "speaker-input", "audio_start_ms": 0})
	_ = source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "speaker-input", "audio_end_ms": 26000})
	_ = source.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.listener.Close()
		t.Fatal("listener did not finish")
	}
	states := []string{}
	for _, event := range w.events {
		if event["type"] == "speaker_observation" {
			states = append(states, event["state"].(string))
		}
	}
	if len(states) != 2 || states[0] != "pending" || states[1] != "uncertain" || len(o.jobs) != 0 {
		t.Fatal("rejected long input left a pending observation", states)
	}
	for _, event := range w.events {
		if event["type"] == "speaker_observation" && event["state"] == "uncertain" && (event["reason"] != "too_long" || event["audio_ms"] != int64(26000)) {
			t.Fatal("complete long input lost known duration", event)
		}
	}
}

type failingOfferObserver struct{ session *Session }

func (o *failingOfferObserver) Offer(job speakerlive.Job) bool {
	o.session.speakerResult(speakerlive.Result{ID: job.ID, State: "unavailable"})
	return false
}
func (*failingOfferObserver) Close() {}
func TestSpeakerFailureDuringOfferCannotBeOverwritten(t *testing.T) {
	s, w, _ := observationSession(t)
	s.speakerLatest = 1
	s.speaker = &failingOfferObserver{session: s}
	s.observeSpeaker(1, 0, 32000, make([]byte, 64000), true)
	for _, event := range w.events {
		if event["state"] != "unavailable" {
			t.Fatal("terminal state overwritten during Offer", w.events)
		}
	}
}

func TestSpeakerRejectionReasonAndAudioLength(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end int64
		bytes      int
		complete   bool
		reason     string
		ms         int64
	}{
		{"source", 0, 32000, 64000, false, "incomplete_span", 2000},
		{"short", 0, 16000, 32000, true, "too_short", 1000},
		{"long", 0, 128001, 256002, true, "too_long", 8000},
		{"missing", 0, 32000, 48000, true, "incomplete_span", 1500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, w, _ := observationSession(t)
			s.speakerLatest = 1
			s.observeSpeaker(1, tc.start, tc.end, make([]byte, tc.bytes), tc.complete)
			if len(w.events) != 1 || w.events[0]["reason"] != tc.reason || w.events[0]["audio_ms"] != tc.ms {
				t.Fatal("rejection reason missing or incorrect", w.events)
			}
		})
	}
	s, w, o := observationSession(t)
	s.speakerLatest = 1
	o.accept = false
	s.observeSpeaker(1, 0, 32000, make([]byte, 64000), true)
	if len(w.events) != 1 || w.events[0]["reason"] != "not_queued" {
		t.Fatal(w.events)
	}
}
func TestSpeakerUnavailableHasUnambiguousReason(t *testing.T) {
	s, w, _ := observationSession(t)
	s.speakerLatest = 1
	s.speakerFailed = true
	s.observeSpeaker(1, 0, 16000, make([]byte, 32000), false)
	if len(w.events) != 1 || w.events[0]["reason"] != "observer_unavailable" {
		t.Fatal(w.events)
	}
}

func TestSpeakerReasonReachesCurrentWindow(t *testing.T) {
	s, w, _ := observationSession(t)
	s.speakerLatest = 1
	s.speakerResult(speakerlive.Result{ID: 1, Start: 16000, End: 48000, State: "uncertain", Reason: "match_uncertain", AudioMS: 2000})
	if len(w.events) != 1 || w.events[0]["reason"] != "match_uncertain" || w.events[0]["audio_ms"] != int64(2000) {
		t.Fatal(w.events)
	}
	s.speakerResult(speakerlive.Result{State: "unavailable"})
	s.speakerResult(speakerlive.Result{ID: 1, State: "uncertain", Reason: "low_energy", AudioMS: 2000})
	if last := w.events[len(w.events)-1]; last["state"] != "unavailable" || last["reason"] != "observer_unavailable" {
		t.Fatal(last)
	}
}

func makeAmplitudePCM(samples int, amp int16) []byte {
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(amp))
	}
	return buf
}

func TestSpeakerPhase1BystanderFilteringAndSessionAnchor(t *testing.T) {
	t.Run("low_energy_bystander_rejected_even_under_1500ms", func(t *testing.T) {
		r := newAdmissionRig(t, func(s *Session) {
			s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi"}
			s.cfg.Speaker = &speakerlive.Config{}
			s.speaker = &speakerCaptureObserver{accept: true}
			s.ring = realtimestate.NewAudioRing(16000 * 10)
			// Amplitude 20 -> 20/32768 = 0.00061 < minSpeakerRMS (0.005).
			_ = s.ring.Append(makeAmplitudePCM(16000*2, 20))
			s.providerEnd = 16000 * 2
		})
		old := echoOutput(t, r, "我正在给你讲一个故事，内容很长。", 15)
		if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
			t.Fatal(err)
		}
		r.event(t, "input_audio_buffer.speech_started", "low-tv", map[string]any{"audio_start_ms": 0})
		r.preview(t, "low-tv", "text", "旁边电视里说的台词")
		r.event(t, "input_audio_buffer.speech_stopped", "low-tv", map[string]any{"audio_end_ms": 800})
		r.final(t, "low-tv", "旁边电视里说的台词")
		r.sync(t)
		r.s.mu.Lock()
		defer r.s.mu.Unlock()
		if old.turn.Cut || r.s.output != old || r.primary.Load() != 0 {
			t.Fatalf("low-energy bystander interrupted or generated: cut=%v primary=%d", old.turn.Cut, r.primary.Load())
		}
	})

	t.Run("anchor_locks_on_non_overlapped_turn_blocks_guest2_and_allows_short_command", func(t *testing.T) {
		r := newAdmissionRig(t, func(s *Session) {
			s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi"}
			s.cfg.Speaker = &speakerlive.Config{}
			s.speaker = &speakerCaptureObserver{accept: true}
		})
		// 1. Short (<1.5s) first turn with normal energy must be allowed and must not lock anchor.
		r.event(t, "input_audio_buffer.speech_started", "short-hi", map[string]any{"audio_start_ms": 0})
		r.preview(t, "short-hi", "text", "你好")
		r.event(t, "input_audio_buffer.speech_stopped", "short-hi", map[string]any{"audio_end_ms": 800})
		r.final(t, "short-hi", "你好")
		_ = r.expectGenerated(t)
		r.sync(t)
		r.s.mu.Lock()
		if r.s.speakerAnchor != "" {
			r.s.mu.Unlock()
			t.Fatalf("short turn locked speakerAnchor=%q, want empty", r.s.speakerAnchor)
		}
		r.s.output.turn.Cut = true
		r.s.output = nil
		r.s.mu.Unlock()

		// 2. Non-overlapped >=1.5s turn observed as guest_1 locks speakerAnchor = "guest_1".
		r.event(t, "input_audio_buffer.speech_started", "primary-turn", map[string]any{"audio_start_ms": 800})
		r.preview(t, "primary-turn", "text", "给我讲个")
		r.event(t, "input_audio_buffer.speech_stopped", "primary-turn", map[string]any{"audio_end_ms": 2400})
		r.sync(t)
		r.s.speakerResult(speakerlive.Result{ID: 2, Start: 12800, End: 38400, State: "temporary", Label: "guest_1", Reason: "candidate", AudioMS: 1600})
		r.final(t, "primary-turn", "给我讲个西湖的故事")
		_ = r.expectGenerated(t)
		r.sync(t)
		r.s.mu.Lock()
		if r.s.speakerAnchor != "guest_1" {
			r.s.mu.Unlock()
			t.Fatalf("speakerAnchor=%q, want guest_1", r.s.speakerAnchor)
		}
		r.s.mu.Unlock()

		// 3. While playing, bystander guest_2 speaks and must be rejected without cutting output.
		old := echoOutput(t, r, "西湖边上有一座雷峰塔，风景特别好。", 15)
		if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
			t.Fatal(err)
		}
		beforePrimary := r.primary.Load()
		r.event(t, "input_audio_buffer.speech_started", "bystander-guest2", map[string]any{"audio_start_ms": 2400})
		r.event(t, "input_audio_buffer.speech_stopped", "bystander-guest2", map[string]any{"audio_end_ms": 3950})
		r.sync(t)
		r.s.speakerResult(speakerlive.Result{ID: 4, Start: 38400, End: 63200, State: "temporary", Label: "guest_2", Reason: "candidate", AudioMS: 1550})
		r.final(t, "bystander-guest2", "今晚我们吃什么饭")
		r.sync(t)
		r.s.mu.Lock()
		if old.turn.Cut || r.s.output != old || r.primary.Load() != beforePrimary {
			r.s.mu.Unlock()
			t.Fatalf("bystander guest_2 interrupted or generated: cut=%v", old.turn.Cut)
		}
		r.s.mu.Unlock()

		// 4. Short near-field barge-in (<1.5s, too_short) while playing is allowed to interrupt.
		r.event(t, "input_audio_buffer.speech_started", "short-barge", map[string]any{"audio_start_ms": 3500})
		r.event(t, "input_audio_buffer.speech_stopped", "short-barge", map[string]any{"audio_end_ms": 3950})
		r.final(t, "short-barge", "换个故事吧")
		r.sync(t)
		r.s.mu.Lock()
		cut := old.turn.Cut
		old.turn.MarkDrainDone()
		r.s.mu.Unlock()
		if !cut {
			t.Fatal("near-field short command failed to interrupt")
		}
		_ = r.expectGenerated(t)
	})
}
