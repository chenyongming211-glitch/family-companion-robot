package realtimestate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProductStartupCopyAndInstructions(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "product.json")
	raw := `{"schema_version":1,"name":"小满","role":"AI家庭陪伴助手","style":"自然","default_language":"中文","developer":null,"owner":null}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	product, err := LoadProduct(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"QWEN_REALTIME_URL": "wss://private.invalid/?model=qwen-test", "DASHSCOPE_API_KEY": "secret", "enable_search": true}
	snapshot, err := product.Snapshot(cfg, ContextFor(nil), true)
	if err != nil {
		t.Fatal(err)
	}
	snapshot["product"].(map[string]any)["name"] = "wrong"
	text, err := product.InstructionsFor(cfg, ContextFor(nil), true, time.Date(2026, 10, 25, 5, 14, 15, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "你是小满") || !strings.Contains(text, "2026-10-25 13:14:15") || strings.Contains(text, "wrong") || strings.Contains(text, "secret") || strings.Contains(text, "private.invalid") {
		t.Fatal(text)
	}
	if _, err := LoadProduct(path); err == nil {
		t.Fatal("invalid product accepted")
	}
}
