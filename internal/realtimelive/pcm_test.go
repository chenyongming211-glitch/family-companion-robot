package realtimelive

// Synthetic PCM exercises the authenticated device entry and common Session.
// These tests use no model or physical device; samples remain in memory.
import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"familyrobot/internal/protocol"
	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func pcmHello() protocol.Hello {
	return protocol.Hello{Type: "hello", Version: 1, Transport: "websocket",
		AudioParams: protocol.AudioParams{Format: "pcm_s16le", SampleRate: 16000, Channels: 1, FrameDuration: 60},
		Features:    map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true, "pcm_s16le_v1": true}}
}

func newPCMSession(t *testing.T) (*Session, *websocket.Conn) {
	t.Helper()
	entered := make(chan *Session, 1)
	done := make(chan struct{})
	server := NewServer(Config{ASRURL: "ws://127.0.0.1:1", Synthetic: true}, nil, t.TempDir(), "pcm-test")
	server.runDevice = func(s *Session) { entered <- s; <-done }
	h := httptest.NewServer(server.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/",
		http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}})
	if err != nil {
		h.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		activeDone := singleActiveDone(server)
		close(done)
		c.Close()
		h.Close()
		if activeDone != nil {
			select {
			case <-activeDone:
			case <-time.After(time.Second):
				t.Error("PCM handler did not finish")
			}
		}
	})
	if err = c.WriteJSON(pcmHello()); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-entered:
		t.Cleanup(func() {
			// This helper bypasses Run, so it must join the same background
			// work before TempDir cleanup. OutputASR.Finish is asynchronous.
			s.cancel()
			s.wg.Wait()
			s.mu.Lock()
			history := append([]*liveTurn(nil), s.history...)
			s.mu.Unlock()
			for _, rt := range history {
				if rt.asr != nil {
					rt.asr.Close()
				}
			}
		})
		return s, c
	case <-time.After(time.Second):
		t.Fatal("authenticated PCM hello did not reach the common Session")
	}
	return nil, nil
}

func pcmSamples(n int) []byte {
	b := make([]byte, n*2)
	for i := range n {
		b[i*2] = byte(i*37 + 11)
		b[i*2+1] = byte(i*13 + 5)
	}
	return b
}

func TestPCMNegotiation(t *testing.T) {
	for _, v := range []struct {
		name  string
		edit  func(*protocol.Hello)
		valid bool
	}{
		{"valid", func(*protocol.Hello) {}, true},
		{"missing-capability", func(h *protocol.Hello) { delete(h.Features, "pcm_s16le_v1") }, false},
		{"disabled-capability", func(h *protocol.Hello) { h.Features["pcm_s16le_v1"] = false }, false},
		{"missing-progress", func(h *protocol.Hello) { delete(h.Features, "playback_progress_v1") }, false},
		{"missing-drain", func(h *protocol.Hello) { delete(h.Features, "playback_drain_v1") }, false},
		{"wrong-rate", func(h *protocol.Hello) { h.AudioParams.SampleRate = 24000 }, false},
		{"stereo", func(h *protocol.Hello) { h.AudioParams.Channels = 2 }, false},
		{"unsupported-frame", func(h *protocol.Hello) { h.AudioParams.FrameDuration = 20 }, false},
		{"forty-ms-frame", func(h *protocol.Hello) { h.AudioParams.FrameDuration = 40 }, false},
		{"ambiguous-format", func(h *protocol.Hello) { h.AudioParams.Format = "pcm" }, false},
		{"unsupported-envelope", func(h *protocol.Hello) { h.Version = 2 }, false},
		{"v3-envelope", func(h *protocol.Hello) { h.Version = 3 }, false},
	} {
		t.Run(v.name, func(t *testing.T) {
			h := pcmHello()
			v.edit(&h)
			b, _ := json.Marshal(h)
			// The legacy/shared parser must remain Opus-only, even with a PCM capability.
			if _, err := protocol.ParseHello(b, h.Version); err == nil {
				t.Fatal("PCM widened a legacy entry")
			}
			s := NewServer(Config{}, nil, t.TempDir(), "pcm-test")
			s.runDevice = func(session *Session) { session.send(map[string]any{"type": "ready"}) }
			server := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
			defer server.Close()
			c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}, "Protocol-Version": []string{fmt.Sprint(h.Version)}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetReadDeadline(time.Now().Add(time.Second))
			c.WriteMessage(websocket.TextMessage, b)
			var reply protocol.Hello
			err = c.ReadJSON(&reply)
			if (err == nil) != v.valid {
				t.Fatalf("accepted=%v want=%v: %v", err == nil, v.valid, err)
			}
		})
	}
	for version := 1; version <= 3; version++ {
		for _, ms := range []int{20, 40, 60} {
			h := pcmHello()
			h.Version = version
			h.AudioParams.Format = "opus"
			h.AudioParams.FrameDuration = ms
			delete(h.Features, "pcm_s16le_v1")
			b, _ := json.Marshal(h)
			if _, err := protocol.ParseHello(b, version); err != nil {
				t.Fatalf("legacy v%d/%d: %v", version, ms, err)
			}
		}
	}
}

