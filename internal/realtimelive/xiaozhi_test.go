package realtimelive

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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testDeviceMAC = "aa:bb:cc:dd:ee:01"

var testDeviceToken = strings.Repeat("q", 64)

func TestDeviceConfigOnlyReadsAllowlistedKeysAndRequiresSpecificMAC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.env")
	os.WriteFile(path, []byte("DASHSCOPE_API_KEY=never-read\nROBOT_DEVICE_TOKEN='"+testDeviceToken+"'\nROBOT_ALLOWED_DEVICE=AA:BB:CC:DD:EE:01\n"), 0600)
	c, err := LoadDeviceConfig(path)
	if err != nil || c.AllowedDevice != testDeviceMAC || c.Token != testDeviceToken {
		t.Fatal("valid protected config rejected", err)
	}
	os.WriteFile(path, []byte("ROBOT_DEVICE_TOKEN="+testDeviceToken+"\n"), 0600)
	if _, err := LoadDeviceConfig(path); err == nil {
		t.Fatal("unbounded device admission")
	}
}

func TestDeviceHandlerAuthenticationOwnershipAndSharedAdmission(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "device-test")
	entered := make(chan *Session, 4)
	s.runDevice = func(session *Session) { entered <- session; session.ws.ReadMessage() }
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	mac := httptest.NewServer(s.Handler())
	defer mac.Close()
	for _, p := range []string{"/devices", "/pair", "/live", "/healthz"} {
		r, err := http.Get(h.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatalf("public %s: %d", p, r.StatusCode)
		}
	}
	ownedRequest(t, mac.URL, "/devices", `{"client_type":"xiaozhi"}`, "", 400)
	dial := func(token, id, version string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + token}, "Device-Id": []string{id}, "Protocol-Version": []string{version}, "Client-Id": []string{"not-the-owner"}})
	}
	for _, v := range []struct {
		token, id, version string
		status             int
	}{{"bad", testDeviceMAC, "1", 401}, {testDeviceToken, "aa:bb:cc:dd:ee:02", "1", 403}, {testDeviceToken, testDeviceMAC, "4", 400}} {
		c, r, e := dial(v.token, v.id, v.version)
		if c != nil {
			c.Close()
		}
		if e == nil || r == nil || r.StatusCode != v.status {
			t.Fatalf("authentication status: %v %v", r, e)
		}
	}
	connect := func() (*websocket.Conn, *Session) {
		t.Helper()
		c, _, err := dial(testDeviceToken, testDeviceMAC, "1")
		if err != nil {
			t.Fatal(err)
		}
		c.WriteJSON(protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true}})
		select {
		case sess := <-entered:
			return c, sess
		case <-time.After(time.Second):
			t.Fatal("device not admitted")
			return nil, nil
		}
	}
	c, first := connect()
	if first.rec.owner.ClientType != "xiaozhi" || first.rec.owner.DeviceID == "" || first.rec.owner.SessionID == "" {
		t.Fatal("unattributed device")
	}
	ownedRequest(t, mac.URL, "/pair", "", "", 409)
	c2, r, e := dial(testDeviceToken, testDeviceMAC, "1")
	if c2 != nil {
		c2.Close()
	}
	if e == nil || r.StatusCode != 409 {
		t.Fatal("concurrent device admitted")
	}
	closeAndWait := func(c *websocket.Conn) {
		c.Close()
		s.mu.Lock()
		done := s.activeDone
		s.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("admission stuck")
			}
		}
	}
	closeAndWait(c)
	c, second := connect()
	closeAndWait(c)
	if first.rec.owner.DeviceID != second.rec.owner.DeviceID || first.rec.owner.SessionID == second.rec.owner.SessionID {
		t.Fatal("reconnect ownership lost")
	}
	if _, err := ReadSessionInfo(second.directory); err != nil {
		t.Fatal(err)
	}
	// A Mac session occupies the same gate in the other direction.
	s.run = func(ws *websocket.Conn, dir string) { entered <- NewSession(ws, Config{}, dir, nil); ws.ReadMessage() }
	pair := ownedRequest(t, mac.URL, "/pair", "", "", 200)
	m, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(mac.URL, "http")+"/live", http.Header{"Authorization": []string{"Bearer " + pair["ticket"].(string)}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	c2, r, e = dial(testDeviceToken, testDeviceMAC, "1")
	if c2 != nil {
		c2.Close()
	}
	if e == nil || r.StatusCode != 409 {
		t.Fatal("Mac gate bypassed")
	}
	closeAndWait(m)
}

