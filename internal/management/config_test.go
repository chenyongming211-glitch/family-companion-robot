package management

import (
	"testing"
	"time"
)

func TestAuthorizationBinding(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg := Config{SchemaVersion: 1, Accounts: []AccountConfig{{ID: "a", Username: "a", Password: PasswordHash{Salt: "0123456789abcdef0123456789abcdef", Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Iterations: 600000}, Memberships: []Membership{{FamilyID: "f", Devices: []DeviceGrant{{DeviceID: "dev_0123456789abcdef0123456789abcdef", CanReadBody: true}}}}}}, Families: []FamilyConfig{{ID: "f", Name: "家"}}, Devices: []DeviceConfig{{ID: "dev_0123456789abcdef0123456789abcdef", Name: "机器人", Model: "微雪 ESP32-S3"}}, Bindings: []Binding{{ID: "b", Version: 1, FamilyID: "f", DeviceID: "dev_0123456789abcdef0123456789abcdef", From: start}}}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Authorize("a", "family", cfg.Devices[0].ID, start.Add(-time.Second), false); err == nil {
		t.Fatal("before binding allowed")
	}
	if _, err := cfg.Authorize("a", "family", cfg.Devices[0].ID, start, true); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Authorize("a", "admin", cfg.Devices[0].ID, start, true); err == nil {
		t.Fatal("non-admin allowed")
	}
}
