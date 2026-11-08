package realtimelive

import (
	"familyrobot/internal/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func readFaceEvent(t *testing.T, client *websocket.Conn) map[string]any {
	t.Helper()
	client.SetReadDeadline(time.Now().Add(time.Second))
	var event map[string]any
	if err := client.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	return event
}

func TestFaceStateIsNegotiatedMetadataAndPreservesPlayback(t *testing.T) {
	server, client := testSocket(t)
	w := newXiaozhiWire(server, 1, 60, "face-session")
	if err := w.sendEvent(map[string]any{"type": "status", "code": "thinking"}); err != nil {
		t.Fatal(err)
	}
	if err := w.sendEvent(map[string]any{"type": "ready"}); err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := client.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "hello" {
		t.Fatal("legacy client received face side effect", message)
	}
	if object(message["features"])["face_state_v1"] == true {
		t.Fatal("unnegotiated capability")
	}
	w.face = true
	if err := w.sendEvent(map[string]any{"type": "ready"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if object(message["features"])["face_state_v1"] != true {
		t.Fatal("missing face acknowledgement")
	}
	readState := func(state string) {
		t.Helper()
		message = nil
		if err := client.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message["type"] != "llm" || message["state"] != state || message["session_id"] != "face-session" {
			t.Fatal(message)
		}
		if _, exists := message["emotion"]; exists {
			t.Fatal("generation state overwrote mood", message)
		}
	}
	if err := w.sendEvent(map[string]any{"type": "status", "code": "thinking"}); err != nil {
		t.Fatal(err)
	}
	readState("thinking")
	// Repeated state is coalesced; it must not insert extra TTS events or replies.
	if err := w.sendEvent(map[string]any{"type": "status", "code": "thinking"}); err != nil {
		t.Fatal(err)
	}
	if err := w.sendEvent(map[string]any{"type": "begin", "id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	message = nil
	if err := client.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "tts" || message["state"] != "start" || message["playback_id"] != float64(1) {
		t.Fatal(message)
	}
	if err := w.sendEvent(map[string]any{"type": "end", "id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	message = nil
	if err := client.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "tts" || message["state"] != "stop" {
		t.Fatal(message)
	}
	readState("listening")
	if err := w.sendEvent(map[string]any{"type": "status", "code": "thinking"}); err != nil {
		t.Fatal(err)
	}
	readState("thinking")
	if err := w.sendEvent(map[string]any{"type": "status", "code": "turn_failed"}); err != nil {
		t.Fatal(err)
	}
	readState("listening")
}

func TestRetiredTurnCannotPublishThinkingAndUnbegunAbortClearsIt(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	defer s.cancel()
	w := newXiaozhiWire(server, 1, 60, "face-session")
	w.face = true
	s.wire = w
	old := s.addTurn()
	current := s.addTurn()
	if err := s.sendThinking(old); err != nil {
		t.Fatal(err)
	}
	if err := s.sendThinking(current); err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	client.SetReadDeadline(time.Now().Add(time.Second))
	if err := client.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["state"] != "thinking" {
		t.Fatal(message)
	}
	if err := s.interruptExpected(old); err != nil {
		t.Fatal(err)
	}
	if err := s.send(map[string]any{"type": "ready"}); err != nil {
		t.Fatal(err)
	}
	if message = readFaceEvent(t, client); message["type"] != "hello" || s.output != current || current.turn.Cut {
		t.Fatal("retired turn cleared current thinking", message)
	}
	if err := s.interrupt(); err != nil {
		t.Fatal(err)
	}
	message = nil
	if err := client.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "llm" || message["state"] != "listening" {
		t.Fatal(message)
	}
}

func TestFaceStateNegotiationUsesAuthenticatedHello(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "negotiated"}[negotiated], func(t *testing.T) {
			s := NewServer(Config{}, nil, t.TempDir(), "face-test")
			s.runDevice = func(session *Session) {
				defer session.cancel()
				if err := session.send(map[string]any{"type": "ready"}); err != nil {
					t.Error(err)
				}
			}
			h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
			defer h.Close()
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			hello := protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true, "face_state_v1": negotiated}}
			if err = client.WriteJSON(hello); err != nil {
				t.Fatal(err)
			}
			reply := readFaceEvent(t, client)
			if (object(reply["features"])["face_state_v1"] == true) != negotiated || reply["session_id"] == "" {
				t.Fatal("face negotiation not scoped to admitted hello", reply)
			}
		})
	}
}

func TestFaceErrorsClearOnlyNegotiatedMetadata(t *testing.T) {
	for _, code := range []string{"turn_failed", "drain_failed", "input_too_long"} {
		for _, negotiated := range []bool{false, true} {
			t.Run(code+map[bool]string{false: "/legacy", true: "/negotiated"}[negotiated], func(t *testing.T) {
				server, client := testSocket(t)
				w := newXiaozhiWire(server, 1, 60, "face-session")
				w.face = negotiated
				for _, state := range []string{"thinking", code, code, "unknown_status"} {
					if err := w.sendEvent(map[string]any{"type": "status", "code": state}); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.sendEvent(map[string]any{"type": "ready"}); err != nil {
					t.Fatal(err)
				}
				if negotiated {
					for _, state := range []string{"thinking", "listening"} {
						if event := readFaceEvent(t, client); event["type"] != "llm" || event["state"] != state || event["session_id"] != "face-session" || len(event) != 3 {
							t.Fatal("status changed mood or playback", event)
						}
					}
				}
				if event := readFaceEvent(t, client); event["type"] != "hello" || len(w.blocks) != 0 {
					t.Fatal("status created extra metadata or playback", event)
				}
			})
		}
	}
}

func TestFaceSealValidatesPlaybackBeforeClearingThinking(t *testing.T) {
	for _, kind := range []string{"end", "drain"} {
		t.Run(kind, func(t *testing.T) {
			server, client := testSocket(t)
			w := newXiaozhiWire(server, 1, 60, "face-session")
			w.face = true
			if err := w.sendEvent(map[string]any{"type": "status", "code": "thinking"}); err != nil {
				t.Fatal(err)
			}
			if event := readFaceEvent(t, client); event["state"] != "thinking" {
				t.Fatal(event)
			}
			if err := w.sendEvent(map[string]any{"type": kind, "id": int64(99), "sent": int64(0)}); err == nil {
				t.Fatal("unknown seal accepted")
			}
			if err := w.sendEvent(map[string]any{"type": "begin", "id": int64(1)}); err != nil {
				t.Fatal(err)
			}
			if event := readFaceEvent(t, client); event["type"] != "tts" || event["state"] != "start" {
				t.Fatal("invalid seal cleared thinking", event)
			}
			if err := w.sendAudio(1, []byte{1, 2, 3}); err != nil {
				t.Fatal(err)
			}
			if messageType, _, err := client.ReadMessage(); err != nil || messageType != websocket.BinaryMessage {
				t.Fatal("playback packet changed", messageType, err)
			}
			if kind == "drain" {
				if err := w.sendEvent(map[string]any{"type": kind, "id": int64(1), "sent": int64(0)}); err == nil || w.blocks[1].sealed {
					t.Fatal("mismatched drain changed playback")
				}
			}
			if err := w.sendEvent(map[string]any{"type": kind, "id": int64(1), "sent": int64(1440)}); err != nil {
				t.Fatal(err)
			}
			if event := readFaceEvent(t, client); event["type"] != "tts" || event["state"] != "stop" || event["sent_samples"] != float64(1440) || (event["reason"] == "drain") != (kind == "drain") {
				t.Fatal("seal ordering or ledger changed", event)
			}
			if event := readFaceEvent(t, client); event["type"] != "llm" || event["state"] != "listening" {
				t.Fatal(event)
			}
			if !w.blocks[1].sealed || w.blocks[1].sent != 1440 || w.blocks[1].draining != (kind == "drain") {
				t.Fatal("seal ledger changed", w.blocks[1])
			}
		})
	}
}

func TestFaceThinkingRejectsCancelledAndCutTurns(t *testing.T) {
	for _, kind := range []string{"turn_cancelled", "session_cancelled", "cut"} {
		t.Run(kind, func(t *testing.T) {
			server, client := testSocket(t)
			s := NewSession(server, Config{}, t.TempDir(), nil)
			defer s.cancel()
			w := newXiaozhiWire(server, 1, 60, "face-session")
			w.face = true
			s.wire = w
			rt := s.addTurn()
			switch kind {
			case "turn_cancelled":
				rt.cancel()
			case "session_cancelled":
				s.cancel()
			case "cut":
				rt.turn.Cut = true
			}
			if err := s.sendThinking(rt); err != nil {
				t.Fatal(err)
			}
			if err := s.send(map[string]any{"type": "ready"}); err != nil {
				t.Fatal(err)
			}
			if event := readFaceEvent(t, client); event["type"] != "hello" {
				t.Fatal("ineligible turn published thinking", event)
			}
		})
	}
}

func TestFaceThinkingAndUnbegunInterruptAreSerialized(t *testing.T) {
	for range 20 {
		server, client := testSocket(t)
		s := NewSession(server, Config{}, t.TempDir(), nil)
		w := newXiaozhiWire(server, 1, 60, "face-session")
		w.face = true
		s.wire = w
		rt := s.addTurn()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := s.sendThinking(rt); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := s.interrupt(); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		if err := s.send(map[string]any{"type": "ready"}); err != nil {
			t.Fatal(err)
		}
		last := ""
		for {
			event := readFaceEvent(t, client)
			if event["type"] == "hello" {
				break
			}
			if event["type"] != "llm" || (last == "listening" && event["state"] == "thinking") {
				t.Fatal("interrupted turn restored thinking", event)
			}
			last, _ = event["state"].(string)
		}
		s.cancel()
		if last != "listening" || len(w.blocks) != 0 {
			t.Fatal("unbegun interrupt did not clear thinking", last)
		}
	}
}

func TestMacUnbegunInterruptKeepsExistingStatusProtocol(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	defer s.cancel()
	rt := s.addTurn()
	if err := s.sendThinking(rt); err != nil {
		t.Fatal(err)
	}
	if event := readFaceEvent(t, client); event["type"] != "status" || event["code"] != "thinking" {
		t.Fatal(event)
	}
	if err := s.interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := s.send(map[string]any{"type": "ready"}); err != nil {
		t.Fatal(err)
	}
	if event := readFaceEvent(t, client); event["type"] != "ready" {
		t.Fatal("Mac interrupt acquired a new status", event)
	}
}
