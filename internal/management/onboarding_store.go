package management

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const onboardingOrigin = "https://robot.songguoxue.com"
const onboardingMiniProgramID = "wxb3f1745f8ed1fa1d"

type FactoryDevice struct {
	MigrationBindingID string `json:"migration_binding_id,omitempty"`
	DeviceID           string `json:"device_id"`
	MAC                string `json:"mac"`
	Serial             string `json:"serial"`
	Model              string `json:"model"`
	KeyDigest          string `json:"key_digest"`
	ClaimDigest        string `json:"claim_digest"`
}
type OnboardingClaim struct {
	ID          string     `json:"id"`
	ActorID     string     `json:"actor_id"`
	FamilyID    string     `json:"family_id"`
	DeviceID    string     `json:"device_id"`
	Challenge   string     `json:"challenge"`
	Status      string     `json:"status"`
	ExpiresAt   time.Time  `json:"expires_at"`
	BindingFrom *time.Time `json:"binding_from,omitempty"`
	Name        string     `json:"name,omitempty"`
}
type FactoryBundle struct {
	SchemaVersion int    `json:"schema_version"`
	DeviceID      string `json:"device_id"`
	MAC           string `json:"mac"`
	Serial        string `json:"serial"`
	Model         string `json:"model"`
	DeviceKey     string `json:"device_key"`
	ClaimCode     string `json:"claim_code"`
	BootstrapURL  string `json:"bootstrap_url"`
	VoiceURL      string `json:"voice_url"`
}

var canonicalMAC = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
var claimCode = regexp.MustCompile(`^[0-9a-f]{32}$`)

