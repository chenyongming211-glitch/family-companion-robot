package realtimelive

import (
	"context"
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
	if err := source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_started", "audio_start_ms": 0}); err != nil {
		t.Fatal(err)
	}
	if err := source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_stopped", "audio_end_ms": 2000}); err != nil {
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
	// The fake observer intentionally never emits an identity result.
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
	_ = source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_started", "audio_start_ms": 0})
	_ = source.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_stopped", "audio_end_ms": 26000})
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
