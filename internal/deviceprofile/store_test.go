package deviceprofile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const idA = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const idB = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestProfilesRefreshFallbackAndRevisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, reason := reader.Resolve(idA, "小满")
	if p.Name != "小满" || p.Revision != 0 || reason == nil {
		t.Fatalf("missing fallback: %+v %v", p, reason)
	}
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := writer.SetName(idA, " 豆豆 ")
	if err != nil || first.Name != "豆豆" || first.Revision != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	if _, err = writer.SetName(idB, "星星"); err != nil {
		t.Fatal(err)
	}
	p, err = reader.Resolve(idA, "小满")
	if err != nil || p != first {
		t.Fatalf("refresh: %+v %v", p, err)
	}
	if err = os.WriteFile(path, []byte(`{"schema_version":9}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = reader.Resolve(idA, "小满")
	if err == nil || p != first {
		t.Fatalf("invalid refresh lost cached profile: %+v %v", p, err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	p, err = reader.Resolve(idA, "小满")
	if err == nil || p != first {
		t.Fatalf("missing refresh lost cached profile: %+v %v", p, err)
	}
	// A valid replacement intentionally dropping an entry returns to the default.
	if err = os.WriteFile(path, []byte(`{"schema_version":1,"profiles":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = reader.Resolve(idA, "小满")
	if err == nil || p.Name != "小满" || p.Revision != 0 {
		t.Fatalf("removed entry persisted: %+v %v", p, err)
	}
	if _, err = writer.SetName(idA, "月月"); err != nil {
		t.Fatal(err)
	}
	next, err := writer.SetName(idA, "花花")
	if err != nil || next.Revision != 2 {
		t.Fatalf("revision: %+v %v", next, err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("private file: %v %v", st, err)
	}
	var disk struct {
		Version  int                `json:"schema_version"`
		Profiles map[string]Profile `json:"profiles"`
	}
	raw, _ := os.ReadFile(path)
	if json.Unmarshal(raw, &disk) != nil || disk.Version != 1 || disk.Profiles[idA] != next {
		t.Fatal(string(raw))
	}
}

func TestProfilesInvalidNamesAndIDsNeverWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetName(idA, "豆豆"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	for _, name := range []string{"", "   ", strings.Repeat("星", 13), "豆\n豆", "豆\r豆", "豆\t豆", "豆\x00豆", "\n豆豆", "豆豆\r", "\t豆豆", string([]byte{0xff})} {
		if _, err = s.SetName(idA, name); err == nil {
			t.Errorf("accepted bad name %q", name)
		}
	}
	for _, id := range []string{"", "dev_bad", "dev_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "../profiles"} {
		if _, err = s.SetName(id, "星星"); err == nil {
			t.Errorf("accepted bad ID %q", id)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid operation changed file")
	}
	p, err := s.SetName(idA, strings.Repeat("星", 12))
	if err != nil || len([]rune(p.Name)) != 12 {
		t.Fatalf("unicode limit: %+v %v", p, err)
	}
}

func TestProfilesRejectUnsafeWritesAndCorruptDocuments(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	raw := []byte(`{"schema_version":1,"profiles":{}}`)
	os.WriteFile(target, raw, 0600)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, dir} {
		s, _ := Open(path)
		if s != nil {
			if _, err := s.SetName(idA, "豆豆"); err == nil {
				t.Errorf("unsafe path accepted: %s", path)
			}
		}
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	s, _ := Open(filepath.Join(alias, "new.json"))
	if s != nil {
		if _, err := s.SetName(idA, "豆豆"); err == nil {
			t.Fatal("symlink parent accepted")
		}
	}
	after, _ := os.ReadFile(target)
	if !bytes.Equal(after, raw) {
		t.Fatal("symlink target changed")
	}
	path := filepath.Join(dir, "profiles.json")
	for _, bad := range []string{`{"schema_version":2,"profiles":{}}`, `{"schema_version":1,"profiles":{"bad":{"name":"豆豆","revision":1}}}`, `{"schema_version":1,"profiles":{"` + idA + `":{"name":"豆豆","revision":0}}}`, `{"schema_version":1,"profiles":{"` + idA + `":{"name":"豆豆","revision":1,"token":"secret"}}}`, `{"schema_version":1,"schema_version":1,"profiles":{}}`, `{"schema_version":1,"profiles":{}} {}`} {
		os.WriteFile(path, []byte(bad), 0600)
		s, _ := Open(path)
		if s == nil {
			continue
		}
		if _, err := s.SetName(idA, "星星"); err == nil {
			t.Fatalf("corrupt document overwritten: %s", bad)
		}
		unchanged, _ := os.ReadFile(path)
		if string(unchanged) != bad {
			t.Fatal("corrupt document changed")
		}
	}
	os.WriteFile(path, raw, 0600)
	os.Chmod(path, 0644)
	s, _ = Open(path)
	if s != nil {
		if _, err := s.SetName(idA, "豆豆"); err == nil {
			t.Fatal("public profile accepted")
		}
	}
}

func TestProfileWriterProcess(t *testing.T) {
	if os.Getenv("DEVICE_PROFILE_WRITER") != "1" {
		return
	}
	args := os.Args
	sep := 0
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == 0 {
		t.Fatal("missing subprocess args")
	}
	s, err := Open(args[sep+1])
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err = s.SetName(args[sep+2], "豆豆"); err != nil {
			t.Fatal(err)
		}
		if _, err = s.SetName(idB, "共享"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProfilesConcurrentProcessesPreserveEntriesAndRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("dev_%032x", i+1)
			cmd := exec.Command(os.Args[0], "-test.run=^TestProfileWriterProcess$", "--", path, id)
			cmd.Env = append(os.Environ(), "DEVICE_PROFILE_WRITER=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("writer: %v %s", err, out)
			}
		}(i)
	}
	wg.Wait()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := s.Resolve(idB, "小满")
	if err != nil || shared.Revision != 40 {
		t.Fatalf("same-device lost update: %+v %v", shared, err)
	}
	for i := 0; i < 8; i++ {
		p, err := s.Resolve(fmt.Sprintf("dev_%032x", i+1), "小满")
		if err != nil || p.Name != "豆豆" || p.Revision != 5 {
			t.Fatalf("lost update: %+v %v", p, err)
		}
	}
}

func TestProfilesRejectDotDotAliasPath(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(t.TempDir(), alias); err != nil {
		t.Fatal(err)
	}
	s, _ := Open(alias + "/../profiles.json")
	if s != nil {
		if _, err := s.SetName(idA, "豆豆"); err == nil {
			t.Fatal("dot-dot alias path accepted")
		}
	}
}