func TestPCMRealtimeHelloCompatibility(t *testing.T) {
	for version := 1; version <= 3; version++ {
		for _, ms := range []int{20, 40, 60} {
			h := pcmHello()
			h.Version = version
			h.AudioParams.Format = "opus"
			h.AudioParams.FrameDuration = ms
			// A capability does not select PCM when the format is still Opus.
			b, _ := json.Marshal(h)
			got, err := protocol.ParseRealtimeHello(b, version)
			if err != nil || got.AudioParams.Format != "opus" {
				t.Fatalf("Realtime Opus v%d/%d: %v", version, ms, err)
			}
		}
	}
	for _, v := range []struct {
		name    string
		data    []byte
		version int
	}{
		{"malformed", []byte(`{"type":`), 1},
		{"oversize", bytes.Repeat([]byte(" "), 16385), 1},
		{"header-version-mismatch", mustPCMHelloJSON(t), 2},
	} {
		t.Run(v.name, func(t *testing.T) {
			if _, err := protocol.ParseRealtimeHello(v.data, v.version); err == nil {
				t.Fatal("invalid hello accepted")
			}
		})
	}
}

func mustPCMHelloJSON(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(pcmHello())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPCMInputWakeIsolationAndFrameBounds(t *testing.T) {
	s, c := newPCMSession(t)
	w := s.wire.(*xiaozhiWire)
	if err := s.send(map[string]any{"type": "ready"}); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	var hello protocol.Hello
	if err := c.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.AudioParams.Format != "pcm_s16le" || hello.AudioParams.SampleRate != 24000 || !hello.Features["pcm_s16le_v1"] {
		t.Fatal("PCM was not negotiated in both directions", hello)
	}
	samples := pcmSamples(960)
	if pcm, err := w.decodeAudio(samples, nil); err != nil || pcm != nil {
		t.Fatal("wake cache reached business input", err)
	}
	start, _ := json.Marshal(map[string]any{"type": "listen", "state": "start", "mode": "realtime", "session_id": w.scope})
	if _, err := w.control(start); err != nil {
		t.Fatal(err)
	}
	pcm, err := w.decodeAudio(samples, nil)
	if err != nil || !bytes.Equal(pcm, samples) {
		t.Fatal("input was transformed/lost", err)
	}
	for _, n := range []int{0, 1, 1918, 1921, 1922, 4096} {
		if _, err := w.decodeAudio(make([]byte, n), nil); err == nil {
			t.Fatalf("accepted wrong frame length %d", n)
		}
	}
	// Playback must not close the microphone; this remains one listen epoch.
	if err := s.send(map[string]any{"type": "begin", "id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	if pcm, err := w.decodeAudio(samples, nil); err != nil || !bytes.Equal(pcm, samples) {
		t.Fatal("playback gated PCM uplink", err)
	}
}

func TestPCMWakeBudget(t *testing.T) {
	s, _ := newPCMSession(t)
	w := s.wire.(*xiaozhiWire)
	frame := make([]byte, 1920)
	for range 33 {
		if _, err := w.decodeAudio(frame, nil); err != nil {
			t.Fatal("early wake rejection", err)
		}
	}
	if _, err := w.decodeAudio(frame, nil); err == nil {
		t.Fatal("unbounded wake cache")
	}
}

func TestPCMRunRoutesInputWithoutCodec(t *testing.T) {
	appended := make(chan []byte, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		for i := 0; i < 2; i++ {
			var e map[string]any
			if c.ReadJSON(&e) != nil {
				return
			}
			v := map[string]any{}
			if i == 0 {
				v["enable_search"] = false
			}
			c.WriteJSON(map[string]any{"type": "session.updated", "session": v})
		}
		for {
			var e map[string]any
			if c.ReadJSON(&e) != nil {
				return
			}
			if e["type"] == "input_audio_buffer.append" {
				b, _ := base64.StdEncoding.DecodeString(e["audio"].(string))
				appended <- b
			}
		}
	}))
	defer provider.Close()
	s := NewServer(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http"), ASRURL: "ws://127.0.0.1:1", Synthetic: true}, nil, t.TempDir(), "pcm-test")
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	c.WriteJSON(pcmHello())
	var hello protocol.Hello
	if err := c.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	frame := pcmSamples(960)
	c.WriteMessage(websocket.BinaryMessage, frame)
	c.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "realtime", "session_id": hello.SessionID})
	c.WriteMessage(websocket.BinaryMessage, frame)
	select {
	case pcm := <-appended:
		if !bytes.Equal(pcm, frame) {
			t.Fatal("business PCM changed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PCM input missing at provider")
	}
	c.WriteJSON(map[string]any{"type": "listen", "state": "stop", "session_id": hello.SessionID})
	if done := singleActiveDone(s); done != nil {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("stop did not release admission")
		}
	}
	select {
	case <-appended:
		t.Fatal("wake audio reached provider")
	default:
	}
}

