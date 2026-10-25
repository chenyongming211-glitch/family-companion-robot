package realtimelive

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func ownedRequest(t *testing.T, base, path, body, token string, want int) map[string]any {
	t.Helper()
	req, err := http.NewRequest("POST", base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s status=%d want=%d", path, resp.StatusCode, want)
	}
	var value map[string]any
	if len(raw) > 0 && json.Unmarshal(raw, &value) != nil {
		t.Fatal("invalid response JSON")
	}
	return value
}

func TestOwnedEnrollmentTicketBindingReconnectAndRecords(t *testing.T) {
	root := t.TempDir()
	s := NewServer(Config{}, nil, root, "ownership-test")
	type connected struct {
		dir  string
		info map[string]any
	}
	entered := make(chan connected, 4)
	released := make(chan struct{}, 4)
	s.run = func(ws *websocket.Conn, dir string) {
		raw, err := os.ReadFile(filepath.Join(dir, "session.json"))
		if err != nil {
			t.Error(err)
			return
		}
		var info map[string]any
		if json.Unmarshal(raw, &info) != nil {
			t.Error("manifest invalid")
			return
		}
		session := NewSession(ws, Config{}, dir, nil)
		defer session.cancel()
		if err := session.record("test_owned", map[string]any{"device_id": "forged", "session_id": "forged", "client_type": "forged", "client_version": "forged", "text": "private-test"}); err != nil {
			t.Error(err)
		}
		intent, err := newIntent(dir, "test_owned", true)
		if err != nil {
			t.Error(err)
		} else if err = intent.update(map[string]any{"session_id": "forged", "device_id": "forged", "client_version": "forged"}); err != nil {
			t.Error(err)
		}
		entered <- connected{dir, info}
		ws.ReadMessage()
		released <- struct{}{}
	}
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	a := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
	b := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
	if a["device_id"] == b["device_id"] || a["credential"] == b["credential"] {
		t.Fatal("enrollments shared identity")
	}
	connect := func(d map[string]any) connected {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"device_id": d["device_id"], "client_version": "ownership-test"})
		pair := ownedRequest(t, h.URL, "/pair", string(body), d["credential"].(string), 200)
		if pair["device_id"] != d["device_id"] {
			t.Fatal("pair identity missing")
		}
		headers := http.Header{"Authorization": []string{"Bearer " + pair["ticket"].(string)}, "Device-Id": []string{"forged"}}
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/live", headers)
		if err != nil {
			t.Fatal(err)
		}
		var c connected
		select {
		case c = <-entered:
		case <-time.After(time.Second):
			t.Fatal("session not admitted")
		}
		ws.Close()
		<-released
		s.mu.Lock()
		done := s.activeDone
		s.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("cleanup stalled")
			}
		}
		if c.info["device_id"] != d["device_id"] || c.info["session_id"] == "" || c.info["client_type"] != "mac_fixture" {
			t.Fatal("manifest ownership missing")
		}
		raw, _ := os.ReadFile(filepath.Join(c.dir, "events.jsonl"))
		var event map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &event) != nil || event["device_id"] != d["device_id"] || event["session_id"] != c.info["session_id"] {
			t.Fatal("event ownership overridden")
		}
		files, _ := filepath.Glob(filepath.Join(c.dir, "test_owned-*.json"))
		if len(files) != 1 {
			t.Fatal("intent missing")
		}
		raw, _ = os.ReadFile(files[0])
		var intent map[string]any
		json.Unmarshal(raw, &intent)
		if intent["device_id"] != d["device_id"] || intent["session_id"] != c.info["session_id"] {
			t.Fatal("intent ownership missing")
		}
		if strings.Contains(string(raw), d["credential"].(string)) {
			t.Fatal("credential leaked")
		}
		return c
	}
	first, second, other := connect(a), connect(a), connect(b)
	if first.info["session_id"] == second.info["session_id"] || first.dir == second.dir || other.info["device_id"] == first.info["device_id"] {
		t.Fatal("reconnect or isolation failed")
	}
	// Reload the registry, as after server restart: ID and credential remain valid.
	restarted := httptest.NewServer(NewServer(Config{}, nil, root, "restart").Handler())
	defer restarted.Close()
	body, _ := json.Marshal(map[string]any{"device_id": a["device_id"]})
	ownedRequest(t, restarted.URL, "/pair", string(body), a["credential"].(string), 200)
}

func TestOwnedAuthenticationCannotDowngradeOrClaimOtherDevice(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "test")
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	a := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_native"}`, "", 201)
	b := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_native"}`, "", 201)
	body, _ := json.Marshal(map[string]any{"device_id": b["device_id"]})
	ownedRequest(t, h.URL, "/pair", string(body), a["credential"].(string), 403)
	ownedRequest(t, h.URL, "/pair", string(body), "", 403)
	ownedRequest(t, h.URL, "/pair", `{}`, a["credential"].(string), 403)
	ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_native","device_id":"chosen"}`, "", 400)
	ownedRequest(t, h.URL, "/devices", `{"client_type":"esp32"}`, "", 400)
	ownedRequest(t, h.URL, "/pair", `{"device_id":"x"} {}`, "", 400)
	// Unattributed legacy admission is still explicit, never inferred as device A.
	pair := ownedRequest(t, h.URL, "/pair", "", "", 200)
	if pair["device_id"] != nil {
		t.Fatal("legacy assigned a stable device")
	}
}

func TestOwnedCorruptRegistryFailsClosed(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "devices.json"), []byte(`broken`), 0600)
	s := NewServer(Config{}, nil, root, "test")
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	ownedRequest(t, h.URL, "/pair", "", "", 503)
	raw, _ := os.ReadFile(filepath.Join(root, "devices.json"))
	if string(raw) != "broken" {
		t.Fatal("registry reset")
	}
}
