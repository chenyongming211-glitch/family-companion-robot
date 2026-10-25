// Package voiceidentity binds a test terminal to a server-issued credential.
// Enrollment is only for the existing trusted loopback/SSH boundary.
package voiceidentity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var ErrIdentity = errors.New("invalid_device_identity")
var idPattern = regexp.MustCompile(`^dev_[a-f0-9]{32}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var VersionPattern = regexp.MustCompile(`^[A-Za-z0-9._+-]{0,64}$`)

type Device struct {
	ID         string `json:"device_id"`
	ClientType string `json:"client_type"`
	Digest     string `json:"credential_sha256"`
	ExternalID string `json:"external_id,omitempty"`
}
type diskStore struct {
	Version int      `json:"schema_version"`
	Devices []Device `json:"devices"`
}
type Store struct {
	mu      sync.Mutex
	path    string
	devices map[string]Device
}

func ValidEnrollmentClient(kind string) bool { return kind == "mac_native" || kind == "mac_fixture" }
func ValidClient(kind string) bool           { return ValidEnrollmentClient(kind) || kind == "xiaozhi" }
func Open(path string) (*Store, error) {
	s := &Store{path: path, devices: map[string]Device{}}
	st, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) {
		return s, nil
	}
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 2<<20 {
		return nil, ErrIdentity
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var disk diskStore
	if json.Unmarshal(raw, &disk) != nil || disk.Version != 1 || len(disk.Devices) > 1024 {
		return nil, ErrIdentity
	}
	external := map[string]bool{}
	for _, d := range disk.Devices {
		if !idPattern.MatchString(d.ID) || !ValidClient(d.ClientType) || !digestPattern.MatchString(d.Digest) {
			return nil, ErrIdentity
		}
		if _, exists := s.devices[d.ID]; exists {
			return nil, ErrIdentity
		}
		if d.ClientType == "xiaozhi" {
			mac, err := net.ParseMAC(d.ExternalID)
			if err != nil || len(mac) != 6 || mac.String() != d.ExternalID || external[d.ExternalID] {
				return nil, ErrIdentity
			}
			external[d.ExternalID] = true
		} else if d.ExternalID != "" {
			return nil, ErrIdentity
		}
		s.devices[d.ID] = d
	}
	return s, nil
}
func (s *Store) Enroll(kind string) (Device, string, error) {
	if !ValidEnrollmentClient(kind) {
		return Device{}, "", ErrIdentity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.devices) >= 1024 {
		return Device{}, "", errors.New("device_registry_full")
	}
	var id [16]byte
	var key [32]byte
	if _, e := rand.Read(id[:]); e != nil {
		return Device{}, "", e
	}
	if _, e := rand.Read(key[:]); e != nil {
		return Device{}, "", e
	}
	secret := base64.RawURLEncoding.EncodeToString(key[:])
	sum := sha256.Sum256([]byte(secret))
	d := Device{ID: "dev_" + hex.EncodeToString(id[:]), ClientType: kind, Digest: hex.EncodeToString(sum[:])}
	if e := s.saveDevice(d); e != nil {
		return Device{}, "", e
	}
	return d, secret, nil
}

// BindXiaozhi is called only after the configured token and MAC have both been
// authenticated. It is deliberately unavailable through public enrollment.
func (s *Store) BindXiaozhi(externalID, secret string) (Device, error) {
	mac, err := net.ParseMAC(externalID)
	if err != nil || len(mac) != 6 || len(secret) < 32 || len(secret) > 512 || strings.ContainsAny(secret, "\x00\r\n") {
		return Device{}, ErrIdentity
	}
	externalID = mac.String()
	sum := sha256.Sum256([]byte(secret))
	digest := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.ClientType == "xiaozhi" && d.ExternalID == externalID {
			if subtle.ConstantTimeCompare([]byte(d.Digest), []byte(digest)) != 1 {
				return Device{}, ErrIdentity
			}
			return d, nil
		}
	}
	if len(s.devices) >= 1024 {
		return Device{}, errors.New("device_registry_full")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Device{}, err
	}
	d := Device{ID: "dev_" + hex.EncodeToString(id[:]), ClientType: "xiaozhi", Digest: digest, ExternalID: externalID}
	if err := s.saveDevice(d); err != nil {
		return Device{}, err
	}
	return d, nil
}

// Caller holds s.mu; a failed durable write must never mutate the live registry.
func (s *Store) saveDevice(d Device) error {
	disk := diskStore{Version: 1, Devices: make([]Device, 0, len(s.devices)+1)}
	for _, v := range s.devices {
		disk.Devices = append(disk.Devices, v)
	}
	disk.Devices = append(disk.Devices, d)
	if e := WritePrivateJSON(s.path, disk); e != nil {
		return e
	}
	s.devices[d.ID] = d
	return nil
}
func (s *Store) Authenticate(id, secret string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	sum := sha256.Sum256([]byte(secret))
	want, _ := hex.DecodeString(d.Digest)
	if !ok || len(secret) != 43 || subtle.ConstantTimeCompare(sum[:], want) != 1 {
		return Device{}, ErrIdentity
	}
	return d, nil
}

// WritePrivateJSON replaces a private file atomically; callers never publish a
// successful enrollment before its credential digest is durable.
func WritePrivateJSON(path string, value any) error {
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".private-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
