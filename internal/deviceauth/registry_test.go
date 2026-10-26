package deviceauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const primaryMAC = "aa:bb:cc:dd:ee:01"

var primaryToken = strings.Repeat("p", 64)
var otherToken = strings.Repeat("s", 64)

func registryFile(t *testing.T, raw string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(raw), mode); err != nil {
		t.Fatal(err)
	}
	return path
}
func validJSON() string {
	return fmt.Sprintf(`{"schema_version":1,"devices":[{"mac":"AA:BB:CC:DD:EE:01","token":%q},{"mac":"aa:bb:cc:dd:ee:02","token":%q}]}`, primaryToken, otherToken)
}
func TestRegistryAuthenticatesCanonicalMACIndependentCredentialsAndOwnsSnapshot(t *testing.T) {
	path := registryFile(t, validJSON(), 0600)
	registry, err := Load(path, primaryMAC, primaryToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		mac, token string
		want       bool
	}{{"AA-BB-CC-DD-EE-01", primaryToken, true}, {"aa:bb:cc:dd:ee:02", otherToken, true}, {primaryMAC, otherToken, false}, {"aa:bb:cc:dd:ee:02", primaryToken, false}, {"aa:bb:cc:dd:ee:03", primaryToken, false}} {
		if _, ok := registry.Authenticate(v.mac, v.token); ok != v.want {
			t.Fatal("credential boundary failed")
		}
	}
	entry, ok := registry.Lookup(primaryMAC)
	if !ok || entry.MAC != primaryMAC {
		t.Fatal("canonical lookup failed")
	}
	entry.Token = "changed"
	os.WriteFile(path, []byte(`broken`), 0600)
	if got, ok := registry.Lookup(primaryMAC); !ok || got.Token != primaryToken {
		t.Fatal("snapshot modified by caller or file")
	}
}
func TestRegistryRejectsUnsafeOrAmbiguousInputWithoutSecrets(t *testing.T) {
	good := validJSON()
	cases := map[string]string{
		"duplicate_schema":    strings.Replace(good, `"schema_version":1`, `"schema_version":2,"schema_version":1`, 1),
		"duplicate_entry_key": strings.Replace(good, `"mac":"AA:BB:CC:DD:EE:01"`, `"mac":"aa:bb:cc:dd:ee:03","mac":"AA:BB:CC:DD:EE:01"`, 1),
		"invalid_json":        "{", "trailing": good + ` {}`, "schema": strings.Replace(good, `"schema_version":1`, `"schema_version":2`, 1),
		"unknown_field":   strings.Replace(good, `"devices":`, `"extra":true,"devices":`, 1),
		"missing_devices": `{"schema_version":1}`, "empty": `{"schema_version":1,"devices":[]}`,
		"duplicate_mac":   strings.Replace(good, "aa:bb:cc:dd:ee:02", primaryMAC, 1),
		"duplicate_token": strings.Replace(good, otherToken, primaryToken, 1),
		"short_token":     strings.Replace(good, otherToken, "short", 1), "long_token": strings.Replace(good, otherToken, strings.Repeat("x", 513), 1),
		"whitespace_token": strings.Replace(good, otherToken, otherToken+" ", 1),
		"invalid_mac":      strings.Replace(good, "aa:bb:cc:dd:ee:02", "not-a-mac", 1), "eui64": strings.Replace(good, "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:ff:00:11", 1),
		"oversize": strings.Repeat(" ", 1<<20) + good,
	}
	devices := make([]map[string]string, 65)
	for i := range devices {
		devices[i] = map[string]string{"mac": fmt.Sprintf("aa:bb:cc:dd:ee:%02x", i), "token": strings.Repeat(fmt.Sprintf("%02x", i), 32)}
	}
	many, _ := json.Marshal(map[string]any{"schema_version": 1, "devices": devices})
	cases["too_many"] = string(many)
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(registryFile(t, raw, 0600), primaryMAC, primaryToken)
			if err == nil {
				t.Fatal("unsafe registry accepted")
			}
			if strings.Contains(err.Error(), primaryToken) || strings.Contains(err.Error(), otherToken) {
				t.Fatal("secret leaked")
			}
		})
	}
	for _, mode := range []os.FileMode{0644, 0640, 0604, 0700} {
		t.Run(fmt.Sprintf("mode_%o", mode), func(t *testing.T) {
			if _, err := Load(registryFile(t, good, mode), primaryMAC, primaryToken); err == nil {
				t.Fatal("unsafe permissions accepted")
			}
		})
	}
	path := registryFile(t, good, 0600)
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link, primaryMAC, primaryToken); err == nil {
		t.Fatal("symlink accepted")
	}
	for _, v := range []struct{ mac, token string }{{"", primaryToken}, {primaryMAC, ""}, {"aa:bb:cc:dd:ee:03", primaryToken}, {primaryMAC, otherToken}} {
		if _, err := Load(path, v.mac, v.token); err == nil {
			t.Fatal("legacy primary mismatch accepted")
		}
	}
}
