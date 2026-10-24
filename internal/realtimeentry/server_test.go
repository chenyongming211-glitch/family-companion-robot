package realtimeentry

import (
	"encoding/json"
	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func rig(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		for {
			var e map[string]any
			if c.ReadJSON(&e) != nil {
				return
			}
			if e["type"] == "session.update" {
				c.WriteJSON(map[string]any{"type": "session.updated"})
			}
		}
	}))
	t.Cleanup(up.Close)
	s, err := New(qwen.Config{URL: "ws" + strings.TrimPrefix(up.URL, "http"), APIKey: "private-test-key", Voice: "Tina", VAD: "semantic_vad", Instructions: "test"})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	t.Cleanup(h.Close)
	t.Cleanup(s.Close)
	return s, h
}
func pair(t *testing.T, base string) string {
	t.Helper()
	r, e := http.Post(base+"/v1/pair", "application/json", strings.NewReader(`{"device_id":"native-realtime"}`))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var v map[string]any
	json.NewDecoder(r.Body).Decode(&v)
	if r.StatusCode != 200 || v["device_id"] != "native-realtime" || v["backend"] != "qwen-native-existing" || v["voice"] != "Tina" {
		t.Fatalf("pair: %d %v", r.StatusCode, v)
	}
	token, _ := v["ticket"].(string)
	if len(token) < 32 {
		t.Fatal("missing short-lived ticket")
	}
	return token
}
func dial(base, token string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + token}, "Device-Id": []string{"native-realtime"}, "Protocol-Version": []string{"1"}})
}
func TestPairingAuthenticatesExistingXiaozhiGateway(t *testing.T) {
	s, h := rig(t)
	token := pair(t, h.URL)
	c, _, e := dial(h.URL, token)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	c.WriteJSON(map[string]any{"type": "hello", "version": 1, "transport": "websocket", "audio_params": map[string]any{"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 20}})
	var hello map[string]any
	if c.ReadJSON(&hello) != nil || hello["type"] != "hello" {
		t.Fatal("existing gateway hello missing")
	}
	if s.g.Active() != 1 {
		t.Fatal("gateway not owning session")
	}
	_, r, e := dial(h.URL, token)
	if e == nil || r == nil || r.StatusCode != 401 {
		t.Fatal("ticket reused")
	}
	r2, err := http.Get(h.URL + "/xiaozhi/ota/anything/")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 404 {
		t.Fatal("OTA exposed")
	}
}
func TestOriginDeviceAndExpiredTicketRejected(t *testing.T) {
	s, h := rig(t)
	for _, body := range []string{`{"device_id":"watch"}`, `{}`, `{"device_id":"native-realtime","url":"ws://bad"}`} {
		r, e := http.Post(h.URL+"/v1/pair", "application/json", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Fatal("invalid pairing accepted")
		}
	}
	req, _ := http.NewRequest("POST", h.URL+"/v1/pair", strings.NewReader(`{"device_id":"native-realtime"}`))
	req.Header.Set("Origin", "https://example.test")
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("browser origin accepted")
	}
	token := pair(t, h.URL)
	s.mu.Lock()
	s.tickets[token] = time.Now().Add(-time.Second)
	s.mu.Unlock()
	_, r, e = dial(h.URL, token)
	if e == nil || r == nil || r.StatusCode != 401 {
		t.Fatal("expired ticket accepted")
	}
	if s.g.Active() != 0 {
		t.Fatal("unauthorized upstream session")
	}
}
func TestHealthAndCapacityDoNotExposeCredentials(t *testing.T) {
	s, h := rig(t)
	r, e := http.Get(h.URL + "/healthz")
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var v map[string]any
	json.NewDecoder(r.Body).Decode(&v)
	if v["text_recording"] != false || v["backend"] != "qwen-native-existing" || v["active_sessions"] != float64(0) {
		t.Fatalf("health %v", v)
	}
	encoded, _ := json.Marshal(v)
	if strings.Contains(string(encoded), "private-test-key") || strings.Contains(string(encoded), s.token) {
		t.Fatal("credential leak")
	}
	for i := 0; i < 8; i++ {
		pair(t, h.URL)
	}
	busy, err := http.Post(h.URL+"/v1/pair", "application/json", strings.NewReader(`{"device_id":"native-realtime"}`))
	if err != nil {
		t.Fatal(err)
	}
	busy.Body.Close()
	if busy.StatusCode != 429 {
		t.Fatal("unbounded tickets")
	}
}
