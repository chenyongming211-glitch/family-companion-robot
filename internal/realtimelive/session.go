package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/music"
	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/speakerlive"
	"github.com/gorilla/websocket"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type liveTurn struct {
	media                   bool // Session.mu; media never supplies spoken text.
	musicClassified         bool // Session.mu; defer memory until music routing finishes.
	turn                    *realtimestate.Turn
	ctx                     context.Context
	cancel                  context.CancelFunc
	done                    chan struct{}
	stoppedAt, drainStarted time.Time
	asr                     *OutputASR
	transcriptReady         chan struct{}
	transcriptOnce          sync.Once
	transcriptWait          sync.Once // Memory and controls share one ASR budget.
	expressionContextReady  bool
	expressionStarted       bool
	expressionSent          bool
	expressionHistory       string
	expressionPending       *ExpressionResult
	expressionCancel        context.CancelFunc
}
type Session struct {
	music            *sessionMusic // Session.mu; discarded with this authenticated connection.
	ws               *websocket.Conn
	wire             sessionWire
	inputFrame       int
	cfg              Config
	directory        string
	product          *realtimestate.Product
	mu               sync.Mutex
	history          []*liveTurn
	ring             *realtimestate.AudioRing
	input            chan []byte
	listener         *Native
	controls         *deviceControls
	diag             *audioDiagnostics
	memory           *sessionMemory
	memoryFloor      int64 // Protected by mu; old playback state remains intact.
	speaker          speakerObserver
	speakerLatest    int64
	speakerFailed    bool
	providerEnd      int64
	seq              int64
	output           *liveTurn
	lastInput        *pendingInput
	items            map[string]*pendingInput
	itemOrder        []string
	changed          chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	rec              recorder
	manifest         SessionInfo
	ownerErr         error
	serverStopping   func() bool
	failureMu        sync.Mutex
	failureReason    string
	generate         func(context.Context, int64, string, []byte, func(string, []byte) error) error
	selectExpression func(context.Context, ExpressionInput) ExpressionResult
}

func NewSession(ws *websocket.Conn, cfg Config, dir string, product *realtimestate.Product) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	if cfg.MemoryOnly {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Minute)
	}
	cfg = cfg.snapshot()
	lc := cfg
	off := false
	lc.Search = &off
	s := &Session{ws: ws, cfg: cfg, directory: dir, product: product, ctx: ctx, cancel: cancel, ring: realtimestate.NewAudioRing(16000 * 35), input: make(chan []byte, 100), listener: NewNative(lc, dir), items: map[string]*pendingInput{}, changed: make(chan struct{}, 1), rec: recorder{dir: dir, memoryOnly: cfg.MemoryOnly}}
	s.wire = &macWire{ws: ws}
	s.inputFrame = 320
	info, err := ReadSessionInfo(dir)
	if err != nil && !os.IsNotExist(err) {
		s.ownerErr = err
	}
	s.manifest = info
	s.rec.owner = info.Ownership
	s.generate = s.generateNative
	s.selectExpression = func(ctx context.Context, input ExpressionInput) ExpressionResult {
		return SelectExpression(ctx, s.cfg, s.directory, input)
	}
	return s
}
func (s *Session) record(event string, m map[string]any) error {
	e := s.rec.add(event, m)
	if e != nil {
		s.failReason("record_write_failed")
	}
	if e == nil {
		s.notifyMemory(event)
		s.diag.observed(event, m)
	}
	return e
}
func (s *Session) fail() { s.failReason("session_error") }
func (s *Session) failReason(reason string) {
	s.failureMu.Lock()
	if s.failureReason == "" {
		s.failureReason = reason
	}
	s.failureMu.Unlock()
	s.cancel()
	s.ws.Close()
}
func (s *Session) terminalFailure() string {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	return s.failureReason
}
func (s *Session) spawn(fn func()) { s.wg.Add(1); go func() { defer s.wg.Done(); fn() }() }
func (s *Session) sendLocked(event map[string]any) error {
	if s.rec.owner.SessionID != "" {
		fields := event
		copy := make(map[string]any, len(fields)+1)
		for k, v := range fields {
			copy[k] = v
		}
		copy["session_id"] = s.rec.owner.SessionID
		event = copy
	}
	s.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return s.wire.sendEvent(event)
}
func (s *Session) send(event map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendLocked(event)
}
func (s *Session) sendThinking(rt *liveTurn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || rt.ctx.Err() != nil || rt.turn.Cut || s.output != rt {
		return nil
	}
	return s.sendLocked(map[string]any{"type": "status", "code": "thinking"})
}
func (s *Session) pcmTransport() bool {
	w, ok := s.wire.(*xiaozhiWire)
	return ok && w.pcm
}
func (s *Session) binary(rt *liveTurn, packet []byte) (bool, error) {
	return s.release(rt, packet, nil, nil)
}
func (s *Session) release(rt *liveTurn, packet []byte, codec *realtimeaudio.Codec, resampler *realtimeaudio.Resampler) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rt.turn.Cut || s.ctx.Err() != nil {
		return false, nil
	}
	rt.turn.Sent += 1440
	s.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if e := s.wire.sendAudio(rt.turn.ID, packet); e != nil {
		return true, e
	}
	if rt.asr != nil && (codec != nil || (s.pcmTransport() && resampler != nil)) {
		decoded := packet
		if codec != nil {
			var e error
			decoded, e = codec.Decode(packet)
			if e != nil {
				return true, e
			}
		}
		converted, e := resampler.Process(decoded)
		if e != nil {
			return true, e
		}
		rt.asr.Feed(converted)
	}
	return true, nil
}
func (s *Session) addTurn() *liveTurn {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return s.activateTurnLocked(s.seq, time.Now())
}

