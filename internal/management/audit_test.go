package management

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestAuditPrivateRegularAndBounded(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "audit")
	entry := auditEntry{time.Now().UTC(), "alice", "family", "read_conversation", "dev_synthetic", "synthetic-record"}
	if e := appendAudit(p, entry); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(p)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("private mode")
	}
	raw, _ := os.ReadFile(p)
	var got auditEntry
	if json.Unmarshal(raw, &got) != nil || got.ActorID != "alice" || !got.UTC.Equal(entry.UTC) {
		t.Fatal("entry")
	}
	os.Chmod(p, 0644)
	if appendAudit(p, entry) == nil {
		t.Fatal("public file")
	}
	link := filepath.Join(root, "link")
	os.Symlink("audit", link)
	if appendAudit(link, entry) == nil {
		t.Fatal("symlink")
	}
	fifo := filepath.Join(root, "fifo")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		t.Fatal(e)
	}
	if appendAudit(fifo, entry) == nil {
		t.Fatal("fifo")
	}
	entry.RecordID = strings.Repeat("x", 4097)
	if appendAudit(filepath.Join(root, "oversized"), entry) == nil {
		t.Fatal("bounded")
	}
	if appendAudit(root, entry) == nil {
		t.Fatal("directory")
	}
}
func TestAuditSerialConcurrentWrites(t *testing.T) {
	s, _ := httpFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.audit("alice", "family", "synthetic-device", "record"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	raw, e := os.ReadFile(s.opts.AuditPath)
	if e != nil || strings.Count(string(raw), "\n") != 20 {
		t.Fatal("audit count", e)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row auditEntry
		if json.Unmarshal([]byte(line), &row) != nil {
			t.Fatal("interleaving")
		}
	}
}
