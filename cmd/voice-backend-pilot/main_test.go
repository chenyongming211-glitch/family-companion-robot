package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisabledBeforeReadingConfig(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); err == nil || err.Error() != "provider_calls_disabled" {
		t.Fatalf("default must not read config or call provider: %v", err)
	}
}

func TestInvalidOptionsBeforeReadingConfig(t *testing.T) {
	for _, args := range [][]string{
		{"--allow-provider-calls", "--backend=device"},
		{"--allow-provider-calls", "--backend=tts", "--text=你好"},
		{"--allow-provider-calls", "--backend=native", "--text=你好"},
		{"--allow-provider-calls", "--budget-fen=101"},
	} {
		var out bytes.Buffer
		if err := run(args, &out); err == nil || strings.Contains(err.Error(), "config") {
			t.Fatalf("invalid options reached config: %v: %v", args, err)
		}
	}
}

func TestBudgetReservationsAreNotRefunded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cost.jsonl")
	if err := reserveBudget(p, "tts", .1, 20); err != nil {
		t.Fatal(err)
	}
	if err := recordOutcome(p, "tts", map[string]any{"characters": 2}, false); err != nil {
		t.Fatal(err)
	}
	if err := reserveBudget(p, "native", .1, 20); err != nil {
		t.Fatal(err)
	}
	if err := recordOutcome(p, "native", map[string]any{"total_tokens": 2}, false); err != nil {
		t.Fatal(err)
	}
	if err := reserveBudget(p, "tts", .01, 20); err == nil {
		t.Fatal("unknown outcome refunded budget")
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("private cost file: %v %v", st, err)
	}
}

func TestUnknownOrUnfinishedAPIPreventsFurtherCalls(t *testing.T) {
	for _, finish := range []bool{false, true} {
		p := filepath.Join(t.TempDir(), "cost.jsonl")
		if err := reserveBudget(p, "tts", .1, 100); err != nil {
			t.Fatal(err)
		}
		if finish {
			if err := recordOutcome(p, "tts", nil, true); err != nil {
				t.Fatal(err)
			}
		}
		if err := reserveBudget(p, "native", .1, 100); err == nil {
			t.Fatal("continued after unknown call")
		}
	}
}

func TestEachBackendRunsAtMostOnce(t *testing.T) {
	for _, backend := range []string{"native", "cascade"} {
		p := filepath.Join(t.TempDir(), "attempts.jsonl")
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := reserveGlobal(p, backend, "quiet-voice-20261114/first"); err != nil {
			t.Fatal(err)
		}
		if err := reserveGlobal(p, backend, "quiet-voice-20261114/second"); err == nil {
			t.Fatal("same backend invoked twice")
		}
	}
}

func TestBudgetCorruptionFailsClosed(t *testing.T) {
	for _, body := range []string{"broken\n", `{"event":"intent","reserved_microyuan":100000}`} {
		p := filepath.Join(t.TempDir(), "cost.jsonl")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := reserveBudget(p, "tts", .1, 100); err == nil {
			t.Fatal("accepted corrupt or unfinished costs")
		}
	}
}

func TestGlobalLedgerCountsIntentsAndRejectsDuplicate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "attempts.jsonl")
	if err := os.WriteFile(p, []byte(`{"event":"result","stage":"protocol","attempt":"old"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reserveGlobal(p, "tts", "quiet-voice-20261114/tts-1"); err != nil {
		t.Fatal(err)
	}
	if err := reserveGlobal(p, "tts", "quiet-voice-20261114/tts-1"); err == nil {
		t.Fatal("duplicate retried")
	}
	if err := reserveGlobal(p, "tts", "quiet-voice-20261114/tts-2"); err != nil {
		t.Fatal(err)
	}
	if err := reserveGlobal(p, "tts", "quiet-voice-20261114/tts-3"); err == nil {
		t.Fatal("exceeded quiet phase allowance")
	}
}

func TestGlobalLedgerHonorsOriginalLimits(t *testing.T) {
	for _, tc := range []struct {
		backend, stage string
		count          int
	}{{"tts", "protocol", 8}, {"native", "primary", 48}} {
		p := filepath.Join(t.TempDir(), "attempts.jsonl")
		var body bytes.Buffer
		for i := 0; i < tc.count; i++ {
			if err := json.NewEncoder(&body).Encode(map[string]any{"event": "intent", "stage": tc.stage, "attempt": strings.Repeat("x", i+1)}); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(p, body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if err := reserveGlobal(p, tc.backend, "quiet-voice-20261114/new"); err == nil {
			t.Fatal("original budget bypassed")
		}
	}
}

func TestInputRejectsOversizeAndPartialSamples(t *testing.T) {
	for _, n := range []int{0, 3, 320002} {
		p := filepath.Join(t.TempDir(), "input.pcm")
		if err := os.WriteFile(p, make([]byte, n), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readInput(p); err == nil {
			t.Fatalf("accepted %d bytes", n)
		}
	}
}

func TestInputRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "source.pcm")
	link := filepath.Join(dir, "alias.pcm")
	if err := os.WriteFile(target, []byte{0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readInput(link); err == nil {
		t.Fatal("accepted indirect input")
	}
}