// Input admission reserves its ID at capture start and promotes it only after
// final ASR. addTurn remains the immediate allocator used by other turn tests.
func (s *Session) activateTurnLocked(id int64, stoppedAt time.Time) *liveTurn {
	if s.output != nil {
		s.retireExpressionLocked(s.output)
	}
	ctx, cancel := context.WithCancel(s.ctx)
	rt := &liveTurn{turn: realtimestate.NewTurn(id), ctx: ctx, cancel: cancel, done: make(chan struct{}), transcriptReady: make(chan struct{}), stoppedAt: stoppedAt}
	// Robot ownership comes from the authenticated adapter. Apply the same
	// playback credit to every robot; client messages cannot enlarge it.
	if s.ownerErr == nil && s.rec.owner.ClientType == "xiaozhi" && diagnosticDevicePattern.MatchString(s.rec.owner.DeviceID) {
		rt.turn.PlaybackWindowSamples = realtimestate.ExtendedWindowSamples
	}
	s.history = append(s.history, rt)
	s.output = rt
	return rt
}
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
func (s *Session) interrupt() error { return s.interruptFrom(nil, "unspecified") }
func (s *Session) interruptExpected(expected *liveTurn) error {
	return s.interruptFrom(expected, "generation_error")
}
func (s *Session) interruptFrom(expected *liveTurn, source string) error {
	return s.interruptInput(expected, source, nil)
}

