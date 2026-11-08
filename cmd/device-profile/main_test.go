package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIGetSetAndFailureDoNotChangeWakeWordOrCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	auth := filepath.Join(dir, "devices.json")
	os.WriteFile(auth, []byte("auth sentinel"), 0600)
	id := "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	invoke := func(args ...string) (int, map[string]any) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := run(args, &out, &stderr)
		var result map[string]any
		if out.Len() > 0 && json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal(out.String())
		}
		return code, result
	}
	code, p := invoke("--profiles", path, "--default-name", "小满", "get", id)
	if code != 0 || p["name"] != "小满" || p["revision"] != float64(0) || p["effective_wake_word"] != "unknown" {
		t.Fatalf("fallback: %d %v", code, p)
	}
	code, p = invoke("--profiles", path, "set", id, "豆豆")
	if code != 0 || p["name"] != "豆豆" || p["revision"] != float64(1) || p["effective_wake_word"] != "unknown" {
		t.Fatalf("set: %d %v", code, p)
	}
	code, p = invoke("--profiles", path, "--default-name", "小满", "get", id)
	if code != 0 || p["name"] != "豆豆" {
		t.Fatal(code, p)
	}
	before, _ := os.ReadFile(path)
	code, _ = invoke("--profiles", path, "set", id, "bad\nname")
	after, _ := os.ReadFile(path)
	if code == 0 || !bytes.Equal(before, after) {
		t.Fatal("invalid name wrote file")
	}
	sentinel, _ := os.ReadFile(auth)
	if string(sentinel) != "auth sentinel" {
		t.Fatal("auth registry changed")
	}
}

func TestCLIGetUsesConfiguredProductDefaultAndSetNeedsNoIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	identity := filepath.Join(dir, "identity.json")
	raw := `{"schema_version":1,"name":"阿团","role":"家庭助手","style":"自然","default_language":"中文","developer":null,"owner":null}`
	if err := os.WriteFile(identity, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	id := "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var out, stderr bytes.Buffer
	code := run([]string{"--profiles", path, "--identity", identity, "get", id}, &out, &stderr)
	var p map[string]any
	if code != 0 || json.Unmarshal(out.Bytes(), &p) != nil || p["name"] != "阿团" || p["effective_wake_word"] != "unknown" {
		t.Fatalf("configured default: code=%d out=%s err=%s", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	code = run([]string{"--profiles", path, "--identity", filepath.Join(dir, "missing.json"), "set", id, "花花"}, &out, &stderr)
	if code != 0 {
		t.Fatalf("set required unrelated identity: %s", stderr.String())
	}
}