func secretDigest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", ErrSource
	}
	return hex.EncodeToString(b), nil
}
func validateOnboarding(c Config) error {
	if len(c.FactoryDevices) > 1024 || len(c.OnboardingClaims) > 4096 {
		return ErrInvalid
	}
	ids, macs, serials, codes, keys := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range c.FactoryDevices {
		if !device.MatchString(d.DeviceID) || !canonicalMAC.MatchString(d.MAC) || !ident.MatchString(d.Serial) || ids[d.DeviceID] || macs[d.MAC] || serials[d.Serial] || codes[d.ClaimDigest] || keys[d.KeyDigest] || !digest.MatchString(d.KeyDigest) || !digest.MatchString(d.ClaimDigest) || (d.Model != "微雪 ESP32-S3" && d.Model != "M5 ESP32-S3") {
			return ErrInvalid
		}
		if d.MigrationBindingID != "" {
			if !ident.MatchString(d.MigrationBindingID) {
				return ErrInvalid
			}
			found := false
			for _, b := range c.Bindings {
				if b.ID == d.MigrationBindingID && b.DeviceID == d.DeviceID {
					for _, old := range c.Devices {
						if old.ID == d.DeviceID && old.Model == d.Model {
							found = true
						}
					}
				}
			}
			if !found {
				return ErrInvalid
			}
		}
		ids[d.DeviceID] = true
		macs[d.MAC] = true
		serials[d.Serial] = true
		codes[d.ClaimDigest] = true
		keys[d.KeyDigest] = true
	}
	tasks := map[string]bool{}
	active := map[string]bool{}
	confirmed := map[string]bool{}
	for _, t := range c.OnboardingClaims {
		a, ok := c.AccountByID(t.ActorID)
		if !ok || !ident.MatchString(t.ID) || tasks[t.ID] || !ids[t.DeviceID] || !digest.MatchString(t.Challenge) || t.ExpiresAt.IsZero() || len(t.Name) > 256 {
			return ErrInvalid
		}
		tasks[t.ID] = true
		member := false
		for _, m := range a.Memberships {
			if m.FamilyID == t.FamilyID {
				member = true
			}
		}
		if !member {
			return ErrInvalid
		}
		d, _ := factoryByID(c, t.DeviceID)
		if d.MigrationBindingID != "" {
			for _, b := range c.Bindings {
				if b.ID == d.MigrationBindingID && b.FamilyID != t.FamilyID {
					return ErrInvalid
				}
			}
		}
		switch t.Status {
		case "waiting":
			if active[t.DeviceID] || t.BindingFrom != nil {
				return ErrInvalid
			}
			active[t.DeviceID] = true
		case "confirmed":
			if confirmed[t.DeviceID] || t.BindingFrom == nil || t.BindingFrom.IsZero() {
				return ErrInvalid
			}
			confirmed[t.DeviceID] = true
			d, _ := factoryByID(c, t.DeviceID)
			foundDevice, foundBinding := false, false
			for _, v := range c.Devices {
				if v.ID == t.DeviceID && v.Model == d.Model {
					foundDevice = true
				}
			}
			for _, v := range c.Bindings {
				if v.DeviceID == t.DeviceID && v.FamilyID == t.FamilyID && v.From.Equal(*t.BindingFrom) && (d.MigrationBindingID == "" || v.ID == d.MigrationBindingID) {
					foundBinding = true
				}
			}
			if !foundDevice || !foundBinding {
				return ErrInvalid
			}
		case "cancelled", "expired":
			if t.BindingFrom != nil {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	for _, d := range c.FactoryDevices {
		for _, old := range c.Devices {
			if old.ID == d.DeviceID && !confirmed[d.DeviceID] && d.MigrationBindingID == "" {
				return ErrInvalid
			}
		}
	}
	return nil
}

// ConfigStore serializes all writers, including the trusted CLI, through a fixed
// sibling inode. The config inode itself is atomically replaced after fsync.
type ConfigStore struct{ Path string }

func (s ConfigStore) Transaction(fn func(*Config) error) error {
	if s.Path == "" {
		return ErrSource
	}
	fd, e := syscall.Open(s.Path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if e != nil {
		return ErrSource
	}
	f := os.NewFile(uintptr(fd), s.Path+".lock")
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return ErrSource
	}
	if syscall.Flock(fd, syscall.LOCK_EX) != nil {
		return ErrSource
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	c, e := LoadConfig(s.Path)
	if e != nil {
		return ErrSource
	}
	if e = fn(&c); e != nil {
		return e
	}
	return WritePrivateConfig(s.Path, c)
}

// WithSnapshot reads without rewriting config. Cancellation bounds lock
// acquisition and callback admission only; fn always finishes before unlock.
func (s ConfigStore) WithSnapshot(ctx context.Context, fn func(Config) error) error {
	if s.Path == "" || fn == nil {
		return ErrSource
	}
	fd, e := syscall.Open(s.Path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if e != nil {
		return ErrSource
	}
	f := os.NewFile(uintptr(fd), s.Path+".lock")
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return ErrSource
	}
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EAGAIN) {
			return ErrSource
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	c, e := LoadConfig(s.Path)
	if e != nil {
		return ErrSource
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return fn(c)
}

// ProvisionFactory commits authority only after saveBundle has durably saved
// the private package. A save error aborts the transaction before config write.
// Callers must preserve a saved bundle if the final config fsync is ambiguous.
func ProvisionFactory(path, mac, serial, model string, saveBundle func(FactoryBundle) error) (FactoryBundle, error) {
	return provisionFactory(path, "", "", mac, serial, model, saveBundle)
}

// ProvisionLegacyFactory adds only factory authentication for an existing device.
// The private bundle must be durable before authority is committed. Legacy voice
// credentials and identity files are verified by the trusted CLI and stay intact.
func ProvisionLegacyFactory(path, deviceID, bindingID, mac, serial, model string, saveBundle func(FactoryBundle) error) (FactoryBundle, error) {
	if !device.MatchString(deviceID) || !ident.MatchString(bindingID) || model != "微雪 ESP32-S3" || saveBundle == nil {
		return FactoryBundle{}, ErrInvalid
	}
	return provisionFactory(path, deviceID, bindingID, mac, serial, model, saveBundle)
}

func provisionFactory(path, deviceID, bindingID, mac, serial, model string, saveBundle func(FactoryBundle) error) (FactoryBundle, error) {
	var b FactoryBundle
	mac = strings.ToLower(mac)
	if !canonicalMAC.MatchString(mac) || !ident.MatchString(serial) || (model != "微雪 ESP32-S3" && model != "M5 ESP32-S3") {
		return b, ErrInvalid
	}
	id, e := randomHex(16)
	if e != nil {
		return b, e
	}
	code, e := randomHex(16)
	if e != nil {
		return b, e
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		return b, ErrSource
	}
	if deviceID == "" {
		deviceID = "dev_" + id
	}
	b = FactoryBundle{1, deviceID, mac, serial, model, base64.RawURLEncoding.EncodeToString(key), code, onboardingOrigin + "/api/device/bootstrap", "wss://robot.songguoxue.com/xiaozhi/v1/"}
	e = (ConfigStore{path}).Transaction(func(c *Config) error {
		for _, d := range c.FactoryDevices {
			if d.MAC == mac || d.Serial == serial || d.DeviceID == b.DeviceID {
				return ErrInvalid
			}
		}
		record := FactoryDevice{DeviceID: b.DeviceID, MAC: mac, Serial: serial, Model: model, KeyDigest: secretDigest(b.DeviceKey), ClaimDigest: secretDigest(code), MigrationBindingID: bindingID}
		if bindingID != "" {
			if _, ok := migrationBinding(*c, record, time.Now()); !ok {
				return ErrInvalid
			}
		}
		for _, d := range c.Devices {
			if d.ID == b.DeviceID && bindingID == "" {
				return ErrInvalid
			}
		}

		c.FactoryDevices = append(c.FactoryDevices, record)
		if saveBundle != nil {
			return saveBundle(b)
		}
		return nil
	})
	if e != nil {
		return FactoryBundle{}, e
	}
	return b, nil
}

type configContextKey struct{}

func requestConfig(r *http.Request) Config {
	c, _ := r.Context().Value(configContextKey{}).(Config)
	return c
}
func (s *Server) snapshot() (Config, error) {
	if s.opts.ConfigPath != "" {
		return LoadConfig(s.opts.ConfigPath)
	}
	return s.cfg, nil
}
func loopback(r *http.Request) bool {
	h, _, e := net.SplitHostPort(r.RemoteAddr)
	return e == nil && net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback()
}
func (s *Server) publicRequest(r *http.Request) bool {
	return s.opts.PublicOrigin == onboardingOrigin && loopback(r) && r.Host == "robot.songguoxue.com" && r.Header.Get("X-Forwarded-Proto") == "https"
}

// This is only a source/CSRF compatibility check. Family credentials and every
// operation's authorization remain required; the Referer never grants identity.
func wechatOnboardingRequest(r *http.Request) bool {
	allowed := r.Method == "POST" && (r.URL.Path == "/api/family/login" || r.URL.Path == "/api/family/logout" || r.URL.Path == "/api/family/onboarding")
	if r.Method == "PUT" {
		for _, prefix := range []string{"/api/family/device-runtime/", "/api/family/device-settings/"} {
			if strings.HasPrefix(r.URL.Path, prefix) && device.MatchString(strings.TrimPrefix(r.URL.Path, prefix)) && r.URL.RawQuery == "" {
				allowed = true
			}
		}
	}
	if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/family/memory-clear/") && device.MatchString(strings.TrimPrefix(r.URL.Path, "/api/family/memory-clear/")) && r.URL.RawQuery == "" {
		allowed = true
	}
	const cancelPrefix = "/api/family/onboarding/claim_"
	if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, cancelPrefix) {
		allowed = claimCode.MatchString(strings.TrimPrefix(r.URL.Path, cancelPrefix))
	}
	if !allowed {
		return false
	}
	u, e := url.Parse(r.Referer())
	if e != nil || u.Scheme != "https" || u.Host != "servicewechat.com" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] != onboardingMiniProgramID || parts[3] != "page-frame.html" {
		return false
	}
	if parts[2] == "devtools" {
		return true
	}
	if len(parts[2]) == 0 || len(parts[2]) > 10 {
		return false
	}
	for _, c := range parts[2] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *Server) checkOrigin(r *http.Request) bool {
	public := s.publicRequest(r)
	if r.Header.Get("X-Forwarded-Proto") != "" && !public {
		return false
	}
	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
		return true
	}
	raw := r.Header.Get("Origin")
	if public {
		// Native wx.request has no Origin and can carry cross-site Fetch
		// Metadata. Only this mini-program's exact platform page is accepted.
		if raw == "" && wechatOnboardingRequest(r) {
			return true
		}
		return (raw == "" || raw == onboardingOrigin) && r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	if s.origin.Check(r) != nil {
		return false
	}
	if raw == "" {
		return true
	}
	u, e := url.Parse(raw)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return e == nil && u.Scheme == scheme && u.Host == r.Host && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == ""
}