func (s *Session) interruptInput(expected *liveTurn, source string, input *pendingInput) error {
	s.mu.Lock()
	if source == "device_abort" {
		s.discardInputLocked()
	}
	// Abort/teardown may invalidate a preview or final between its validation
	// and interruption. A retired capture must not cut a newer output.
	if input != nil && (s.ctx.Err() != nil || s.lastInput != input || input.retired) {
		s.mu.Unlock()
		return nil
	}
	rt := s.output
	var id int64
	if rt != nil {
		id = rt.turn.ID
	}
	if expected != nil {
		id = expected.turn.ID
	} // A late failure belongs to its retired turn.
	s.diag.interrupt(source, id, rt != nil && (expected == nil || expected == rt))
	if rt == nil || (expected != nil && rt != expected) {
		s.mu.Unlock()
		return nil
	}
	t := rt.turn
	_, device := s.wire.(*xiaozhiWire)
	playbackDone := closed(rt.done)
	if device {
		playbackDone = t.PlaybackFinal
	}
	if t.Sent > 0 && t.Completed == t.Sent && playbackDone {
		s.retireExpressionLocked(rt)
		s.output = nil
		s.mu.Unlock()
		return nil
	}
	t.SealForDrain()
	s.retireExpressionLocked(rt)
	rt.drainStarted = time.Now()
	s.output = nil
	rt.cancel()
	var err error
	if t.Begun {
		err = s.sendLocked(map[string]any{"type": "drain", "id": t.ID, "sent": *t.DrainTarget})
	} else {
		t.MarkDrainDone()
		if device {
			err = s.sendLocked(map[string]any{"type": "status", "code": "listening"})
		}
	}
	if expected != nil && err == nil {
		err = s.sendLocked(map[string]any{"type": "status", "code": "turn_failed"})
	}
	asr := rt.asr
	begun := t.Begun
	fields := map[string]any{"id": t.ID, "sent": t.Sent, "completed_before_receipt": t.Completed}
	s.mu.Unlock()
	if asr != nil {
		asr.Finish()
	}
	if begun {
		s.spawn(func() {
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-s.ctx.Done():
			case <-t.DrainDone:
			case <-timer.C:
				s.mu.Lock()
				f := map[string]any{"id": t.ID, "sent": t.Sent, "completed": t.Completed}
				s.mu.Unlock()
				s.record("drain_timeout", f)
				s.send(map[string]any{"type": "status", "code": "drain_failed"})
				s.failReason("drain_timeout")
			}
		})
	}
	if e := s.record("interrupt", fields); e != nil {
		return e
	}
	return err
}
func (s *Session) Run() (err error) {
	if s.ownerErr != nil {
		return s.ownerErr
	}
	if s.diag != nil {
		go s.diag.run()
		defer s.diag.stop()
	}
	if s.cfg.MemoryOnly {
		stop := context.AfterFunc(s.ctx, func() { s.ws.Close() })
		defer stop()
	}
	reason := "session_error"
	defer func() {
		s.cancel()
		s.listener.Close()
		s.ws.Close()
		if s.speaker != nil {
			s.speaker.Close()
		}
		s.wg.Wait()
		s.mu.Lock()
		history := append([]*liveTurn(nil), s.history...)
		s.mu.Unlock()
		for _, rt := range history {
			if rt.asr != nil {
				rt.asr.Close()
			}
		}
		if s.memory != nil {
			outcome := "saved"
			if e := s.checkpointMemory(); e != nil {
				outcome = "failed_or_unknown"
			}
			s.record("memory_finalized", map[string]any{"outcome": outcome})
		}
		if err != nil {
			s.record("session_error", map[string]any{"error_type": "session_terminated"})
		}
		if failure := s.terminalFailure(); failure != "" {
			reason = failure
			if err == nil {
				err = errors.New(failure)
			}
		}
		if s.serverStopping != nil && s.serverStopping() {
			reason = "server_shutdown"
		}
		s.record("closed", map[string]any{"turns": len(history), "reason": reason})
		if s.manifest.SessionID != "" {
			now := time.Now().UTC()
			s.manifest.EndedAt = &now
			s.manifest.EndReason = reason
			if e := privateJSON(filepath.Join(s.directory, "session.json"), s.manifest); e != nil && err == nil {
				err = e
			}
		}
	}()
	if e := s.record("session_started", map[string]any{}); e != nil {
		return e
	}
	if s.memory != nil {
		if e := s.loadMemory(); e != nil {
			s.record("memory_loaded", map[string]any{"outcome": "unavailable"})
		}
		s.spawn(s.runMemory)
	}
	var codec *realtimeaudio.Codec
	var e error
	if !s.pcmTransport() {
		codec, e = realtimeaudio.New(16000, s.inputFrame)
		if e != nil {
			return e
		}
	}
	defer codec.Close()
	_, device := s.wire.(*xiaozhiWire)
	ready := map[string]any{"type": "ready", "model": s.cfg.Model, "voice": s.cfg.Voice}
	if s.cfg.MemoryOnly {
		ready["speaker_test"] = true
		ready["speaker_enabled"] = s.cfg.Speaker != nil
	}
	// Firmware waits for server hello before enabling capture. Send it before
	// the provider handshake; the Mac retains its original ready ordering.
	if device {
		if e = s.send(ready); e != nil {
			return e
		}
	}
	if e = s.listener.Open(s.ctx, "仅检测语音轮次，不自动生成回复。", true); e != nil {
		return e
	}
	// Start discovery only once the client read loop can service replies;
	// provider setup must not consume the Bridge's discovery timeout.
	s.startDeviceControls()
	if !device {
		if e = s.send(ready); e != nil {
			return e
		}
	}
	if s.cfg.Speaker != nil {
		observer, err := speakerlive.New(s.ctx, s.rec.owner.SessionID, *s.cfg.Speaker, s.speakerResult)
		s.mu.Lock()
		s.speaker = observer
		s.mu.Unlock()
		if err != nil {
			s.speakerResult(speakerlive.Result{State: "unavailable"})
		}
	}
	s.spawn(func() {
		if e := s.microphone(); e != nil && s.ctx.Err() == nil {
			s.record("input_failed", map[string]any{"error_type": "input_provider_failed"})
			s.failReason("input_provider_failed")
		}
	})
	s.spawn(func() {
		if e := s.listen(); e != nil && s.ctx.Err() == nil {
			s.record("input_failed", map[string]any{"error_type": "input_provider_failed"})
			s.failReason("input_provider_failed")
		}
	})
	s.spawn(func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-tick.C:
				s.mu.Lock()
				e := s.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				s.mu.Unlock()
				if e != nil {
					s.fail()
					return
				}
			}
		}
	})
	if device {
		s.ws.SetReadLimit(16384)
	} else {
		s.ws.SetReadLimit(8192)
	}
	for {
		deadline := time.Time{}
		if w, ok := s.wire.(*xiaozhiWire); ok {
			deadline = w.readDeadline()
		}
		s.ws.SetReadDeadline(deadline)
		kind, b, e := s.ws.ReadMessage()
		if e != nil {
			// aiohttp's client iterator ends on transport close/error. Preserve
			// that boundary; parsing/codec/receipt failures still return errors.
			fields := map[string]any{}
			var closeError *websocket.CloseError
			if errors.As(e, &closeError) {
				fields["close_code"] = closeError.Code
			}
			reason = "transport_lost"
			if closeError != nil && closeError.Code == websocket.CloseNormalClosure {
				reason = "client_closed"
			}
			s.record("transport_closed", fields)
			if failure := s.terminalFailure(); failure != "" {
				return errors.New(failure)
			}
			return nil
		}
		if kind == websocket.BinaryMessage {
			s.diag.received(len(b))
			pcm, e := s.wire.decodeAudio(b, codec)
			if e != nil {
				s.diag.decodeFailed()
				return e
			}
			if len(pcm) == 0 {
				continue
			}
			s.diag.decoded(pcm)
			select {
			case s.input <- pcm:
				s.diag.queued(len(pcm)/2, len(s.input))
			default:
				return errors.New("microphone_backpressure")
			}
		}
		if kind == websocket.TextMessage {
			event, e := s.wire.control(b)
			if e != nil {
				return e
			}
			if event == nil {
				continue
			}
			if event["type"] == "mcp" && s.controls != nil {
				payload, _ := event["payload"].(json.RawMessage)
				if e = s.controls.Receive(payload); e != nil {
					return e
				}
				continue
			}
			if e = s.validateScope(event); e != nil {
				return e
			}
			switch event["type"] {
			case "stop":
				reason = "client_stop"
				return nil
			case "interrupt":
				if !device {
					return errors.New("unknown_client_control")
				}
				if e = s.interruptFrom(nil, "device_abort"); e != nil {
					return e
				}
			case "receipt":
				if e = s.receiveReceipt(event); e != nil {
					return e
				}
			default:
				return errors.New("unknown_client_control")
			}
		}
	}
}

