package realtimelive

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func memoryOnlyConfig(t *testing.T) Config {
	t.Helper()
	cfg := Config{}
	field := reflect.ValueOf(&cfg).Elem().FieldByName("MemoryOnly")
	if !field.IsValid() {
		t.Fatal("MemoryOnly recording policy not implemented")
	}
	field.SetBool(true)
	return cfg
}

func TestMemoryOnlyRecorderSuppressesBody(t *testing.T) {
	cfg := memoryOnlyConfig(t)
	server, _ := testSocket(t)
	dir := t.TempDir()
	s := NewSession(server, cfg, dir, nil)
	defer s.cancel()
	if err := s.record("user_text", map[string]any{"text": "private voice test sentinel"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("body persisted: %v %v", entries, err)
	}
	// Ordinary product sessions retain their existing text archive.
	plain := NewSession(server, Config{}, t.TempDir(), nil)
	defer plain.cancel()
	if err := plain.record("user_text", map[string]any{"text": "ordinary archive"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plain.directory, "events.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryOnlyNativeIntentDoesNotWrite(t *testing.T) {
	cfg := memoryOnlyConfig(t)
	cfg.URL = "ws://127.0.0.1:1"
	dir := t.TempDir()
	n := NewNative(cfg, dir)
	_ = n.Open(context.Background(), "private prompt sentinel", false)
	n.Close()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("intent persisted in memory-only mode: %v %v", entries, err)
	}
}

func TestMemoryOnlyServerRejectsCrossSessionMemory(t *testing.T) {
	cfg := memoryOnlyConfig(t)
	cfg.MemoryEnabled = true
	s := NewServer(cfg, nil, t.TempDir(), "test")
	if s.InitializationError() == nil {
		t.Fatal("memory-only server accepted persistent memory")
	}
}