func TestPCMOutputBounds(t *testing.T) {
	s, _ := newPCMSession(t)
	w := s.wire.(*xiaozhiWire)
	if err := s.send(map[string]any{"type": "begin", "id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 1, 2878, 2881, 2882, 4096} {
		if err := w.sendAudio(1, make([]byte, n)); err == nil {
			t.Fatalf("accepted output length %d", n)
		}
	}
}

func TestPCMSharedCreditDrainAndASR(t *testing.T) {
	s, c := newPCMSession(t)
	w := s.wire.(*xiaozhiWire)
	rt := s.addTurn()
	rt.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": rt.turn.ID}); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	var event map[string]any
	if err := c.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	a := &OutputASR{queue: make(chan []byte, 16), Done: make(chan struct{}), cancel: func() {}}
	rt.asr = a
	resampler := realtimeaudio.NewResampler()
	expectedASR := realtimeaudio.NewResampler()
	frame := pcmSamples(1440)
	for range 5 {
		if ok, err := s.release(rt, frame, nil, resampler); err != nil || !ok {
			t.Fatal(err)
		}
		kind, got, err := c.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(got, frame) {
			t.Fatal("PCM output changed", err)
		}
		want, err := expectedASR.Process(frame)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-a.queue:
			if !bytes.Equal(got, want) {
				t.Fatal("output ASR did not receive the raw PCM resampling")
			}
		default:
			t.Fatal("output ASR input missing")
		}
	}
	if rt.turn.CanSend() || rt.turn.Sent != 7200 || a.InputSamples() != 4800 {
		t.Fatalf("window/ASR changed: sent=%d asr=%d", rt.turn.Sent, a.InputSamples())
	}
	if err := s.interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := c.ReadJSON(&event); err != nil || event["reason"] != "drain" || event["sent_samples"] != float64(7200) {
		t.Fatal("wrong seal", event, err)
	}
	if ok, err := s.binary(rt, frame); err != nil || ok {
		t.Fatal("old packet escaped the drain seal", err)
	}
	receipt := func(scope string, n int64) map[string]any {
		b, _ := json.Marshal(map[string]any{"type": "playback", "state": "finished", "session_id": scope, "playback_id": rt.turn.ID, "sample_rate": 24000, "played_samples": n, "valid": true, "basis": "i2s_dma", "interrupted": false})
		e, err := w.control(b)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if err := s.receiveReceipt(receipt(w.scope, 8640)); err == nil {
		t.Fatal("receipt beyond sent accepted")
	}
	if err := s.receiveReceipt(receipt(w.scope, 7200)); err != nil {
		t.Fatal(err)
	}
	if !rt.turn.PlaybackFinal || rt.turn.PlaybackBasis != "i2s_dma" || rt.turn.Completed != 7200 {
		t.Fatal("DMA drain evidence lost", rt.turn)
	}
	select {
	case <-rt.turn.DrainDone:
	default:
		t.Fatal("drain completion did not resolve")
	}
	next := s.addTurn()
	next.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": next.turn.ID}); err != nil {
		t.Fatal(err)
	}
	if next.turn.ID != 2 || next.turn.Cut {
		t.Fatal("new turn did not remain independent")
	}
	if err := s.receiveReceipt(receipt(w.scope, 7200)); err != nil {
		t.Fatal("late duplicate changed old receipt", err)
	}
	if next.turn.Completed != 0 || next.turn.Cut {
		t.Fatal("old receipt affected next turn")
	}
}

