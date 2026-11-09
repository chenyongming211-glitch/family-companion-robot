package main

import (
	"bytes"
	"encoding/json"
	"familyrobot/internal/management"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitPrivateAndNoHistory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "private", "config.json")
	args := []string{"init", "--config", p, "--device-id", "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	var out bytes.Buffer
	before := time.Now()
	if e := run(args, &out); e != nil {
		t.Fatal(e)
	}
	var result struct {
		Username, Password string
		From               time.Time `json:"binding_from"`
	}
	if json.Unmarshal(out.Bytes(), &result) != nil || len(result.Password) < 40 || result.From.Before(before) || result.From.After(time.Now()) {
		t.Fatal("bad output")
	}
	c, e := management.LoadConfig(p)
	if e != nil || !management.VerifyPassword(result.Password, c.Accounts[0].Password) || !c.Bindings[0].From.Equal(result.From) {
		t.Fatal("config")
	}
	st, _ := os.Stat(filepath.Dir(p))
	if st.Mode().Perm() != 0700 {
		t.Fatal("parent perms")
	}
	out.Reset()
	if run(args, &out) == nil || out.Len() != 0 {
		t.Fatal("overwrite")
	}
	if run(append(args, "--from", "2000-01-01"), &out) == nil {
		t.Fatal("history flag")
	}
}
func TestServeValidation(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:18791", ":18791", "localhost:18791", "[::]:18791", "8.8.8.8:18791"} {
		if validListen(addr) {
			t.Fatal(addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:18791", "[::1]:18791"} {
		if !validListen(addr) {
			t.Fatal(addr)
		}
	}
	var b bytes.Buffer
	if run([]string{"serve", "--listen", "0.0.0.0:18791"}, &b) == nil {
		t.Fatal("serve")
	}
	if run([]string{"init", "--config", filepath.Join(t.TempDir(), "c"), "--device-id", "invalid"}, &b) == nil || b.Len() != 0 {
		t.Fatal("invalid")
	}
}
