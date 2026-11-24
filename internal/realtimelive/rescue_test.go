package realtimelive

import (
	"testing"
	"time"

	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"
)

// A cover line is filler: short, a statement, and only as long as it takes to
// say it. Anything else means the model answered instead, and playing it would
// make the user hear the answer twice.
func TestRescueAcceptableRejectsAnswersAndLongLines(t *testing.T) {
	short := make([]byte, rescueRate*2) // 1s at 24k mono
	for _, tc := range []struct {
		name string
		said string
		pcm  []byte
		want bool
	}{
		{"short statement", "我看看啊", short, true},
		{"six characters", "我查一下啊", make([]byte, rescueRate*3/2), true},
		// 什么 inside a statement is reported speech, not a question. Rejecting
		// it left a real search turn silent for 4s.
		{"statement with 什么", "看看有什么新片", short, true},
		{"empty text", "", short, false},
		{"no audio", "我看看啊", nil, false},
		{"question", "你是想找哪一类的", short, false},
		{"question opening", "你想看哪种", short, false},
		{"question ending", "这样好吗", short, false},
		{"ascii question", "which one?", short, false},
		{"too long", "这个我得好好想一下再慢慢跟你说说看", short, false},
		{"too much audio", "我看看啊", make([]byte, rescueRate*2*4), false},
	} {
		if got := rescueAcceptable(tc.said, tc.pcm); got != tc.want {
			t.Errorf("%s: rescueAcceptable(%q)=%v want %v", tc.name, tc.said, got, tc.want)
		}
	}
}

// Routes A and B speak the answer in a different voice, so a line from the
// configured voice would be a second speaker. Media never waits on a model.
func TestRescueEligibleOnlyOnNativeVoiceRoute(t *testing.T) {
	s, _ := newPCMSession(t)
	if s.rescueEligible(false) {
		t.Fatal("an unconfigured session must not try a cover line")
	}
	s.cfg.URL, s.cfg.Key, s.cfg.Voice = "ws://127.0.0.1:1/realtime", "key", "Tina"
	if !s.rescueEligible(false) {
		t.Fatal("route C with a complete config must cover slow turns")
	}
	if s.rescueEligible(true) {
		t.Fatal("media has its own immediate output")
	}
	for _, route := range []string{"A", "B"} {
		s.voiceRoute = route
		if s.rescueEligible(false) {
			t.Fatalf("route %s speaks in another voice and must not cover", route)
		}
	}
}

// The cover line goes out in whole packets, and a cut stops it mid-line rather
// than finishing it over the user's speech.
func TestStreamPCMSendsWholePackets(t *testing.T) {
	s, c := newPCMSession(t)
	rt := s.addTurn()
	rt.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	rt.asr = &OutputASR{queue: make(chan []byte, 72), Done: make(chan struct{}), cancel: func() {}}
	defer close(rt.asr.Done)
	encode := func(p []byte) ([]byte, error) { return append([]byte(nil), p...), nil }
	pcm := make([]byte, 2880*2+101)
	cut, err := s.streamPCM(rt, pcm, encode, nil, realtimeaudio.NewResampler(), nil)
	if cut || err != nil {
		t.Fatalf("streamPCM cut=%v err=%v", cut, err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("packet %d: %v", i+1, err)
		}
	}

	s.mu.Lock()
	rt.turn.Cut = true
	s.mu.Unlock()
	cut, err = s.streamPCM(rt, make([]byte, 2880*4), encode, nil, realtimeaudio.NewResampler(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cut {
		t.Fatal("a cut turn must stop the cover line")
	}
}

// A cover line is synthesized in one piece. Pushing it all at once is what
// closed the connection on every search turn, so sending must wait for the
// client's playback credit exactly like the streaming path does.
func TestStreamPCMWaitsForPlaybackCredit(t *testing.T) {
	s, c := newPCMSession(t)
	rt := s.addTurn()
	rt.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	rt.asr = &OutputASR{queue: make(chan []byte, 72), Done: make(chan struct{}), cancel: func() {}}
	defer close(rt.asr.Done)
	encode := func(p []byte) ([]byte, error) { return append([]byte(nil), p...), nil }
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.streamPCM(rt, make([]byte, 2880*20), encode, nil, realtimeaudio.NewResampler(), nil); err != nil {
			t.Error(err)
		}
	}()
	// The device entry gets the extended window; ask the turn rather than
	// assuming which one applies.
	s.mu.Lock()
	credit := rt.turn.PlaybackWindowSamples
	s.mu.Unlock()
	if credit != realtimestate.ExtendedWindowSamples {
		credit = realtimestate.WindowSamples
	}
	window := credit / 1440
	for i := int64(0); i < window; i++ {
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("packet %d: %v", i+1, err)
		}
	}
	select {
	case <-done:
		t.Fatal("sender ran past the playback credit window")
	case <-time.After(150 * time.Millisecond):
	}

	s.mu.Lock()
	rt.turn.Completed = credit
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
	for i := int64(0); i < window; i++ {
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("after credit, packet %d: %v", i+1, err)
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sender never resumed after credit arrived")
	}
}

