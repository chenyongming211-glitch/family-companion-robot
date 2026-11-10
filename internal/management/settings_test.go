package management

import (
	"encoding/json"
	"familyrobot/internal/deviceprofile"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func settingsFixture(t *testing.T) (*Server, *http.Cookie, string, string) {
	t.Helper()
	s, _ := httpFixture(t)
	c := s.cfg
	c.Accounts[0].Memberships[0].CanAddDevices = true
	p := filepath.Join(t.TempDir(), "config.json")
	if err := WritePrivateConfig(p, c); err != nil {
		t.Fatal(err)
	}
	s.cfg = c
	s.opts.ConfigPath = p
	return s, login(t, s, "family", "alice"), c.Devices[0].ID, p
}

func readSettings(t *testing.T, s *Server, cookie *http.Cookie, id string) map[string]any {
	t.Helper()
	w := request(s, "GET", "/api/family/device-settings/"+id, "", cookie)
	if w.Code != 200 {
		t.Fatalf("settings GET: %d %s", w.Code, w.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func settingsBody(t *testing.T, version any, name, location string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"name": name, "location": location, "version": version})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSettingsRoundTripPreservesAuthorityAndReadsDisplayOverride(t *testing.T) {
	s, cookie, id, path := settingsFixture(t)
	before, _ := LoadConfig(path)
	view := readSettings(t, s, cookie, id)
	if view["deviceId"] != id || view["canEdit"] != true || view["name"] != "甲机器人" {
		t.Fatal(view)
	}
	if v, ok := view["version"].(string); !ok || len(v) != 64 {
		t.Fatal(view)
	}
	w := request(s, "PUT", "/api/family/device-settings/"+id, settingsBody(t, view["version"], "  客厅伙伴  ", " 客厅 "), cookie)
	if w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body)
	}
	after := readSettings(t, s, cookie, id)
	if after["name"] != "客厅伙伴" || after["location"] != "客厅" || after["version"] == view["version"] {
		t.Fatal(after)
	}
	w = request(s, "GET", "/api/family/devices/"+id, "", cookie)
	var device map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &device)
	if w.Code != 200 || device["name"] != "客厅伙伴" || device["location"] != "客厅" {
		t.Fatalf("device projection %d %s", w.Code, w.Body)
	}
	current, _ := LoadConfig(path)
	// Only the target presentation fields may change; authority and history stay exact.
	left, _ := json.Marshal(before)
	raw, _ := json.Marshal(current)
	var oldDoc, newDoc map[string]any
	_ = json.Unmarshal(left, &oldDoc)
	_ = json.Unmarshal(raw, &newDoc)
	newDoc["devices"] = oldDoc["devices"]
	raw, _ = json.Marshal(newDoc)
	left, _ = json.Marshal(oldDoc)
	if string(raw) != string(left) || current.Devices[1] != before.Devices[1] {
		t.Fatal("unrelated config changed")
	}
	if current.Devices[0].Name != before.Devices[0].Name {
		t.Fatal("voice fallback name overwritten")
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
}

func TestSettingsPermissionIsolationAndRevocation(t *testing.T) {
	s, owner, id, path := settingsFixture(t)
	view := readSettings(t, s, owner, id)
	body := settingsBody(t, view["version"], "新名字", "")
	for _, actor := range []string{"bob", "metadata", "observer"} {
		c := login(t, s, "family", actor)
		w := request(s, "PUT", "/api/family/device-settings/"+id, body, c)
		if w.Code != 403 {
			t.Fatalf("%s: %d %s", actor, w.Code, w.Body)
		}
		if actor == "metadata" && readSettings(t, s, c, id)["canEdit"] != false {
			t.Fatal("read-only can edit")
		}
	}
	if request(s, "GET", "/api/family/device-settings/"+id, "", nil).Code != 401 {
		t.Fatal("unauthenticated access")
	}
	if err := (ConfigStore{path}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if w := request(s, "PUT", "/api/family/device-settings/"+id, body, owner); w.Code != 403 {
		t.Fatalf("revoked owner %d %s", w.Code, w.Body)
	}
}

func TestSettingsRejectsAmbiguousInvalidAndUnsupportedWrites(t *testing.T) {
	s, cookie, id, _ := settingsFixture(t)
	version := readSettings(t, s, cookie, id)["version"]
	valid := settingsBody(t, version, "伙伴", "")
	for _, body := range []string{
		`{}`, `{"name":null,"location":"","version":"x"}`, `{"name":"伙伴","version":"x"}`,
		strings.Replace(valid, `"name":"伙伴"`, `"name":"伙伴","name":"其他"`, 1),
		strings.Replace(valid, `"name":"伙伴"`, `"name":"伙伴","Name":"其他"`, 1),
		strings.Replace(valid, `"name":"伙伴"`, `"Name":"伙伴"`, 1),
		strings.TrimSuffix(valid, "}") + `,"volume":80}`,
		settingsBody(t, version, "", ""), settingsBody(t, version, strings.Repeat("伴", 13), ""),
		settingsBody(t, version, "伙伴", strings.Repeat("房", 25)), settingsBody(t, version, "伴\n伙", ""),
		settingsBody(t, version, "伙伴", "房\u2028间"), settingsBody(t, "x", "伙伴", ""),
	} {
		w := request(s, "PUT", "/api/family/device-settings/"+id, body, cookie)
		if w.Code != 400 {
			t.Fatalf("invalid %q: %d %s", body, w.Code, w.Body)
		}
	}
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		if request(s, method, "/api/family/device-settings/"+id, valid, cookie).Code != 405 {
			t.Fatal(method)
		}
	}
}

func TestSettingsConcurrentStaleVersionAndAuditFailure(t *testing.T) {
	s, cookie, id, path := settingsFixture(t)
	view := readSettings(t, s, cookie, id)
	body := settingsBody(t, view["version"], "新伙伴", "卧室")
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- request(s, "PUT", "/api/family/device-settings/"+id, body, cookie).Code
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	before, _ := os.ReadFile(path)
	view = readSettings(t, s, cookie, id)
	s.auditWrite = func(string, auditEntry) error { return ErrSource }
	w := request(s, "PUT", "/api/family/device-settings/"+id, settingsBody(t, view["version"], "失败更改", ""), cookie)
	after, _ := os.ReadFile(path)
	if w.Code != 503 || string(before) != string(after) {
		t.Fatalf("audit failure changed authority %d", w.Code)
	}
}

func TestSettingsFamilyAliasPreservesVoiceProfile(t *testing.T) {
	s, cookie, id, _ := settingsFixture(t)
	profilesDir := t.TempDir()
	if err := os.Chmod(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	s.opts.ProfilesPath = filepath.Join(profilesDir, "profiles.json")
	store, err := deviceprofile.Open(s.opts.ProfilesPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetName(id, "语音伙伴"); err != nil {
		t.Fatal(err)
	}
	view := readSettings(t, s, cookie, id)
	if view["name"] != "语音伙伴" {
		t.Fatal(view)
	}
	w := request(s, "PUT", "/api/family/device-settings/"+id, settingsBody(t, view["version"], "家庭昵称", "客厅"), cookie)
	if w.Code != 200 {
		t.Fatalf("alias %d %s", w.Code, w.Body)
	}
	w = request(s, "GET", "/api/family/devices/"+id, "", cookie)
	var dto DeviceDTO
	if json.Unmarshal(w.Body.Bytes(), &dto) != nil || w.Code != 200 || dto.Name != "家庭昵称" {
		t.Fatalf("projection %d %s", w.Code, w.Body)
	}
	profile, err := store.Resolve(id, "")
	if err != nil || profile.Name != "语音伙伴" || profile.Revision != 1 {
		t.Fatalf("voice profile changed %+v %v", profile, err)
	}
}