func TestTrustedXiaozhiCredentialCannotSelectMacPlaybackProtocol(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "test")
	secret := strings.Repeat("x", 43)
	d, err := s.devices.BindXiaozhi(testDeviceMAC, secret)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	body, _ := json.Marshal(map[string]any{"device_id": d.ID})
	ownedRequest(t, h.URL, "/pair", string(body), secret, 403)
}

func TestXiaozhiNineWireFormatsAndWakeIsolation(t *testing.T) {
	for version := 1; version <= 3; version++ {
		for _, ms := range []int{20, 40, 60} {
			t.Run(fmt.Sprintf("v%d/%dms", version, ms), func(t *testing.T) {
				server, client := testSocket(t)
				w := newXiaozhiWire(server, version, ms, "session")
				codec, err := realtimeaudio.New(16000, ms*16)
				if err != nil {
					t.Fatal(err)
				}
				defer codec.Close()
				packet, err := codec.Encode(make([]byte, ms*32))
				if err != nil {
					t.Fatal(err)
				}
				wrapped, _ := protocol.Wrap(version, packet, 0)
				if pcm, err := w.decodeAudio(wrapped, codec); err != nil || pcm != nil {
					t.Fatal("wake cache reached model", err)
				}
				if _, err := w.control([]byte(`{"type":"listen","state":"detect","session_id":"session"}`)); err != nil {
					t.Fatal(err)
				}
				if _, err := w.control([]byte(`{"type":"listen","state":"start","mode":"realtime","session_id":"session"}`)); err != nil {
					t.Fatal(err)
				}
				pcm, err := w.decodeAudio(wrapped, codec)
				if err != nil || len(pcm) != ms*32 {
					t.Fatal("negotiated input rejected", len(pcm), err)
				}
				if err := w.sendEvent(map[string]any{"type": "begin", "id": int64(1), "session_id": "session"}); err != nil {
					t.Fatal(err)
				}
				var start map[string]any
				client.ReadJSON(&start)
				if start["type"] != "tts" || start["playback_id"] != float64(1) {
					t.Fatal(start)
				}
				out := []byte{3, 4, 5}
				if err := w.sendAudio(1, out); err != nil {
					t.Fatal(err)
				}
				kind, raw, err := client.ReadMessage()
				unwrapped, _, ue := protocol.Unwrap(version, raw)
				if err != nil || ue != nil || kind != websocket.BinaryMessage || !bytes.Equal(unwrapped, out) {
					t.Fatal("nonstandard output envelope", err, ue)
				}
				if err := w.sendEvent(map[string]any{"type": "drain", "id": int64(1), "sent": int64(1440), "session_id": "session"}); err != nil {
					t.Fatal(err)
				}
				var seal map[string]any
				client.ReadJSON(&seal)
				if seal["reason"] != "drain" || seal["sent_samples"] != float64(1440) || seal["interrupted"] != nil {
					t.Fatal("destructive drain", seal)
				}
			})
		}
	}
}

func TestXiaozhiI2SReceiptsKeepBasisAndCompleteOnlyMatchingSeal(t *testing.T) {
	server, client := testSocket(t)
	w := newXiaozhiWire(server, 1, 60, "scope")
	s := NewSession(server, Config{}, t.TempDir(), nil)
	defer s.cancel()
	s.wire = w
	rt := s.addTurn()
	rt.turn.Begun = true
	w.sendEvent(map[string]any{"type": "begin", "id": int64(1), "session_id": "scope"})
	client.ReadMessage()
	for range 2 {
		s.binary(rt, []byte{1})
		client.ReadMessage()
	}
	receipt := func(state string, n int, valid, interrupted bool) map[string]any {
		b, _ := json.Marshal(map[string]any{"type": "playback", "state": state, "session_id": "scope", "playback_id": 1, "sample_rate": 24000, "played_samples": n, "valid": valid, "basis": "i2s_dma", "interrupted": interrupted})
		e, err := w.control(b)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if err := s.receiveReceipt(receipt("progress", 1680, true, false)); err != nil {
		t.Fatal(err)
	}
	if rt.turn.Completed != 1440 || rt.turn.Accepted != 1440 {
		t.Fatal("I2S progress not conservatively rounded")
	}
	if err := s.receiveReceipt(receipt("finished", 2880, true, false)); err == nil {
		t.Fatal("unsealed final accepted")
	}
	if err := s.interrupt(); err != nil {
		t.Fatal(err)
	}
	client.ReadMessage()
	if err := s.receiveReceipt(receipt("finished", 2880, true, false)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.turn.DrainDone:
	default:
		t.Fatal("real I2S drain not released")
	}
	if realtimestate.PlaybackEvidence(rt.turn).Basis != "i2s_dma" {
		t.Fatal("I2S evidence provenance lost")
	}
	raw, _ := os.ReadFile(filepath.Join(s.directory, "events.jsonl"))
	if !bytes.Contains(raw, []byte(`"raw_played_samples":1680`)) {
		t.Fatal("raw progress discarded")
	}
	for _, event := range []map[string]any{receipt("progress", 1440, true, false), receipt("finished", 2880, false, false), receipt("finished", 2880, true, true), {"id": int64(1), "basis": "CoreAudio_dataPlayedBack", "completed": int64(2880), "accepted": int64(2880)}} {
		if err := s.receiveReceipt(event); err == nil {
			t.Fatal("invalid or forged evidence accepted")
		}
	}
	s.cancel()
	s.wg.Wait()
}

func TestDeviceHelloRequiresBothPlaybackCapabilities(t *testing.T) {
	for _, features := range []map[string]bool{nil, {"playback_progress_v1": true}, {"playback_drain_v1": true}} {
		s := NewServer(Config{}, nil, t.TempDir(), "test")
		s.runDevice = func(*Session) { t.Error("unsupported firmware reached voice core") }
		h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}})
		if err != nil {
			t.Fatal(err)
		}
		c.WriteJSON(protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: features})
		c.SetReadDeadline(time.Now().Add(time.Second))
		_, _, err = c.ReadMessage()
		if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
			t.Fatal("missing capability not clearly rejected", err)
		}
		c.Close()
		h.Close()
	}
}