func (s *Session) receiveReceipt(event map[string]any) error {
	s.mu.Lock()
	var rt *liveTurn
	for i := len(s.history) - 1; i >= 0; i-- {
		if id, ok := wireInteger(event["id"]); ok && s.history[i].turn.ID == id {
			rt = s.history[i]
			break
		}
	}
	if rt == nil {
		s.mu.Unlock()
		return errors.New("invalid_playback_receipt")
	}
	normalized, err := s.wire.normalizeReceipt(rt.turn, event)
	basis := "CoreAudio_dataPlayedBack"
	_, device := s.wire.(*xiaozhiWire)
	if device {
		basis = "i2s_dma"
	}
	if err != nil || !rt.turn.ReceiptWithBasis(normalized, basis) {
		if device {
			// Keep bounded protocol evidence, never arbitrary client fields or
			// provider messages, when an invalid hardware receipt closes a session.
			fields := map[string]any{"id": rt.turn.ID, "sent": rt.turn.Sent, "completed": rt.turn.Completed,
				"raw_played_samples": event["played_samples"], "valid": event["valid"],
				"interrupted": event["interrupted"], "state": event["state"]}
			if err != nil {
				fields["reason"] = err.Error() // Adapter errors are fixed local codes.
			} else {
				fields["reason"] = "playback_ledger_rejected"
			}
			s.record("playback_rejected", fields)
		}
		s.mu.Unlock()
		return errors.New("invalid_playback_receipt")
	}
	term := normalized["terminal"]
	if rt.turn.PlaybackFinal {
		s.retireExpressionLocked(rt)
	}
	var fields map[string]any
	if term == "drained" || term == "played" {
		var ms any
		if term == "drained" {
			ms = time.Since(rt.drainStarted).Milliseconds()
		}
		fields = map[string]any{"id": rt.turn.ID, "terminal": term, "completed": rt.turn.Completed, "accepted": rt.turn.Accepted, "sent": rt.turn.Sent, "drain_ms": ms}
		if device {
			fields["basis"] = basis
			fields["raw_played_samples"] = event["played_samples"]
			fields["accepted_basis"] = "played_lower_bound"
		}
	}
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
	if device {
		if err = s.record("device_playback", map[string]any{"id": rt.turn.ID, "state": event["state"], "basis": basis, "raw_played_samples": event["played_samples"], "completed": normalized["completed"], "accepted": normalized["accepted"], "accepted_basis": "played_lower_bound"}); err != nil {
			return err
		}
	}
	if fields != nil {
		return s.record("playback", fields)
	}
	return nil
}
func (s *Session) microphone() error {
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case pcm := <-s.input:
			s.diag.dequeued(len(pcm) / 2)
			s.mu.Lock()
			e := s.ring.Append(pcm)
			s.mu.Unlock()
			if e != nil {
				return e
			}
			started := s.diag.appendBegin()
			e = s.listener.Append(pcm)
			s.diag.appendEnd(len(pcm)/2, started, e != nil)
			if e != nil {
				return e
			}
			if s.cfg.Speaker != nil {
				s.mu.Lock()
				s.providerEnd += int64(len(pcm) / 2)
				s.mu.Unlock()
			}
		}
	}
}
func timestamp(v any) (float64, bool) {
	n, ok := v.(interface{ Float64() (float64, error) })
	if !ok {
		return 0, false
	}
	f, e := n.Float64()
	return f, e == nil
}
func (s *Session) listen() error {
	defer s.closeInputs()
	for {
		event, err := s.listener.ReadListener(s.ctx)
		if err != nil {
			return err
		}
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
		switch event["type"] {
		case "input_audio_buffer.speech_started":
			err = s.startInput(event)
		case "input_audio_buffer.speech_stopped":
			err = s.stopInput(event)
		case "conversation.item.input_audio_transcription.delta":
			err = s.previewInput(event)
		case "conversation.item.input_audio_transcription.completed":
			err = s.finishInput(event)
			// committed is only a provider acknowledgment. The item ID already
			// belongs to its capture; a late commit cannot bind it to another input.
		}
		if err != nil {
			return err
		}
	}
}
func (s *Session) settle(ctx context.Context, rt *liveTurn) (string, error) {
	s.mu.Lock()
	prior := []*liveTurn{}
	for _, v := range s.history {
		if v != rt && v.turn.ID >= s.memoryFloor {
			prior = append(prior, v)
		}
	}
	drains := []<-chan struct{}{}
	for _, v := range prior {
		if v.turn.DrainTarget != nil && !closed(v.turn.DrainDone) {
			drains = append(drains, v.turn.DrainDone)
		}
	}
	s.mu.Unlock()
	for _, ch := range drains {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-s.ctx.Done():
			return "", s.ctx.Err()
		case <-ch:
		}
	}
	started := time.Now()
	deadline := time.NewTimer(1200 * time.Millisecond)
	defer deadline.Stop()
	waiting := []<-chan struct{}{}
	s.mu.Lock()
	for _, v := range prior[max(0, len(prior)-realtimestate.RecentTurnLimit):] {
		if v.asr != nil && !closed(v.asr.Done) {
			waiting = append(waiting, v.asr.Done)
		}
	}
	s.mu.Unlock()
