package management

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func runtimeFixture(t *testing.T) (*Server, FactoryBundle, *http.Cookie, *time.Time) {
	s, b, now, _ := onboardingFixture(t)
	cookie := login(t, s, "family", "alice")
	claim := startClaim(t, s, b, cookie, "home-a")
	raw, _ := json.Marshal(map[string]any{"id": claim["id"], "challenge": claim["challenge"]})
	if w := deviceRequest(s, "POST", "/api/device/activation", string(raw), b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	return s, b, cookie, now
}
func TestRuntimeLifecycle(t *testing.T) {
	s, b, c, now := runtimeFixture(t)
	path := "/api/family/device-runtime/" + b.DeviceID
	if w := request(s, "GET", path, "", c); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	report := `{"boot":"0123456789abcdef0123456789abcdef","seq":1,"volume":25,"brightness":null}`
	if w := deviceRequest(s, "POST", "/api/device/state", report, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w := request(s, "PUT", path, `{"field":"volume","value":50,"boot":"0123456789abcdef0123456789abcdef"}`, c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	cmd := out["command"].(map[string]any)
	report = `{"boot":"0123456789abcdef0123456789abcdef","seq":2,"volume":50,"brightness":null,"ack":{"id":"` + cmd["id"].(string) + `","status":"applied","actual":50}}`
	if w = deviceRequest(s, "POST", "/api/device/state", report, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = request(s, "GET", path, "", c)
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["command"].(map[string]any)["status"] != "confirmed" {
		t.Fatal(out)
	}
	*now = now.Add(21 * time.Second)
	if w = deviceRequest(s, "POST", "/api/device/state", report, b); w.Code != 409 {
		t.Fatal("duplicate", w.Code)
	}
	w = request(s, "GET", path, "", c)
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["connection"] != "offline" || out["volume"] != nil {
		t.Fatal(out)
	}
	restarted, e := NewServer(s.cfg, s.opts)
	if e != nil {
		t.Fatal(e)
	}
	c = login(t, restarted, "family", "alice")
	w = request(restarted, "GET", path, "", c)
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["connection"] != "unknown" || out["command"] != nil {
		t.Fatal(out)
	}
}
func TestRuntimeRejectsInvalidStateAndRetiredBoot(t *testing.T) {
	s, b, _, now := runtimeFixture(t)
	for _, body := range []string{
		`{"boot":"0123456789abcdef0123456789abcdef","seq":1,"volume":25,"brightness":null,"Volume":25}`,
		`{"boot":"0123456789abcdef0123456789abcdef","seq":1,"seq":2,"volume":25,"brightness":null}`,
		`{"boot":"0123456789abcdef0123456789abcdef","seq":1.5,"volume":25,"brightness":null}`,
		`{"boot":"0123456789abcdef0123456789abcdef","seq":null,"volume":25,"brightness":null}`,
		`{"boot":"0123456789abcdef0123456789abcdef","seq":1,"volume":25,"brightness":null,"ack":{"id":"cmd_0123456789abcdef0123456789abcdef","status":"applied","Actual":25}}`,
	} {
		if w := deviceRequest(s, "POST", "/api/device/state", body, b); w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	body := `{"boot":"0123456789abcdef0123456789abcdef","seq":2,"volume":25,"brightness":null}`
	if w := deviceRequest(s, "POST", "/api/device/state", body, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	*now = now.Add(time.Second)
	if w := deviceRequest(s, "POST", "/api/device/state", `{"boot":"abcdef0123456789abcdef0123456789","seq":1,"volume":25,"brightness":null}`, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := deviceRequest(s, "POST", "/api/device/state", body, b); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestRuntimeCommandOutcomes(t *testing.T) {
	for _, scenario := range []string{"mismatch", "failed", "expired", "reboot", "revoke", "null"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, c, now := runtimeFixture(t)
			path := "/api/family/device-runtime/" + b.DeviceID
			boot := "0123456789abcdef0123456789abcdef"
			report := func(seq int, volume any, boot string, ack any) {
				raw, _ := json.Marshal(map[string]any{"boot": boot, "seq": seq, "volume": volume, "brightness": nil, "ack": ack})
				if ack == nil || stringMustJSON(ack) == "null" {
					var fields map[string]any
					json.Unmarshal(raw, &fields)
					delete(fields, "ack")
					raw, _ = json.Marshal(fields)
				}
				if w := deviceRequest(s, "POST", "/api/device/state", string(raw), b); w.Code != 200 {
					t.Fatal(w.Code, w.Body)
				}
			}
			report(1, 25, boot, nil)
			if w := request(s, "PUT", path, `{"field":"brightness","value":50,"boot":"`+boot+`"}`, c); w.Code != 409 {
				t.Fatal(w.Code)
			}
			w := request(s, "PUT", path, `{"field":"volume","value":50,"boot":"`+boot+`"}`, c)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			var out map[string]any
			json.Unmarshal(w.Body.Bytes(), &out)
			id := out["command"].(map[string]any)["id"]
			ack := map[string]any{"id": id, "status": "applied", "actual": 50}
			value := any(50)
			expected := "failed"
			switch scenario {
			case "mismatch":
				value = 49
			case "failed":
				ack["status"] = "failed"
			case "expired":
				*now = now.Add(46 * time.Second)
				expected = "unknown"
			case "reboot":
				boot = "abcdef0123456789abcdef0123456789"
				expected = "unknown"
			case "revoke":
				if e := (ConfigStore{s.opts.ConfigPath}).Transaction(func(cfg *Config) error { cfg.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); e != nil {
					t.Fatal(e)
				}
				expected = "unknown"
			case "null":
				value = nil
				ack = nil
				expected = "unknown"
			}
			report(2, value, boot, ack)
			w = request(s, "GET", path, "", c)
			json.Unmarshal(w.Body.Bytes(), &out)
			if out["command"].(map[string]any)["status"] != expected {
				t.Fatal(out)
			}
		})
	}
}

func stringMustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestRuntimeWeChatPreciseWriteOrigin(t *testing.T) {
	s, _, _, _ := runtimeFixture(t)
	s.opts.PublicOrigin = onboardingOrigin
	for _, resource := range []string{"device-runtime", "device-settings"} {
		r := httptest.NewRequest("PUT", "https://robot.songguoxue.com/api/family/"+resource+"/dev_0123456789abcdef0123456789abcdef", nil)
		r.RemoteAddr = "127.0.0.1:31000"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Referer", "https://servicewechat.com/"+onboardingMiniProgramID+"/devtools/page-frame.html")
		if !s.checkOrigin(r) {
			t.Fatal("valid WeChat write rejected", resource)
		}
		for _, bad := range []string{"https://servicewechat.com/wxbad/devtools/page-frame.html", "https://evil.example/" + onboardingMiniProgramID + "/devtools/page-frame.html"} {
			r.Header.Set("Referer", bad)
			if s.checkOrigin(r) {
				t.Fatal("bad referer accepted")
			}
		}
		r.Header.Set("Referer", "https://servicewechat.com/"+onboardingMiniProgramID+"/devtools/page-frame.html")
		r.URL.Path += "/extra"
		if s.checkOrigin(r) {
			t.Fatal("extra path accepted")
		}
		r.URL.Path = strings.TrimSuffix(r.URL.Path, "/extra")
		r.Header.Set("Origin", "https://evil.example")
		if s.checkOrigin(r) {
			t.Fatal("evil origin accepted")
		}
	}
}
func TestRuntimeAuditFailureAndSameCommandRetry(t *testing.T) {
	s, b, c, _ := runtimeFixture(t)
	path := "/api/family/device-runtime/" + b.DeviceID
	for i := 1; i <= 3; i++ {
		body := fmt.Sprintf(`{"boot":"0123456789abcdef0123456789abcdef","seq":%d,"volume":25,"brightness":null}`, i)
		if w := deviceRequest(s, "POST", "/api/device/state", body, b); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	old := s.auditWrite
	s.auditWrite = func(string, auditEntry) error { return ErrSource }
	if w := request(s, "PUT", path, `{"field":"volume","value":50,"boot":"0123456789abcdef0123456789abcdef"}`, c); w.Code != 503 {
		t.Fatal(w.Code)
	}
	w := request(s, "GET", path, "", c)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["command"] != nil {
		t.Fatal("audit failed but queued", out)
	}
	s.auditWrite = old
	w = request(s, "PUT", path, `{"field":"volume","value":50,"boot":"0123456789abcdef0123456789abcdef"}`, c)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	id := out["command"].(map[string]any)["id"]
	for i := 4; i <= 5; i++ {
		body := fmt.Sprintf(`{"boot":"0123456789abcdef0123456789abcdef","seq":%d,"volume":25,"brightness":null}`, i)
		w = deviceRequest(s, "POST", "/api/device/state", body, b)
		json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != 200 || out["command"].(map[string]any)["id"] != id {
			t.Fatal(w.Code, out)
		}
	}
}

// Hold the runtime gate until the request has finished all pre-lock config reads.
// The stack barrier avoids relying on scheduler delays to reproduce revocation.
func TestRuntimeRechecksAuthorityAfterWaitingForRuntimeLock(t *testing.T) {
	for _, change := range []string{"actor", "binding", "factory"} {
		t.Run(change, func(t *testing.T) {
			s, b, c, _ := runtimeFixture(t)
			body := `{"boot":"0123456789abcdef0123456789abcdef","seq":1,"volume":25,"brightness":null}`
			if w := deviceRequest(s, "POST", "/api/device/state", body, b); w.Code != 200 {
				t.Fatal(w.Code)
			}
			if w := request(s, "PUT", "/api/family/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"0123456789abcdef0123456789abcdef"}`, c); w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			s.runtimeMu.Lock()
			locked := true
			defer func() {
				if locked {
					s.runtimeMu.Unlock()
				}
			}()
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				result <- deviceRequest(s, "POST", "/api/device/state", strings.Replace(body, `"seq":1`, `"seq":2`, 1), b)
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				waiting := false
				for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
					if strings.Contains(stack, "(*Server).deviceState") && strings.Contains(stack, "sync.(*Mutex).lockSlow") {
						waiting = true
						break
					}
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("state did not reach runtime lock")
				}
				runtime.Gosched()
			}
			if err := (ConfigStore{s.opts.ConfigPath}).Transaction(func(cfg *Config) error {
				switch change {
				case "actor":
					cfg.Accounts[0].Memberships[0].CanAddDevices = false
				case "binding":
					for i := range cfg.Bindings {
						if cfg.Bindings[i].DeviceID == b.DeviceID {
							cfg.Bindings[i].Version++
						}
					}
				case "factory":
					for i := range cfg.FactoryDevices {
						if cfg.FactoryDevices[i].DeviceID == b.DeviceID {
							cfg.FactoryDevices[i].KeyDigest = strings.Repeat("a", 64)
						}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s.runtimeMu.Unlock()
			locked = false
			select {
			case w := <-result:
				if change == "factory" {
					if w.Code != 401 {
						t.Fatalf("revoked factory accepted: %d %s", w.Code, w.Body)
					}
				} else {
					var out map[string]any
					json.Unmarshal(w.Body.Bytes(), &out)
					if w.Code != 200 || out["command"] != nil {
						t.Fatalf("revoked authority dispatched command: %d %s", w.Code, w.Body)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("state deadlocked")
			}
		})
	}
}
