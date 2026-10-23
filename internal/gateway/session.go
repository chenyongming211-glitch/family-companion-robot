package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/devicetools"
	"familyrobot/internal/protocol"
	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

type deviceEvent struct {
	kind int
	data []byte
	err  error
}
type modelEvent struct {
	generation uint64
	event      qwen.Event
	err        error
}
type session struct {
	clearConfirmationKey   string
	clearToolsDisabled     bool
	clearResult            json.RawMessage
	suppressResponse       string
	suppressGeneration     uint64
	nativeClear            *nativeClearConnection
	nativeReady            chan nativeOpenResult
	taskDisposition        string
	records                *conversationRecords
	responseAt             time.Time
	playbackEnabled        bool
	playbackSequence       uint32
	taskSequence           uint64
	playbacks              []*playbackRecord
	activePlayback         *playbackRecord
	devicePlayback         *playbackRecord
	paused                 *pausedTask
	snapshotRevision       uint64
	responseTaskID         string
	sync                   generationSync
	route                  taskRouting
	routeSequence          uint64
	routeResults           chan taskRouteResult
	classifyPausedTask     func(context.Context, qwen.Config, string, string, bool) (string, error)
	clarification          clarificationGuard
	voiceRequested         bool
	currentInput           requestOrigin
	responseOrigin         requestOrigin
	inputCommitted         bool
	wake                   wakeHandoff
	listenStarted          bool
	ctx                    context.Context
	device                 *websocket.Conn
	cfg                    Config
	version                int
	id                     string
	input                  chan deviceEvent
	events                 chan modelEvent
	model                  *qwen.Client
	generation             uint64
	decoder                *audio.Decoder
	encoder                *audio.Encoder
	pending                *audio.Buffer
	hist                   history
	mode                   string
	responseID, transcript string
	draftTranscript        string
	responseAudioSent      bool
	retired                retiredResponses
	responseDone, started  bool
	nextAudio              time.Time
	lastAudio              time.Time
	playbackNow            func() time.Time
	writeAudio             func([]byte) error
	inputTail              []byte
	inputBytes             int
	manualCommit           bool
	awaitingResponse       bool
	inputActive            bool
	lastActivity           time.Time
	bridge                 *devicetools.Bridge
	toolCalls              map[string]*toolCallState
	toolOrder              []string
	toolSeen               map[string]bool
	toolCount              int
	toolParentDone         bool
	toolContinuation       bool
	toolPlayback           bool
}

// Native realtime audio can arrive much faster than its 60ms device cadence.
// Keep up to 120s of 24kHz mono16 pending (5.76MB), still finite per session.
// This is server storage, not audio sent ahead to the device; abort clears it.
const maxPendingAudioBytes = 24000 * 2 * 120

