package management

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func memoryFixture(t *testing.T) (Reader, string, map[string]any) {
	t.Helper()
	cfg := authorityFixtureConfig()
	r := Reader{Config: cfg, Root: t.TempDir(), Now: func() time.Time { return cfg.Bindings[0].From.Add(time.Hour) }}
	device := r.Config.Devices[0].ID
	at := r.Config.Bindings[0].From.Add(time.Minute)
	info := realtimelive.SessionInfo{SchemaVersion: 1, Ownership: realtimelive.Ownership{SessionID: "0123456789abcdef0123456789abcdef", DeviceID: device, ClientType: "xiaozhi"}, StartedAt: at}
	if e := os.Mkdir(filepath.Join(r.Root, "source-a"), 0700); e != nil {
		t.Fatal(e)
	}
	writeMemoryFixture(t, filepath.Join(r.Root, "source-a", "session.json"), info)
	// readerFixture uses the constant session ID below.
	turn := map[string]any{"session_id": "0123456789abcdef0123456789abcdef", "turn_id": 1, "at": at.Add(time.Minute), "user": "合成问题", "assistant_spoken": "合成回复", "playback_boundary": "complete_response", "text_source": "output_asr"}
	snapshot := map[string]any{"version": 1, "updated_at": r.now(), "data": map[string]any{"turns": []any{turn}}}
	dir := filepath.Join(r.Root, ".memory")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256([]byte(device))
	p := filepath.Join(dir, hex.EncodeToString(hash[:])+".json")
	writeMemoryFixture(t, p, snapshot)
	return r, p, snapshot
}
func writeMemoryFixture(t *testing.T, p string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestMemoriesAuthorization(t *testing.T) {
	r, _, _ := memoryFixture(t)
	d := r.Config.Devices[0].ID
	items, e := r.ListMemories("alice", "family", d)
	if e != nil || len(items) != 1 || items[0].ConversationID != "source-a" {
		t.Fatalf("%+v %v", items, e)
	}
	for _, a := range []string{"bob", "observer", "metadata"} {
		if _, e = r.ListMemories(a, "family", d); !errors.Is(e, ErrForbidden) {
			t.Fatalf("%s %v", a, e)
		}
	}
	r.Root = "/missing-synthetic-root"
	if _, e = r.ListMemories("metadata", "family", d); !errors.Is(e, ErrForbidden) {
		t.Fatal("read before body grant", e)
	}
}

func TestMemorySnapshotValidationAndReadOnly(t *testing.T) {
	for _, kind := range []string{"valid", "duplicate-key", "unknown-field", "version", "timestamp", "future-snapshot", "duplicate-turn", "turn-limit", "user-limit", "assistant-limit", "total-limit", "oversized", "null", "trailing", "symlink", "directory", "missing-file", "missing-directory", "expired", "unknown-session", "before-source", "before-binding", "other-device", "old-binding"} {
		t.Run(kind, func(t *testing.T) {
			r, p, s := memoryFixture(t)
			d := r.Config.Devices[0].ID
			data := s["data"].(map[string]any)
			turn := data["turns"].([]any)[0].(map[string]any)
			want := ErrSource
			count := 0
			switch kind {
			case "valid":
				want = nil
				count = 1
			case "unknown-field":
				s["unexpected"] = true
			case "version":
				s["version"] = 2
			case "timestamp":
				turn["at"] = "bad"
			case "future-snapshot":
				s["updated_at"] = r.now().Add(time.Second)
			case "duplicate-turn":
				data["turns"] = []any{turn, turn}
			case "turn-limit":
				data["turns"] = make([]any, 25)
				want = ErrTooLarge
			case "user-limit":
				turn["user"] = strings.Repeat("字", 513)
				want = ErrTooLarge
			case "assistant-limit":
				turn["assistant_spoken"] = strings.Repeat("字", 1025)
				want = ErrTooLarge
			case "total-limit":
				turns := []any{}
				for i := 1; i <= 24; i++ {
					v := map[string]any{}
					for k, x := range turn {
						v[k] = x
					}
					v["turn_id"] = i
					v["user"] = strings.Repeat("字", 512)
					v["assistant_spoken"] = strings.Repeat("字", 1024)
					turns = append(turns, v)
				}
				data["turns"] = turns
				want = ErrTooLarge
			case "expired":
				r.Now = func() time.Time { return r.Config.Bindings[0].From.Add(8 * 24 * time.Hour) }
				want = nil
			case "unknown-session":
				turn["session_id"] = strings.Repeat("f", 32)
				want = nil
			case "before-source":
				turn["at"] = r.Config.Bindings[0].From.Add(time.Second)
				want = nil
			case "before-binding":
				turn["at"] = r.Config.Bindings[0].From.Add(-time.Second)
				want = nil
			case "old-binding":
				end := r.Config.Bindings[0].From.Add(30 * time.Minute)
				old := r.Config.Bindings[0]
				old.Until = &end
				r.Config.Bindings[0].From = end
				r.Config.Bindings[0].Version = 2
				r.Config.Bindings = append(r.Config.Bindings, old)
				want = nil
			case "other-device":
				raw, e := os.ReadFile(filepath.Join(r.Root, "source-a", "session.json"))
				if e != nil {
					t.Fatal(e)
				}
				raw = bytes.ReplaceAll(raw, []byte(d), []byte(r.Config.Devices[1].ID))
				if e = os.WriteFile(filepath.Join(r.Root, "source-a", "session.json"), raw, 0600); e != nil {
					t.Fatal(e)
				}
				want = nil
			}
			writeMemoryFixture(t, p, s)
			switch kind {
			case "duplicate-key":
				os.WriteFile(p, []byte(`{"version":1,"version":1}`), 0600)
			case "oversized":
				os.WriteFile(p, []byte(strings.Repeat(" ", (1<<20)+1)), 0600)
				want = ErrTooLarge
			case "null":
				os.WriteFile(p, []byte(`null`), 0600)
			case "trailing":
				f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
				f.WriteString(" {}")
				f.Close()
			case "symlink":
				os.Remove(p)
				os.Symlink(filepath.Join(r.Root, "source-a", "session.json"), p)
			case "directory":
				os.Remove(p)
				os.Mkdir(p, 0700)
			case "missing-file":
				os.Remove(p)
				want = nil
			case "missing-directory":
				os.Remove(p)
				os.Remove(filepath.Dir(p))
			}
			before := memoryTree(t, r.Root)
			got, e := r.ListMemories("alice", "family", d)
			if !errors.Is(e, want) || e == nil && len(got) != count {
				t.Fatalf("got %d %v want %d %v", len(got), e, count, want)
			}
			if after := memoryTree(t, r.Root); !reflect.DeepEqual(before, after) {
				t.Fatal("reader mutated source")
			}
		})
	}
}
func memoryTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		v := st.Mode().String()
		if st.Mode().IsRegular() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			v += string(b)
		}
		out[p] = v
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestMemoryAtomicReplacement(t *testing.T) {
	r, p, s := memoryFixture(t)
	raw, _ := json.Marshal(s)
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			tmp := p + ".tmp"
			if e := os.WriteFile(tmp, raw, 0600); e != nil {
				done <- e
				return
			}
			if e := os.Rename(tmp, p); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 100; i++ {
		items, e := r.ListMemories("alice", "family", r.Config.Devices[0].ID)
		if e != nil && !errors.Is(e, ErrSource) {
			t.Fatal(e)
		}
		if e == nil && (len(items) != 1 || items[0].User != "合成问题") {
			t.Fatal("partial snapshot")
		}
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}

func TestMemoryPathAndIDGuards(t *testing.T) {
	r, p, _ := memoryFixture(t)
	d := r.Config.Devices[0].ID
	for _, id := range []string{"../source-a", "mem-0123456789abcdef0123456789abcdef-0", "mem-0123456789abcdef0123456789abcdef-9223372036854775808", "mem-0123456789abcdef0123456789abcdef-01"} {
		if _, e := r.ReadMemory("alice", "family", id, d); !errors.Is(e, ErrInvalid) {
			t.Fatal(id, e)
		}
	}
	if _, e := r.ListMemories("alice", "family", "../"+d); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	original := filepath.Dir(p)
	outside := filepath.Join(t.TempDir(), "memory")
	if e := os.Rename(original, outside); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, original); e != nil {
		t.Fatal(e)
	}
	if _, e := r.ListMemories("alice", "family", d); !errors.Is(e, ErrSource) {
		t.Fatal("directory escape", e)
	}
}

func TestMemoryRequiredTurnStrings(t *testing.T) {
	for _, field := range []string{"user", "assistant_spoken", "text_source", "playback_boundary"} {
		for _, value := range []string{"missing", "null", "empty"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				r, p, snapshot := memoryFixture(t)
				turn := snapshot["data"].(map[string]any)["turns"].([]any)[0].(map[string]any)
				switch value {
				case "missing":
					delete(turn, field)
				case "null":
					turn[field] = nil
				case "empty":
					turn[field] = ""
				}
				writeMemoryFixture(t, p, snapshot)
				items, err := r.ListMemories("alice", "family", r.Config.Devices[0].ID)
				if value == "empty" {
					if err != nil || len(items) != 1 {
						t.Fatalf("valid empty string: items=%+v err=%v", items, err)
					}
				} else if !errors.Is(err, ErrSource) || len(items) != 0 {
					t.Fatalf("damaged snapshot: items=%+v err=%v", items, err)
				}
			})
		}
	}
}