func TestDeviceSessionWakeIsolationStopAndLifecycleWithoutModelCalls(t *testing.T) {
	appended := make(chan []byte, 8)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		for i := 0; i < 2; i++ {
			var v map[string]any
			if c.ReadJSON(&v) != nil {
				return
			}
			session := map[string]any{}
			if i == 0 {
				session["enable_search"] = false
			}
			c.WriteJSON(map[string]any{"type": "session.updated", "session": session})
		}
		for {
			var v map[string]any
			if c.ReadJSON(&v) != nil {
				return
			}
			if v["type"] == "input_audio_buffer.append" {
				pcm, _ := base64.StdEncoding.DecodeString(v["audio"].(string))
				appended <- pcm
			}
		}
	}))
	defer provider.Close()
	root := t.TempDir()
	s := NewServer(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, nil, root, "test")
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.WriteJSON(protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true}})
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hello protocol.Hello
	if err = c.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.SessionID == "" || hello.AudioParams.SampleRate != 24000 || !hello.Features["playback_drain_v1"] {
		t.Fatal("wrong device hello", hello)
	}
	codec, err := realtimeaudio.New(16000, 960)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	packet, _ := codec.Encode(make([]byte, 1920))
	c.WriteMessage(websocket.BinaryMessage, packet)
	c.WriteJSON(map[string]any{"type": "listen", "state": "detect", "session_id": hello.SessionID})
	// Unnegotiated non-voice initialization has no authority, and must not close
	// the channel merely because it omitted voice-session metadata.
	c.WriteJSON(map[string]any{"type": "mcp", "payload": map[string]any{"method": "initialize"}})
	c.WriteJSON(map[string]any{"type": "status", "state": "connected"})
	c.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "realtime", "session_id": hello.SessionID})
	c.WriteMessage(websocket.BinaryMessage, packet)
	select {
	case pcm := <-appended:
		if len(pcm) != 1920 {
			t.Fatal("wrong business PCM")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("business input missing")
	}
	c.WriteJSON(map[string]any{"type": "listen", "state": "stop", "session_id": hello.SessionID})
	s.mu.Lock()
	done := s.activeDone
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("device stop cleanup stalled")
		}
	}
	select {
	case <-appended:
		t.Fatal("wake cache reached provider")
	default:
	}
	entries, _ := os.ReadDir(root)
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := ReadSessionInfo(filepath.Join(root, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if info.ClientType != "xiaozhi" || info.EndReason != "client_stop" || info.EndedAt == nil {
			t.Fatal("lifecycle lost", info)
		}
		found = true
	}
	if !found {
		t.Fatal("session manifest missing")
	}
}