waitLoop:
	for _, ch := range waiting {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-s.ctx.Done():
			return "", s.ctx.Err()
		case <-deadline.C:
			break waitLoop
		case <-ch:
		}
	}
	s.mu.Lock()
	if rt.turn.Cut || s.ctx.Err() != nil {
		s.mu.Unlock()
		return "", context.Canceled
	}
	turns := []*realtimestate.Turn{}
	records := []map[string]any{}
	for _, v := range prior {
		if !v.media {
			turns = append(turns, v.turn)
		}
	}
	contextText := realtimestate.ContextFor(turns)
	for _, v := range prior[max(0, len(prior)-realtimestate.RecentTurnLimit):] {
		samples := int64(0)
		if v.asr != nil {
			samples = v.asr.InputSamples()
		}
		records = append(records, map[string]any{"id": v.turn.ID, "next_id": rt.turn.ID, "candidate": v.turn.Prefix(), "evidence": realtimestate.PlaybackEvidence(v.turn), "completed": v.turn.Completed, "sent": v.turn.Sent, "asr_input_samples": samples})
	}
	s.mu.Unlock()
	contextText = s.musicContext(s.mergeMemory(contextText))
	for _, r := range records {
		if e := s.record("settled_playback", r); e != nil {
			return "", e
		}
	}
	data, e := decodeObject([]byte(contextText))
	if e != nil {
		return "", e
	}
	if e = s.record("restored_context", map[string]any{"id": rt.turn.ID, "data": data, "extra_wait_ms": time.Since(started).Milliseconds()}); e != nil {
		return "", e
	}
	return contextText, nil
}
func (s *Session) generateNative(ctx context.Context, id int64, prompt string, pcm []byte, emit func(string, []byte) error) error {
	n := NewNative(s.cfg, s.directory)
	defer n.Close()
	n.OnSearch = func(m map[string]any) error {
		fields := map[string]any{"id": id}
		for k, v := range m {
			fields[k] = v
		}
		return s.record("native_search", fields)
	}
	if e := n.Open(ctx, prompt, false); e != nil {
		return e
	}
	if e := emit("ready", nil); e != nil {
		return e
	}
	return n.Stream(ctx, pcm, emit)
}

