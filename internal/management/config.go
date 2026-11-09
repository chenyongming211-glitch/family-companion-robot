package management

import (
	"bytes"
	"encoding/json"
	"errors"
	"familyrobot/internal/voiceidentity"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalid   = errors.New("invalid")
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("not found")
	ErrSource    = errors.New("source error")
	ErrTooLarge  = errors.New("too large")
)

type Config struct {
	SchemaVersion    int               `json:"schema_version"`
	Accounts         []AccountConfig   `json:"accounts"`
	Families         []FamilyConfig    `json:"families"`
	Devices          []DeviceConfig    `json:"devices"`
	Bindings         []Binding         `json:"bindings"`
	FactoryDevices   []FactoryDevice   `json:"factory_devices,omitempty"`
	OnboardingClaims []OnboardingClaim `json:"onboarding_claims,omitempty"`
}
type AccountConfig struct {
	ID            string       `json:"id"`
	Username      string       `json:"username"`
	Password      PasswordHash `json:"password_hash"`
	InternalAdmin bool         `json:"internal_admin"`
	Memberships   []Membership `json:"memberships"`
}
type PasswordHash struct {
	Salt       string `json:"salt"`
	Digest     string `json:"digest"`
	Iterations int    `json:"iterations"`
}
type Membership struct {
	FamilyID      string        `json:"family_id"`
	CanAddDevices bool          `json:"can_add_devices,omitempty"`
	Devices       []DeviceGrant `json:"devices"`
}
type DeviceGrant struct {
	DeviceID    string `json:"device_id"`
	CanReadBody bool   `json:"can_read_body"`
}
type FamilyConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type DeviceConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Model    string `json:"model"`
	Location string `json:"location"`
}
type Binding struct {
	ID       string     `json:"id"`
	Version  int        `json:"version"`
	FamilyID string     `json:"family_id"`
	DeviceID string     `json:"device_id"`
	From     time.Time  `json:"from"`
	Until    *time.Time `json:"until,omitempty"`
}

var ident = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var username = regexp.MustCompile(`^[a-z0-9_.@-]{1,128}$`)
var device = regexp.MustCompile(`^dev_[a-f0-9]{32}$`)
var salt = regexp.MustCompile(`^[a-f0-9]{32,128}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateConfig(c Config) error {
	if c.SchemaVersion != 1 || len(c.Accounts) > 1024 || len(c.Families) > 1024 || len(c.Devices) > 1024 || len(c.Bindings) > 10000 {
		return ErrInvalid
	}
	families := map[string]bool{}
	devices := map[string]bool{}
	ids := map[string]bool{}
	users := map[string]bool{}
	for _, f := range c.Families {
		if !ident.MatchString(f.ID) || families[f.ID] || len(f.Name) == 0 || len(f.Name) > 256 {
			return ErrInvalid
		}
		families[f.ID] = true
	}
	for _, d := range c.Devices {
		if !device.MatchString(d.ID) || devices[d.ID] || len(d.Name) > 256 || len(d.Location) > 256 || (d.Model != "微雪 ESP32-S3" && d.Model != "M5 ESP32-S3") {
			return ErrInvalid
		}
		devices[d.ID] = true
	}
	for _, a := range c.Accounts {
		if !ident.MatchString(a.ID) || ids[a.ID] || !username.MatchString(a.Username) || users[a.Username] || !salt.MatchString(a.Password.Salt) || len(a.Password.Salt)%2 != 0 || !digest.MatchString(a.Password.Digest) || a.Password.Iterations < 600000 || a.Password.Iterations > 2000000 || len(a.Memberships) > 1024 {
			return ErrInvalid
		}
		ids[a.ID] = true
		users[a.Username] = true
		ms := map[string]bool{}
		for _, m := range a.Memberships {
			if !families[m.FamilyID] || ms[m.FamilyID] || len(m.Devices) > 1024 {
				return ErrInvalid
			}
			ms[m.FamilyID] = true
			ds := map[string]bool{}
			for _, g := range m.Devices {
				if !devices[g.DeviceID] || ds[g.DeviceID] {
					return ErrInvalid
				}
				ds[g.DeviceID] = true
			}
		}
	}
	bs := map[string]bool{}
	for i, b := range c.Bindings {
		if !ident.MatchString(b.ID) || bs[b.ID] || b.Version < 1 || !families[b.FamilyID] || !devices[b.DeviceID] || b.From.IsZero() || (b.Until != nil && !b.From.Before(*b.Until)) {
			return ErrInvalid
		}
		bs[b.ID] = true
		for _, p := range c.Bindings[:i] {
			if p.DeviceID == b.DeviceID && (p.Until == nil || b.From.Before(*p.Until)) && (b.Until == nil || p.From.Before(*b.Until)) {
				return ErrInvalid
			}
		}
	}
	return validateOnboarding(c)
}
func LoadConfig(path string) (Config, error) {
	var c Config
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 2<<20 {
		return c, ErrInvalid
	}
	f, e := os.Open(path)
	if e != nil {
		return c, ErrInvalid
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(st, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 {
		return c, ErrInvalid
	}
	raw, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if e != nil || len(raw) > 2<<20 {
		return c, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&c) != nil || ValidateConfig(c) != nil || uniqueConfigKeys(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return Config{}, ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return Config{}, ErrInvalid
	}
	return c, nil
}

func uniqueConfigKeys(dec *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalid
	}
	token, err := dec.Token()
	if err != nil {
		return ErrInvalid
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			name, valid := key.(string)
			if err != nil || !valid || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if uniqueConfigKeys(dec, depth+1) != nil {
				return ErrInvalid
			}
		}
	case '[':
		for dec.More() {
			if uniqueConfigKeys(dec, depth+1) != nil {
				return ErrInvalid
			}
		}
	default:
		return ErrInvalid
	}
	if _, err := dec.Token(); err != nil {
		return ErrInvalid
	}
	return nil
}
func WritePrivateConfig(path string, c Config) error {
	if e := ValidateConfig(c); e != nil {
		return e
	}
	raw, e := json.Marshal(c)
	if e != nil || len(raw) > 2<<20 {
		return ErrTooLarge
	}
	if e := voiceidentity.WritePrivateJSON(path, c); e != nil {
		return ErrSource
	}
	return nil
}
func (c Config) FindAccount(name string) (AccountConfig, bool) {
	for _, a := range c.Accounts {
		if a.Username == name {
			return a, true
		}
	}
	return AccountConfig{}, false
}
func (c Config) AccountByID(id string) (AccountConfig, bool) {
	for _, a := range c.Accounts {
		if a.ID == id {
			return a, true
		}
	}
	return AccountConfig{}, false
}
func (c Config) Authorize(accountID, scope, deviceID string, at time.Time, body bool) (Binding, error) {
	a, ok := c.AccountByID(accountID)
	if !ok || (scope != "family" && scope != "admin") || (scope == "admin" && !a.InternalAdmin) || strings.TrimSpace(deviceID) == "" {
		return Binding{}, ErrForbidden
	}
	for _, b := range c.Bindings {
		if b.DeviceID != deviceID || at.Before(b.From) || (b.Until != nil && !at.Before(*b.Until)) {
			continue
		}
		for _, m := range a.Memberships {
			if m.FamilyID != b.FamilyID {
				continue
			}
			for _, g := range m.Devices {
				if g.DeviceID == deviceID && (!body || g.CanReadBody) {
					return b, nil
				}
			}
		}
	}
	return Binding{}, ErrForbidden
}
