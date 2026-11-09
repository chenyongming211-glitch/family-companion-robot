package realtimelive_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/management"
	"familyrobot/internal/realtimelive"
	"github.com/gorilla/websocket"
)

func TestOnboardingManagementActivationUnlocksVoiceAdmission(t *testing.T) {
	// Entirely synthetic cloud integration: actual management persistence and
	// voice admission. No hello is sent, so no model/audio runner is opened.
	const factoryMAC = "aa:bb:cc:dd:ee:02"
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	hash, err := management.HashPassword("synthetic-only-password")
	if err != nil {
		t.Fatal(err)
	}
	cfg := management.Config{SchemaVersion: 1,
		Accounts: []management.AccountConfig{{ID: "alice", Username: "alice", Password: hash,
			Memberships: []management.Membership{{FamilyID: "home", CanAddDevices: true}}}},
		Families: []management.FamilyConfig{{ID: "home", Name: "Synthetic household"}},
	}
	if err := management.WritePrivateConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	bundle, err := management.ProvisionFactory(path, factoryMAC, "synthetic-s3", "微雪 ESP32-S3", func(management.FactoryBundle) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	createManager := func() *httptest.Server {
		t.Helper()
		current, err := management.LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		app, err := management.NewServer(current, management.Options{ConfigPath: path,
			RecordsPath: t.TempDir(), AuditPath: filepath.Join(directory, "audit.jsonl"), Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		return httptest.NewServer(app)
	}
	manager := createManager()
	defer manager.Close()
	call := func(endpoint, method, resource string, input any, cookie *http.Cookie, device bool) (int, []byte, []*http.Cookie) {
		t.Helper()
		var body io.Reader
		if input != nil {
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(raw)
		}
		r, err := http.NewRequest(method, endpoint+resource, body)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if device {
			r.Header.Set("Device-Id", bundle.MAC)
			r.Header.Set("Authorization", "Bearer "+bundle.DeviceKey)
		}
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 8192))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, raw, response.Cookies()
	}
	status, _, cookies := call(manager.URL, "POST", "/api/family/login", map[string]string{"username": "alice", "password": "synthetic-only-password"}, nil, false)
	if status != 200 || len(cookies) != 1 {
		t.Fatal("synthetic family login failed")
	}
	status, raw, _ := call(manager.URL, "POST", "/api/family/onboarding", map[string]string{"family_id": "home", "claim_code": bundle.ClaimCode, "name": "Synthetic robot"}, cookies[0], false)
	if status != 200 {
		t.Fatalf("claim creation: %d", status)
	}
	var claim map[string]any
	if err := json.Unmarshal(raw, &claim); err != nil || claim["status"] != "waiting" {
		t.Fatal("claim is not waiting")
	}
	voice := realtimelive.NewServer(realtimelive.Config{}, nil, t.TempDir(), "synthetic-onboarding")
	dc, err := (realtimelive.DeviceConfig{Token: strings.Repeat("q", 64), AllowedDevice: "aa:bb:cc:dd:ee:01"}).WithOnboardingAuthURL(manager.URL + "/api/internal/device-auth")
	if err != nil {
		t.Fatal(err)
	}
	device := httptest.NewServer(voice.DeviceHandler(dc))
	defer device.Close()
	dial := func() (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(device.URL, "http")+"/xiaozhi/v1/", http.Header{
			"Authorization": []string{"Bearer " + bundle.DeviceKey}, "Device-Id": []string{bundle.MAC},
		})
	}
	conn, response, err := dial()
	if conn != nil {
		conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != 403 {
		t.Fatal("unconfirmed factory identity admitted")
	}
	status, _, _ = call(manager.URL, "POST", "/api/device/activation", map[string]any{"id": claim["id"], "challenge": claim["challenge"]}, nil, true)
	if status != 200 {
		t.Fatalf("independent device activation: %d", status)
	}
	status, raw, _ = call(manager.URL, "GET", "/api/device/bootstrap", nil, nil, true)
	var bootstrap struct{ Websocket map[string]any }
	if status != 200 || json.Unmarshal(raw, &bootstrap) != nil || bootstrap.Websocket["url"] != bundle.VoiceURL || bootstrap.Websocket["token"] != nil {
		t.Fatal("bootstrap did not return only the self-hosted voice route")
	}
	conn, _, err = dial()
	if err != nil {
		t.Fatal("confirmed device admission failed")
	}
	// Stop before hello/session creation: only the authenticated upgrade is tested.
	conn.Close()
	persisted, err := management.LoadConfig(path)
	if err != nil || len(persisted.Bindings) != 1 || len(persisted.Devices) != 1 || !persisted.Bindings[0].From.Equal(now) {
		t.Fatal("single authority binding was not durably preserved")
	}
	restarted := createManager()
	defer restarted.Close()
	status, _, fresh := call(restarted.URL, "POST", "/api/family/login", map[string]string{"username": "alice", "password": "synthetic-only-password"}, nil, false)
	if status != 200 || len(fresh) != 1 {
		t.Fatal("restart login failed")
	}
	status, raw, _ = call(restarted.URL, "GET", "/api/family/onboarding/"+claim["id"].(string), nil, fresh[0], false)
	if status != 200 || json.Unmarshal(raw, &claim) != nil || claim["status"] != "confirmed" || claim["device_id"] != bundle.DeviceID {
		t.Fatal("confirmed identity lost on management restart")
	}
}
