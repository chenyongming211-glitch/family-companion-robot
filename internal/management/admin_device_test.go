package management

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const adminTestBoot = "0123456789abcdef0123456789abcdef"

func adminRuntimeReport(t *testing.T, s *Server, b FactoryBundle, seq int, ack string) map[string]any {
	t.Helper()
	extra := ""
	if ack != "" {
		extra = `,"ack":{"id":"` + ack + `","status":"applied","actual":50}`
	}
	value := 25
	if ack != "" {
		value = 50
	}
	w := deviceRequest(s, "POST", "/api/device/state", fmt.Sprintf(`{"boot":"%s","seq":%d,"volume":%d,"brightness":40%s}`, adminTestBoot, seq, value, extra), b)
	if w.Code != 200 {
		t.Fatalf("heartbeat %d %s", w.Code, w.Body)
	}
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func adminRuntimeQueue(t *testing.T, s *Server, b FactoryBundle, cookie *http.Cookie) string {
	t.Helper()
	w := request(s, "PUT", "/api/admin/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+adminTestBoot+`"}`, cookie)
	if w.Code != 200 {
		t.Fatalf("admin queue %d %s", w.Code, w.Body)
	}
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	cmd, ok := out["command"].(map[string]any)
	if !ok {
		t.Fatal(out)
	}
	for _, key := range []string{"scope", "actor", "binding", "boot"} {
		if _, ok := cmd[key]; ok {
			t.Fatal("private command field leaked", key)
		}
	}
	return cmd["id"].(string)
}
func TestAdminRuntimeLifecycleAndAuditScope(t *testing.T) {
	s, b, family, _ := runtimeFixture(t)
	admin := login(t, s, "admin", "alice")
	path := "/api/admin/device-runtime/" + b.DeviceID
	if w := request(s, "GET", path, "", family); w.Code != 401 {
		t.Fatal("family cookie", w.Code)
	}
	w := request(s, "GET", path, "", admin)
	if w.Code != 200 {
		t.Fatalf("admin read %d %s", w.Code, w.Body)
	}
	adminRuntimeReport(t, s, b, 1, "")
	id := adminRuntimeQueue(t, s, b, admin)
	if got := adminRuntimeReport(t, s, b, 2, "")["command"].(map[string]any)["id"]; got != id {
		t.Fatal(got)
	}
	adminRuntimeReport(t, s, b, 3, id)
	w = request(s, "GET", path, "", admin)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out["command"].(map[string]any)["status"] != "confirmed" {
		t.Fatal(w.Code, out)
	}
	raw, e := os.ReadFile(s.opts.AuditPath)
	if e != nil || !strings.Contains(string(raw), `"scope":"admin"`) {
		t.Fatal("audit scope", e, string(raw))
	}
	if w := request(s, "PUT", "/api/admin/device-settings/"+b.DeviceID, `{}`, admin); w.Code == 200 {
		t.Fatal("unrelated settings write expanded")
	}
}
func TestAdminRuntimeFreshPermissionsAndCommandRevalidation(t *testing.T) {
	for _, kind := range []string{"role", "grant", "manage", "binding"} {
		for _, stage := range []string{"before-request", "before-pull", "before-ack"} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				s, b, _, _ := runtimeFixture(t)
				admin := login(t, s, "admin", "alice")
				adminRuntimeReport(t, s, b, 1, "")
				id := ""
				if stage != "before-request" {
					id = adminRuntimeQueue(t, s, b, admin)
				}
				if stage == "before-ack" {
					adminRuntimeReport(t, s, b, 2, "")
				}
				e := (ConfigStore{s.opts.ConfigPath}).Transaction(func(c *Config) error {
					switch kind {
					case "role":
						c.Accounts[0].InternalAdmin = false
					case "grant":
						for i := range c.Accounts[0].Memberships {
							c.Accounts[0].Memberships[i].Devices = nil
						}
					case "manage":
						c.Accounts[0].Memberships[0].CanAddDevices = false
					case "binding":
						for i := range c.Bindings {
							if c.Bindings[i].DeviceID == b.DeviceID {
								c.Bindings[i].Version++
							}
						}
					}
					return nil
				})
				if e != nil {
					t.Fatal(e)
				}
				if stage == "before-request" {
					w := request(s, "PUT", "/api/admin/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+adminTestBoot+`"}`, admin)
					want := 403
					if kind == "role" {
						want = 401
					}
					if kind == "binding" {
						want = 200
					}
					if w.Code != want {
						t.Fatalf("deny %d want %d %s", w.Code, want, w.Body)
					}
					return
				}
				seq := 2
				if stage == "before-ack" {
					seq = 3
				}
				ack := ""
				if stage == "before-ack" {
					ack = id
				}
				out := adminRuntimeReport(t, s, b, seq, ack)
				if out["command"] != nil {
					t.Fatal("revoked command dispatched", out)
				}
				s.runtimeMu.Lock()
				status := s.runtimes[b.DeviceID].command.Status
				s.runtimeMu.Unlock()
				if status != "unknown" {
					t.Fatal("revoked command confirmed", status)
				}
			})
		}
	}
}
func TestAdminRuntimeReadOnlyAndExactShape(t *testing.T) {
	s, b, _, _ := runtimeFixture(t)
	admin := login(t, s, "admin", "alice")
	adminRuntimeReport(t, s, b, 1, "")
	path := "/api/admin/device-runtime/" + b.DeviceID
	for _, body := range []string{`{"field":"volume","value":50,"boot":"` + adminTestBoot + `","scope":"family"}`, `{"field":"volume","value":50,"value":50,"boot":"` + adminTestBoot + `"}`} {
		if w := request(s, "PUT", path, body, admin); w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if e := (ConfigStore{s.opts.ConfigPath}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); e != nil {
		t.Fatal(e)
	}
	w := request(s, "GET", path, "", admin)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out["canControl"] != false {
		t.Fatal(w.Code, out)
	}
}
func TestAdminMemoryClearRealUnixStoreAndFreshGuard(t *testing.T) {
	for _, kind := range []string{"confirmed", "role-revoked-before-voice", "manage-revoked", "body-revoked", "grant-revoked", "audit-failure"} {
		t.Run(kind, func(t *testing.T) {
			s, family, id, path := settingsFixture(t)
			admin := login(t, s, "admin", "alice")
			r, snapshotPath, _ := memoryFixture(t)
			s.opts.RecordsPath = r.Root
			s.reader.Root = r.Root
			archive := filepath.Join(r.Root, "source-a", "session.json")
			before, e := os.ReadFile(archive)
			if e != nil {
				t.Fatal(e)
			}
			memoryBefore, e := os.ReadFile(snapshotPath)
			if e != nil {
				t.Fatal(e)
			}
			voice := realtimelive.NewServer(realtimelive.Config{MemoryEnabled: true, MaxSessions: 2}, nil, r.Root, "synthetic-admin-clear")
			if e = voice.InitializationError(); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { voice.CloseActive(context.Background()) })
			var calls atomic.Int32
			s.opts.MemoryControlSocket = memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				if kind == "role-revoked-before-voice" {
					if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Accounts[0].InternalAdmin = false; return nil }); e != nil {
						t.Error(e)
					}
				}
				voice.MemoryControlHandler(NewMemoryClearGuard(path)).ServeHTTP(w, req)
			}))
			if w := request(s, "POST", "/api/admin/memory-clear/"+id, `{"confirm":true}`, family); w.Code != 401 {
				t.Fatal("family cookie", w.Code)
			}
			if strings.HasSuffix(kind, "revoked") {
				if e := (ConfigStore{path}).Transaction(func(c *Config) error {
					switch kind {
					case "manage-revoked":
						c.Accounts[0].Memberships[0].CanAddDevices = false
					case "body-revoked":
						c.Accounts[0].Memberships[0].Devices[0].CanReadBody = false
					case "grant-revoked":
						c.Accounts[0].Memberships[0].Devices = nil
					}
					return nil
				}); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "audit-failure" {
				s.auditWrite = func(string, auditEntry) error { return ErrSource }
			}
			w := request(s, "POST", "/api/admin/memory-clear/"+id, `{"confirm":true}`, admin)
			want := 403
			if kind == "confirmed" {
				want = 200
			}
			if kind == "audit-failure" {
				want = 503
			}
			if w.Code != want {
				t.Fatalf("status %d want %d %s", w.Code, want, w.Body)
			}
			wantCalls := int32(0)
			if kind == "confirmed" || kind == "role-revoked-before-voice" {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatal("RPC count", calls.Load())
			}
			after, e := os.ReadFile(archive)
			if e != nil || string(before) != string(after) {
				t.Fatal("archive changed", e)
			}
			raw, e := os.ReadFile(snapshotPath)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "confirmed" {
				var snapshot memorySnapshot
				if json.Unmarshal(raw, &snapshot) != nil || len(snapshot.Data.Turns) != 0 {
					t.Fatal("not empty")
				}
				audit, _ := os.ReadFile(s.opts.AuditPath)
				if !strings.Contains(string(audit), `"scope":"admin"`) {
					t.Fatal("audit scope", string(audit))
				}
			} else if string(raw) != string(memoryBefore) {
				t.Fatal("denied clear mutated memory")
			}
		})
	}
}
func TestAdminMemoryClearClaimScopeAndShape(t *testing.T) {
	s, _, id, path := settingsFixture(t)
	admin := login(t, s, "admin", "alice")
	for _, body := range []string{`{}`, `{"confirm":true,"scope":"family"}`, `{"confirm":true,"confirm":true}`, `{"Confirm":true}`} {
		if w := request(s, "POST", "/api/admin/memory-clear/"+id, body, admin); w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	b := s.cfg.Bindings[0]
	for _, scope := range []string{"admin", "family", "", "bad"} {
		raw, _ := json.Marshal(map[string]any{"deviceId": id, "actorId": "alice", "familyId": b.FamilyID, "bindingId": b.ID, "bindingVersion": b.Version, "deadline": time.Now().Add(time.Minute), "scope": scope})
		var claim realtimelive.MemoryClearClaim
		json.Unmarshal(raw, &claim)
		called := false
		e := NewMemoryClearGuard(path)(context.Background(), claim, func() error { called = true; return nil })
		if scope == "bad" {
			if !errors.Is(e, realtimelive.ErrMemoryClearForbidden) || called {
				t.Fatal("invalid scope", e, called)
			}
		} else if e != nil || !called {
			t.Fatal(scope, e)
		}
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	w := request(s, "POST", "/api/admin/memory-clear/"+id, `{"confirm":true}`, admin)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"memory_clear_unconfirmed"`) {
		t.Fatal(w.Code, w.Body)
	}
}

// Config reads before runtimeMu do not cover an arbitrary wait for the gate.
func TestAdminRuntimeRejectsAuthorityRevokedWhileRequestWaits(t *testing.T) {
	for _, kind := range []string{"role", "grant", "manage"} {
		t.Run(kind, func(t *testing.T) {
			s, b, _, _ := runtimeFixture(t)
			admin := login(t, s, "admin", "alice")
			adminRuntimeReport(t, s, b, 1, "")
			s.runtimeMu.Lock()
			locked := true
			defer func() {
				if locked {
					s.runtimeMu.Unlock()
				}
			}()
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				result <- request(s, "PUT", "/api/admin/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+adminTestBoot+`"}`, admin)
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				waiting := false
				for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
					if strings.Contains(stack, "(*Server).deviceRuntime") && strings.Contains(stack, "sync.(*Mutex).lockSlow") {
						waiting = true
						break
					}
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("request did not reach runtime lock")
				}
				runtime.Gosched()
			}
			if e := (ConfigStore{s.opts.ConfigPath}).Transaction(func(c *Config) error {
				switch kind {
				case "role":
					c.Accounts[0].InternalAdmin = false
				case "grant":
					c.Accounts[0].Memberships[0].Devices = nil
				case "manage":
					c.Accounts[0].Memberships[0].CanAddDevices = false
				}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			s.runtimeMu.Unlock()
			locked = false
			select {
			case w := <-result:
				if w.Code != 403 {
					t.Fatalf("revoked request created command: %d %s", w.Code, w.Body)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request deadlocked")
			}
			s.runtimeMu.Lock()
			command := s.runtimes[b.DeviceID].command
			s.runtimeMu.Unlock()
			if command != nil {
				t.Fatal("revoked request retained command")
			}
		})
	}
}
