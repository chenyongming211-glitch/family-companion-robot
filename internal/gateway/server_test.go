package gateway

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

const testToken = "test-device-token-at-least-thirty-two-characters"
const testSecret = "test-bootstrap-secret-at-least-thirty-two-characters"

func testConfig(upstream string) Config {
	return Config{DeviceToken: testToken, BootstrapSecret: testSecret, PublicWSURL: "ws://example.test/xiaozhi/v1/", Qwen: qwen.Config{URL: upstream, APIKey: "test-api-key", Voice: "Tina", VAD: "semantic_vad", Instructions: "test"}, MaxSessions: 1, IdleTimeout: time.Minute, SessionTimeout: 10 * time.Minute}
}

func TestAuthOTAAndHealth(t *testing.T) {
	g, err := New(testConfig("ws://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(g.Handler())
	defer s.Close()
	r, err := http.Get(s.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.Status)
	}
	r, _ = http.Get(s.URL + "/xiaozhi/v1/")
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("unauthenticated websocket accepted")
	}
	req, _ := http.NewRequest("POST", s.URL+"/xiaozhi/ota/"+testSecret+"/", strings.NewReader(`{"application":{"version":"2.5.2"}}`))
	req.Header.Set("Device-Id", "c4:9e:7e:12:99:10")
	r, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var v map[string]any
	json.NewDecoder(r.Body).Decode(&v)
	ws, ok := v["websocket"].(map[string]any)
	if r.StatusCode != 200 || !ok || ws["token"] != testToken || ws["version"] != float64(1) {
		t.Fatalf("OTA: %v", v)
	}
	if _, exists := v["firmware"].(map[string]any)["url"]; exists {
		t.Fatal("unexpected firmware upgrade")
	}
	r, _ = http.Post(s.URL+"/xiaozhi/ota/wrong/", "application/json", strings.NewReader(`{}`))
	r.Body.Close()
	if r.StatusCode != 404 {
		t.Fatal("public bootstrap exposed")
	}
}

func TestRejectUnsafeConfig(t *testing.T) {
	cfg := testConfig("ws://127.0.0.1:1")
	cfg.DeviceToken = ""
	if _, err := New(cfg); err == nil {
		t.Fatal("accepted missing auth")
	}
}

func TestInterruptDropsOldConnectionAudioAndStartsNewResponse(t *testing.T) {
	for _, source := range []string{"device", "model", "pending", "capturing"} {
		t.Run(source, func(t *testing.T) { testInterrupt(t, source) })
	}
}
func testInterrupt(t *testing.T, source string) {
	var opened atomic.Int32
	oldContinue := make(chan struct{})
	var once sync.Once
	resumeOld := func() { once.Do(func() { close(oldContinue) }) }
	defer resumeOld()
	sawRebuilt := make(chan bool, 1)
	captureReady := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		n := opened.Add(1)
		c.WriteJSON(map[string]any{"type": "session.created"})
		var setup map[string]any
		if c.ReadJSON(&setup) != nil {
			return
		}
		if n == 2 {
			instructions := setup["session"].(map[string]any)["instructions"].(string)
			sawRebuilt <- strings.Contains(instructions, "中断")
		}
		c.WriteJSON(map[string]any{"type": "session.updated"})
		answered := false
		for {
			var ev map[string]any
			if c.ReadJSON(&ev) != nil {
				return
			}
			if ev["type"] != "input_audio_buffer.append" || answered {
				continue
			}
			answered = true
			if n == 1 && source == "capturing" {
				captureReady <- struct{}{}
				<-oldContinue
				c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
				c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "old"}})
				c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "old", "delta": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes*8))})
				return
			}
			if n == 1 && source == "pending" {
				c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
				c.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "synthetic input"})
				<-oldContinue
				return
			}
			id := "old"
			if n == 2 {
				id = "new"
			}
			c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
			frames := 30
			if n == 2 {
				frames = 2
			}
			c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": id, "delta": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes*frames))})
			if n == 1 {
				<-oldContinue
				if source == "model" {
					c.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_started"})
				}
				c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "old", "delta": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))})
				return
			}
			// A delayed response from the previous round must not enter the new queue.
			c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "old", "delta": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes*8))})
			c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
		}
	}))
	defer upstream.Close()
	g, err := New(testConfig("ws" + strings.TrimPrefix(upstream.URL, "http")))
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(g.Handler())
	defer s.Close()
	h := http.Header{"Authorization": []string{"Bearer " + testToken}, "Device-Id": []string{"c4:9e:7e:12:99:10"}, "Protocol-Version": []string{"1"}}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/xiaozhi/v1/", h)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(8 * time.Second))
	c.WriteJSON(map[string]any{"type": "hello", "version": 1, "transport": "websocket", "audio_params": map[string]any{"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60}})
	var hello map[string]any
	if c.ReadJSON(&hello) != nil || hello["type"] != "hello" {
		t.Fatal("missing hello")
	}
	c.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "realtime"})
	enc, _ := audio.NewEncoder()
	defer enc.Close()
	packet, _ := enc.Encode(make([]byte, audio.FrameBytes))
	c.WriteMessage(websocket.BinaryMessage, packet)
	firstFrames := 0
	if source == "capturing" {
		select {
		case <-captureReady:
		case <-time.After(8 * time.Second):
			t.Fatal("upstream did not receive capturing audio")
		}
		firstFrames = 1
	}
	for firstFrames < 1 {
		kind, data, err := c.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.BinaryMessage {
			firstFrames++
		}
		if source == "pending" && kind == websocket.TextMessage {
			var v map[string]any
			json.Unmarshal(data, &v)
			if v["type"] == "stt" {
				break
			}
		}
	}
	if source != "model" {
		c.WriteJSON(map[string]any{"type": "abort"})
	} else {
		resumeOld()
	}
	stopped := false
	for !stopped {
		kind, data, err := c.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.TextMessage {
			var v map[string]any
			json.Unmarshal(data, &v)
			stopped = v["type"] == "tts" && v["state"] == "stop"
		}
	}
	resumeOld()
	c.WriteMessage(websocket.BinaryMessage, packet)
	if !<-sawRebuilt {
		t.Fatal("reconnection lost interrupted context marker")
	}
	frames := 0
	newStarted := false
	for {
		kind, data, err := c.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.BinaryMessage {
			if !newStarted {
				t.Fatal("old audio arrived after stop")
			}
			frames++
			continue
		}
		var v map[string]any
		json.Unmarshal(data, &v)
		if v["type"] == "tts" && v["state"] == "start" {
			newStarted = true
		}
		if v["type"] == "tts" && v["state"] == "stop" {
			break
		}
	}
	if frames != 2 {
		t.Fatalf("new playback contained %d frames, expected 2", frames)
	}
	c.Close()
	deadline := time.Now().Add(time.Second)
	for g.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if g.Active() != 0 {
		t.Fatal("device disconnect leaked active session")
	}
}