func newSession(ctx context.Context, c *websocket.Conn, cfg Config, version int) *session {
	var b [12]byte
	rand.Read(b[:])
	s := &session{ctx: ctx, device: c, cfg: cfg, version: version, id: hex.EncodeToString(b[:]), input: make(chan deviceEvent, 64), events: make(chan modelEvent, 64), pending: audio.NewBuffer(maxPendingAudioBytes), mode: "auto", lastActivity: time.Now(), playbackNow: time.Now}
	s.writeAudio = func(data []byte) error { return s.write(websocket.BinaryMessage, data) }
	return s
}
func (s *session) write(kind int, data []byte) error {
	s.device.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if s.device.WriteMessage(kind, data) != nil {
		return errors.New("device write failed")
	}
	return nil
}
func (s *session) json(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.write(websocket.TextMessage, b)
}
func (s *session) tts(state, text string) error {
	v := protocol.Control{Type: "tts", State: state, Text: text, SessionID: s.id}
	if s.playbackEnabled && s.activePlayback != nil {
		v.PlaybackID = s.activePlayback.id
	}
	if err := s.json(v); err != nil {
		return err
	}
	if state == "start" {
		s.traceVoice("tts_start")
	} else if state == "stop" {
		s.traceVoice("tts_stop")
	}
	return nil
}
func (s *session) run() error {
	s.device.SetReadLimit(16384)
	s.device.SetReadDeadline(time.Now().Add(8 * time.Second))
	kind, data, err := s.device.ReadMessage()
	if err != nil || kind != websocket.TextMessage {
		return errors.New("device hello missing")
	}
	hello, err := protocol.ParseHello(data, s.version)
	if err != nil {
		return err
	}
	defer func() {
		s.clearClarification()
		s.playbacks = nil
		s.activePlayback = nil
		s.devicePlayback = nil
		s.paused = nil
		s.currentInput = requestOrigin{}
		s.responseOrigin = requestOrigin{}
		s.wake = wakeHandoff{}
	}()
	defer s.finalizeRecords()
	s.playbackEnabled = hello.Features["playback_progress_v1"]
	features := map[string]bool(nil)
	if s.playbackEnabled {
		features = map[string]bool{"playback_progress_v1": true}
	}
	// This bridge does not implement server-side AEC; do not imply otherwise.
	slog.Info("device_connected", "session", s.id, "protocol", s.version, "input_frame_ms", hello.AudioParams.FrameDuration, "server_aec_requested", hello.Features["aec"], "playback_progress_enabled", s.playbackEnabled)
	if err := s.json(protocol.Hello{Type: "hello", Version: s.version, Transport: "websocket", SessionID: s.id, Features: features, AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 24000, Channels: 1, FrameDuration: 60}}); err != nil {
		return err
	}
	s.device.SetReadDeadline(time.Time{})
	s.decoder, err = audio.NewDecoder()
	if err != nil {
		return err
	}
	defer s.decoder.Close()
	s.encoder, err = audio.NewEncoder()
	if err != nil {
		return err
	}
	defer func() { s.encoder.Close() }()
	ctx, cancel := context.WithCancel(s.ctx)
	s.ctx = ctx
	defer cancel()
	defer s.cancelRouting()
	go s.readDevice(ctx, cancel)
	buffered, err := s.discoverTools(ctx, hello.Features["mcp"])
	if err != nil {
		return err
	}
	if err := s.connect(ctx); err != nil {
		s.alert()
		return err
	}
	s.lastActivity = time.Now()
	defer func() {
		if s.model != nil {
			s.model.Close()
		}
	}()
	for _, ev := range buffered {
		if err := s.onDevice(ctx, ev); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		changed := true
		select {
		case <-ctx.Done():
			return nil
		case ev := <-s.input:
			changed = ev.kind == websocket.TextMessage
			if ev.err != nil {
				return nil
			}
			if err := s.onDevice(ctx, ev); err != nil {
				s.alert()
				return err
			}
		case result := <-s.nativeReady:
			if err := s.finishNativeClear(result); err != nil {
				return err
			}
		case result := <-s.routeResults:
			if err := s.finishRouting(result); err != nil {
				return err
			}
		case ev := <-s.events:
			changed = ev.event.Type != "response.audio.delta" && ev.event.Type != "response.audio_transcript.delta"
			if ev.generation != s.generation {
				continue
			}
			if ev.err != nil {
				s.alert()
				return ev.err
			}
			if err := s.onModel(ctx, ev.event); err != nil {
				s.alert()
				return err
			}
		case <-ticker.C:
			changed = false
			now := time.Now()
			if !s.wake.deadline.IsZero() && !now.Before(s.wake.deadline) {
				return errors.New("wake input handshake timed out")
			}
			if now.Sub(s.lastActivity) > s.cfg.IdleTimeout {
				return errors.New("device idle timeout")
			}
			if s.bridge != nil {
				if err := s.applyToolUpdate(s.bridge.Expire(now)); err != nil {
					return err
				}
			}
			if err := s.play(); err != nil {
				return err
			}
		}
		s.pollRecords()
		if changed {
			s.checkpointRecords()
		}
		if err := s.advanceGeneration(time.Now()); err != nil {
			return err
		}
		if err := s.continueTools(); err != nil {
			s.alert()
			return err
		}
	}
}
func (s *session) readDevice(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	for {
		kind, data, err := s.device.ReadMessage()
		select {
		case s.input <- deviceEvent{kind, data, err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}
func (s *session) connect(ctx context.Context) error {
	if s.generation > 0 {
		s.listenStarted = true
		s.wake = wakeHandoff{}
	}
	s.clearClarification()
	s.sync = generationSync{}
	s.clearToolsDisabled = false
	s.cancelRouting()
	s.voiceRequested = false
	s.responseTaskID = ""
	s.responseAudioSent = false
	s.inputActive = false
	if s.model != nil {
		s.model.Close()
		s.model = nil
	}
	s.cancelTools()
	s.generation++
	s.currentInput = requestOrigin{}
	s.inputCommitted = false
	s.toolContinuation = false
	s.responseOrigin = requestOrigin{}
	s.retired = retiredResponses{}
	cfg := s.toolConfig()
	c, err := qwen.Open(ctx, cfg, s.mode, s.hist.context())
	if err != nil {
		return err
	}
	s.model = c
	s.readModel(ctx, c)
	return nil
}
func (s *session) alert() {
	s.json(map[string]any{"type": "alert", "status": "连接中断", "message": "语音连接暂时不可用，请重新唤醒。", "emotion": "neutral"})
}
func (s *session) onDevice(ctx context.Context, ev deviceEvent) error {
	if s.nativeClear != nil {
		return s.bufferNativeClearInput(ev)
	}
	if ev.kind == websocket.BinaryMessage {
		packet, _, err := protocol.Unwrap(s.version, ev.data)
		if err != nil {
			return err
		}
		pcm, err := s.decoder.Decode(packet)
		if err != nil {
			return err
		}
		return s.receiveInput(pcm)
	}
	if ev.kind != websocket.TextMessage {
		return nil
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(ev.data, &envelope) != nil {
		return errors.New("invalid device control JSON")
	}
	if envelope.Type == "playback" {
		s.receivePlayback(ev.data)
		return nil
	}
	var v protocol.Control
	if json.Unmarshal(ev.data, &v) != nil {
		return errors.New("invalid device control JSON")
	}
	if v.SessionID != "" && v.SessionID != s.id {
		return errors.New("wrong device session ID")
	}
	switch v.Type {
	case "abort":
		s.traceVoice("device_abort")
		s.lastActivity = time.Now()
		return s.interrupt(ctx, false, true)
	case "listen":
		switch v.State {
		case "start":
			if v.Mode != "auto" && v.Mode != "manual" && v.Mode != "realtime" {
				return errors.New("unsupported listen mode")
			}
			first := !s.listenStarted
			if !first && !s.wake.deadline.IsZero() {
				if v.Mode != s.mode {
					return errors.New("listen mode changed during wake handshake")
				}
				return nil
			}
			if !first && (s.responseID != "" || s.started || s.awaitingResponse || len(s.toolCalls) > 0) {
				if err := s.interrupt(ctx, false, false); err != nil {
					return err
				}
			}
			s.listenStarted = true
			changed := s.mode != v.Mode
			s.mode = v.Mode
			s.inputActive = v.Mode == "manual"
			if s.inputActive {
				s.lastActivity = time.Now()
			}
			s.traceVoice("listen_start")
			if !s.wake.deadline.IsZero() {
				return s.advanceWake()
			}
			if changed || s.wake.manual {
				s.wake.manual = false
				return s.model.SetMode(v.Mode)
			}
		case "stop":
			s.traceVoice("listen_stop")
			return s.stopInput()
		case "detect":
			s.lastActivity = time.Now()
			return s.detectWake()
		default:
			return errors.New("unsupported listen state")
		}
	case "mcp":
		return s.receiveTools(ev.data, time.Now())
	case "iot": // Legacy IoT commands are not part of the fixed MCP control surface.
	}
	return nil
}
func (s *session) appendAudio(pcm []byte) error {
	return s.model.Send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm)})
}
func (s *session) interrupt(ctx context.Context, replay, force bool) error {
	// Explicit device abort also cancels uncommitted input, before a model
	// response exists. A first speech_started event alone is not an abort.
	target := s.interruptiblePlayback()
	if !force && target == nil && s.responseID == "" && !s.started && !s.awaitingResponse && len(s.toolCalls) == 0 {
		return nil
	}
	s.cancelRouting()
	// Stop playback before any network reconnect. No queued old audio survives.
	s.pausePlayback()
	stop := protocol.Control{Type: "tts", State: "stop", Reason: "interrupted", SessionID: s.id}
	if s.playbackEnabled && target != nil {
		stop.PlaybackID = target.id
	}
	if err := s.json(stop); err != nil {
		return err
	}
	s.traceVoice("interrupt")
	s.pending.Clear()
	if s.started && s.responseID != "" {
		if !s.responseOrigin.wake && !s.suppressedResponse() {
			s.hist.addInterrupted(s.draftTranscript)
			s.hist.stampLast(s.id, s.responseID, s.responseOrigin.itemID, 0)
		}
	}
	s.hist.Interrupted = true
	if !s.responseDone {
		s.model.Send(map[string]any{"type": "response.cancel"})
	}
	s.clearClarification()
	s.responseID = ""
	s.responseAudioSent = false
	s.responseDone = false
	s.started = false
	s.activePlayback = nil
	s.devicePlayback = nil
	s.transcript = ""
	s.draftTranscript = ""
	s.lastAudio = time.Time{}
	s.manualCommit = false
	s.awaitingResponse = false
	s.toolPlayback = false
	s.encoder.Close()
	var err error
	s.encoder, err = audio.NewEncoder()
	if err != nil {
		return err
	}
	if !replay {
		s.inputTail = nil
		s.inputBytes = 0
	}
	if err := s.connect(ctx); err != nil {
		return err
	}
	if replay && len(s.inputTail) > 0 {
		return s.appendAudio(s.inputTail)
	}
	return nil
}
func (s *session) onModel(ctx context.Context, ev qwen.Event) error {
	if (ev.Type == "input_audio_buffer.committed" || ev.Type == "conversation.item.input_audio_transcription.completed") && len(ev.ItemID) > 256 {
		return errors.New("upstream input identity exceeded limits")
	}
	if ev.ResponseID != "" && s.retired.contains(ev.ResponseID) || ev.Type == "response.created" && s.retired.contains(ev.Response.ID) || ev.Type == "response.done" && s.retired.contains(ev.Response.ID) {
		return nil
	}
	if err := s.guardClarification(ev); err != nil {
		return err
	}
	if (s.route.active || (s.route.decided && (s.sync.requested || s.sync.token != 0))) && strings.HasPrefix(ev.Type, "response.") {
		return errors.New("unsolicited main response during task classification")
	}
	switch ev.Type {
	case "session.updated":
		return s.acknowledgeSnapshot()
	case "error":
		return errors.New("upstream rejected a realtime event")
	case "input_audio_buffer.speech_started":
		if !s.wake.deadline.IsZero() {
			return errors.New("unexpected VAD during wake handshake")
		}
		s.listenStarted = true
		s.traceVoice("vad_started")
		if err := s.interrupt(ctx, true, false); err != nil {
			return err
		}
		s.inputActive = true
		s.lastActivity = time.Now()
	case "input_audio_buffer.speech_stopped":
		s.traceVoice("vad_stopped")
		s.inputActive = false
		s.lastActivity = time.Now()
	case "input_audio_buffer.committed":
		if ev.ItemID != "" && ev.ItemID == s.wake.inputID {
			return nil
		}
		wakeCommit := s.wake.commitPending
		if wakeCommit && ev.ItemID == "" {
			return errors.New("wake commit identity missing")
		}
		if !wakeCommit && !s.wake.deadline.IsZero() {
			return errors.New("unexpected commit during wake handshake")
		}
		if !wakeCommit {
			s.listenStarted = true
		}
		s.traceVoice("input_committed")
		if err := s.commitOrigin(ev.ItemID); err != nil {
			return err
		}
		if !wakeCommit {
			s.inputActive = false
		}
		s.lastActivity = time.Now()
		s.inputTail = nil
		s.inputBytes = 0
		s.awaitingResponse = true
		if wakeCommit {
			s.currentInput.wake = true
			s.wake.inputID = ev.ItemID
			s.wake.commitPending = false
			return s.advanceWake()
		}
		s.manualCommit = false
		return s.requestResponse()
	case "conversation.item.input_audio_transcription.completed":
		s.traceVoice("transcription_ready")
		s.toolCount = 0
		text := limitText(ev.Transcript, 2048)
		if err := s.transcribeOrigin(ev.ItemID, ev.Transcript); err != nil {
			return err
		}
		if text != "" {
			s.lastActivity = time.Now()
		}
		if text != "" && !s.currentInput.wake && s.listenStarted && !s.wake.commitPending && (s.wake.inputID == "" || ev.ItemID != s.wake.inputID) {
			s.hist.add("user", text)
			s.hist.stampLast(s.id, "", ev.ItemID, 0)
		}
		return s.json(protocol.Control{Type: "stt", SessionID: s.id, Text: text})
	case "response.created":
		if !s.wake.deadline.IsZero() && !s.wake.responsePending {
			return errors.New("unsolicited response during wake handshake")
		}
		if ev.Response.ID == "" || len(ev.Response.ID) > 256 {
			return errors.New("upstream response ID missing")
		}
		if s.retired.contains(ev.Response.ID) {
			return nil
		}
		if s.route.active || (s.route.decided && (!s.voiceRequested || s.sync.requested || s.sync.token != 0)) {
			return errors.New("unsolicited response before voice snapshot acknowledgement")
		}
		s.voiceRequested = false
		if s.responseID != "" || len(s.toolCalls) > 0 {
			return errors.New("overlapping upstream responses")
		}
		if !s.wake.responsePending {
			s.listenStarted = true
		}
		s.lastActivity = time.Now()
		// A tool continuation shares the open TTS block and encoder. Firmware
		// resets its decoder only at TTS start, and resumes listening on stop.
		if !s.toolPlayback {
			if !s.toolContinuation {
				s.responseTaskID = ""
				s.responseOrigin = s.currentInput
				if s.wake.responsePending {
					s.responseOrigin = s.wake.origin
				}
			}
			s.encoder.Close()
			var err error
			s.encoder, err = audio.NewEncoder()
			if err != nil {
				return err
			}
			s.started = false
		}
		s.toolPlayback = false
		s.toolContinuation = false
		s.sync.requested = false
		s.responseID = ev.Response.ID
		s.responseAt = time.Now()
		s.responseAudioSent = false
		s.awaitingResponse = false
		s.responseDone = false
		s.transcript = ""
		s.draftTranscript = ""
		s.nextAudio = time.Time{}
		s.lastAudio = time.Time{}
		s.traceVoice("response_created")
		if s.wake.responsePending {
			s.wake.responsePending = false
			return s.finishWake()
		}
	case "response.function_call_arguments.done":
		return s.callTool(ev)
	case "response.audio.delta":
		if ev.ResponseID == "" || ev.ResponseID != s.responseID || s.responseDone {
			return nil
		}
		pcm, err := base64.StdEncoding.DecodeString(ev.Delta)
		if err != nil {
			return errors.New("invalid upstream audio")
		}
		if len(pcm) > 0 {
			s.lastActivity = time.Now()
		}
		return s.pending.Append(pcm)
	case "response.audio_transcript.delta":
		if ev.ResponseID == s.responseID && s.responseID != "" && !s.responseDone {
			s.draftTranscript = limitText(s.draftTranscript+ev.Delta, 2048)
		}
	case "response.audio_transcript.done":
		if ev.ResponseID != s.responseID || s.responseID == "" || s.responseDone {
			return nil
		}
		s.transcript = limitText(ev.Transcript, 2048)
		s.draftTranscript = s.transcript
		if s.started {
			return s.tts("sentence_start", s.transcript)
		}
	case "response.done":
		if ev.Response.ID != s.responseID || s.responseID == "" || s.responseDone {
			return nil
		}
		if ev.Response.Status != "completed" {
			s.traceVoice("response_cancelled")
			// Native VAD may cancel before speech_started. Preserve its already
			// uploaded, uncommitted prefix just as the speech_started path does.
			// Explicit aborts, failed responses and manual mode discard input.
			replay := ev.Response.Status == "cancelled" && s.mode != "manual"
			return s.interrupt(ctx, replay, false)
		}
		s.traceVoice("response_completed")
		if s.pending.Len()%2 != 0 {
			return errors.New("upstream PCM ended with a partial sample")
		}
		s.responseDone = true
		if len(s.toolCalls) > 0 {
			s.toolParentDone = true
		}
	}
	return nil
}

func (s *session) readModel(ctx context.Context, c *qwen.Client) {
	generation := s.generation
	go func() {
		for {
			ev, err := c.Read()
			select {
			case s.events <- modelEvent{generation, ev, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
}