// A cover line is filler, not part of the answer. It must not enter OutputASR
// (where it would pollute assistant_spoken_prefix and cross-session memory),
// while the real answer that follows on the same turn must still be transcribed.
func TestStreamPCMKeepsCoverOutOfOutputASR(t *testing.T) {
	s, c := newPCMSession(t)
	rt := s.addTurn()
	rt.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	rt.asr = &OutputASR{queue: make(chan []byte, 72), Done: make(chan struct{}), cancel: func() {}}
	defer close(rt.asr.Done)
	resampler := realtimeaudio.NewResampler()
	encode := func(p []byte) ([]byte, error) { return append([]byte(nil), p...), nil }

	cut, err := s.streamPCM(rt, make([]byte, 2880*2), encode, nil, resampler, nil)
	if cut || err != nil {
		t.Fatalf("streamPCM cut=%v err=%v", cut, err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("cover packet %d: %v", i+1, err)
		}
	}
	if got := len(rt.asr.queue); got != 0 {
		t.Fatalf("cover packets fed %d chunks into OutputASR, want 0", got)
	}

	ok, err := s.release(rt, make([]byte, 2880), nil, resampler)
	if !ok || err != nil {
		t.Fatalf("release ok=%v err=%v", ok, err)
	}
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.asr.queue); got != 1 {
		t.Fatalf("answer packet fed %d chunks into OutputASR, want 1", got)
	}
}

// On a recall or device-control turn, search_gate turns search off, yet memory
// selection or control routing can spend ~1s before the main session even opens.
// Measuring 1.5s from speech_stop alone clamped the wait to 200ms after Open and
// fired a redundant rescue call on non-search turns.
func TestRescueWaitHonorsSearchGateAndPreOpenRouting(t *testing.T) {
	on, off := true, false
	stopped := time.Unix(100, 0)
	afterRecall := stopped.Add(1150 * time.Millisecond)

	// Search turn ("明天杭州天气"): still fires 1.5s after speech_stop (350ms left).
	if got := rescueWait(stopped, afterRecall, "明天杭州天气怎么样", &on); got != 350*time.Millisecond {
		t.Fatalf("search turn wait=%v, want 350ms", got)
	}
	// Late search turn: clamped to rescueMinWait (200ms).
	if got := rescueWait(stopped, stopped.Add(1450*time.Millisecond), "明天杭州天气怎么样", &on); got != rescueMinWait {
		t.Fatalf("late search turn wait=%v, want %v", got, rescueMinWait)
	}
	// Recall turn ("记不记得我上次说的"): search is gated off, so give the open
	// main session a full rescueNoSearchDelay (1200ms) instead of firing at 350ms.
	if got := rescueWait(stopped, afterRecall, "记不记得我上次说的", &on); got != rescueNoSearchDelay {
		t.Fatalf("recall turn wait=%v, want %v", got, rescueNoSearchDelay)
	}
	// Search disabled by config (--search=0): also uses rescueNoSearchDelay floor.
	if got := rescueWait(stopped, afterRecall, "明天杭州天气怎么样", &off); got != rescueNoSearchDelay {
		t.Fatalf("search-off config wait=%v, want %v", got, rescueNoSearchDelay)
	}
}

// If the answer arrives before the cover line sends its first packet, the cover
// line must be dropped completely. Once the short cover phrase has started
// speaking, it must finish its remaining frames rather than chopping a word in
// half when the answer's first packet arrives mid-phrase.
func TestStreamPCMDropsUnstartedCoverButFinishesStartedPhrase(t *testing.T) {
	s, c := newPCMSession(t)
	rt := s.addTurn()
	rt.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	encode := func(p []byte) ([]byte, error) { return append([]byte(nil), p...), nil }

	var alreadyReady answerClock
	alreadyReady.arrive()
	cut, err := s.streamPCM(rt, make([]byte, 2880*2), encode, nil, realtimeaudio.NewResampler(), &alreadyReady)
	if cut || err != nil {
		t.Fatalf("unstarted cover cut=%v err=%v", cut, err)
	}
	s.mu.Lock()
	sentBefore := rt.turn.Sent
	s.mu.Unlock()
	if sentBefore != 0 {
		t.Fatalf("unstarted cover sent %d samples when answer had already arrived, want 0", sentBefore)
	}

	var midPhrase answerClock
	frameCount := 0
	encodeAndArrive := func(p []byte) ([]byte, error) {
		frameCount++
		if frameCount == 1 {
			midPhrase.arrive()
		}
		return append([]byte(nil), p...), nil
	}
	cut, err = s.streamPCM(rt, make([]byte, 2880*3), encodeAndArrive, nil, realtimeaudio.NewResampler(), &midPhrase)
	if cut || err != nil {
		t.Fatalf("started cover cut=%v err=%v", cut, err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("started cover packet %d: %v", i+1, err)
		}
	}
	s.mu.Lock()
	sentAfter := rt.turn.Sent
	s.mu.Unlock()
	if sentAfter != 1440*3 {
		t.Fatalf("started cover sent %d samples, want %d (full 3-frame phrase)", sentAfter, 1440*3)
	}
}

// While a cover line is playing on the speaker, the main answer's Generated
// text has not arrived yet. An echo of the cover line itself must still be
// recognized as an opening echo so it does not cut the turn before the answer.
func TestRescueOpeningEchoDoesNotInterruptAnswer(t *testing.T) {
	r := newEchoAdmissionRig(t)
	old := echoOutput(t, r, "", 10)
	r.s.mu.Lock()
	old.rescueText = "我看看啊"
	r.s.mu.Unlock()
	if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
		t.Fatal(err)
	}
	r.start(t, "cover-echo")
	r.preview(t, "cover-echo", "text", "我看看")
	r.sync(t)
	r.s.mu.Lock()
	cut := old.turn.Cut || r.s.output != old
	r.s.mu.Unlock()
	if cut {
		t.Fatal("echo of the cover line cut the active turn")
	}
}
