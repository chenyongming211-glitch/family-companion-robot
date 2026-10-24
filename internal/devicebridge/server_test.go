package devicebridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"github.com/gorilla/websocket"
)

type testRig struct {
	t       *testing.T
	server  *Server
	http    *httptest.Server
	backend *websocket.Conn
	client  *websocket.Conn
	input   chan map[string]any
	session string
}

func newRig(t *testing.T, features map[string]bool) *testRig {
	return newRigAtFrame(t, features, 60)
}

func newRigAtFrame(t *testing.T, features map[string]bool, frameMS int) *testRig {
	rig := newUnnegotiatedRig(t, features, frameMS, Config{}, nil)
	if features == nil || (features["drain"] && features["staging"] && features["coreaudio_playback"]) {
		rig.negotiate()
	}
	return rig
}

func newUnnegotiatedRig(t *testing.T, features map[string]bool, frameMS int, cfg Config, ready map[string]any) *testRig {
	t.Helper()
	if ready == nil {
		ready = map[string]any{"type": "ready", "mode": "drain", "input_rate": 16000, "output_rate": 24000}
	}
	connected := make(chan *websocket.Conn, 1)
	input := make(chan map[string]any, 32)
	back := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer server-only-test-token" || r.URL.Query().Get("mode") != "drain" {
			t.Error("missing protected drain authorization")
			http.Error(w, "bad backend", 401)
			return
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if err := c.WriteJSON(ready); err != nil {
			return
		}
		connected <- c
		for {
			var e map[string]any
			if c.ReadJSON(&e) != nil {
				return
			}
			input <- e
		}
	}))
	cfg.BackendURL = "ws" + strings.TrimPrefix(back.URL, "http") + "/v1/audio?mode=drain"
	cfg.BackendToken = "server-only-test-token"
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	rig := &testRig{t: t, server: s, http: h, input: input}
	t.Cleanup(func() {
		if rig.client != nil {
			rig.client.Close()
		}
		s.Close()
		h.Close()
		back.Close()
	})
	ticket := rig.ticket()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": {"Bearer " + ticket}, "Device-Id": {"native-mac"}, "Protocol-Version": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	rig.client = c
	if features == nil {
		features = map[string]bool{"drain": true, "staging": true, "coreaudio_playback": true}
	}
	rig.write(map[string]any{"type": "hello", "version": 1, "transport": "websocket", "audio_params": map[string]any{"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": frameMS}, "features": features})
	if !features["drain"] || !features["staging"] || !features["coreaudio_playback"] {
		return rig
	}
	select {
	case rig.backend = <-connected:
	case <-time.After(time.Second):
		t.Fatal("backend not opened")
	}
	return rig
}

func (r *testRig) negotiate() map[string]any {
	r.t.Helper()
	hello := r.readText()
	if hello["type"] != "hello" {
		r.t.Fatalf("hello=%v", hello)
	}
	if hello["control_seq"] != float64(1) {
		r.t.Fatal("hello must initialize control sequence")
	}
	r.session, _ = hello["session_id"].(string)
	params := hello["audio_params"].(map[string]any)
	if r.session == "" || params["sample_rate"] != float64(24000) || params["frame_duration"] != float64(60) {
		r.t.Fatalf("bad negotiated hello: %v", hello)
	}
	ready := r.readText()
	if ready["type"] != "ready" || ready["control_seq"] != float64(2) {
		r.t.Fatal("missing ready")
	}
	return ready
}