type encodedPacket struct {
	packet []byte
	err    error
	end    bool
}

func (s *Session) reply(rt *liveTurn, pcm []byte) {
	ctx := rt.ctx
	var asr *OutputASR
	var codec *realtimeaudio.Codec
	var e error
	pcmTransport := s.pcmTransport()
	if !pcmTransport {
		codec, e = realtimeaudio.New(24000, 1440)
		if e != nil {
			s.turnError(rt)
			return
		}
	}
	defer codec.Close()
	encodeFrame := func(pcm []byte) ([]byte, error) {
		if pcmTransport {
			return append([]byte(nil), pcm...), nil
		}
		return codec.Encode(pcm)
	}
	contextText, e := s.settle(ctx, rt)
	if e != nil {
		return
	}
	if e = s.sendThinking(rt); e != nil {
		s.fail()
		return
	}
	route, e := s.routeMusic(rt, contextText)
	if e != nil {
		return
	}
	contextText = s.musicContext(contextText)
	media := route.Stream != nil
	if media && (route.Duration < time.Second || route.Duration > 20*time.Minute) {
		s.turnError(rt)
		return
	}
	memoryReceipt, controlContext := "", ""
	if !route.Handled {
		contextText, memoryReceipt, e = s.routeMemory(rt, contextText)
		if e != nil {
			return
		}
		contextText = s.musicContext(contextText)
		s.expressionContextReady(rt, contextText)
		controlContext, e = s.routeDeviceControls(rt, contextText)
		if e != nil {
			return
		}
	}
	contextText, currentMusicGuidance, musicAllowed := s.musicReplyState(contextText, route)
	route.Guidance = currentMusicGuidance
	prompt, e := s.product.InstructionsFor(s.sessionFacts(), contextText, true, time.Now())
	if e != nil {
		s.turnError(rt)
		return
	}
	prompt += controlContext + memoryReceipt + route.Guidance
	producerCtx, cancel := context.WithCancel(ctx)
	queueFrames := 1002
	if media {
		cancel()
		// The source and synchronous encoded puts share one deadline, so a
		// stalled consumer cannot outlive the bounded media source/process.
		producerCtx, cancel = context.WithTimeout(ctx, route.Duration+30*time.Second)
		queueFrames = 10
	}
	defer cancel()
	encoded := make(chan encodedPacket, queueFrames)
	produced := make(chan struct{})
	prepared := make(chan struct{})
	var ready sync.Once
	go func() {
		defer close(produced)
		defer ready.Do(func() { close(prepared) })
		pending := []byte{}
		total, frames := 0, 0
		put := func(v encodedPacket) error {
			if v.packet != nil && musicAllowed != nil && !musicAllowed() {
				return music.ErrLibraryForbidden
			}
			select {
			case <-producerCtx.Done():
				return producerCtx.Err()
			case encoded <- v:
				return nil
			}
		}
		emit := func(kind string, value []byte) error {
			if producerCtx.Err() != nil {
				return producerCtx.Err()
			}
			if musicAllowed != nil && !musicAllowed() {
				return music.ErrLibraryForbidden
			}
			ready.Do(func() { close(prepared) })
			if kind == "ready" {
				return nil
			}
			if media && kind != "audio" {
				return errors.New("invalid_media_event")
			}
			if kind == "text" {
				s.mu.Lock()
				rt.turn.Generated = string(value)
				s.mu.Unlock()
				return nil
			}
			pending = append(pending, value...)
			total += len(value) / 2
			for len(pending) >= 2880 {
				packet, e := encodeFrame(pending[:2880])
				if e != nil {
					return e
				}
				if e = put(encodedPacket{packet: packet}); e != nil {
					return e
				}
				pending = pending[2880:]
				frames++
			}
			return nil
		}
		var e error
		if media {
			e = route.Stream(producerCtx, emit)
		} else {
			e = s.generate(producerCtx, rt.turn.ID, prompt, pcm, emit)
		}
		if e == nil && pcmTransport && total == 0 {
			// There is no PCM packet to begin/seal. Resolve only this turn through
			// the existing generation-error drain path; keep the Session usable.
			e = errors.New("empty_pcm_output")
		}
		if e == nil {
			tail := 312
			if pcmTransport {
				tail = 0 // Native PCM has no Opus encoder lookahead to flush.
			}
			target := (total + tail + 1439) / 1440
			for frames < target {
				chunk := make([]byte, 2880)
				n := copy(chunk, pending)
				pending = pending[n:]
				packet, er := encodeFrame(chunk)
				if er != nil {
					e = er
					break
				}
				if er = put(encodedPacket{packet: packet}); er != nil {
					e = er
					break
				}
				frames++
			}
		}
		put(encodedPacket{err: e, end: e == nil})
	}()
	defer func() {
		cancel()
		<-produced
		if asr != nil {
			asr.Finish()
		}
		s.mu.Lock()
		text, sent := rt.turn.Generated, rt.turn.Sent
		s.mu.Unlock()
		if ctx.Err() != nil {
			s.record("generated_interrupted", map[string]any{"id": rt.turn.ID, "text": text, "sent": sent})
		}
	}()
	// Opening the native session has its own 12s budget; the baseline 40s
	// packet wait starts only after Open returns.
	select {
	case <-producerCtx.Done():
		if ctx.Err() == nil {
			s.turnError(rt)
		}
		return
	case <-prepared:
	}
	resampler := realtimeaudio.NewResampler()
	for {
		var p encodedPacket
		timer := time.NewTimer(40 * time.Second)
		select {
		case <-producerCtx.Done():
			timer.Stop()
			if ctx.Err() == nil {
				s.turnError(rt)
			}
			return
		case <-timer.C:
			s.turnError(rt)
			return
		case p = <-encoded:
			timer.Stop()
		}
		if p.err != nil {
			if ctx.Err() == nil {
				s.turnError(rt)
			}
			return
		}
		if musicAllowed != nil && !musicAllowed() {
			s.turnError(rt)
			return
		}
		if p.end {
			break
		}
		for {
			s.mu.Lock()
			allowed := rt.turn.CanSend()
			cut := rt.turn.Cut
			s.mu.Unlock()
			if cut {
				return
			}
			if allowed {
				break
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-producerCtx.Done():
				timer.Stop()
				if ctx.Err() == nil {
					s.turnError(rt)
				}
				return
			case <-timer.C:
				s.turnError(rt)
				return
			case <-s.changed:
				timer.Stop()
			}
		}
		s.mu.Lock()
		if musicAllowed != nil && !musicAllowed() {
			s.mu.Unlock()
			s.turnError(rt)
			return
		}
		if rt.turn.Cut || s.ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		if !rt.turn.Begun {
			rt.turn.Begun = true
			e = s.sendLocked(map[string]any{"type": "begin", "id": rt.turn.ID})
			if e != nil {
				s.mu.Unlock()
				s.fail()
				return
			}
			if !media {
				if e = s.sendExpressionLocked(rt); e != nil {
					s.mu.Unlock()
					s.fail()
					return
				}
				asr = NewOutputASR(s.ctx, s.cfg, s.directory, func(sentence map[string]any, elapsed int64) error {
					s.mu.Lock()
					rt.turn.Tracker.Add(sentence, float64(elapsed))
					s.mu.Unlock()
					return s.record("output_asr_final", map[string]any{"id": rt.turn.ID, "elapsed_ms": elapsed, "sentence": sentence})
				})
				rt.asr = asr
			}
			if e = s.record("first_packet", map[string]any{"id": rt.turn.ID, "speech_stop_to_packet_ms": time.Since(rt.stoppedAt).Milliseconds()}); e != nil {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
		ok, e := s.release(rt, p.packet, codec, resampler)
		if e != nil {
			s.fail()
			return
		}
		if !ok {
			return
		}
	}
	s.mu.Lock()
	if rt.turn.Cut || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	rt.turn.Ended = true
	text, sent := rt.turn.Generated, rt.turn.Sent
	e = s.sendLocked(map[string]any{"type": "end", "id": rt.turn.ID, "generated_text": text})
	s.mu.Unlock()
	if e != nil {
		s.fail()
		return
	}
	s.record("generated", map[string]any{"id": rt.turn.ID, "text": text, "sent": sent})
}
func (s *Session) turnError(rt *liveTurn) {
	s.record("turn_error", map[string]any{"id": rt.turn.ID, "error_type": "generation_failed"})
	s.interruptExpected(rt)
}