func TestPCMOutputFramingWithoutOpusTail(t *testing.T) {
	for _, samples := range []int{1, 1439, 1440, 1441, 2880} {
		t.Run(fmt.Sprint(samples), func(t *testing.T) {
			s, c := newPCMSession(t)
			product, err := realtimestate.LoadProduct("../../config/product-identity.json")
			if err != nil {
				t.Fatal(err)
			}
			s.product = product
			original := pcmSamples(samples)
			s.generate = func(ctx context.Context, id int64, prompt string, input []byte, emit func(string, []byte) error) error {
				// Fragment across model events, including through a 60ms packet boundary.
				split := len(original) / 4 * 2
				if err := emit("pcm", original[:split]); err != nil {
					return err
				}
				if err := emit("pcm", original[split:]); err != nil {
					return err
				}
				return emit("text", []byte("合成测试"))
			}
			rt := s.addTurn()
			done := make(chan struct{})
			go func() { s.reply(rt, nil); close(done) }()
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			var got []byte
			stopped := false
			for !stopped {
				kind, b, err := c.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
				if kind == websocket.BinaryMessage {
					if len(b) != 2880 {
						t.Fatalf("frame bytes=%d", len(b))
					}
					got = append(got, b...)
					continue
				}
				var e map[string]any
				if json.Unmarshal(b, &e) != nil {
					t.Fatal("invalid control")
				}
				stopped = e["type"] == "tts" && e["state"] == "stop"
			}
			want := (samples + 1439) / 1440 * 2880
			if len(got) != want || !bytes.Equal(got[:len(original)], original) || !bytes.Equal(got[len(original):], make([]byte, want-len(original))) {
				t.Fatalf("PCM payload/padding: got=%d want=%d", len(got), want)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("reply did not finish")
			}
			if rt.turn.Sent != int64(want/2) || !rt.turn.Ended {
				t.Fatal("sample clock not sealed", rt.turn)
			}
		})
	}
}

func TestPCMEmptyOutputDrainsTurnAndKeepsSession(t *testing.T) {
	s, c := newPCMSession(t)
	product, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	s.product = product
	s.generate = func(ctx context.Context, id int64, prompt string, input []byte, emit func(string, []byte) error) error {
		return emit("text", []byte("合成空音频"))
	}
	empty := s.addTurn()
	s.reply(empty, nil)
	if s.ctx.Err() != nil {
		t.Fatal("empty PCM output canceled the Session", s.terminalFailure())
	}
	if !empty.turn.Cut || !closed(empty.turn.DrainDone) || empty.turn.Begun || empty.turn.Sent != 0 {
		t.Fatal("empty output did not resolve as an unbegun drained turn", empty.turn)
	}
	frame := pcmSamples(1440)
	s.generate = func(ctx context.Context, id int64, prompt string, input []byte, emit func(string, []byte) error) error {
		if err := emit("pcm", frame); err != nil {
			return err
		}
		return emit("text", []byte("下一轮正常"))
	}
	next := s.addTurn()
	done := make(chan struct{})
	go func() { s.reply(next, nil); close(done) }()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event map[string]any
	if err := c.ReadJSON(&event); err != nil || event["type"] != "tts" || event["state"] != "start" || event["playback_id"] != float64(next.turn.ID) {
		t.Fatal("empty turn emitted media/control or next turn did not begin", event, err)
	}
	kind, got, err := c.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(got, frame) {
		t.Fatal("next positive-audio turn failed", err)
	}
	if err := c.ReadJSON(&event); err != nil || event["state"] != "stop" || event["sent_samples"] != float64(1440) {
		t.Fatal("next turn did not seal one real PCM packet", event, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("next reply did not finish")
	}
	if s.ctx.Err() != nil || !next.turn.Ended || next.turn.Cut {
		t.Fatal("Session or next turn was affected by empty output", next.turn)
	}
}

func TestPCMEmptyOutputKeepsLegacyOpusTail(t *testing.T) {
	server, client := testSocket(t)
	product, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(server, Config{ASRURL: "ws://127.0.0.1:1", Synthetic: true}, t.TempDir(), product)
	t.Cleanup(s.cancel)
	s.wire = newXiaozhiWire(server, 1, 60, "opus-session")
	s.generate = func(ctx context.Context, id int64, prompt string, input []byte, emit func(string, []byte) error) error {
		return emit("text", []byte("合成空音频"))
	}
	rt := s.addTurn()
	done := make(chan struct{})
	go func() { s.reply(rt, nil); close(done) }()
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event map[string]any
	if err := client.ReadJSON(&event); err != nil || event["state"] != "start" {
		t.Fatal("legacy Opus output did not begin", event, err)
	}
	kind, packet, err := client.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		t.Fatal("legacy Opus tail missing", err)
	}
	codec, err := realtimeaudio.New(24000, 1440)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	if _, err := codec.Decode(packet); err != nil {
		t.Fatal("legacy tail was not one valid Opus frame", err)
	}
	if err := client.ReadJSON(&event); err != nil || event["state"] != "stop" || event["sent_samples"] != float64(1440) {
		t.Fatal("legacy Opus tail clock changed", event, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("legacy Opus reply did not finish")
	}
	if s.ctx.Err() != nil || rt.turn.Cut || !rt.turn.Ended {
		t.Fatal("legacy Opus empty output changed", rt.turn)
	}
}
