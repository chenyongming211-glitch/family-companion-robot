package realtimelive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordLookupSeparatesTerminalsAndLegacy(t *testing.T) {
	root := t.TempDir()
	for index, device := range []string{"dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"} {
		dir := filepath.Join(root, []string{"session-a", "session-b"}[index])
		os.Mkdir(dir, 0700)
		info := SessionInfo{Ownership: Ownership{DeviceID: device, SessionID: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}[index], ClientType: "mac_fixture"}, SchemaVersion: 1, StartedAt: time.Now().UTC()}
		if e := privateJSON(filepath.Join(dir, "session.json"), info); e != nil {
			t.Fatal(e)
		}
	}
	old := filepath.Join(root, "old-session")
	os.Mkdir(old, 0700)
	os.WriteFile(filepath.Join(old, "events.jsonl"), []byte("{}\n"), 0600)
	// Symlink directories never grant a path into another record tree.
	os.Symlink(old, filepath.Join(root, "alias"))
	records, e := ListRecords(root, "", "")
	if e != nil || len(records) != 3 {
		t.Fatalf("all records %d %v", len(records), e)
	}
	filtered, e := ListRecords(root, "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "")
	if e != nil || len(filtered) != 1 || filtered[0].RecordID != "session-a" {
		t.Fatal("device filter")
	}
	filtered, e = ListRecords(root, "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if e != nil || len(filtered) != 0 {
		t.Fatal("cross-device record selected")
	}
	filtered, e = ListRecords(root, "", "old-session")
	if e != nil || len(filtered) != 1 || filtered[0].DeviceID != "" || filtered[0].ClientType != "legacy_unattributed" {
		t.Fatal("legacy ownership invented")
	}
}

func TestSessionScopeCannotAcceptOldReceipt(t *testing.T) {
	s := &Session{rec: recorder{owner: Ownership{DeviceID: "device", SessionID: "current"}}}
	for _, event := range []map[string]any{{"type": "receipt", "id": 1}, {"type": "receipt", "id": 1, "session_id": "old"}, {"type": "stop", "session_id": "old"}} {
		if s.validateScope(event) == nil {
			t.Fatal("old or missing session accepted")
		}
	}
	if e := s.validateScope(map[string]any{"type": "receipt", "session_id": "current"}); e != nil {
		t.Fatal(e)
	}
	legacy := &Session{rec: recorder{owner: Ownership{SessionID: "current"}}}
	if e := legacy.validateScope(map[string]any{"type": "receipt"}); e != nil {
		t.Fatal("legacy contract broken")
	}
	if e := legacy.validateScope(map[string]any{"type": "receipt", "session_id": "old"}); e == nil {
		t.Fatal("explicit stale legacy scope accepted")
	}
}
