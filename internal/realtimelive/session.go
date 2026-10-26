package realtimelive

import (
	"context"
	"errors"
	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

type liveTurn struct {
	turn                    *realtimestate.Turn
	ctx                     context.Context
	cancel                  context.CancelFunc
	done                    chan struct{}
	stoppedAt, drainStarted time.Time
	asr                     *OutputASR
}
type Session struct {
	ws                *websocket.Conn
	wire              sessionWire
	inputFrame        int
	cfg               Config
	directory         string
	product           *realtimestate.Product
	mu                sync.Mutex
	history           []*liveTurn
	ring              *realtimestate.AudioRing
	input             chan []byte
	listener          *Native
	speechStart       *int64
	seq               int64
	output, lastInput *liveTurn
	items             map[string]*liveTurn
	itemOrder         []string
	changed           chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	rec               recorder
	manifest          SessionInfo
	ownerErr          error
	serverStopping    func() bool
	failureMu         sync.Mutex
	failureReason     string
	generate          func(context.Context, int64, string, []byte, func(string, []byte) error) error
}

func NewSession(ws *websocket.Conn, cfg Config, dir string, product *realtimestate.Product) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	cfg = cfg.snapshot()
	lc := cfg
	off := false
	lc.Search = &off
	s := &Session{ws: ws, cfg: cfg, directory: dir, product: product, ctx: ctx, cancel: cancel, ring: realtimestate.NewAudioRing(16000 * 35), input: make(chan []byte, 100), listener: NewNative(lc, dir), items: map[string]*liveTurn{}, changed: make(chan struct{}, 1), rec: recorder{dir: dir}}
	s.wire = &macWire{ws: ws}
	s.inputFrame = 320
	info, err := ReadSessionInfo(dir)
	if err != nil && !os.IsNotExist(err) {
		s.ownerErr = err
	}
	s.manifest = info
	s.rec.owner = info.Ownership
	s.generate = s.generateNative
	return s
}
func (s *Session) record(event string, m map[string]any) error {
	e := s.rec.add(event, m)
	if e != nil {
		s.failReason("record_write_failed")
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
	if codec != nil {
		decoded, e := codec.Decode(packet)
		if e != nil {
			return true, e
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
	ctx, cancel := context.WithCancel(s.ctx)
	rt := &liveTurn{turn: realtimestate.NewTurn(s.seq), ctx: ctx, cancel: cancel, done: make(chan struct{}), stoppedAt: time.Now()}
	s.history = append(s.history, rt)
	s.output = rt
	s.lastInput = rt
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
func (s *Session) interrupt() error { return s.interruptExpected(nil) }
func (s *Session) interruptExpected(expected *liveTurn) error {
	s.mu.Lock()
	rt := s.output
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
		s.output = nil
		s.mu.Unlock()
		return nil
	}
	t.SealForDrain()
	rt.drainStarted = time.Now()
	s.output = nil
	rt.cancel()
	var err error
	if t.Begun {
		err = s.sendLocked(map[string]any{"type": "drain", "id": t.ID, "sent": *t.DrainTarget})
	} else {
		t.MarkDrainDone()
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
	reason := "session_error"
	defer func() {
		s.cancel()
		s.listener.Close()
		s.ws.Close()
		s.wg.Wait()
		s.mu.Lock()
		history := append([]*liveTurn(nil), s.history...)
		s.mu.Unlock()
		for _, rt := range history {
			if rt.asr != nil {
				rt.asr.Close()
			}
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
	codec, e := realtimeaudio.New(16000, s.inputFrame)
	if e != nil {
		return e
	}
	defer codec.Close()
	_, device := s.wire.(*xiaozhiWire)
	ready := map[string]any{"type": "ready", "model": s.cfg.Model, "voice": s.cfg.Voice}
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
	if !device {
		if e = s.send(ready); e != nil {
			return e
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
			pcm, e := s.wire.decodeAudio(b, codec)
			if e != nil {
				return e
			}
			if len(pcm) == 0 {
				continue
			}
			select {
			case s.input <- pcm:
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
				if e = s.interrupt(); e != nil {
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
			s.mu.Lock()
			e := s.ring.Append(pcm)
			s.mu.Unlock()
			if e != nil {
				return e
			}
			if e = s.listener.Append(pcm); e != nil {
				return e
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
	for {
		event, e := s.listener.ReadListener(s.ctx)
		if e != nil {
			return e
		}
		switch event["type"] {
		case "input_audio_buffer.speech_started":
			if e = s.interrupt(); e != nil {
				return e
			}
			s.mu.Lock()
			end := s.ring.End
			pos, valid := timestamp(event["audio_start_ms"])
			start := max(int64(0), end-8000)
			if valid && pos >= 0 && pos*16 <= float64(end) {
				start = int64(pos * 16)
			}
			s.speechStart = &start
			s.mu.Unlock()
			if e = s.record("speech_started", map[string]any{"sample": start, "received_end": end, "provider_timestamp": valid}); e != nil {
				return e
			}
			if e = s.send(map[string]any{"type": "status", "code": "listening"}); e != nil {
				return e
			}
		case "input_audio_buffer.speech_stopped":
			s.mu.Lock()
			end := s.ring.End
			pos, valid := timestamp(event["audio_end_ms"])
			if valid && pos > 0 {
				end = min(end, int64(pos*16))
			}
			start := max(int64(0), end-8000)
			if s.speechStart != nil {
				start = *s.speechStart
			}
			pcm := s.ring.Slice(start, end)
			s.speechStart = nil
			s.mu.Unlock()
			if pcm == nil || len(pcm) > 16000*2*25 {
				if e = s.send(map[string]any{"type": "status", "code": "input_too_long"}); e != nil {
					return e
				}
				continue
			}
			if e = s.interrupt(); e != nil {
				return e
			}
			rt := s.addTurn()
			if e = s.record("input_segment", map[string]any{"id": rt.turn.ID, "samples": len(pcm) / 2}); e != nil {
				return e
			}
			s.spawn(func() { defer close(rt.done); s.reply(rt, pcm) })
		case "input_audio_buffer.committed":
			id, _ := event["item_id"].(string)
			s.mu.Lock()
			if s.lastInput != nil {
				if _, ok := s.items[id]; !ok {
					s.itemOrder = append(s.itemOrder, id)
				}
				s.items[id] = s.lastInput
				if len(s.itemOrder) > 64 {
					delete(s.items, s.itemOrder[0])
					s.itemOrder = s.itemOrder[1:]
				}
			}
			s.mu.Unlock()
		case "conversation.item.input_audio_transcription.completed":
			id, _ := event["item_id"].(string)
			text, valid := event["transcript"].(string)
			s.mu.Lock()
			rt := s.items[id]
			if rt != nil && valid && utf8.RuneCountInString(text) <= 4096 {
				rt.turn.User = text
			}
			s.mu.Unlock()
			if rt != nil && valid && utf8.RuneCountInString(text) <= 4096 {
				if e = s.record("user_text", map[string]any{"id": rt.turn.ID, "text": text}); e != nil {
					return e
				}
			}
		}
	}
}
func (s *Session) settle(ctx context.Context, rt *liveTurn) (string, error) {
	s.mu.Lock()
	prior := []*liveTurn{}
	for _, v := range s.history {
		if v != rt {
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
		turns = append(turns, v.turn)
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
	codec, e := realtimeaudio.New(24000, 1440)
	if e != nil {
		s.turnError(rt)
		return
	}
	defer codec.Close()
	contextText, e := s.settle(ctx, rt)
	if e != nil {
		return
	}
	if e = s.send(map[string]any{"type": "status", "code": "thinking"}); e != nil {
		s.fail()
		return
	}
	prompt, e := s.product.InstructionsFor(s.cfg.facts(), contextText, true, time.Now())
	if e != nil {
		s.turnError(rt)
		return
	}
	producerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	encoded := make(chan encodedPacket, 1002)
	produced := make(chan struct{})
	prepared := make(chan struct{})
	var ready sync.Once
	go func() {
		defer close(produced)
		defer ready.Do(func() { close(prepared) })
		pending := []byte{}
		total, frames := 0, 0
		put := func(v encodedPacket) error {
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
			ready.Do(func() { close(prepared) })
			if kind == "ready" {
				return nil
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
				packet, e := codec.Encode(pending[:2880])
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
		e := s.generate(producerCtx, rt.turn.ID, prompt, pcm, emit)
		if e == nil {
			target := (total + 312 + 1439) / 1440
			for frames < target {
				chunk := make([]byte, 2880)
				n := copy(chunk, pending)
				pending = pending[n:]
				packet, er := codec.Encode(chunk)
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
	case <-ctx.Done():
		return
	case <-prepared:
	}
	resampler := realtimeaudio.NewResampler()
	for {
		var p encodedPacket
		timer := time.NewTimer(40 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
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
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.turnError(rt)
				return
			case <-s.changed:
				timer.Stop()
			}
		}
		s.mu.Lock()
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
			asr = NewOutputASR(s.ctx, s.cfg, s.directory, func(sentence map[string]any, elapsed int64) error {
				s.mu.Lock()
				rt.turn.Tracker.Add(sentence, float64(elapsed))
				s.mu.Unlock()
				return s.record("output_asr_final", map[string]any{"id": rt.turn.ID, "elapsed_ms": elapsed, "sentence": sentence})
			})
			rt.asr = asr
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
