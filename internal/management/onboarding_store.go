package management

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

type FactoryDevice struct {
	DeviceID    string `json:"device_id"`
	MAC         string `json:"mac"`
	Serial      string `json:"serial"`
	Model       string `json:"model"`
	KeyDigest   string `json:"key_digest"`
	ClaimDigest string `json:"claim_digest"`
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
				if v.DeviceID == t.DeviceID && v.FamilyID == t.FamilyID && v.From.Equal(*t.BindingFrom) {
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
			if old.ID == d.DeviceID && !confirmed[d.DeviceID] {
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

// ProvisionFactory commits authority only after saveBundle has durably saved
// the private package. A save error aborts the transaction before config write.
// Callers must preserve a saved bundle if the final config fsync is ambiguous.
func ProvisionFactory(path, mac, serial, model string, saveBundle func(FactoryBundle) error) (FactoryBundle, error) {
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
	b = FactoryBundle{1, "dev_" + id, mac, serial, model, base64.RawURLEncoding.EncodeToString(key), code, onboardingOrigin + "/api/device/bootstrap", "wss://robot.songguoxue.com/xiaozhi/v1/"}
	e = (ConfigStore{path}).Transaction(func(c *Config) error {
		for _, d := range c.FactoryDevices {
			if d.MAC == mac || d.Serial == serial {
				return ErrInvalid
			}
		}
		for _, d := range c.Devices {
			if d.ID == b.DeviceID {
				return ErrInvalid
			}
		}

		c.FactoryDevices = append(c.FactoryDevices, FactoryDevice{b.DeviceID, mac, serial, model, secretDigest(b.DeviceKey), secretDigest(code)})
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
