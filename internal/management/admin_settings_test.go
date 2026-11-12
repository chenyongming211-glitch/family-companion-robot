package management

import (
	"bytes"
	"encoding/json"
	"familyrobot/internal/deviceprofile"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func readAdminSettings(t *testing.T, s *Server, cookie *http.Cookie, id string) DeviceSettingsDTO {
	t.Helper()
	w := request(s, "GET", "/api/admin/device-settings/"+id, "", cookie)
	if w.Code != 200 {
		t.Fatalf("admin settings GET: %d", w.Code)
	}
	var out DeviceSettingsDTO
	var shape map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || json.Unmarshal(w.Body.Bytes(), &shape) != nil || len(shape) != 5 || out.DeviceID != id || !digest.MatchString(out.Version) {
		t.Fatal("invalid admin settings contract")
	}
	for _, key := range []string{"deviceId", "name", "location", "version", "canEdit"} {
		if shape[key] == nil {
			t.Fatal("missing settings field", key)
		}
	}
	return out
}

func TestAdminSettingsRoundTripSharesFamilyVersionAndPreservesVoiceProfile(t *testing.T) {
	s, family, id, path := settingsFixture(t)
	admin := login(t, s, "admin", "alice")
	s.opts.ProfilesPath = filepath.Join(t.TempDir(), "profiles.json")
	store, err := deviceprofile.Open(s.opts.ProfilesPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetName(id, "语音伙伴"); err != nil {
		t.Fatal(err)
	}
	before, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	profileBefore, err := os.ReadFile(s.opts.ProfilesPath)
	if err != nil {
		t.Fatal(err)
	}
	view := readAdminSettings(t, s, admin, id)
	if view.Name != "语音伙伴" || !view.CanEdit || readSettings(t, s, family, id)["version"] != view.Version {
		t.Fatal("initial shared projection")
	}
	w := request(s, "PUT", "/api/admin/device-settings/"+id, settingsBody(t, view.Version, "  客厅伙伴  ", " 客厅 "), admin)
	if w.Code != 200 {
		t.Fatalf("admin save: %d", w.Code)
	}
	var saved DeviceSettingsDTO
	if json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatal("save contract")
	}
	got := readAdminSettings(t, s, admin, id)
	familyGot := readSettings(t, s, family, id)
	if saved != got || got.Name != "客厅伙伴" || got.Location != "客厅" || got.Version == view.Version || familyGot["version"] != got.Version || familyGot["name"] != got.Name || familyGot["location"] != got.Location {
		t.Fatal("shared persisted settings mismatch")
	}
	// A version from either surface must conflict after the other surface saves.
	w = request(s, "PUT", "/api/family/device-settings/"+id, settingsBody(t, view.Version, "过期昵称", ""), family)
	if w.Code != 409 {
		t.Fatalf("family stale version: %d", w.Code)
	}
	w = request(s, "PUT", "/api/family/device-settings/"+id, settingsBody(t, got.Version, "家庭伙伴", "卧室"), family)
	if w.Code != 200 {
		t.Fatalf("family save: %d", w.Code)
	}
	w = request(s, "PUT", "/api/admin/device-settings/"+id, settingsBody(t, got.Version, "过期昵称", ""), admin)
	if w.Code != 409 {
		t.Fatalf("admin stale version: %d", w.Code)
	}
	if readAdminSettings(t, s, admin, id).Name != "家庭伙伴" {
		t.Fatal("admin did not observe family save")
	}
	after, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	after.Devices[0].DisplayName, after.Devices[0].Location = before.Devices[0].DisplayName, before.Devices[0].Location
	beforeRaw, _ := json.Marshal(before)
	afterRaw, _ := json.Marshal(after)
	profileAfter, err := os.ReadFile(s.opts.ProfilesPath)
	if err != nil || !bytes.Equal(beforeRaw, afterRaw) || !bytes.Equal(profileBefore, profileAfter) {
		t.Fatal("settings changed unrelated authority or voice profile")
	}
	raw, err := os.ReadFile(s.opts.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var entry auditEntry
		if json.Unmarshal(line, &entry) != nil {
			t.Fatal("invalid audit")
		}
		if entry.Action == "update_device_settings_requested" {
			scopes = append(scopes, entry.Scope)
		}
	}
	if strings.Join(scopes, ",") != "admin,family" {
		t.Fatal("incorrect actual audit scopes", scopes)
	}
}

func TestAdminSettingsAuthenticationGrantBindingAndReadOnly(t *testing.T) {
	for _, kind := range []string{"family-cookie", "role", "grant", "binding", "read-only", "global-admin"} {
		t.Run(kind, func(t *testing.T) {
			s, family, id, path := settingsFixture(t)
			admin := login(t, s, "admin", "alice")
			version := readSettings(t, s, family, id)["version"]
			wantGET, wantPUT := 403, 403
			if kind == "family-cookie" {
				admin = family
				wantGET, wantPUT = 401, 401
			} else if kind == "global-admin" {
				admin = login(t, s, "admin", "observer")
			} else {
				err := (ConfigStore{path}).Transaction(func(c *Config) error {
					switch kind {
					case "role":
						c.Accounts[0].InternalAdmin = false
						wantGET, wantPUT = 401, 401
					case "grant":
						c.Accounts[0].Memberships[0].Devices = nil
					case "binding":
						end := s.now()
						c.Bindings[0].Until = &end
					case "read-only":
						c.Accounts[0].Memberships[0].CanAddDevices = false
						wantGET = 200
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if w := request(s, "GET", "/api/admin/device-settings/"+id, "", admin); w.Code != wantGET {
				t.Fatalf("read: %d want %d", w.Code, wantGET)
			}
			if kind == "read-only" && readAdminSettings(t, s, admin, id).CanEdit {
				t.Fatal("read-only can edit")
			}
			before, _ := os.ReadFile(path)
			if w := request(s, "PUT", "/api/admin/device-settings/"+id, settingsBody(t, version, "新伙伴", ""), admin); w.Code != wantPUT {
				t.Fatalf("write: %d want %d", w.Code, wantPUT)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("denied write persisted")
			}
		})
	}
}

func TestAdminSettingsRechecksAfterWriterLock(t *testing.T) {
	for _, kind := range []string{"role", "grant", "manage", "binding", "session", "version"} {
		t.Run(kind, func(t *testing.T) {
			s, _, id, path := settingsFixture(t)
			admin := login(t, s, "admin", "alice")
			view := readAdminSettings(t, s, admin, id)
			lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
				t.Fatal(err)
			}
			locked := true
			defer func() {
				if locked {
					_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
				}
			}()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- request(s, "PUT", "/api/admin/device-settings/"+id, settingsBody(t, view.Version, "迟到写入", ""), admin)
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				waiting := false
				for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
					if strings.Contains(stack, "(*Server).deviceSettings") && strings.Contains(stack, "ConfigStore.Transaction") && strings.Contains(stack, "syscall.Flock") {
						waiting = true
						break
					}
				}
				if waiting {
					break
				}
				select {
				case w := <-done:
					t.Fatalf("request bypassed writer lock: %d", w.Code)
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("request did not reach writer lock")
				}
				runtime.Gosched()
			}
			c, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			want := 403
			switch kind {
			case "role":
				c.Accounts[0].InternalAdmin = false
			case "grant":
				c.Accounts[0].Memberships[0].Devices = nil
			case "manage":
				c.Accounts[0].Memberships[0].CanAddDevices = false
			case "binding":
				end := s.now()
				c.Bindings[0].Until = &end
			case "session":
				if w := request(s, "POST", "/api/admin/logout", `{}`, admin); w.Code != 200 {
					t.Fatal("logout")
				}
			case "version":
				c.Devices[0].DisplayName = "其他更新"
				want = 409
			}
			// Synthetic test writer owns the flock while replacing the authoritative snapshot.
			if err := WritePrivateConfig(path, c); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
				t.Fatal(err)
			}
			locked = false
			select {
			case w := <-done:
				if w.Code != want {
					t.Fatalf("late authorization: %d want %d", w.Code, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request deadlocked")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("late write persisted")
			}
		})
	}
}

func TestAdminSettingsRejectsInvalidFieldsAndFailedPersistence(t *testing.T) {
	s, _, id, path := settingsFixture(t)
	admin := login(t, s, "admin", "alice")
	view := readAdminSettings(t, s, admin, id)
	valid := settingsBody(t, view.Version, "伙伴", "")
	before, _ := os.ReadFile(path)
	for _, body := range []string{
		`{}`, `{"name":null,"location":"","version":"x"}`,
		strings.TrimSuffix(valid, "}") + `,"scope":"family"}`,
		strings.Replace(valid, `"name":"伙伴"`, `"Name":"伙伴"`, 1),
		strings.Replace(valid, `"name":"伙伴"`, `"name":"伙伴","name":"其他"`, 1),
		settingsBody(t, view.Version, "", ""), settingsBody(t, view.Version, strings.Repeat("伴", 13), ""),
		settingsBody(t, view.Version, "伙伴", strings.Repeat("房", 25)), settingsBody(t, view.Version, "伴\n伙", ""),
		settingsBody(t, view.Version, "伙伴", "房\u2028间"), settingsBody(t, strings.ToUpper(view.Version), "伙伴", ""),
	} {
		if w := request(s, "PUT", "/api/admin/device-settings/"+id, body, admin); w.Code != 400 {
			t.Fatalf("invalid write: %d", w.Code)
		}
	}
	originalAuditWrite := s.auditWrite
	s.auditWrite = func(string, auditEntry) error { return ErrSource }
	if w := request(s, "PUT", "/api/admin/device-settings/"+id, valid, admin); w.Code != 503 {
		t.Fatalf("audit failure: %d", w.Code)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid or audit-failed write changed config")
	}
	s.auditWrite = originalAuditWrite
	// Keep the existing readable config and lock, but deny atomic replacement.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if err := os.Chmod(filepath.Dir(path), 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Dir(path), 0700)
	if w := request(s, "PUT", "/api/admin/device-settings/"+id, valid, admin); w.Code != 503 {
		t.Fatalf("config-write failure: %d", w.Code)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("config-write failure changed config")
	}
}
