package realtimelive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOnboardingFactoryAuthFromProtectedConfig(t *testing.T) {
	var calls atomic.Int32
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/internal/device-auth" {
			t.Error("wrong authority route")
		}
		var in map[string]string
		if json.NewDecoder(r.Body).Decode(&in) != nil || in["mac"] != secondDeviceMAC || in["token"] != secondDeviceToken {
			t.Error("wrong independent device authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"device_id":"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","serial":"synthetic-s3-1","model":"微雪 ESP32-S3"}`))
	}))
	defer authority.Close()
	path := filepath.Join(t.TempDir(), "server.env")
	if err := os.WriteFile(path, []byte("ROBOT_DEVICE_TOKEN="+testDeviceToken+"\nROBOT_ALLOWED_DEVICE="+testDeviceMAC+"\nROBOT_ONBOARDING_AUTH_URL="+authority.URL+"/api/internal/device-auth\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDeviceConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{}, nil, t.TempDir(), "factory-test")
	r := httptest.NewRequest("GET", "/xiaozhi/v1/", nil)
	r.Header.Set("Device-Id", strings.ToUpper(secondDeviceMAC))
	r.Header.Set("Authorization", "Bearer "+secondDeviceToken)
	w := httptest.NewRecorder()
	s.DeviceHandler(cfg).ServeHTTP(w, r)
	// Authentication succeeds, but this deliberately is not a WebSocket upgrade.
	// No model or real audio connection is opened by the test.
	if w.Code != 400 || calls.Load() != 1 {
		t.Fatalf("factory authentication not reached: status=%d calls=%d", w.Code, calls.Load())
	}
}

func TestOnboardingAuthorityCannotBypassLegacyOrBrowserGuards(t *testing.T) {
	var calls atomic.Int32
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"device_id":"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","serial":"s3","model":"M5 ESP32-S3"}`)
	}))
	defer authority.Close()
	cfg, err := (DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}).WithOnboardingAuthURL(authority.URL + "/api/internal/device-auth")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{}, nil, t.TempDir(), "factory-test")
	h := s.DeviceHandler(cfg)
	for _, tc := range []struct {
		mac, auth, origin string
		want              int
	}{
		{testDeviceMAC, "Bearer " + secondDeviceToken, "", 401},
		{secondDeviceMAC, "Bearer short", "", 401},
		{secondDeviceMAC, secondDeviceToken, "", 401},
		{secondDeviceMAC, "Bearer " + secondDeviceToken, "https://robot.songguoxue.com", 403},
	} {
		r := httptest.NewRequest("GET", "/xiaozhi/v1/", nil)
		r.Header.Set("Device-Id", tc.mac)
		r.Header.Set("Authorization", tc.auth)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("guard status %d, want %d", w.Code, tc.want)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("guarded requests reached authority")
	}
}

func TestOnboardingFactoryIdentityOwnsRecordsAcrossReconnects(t *testing.T) {
	const issuedID = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"device_id":%q,"serial":"s3","model":"微雪 ESP32-S3"}`, issuedID)
	}))
	defer authority.Close()
	cfg, err := (DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}).WithOnboardingAuthURL(authority.URL + "/api/internal/device-auth")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{}, nil, t.TempDir(), "factory-test")
	entered := make(chan *Session, 1)
	s.runDevice = func(session *Session) { entered <- session; session.ws.ReadMessage(); session.cancel() }
	h := httptest.NewServer(s.DeviceHandler(cfg))
	defer h.Close()
	var priorSession string
	for i := 0; i < 2; i++ {
		c, response, err := deviceDial(h.URL, secondDeviceMAC, secondDeviceToken)
		if err != nil {
			t.Fatalf("upgrade failed: response=%v error=%v", response, err)
		}
		deviceHello(t, c)
		session := nextSession(t, entered)
		info, err := ReadSessionInfo(session.directory)
		if err != nil || info.DeviceID != issuedID || session.rec.owner.DeviceID != issuedID || info.SessionID == priorSession {
			t.Fatal("factory authority identity was lost or changed")
		}
		priorSession = info.SessionID
		c.Close()
		waitActive(t, s, 0)
	}
	if _, err := os.Stat(filepath.Join(s.root, "devices.json")); !os.IsNotExist(err) {
		t.Fatal("factory auth minted a second legacy identity")
	}
}

func TestOnboardingAuthRejectsUntrustedURLsAndMalformedAuthority(t *testing.T) {
	for _, endpoint := range []string{
		"http://evil.test:18791/api/internal/device-auth", "https://127.0.0.1:18791/api/internal/device-auth",
		"http://127.0.0.1/api/internal/device-auth", "http://127.0.0.1:18791/api/internal/device-auth?x=1",
		"http://127.0.0.1:18791/api/internal/device-auth#x", "http://user@127.0.0.1:18791/api/internal/device-auth",
		"http://127.0.0.1:18791/api/%69nternal/device-auth", "http://127.0.0.1:18791/api/internal/device-auth/",
	} {
		if validOnboardingAuthURL(endpoint) {
			t.Fatalf("untrusted endpoint accepted: %s", endpoint)
		}
	}
	for _, raw := range []string{
		`{"device_id":"forged","serial":"s3","model":"M5 ESP32-S3"}`,
		`{"device_id":"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","device_id":"dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","serial":"s3","model":"M5 ESP32-S3"}`,
		`{"device_id":"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","serial":"../s3","model":"M5 ESP32-S3"}`,
		`{"device_id":"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","serial":"s3","model":"ESP32-C3"}`,
		`{"device_id":"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","serial":"s3","model":"M5 ESP32-S3","extra":"untrusted"}`,
		`{"device_id":"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","serial":"s3","model":"M5 ESP32-S3"} {}`,
	} {
		if _, err := decodeFactoryIdentity([]byte(raw)); err == nil {
			t.Fatal("malformed authority response accepted")
		}
	}
}

func TestOnboardingAuthorityDenialRedirectCancellationAndBounds(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1) }))
	defer target.Close()
	for _, tc := range []struct {
		name string
		want int
	}{{"deny", 403}, {"redirect", 503}, {"oversize", 503}, {"cancel", 503}} {
		t.Run(tc.name, func(t *testing.T) {
			authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch tc.name {
				case "deny":
					w.WriteHeader(401)
				case "redirect":
					http.Redirect(w, r, target.URL, 307)
				case "oversize":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, strings.Repeat("x", 4097))
				case "cancel":
					io.Copy(io.Discard, r.Body)
					<-r.Context().Done()
				}
			}))
			defer authority.Close()
			ctx := context.Background()
			if tc.name == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 40*time.Millisecond)
				defer cancel()
			}
			_, status := newOnboardingAuthority(authority.URL+"/api/internal/device-auth").authenticate(ctx, secondDeviceMAC, secondDeviceToken)
			if status != tc.want {
				t.Fatalf("status=%d want=%d", status, tc.want)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("factory credential followed redirect")
	}
}
