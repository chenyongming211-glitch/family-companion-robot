package gateway

import (
	"encoding/json"
	"errors"
	"time"

	"familyrobot/internal/protocol"
	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

type nativeOpenResult struct {
	client *qwen.Client
	err    error
}
type nativeClearConnection struct {
	key    string
	input  requestOrigin
	prefix []byte
	queued []deviceEvent
	bytes  int
}

// Clear is an active-conversation barrier even if durable deletion later fails.
// Retire the old socket immediately, before another input can generate against
// its native items. Only the authorized command/result and uncommitted new input
// may cross the barrier; no parent text/audio or old native items are replayed.
func (s *session) beginNativeClear(key string) error {
	if s.model == nil {
		return nil
	} // A storage-only session has no native conversation to retire.
	call := s.toolCalls[key]
	target := s.interruptiblePlayback()
	if s.started || target != nil {
		s.pausePlayback()
		stop := protocol.Control{Type: "tts", State: "stop", Reason: "interrupted", SessionID: s.id}
		if s.playbackEnabled && target != nil {
			stop.PlaybackID = target.id
		}
		if err := s.json(stop); err != nil {
			return err
		}
	}

	s.pending.Clear()
	s.model.Close()
	s.model = nil
	s.cancelTools()
	s.cancelRouting()
	s.generation++
	s.retired = retiredResponses{}
	call.generation = s.generation
	s.toolCalls = map[string]*toolCallState{key: call}
	s.toolOrder = []string{key}
	s.clearConfirmationKey = key
	s.toolParentDone = true
	if s.records != nil && s.records.clear != nil {
		s.records.clear.generation = s.generation
	}
	s.currentInput.generation = s.generation
	s.responseOrigin = s.currentInput
	s.responseID = ""
	s.responseDone = false
	s.responseAudioSent = false
	s.transcript = ""
	s.draftTranscript = ""
	s.started = false
	s.activePlayback = nil
	s.devicePlayback = nil
	s.toolPlayback = false
	s.toolContinuation = false
	s.awaitingResponse = false
	s.voiceRequested = false
	s.sync = generationSync{}
	s.clearToolsDisabled = false
	s.lastAudio = time.Time{}
	s.nextAudio = time.Time{}
	s.nativeClear = &nativeClearConnection{key: key, input: s.currentInput, prefix: append([]byte(nil), s.inputTail...)}
	s.inputTail = nil
	s.inputBytes = 0
	s.nativeReady = make(chan nativeOpenResult)
	ready, ctx, cfg, mode := s.nativeReady, s.ctx, s.toolConfig(), s.mode
	go func() {
		c, err := qwen.Open(ctx, cfg, mode, "")
		select {
		case ready <- nativeOpenResult{c, err}:
		case <-ctx.Done():
			if c != nil {
				c.Close()
			}
		}
	}()
	return nil
}

// The event loop keeps consuming device input throughout the upstream handshake.
// An abort stops immediately and discards only the explicitly aborted input.
func (s *session) bufferNativeClearInput(ev deviceEvent) error {
	n := s.nativeClear
	if ev.kind == websocket.TextMessage {
		var v protocol.Control
		if json.Unmarshal(ev.data, &v) != nil {
			return errors.New("invalid device control JSON")
		}
		if v.SessionID != "" && v.SessionID != s.id {
			return errors.New("wrong device session ID")
		}
		if v.Type == "abort" {
			s.cancelTools()
			n.prefix = nil
			n.queued = nil
			n.bytes = 0
			s.lastActivity = time.Now()
			return s.json(protocol.Control{Type: "tts", State: "stop", Reason: "interrupted", SessionID: s.id})
		}
	}
	if len(n.queued) >= 256 || n.bytes+len(ev.data) > 1<<20 {
		return errors.New("clear reconnect input buffer exceeded")
	}
	n.queued = append(n.queued, ev)
	n.bytes += len(ev.data)
	return nil
}

func (s *session) finishNativeClear(result nativeOpenResult) error {
	n := s.nativeClear
	s.nativeReady = nil
	s.nativeClear = nil
	if result.err != nil {
		return result.err
	}
	s.model = result.client
	s.readModel(s.ctx, s.model)
	// Explicit new capture supersedes the confirmation, just as on an ordinary
	// connection. Do this before replay so it needs no second reconnect and does
	// not throw away input accumulated while the handshake was pending.
	for _, ev := range n.queued {
		if ev.kind == websocket.TextMessage {
			var v protocol.Control
			json.Unmarshal(ev.data, &v)
			if v.Type == "listen" && v.State == "start" {
				s.cancelTools()
				break
			}
		}
	}
	if s.toolCalls[n.key] != nil {
		if err := s.model.Send(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": n.input.text}}}}); err != nil {
			return err
		}
	}
	if len(n.prefix) > 0 {
		if err := s.forwardInput(n.prefix); err != nil {
			return err
		}
	}
	for _, ev := range n.queued {
		if err := s.onDevice(s.ctx, ev); err != nil {
			return err
		}
	}
	return nil
}
