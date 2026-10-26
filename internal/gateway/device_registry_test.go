package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const registryPrimary = "aa:bb:cc:dd:ee:01"
const registrySecond = "aa:bb:cc:dd:ee:02"

func protectedRegistry(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "devices": []any{map[string]any{"mac": registryPrimary, "token": testToken}, map[string]any{"mac": registrySecond, "token": strings.Repeat("z", 64)}}})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestRegistryOTADistributesOnlyEachRegisteredCredential(t *testing.T) {
	cfg := testConfig("ws://127.0.0.1:1")
	cfg.AllowedDevice = registryPrimary
	cfg.DeviceRegistryPath = protectedRegistry(t)
	cfg.PublicWSURL = "ws://example.test:18041/xiaozhi/v1/"
	g, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	// The loaded protected file is an immutable startup snapshot.
	if err := os.WriteFile(cfg.DeviceRegistryPath, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ mac, token string }{{registryPrimary, testToken}, {strings.ToUpper(registrySecond), strings.Repeat("z", 64)}} {
		req := httptest.NewRequest("POST", "/xiaozhi/ota/"+testSecret+"/", strings.NewReader(`{"application":{"version":"2.2.4"}}`))
		req.Header.Set("Device-Id", v.mac)
		w := httptest.NewRecorder()
		g.Handler().ServeHTTP(w, req)
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		ws, _ := body["websocket"].(map[string]any)
		firmware, _ := body["firmware"].(map[string]any)
		if w.Code != 200 || ws["token"] != v.token || ws["url"] != cfg.PublicWSURL || firmware["version"] != "2.2.4" || firmware["url"] != nil {
			t.Fatalf("wrong registered OTA payload: status=%d", w.Code)
		}
	}
	for _, v := range []struct {
		path, mac, origin, body string
		status                  int
	}{
		{"/xiaozhi/ota/" + testSecret + "/", "aa:bb:cc:dd:ee:03", "", "{}", 403},
		{"/xiaozhi/ota/" + testSecret + "/", registrySecond, "https://evil.test", "{}", 403},
		{"/xiaozhi/ota/" + testSecret + "/", registrySecond, "", `{}` + strings.Repeat(" ", 32768), 400},
		{"/xiaozhi/ota/" + testSecret + "/", registrySecond, "", `{"application":{"version":"` + strings.Repeat("x", 65) + `"}}`, 400},
		{"/xiaozhi/ota/wrong/", registrySecond, "", "{}", 404},
	} {
		req := httptest.NewRequest("POST", v.path, strings.NewReader(v.body))
		req.Header.Set("Device-Id", v.mac)
		req.Header.Set("Origin", v.origin)
		w := httptest.NewRecorder()
		g.Handler().ServeHTTP(w, req)
		if w.Code != v.status {
			t.Fatalf("OTA boundary status=%d want=%d", w.Code, v.status)
		}
	}
	// Neither secret can admit the second device to the deprecated voice backend.
	for _, token := range []string{testToken, strings.Repeat("z", 64)} {
		req := httptest.NewRequest("GET", "/xiaozhi/v1/", nil)
		req.Header.Set("Device-Id", registrySecond)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		g.Handler().ServeHTTP(w, req)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("second device entered legacy backend", w.Code)
		}
	}
}
func TestRegistryGatewayRequiresMatchingUnchangedLegacyPrimary(t *testing.T) {
	for _, v := range []struct{ mac, token string }{{"", testToken}, {registryPrimary, ""}, {registryPrimary, strings.Repeat("x", 64)}, {registrySecond, testToken}} {
		cfg := testConfig("ws://127.0.0.1:1")
		cfg.AllowedDevice = v.mac
		cfg.DeviceToken = v.token
		cfg.DeviceRegistryPath = protectedRegistry(t)
		if _, err := New(cfg); err == nil {
			t.Fatal("registry primary mismatch accepted")
		}
	}
}
func TestLegacyOTARetainsSingleEnvironmentCredential(t *testing.T) {
	cfg := testConfig("ws://127.0.0.1:1")
	cfg.AllowedDevice = registryPrimary
	g, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for _, v := range []struct {
		mac    string
		status int
	}{{registryPrimary, 200}, {registrySecond, 403}} {
		req := httptest.NewRequest("GET", "/xiaozhi/ota/"+testSecret+"/", nil)
		req.Header.Set("Device-Id", v.mac)
		w := httptest.NewRecorder()
		g.Handler().ServeHTTP(w, req)
		if w.Code != v.status {
			t.Fatal("legacy allowlist changed", w.Code)
		}
	}
}
