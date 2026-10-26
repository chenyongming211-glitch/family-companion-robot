// Package deviceauth reads a bounded, private allowlist once at startup. It
// neither enrolls devices nor writes credentials; each returned entry is a value.
package deviceauth

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
)

var ErrRegistry = errors.New("device_auth_registry_invalid")

const maxBytes = 1 << 20
const maxDevices = 64

type Entry struct {
	MAC   string `json:"mac"`
	Token string `json:"token"`
}
type Registry struct{ devices map[string]Entry }

func canonicalMAC(value string) (string, bool) {
	m, err := net.ParseMAC(value)
	if err != nil || len(m) != 6 {
		return "", false
	}
	return m.String(), true
}
func validToken(value string) bool {
	return len(value) >= 32 && len(value) <= 512 && !strings.ContainsAny(value, "\x00\r\n \t")
}
func Single(mac, token string) (*Registry, error) {
	canonical, ok := canonicalMAC(mac)
	if !ok || !validToken(token) {
		return nil, ErrRegistry
	}
	return &Registry{devices: map[string]Entry{canonical: {MAC: canonical, Token: token}}}, nil
}

// Load requires the existing primary device to be present with its unchanged
// secret, so enabling a registry cannot silently replace its stable binding.
func Load(path, primaryMAC, primaryToken string) (*Registry, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0177 != 0 || st.Size() > maxBytes {
		return nil, ErrRegistry
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrRegistry
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(st, opened) || opened.Mode().Perm()&0177 != 0 {
		return nil, ErrRegistry
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(raw) > maxBytes {
		return nil, ErrRegistry
	}
	var disk struct {
		Version int     `json:"schema_version"`
		Devices []Entry `json:"devices"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&disk) != nil || uniqueKeys(json.NewDecoder(bytes.NewReader(raw)), 0) != nil || disk.Version != 1 || len(disk.Devices) == 0 || len(disk.Devices) > maxDevices {
		return nil, ErrRegistry
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		return nil, ErrRegistry
	}
	r := &Registry{devices: make(map[string]Entry, len(disk.Devices))}
	tokens := map[string]bool{}
	for _, entry := range disk.Devices {
		mac, ok := canonicalMAC(entry.MAC)
		if !ok || !validToken(entry.Token) || tokens[entry.Token] {
			return nil, ErrRegistry
		}
		if _, exists := r.devices[mac]; exists {
			return nil, ErrRegistry
		}
		entry.MAC = mac
		r.devices[mac] = entry
		tokens[entry.Token] = true
	}
	if _, ok := r.Authenticate(primaryMAC, primaryToken); !ok {
		return nil, ErrRegistry
	}
	return r, nil
}
func (r *Registry) Lookup(mac string) (Entry, bool) {
	canonical, ok := canonicalMAC(mac)
	if !ok || r == nil {
		return Entry{}, false
	}
	entry, ok := r.devices[canonical]
	return entry, ok
}
func (r *Registry) Authenticate(mac, token string) (Entry, bool) {
	entry, ok := r.Lookup(mac)
	if !ok || subtle.ConstantTimeCompare([]byte(entry.Token), []byte(token)) != 1 {
		return Entry{}, false
	}
	return entry, true
}

// encoding/json normally accepts repeated object keys using the last value.
// A protected credential document must have only one interpretation.
func uniqueKeys(dec *json.Decoder, depth int) error {
	if depth > 4 {
		return ErrRegistry
	}
	token, err := dec.Token()
	if err != nil {
		return ErrRegistry
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return ErrRegistry
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrRegistry
			}
			seen[name] = true
			if uniqueKeys(dec, depth+1) != nil {
				return ErrRegistry
			}
		}
	case '[':
		for dec.More() {
			if uniqueKeys(dec, depth+1) != nil {
				return ErrRegistry
			}
		}
	default:
		return ErrRegistry
	}
	if _, err := dec.Token(); err != nil {
		return ErrRegistry
	}
	return nil
}
