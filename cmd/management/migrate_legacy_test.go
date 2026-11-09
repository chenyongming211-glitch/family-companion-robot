package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"familyrobot/internal/management"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func legacyFixture(t *testing.T) (string, string, string, []string) {
	t.Helper()
	p, registry := provisionConfig(t)
	id := "dev_" + strings.Repeat("a", 32)
	token := "synthetic-legacy-token-012345678901234567890"
	c := management.Config{SchemaVersion: 1, Families: []management.FamilyConfig{{ID: "home", Name: "家"}}, Devices: []management.DeviceConfig{{ID: id, Name: "原机器人", Model: "微雪 ESP32-S3", Location: "客厅"}}, Bindings: []management.Binding{{ID: "original", Version: 1, FamilyID: "home", DeviceID: id, From: time.Now().UTC().Add(-time.Hour)}}, Accounts: []management.AccountConfig{{ID: "owner", Username: "owner", Password: management.PasswordHash{Salt: strings.Repeat("01", 16), Digest: strings.Repeat("02", 32), Iterations: 600000}, Memberships: []management.Membership{{FamilyID: "home", CanAddDevices: true, Devices: []management.DeviceGrant{{DeviceID: id, CanReadBody: false}}}}}}}
	if e := management.WritePrivateConfig(p, c); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "devices": []any{map[string]string{"mac": "02:00:00:00:00:01", "token": token}}})
	if e := os.WriteFile(registry, raw, 0600); e != nil {
		t.Fatal(e)
	}
	identities := filepath.Join(filepath.Dir(p), "identities.json")
	sum := sha256.Sum256([]byte(token))
	raw, _ = json.Marshal(map[string]any{"schema_version": 1, "devices": []any{map[string]string{"device_id": id, "client_type": "xiaozhi", "credential_sha256": hex.EncodeToString(sum[:]), "external_id": "02:00:00:00:00:01"}}})
	if e := os.WriteFile(identities, raw, 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"migrate-legacy", "--config", p, "--device-id", id, "--binding-id", "original", "--mac", "02:00:00:00:00:01", "--serial", "LEGACY1", "--model", "微雪 ESP32-S3", "--device-auth", registry, "--identities", identities, "--output", filepath.Join(filepath.Dir(p), "bundle.json")}
	return p, registry, identities, args
}
func TestMigrateLegacyPreservesAuthorityAndReadOnlyIdentity(t *testing.T) {
	p, registry, identities, args := legacyFixture(t)
	before, _ := management.LoadConfig(p)
	oldRegistry, _ := os.ReadFile(registry)
	oldIDs, _ := os.ReadFile(identities)
	var out bytes.Buffer
	if e := run(args, &out); e != nil {
		t.Fatal("valid migration rejected", e)
	}
	after, e := management.LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(after.FactoryDevices) != 1 {
		t.Fatal("missing migration")
	}
	added := after.FactoryDevices
	after.FactoryDevices = nil
	if !reflect.DeepEqual(before, after) {
		t.Fatal("original authority changed")
	}
	raw, _ := json.Marshal(added[0])
	var entry map[string]any
	_ = json.Unmarshal(raw, &entry)
	if entry["migration_binding_id"] != "original" {
		t.Fatal("missing original binding association")
	}
	raw, e = os.ReadFile(args[len(args)-1])
	if e != nil {
		t.Fatal(e)
	}
	var b management.FactoryBundle
	_ = json.Unmarshal(raw, &b)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 9 || b.DeviceID != before.Devices[0].ID || len(b.DeviceKey) != 43 || len(b.ClaimCode) != 32 {
		t.Fatal("migration bundle schema")
	}
	if strings.Contains(out.String(), b.DeviceKey) || strings.Contains(out.String(), b.ClaimCode) {
		t.Fatal("secret output")
	}
	got, _ := os.ReadFile(registry)
	gotIDs, _ := os.ReadFile(identities)
	if !bytes.Equal(got, oldRegistry) || !bytes.Equal(gotIDs, oldIDs) {
		t.Fatal("legacy identity changed")
	}
	args[len(args)-1] = filepath.Join(filepath.Dir(p), "retry.json")
	if run(args, &out) == nil {
		t.Fatal("duplicate migration accepted")
	}
	if _, e = os.Stat(args[len(args)-1]); !os.IsNotExist(e) {
		t.Fatal("failed retry left bundle")
	}
}
func TestMigrateLegacyRejectsIncorrectAuthorityAndProtectedInputs(t *testing.T) {
	for _, kind := range []string{"id", "mac", "binding", "model", "missing-registry", "missing-identities", "wrong-token", "wrong-sha", "wrong-external", "duplicate-json", "unknown-json", "symlink", "public", "oversized", "ended", "future", "output-exists"} {
		t.Run(kind, func(t *testing.T) {
			p, registry, identities, args := legacyFixture(t)
			switch kind {
			case "id":
				args[4] = "dev_" + strings.Repeat("b", 32)
			case "mac":
				args[8] = "02:00:00:00:00:02"
			case "binding":
				args[6] = "missing"
			case "model":
				args[12] = "M5 ESP32-S3"
			case "missing-registry":
				args[14] = ""
			case "missing-identities":
				args[16] = ""
			case "wrong-token":
				raw, _ := os.ReadFile(registry)
				raw = bytes.ReplaceAll(raw, []byte("synthetic-legacy-token"), []byte("different-legacy-token"))
				_ = os.WriteFile(registry, raw, 0600)
			case "wrong-sha":
				raw, _ := os.ReadFile(identities)
				var v map[string]any
				_ = json.Unmarshal(raw, &v)
				v["devices"].([]any)[0].(map[string]any)["credential_sha256"] = strings.Repeat("0", 64)
				raw, _ = json.Marshal(v)
				_ = os.WriteFile(identities, raw, 0600)
			case "wrong-external":
				raw, _ := os.ReadFile(identities)
				raw = bytes.ReplaceAll(raw, []byte("00:01"), []byte("00:02"))
				_ = os.WriteFile(identities, raw, 0600)
			case "duplicate-json":
				raw, _ := os.ReadFile(identities)
				raw = bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)
				_ = os.WriteFile(identities, raw, 0600)
			case "unknown-json":
				raw, _ := os.ReadFile(identities)
				raw = append([]byte(`{"unknown":true,`), raw[1:]...)
				_ = os.WriteFile(identities, raw, 0600)
			case "symlink":
				link := identities + ".link"
				_ = os.Symlink(identities, link)
				args[16] = link
			case "public":
				_ = os.Chmod(identities, 0644)
			case "oversized":
				_ = os.WriteFile(identities, bytes.Repeat([]byte(" "), (2<<20)+1), 0600)
			case "ended", "future":
				c, _ := management.LoadConfig(p)
				v := time.Now().UTC()
				if kind == "ended" {
					c.Bindings[0].Until = &v
				} else {
					c.Bindings[0].From = v.Add(time.Hour)
				}
				if e := management.WritePrivateConfig(p, c); e != nil {
					t.Fatal(e)
				}
			case "output-exists":
				_ = os.WriteFile(args[len(args)-1], []byte("existing"), 0600)
			}
			before, _ := os.ReadFile(p)
			var out bytes.Buffer
			if run(args, &out) == nil {
				t.Fatal("invalid migration accepted")
			}
			after, _ := os.ReadFile(p)
			if !bytes.Equal(before, after) {
				t.Fatal("authority changed on rejection")
			}
		})
	}
}
