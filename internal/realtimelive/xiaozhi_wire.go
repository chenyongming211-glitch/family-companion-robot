package realtimelive

import (
	"errors"
	"familyrobot/internal/protocol"
	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"
	"math"
	"time"

	"github.com/gorilla/websocket"
)

type devicePlayback struct {
	sent, rawPlayed  int64
	sealed, draining bool
}
type xiaozhiWire struct {
	ws               *websocket.Conn
	version, frameMS int
	scope            string
	listening        bool // Read-loop owned; writes never consult it.
	wakeSamples      int
	startDeadline    time.Time
	blocks           map[int64]*devicePlayback // Session mutex owns all playback access.
}

func newXiaozhiWire(ws *websocket.Conn, version, frameMS int, scope string) *xiaozhiWire {
	return &xiaozhiWire{ws: ws, version: version, frameMS: frameMS, scope: scope, blocks: map[int64]*devicePlayback{}}
}
func (w *xiaozhiWire) readDeadline() time.Time {
	if w.listening {
		return time.Time{}
	}
	if w.startDeadline.IsZero() {
		w.startDeadline = time.Now().Add(8 * time.Second)
	}
	return w.startDeadline
}
func (w *xiaozhiWire) decodeAudio(b []byte, codec *realtimeaudio.Codec) ([]byte, error) {
	packet, _, err := protocol.Unwrap(w.version, b)
	if err != nil {
		return nil, err
	}
	pcm, err := codec.Decode(packet)
	if err != nil {
		return nil, err
	}
	if !w.listening {
		w.wakeSamples += len(pcm) / 2
		if w.wakeSamples > 32000 {
			return nil, errors.New("wake_cache_limit")
		}
		return nil, nil
	}
	return pcm, nil
}
func (w *xiaozhiWire) control(b []byte) (map[string]any, error) {
	e, err := decodeObject(b)
	if err != nil {
		return nil, err
	}
	switch e["type"] {
	case "mcp", "iot", "status", "ping":
		// Unnegotiated non-voice messages have no authority or model effects.
		// Some stock initialization messages omit session_id; ignore them within
		// the WebSocket's message-size bound rather than breaking voice startup.
		return nil, nil
	}
	if e["session_id"] != w.scope {
		return nil, errors.New("wrong_session_scope")
	}
	switch e["type"] {
	case "listen":
		switch e["state"] {
		case "detect":
			return nil, nil // Wake audio is never business input/history.
		case "start":
			if e["mode"] != "realtime" && e["mode"] != "auto" {
				return nil, errors.New("unsupported_listen_mode")
			}
			w.listening = true
			w.wakeSamples = 0
			return nil, nil
		case "stop":
			return map[string]any{"type": "stop", "session_id": w.scope}, nil
		default:
			return nil, errors.New("unsupported_listen_state")
		}
	case "abort":
		return map[string]any{"type": "interrupt", "session_id": w.scope}, nil
	case "playback":
		p, err := protocol.ParsePlayback(b)
		if err != nil {
			return nil, err
		}
		// Preserve the actual wire receipt; normalization happens under the core's
		// mutex against that exact turn's sent/sealed counters.
		e["type"] = "receipt"
		e["id"] = int64(p.PlaybackID)
		return e, nil
	default:
		return nil, errors.New("unknown_device_control")
	}
}
func (w *xiaozhiWire) sendEvent(e map[string]any) error {
	scope := map[string]any{"session_id": w.scope}
	switch e["type"] {
	case "ready":
		return w.ws.WriteJSON(protocol.Hello{Type: "hello", Version: w.version, Transport: "websocket", SessionID: w.scope, AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 24000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true}})
	case "begin":
		id, ok := wireInteger(e["id"])
		if !ok || id < 1 || id > math.MaxUint32 {
			return errors.New("playback_identity_exhausted")
		}
		for old := range w.blocks {
			if old < id-15 {
				delete(w.blocks, old)
			}
		}
		if w.blocks[id] != nil {
			return errors.New("duplicate_playback_begin")
		}
		w.blocks[id] = &devicePlayback{}
		scope["type"] = "tts"
		scope["state"] = "start"
		scope["playback_id"] = id
	case "drain", "end":
		id, ok := wireInteger(e["id"])
		block := w.blocks[id]
		if !ok || block == nil {
			return errors.New("unknown_playback_seal")
		}
		if e["type"] == "drain" {
			target, ok := wireInteger(e["sent"])
			if !ok || target != block.sent {
				return errors.New("playback_seal_mismatch")
			}
			block.draining = true
			scope["reason"] = "drain"
		}
		block.sealed = true
		scope["type"] = "tts"
		scope["state"] = "stop"
		scope["playback_id"] = id
		scope["sent_samples"] = block.sent
	case "status":
		return nil // Do not masquerade model generation as firmware TTS state.
	default:
		return errors.New("unknown_server_control")
	}
	return w.ws.WriteJSON(scope)
}
func (w *xiaozhiWire) sendAudio(id int64, packet []byte) error {
	block := w.blocks[id]
	if block == nil || block.sealed {
		return errors.New("audio_outside_playback")
	}
	b, err := protocol.Wrap(w.version, packet, 0)
	if err != nil {
		return err
	}
	if err = w.ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return err
	}
	block.sent += realtimestate.PacketSamples
	return nil
}
func (w *xiaozhiWire) normalizeReceipt(t *realtimestate.Turn, e map[string]any) (map[string]any, error) {
	id, ok := wireInteger(e["id"])
	raw, rok := wireInteger(e["played_samples"])
	block := w.blocks[id]
	if !ok || !rok || id != t.ID || block == nil || e["basis"] != "i2s_dma" || e["valid"] != true || e["interrupted"] == true || raw < block.rawPlayed || raw > t.Sent || block.sent != t.Sent {
		return nil, errors.New("invalid_i2s_receipt")
	}
	terminal := "progress"
	if e["state"] == "finished" {
		if !block.sealed || raw != block.sent || e["interrupted"] != false {
			return nil, errors.New("invalid_i2s_finished")
		}
		terminal = "played"
		if block.draining {
			terminal = "drained"
		}
	} else if e["state"] != "progress" {
		return nil, errors.New("invalid_i2s_state")
	}
	completed := raw / realtimestate.PacketSamples * realtimestate.PacketSamples
	// A completed hardware prefix also proves this much was received. The
	// protocol has no exact accepted counter, so never substitute server sent.
	canonical := map[string]any{"id": id, "basis": "i2s_dma", "completed": completed, "accepted": completed, "terminal": terminal}
	block.rawPlayed = raw
	return canonical, nil
}

// Keep compiler checking this adapter against the single core's interface.
var _ sessionWire = (*xiaozhiWire)(nil)