func (r *testRig) ticket() string {
	r.t.Helper()
	resp, err := http.Post(r.http.URL+"/v1/pair", "application/json", strings.NewReader(`{"device_id":"native-mac"}`))
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var v struct {
		Ticket  string `json:"ticket"`
		Expires int    `json:"expires_in"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil || resp.StatusCode != 200 || v.Expires != 120 || v.Ticket == "" {
		r.t.Fatalf("pair status=%d err=%v", resp.StatusCode, err)
	}
	return v.Ticket
}
func (r *testRig) write(e any) {
	r.t.Helper()
	if err := r.client.WriteJSON(e); err != nil {
		r.t.Fatal(err)
	}
}
func (r *testRig) send(e any) {
	r.t.Helper()
	if err := r.backend.WriteJSON(e); err != nil {
		r.t.Fatal(err)
	}
}
func (r *testRig) readText() map[string]any {
	r.t.Helper()
	r.client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var e map[string]any
	if err := r.client.ReadJSON(&e); err != nil {
		r.t.Fatal(err)
	}
	return e
}
func (r *testRig) receive() map[string]any {
	r.t.Helper()
	select {
	case e := <-r.input:
		return e
	case <-time.After(time.Second):
		r.t.Fatal("missing backend input")
	}
	return nil
}
func (r *testRig) handoff(id, epoch int, after []int) {
	r.send(map[string]any{"type": "output.handoff", "block_id": id, "epoch": epoch, "after": after, "input_epoch": epoch})
	e := r.readText()
	if e["block_id"] != float64(id) || e["playback_id"] != float64(id) || e["session_id"] != r.session {
		r.t.Fatalf("identity lost: %v", e)
	}
}
func (r *testRig) pcm(id, epoch, samples int) {
	r.send(map[string]any{"type": "audio", "block_id": id, "epoch": epoch, "samples": samples, "data": base64.StdEncoding.EncodeToString(make([]byte, samples*2))})
}
func (r *testRig) receipt(id, epoch, played int, finished bool) map[string]any {
	return map[string]any{"type": "playback", "session_id": r.session, "basis": "coreaudio_playback", "sample_rate": 24000, "block_id": id, "epoch": epoch, "played_samples": played, "valid": true, "finished": finished, "discarded": false}
}
func (r *testRig) packet() {
	r.t.Helper()
	e := r.readText()
	if e["type"] != "audio.packet" || e["samples"] != float64(1440) || e["playback_id"] != e["block_id"] || e["session_id"] != r.session {
		r.t.Fatalf("packet metadata=%v", e)
	}
	kind, p, err := r.client.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		r.t.Fatalf("packet kind=%d err=%v", kind, err)
	}
	d, err := audio.NewDecoderAtRate(24000)
	if err != nil {
		r.t.Fatal(err)
	}
	defer d.Close()
	pcm, err := d.Decode(p)
	if err != nil || len(pcm) != 2880 {
		r.t.Fatalf("real24k packet bytes=%d err=%v", len(pcm), err)
	}
}

func TestNativeHelloRejectsLegacyCapabilities(t *testing.T) {
	r := newRig(t, map[string]bool{"drain": true})
	e := r.readText()
	if e["type"] != "error" || e["code"] != "capability_required" {
		t.Fatalf("legacy accepted: %v", e)
	}
}

func TestTicketSingleUseAndBoundDevice(t *testing.T) {
	r := newRig(t, nil)
	ticket := r.ticket()
	url := "ws" + strings.TrimPrefix(r.http.URL, "http") + "/xiaozhi/v1/"
	_, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer " + ticket}, "Device-Id": {"other"}})
	if err == nil || resp.StatusCode != 401 {
		t.Fatal("ticket not device-bound")
	}
	r.client.Close()
	deadline := time.Now().Add(time.Second)
	for r.server.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	c, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer " + ticket}, "Device-Id": {"native-mac"}})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	_, resp, err = websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer " + ticket}, "Device-Id": {"native-mac"}})
	if err == nil || resp.StatusCode != 401 {
		t.Fatal("replayed ticket accepted")
	}
}

func TestOpusInputSplitsIntoAtMost20msPCM(t *testing.T) {
	r := newRig(t, nil)
	e, err := audio.NewEncoderAtRate(16000)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	packet, err := e.Encode(make([]byte, 1920))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.client.WriteMessage(websocket.BinaryMessage, packet); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		v := r.receive()
		pcm, err := base64.StdEncoding.DecodeString(v["data"].(string))
		if err != nil || v["type"] != "listen.audio" || len(pcm) != 640 {
			t.Fatalf("upstream=%v bytes=%d err=%v", v, len(pcm), err)
		}
	}
}

func TestNegotiated20And40msRealOpusInput(t *testing.T) {
	// Synthetic silence packets encoded by server libopus 1.4 at 16k mono.
	for _, fixture := range []struct {
		ms     int
		packet string
	}{{20, "SAvkwTbsxYA="}, {40, "UAXyYJt2F7vHLYsWmeiw"}} {
		t.Run(fmt.Sprint(fixture.ms), func(t *testing.T) {
			r := newRigAtFrame(t, nil, fixture.ms)
			packet, err := base64.StdEncoding.DecodeString(fixture.packet)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.client.WriteMessage(websocket.BinaryMessage, packet); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < fixture.ms/20; i++ {
				e := r.receive()
				pcm, err := base64.StdEncoding.DecodeString(e["data"].(string))
				if err != nil || len(pcm) != 640 || e["type"] != "listen.audio" {
					t.Fatalf("negotiated input bytes=%d err=%v", len(pcm), err)
				}
			}
		})
	}
}

func TestUnknownPlaybackClosesInsteadOfReleasingNewAudio(t *testing.T) {
	r := newRig(t, nil)
	r.handoff(1, 0, []int{})
	r.pcm(1, 0, 480)
	r.send(map[string]any{"type": "audio.end", "block_id": 1, "epoch": 0, "sent_samples": 480, "policy": "drain"})
	r.packet()
	r.readText()
	unknown := r.receipt(1, 0, 0, true)
	unknown["valid"] = false
	r.write(unknown)
	if e := r.receive(); e["valid"] != false {
		t.Fatal("unknown receipt not forwarded")
	}
	r.client.SetReadDeadline(time.Now().Add(time.Second))
	for {
		_, _, err := r.client.ReadMessage()
		if err != nil {
			if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
				t.Fatal("unknown playback left session playable")
			}
			break
		}
	}
}

func expectClosed(t *testing.T, r *testRig) {
	t.Helper()
	r.client.SetReadDeadline(time.Now().Add(time.Second))
	for {
		_, _, err := r.client.ReadMessage()
		if err != nil {
			if timed, ok := err.(interface{ Timeout() bool }); ok && timed.Timeout() {
				t.Fatal("failed playback session remained usable")
			}
			return
		}
	}
}

func TestBackendPlaybackUnknownTerminatesDeviceSession(t *testing.T) {
	r := newRig(t, nil)
	r.send(map[string]any{"type": "error", "code": "playback_unknown"})
	if r.readText()["code"] != "playback_unknown" {
		t.Fatal("failure reason lost")
	}
	expectClosed(t, r)
}

func TestUnsealedUnknownTerminatesDeviceSession(t *testing.T) {
	r := newRig(t, nil)
	r.handoff(1, 0, []int{})
	r.pcm(1, 0, 480)
	unknown := r.receipt(1, 0, 0, true)
	unknown["valid"] = false
	r.write(unknown)
	expectClosed(t, r)
}

func TestRevokedUnstartedAnswerCannotBeginAfterOldDrain(t *testing.T) {
	r := newRig(t, nil)
	for id := 1; id <= 2; id++ {
		after := []int{}
		if id == 2 {
			after = []int{1}
		}
		r.handoff(id, id-1, after)
		r.pcm(id, id-1, 480)
		r.send(map[string]any{"type": "audio.end", "block_id": id, "epoch": id - 1, "sent_samples": 480, "policy": "drain"})
		r.packet()
		r.readText()
	}
	r.send(map[string]any{"type": "output.revoke", "blocks": []int{2}, "input_epoch": 2})
	r.readText()
	r.write(map[string]any{"type": "playback.started", "session_id": r.session, "basis": "coreaudio_playback", "sample_rate": 24000, "block_id": 1, "epoch": 0})
	r.receive()
	r.write(r.receipt(1, 0, 1440, true))
	r.receive()
	r.write(map[string]any{"type": "playback.started", "session_id": r.session, "basis": "coreaudio_playback", "sample_rate": 24000, "block_id": 2, "epoch": 1})
	if r.readText()["code"] != "invalid_playback" {
		t.Fatal("revoked answer was authorized")
	}
}

func TestStartObservedBeforeRevokeCanArriveAfterRevoke(t *testing.T) {
	r := newRig(t, nil)
	r.handoff(1, 0, []int{})
	r.pcm(1, 0, 480)
	r.send(map[string]any{"type": "audio.end", "block_id": 1, "epoch": 0, "sent_samples": 480, "policy": "drain"})
	r.packet()
	end := r.readText()
	observed, ok := end["control_seq"].(float64)
	if !ok {
		t.Fatal("missing causal control sequence")
	}
	r.send(map[string]any{"type": "output.revoke", "blocks": []int{1}, "input_epoch": 1})
	r.readText()
	r.write(map[string]any{"type": "playback.started", "session_id": r.session, "basis": "coreaudio_playback", "sample_rate": 24000, "block_id": 1, "epoch": 0, "observed_control_seq": int(observed)})
	if r.receive()["type"] != "playback.started" {
		t.Fatal("legitimate prior local start lost")
	}
	r.write(r.receipt(1, 0, 1440, true))
	if r.receive()["finished"] != true {
		t.Fatal("already started output could not drain")
	}
}

func TestRepeatedRevokeKeepsEarliestCausalFence(t *testing.T) {
	r := newRig(t, nil)
	r.handoff(1, 0, []int{})
	r.pcm(1, 0, 480)
	r.send(map[string]any{"type": "audio.end", "block_id": 1, "epoch": 0, "sent_samples": 480, "policy": "drain"})
	r.packet()
	r.readText()
	r.send(map[string]any{"type": "output.revoke", "blocks": []int{1}, "input_epoch": 1})
	first := r.readText()["control_seq"].(float64)
	r.send(map[string]any{"type": "output.revoke", "blocks": []int{1}, "input_epoch": 2})
	r.readText()
	r.write(map[string]any{"type": "playback.started", "session_id": r.session, "basis": "coreaudio_playback", "sample_rate": 24000, "block_id": 1, "epoch": 0, "observed_control_seq": int(first)})
	if r.readText()["code"] != "invalid_playback" {
		t.Fatal("first revoke lost its fence")
	}
}

func TestCloseReleasesActiveSession(t *testing.T) {
	r := newRig(t, nil)
	done := make(chan struct{})
	go func() { r.server.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not join readers")
	}
	if r.server.Active() != 0 {
		t.Fatal("active session survived close")
	}
}

func TestLegacySessionClosesAtConfiguredTimeout(t *testing.T) {
	r := newUnnegotiatedRig(t, nil, 60, Config{SessionTimeout: 250 * time.Millisecond}, nil)
	r.negotiate()
	expectClosed(t, r)
	r.waitInactive()
}

func TestKeepOpenSurvivesConfiguredTimeoutAndManualDisconnect(t *testing.T) {
	const timeout = 250 * time.Millisecond
	r := newUnnegotiatedRig(t, nil, 60, Config{KeepOpen: true, SessionTimeout: timeout}, map[string]any{
		"type": "ready", "mode": "drain", "input_rate": 16000, "output_rate": 24000,
		"manual_close": true, "session_seconds": nil, "max_turns": nil,
	})
	ready := r.negotiate()
	if ready["manual_close"] != true || ready["session_seconds"] != nil || ready["max_turns"] != nil {
		t.Fatalf("manual-close capability lost: %v", ready)
	}
	<-time.After(3 * timeout)
	r.write(map[string]any{"type": "content.snapshot"})
	if e := r.receive(); e["type"] != "content.snapshot" {
		t.Fatalf("session did not remain usable: %v", e)
	}
	r.send(map[string]any{"type": "content.snapshot", "content": []any{}})
	if e := r.readText(); e["type"] != "content.snapshot" {
		t.Fatalf("backend read deadline was not cleared: %v", e)
	}
	if r.server.Active() != 1 {
		t.Fatal("keep-open session lost its lease")
	}
	r.client.Close()
	r.waitInactive()
}

func TestKeepOpenRequiresBackendManualCloseCapability(t *testing.T) {
	for _, capability := range []any{nil, false, "true"} {
		t.Run(fmt.Sprint(capability), func(t *testing.T) {
			r := newUnnegotiatedRig(t, nil, 60, Config{KeepOpen: true}, map[string]any{
				"type": "ready", "mode": "drain", "input_rate": 16000, "output_rate": 24000,
				"manual_close": capability,
			})
			if e := r.readText(); e["type"] != "error" || e["code"] != "backend_capability_required" {
				t.Fatalf("manual-close mismatch accepted: %v", e)
			}
			expectClosed(t, r)
			r.waitInactive()
		})
	}
}

func TestCloseJoinsKeepOpenSessionReaders(t *testing.T) {
	r := newUnnegotiatedRig(t, nil, 60, Config{KeepOpen: true}, map[string]any{
		"type": "ready", "mode": "drain", "input_rate": 16000, "output_rate": 24000, "manual_close": true,
	})
	r.negotiate()
	done := make(chan struct{})
	go func() { r.server.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("keep-open shutdown did not join readers")
	}
	if r.server.Active() != 0 {
		t.Fatal("keep-open active session survived server close")
	}
	expectClosed(t, r)
}

func (r *testRig) waitInactive() {
	r.t.Helper()
	deadline := time.Now().Add(time.Second)
	for r.server.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.server.Active() != 0 {
		r.t.Fatal("closed session kept its lease")
	}
}

func TestKeepOpenPreservesSessionTimeoutDefaultAndValidation(t *testing.T) {
	for _, keepOpen := range []bool{false, true} {
		cfg := Config{BackendURL: "ws://127.0.0.1:18010/v1/audio?mode=drain", BackendToken: "test", KeepOpen: keepOpen}
		s, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if s.cfg.SessionTimeout != 15*time.Minute {
			t.Fatalf("keep_open=%t changed default timeout: %s", keepOpen, s.cfg.SessionTimeout)
		}
		s.Close()
		for _, timeout := range []time.Duration{-time.Second, 15*time.Minute + time.Second} {
			cfg.SessionTimeout = timeout
			if s, err := New(cfg); err == nil {
				s.Close()
				t.Fatalf("keep_open=%t accepted invalid timeout %s", keepOpen, timeout)
			}
		}
	}
}

func TestCodecTailMustPlayBeforeFinalReceipt(t *testing.T) {
	r := newRig(t, nil)
	r.handoff(1, 0, []int{})
	for i := 0; i < 4; i++ {
		r.pcm(1, 0, 480)
	}
	r.packet()
	r.send(map[string]any{"type": "audio.end", "block_id": 1, "epoch": 0, "sent_samples": 1920, "policy": "drain"})
	r.packet()
	end := r.readText()
	look := int(end["lookahead_samples"].(float64))
	total := int(end["device_sent_samples"].(float64))
	if end["type"] != "audio.end" || end["valid_samples"] != float64(1920) || look <= 0 || total != 2880 {
		t.Fatalf("bad seal: %v", end)
	}
	r.write(map[string]any{"type": "playback.started", "session_id": r.session, "basis": "coreaudio_playback", "sample_rate": 24000, "block_id": 1, "epoch": 0})
	if r.receive()["type"] != "playback.started" {
		t.Fatal("start lost")
	}
	r.write(r.receipt(1, 0, look+600, false))
	progress := r.receive()
	if progress["played_samples"] != float64(600) {
		t.Fatalf("lookahead not removed %v", progress)
	}
	r.write(r.receipt(1, 0, 1920+look, true))
	if r.readText()["code"] != "invalid_playback" {
		t.Fatal("accepted unplayed pad/tail")
	}
	r.write(r.receipt(1, 0, total, true))
	final := r.receive()
	if final["played_samples"] != float64(1920) || final["finished"] != true {
		t.Fatalf("final map=%v", final)
	}
	r.send(map[string]any{"type": "playback.ack", "block_id": 1, "epoch": 0})
	if ack := r.readText(); ack["type"] != "playback.ack" || ack["session_id"] != r.session {
		t.Fatalf("ack=%v", ack)
	}
}

func TestStagedStartAndDiscardAreIdentityFenced(t *testing.T) {
	r := newRig(t, nil)
	r.handoff(1, 0, []int{})
	r.pcm(1, 0, 480)
	r.send(map[string]any{"type": "audio.end", "block_id": 1, "epoch": 0, "sent_samples": 480, "policy": "drain"})
	r.packet()
	r.readText()
	r.send(map[string]any{"type": "output.drain", "blocks": []int{1}, "next_epoch": 1})
	if r.readText()["type"] != "output.drain" {
		t.Fatal("missing drain")
	}
	r.handoff(2, 1, []int{1})
	r.pcm(2, 1, 480)
	r.send(map[string]any{"type": "audio.end", "block_id": 2, "epoch": 1, "sent_samples": 480, "policy": "drain"})
	r.packet()
	r.readText()
	start := map[string]any{"type": "playback.started", "session_id": r.session, "basis": "coreaudio_playback", "sample_rate": 24000, "block_id": 2, "epoch": 1}
	r.write(start)
	if r.readText()["code"] != "invalid_playback" {
		t.Fatal("staged audio started before drain")
	}
	r.send(map[string]any{"type": "output.revoke", "blocks": []int{2}, "input_epoch": 2})
	if r.readText()["type"] != "output.revoke" {
		t.Fatal("missing revoke")
	}
	discard := r.receipt(2, 1, 0, true)
	discard["discarded"] = true
	r.write(discard)
	if e := r.receive(); e["discarded"] != true || e["played_samples"] != float64(0) {
		t.Fatalf("discard=%v", e)
	}
	wrong := r.receipt(1, 0, 1440, true)
	wrong["session_id"] = "wrong"
	r.write(wrong)
	if r.readText()["code"] != "invalid_playback" {
		t.Fatal("wrong session accepted")
	}
	wrong = r.receipt(1, 9, 1440, true)
	r.write(wrong)
	if r.readText()["code"] != "invalid_playback" {
		t.Fatal("wrong epoch accepted")
	}
}

func TestPairRejectsNonLoopbackOriginAndExpiredTicket(t *testing.T) {
	s, err := New(Config{BackendURL: "ws://127.0.0.1:18010/v1/audio?mode=drain", BackendToken: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.tickets["expired"] = ticket{device: "native-mac", expires: time.Now().Add(-time.Second)}
	expired := httptest.NewRequest("GET", "http://localhost/xiaozhi/v1/", nil)
	expired.RemoteAddr = "127.0.0.1:1"
	expired.Header.Set("Authorization", "Bearer expired")
	expired.Header.Set("Device-Id", "native-mac")
	expiredResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(expiredResponse, expired)
	if expiredResponse.Code != 401 {
		t.Fatal("expired ticket accepted")
	}
	for _, remote := range []string{"203.0.113.1:9", "bad"} {
		req := httptest.NewRequest("POST", "http://localhost/v1/pair", bytes.NewBufferString(`{"device_id":"native-mac"}`))
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatalf("remote=%s status=%d", remote, w.Code)
		}
	}
	req := httptest.NewRequest("POST", "http://localhost/v1/pair", strings.NewReader(`{"device_id":"native-mac"}`))
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("browser origin paired")
	}
	for _, url := range []string{"ws://example.com/v1/audio?mode=drain", "ws://127.0.0.1:18010/v1/audio?mode=continuous"} {
		if _, err := New(Config{BackendURL: url, BackendToken: "test"}); err == nil {
			t.Fatalf("unsafe backend accepted %s", url)
		}
	}
}

func TestJSONRejectsTrailingEvent(t *testing.T) {
	if _, err := parseEvent([]byte(`{"type":"playback"}{"type":"hard.stop"}`)); err == nil {
		t.Fatal("accepted two events in one frame")
	}
}

func TestHistoryStaysBoundedWithUnfinishedFirstBlock(t *testing.T) {
	s := &session{blocks: make(map[int64]*block)}
	for id := int64(1); id <= 100; id++ {
		s.order = append(s.order, id)
		s.blocks[id] = &block{id: id, finished: id > 1}
		s.prune()
	}
	if len(s.blocks) > 64 || s.blocks[1] == nil {
		t.Fatalf("history grew or dropped unsettled block: %d", len(s.blocks))
	}
}

func TestCodecAllowanceIncludesTailOfEveryBlock(t *testing.T) {
	s := &session{blocks: make(map[int64]*block)}
	for id := int64(1); id <= 10; id++ {
		s.blocks[id] = &block{id: id, valid: 1, lookahead: 156}
	}
	if !s.withinCredits() {
		t.Fatal("600ms codec boundary rejected")
	}
	s.blocks[11] = &block{id: 11, valid: 1, lookahead: 156}
	if s.withinCredits() {
		t.Fatal("padding escaped total device allowance")
	}
}

func ExampleConfig() {
	fmt.Println("backend token remains on server; native ticket expires after 120 seconds")
	// Output: backend token remains on server; native ticket expires after 120 seconds
}
