package management

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func migrationFixture(t *testing.T) (*Server, FactoryBundle, *time.Time, string) {
	t.Helper()
	s, now := httpFixture(t)
	c := s.cfg
	c.Accounts[0].Memberships[0].CanAddDevices = true
	c.Accounts[1].Memberships[0].CanAddDevices = true
	b := FactoryBundle{1, c.Devices[0].ID, "02:00:00:00:00:01", "MIGRATE1", c.Devices[0].Model, strings.Repeat("a", 43), strings.Repeat("b", 32), onboardingOrigin + "/api/device/bootstrap", "wss://robot.songguoxue.com/xiaozhi/v1/"}
	raw, _ := json.Marshal(map[string]any{"device_id": b.DeviceID, "mac": b.MAC, "serial": b.Serial, "model": b.Model, "key_digest": secretDigest(b.DeviceKey), "claim_digest": secretDigest(b.ClaimCode), "migration_binding_id": c.Bindings[0].ID})
	var d FactoryDevice
	_ = json.Unmarshal(raw, &d)
	c.FactoryDevices = []FactoryDevice{d}
	p := filepath.Join(t.TempDir(), "config.json")
	if e := WritePrivateConfig(p, c); e != nil {
		t.Fatal("valid migration preparation rejected", e)
	}
	s.opts.ConfigPath = p
	return s, b, now, p
}
func activationBody(pending map[string]any) string {
	raw, _ := json.Marshal(map[string]any{"id": pending["id"], "challenge": pending["challenge"]})
	return string(raw)
}
func TestMigrationConfirmationPreservesOriginalRecordsAndPermissions(t *testing.T) {
	s, b, now, p := migrationFixture(t)
	before, _ := LoadConfig(p)
	a := login(t, s, "family", "alice")
	pending := startClaim(t, s, b, a, "home-a")
	again := startClaim(t, s, b, a, "home-a")
	if !reflect.DeepEqual(pending, again) {
		t.Fatal("retry changed waiting task")
	}
	if w := deviceRequest(s, "GET", "/api/device/bootstrap", "", b); w.Code != 409 {
		t.Fatal("premature admission", w.Code)
	}
	*now = now.Add(time.Minute)
	for i := 0; i < 2; i++ {
		w := deviceRequest(s, "POST", "/api/device/activation", activationBody(pending), b)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		if v["binding_from"] != before.Bindings[0].From.Format(time.RFC3339Nano) {
			t.Fatal("binding origin changed", v)
		}
	}
	after, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before.Devices, after.Devices) || !reflect.DeepEqual(before.Bindings, after.Bindings) || !reflect.DeepEqual(before.Accounts, after.Accounts) {
		t.Fatal("original authority modified")
	}
	if w := deviceRequest(s, "GET", "/api/device/bootstrap", "", b); w.Code != 200 {
		t.Fatal("confirmed admission", w.Code)
	}
	if _, e := after.Authorize("alice", "family", b.DeviceID, before.Bindings[0].From.Add(-time.Nanosecond), true); e == nil {
		t.Fatal("prebinding records exposed")
	}
}
func TestMigrationRejectsOtherFamilyAndMissingDeviceGrant(t *testing.T) {
	s, b, _, p := migrationFixture(t)
	bob := login(t, s, "family", "bob")
	if w := request(s, "POST", "/api/family/onboarding", `{"family_id":"home-b","claim_code":"`+b.ClaimCode+`"}`, bob); w.Code != 403 {
		t.Fatal("cross-family", w.Code, w.Body)
	}
	if e := (ConfigStore{p}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].Devices = nil; return nil }); e != nil {
		t.Fatal(e)
	}
	a := login(t, s, "family", "alice")
	if w := request(s, "POST", "/api/family/onboarding", `{"family_id":"home-a","claim_code":"`+b.ClaimCode+`"}`, a); w.Code != 403 {
		t.Fatal("no existing device access", w.Code, w.Body)
	}
}
func TestMigrationRechecksPermissionAndBindingAtPostAndActivation(t *testing.T) {
	for _, kind := range []string{"permission", "grant", "ended", "rebound"} {
		t.Run(kind, func(t *testing.T) {
			s, b, now, p := migrationFixture(t)
			a := login(t, s, "family", "alice")
			pending := startClaim(t, s, b, a, "home-a")
			if e := (ConfigStore{p}).Transaction(func(c *Config) error {
				switch kind {
				case "permission":
					c.Accounts[0].Memberships[0].CanAddDevices = false
				case "grant":
					c.Accounts[0].Memberships[0].Devices = nil
				case "ended", "rebound":
					v := now.Add(-time.Second)
					c.Bindings[0].Until = &v
					if kind == "rebound" {
						c.Bindings = append(c.Bindings, Binding{ID: "rebound", Version: 2, FamilyID: "home-b", DeviceID: b.DeviceID, From: v})
					}
				}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			if w := request(s, "POST", "/api/family/onboarding", `{"family_id":"home-a","claim_code":"`+b.ClaimCode+`"}`, a); w.Code == 200 {
				t.Fatal("stale POST accepted")
			}
			if w := deviceRequest(s, "POST", "/api/device/activation", activationBody(pending), b); w.Code == 200 {
				t.Fatal("stale activation accepted")
			}
			c, e := LoadConfig(p)
			if e != nil || c.OnboardingClaims[0].Status != "waiting" {
				t.Fatal("unauthorized activation mutated state", e)
			}
		})
	}
}
func TestMigrationCancelledExpiredAndConcurrentRetry(t *testing.T) {
	for _, kind := range []string{"cancelled", "expired", "concurrent"} {
		t.Run(kind, func(t *testing.T) {
			s, b, now, p := migrationFixture(t)
			a := login(t, s, "family", "alice")
			pending := startClaim(t, s, b, a, "home-a")
			if kind == "cancelled" {
				if w := request(s, http.MethodDelete, "/api/family/onboarding/"+pending["id"].(string), "", a); w.Code != 200 {
					t.Fatal(w.Code)
				}
			}
			if kind == "expired" {
				*now = now.Add(181 * time.Second)
			}
			if kind != "concurrent" {
				if w := deviceRequest(s, "POST", "/api/device/activation", activationBody(pending), b); w.Code == 200 {
					t.Fatal("stale confirmation accepted")
				}
				fresh := startClaim(t, s, b, a, "home-a")
				if fresh["id"] == pending["id"] {
					t.Fatal("stale task reused")
				}
				pending = fresh
			}
			var wg sync.WaitGroup
			codes := make(chan int, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					codes <- deviceRequest(s, "POST", "/api/device/activation", activationBody(pending), b).Code
				}()
			}
			wg.Wait()
			close(codes)
			for code := range codes {
				if code != 200 {
					t.Fatal("concurrent retry", code)
				}
			}
			c, e := LoadConfig(p)
			if e != nil || len(c.Devices) != 2 || len(c.Bindings) != 2 {
				t.Fatal("duplicate authority", e)
			}
		})
	}
}
func TestMigrationConfigRejectsWrongReference(t *testing.T) {
	_, _, _, p := migrationFixture(t)
	raw, _ := os.ReadFile(p)
	for _, replacement := range []string{"missing", "bind-b"} {
		bad := strings.Replace(string(raw), `"migration_binding_id":"bind-a"`, `"migration_binding_id":"`+replacement+`"`, 1)
		path := p + replacement
		_ = os.WriteFile(path, []byte(bad), 0600)
		if _, e := LoadConfig(path); e == nil {
			t.Fatal("bad migration reference accepted")
		}
	}
}
func TestMigrationConfirmedRetryRechecksExistingDeviceAccess(t *testing.T) {
	s, b, _, p := migrationFixture(t)
	a := login(t, s, "family", "alice")
	pending := startClaim(t, s, b, a, "home-a")
	if w := deviceRequest(s, "POST", "/api/device/activation", activationBody(pending), b); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if e := (ConfigStore{p}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].Devices = nil; return nil }); e != nil {
		t.Fatal(e)
	}
	if w := request(s, "POST", "/api/family/onboarding", `{"family_id":"home-a","claim_code":"`+b.ClaimCode+`"}`, a); w.Code != 403 {
		t.Fatal("confirmed POST bypassed access check", w.Code)
	}
	if w := deviceRequest(s, "POST", "/api/device/activation", activationBody(pending), b); w.Code != 403 {
		t.Fatal("confirmed activation bypassed access check", w.Code)
	}
}
func TestMigrationConfigRejectsClaimForDifferentOriginalFamily(t *testing.T) {
	s, b, _, p := migrationFixture(t)
	a := login(t, s, "family", "alice")
	_ = startClaim(t, s, b, a, "home-a")
	c, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	c.OnboardingClaims[0].ActorID = "bob"
	c.OnboardingClaims[0].FamilyID = "home-b"
	if e := ValidateConfig(c); e == nil {
		t.Fatal("cross-family migration claim accepted in authority config")
	}
}
