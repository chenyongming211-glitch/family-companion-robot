package realtimelive

import (
	"encoding/binary"
	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"

	"github.com/gorilla/websocket"
)

// The voice/session state machine is shared. A wire only adapts terminal bytes
// and validates evidence from its authenticated terminal kind.
type sessionWire interface {
	decodeAudio([]byte, *realtimeaudio.Codec) ([]byte, error)
	control([]byte) (map[string]any, error)
	sendEvent(map[string]any) error
	sendAudio(int64, []byte) error
	normalizeReceipt(*realtimestate.Turn, map[string]any) (map[string]any, error)
}

type macWire struct{ ws *websocket.Conn }

// Outbound core events use Go integers; inbound JSON remains strict json.Number.
func wireInteger(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	default:
		return integer(v)
	}
}

func (w *macWire) decodeAudio(b []byte, codec *realtimeaudio.Codec) ([]byte, error) {
	return codec.Decode(b)
}
func (w *macWire) control(b []byte) (map[string]any, error) { return decodeObject(b) }
func (w *macWire) sendEvent(event map[string]any) error     { return w.ws.WriteJSON(event) }
func (w *macWire) sendAudio(id int64, packet []byte) error {
	b := make([]byte, 4+len(packet))
	binary.BigEndian.PutUint32(b, uint32(id))
	copy(b[4:], packet)
	return w.ws.WriteMessage(websocket.BinaryMessage, b)
}
func (w *macWire) normalizeReceipt(_ *realtimestate.Turn, event map[string]any) (map[string]any, error) {
	return event, nil
}
