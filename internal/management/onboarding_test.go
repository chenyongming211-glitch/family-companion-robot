package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOnboardingRequiresExplicitFamilyGrant(t *testing.T) {
	s, _ := httpFixture(t)
	c := login(t, s, "family", "alice")
	w := request(s, "POST", "/api/family/onboarding", `{"family_id":"home-a","claim_code":"0123456789abcdef0123456789abcdef"}`, c)
	if w.Code != 403 {
		t.Fatalf("expected denied family grant 403, got %d %s", w.Code, w.Body)
	}
}

func onboardingFixture(t *testing.T) (*Server, FactoryBundle, *time.Time, string) {
	t.Helper()
	s, now := httpFixture(t)
	c := s.cfg
	c.Accounts[0].Memberships[0].CanAddDevices = true
	c.Accounts[1].Memberships[0].CanAddDevices = true
	p := filepath.Join(t.TempDir(), "config.json")
	if e := WritePrivateConfig(p, c); e != nil {
		t.Fatal(e)
	}
	s.opts.ConfigPath = p
	b, e := ProvisionFactory(p, "02:00:00:00:00:01", "UNIT001", "微雪 ESP32-S3", nil)
	if e != nil {
		t.Fatal(e)
	}
	return s, b, now, p
}
func deviceRequest(s *Server, method, path, body string, b FactoryBundle) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost:18791"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:31000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+b.DeviceKey)
	r.Header.Set("Device-Id", b.MAC)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func startClaim(t *testing.T, s *Server, b FactoryBundle, c *http.Cookie, family string) map[string]any {
	t.Helper()
	w := request(s, "POST", "/api/family/onboarding", `{"family_id":"`+family+`","claim_code":"`+b.ClaimCode+`","name":"新机器人"}`, c)
	if w.Code != 200 {
		t.Fatalf("claim %d %s", w.Code, w.Body)
	}
	var v map[string]any
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal("json")
	}
	return v
}
func TestOnboardingDurableActivationIsolationAndFreshSnapshots(t *testing.T) {
	s, b, now, p := onboardingFixture(t)
	a := login(t, s, "family", "alice")
	other := login(t, s, "family", "bob")
	before, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	legacy, _ := json.Marshal(before.Bindings)
	pending := startClaim(t, s, b, a, "home-a")
	again := startClaim(t, s, b, a, "home-a")
	if !reflect.DeepEqual(pending, again) {
		t.Fatal("retry changes task")
	}
	if w := request(s, "GET", "/api/family/devices/"+b.DeviceID, "", a); w.Code != 404 {
		t.Fatal("premature grant", w.Code)
	}
	if w := deviceRequest(s, "GET", "/api/device/bootstrap", "", b); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	id := pending["id"].(string)
	challenge := pending["challenge"].(string)
	payload := `{"id":"` + id + `","challenge":"` + challenge + `"}`
	for _, method := range []string{"GET", "DELETE"} {
		if w := request(s, method, "/api/family/onboarding/"+id, "", other); w.Code != 404 {
			t.Fatal("other task access", w.Code)
		}
	}
	wrong := b
	wrong.DeviceKey = strings.Repeat("a", 43)
	if w := deviceRequest(s, "POST", "/api/device/activation", payload, wrong); w.Code != 401 {
		t.Fatal("wrong key", w.Code)
	}
	*now = now.Add(17 * time.Second)
	w := deviceRequest(s, "POST", "/api/device/activation", payload, b)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	persisted, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(persisted.Bindings) != len(before.Bindings)+1 || !persisted.Bindings[len(before.Bindings)].From.Equal(*now) {
		t.Fatal("actual binding time")
	}
	after, _ := json.Marshal(persisted.Bindings[:len(before.Bindings)])
	if string(legacy) != string(after) {
		t.Fatal("legacy bindings changed")
	}
	if _, e = persisted.Authorize("alice", "family", b.DeviceID, *now, true); e != nil {
		t.Fatal("no body grant", e)
	}
	if _, e = persisted.Authorize("bob", "family", b.DeviceID, *now, false); e == nil {
		t.Fatal("cross family grant")
	}
	if w = request(s, "GET", "/api/family/devices/"+b.DeviceID, "", a); w.Code != 200 {
		t.Fatal("stale snapshot", w.Code, w.Body)
	}
	*now = now.Add(10 * time.Minute)
	if w = deviceRequest(s, "POST", "/api/device/activation", payload, b); w.Code != 200 {
		t.Fatal("confirmed retry", w.Code, w.Body)
	}
	if w = deviceRequest(s, "GET", "/api/device/bootstrap", "", b); w.Code != 200 || strings.Contains(w.Body.String(), b.DeviceKey) {
		t.Fatal("bootstrap", w.Code)
	}
	reboot, e := NewServer(persisted, s.opts)
	if e != nil {
		t.Fatal(e)
	}
	relogin := login(t, reboot, "family", "alice")
	if w = request(reboot, "GET", "/api/family/onboarding/"+id, "", relogin); w.Code != 200 || !strings.Contains(w.Body.String(), "confirmed") || strings.Contains(w.Body.String(), challenge) {
		t.Fatal("restart status", w.Code, w.Body)
	}
	if strings.Contains(string(mustRead(t, p)), b.ClaimCode) || strings.Contains(string(mustRead(t, p)), b.DeviceKey) {
		t.Fatal("raw secret persisted")
	}
}
func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestOnboardingCompetingFamiliesCancelExpiryAndReplay(t *testing.T) {
	for _, mode := range []string{"cancel", "expire", "race"} {
		t.Run(mode, func(t *testing.T) {
			s, b, now, p := onboardingFixture(t)
			a := login(t, s, "family", "alice")
			other := login(t, s, "family", "bob")
			pending := startClaim(t, s, b, a, "home-a")
			id := pending["id"].(string)
			body := `{"id":"` + id + `","challenge":"` + pending["challenge"].(string) + `"}`
			if w := request(s, "POST", "/api/family/onboarding", `{"family_id":"home-b","claim_code":"`+b.ClaimCode+`"}`, other); w.Code != 409 {
				t.Fatal("steal", w.Code)
			}
			switch mode {
			case "cancel":
				if request(s, "DELETE", "/api/family/onboarding/"+id, "", a).Code != 200 {
					t.Fatal("cancel")
				}
				if w := deviceRequest(s, "POST", "/api/device/activation", body, b); w.Code != 409 {
					t.Fatal(w.Code)
				}
			case "expire":
				*now = now.Add(181 * time.Second)
				if w := deviceRequest(s, "POST", "/api/device/activation", body, b); w.Code != 410 {
					t.Fatal(w.Code)
				}
			case "race":
				var wg sync.WaitGroup
				wg.Add(2)
				codes := make(chan int, 2)
				go func() { defer wg.Done(); codes <- deviceRequest(s, "POST", "/api/device/activation", body, b).Code }()
				go func() { defer wg.Done(); codes <- request(s, "DELETE", "/api/family/onboarding/"+id, "", a).Code }()
				wg.Wait()
				close(codes)
				for code := range codes {
					if code != 200 && code != 409 {
						t.Fatal(code)
					}
				}
			}
			c, e := LoadConfig(p)
			if e != nil {
				t.Fatal(e)
			}
			if len(c.Bindings) > 3 {
				t.Fatal("duplicate binding")
			}
			if mode != "race" {
				next := startClaim(t, s, b, other, "home-b")
				if next["id"] == id {
					t.Fatal("reused cancelled task")
				}
				if w := deviceRequest(s, "POST", "/api/device/activation", body, b); w.Code == 200 {
					t.Fatal("old replay")
				}
			}
		})
	}
}
func TestOnboardingTwoServerInstancesOneWinner(t *testing.T) {
	s, b, _, p := onboardingFixture(t)
	c, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	second, e := NewServer(c, s.opts)
	if e != nil {
		t.Fatal(e)
	}
	a := login(t, s, "family", "alice")
	other := login(t, second, "family", "bob")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, v := range []struct {
		s      *Server
		c      *http.Cookie
		family string
	}{{s, a, "home-a"}, {second, other, "home-b"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(v.s, "POST", "/api/family/onboarding", `{"family_id":"`+v.family+`","claim_code":"`+b.ClaimCode+`"}`, v.c)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}
func TestOnboardingWrongDeviceChallengeScopeAndWriteFailure(t *testing.T) {
	s, b, _, p := onboardingFixture(t)
	a := login(t, s, "family", "alice")
	v := startClaim(t, s, b, a, "home-a")
	id := v["id"].(string)
	challenge := v["challenge"].(string)
	b2, e := ProvisionFactory(p, "02:00:00:00:00:02", "UNIT002", "M5 ESP32-S3", nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		b         FactoryBundle
		challenge string
	}{{b2, challenge}, {b, strings.Repeat("0", 64)}} {
		w := deviceRequest(s, "POST", "/api/device/activation", `{"id":"`+id+`","challenge":"`+tt.challenge+`"}`, tt.b)
		if w.Code != 401 {
			t.Fatal(w.Code, w.Body)
		}
	}
	// Allow the existing lock and config to be opened, but reject the actual
	// atomic replacement tempfile after the transaction has validated activation.
	if e = os.Chmod(filepath.Dir(p), 0500); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(p), 0700) })
	w := deviceRequest(s, "POST", "/api/device/activation", `{"id":"`+id+`","challenge":"`+challenge+`"}`, b)
	if w.Code != 503 {
		t.Fatal("write failure", w.Code)
	}
	c, e := LoadConfig(p)
	if e != nil || len(c.Bindings) != 2 {
		t.Fatal("write failure published")
	}
}
func TestOnboardingLoopbackAuthPublicOriginAndCookies(t *testing.T) {
	s, b, _, _ := onboardingFixture(t)
	a := login(t, s, "family", "alice")
	v := startClaim(t, s, b, a, "home-a")
	if w := deviceRequest(s, "POST", "/api/device/activation", `{"id":"`+v["id"].(string)+`","challenge":"`+v["challenge"].(string)+`"}`, b); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, remote := range []string{"127.0.0.1:1234", "203.0.113.1:1234"} {
		r := httptest.NewRequest("POST", "http://localhost:18791/api/internal/device-auth", strings.NewReader(`{"mac":"`+b.MAC+`","token":"`+b.DeviceKey+`"}`))
		r.RemoteAddr = remote
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := 404
		if strings.HasPrefix(remote, "127.") {
			want = 200
		}
		if w.Code != want {
			t.Fatal(remote, w.Code, w.Body)
		}
	}
	s.opts.PublicOrigin = onboardingOrigin
	for _, remote := range []string{"127.0.0.1:1234", "203.0.113.1:1234"} {
		r := httptest.NewRequest("POST", onboardingOrigin+"/api/family/login", strings.NewReader(`{"username":"alice","password":"synthetic-password-123"}`))
		r.RemoteAddr = remote
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Origin", onboardingOrigin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := 403
		if strings.HasPrefix(remote, "127.") {
			want = 200
		}
		if w.Code != want {
			t.Fatal(remote, w.Code, w.Body)
		}
		if want == 200 && !w.Result().Cookies()[0].Secure {
			t.Fatal("insecure public cookie")
		}
	}
}

