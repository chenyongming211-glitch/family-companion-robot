// Package protocol implements the documented Xiaozhi WebSocket wire format.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
)

type AudioParams struct {
	Format        string `json:"format"`
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
}
type Hello struct {
	Type        string          `json:"type"`
	Version     int             `json:"version"`
	Transport   string          `json:"transport"`
	SessionID   string          `json:"session_id,omitempty"`
	AudioParams AudioParams     `json:"audio_params"`
	Features    map[string]bool `json:"features,omitempty"`
}
type Control struct {
	PlaybackID uint32 `json:"playback_id,omitempty"`
	Type       string `json:"type"`
	State      string `json:"state,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Mode       string `json:"mode,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Text       string `json:"text,omitempty"`
}

func ParseHello(data []byte, version int) (Hello, error) {
	var h Hello
	if len(data) > 16384 || json.Unmarshal(data, &h) != nil {
		return h, errors.New("invalid hello JSON")
	}
	a := h.AudioParams
	if h.Type != "hello" || h.Transport != "websocket" || h.Version < 1 || h.Version > 3 || h.Version != version {
		return h, errors.New("unsupported hello protocol")
	}
	if a.Format != "opus" || a.SampleRate != 16000 || a.Channels != 1 || (a.FrameDuration != 20 && a.FrameDuration != 40 && a.FrameDuration != 60) {
		return h, errors.New("expected mono 16kHz Opus in 20/40/60ms frames")
	}
	return h, nil
}
func Unwrap(version int, data []byte) ([]byte, uint32, error) {
	var timestamp uint32
	switch version {
	case 1:
	case 2:
		if len(data) < 16 || binary.BigEndian.Uint16(data) != 2 || binary.BigEndian.Uint16(data[2:]) != 0 || int(binary.BigEndian.Uint32(data[12:])) != len(data)-16 {
			return nil, 0, errors.New("invalid v2 envelope")
		}
		timestamp = binary.BigEndian.Uint32(data[8:])
		data = data[16:]
	case 3:
		if len(data) < 4 || data[0] != 0 || int(binary.BigEndian.Uint16(data[2:])) != len(data)-4 {
			return nil, 0, errors.New("invalid v3 envelope")
		}
		data = data[4:]
	default:
		return nil, 0, errors.New("unsupported audio protocol")
	}
	if len(data) == 0 || len(data) > 4096 {
		return nil, 0, errors.New("invalid Opus packet size")
	}
	return data, timestamp, nil
}
func Wrap(version int, payload []byte, timestamp uint32) ([]byte, error) {
	if len(payload) == 0 || len(payload) > 4096 {
		return nil, errors.New("invalid Opus packet size")
	}
	switch version {
	case 1:
		return payload, nil
	case 2:
		b := make([]byte, 16+len(payload))
		binary.BigEndian.PutUint16(b, 2)
		binary.BigEndian.PutUint32(b[8:], timestamp)
		binary.BigEndian.PutUint32(b[12:], uint32(len(payload)))
		copy(b[16:], payload)
		return b, nil
	case 3:
		b := make([]byte, 4+len(payload))
		binary.BigEndian.PutUint16(b[2:], uint16(len(payload)))
		copy(b[4:], payload)
		return b, nil
	default:
		return nil, errors.New("unsupported audio protocol")
	}
}
