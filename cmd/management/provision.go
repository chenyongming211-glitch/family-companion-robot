package main

import (
	"bytes"
	"encoding/json"
	"familyrobot/internal/management"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
)

func provision(args []string, out io.Writer) error {
	f := flags("provision")
	config := f.String("config", "", "existing private config")
	mac := f.String("mac", "", "canonical MAC")
	serial := f.String("serial", "", "unique serial")
	model := f.String("model", "", "S3 model")
	output := f.String("output", "", "new private factory bundle")
	guard := f.String("device-auth", "", "required private legacy device registry")
	if f.Parse(args) != nil || f.NArg() != 0 || *config == "" || *output == "" || *guard == "" {
		return management.ErrInvalid
	}
	if e := checkLegacyMAC(*guard, *mac); e != nil {
		return e
	}
	// Reserve the private bundle first: a failed transaction never exposes secrets.
	dir := filepath.Dir(*output)
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return management.ErrInvalid
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return management.ErrSource
	}
	defer root.Close()
	file, e := root.OpenFile(filepath.Base(*output), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return management.ErrSource
	}
	keep, saved := false, false
	var b management.FactoryBundle
	defer func() {
		file.Close()
		if keep {
			return
		}
		if saved {
			// A failed directory fsync can follow a successful rename. Never destroy
			// the only usable key while the authority outcome is ambiguous.
			c, e := management.LoadConfig(*config)
			if e != nil {
				return
			}
			for _, d := range c.FactoryDevices {
				if d.DeviceID == b.DeviceID {
					return
				}
			}
		}
		_ = root.Remove(filepath.Base(*output))
	}()
	_, e = management.ProvisionFactory(*config, *mac, *serial, *model, func(bundle management.FactoryBundle) error {
		b = bundle
		if e := persistFactoryBundle(file, dir, bundle); e != nil {
			return e
		}
		saved = true
		return nil
	})
	if e != nil {
		return e
	}
	keep = true
	_, e = io.WriteString(out, "工厂设备已供应；私有包已保存。\n")
	return e
}
func grantDeviceAdd(args []string, out io.Writer) error {
	f := flags("grant-device-add")
	config := f.String("config", "", "existing private config")
	account := f.String("account", "", "account ID")
	family := f.String("family", "", "family ID")
	if f.Parse(args) != nil || f.NArg() != 0 || *config == "" || *account == "" || *family == "" {
		return management.ErrInvalid
	}
	e := (management.ConfigStore{Path: *config}).Transaction(func(c *management.Config) error {
		for ai := range c.Accounts {
			a := &c.Accounts[ai]
			if a.ID != *account {
				continue
			}
			for mi := range a.Memberships {
				if a.Memberships[mi].FamilyID == *family {
					a.Memberships[mi].CanAddDevices = true
					return nil
				}
			}
		}
		return management.ErrNotFound
	})
	if e != nil {
		return e
	}
	_, e = io.WriteString(out, "指定家庭成员已获添加设备权限。\n")
	return e
}

// The registry is inspected only on the cloud and never logged or returned.
// An explicit empty registry supports new deployments without silently skipping
// the ownership guard on deployments that already have legacy devices.
func checkLegacyMAC(path, mac string) error {
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 1<<20 {
		return management.ErrInvalid
	}
	file, e := os.Open(path)
	if e != nil {
		return management.ErrSource
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil || !os.SameFile(st, opened) {
		return management.ErrInvalid
	}
	raw, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return management.ErrInvalid
	}
	var registry struct {
		Version int `json:"schema_version"`
		Devices []struct {
			MAC   string `json:"mac"`
			Token string `json:"token"`
		} `json:"devices"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&registry) != nil || registry.Version != 1 || len(registry.Devices) > 64 || uniqueRegistryKeys(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return management.ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return management.ErrInvalid
	}
	candidate, e := net.ParseMAC(mac)
	if e != nil || len(candidate) != 6 {
		return management.ErrInvalid
	}
	seen := map[string]bool{}
	tokens := map[string]bool{}
	tokenValid := regexp.MustCompile(`^[^\x00\r\n \t]{32,512}$`)
	for _, d := range registry.Devices {
		m, e := net.ParseMAC(d.MAC)
		if e != nil || len(m) != 6 || seen[m.String()] || tokens[d.Token] || !tokenValid.MatchString(d.Token) {
			return management.ErrInvalid
		}
		if m.String() == candidate.String() {
			return management.ErrInvalid
		}
		seen[m.String()] = true
		tokens[d.Token] = true
	}
	return nil
}
func uniqueRegistryKeys(d *json.Decoder, depth int) error {
	if depth > 5 {
		return management.ErrInvalid
	}
	token, e := d.Token()
	if e != nil {
		return management.ErrInvalid
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			key, ok := k.(string)
			if e != nil || !ok || seen[key] {
				return management.ErrInvalid
			}
			seen[key] = true
			if e = uniqueRegistryKeys(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = uniqueRegistryKeys(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return management.ErrInvalid
	}
	_, e = d.Token()
	return e
}

func persistFactoryBundle(file *os.File, dir string, b management.FactoryBundle) error {
	if e := json.NewEncoder(file).Encode(b); e != nil {
		return management.ErrSource
	}
	if e := file.Sync(); e != nil {
		return management.ErrSource
	}
	if e := file.Close(); e != nil {
		return management.ErrSource
	}
	parent, e := os.Open(dir)
	if e != nil {
		return management.ErrSource
	}
	defer parent.Close()
	if e = parent.Sync(); e != nil {
		return management.ErrSource
	}
	return nil
}