func TestOnboardingRejectsBareDeviceKeyAndRevokedFamilyPermission(t *testing.T) {
	s, b, _, p := onboardingFixture(t)
	a := login(t, s, "family", "alice")
	v := startClaim(t, s, b, a, "home-a")
	r := httptest.NewRequest("GET", "http://localhost:18791/api/device/bootstrap", nil)
	r.Header.Set("Authorization", b.DeviceKey)
	r.Header.Set("Device-Id", b.MAC)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("bare key authorization accepted: %d", w.Code)
	}
	if e := (ConfigStore{p}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); e != nil {
		t.Fatal(e)
	}
	id := v["id"].(string)
	if w := request(s, "GET", "/api/family/onboarding/"+id, "", a); w.Code != 403 {
		t.Fatal("revoked status permission", w.Code)
	}
	if w := deviceRequest(s, "POST", "/api/device/activation", `{"id":"`+id+`","challenge":"`+v["challenge"].(string)+`"}`, b); w.Code != 403 {
		t.Fatal("revoked permission activation", w.Code)
	}
}

func TestPublicLoginLimitUsesTrustedProxyClientIP(t *testing.T) {
	s, _, _, _ := onboardingFixture(t)
	s.opts.PublicOrigin = onboardingOrigin
	attempt := func(ip string) int {
		r := httptest.NewRequest("POST", onboardingOrigin+"/api/family/login", strings.NewReader(`{"username":"absent","password":"bad"}`))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 10; i++ {
		if code := attempt("203.0.113.1"); code != 401 {
			t.Fatal(i, code)
		}
	}
	if code := attempt("203.0.113.1"); code != 429 {
		t.Fatal("limit", code)
	}
	if code := attempt("203.0.113.2"); code != 401 {
		t.Fatalf("independent public client rate limited: %d", code)
	}
}
func TestPrivateFactoryStateRejectsBrokenConfirmedBinding(t *testing.T) {
	s, b, _, p := onboardingFixture(t)
	a := login(t, s, "family", "alice")
	v := startClaim(t, s, b, a, "home-a")
	if w := deviceRequest(s, "POST", "/api/device/activation", `{"id":"`+v["id"].(string)+`","challenge":"`+v["challenge"].(string)+`"}`, b); w.Code != 200 {
		t.Fatal(w.Code)
	}
	c, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	wrong := c.OnboardingClaims[0].BindingFrom.Add(-time.Second)
	c.OnboardingClaims[0].BindingFrom = &wrong
	if e = ValidateConfig(c); e == nil {
		t.Fatal("confirmed claim without exact binding accepted")
	}
}

func TestConfigStoreProcessWorker(t *testing.T) {
	if os.Getenv("ONBOARDING_SYNTHETIC_PROCESS") == "" {
		return
	}
	_, e := ProvisionFactory(os.Getenv("ONBOARDING_SYNTHETIC_PATH"), "02:00:00:00:00:30", "PROCESS-UNIT", "M5 ESP32-S3", nil)
	if e != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
func TestConfigStoreCrossProcessProvisionOneWinner(t *testing.T) {
	_, _, _, p := onboardingFixture(t)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	commands := []*exec.Cmd{exec.Command(exe, "-test.run=^TestConfigStoreProcessWorker$"), exec.Command(exe, "-test.run=^TestConfigStoreProcessWorker$")}
	for _, cmd := range commands {
		cmd.Env = append(os.Environ(), "ONBOARDING_SYNTHETIC_PROCESS=1", "ONBOARDING_SYNTHETIC_PATH="+p)
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
	}
	success, rejected := 0, 0
	for _, cmd := range commands {
		e = cmd.Wait()
		if e == nil {
			success++
		} else if status, ok := e.(*exec.ExitError); ok && status.ExitCode() == 3 {
			rejected++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatal(success, rejected)
	}
	c, e := LoadConfig(p)
	if e != nil || len(c.FactoryDevices) != 2 {
		t.Fatal("lost/duplicate write", e)
	}
}
