package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"familyrobot/internal/management"
	"familyrobot/internal/voiceidentity"
	"io"
	"net"
	"os"
	"regexp"
	"syscall"
)

type legacyRegistry struct {
	Version int `json:"schema_version"`
	Devices []struct {
		MAC   string `json:"mac"`
		Token string `json:"token"`
	} `json:"devices"`
}

// Strict read-only input: refuse symlinks/FIFOs, public files, oversized JSON,
// unknown fields, duplicate object keys and trailing documents. Never enroll.
func readPrivateJSON(path string, max int64, value any) error {
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return management.ErrInvalid
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > max {
		return management.ErrInvalid
	}
	raw, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(raw)) > max {
		return management.ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(value) != nil || uniqueRegistryKeys(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return management.ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return management.ErrInvalid
	}
	return nil
}
func readLegacyRegistry(path string) (legacyRegistry, error) {
	var r legacyRegistry
	if e := readPrivateJSON(path, 1<<20, &r); e != nil {
		return r, e
	}
	if r.Version != 1 || len(r.Devices) > 64 {
		return r, management.ErrInvalid
	}
	seen, tokens := map[string]bool{}, map[string]bool{}
	validToken := regexp.MustCompile(`^[^\x00\r\n \t]{32,512}$`)
	for i, d := range r.Devices {
		m, e := net.ParseMAC(d.MAC)
		if e != nil || len(m) != 6 || seen[m.String()] || tokens[d.Token] || !validToken.MatchString(d.Token) {
			return legacyRegistry{}, management.ErrInvalid
		}
		r.Devices[i].MAC = m.String()
		seen[m.String()] = true
		tokens[d.Token] = true
	}
	return r, nil
}
func checkLegacyMigration(registryPath, identitiesPath, deviceID, mac string) error {
	candidate, e := net.ParseMAC(mac)
	if e != nil || len(candidate) != 6 {
		return management.ErrInvalid
	}
	registry, e := readLegacyRegistry(registryPath)
	if e != nil {
		return e
	}
	token := ""
	for _, d := range registry.Devices {
		if d.MAC == candidate.String() {
			token = d.Token
		}
	}
	if token == "" {
		return management.ErrInvalid
	}
	var disk struct {
		Version int                    `json:"schema_version"`
		Devices []voiceidentity.Device `json:"devices"`
	}
	if e := readPrivateJSON(identitiesPath, 2<<20, &disk); e != nil {
		return e
	}
	if disk.Version != 1 || len(disk.Devices) > 1024 {
		return management.ErrInvalid
	}
	seen, external := map[string]bool{}, map[string]bool{}
	validID := regexp.MustCompile(`^dev_[a-f0-9]{32}$`)
	validDigest := regexp.MustCompile(`^[a-f0-9]{64}$`)
	sum := sha256.Sum256([]byte(token))
	want := hex.EncodeToString(sum[:])
	found := false
	for _, d := range disk.Devices {
		if !validID.MatchString(d.ID) || seen[d.ID] || !voiceidentity.ValidClient(d.ClientType) || !validDigest.MatchString(d.Digest) {
			return management.ErrInvalid
		}
		seen[d.ID] = true
		if d.ClientType == "xiaozhi" {
			m, e := net.ParseMAC(d.ExternalID)
			if e != nil || len(m) != 6 || m.String() != d.ExternalID || external[d.ExternalID] {
				return management.ErrInvalid
			}
			external[d.ExternalID] = true
		} else if d.ExternalID != "" {
			return management.ErrInvalid
		}
		if d.ID == deviceID {
			if d.ClientType != "xiaozhi" || d.ExternalID != candidate.String() || subtle.ConstantTimeCompare([]byte(d.Digest), []byte(want)) != 1 {
				return management.ErrInvalid
			}
			found = true
		}
	}
	if !found {
		return management.ErrInvalid
	}
	return nil
}