func TestDeviceDrainTimeoutAndAlreadyFinishedRace(t *testing.T) {
	t.Run("no_receipt", func(t *testing.T) {
		server, client := testSocket(t)
		s := NewSession(server, Config{}, t.TempDir(), nil)
		s.wire = newXiaozhiWire(server, 1, 60, "scope")
		rt := s.addTurn()
		rt.turn.Begun = true
		if err := s.send(map[string]any{"type": "begin", "id": int64(1)}); err != nil {
			t.Fatal(err)
		}
		client.ReadMessage()
		if _, err := s.binary(rt, []byte{1}); err != nil {
			t.Fatal(err)
		}
		client.ReadMessage()
		if err := s.interrupt(); err != nil {
			t.Fatal(err)
		}
		client.ReadMessage()
		select {
		case <-s.ctx.Done():
		case <-time.After(4 * time.Second):
			t.Fatal("missing receipt did not end session")
		}
		s.wg.Wait()
		if s.terminalFailure() != "drain_timeout" {
			t.Fatal(s.terminalFailure())
		}
	})
	t.Run("finished_before_generator_done", func(t *testing.T) {
		server, client := testSocket(t)
		s := NewSession(server, Config{}, t.TempDir(), nil)
		defer s.cancel()
		w := newXiaozhiWire(server, 1, 60, "scope")
		s.wire = w
		rt := s.addTurn()
		rt.turn.Begun = true
		if err := s.send(map[string]any{"type": "begin", "id": int64(1)}); err != nil {
			t.Fatal(err)
		}
		client.ReadMessage()
		if _, err := s.binary(rt, []byte{1}); err != nil {
			t.Fatal(err)
		}
		client.ReadMessage()
		rt.turn.Ended = true
		if err := s.send(map[string]any{"type": "end", "id": int64(1)}); err != nil {
			t.Fatal(err)
		}
		client.ReadMessage()
		e, err := w.control([]byte(`{"type":"playback","state":"finished","session_id":"scope","playback_id":1,"sample_rate":24000,"played_samples":1440,"valid":true,"basis":"i2s_dma","interrupted":false}`))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.receiveReceipt(e); err != nil {
			t.Fatal(err)
		}
		// done deliberately remains open: the terminal has already proved the
		// final playback, so a second seal cannot demand a second firmware final.
		if err := s.interrupt(); err != nil {
			t.Fatal(err)
		}
		if rt.turn.DrainTarget != nil || rt.turn.Cut {
			t.Fatal("already finished playback resealed")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		next := s.addTurn()
		if _, err := s.settle(ctx, next); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDeviceFullProgressStillWaitsForRealFinished(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	defer s.cancel()
	w := newXiaozhiWire(server, 1, 60, "scope")
	s.wire = w
	rt := s.addTurn()
	rt.turn.Begun = true
	if err := s.send(map[string]any{"type": "begin", "id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	client.ReadMessage()
	if _, err := s.binary(rt, []byte{1}); err != nil {
		t.Fatal(err)
	}
	client.ReadMessage()
	rt.turn.Ended = true
	if err := s.send(map[string]any{"type": "end", "id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	client.ReadMessage()
	e, err := w.control([]byte(`{"type":"playback","state":"progress","session_id":"scope","playback_id":1,"sample_rate":24000,"played_samples":1440,"valid":true,"basis":"i2s_dma"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.receiveReceipt(e); err != nil {
		t.Fatal(err)
	}
	if realtimestate.PlaybackEvidence(rt.turn).Boundary == "complete_response" {
		t.Fatal("progress mislabeled as finished response")
	}
	if err = s.interrupt(); err != nil {
		t.Fatal(err)
	}
	client.ReadMessage()
	if rt.turn.DrainTarget == nil {
		t.Fatal("full progress bypassed terminal drain")
	}
	e, err = w.control([]byte(`{"type":"playback","state":"finished","session_id":"scope","playback_id":1,"sample_rate":24000,"played_samples":1440,"valid":true,"basis":"i2s_dma","interrupted":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.receiveReceipt(e); err != nil {
		t.Fatal(err)
	}
	s.cancel()
	s.wg.Wait()
}

func TestDeviceFinishedMustExplicitlyConfirmNoQueueInterruption(t *testing.T) {
	for _, value := range []string{"", "null", "true"} {
		server, _ := testSocket(t)
		w := newXiaozhiWire(server, 1, 60, "scope")
		w.blocks[1] = &devicePlayback{sent: 1440, sealed: true}
		tt := realtimestate.NewTurn(1)
		tt.Sent = 1440
		tt.Ended = true
		extra := ""
		if value != "" {
			extra = `,"interrupted":` + value
		}
		e, err := w.control([]byte(`{"type":"playback","state":"finished","session_id":"scope","playback_id":1,"sample_rate":24000,"played_samples":1440,"valid":true,"basis":"i2s_dma"` + extra + `}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.normalizeReceipt(tt, e); err == nil {
			t.Fatalf("ambiguous final accepted: %s", value)
		}
	}
}
