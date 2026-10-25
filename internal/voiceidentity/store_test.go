package voiceidentity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryDurabilityPermissionsAndFailedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	d, secret, e := s.Enroll("mac_native")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), secret) {
		t.Fatal("plaintext credential stored")
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal("credential registry permissions")
	}
	loaded, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := loaded.Authenticate(d.ID, secret); e != nil || got.ID != d.ID {
		t.Fatal("lost identity after reopen")
	}
	for _, v := range []struct{ id, key string }{{d.ID, "wrong"}, {"dev_unknown", secret}} {
		if _, e := loaded.Authenticate(v.id, v.key); e == nil {
			t.Fatal("forged authentication")
		}
	}
	if _, _, e := s.Enroll("esp32"); e == nil {
		t.Fatal("unsupported enrollment")
	}
	// Atomic rename fails, and the still-valid registered identity is retained.
	s.path = filepath.Dir(path)
	if _, _, e := s.Enroll("mac_fixture"); e == nil {
		t.Fatal("failed save accepted")
	}
	if len(s.devices) != 1 {
		t.Fatal("failed enrollment mutated registry")
	}
	if _, e := s.Authenticate(d.ID, secret); e != nil {
		t.Fatal("existing device lost")
	}
}

func TestRegistryRejectsSymlinkAndNonPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	if e := os.WriteFile(path, []byte(`{"schema_version":1,"devices":[]}`), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := Open(path); e == nil {
		t.Fatal("public registry accepted")
	}
	os.Chmod(path, 0600)
	link := filepath.Join(dir, "alias.json")
	os.Symlink(path, link)
	if _, e := Open(link); e == nil {
		t.Fatal("symlink registry accepted")
	}
}

func TestXiaozhiIsRecognizedButCannotSelfEnroll(t *testing.T) {
	if !ValidClient("xiaozhi") {
		t.Fatal("trusted Xiaozhi identity is unsupported")
	}
	s, err := Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Enroll("xiaozhi"); err == nil {
		t.Fatal("device self-enrollment accepted")
	}
}

func TestTrustedXiaozhiBindingPersistsAndCannotChangeCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s, _ := Open(path)
	secret := strings.Repeat("a", 64)
	a, err := s.BindXiaozhi("AA:BB:CC:DD:EE:01", secret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.BindXiaozhi("aa:bb:cc:dd:ee:01", secret)
	if err != nil || a.ID != b.ID || a.ClientType != "xiaozhi" {
		t.Fatal("unstable device", err)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := loaded.BindXiaozhi("aa:bb:cc:dd:ee:01", secret)
	if err != nil || a.ID != c.ID {
		t.Fatal("binding lost on restart", err)
	}
	if _, err := loaded.BindXiaozhi("aa:bb:cc:dd:ee:01", strings.Repeat("b", 64)); err == nil {
		t.Fatal("silently replaced credential")
	}
	if _, err := loaded.BindXiaozhi("chosen-id", secret); err == nil {
		t.Fatal("non-MAC accepted")
	}
	if _, err := loaded.BindXiaozhi("aa:bb:cc:dd:ee:02", "short"); err == nil {
		t.Fatal("weak credential accepted")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), secret) {
		t.Fatal("plaintext token retained")
	}
}
