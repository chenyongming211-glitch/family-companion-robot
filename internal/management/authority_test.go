package management

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func authorityFixtureConfig() Config {
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	h := PasswordHash{Salt: strings.Repeat("01", 16), Digest: strings.Repeat("02", 32), Iterations: 600000}
	return Config{SchemaVersion: 1,
		Accounts: []AccountConfig{
			{ID: "alice", Username: "alice", Password: h, InternalAdmin: true, Memberships: []Membership{{FamilyID: "home-a", Devices: []DeviceGrant{{DeviceID: "dev_" + strings.Repeat("a", 32), CanReadBody: true}}}}},
			{ID: "bob", Username: "bob", Password: h, Memberships: []Membership{{FamilyID: "home-b", Devices: []DeviceGrant{{DeviceID: "dev_" + strings.Repeat("b", 32), CanReadBody: true}}}}},
			{ID: "observer", Username: "observer", Password: h, InternalAdmin: true},
			{ID: "metadata", Username: "metadata", Password: h, Memberships: []Membership{{FamilyID: "home-a", Devices: []DeviceGrant{{DeviceID: "dev_" + strings.Repeat("a", 32), CanReadBody: false}}}}},
		},
		Families: []FamilyConfig{{ID: "home-a", Name: "甲家庭"}, {ID: "home-b", Name: "乙家庭"}},
		Devices:  []DeviceConfig{{ID: "dev_" + strings.Repeat("a", 32), Name: "甲机器人", Model: "微雪 ESP32-S3"}, {ID: "dev_" + strings.Repeat("b", 32), Name: "乙机器人", Model: "M5 ESP32-S3"}},
		Bindings: []Binding{{ID: "bind-a", Version: 1, FamilyID: "home-a", DeviceID: "dev_" + strings.Repeat("a", 32), From: start}, {ID: "bind-b", Version: 1, FamilyID: "home-b", DeviceID: "dev_" + strings.Repeat("b", 32), From: start}},
	}
}

func TestAuthorityIsolationAndBodyPermission(t *testing.T) {
	c := authorityFixtureConfig()
	at := c.Bindings[0].From
	if err := ValidateConfig(c); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		account, scope, device string
		body                   bool
		allowed                bool
	}{
		{"alice", "admin", c.Devices[0].ID, true, true}, {"alice", "family", c.Devices[1].ID, true, false},
		{"bob", "admin", c.Devices[1].ID, true, false}, {"observer", "admin", c.Devices[0].ID, true, false},
		{"metadata", "family", c.Devices[0].ID, false, true}, {"metadata", "family", c.Devices[0].ID, true, false},
		{"alice", "invalid", c.Devices[0].ID, true, false}, {"missing", "family", c.Devices[0].ID, false, false},
	} {
		_, err := c.Authorize(tt.account, tt.scope, tt.device, at, tt.body)
		if (err == nil) != tt.allowed {
			t.Errorf("%+v: %v", tt, err)
		}
	}
	end := at.Add(time.Hour)
	c.Bindings[0].Until = &end
	for _, tt := range []struct {
		at      time.Time
		allowed bool
	}{{at.Add(-time.Nanosecond), false}, {at, true}, {end.Add(-time.Nanosecond), true}, {end, false}} {
		_, err := c.Authorize("alice", "family", c.Devices[0].ID, tt.at, true)
		if (err == nil) != tt.allowed {
			t.Errorf("boundary %v: %v", tt.at, err)
		}
	}
}

func TestAuthorityPrivateConfigRejectsAmbiguousJSON(t *testing.T) {
	c := authorityFixtureConfig()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	for _, tt := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"unknown-field", append(raw[:len(raw)-1], []byte(`,"allow_all":true}`)...), 0600},
		{"duplicate-field", []byte(`{"schema_version":0,"schema_version":1,"accounts":[],"families":[],"devices":[],"bindings":[]}`), 0600},
		{"trailing-json", append(append([]byte{}, raw...), []byte(` {}`)...), 0600},
		{"public-config", raw, 0644},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, tt.data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tt.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid, got %v", err)
			}
		})
	}
	if err := WritePrivateConfig(path, c); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(link); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink: %v", err)
	}
}

func TestAuthorityRejectsConflictingBindingsAndCredentials(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*Config)
	}{
		{"duplicate-account", func(c *Config) { c.Accounts = append(c.Accounts, c.Accounts[0]) }},
		{"unknown-device-grant", func(c *Config) { c.Accounts[0].Memberships[0].Devices[0].DeviceID = "dev_" + strings.Repeat("c", 32) }},
		{"overlap", func(c *Config) {
			b := c.Bindings[0]
			b.ID = "overlap"
			b.FamilyID = "home-b"
			b.Version = 2
			c.Bindings = append(c.Bindings, b)
		}},
		{"weak-hash", func(c *Config) { c.Accounts[0].Password.Iterations = 1 }},
		{"invalid-salt", func(c *Config) { c.Accounts[0].Password.Salt = "01" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := authorityFixtureConfig()
			tt.change(&c)
			if !errors.Is(ValidateConfig(c), ErrInvalid) {
				t.Fatal("conflict accepted")
			}
		})
	}
}
