package main

import (
	"bytes"
	"encoding/json"
	"familyrobot/internal/management"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func provisionConfig(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "management.json")
	c := management.Config{SchemaVersion: 1}
	if e := management.WritePrivateConfig(p, c); e != nil {
		t.Fatal(e)
	}
	legacy := filepath.Join(dir, "legacy.json")
	if e := os.WriteFile(legacy, []byte(`{"schema_version":1,"devices":[]}`), 0600); e != nil {
		t.Fatal(e)
	}
	return p, legacy
}
func TestProvisionRequiresExplicitLegacyGuardAndPrivateExclusiveBundle(t *testing.T) {
	p, legacy := provisionConfig(t)
	output := filepath.Join(filepath.Dir(p), "bundle.json")
	args := []string{"provision", "--config", p, "--mac", "02:00:00:00:00:01", "--serial", "UNIT1", "--model", "M5 ESP32-S3", "--output", output}
	var out bytes.Buffer
	if e := run(args, &out); e == nil {
		t.Fatal("missing legacy MAC guard accepted")
	}
	args = append(args, "--device-auth", legacy)
	if e := run(args, &out); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(output)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("private bundle", e)
	}
	var b management.FactoryBundle
	raw, e := os.ReadFile(output)
	if e != nil || json.Unmarshal(raw, &b) != nil {
		t.Fatal("bundle")
	}
	if b.SchemaVersion != 1 || len(b.ClaimCode) != 32 || len(b.DeviceKey) != 43 || !strings.HasPrefix(b.DeviceID, "dev_") {
		t.Fatal("bundle schema")
	}
	if strings.Contains(out.String(), b.DeviceKey) || strings.Contains(out.String(), b.ClaimCode) {
		t.Fatal("stdout secret")
	}
	c, e := management.LoadConfig(p)
	if e != nil || len(c.FactoryDevices) != 1 || len(c.Accounts) != 0 {
		t.Fatal("config", e)
	}
	configRaw, _ := os.ReadFile(p)
	if bytes.Contains(configRaw, []byte(b.DeviceKey)) || bytes.Contains(configRaw, []byte(b.ClaimCode)) {
		t.Fatal("raw private authority secret")
	}
	saved := append([]byte(nil), raw...)
	if e = run(args, &out); e == nil {
		t.Fatal("overwrite accepted")
	}
	raw, _ = os.ReadFile(output)
	if !bytes.Equal(saved, raw) {
		t.Fatal("bundle replaced")
	}
	args[10] = filepath.Join(filepath.Dir(p), "second.json") // output value
	if e = run(args, &out); e == nil {
		t.Fatal("duplicate MAC accepted")
	}
}
func TestProvisionRejectsLegacyMACMalformedRegistryAndSymlink(t *testing.T) {
	for _, raw := range []string{`{"schema_version":1,"devices":[{"mac":"02:00:00:00:00:01","token":"synthetic-long-token-01234567890123456789"}]}`, `{"schema_version":1,"schema_version":1,"devices":[]}`, `{"schema_version":1,"devices":[],"unknown":true}`} {
		t.Run(raw[:20], func(t *testing.T) {
			p, legacy := provisionConfig(t)
			if e := os.WriteFile(legacy, []byte(raw), 0600); e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			if e := run([]string{"provision", "--config", p, "--device-auth", legacy, "--mac", "02:00:00:00:00:01", "--serial", "UNIT1", "--model", "M5 ESP32-S3", "--output", filepath.Join(filepath.Dir(p), "bundle.json")}, &out); e == nil {
				t.Fatal("legacy guard bypass")
			}
			c, e := management.LoadConfig(p)
			if e != nil || len(c.FactoryDevices) != 0 {
				t.Fatal("mutation on rejection")
			}
		})
	}
}

func TestGrantDeviceAddOnlyExistingMembership(t *testing.T) {
	p, _ := provisionConfig(t)
	c, e := management.LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	c.Families = []management.FamilyConfig{{ID: "home", Name: "家"}}
	c.Accounts = []management.AccountConfig{{ID: "owner", Username: "owner", Password: management.PasswordHash{Salt: strings.Repeat("01", 16), Digest: strings.Repeat("02", 32), Iterations: 600000}, Memberships: []management.Membership{{FamilyID: "home"}}}}
	if e = management.WritePrivateConfig(p, c); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = run([]string{"grant-device-add", "--config", p, "--account", "owner", "--family", "missing"}, &out); e == nil {
		t.Fatal("invented family grant")
	}
	if e = run([]string{"grant-device-add", "--config", p, "--account", "owner", "--family", "home"}, &out); e != nil {
		t.Fatal(e)
	}
	updated, e := management.LoadConfig(p)
	if e != nil || !updated.Accounts[0].Memberships[0].CanAddDevices {
		t.Fatal("grant")
	}
	c.Accounts[0].Memberships[0].CanAddDevices = true
	raw, _ := json.Marshal(c)
	got, _ := json.Marshal(updated)
	if !bytes.Equal(raw, got) {
		t.Fatal("unrelated values changed")
	}
}

func TestProvisionAcceptsValidUnrelatedLegacyRegistry(t *testing.T) {
	p, legacy := provisionConfig(t)
	raw := `{"schema_version":1,"devices":[{"mac":"02:00:00:00:00:80","token":"synthetic-long-token-01234567890123456789"}]}`
	if e := os.WriteFile(legacy, []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := run([]string{"provision", "--config", p, "--device-auth", legacy, "--mac", "02:00:00:00:00:81", "--serial", "UNIT81", "--model", "M5 ESP32-S3", "--output", filepath.Join(filepath.Dir(p), "bundle.json")}, &out); e != nil {
		t.Fatal("unrelated valid legacy registry rejected", e)
	}
}

func TestProvisionOutputOpenAndWriteFailuresNeverRegisterIdentity(t *testing.T) {
	p, legacy := provisionConfig(t)
	dir := filepath.Dir(p)
	output := filepath.Join(dir, "bundle.json")
	if e := os.WriteFile(output, []byte("existing private file"), 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := run([]string{"provision", "--config", p, "--device-auth", legacy, "--mac", "02:00:00:00:00:91", "--serial", "UNIT91", "--model", "M5 ESP32-S3", "--output", output}, &out); e == nil {
		t.Fatal("output open accepted")
	}
	c, e := management.LoadConfig(p)
	if e != nil || len(c.FactoryDevices) != 0 {
		t.Fatal("identity committed before output open", e)
	}
	broken, e := os.OpenFile(filepath.Join(dir, "closed.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = broken.Close(); e != nil {
		t.Fatal(e)
	}
	_, e = management.ProvisionFactory(p, "02:00:00:00:00:92", "UNIT92", "M5 ESP32-S3", func(b management.FactoryBundle) error { return persistFactoryBundle(broken, dir, b) })
	if e == nil {
		t.Fatal("output write failure accepted")
	}
	c, e = management.LoadConfig(p)
	if e != nil || len(c.FactoryDevices) != 0 {
		t.Fatal("identity committed after failed output write", e)
	}
}
