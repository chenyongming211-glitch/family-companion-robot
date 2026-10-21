package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
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
	toolPlayback           bool
}

func newSession(ctx context.Context, c *websocket.Conn, cfg Config, version int) *session {
	var b [12]byte
	rand.Read(b[:])
	s := &session{ctx: ctx, device: c, cfg: cfg, version: version, id: hex.EncodeToString(b[:]), input: make(chan deviceEvent, 64), events: make(chan modelEvent, 64), pending: audio.NewBuffer(48000 * 20), mode: "auto", lastActivity: time.Now(), playbackNow: time.Now}
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
	return s.json(protocol.Control{Type: "tts", State: state, Text: text, SessionID: s.id})
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
	// This bridge does not implement server-side AEC; do not imply otherwise.
	slog.Info("device_connected", "session", s.id, "protocol", s.version, "input_frame_ms", hello.AudioParams.FrameDuration, "server_aec_requested", hello.Features["aec"])
	if err := s.json(protocol.Hello{Type: "hello", Version: s.version, Transport: "websocket", SessionID: s.id, AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 24000, Channels: 1, FrameDuration: 60}}); err != nil {
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
	defer cancel()
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
		select {
		case <-ctx.Done():
			return nil
		case ev := <-s.input:
			if ev.err != nil {
				return nil
			}
			if err := s.onDevice(ctx, ev); err != nil {
				s.alert()
				return err
			}
		case ev := <-s.events:
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
			now := time.Now()
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
	s.inputActive = false
	if s.model != nil {
		s.model.Close()
		s.model = nil
	}
	s.cancelTools()
	s.generation++
	cfg := s.toolConfig()
	c, err := qwen.Open(ctx, cfg, s.mode, s.hist.context())
	if err != nil {
		return err
	}
	s.model = c
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
	return nil
}
func (s *session) alert() {
	s.json(map[string]any{"type": "alert", "status": "连接中断", "message": "语音连接暂时不可用，请重新唤醒。", "emotion": "neutral"})
}
func (s *session) onDevice(ctx context.Context, ev deviceEvent) error {
	if ev.kind == websocket.BinaryMessage {
		packet, _, err := protocol.Unwrap(s.version, ev.data)
		if err != nil {
			return err
		}
		pcm, err := s.decoder.Decode(packet)
		if err != nil {
			return err
		}
		// Listening firmware streams silence too. Only a VAD speech interval
		// or an explicit manual capture makes these packets user activity.
		if s.inputActive {
			s.lastActivity = time.Now()
		}
		s.inputBytes += len(pcm)
		s.inputTail = append(s.inputTail, pcm...)
		if len(s.inputTail) > 64000 {
			s.inputTail = append([]byte(nil), s.inputTail[len(s.inputTail)-64000:]...)
		}
		return s.appendAudio(pcm)
	}
	if ev.kind != websocket.TextMessage {
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
		s.lastActivity = time.Now()
		return s.interrupt(ctx, false, true)
	case "listen":
		switch v.State {
		case "start":
			if v.Mode != "auto" && v.Mode != "manual" && v.Mode != "realtime" {
				return errors.New("unsupported listen mode")
			}
			if s.responseID != "" || s.awaitingResponse || len(s.toolCalls) > 0 {
				if err := s.interrupt(ctx, false, false); err != nil {
					return err
				}
			}
			s.inputActive = v.Mode == "manual"
			if s.inputActive {
				s.lastActivity = time.Now()
			}
			if s.mode != v.Mode {
				s.mode = v.Mode
				return s.model.SetMode(v.Mode)
			}
		case "stop":
			if s.inputActive {
				s.lastActivity = time.Now()
			}
			s.inputActive = false
			if s.mode == "manual" && s.inputBytes >= 3200 && !s.manualCommit {
				s.manualCommit = true
				s.awaitingResponse = true
				return s.model.Send(map[string]any{"type": "input_audio_buffer.commit"})
			}
		case "detect": // Wake-word audio is handled by the normal audio stream.
			s.lastActivity = time.Now()
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
	if !force && s.responseID == "" && !s.started && !s.awaitingResponse && len(s.toolCalls) == 0 {
		return nil
	}
	// Stop playback before any network reconnect. No queued old audio survives.
	if err := s.json(protocol.Control{
		Type: "tts", State: "stop", Reason: "interrupted", SessionID: s.id,
	}); err != nil {
		return err
	}
	s.pending.Clear()
	s.hist.Interrupted = true
	if !s.responseDone {
		s.model.Send(map[string]any{"type": "response.cancel"})
	}
	s.responseID = ""
	s.responseDone = false
	s.started = false
	s.transcript = ""
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
	switch ev.Type {
	case "error":
		return errors.New("upstream rejected a realtime event")
	case "input_audio_buffer.speech_started":
		if err := s.interrupt(ctx, true, false); err != nil {
			return err
		}
		s.inputActive = true
		s.lastActivity = time.Now()
	case "input_audio_buffer.speech_stopped":
		s.inputActive = false
		s.lastActivity = time.Now()
	case "input_audio_buffer.committed":
		s.inputActive = false
		s.lastActivity = time.Now()
		s.inputTail = nil
		s.inputBytes = 0
		s.awaitingResponse = true
		if s.manualCommit {
			s.manualCommit = false
			return s.model.Send(map[string]any{"type": "response.create"})
		}
	case "conversation.item.input_audio_transcription.completed":
		s.toolCount = 0
		text := limitText(ev.Transcript, 2048)
		if text != "" {
			s.lastActivity = time.Now()
		}
		s.hist.add("user", text)
		return s.json(protocol.Control{Type: "stt", SessionID: s.id, Text: text})
	case "response.created":
		if ev.Response.ID == "" || len(ev.Response.ID) > 256 {
			return errors.New("upstream response ID missing")
		}
		if s.responseID != "" || len(s.toolCalls) > 0 {
			return errors.New("overlapping upstream responses")
		}
		s.lastActivity = time.Now()
		// A tool continuation shares the open TTS block and encoder. Firmware
		// resets its decoder only at TTS start, and resumes listening on stop.
		if !s.toolPlayback {
			s.encoder.Close()
			var err error
			s.encoder, err = audio.NewEncoder()
			if err != nil {
				return err
			}
			s.started = false
		}
		s.toolPlayback = false
		s.responseID = ev.Response.ID
		s.awaitingResponse = false
		s.responseDone = false
		s.transcript = ""
		s.nextAudio = time.Time{}
		s.lastAudio = time.Time{}
	case "response.function_call_arguments.done":
		return s.callTool(ev)
	case "response.audio.delta":
		if ev.ResponseID == "" || ev.ResponseID != s.responseID {
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
	case "response.audio_transcript.done":
		if ev.ResponseID != s.responseID || s.responseID == "" {
			return nil
		}
		s.transcript = limitText(ev.Transcript, 2048)
		if s.started {
			return s.tts("sentence_start", s.transcript)
		}
	case "response.done":
		if ev.Response.ID != s.responseID || s.responseID == "" {
			return nil
		}
		if ev.Response.Status != "completed" {
			return s.interrupt(ctx, false, false)
		}
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
