package management

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func auditRows(t *testing.T, s *Server) []map[string]any {
	t.Helper()
	raw, e := os.ReadFile(s.opts.AuditPath)
	if e != nil {
		t.Fatal(e)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil {
			t.Fatal("bad audit")
		}
		rows = append(rows, row)
	}
	return rows
}
func TestAuditQueryLegacyIsolation(t *testing.T) {
	s, _ := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	for _, d := range []string{s.cfg.Devices[0].ID, s.cfg.Devices[1].ID, ""} {
		if e := s.auditAction("alice", "admin", "read_chat", d, "private-record-id"); e != nil {
			t.Fatal(e)
		}
	}
	w := request(s, "GET", "/api/admin/audit", "", admin)
	if w.Code != 200 {
		t.Fatalf("audit route: %d %s", w.Code, w.Body)
	}
	var page struct {
		Items            []struct{ Status, DeviceID string }
		HasMore, Pending bool
	}
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].Status != "recorded" || page.Items[0].DeviceID != s.cfg.Devices[0].ID {
		t.Fatalf("authorized projection: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), "private-record-id") {
		t.Fatal("raw record leaked")
	}
}
func TestAuditRuntimeDurableRequestAndTerminal(t *testing.T) {
	s, b, c, _ := runtimeFixture(t)
	boot := "0123456789abcdef0123456789abcdef"
	report := `{"boot":"` + boot + `","seq":1,"volume":25,"brightness":null}`
	if w := deviceRequest(s, "POST", "/api/device/state", report, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w := request(s, "PUT", "/api/family/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+boot+`"}`, c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var out struct{ Command struct{ ID string } }
	json.Unmarshal(w.Body.Bytes(), &out)
	rows := auditRows(t, s)
	req := rows[len(rows)-1]
	if req["operationID"] != out.Command.ID || req["familyID"] != "home-a" || req["bindingID"] == nil || req["field"] != "volume" || req["value"] != float64(50) {
		t.Fatalf("request lacks immutable target: %#v", req)
	}
	report = `{"boot":"` + boot + `","seq":2,"volume":50,"brightness":null,"ack":{"id":"` + out.Command.ID + `","status":"applied","actual":50}}`
	if w = deviceRequest(s, "POST", "/api/device/state", report, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	rows = auditRows(t, s)
	terminal := rows[len(rows)-1]
	if terminal["outcome"] != "confirmed" || terminal["operationID"] != out.Command.ID || terminal["actual"] != float64(50) {
		t.Fatalf("missing confirmed receipt: %#v", terminal)
	}
	n := len(rows)
	request(s, "GET", "/api/family/device-runtime/"+b.DeviceID, "", c)
	if len(auditRows(t, s)) != n {
		t.Fatal("duplicate terminal")
	}
}

func TestAuditStrictFilters(t *testing.T) {
	s, _ := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	for _, q := range []string{"scope=family", "path=x", "device=", "device=x", "limit=0", "limit=101", "limit=+1", "offset=-1", "offset=10001", "offset=1&offset=2", "action=control", "from=2026-11-11", "from=2026-11-11T00:00:00Z&to=2026-11-11T00:00:00Z", "%zz=1"} {
		if w := request(s, "GET", "/api/admin/audit?"+q, "", admin); w.Code != 400 {
			t.Fatalf("query %s: %d", q, w.Code)
		}
	}
	if w := request(s, "GET", "/api/family/audit", "", login(t, s, "family", "alice")); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request(s, "PUT", "/api/admin/audit", "{}", admin); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request(s, "GET", "/api/admin/audit", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestAuditHistoricalBindingAndExactTerminalJoin(t *testing.T) {
	s, now := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	b := s.cfg.Bindings[0]
	v := 50
	actual := 50
	req := s.operationAudit("alice", "admin", "device_control_requested", b.DeviceID, "cmd_history", b, "volume", &v)
	req.UTC = b.From.Add(time.Minute)
	terminal := req
	terminal.Action = "device_control_result"
	terminal.Outcome = "confirmed"
	terminal.Actual = &actual
	terminal.UTC = now.Add(time.Hour)
	for _, e := range []auditEntry{req, terminal, terminal} {
		if err := s.auditRecord(e); err != nil {
			t.Fatal(err)
		}
	}
	cut := b.From.Add(2 * time.Minute)
	s.cfg.Bindings[0].Until = &cut
	s.cfg.Bindings = append(s.cfg.Bindings, Binding{ID: "new-binding", Version: 2, DeviceID: b.DeviceID, FamilyID: "home-b", From: cut})
	s.cfg.Devices[0].DisplayName = "新家庭机密名字"
	s.cfg.Accounts[1].Memberships[0].Devices = append(s.cfg.Accounts[1].Memberships[0].Devices, DeviceGrant{DeviceID: b.DeviceID})
	w := request(s, "GET", "/api/admin/audit", "", admin)
	var page AuditPage
	json.Unmarshal(w.Body.Bytes(), &page)
	if w.Code != 200 || len(page.Items) != 1 || page.Items[0].Status != "confirmed" || page.Items[0].CanOpenDevice || page.Items[0].Target != "历史设备" || page.Items[0].Actor != "历史账号" {
		t.Fatalf("history: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "新家庭") {
		t.Fatal("rename leak")
	}
	// New-family authority does not grant access to the old anchor, even with a late result.
	s.cfg.Accounts[1].InternalAdmin = true
	bob := login(t, s, "admin", "bob")
	w = request(s, "GET", "/api/admin/audit", "", bob)
	json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 0 {
		t.Fatal("cross family history", w.Body)
	}
	// A different-family result cannot establish success for a request.
	terminal.FamilyID = "home-b"
	terminal.BindingID = "new-binding"
	terminal.BindingVersion = 2
	raw, _ := json.Marshal(req)
	resultRaw, _ := json.Marshal(terminal)
	os.WriteFile(s.opts.AuditPath, append(append(raw, '\n'), append(resultRaw, '\n')...), 0600)
	w = request(s, "GET", "/api/admin/audit", "", admin)
	json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 1 || page.Items[0].Status != "unknown" {
		t.Fatal("cross-family join", w.Body)
	}
	terminal = req
	terminal.Action = "device_control_result"
	terminal.Outcome = "confirmed"
	terminal.Actual = &actual
	conflict := terminal
	conflict.Outcome = "failed"
	os.Remove(s.opts.AuditPath)
	for _, e := range []auditEntry{req, terminal, conflict} {
		s.auditRecord(e)
	}
	w = request(s, "GET", "/api/admin/audit", "", admin)
	json.Unmarshal(w.Body.Bytes(), &page)
	if page.Items[0].Status != "unknown" {
		t.Fatal("conflicting terminal", w.Body)
	}
	os.Remove(s.opts.AuditPath)
	s.auditRecord(terminal)
	w = request(s, "GET", "/api/admin/audit", "", admin)
	json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 0 {
		t.Fatal("orphan result", w.Body)
	}
}

func TestAuditFilesAndPendingTail(t *testing.T) {
	s, _ := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	w := request(s, "GET", "/api/admin/audit", "", admin)
	if w.Code != 200 {
		t.Fatal("missing", w.Code)
	}
	s.audit("alice", "family", s.cfg.Devices[0].ID, "raw-private-record")
	raw, _ := os.ReadFile(s.opts.AuditPath)
	for _, tail := range []string{`{"utc":`, "uncompleted"} {
		os.WriteFile(s.opts.AuditPath, append(append([]byte{}, raw...), []byte(tail)...), 0600)
		w = request(s, "GET", "/api/admin/audit", "", admin)
		var p AuditPage
		json.Unmarshal(w.Body.Bytes(), &p)
		if w.Code != 200 || !p.Pending || len(p.Items) != 1 {
			t.Fatal("pending", w.Code, w.Body)
		}
	}
	for _, bad := range [][]byte{[]byte("bad\n"), []byte("\n"), []byte(strings.Repeat("x", 4097) + "\n"), []byte(strings.Repeat("x", 4097)), []byte(`{"utc":"2026-11-09T00:00:00Z","utc":"2026-11-09T00:00:00Z"}` + "\n"), []byte(strings.Repeat("{}\n", 100001))} {
		os.WriteFile(s.opts.AuditPath, bad, 0600)
		if w = request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
			t.Fatal("malformed", w.Code, w.Body)
		}
	}
	os.WriteFile(s.opts.AuditPath, raw, 0600)
	os.Chmod(s.opts.AuditPath, 0644)
	if w = request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
		t.Fatal("public", w.Code)
	}
	os.Remove(s.opts.AuditPath)
	os.Symlink("missing", s.opts.AuditPath)
	if w = request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
		t.Fatal("symlink", w.Code)
	}
	os.Remove(s.opts.AuditPath)
	if e := syscall.Mkfifo(s.opts.AuditPath, 0600); e != nil {
		t.Fatal(e)
	}
	if w = request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
		t.Fatal("fifo", w.Code)
	}
	os.Remove(s.opts.AuditPath)
	os.Mkdir(s.opts.AuditPath, 0700)
	if w = request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
		t.Fatal("directory", w.Code)
	}
	os.Remove(s.opts.AuditPath)
	f, e := os.OpenFile(s.opts.AuditPath, os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.Truncate(auditMaxBytes + 1)
	f.Close()
	if w = request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
		t.Fatal("oversize", w.Code)
	}
}

func TestAuditAuthorizationAfterSnapshotWait(t *testing.T) {
	for _, kind := range []string{"role", "grants", "session"} {
		t.Run(kind, func(t *testing.T) {
			s, _, id, path := settingsFixture(t)
			admin := login(t, s, "admin", "alice")
			s.audit("alice", "admin", id, "private-record")
			s.auditMu.Lock()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- request(s, "GET", "/api/admin/audit", "", admin) }()
			// Wait for the handler to enter the audit snapshot mutex, without a product hook.
			deadline := time.Now().Add(time.Second)
			blocked := false
			for time.Now().Before(deadline) {
				buf := make([]byte, 65536)
				n := runtime.Stack(buf, true)
				if strings.Contains(string(buf[:n]), "management.(*Server).auditSnapshot") {
					blocked = true
					break
				}
				runtime.Gosched()
			}
			if !blocked {
				s.auditMu.Unlock()
				t.Fatal("query did not wait")
			}
			if kind == "session" {
				request(s, "POST", "/api/admin/logout", "{}", admin)
			} else {
				if err := (ConfigStore{path}).Transaction(func(c *Config) error {
					if kind == "role" {
						c.Accounts[0].InternalAdmin = false
					} else {
						c.Accounts[0].Memberships = nil
					}
					return nil
				}); err != nil {
					s.auditMu.Unlock()
					t.Fatal(err)
				}
			}
			s.auditMu.Unlock()
			w := <-done
			if kind == "grants" {
				var p AuditPage
				json.Unmarshal(w.Body.Bytes(), &p)
				if w.Code != 200 || len(p.Items) != 0 {
					t.Fatal("revoked grants leaked", w.Code, w.Body)
				}
			} else if w.Code != 403 {
				t.Fatal("revoked identity", w.Code, w.Body)
			}
		})
	}
}

func TestAuditClearTerminalAndFailureDoesNotChangeHTTP(t *testing.T) {
	for _, kind := range []string{"confirmed", "busy", "forbidden", "unknown", "terminal-write-failure"} {
		t.Run(kind, func(t *testing.T) {
			s, c, id, _ := settingsFixture(t)
			calls := 0
			attempts := 0
			s.opts.MemoryControlSocket = memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch kind {
				case "busy":
					w.WriteHeader(409)
				case "forbidden":
					w.WriteHeader(403)
				case "unknown":
					w.WriteHeader(503)
				default:
					json.NewEncoder(w).Encode(map[string]string{"deviceId": id, "status": "confirmed"})
				}
			}))
			s.auditWrite = func(path string, e auditEntry) error {
				if e.Action == "clear_memory_result" {
					attempts++
					if kind == "terminal-write-failure" {
						return ErrSource
					}
				}
				return appendAudit(path, e)
			}
			w := request(s, "POST", "/api/family/memory-clear/"+id, `{"confirm":true}`, c)
			want := 200
			outcome := "confirmed"
			switch kind {
			case "busy":
				want = 409
				outcome = "failed"
			case "forbidden":
				want = 403
				outcome = "failed"
			case "unknown":
				want = 503
				outcome = "unknown"
			}
			if w.Code != want || calls != 1 || attempts != 1 {
				t.Fatalf("status %d calls %d attempts %d", w.Code, calls, attempts)
			}
			rows := auditRows(t, s)
			if kind == "terminal-write-failure" {
				if len(rows) != 1 {
					t.Fatal(rows)
				}
			} else if len(rows) != 2 || rows[1]["outcome"] != outcome || rows[0]["operationID"] != rows[1]["operationID"] {
				t.Fatal(rows)
			}
		})
	}
}

func TestAuditAccessMemoryListAndStrictRFC3339(t *testing.T) {
	s, _ := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	s.auditAction("alice", "admin", "read_memories", s.cfg.Devices[0].ID, "")
	w := request(s, "GET", "/api/admin/audit?action=access", "", admin)
	var p AuditPage
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || len(p.Items) != 1 || p.Items[0].Status != "recorded" {
		t.Errorf("memory access missing: %d %s", w.Code, w.Body)
	}
	for _, q := range []string{"from=2026-11-09T1:00:00Z", "from=2026-11-09T01:00:00%2B24:00", "from=2026-11-09T01:00:00%2B00:60", "from=2026-11-09T01:00:00,5Z"} {
		if w = request(s, "GET", "/api/admin/audit?"+q, "", admin); w.Code != 400 {
			t.Errorf("non-RFC3339 accepted: %s %d", q, w.Code)
		}
	}
}
func TestAuditRuntimeTerminalFailureAndUncertainACK(t *testing.T) {
	for _, kind := range []string{"append-failed", "mismatch", "failed", "expired", "reboot", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			s, b, c, now := runtimeFixture(t)
			boot := "0123456789abcdef0123456789abcdef"
			path := "/api/family/device-runtime/" + b.DeviceID
			deviceRequest(s, "POST", "/api/device/state", `{"boot":"`+boot+`","seq":1,"volume":25,"brightness":null}`, b)
			w := request(s, "PUT", path, `{"field":"volume","value":50,"boot":"`+boot+`"}`, c)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			var out struct{ Command struct{ ID string } }
			json.Unmarshal(w.Body.Bytes(), &out)
			attempts := 0
			s.auditWrite = func(path string, e auditEntry) error {
				if e.Action == "device_control_result" {
					attempts++
					if kind == "append-failed" {
						return ErrSource
					}
				}
				return appendAudit(path, e)
			}
			actual := 50
			ackStatus := "applied"
			wantStatus := "confirmed"
			wantAudit := "confirmed"
			switch kind {
			case "mismatch":
				actual = 49
				wantStatus = "failed"
				wantAudit = "unknown"
			case "failed":
				ackStatus = "failed"
				wantStatus = "failed"
				wantAudit = "failed"
			case "expired":
				*now = now.Add(46 * time.Second)
				wantStatus = "unknown"
				wantAudit = "unknown"
			case "reboot":
				boot = "abcdef0123456789abcdef0123456789"
				wantStatus = "unknown"
				wantAudit = "unknown"
			case "revoked":
				if err := (ConfigStore{s.opts.ConfigPath}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); err != nil {
					t.Fatal(err)
				}
				wantStatus = "unknown"
				wantAudit = "unknown"
			}
			raw, _ := json.Marshal(map[string]any{"boot": boot, "seq": 2, "volume": actual, "brightness": nil, "ack": map[string]any{"id": out.Command.ID, "status": ackStatus, "actual": actual}})
			if w = deviceRequest(s, "POST", "/api/device/state", string(raw), b); w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			w = request(s, "GET", path, "", c)
			var state RuntimeDTO
			json.Unmarshal(w.Body.Bytes(), &state)
			if w.Code != 200 || state.Command == nil || state.Command.Status != wantStatus || attempts != 1 {
				t.Fatalf("state changed by audit: %d %s attempts %d", w.Code, w.Body, attempts)
			}
			request(s, "GET", path, "", c)
			if attempts != 1 {
				t.Fatal("terminal append retried")
			}
			rows := auditRows(t, s)
			if kind != "append-failed" && rows[len(rows)-1]["outcome"] != wantAudit {
				t.Fatalf("wrong terminal %#v", rows[len(rows)-1])
			}
			if kind == "append-failed" {
				admin := login(t, s, "admin", "alice")
				w = request(s, "GET", "/api/admin/audit?action=device_control", "", admin)
				var p AuditPage
				json.Unmarshal(w.Body.Bytes(), &p)
				if len(p.Items) != 1 || p.Items[0].Status != "unknown" {
					t.Fatal("invented persistence", w.Body)
				}
			}
		})
	}
}

func TestAuditAuthorizedPaginationAndStableIDs(t *testing.T) {
	s, now := httpFixture(t)
	c := login(t, s, "admin", "alice")
	s.audit("alice", "admin", s.cfg.Devices[1].ID, "private-b")
	for i := 0; i < 3; i++ {
		*now = now.Add(time.Second)
		s.audit("alice", "admin", s.cfg.Devices[0].ID, "private-a")
	}
	w := request(s, "GET", "/api/admin/audit?offset=0&limit=1", "", c)
	var first AuditPage
	json.Unmarshal(w.Body.Bytes(), &first)
	if w.Code != 200 || len(first.Items) != 1 || !first.HasMore {
		t.Fatal(w.Code, w.Body)
	}
	w = request(s, "GET", "/api/admin/audit?offset=1&limit=1", "", c)
	var second AuditPage
	json.Unmarshal(w.Body.Bytes(), &second)
	if len(second.Items) != 1 || !second.HasMore || second.Items[0].ID == first.Items[0].ID || !first.Items[0].CanOpenDevice || first.Items[0].Actor != "alice" {
		t.Fatal(w.Body)
	}
	w = request(s, "GET", "/api/admin/audit?offset=2&limit=1", "", c)
	var last AuditPage
	json.Unmarshal(w.Body.Bytes(), &last)
	if len(last.Items) != 1 || last.HasMore {
		t.Fatal("unauthorized count", w.Body)
	}
	from := now.Add(-time.Second).UTC().Format(time.RFC3339)
	to := now.UTC().Format(time.RFC3339)
	w = request(s, "GET", "/api/admin/audit?from="+from+"&to="+to, "", c)
	var rangePage AuditPage
	json.Unmarshal(w.Body.Bytes(), &rangePage)
	if len(rangePage.Items) != 1 || rangePage.Items[0].ID != second.Items[0].ID {
		t.Fatal("time range or unstable ID", w.Body)
	}
}

func TestAuditIDCannotRevealPrivateLineOffset(t *testing.T) {
	s, _ := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	s.audit("alice", "admin", s.cfg.Devices[1].ID, "private-other-family")
	s.audit("alice", "admin", s.cfg.Devices[0].ID, "private-record")
	w := request(s, "GET", "/api/admin/audit", "", admin)
	var p AuditPage
	json.Unmarshal(w.Body.Bytes(), &p)
	if len(p.Items) != 1 {
		t.Fatal(w.Body)
	}
	e := auditRows(t, s)[1]
	at, _ := time.Parse(time.RFC3339Nano, e["utc"].(string))
	guess := sha256.Sum256([]byte("1\x00" + at.String() + "\x00\x00alice\x00" + s.cfg.Devices[0].ID))
	if p.Items[0].ID == "audit_"+hex.EncodeToString(guess[:16]) {
		t.Fatal("opaque ID permits guessing the hidden global line index")
	}
}

func waitAuditStack(t *testing.T, method string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		if strings.Contains(string(buf[:n]), method) {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("handler did not reach " + method)
}
func TestAuditAuthorityAfterSessionWait(t *testing.T) {
	for _, kind := range []string{"role", "grants"} {
		t.Run(kind, func(t *testing.T) {
			s, _, id, path := settingsFixture(t)
			admin := login(t, s, "admin", "alice")
			s.audit("alice", "admin", id, "private-record")
			s.auditMu.Lock()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- request(s, "GET", "/api/admin/audit", "", admin) }()
			waitAuditStack(t, "management.(*Server).auditSnapshot")
			s.mu.Lock()
			s.auditMu.Unlock()
			waitAuditStack(t, "management.(*Server).adminAudit")
			if err := (ConfigStore{path}).Transaction(func(c *Config) error {
				if kind == "role" {
					c.Accounts[0].InternalAdmin = false
				} else {
					c.Accounts[0].Memberships = nil
				}
				return nil
			}); err != nil {
				s.mu.Unlock()
				t.Fatal(err)
			}
			s.mu.Unlock()
			w := <-done
			if kind == "grants" {
				var p AuditPage
				json.Unmarshal(w.Body.Bytes(), &p)
				if w.Code != 200 || len(p.Items) != 0 {
					t.Fatalf("authority stale across session lock: %d %s", w.Code, w.Body)
				}
			} else if w.Code != 403 {
				t.Fatalf("role stale across session lock: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestAuditRemovedActorStillVisibleAndPrivateOwner(t *testing.T) {
	s, _ := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	b := s.cfg.Bindings[0]
	e := s.operationAudit("former-staff", "admin", "clear_memory_requested", b.DeviceID, "op_removed_actor", b, "", nil)
	if err := s.auditRecord(e); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/admin/audit", "", admin)
	var p AuditPage
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || len(p.Items) != 1 || p.Items[0].Actor != "历史账号" || p.Items[0].Status != "unknown" {
		t.Fatal("actor removal erased history", w.Code, w.Body)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(s.opts.AuditPath, 1001, 1001); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chown(s.opts.AuditPath, 0, 0) })
		if w = request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
			t.Fatal("non-root owner", w.Code, w.Body)
		}
	}
}

func TestAuditHistoricalReadDoesNotLeakAcrossTransfer(t *testing.T) {
	for _, dualGrant := range []bool{false, true} {
		t.Run(fmt.Sprintf("dual-grant-%v", dualGrant), func(t *testing.T) {
			s, now := httpFixture(t)
			id := s.cfg.Devices[0].ID
			record := "20261109T010000Z-0123456789abcdef0123456789abcdef"
			httpRecord(t, s, record, id, *now, "")
			cut := now.Add(time.Minute)
			s.cfg.Bindings[0].Until = &cut
			s.cfg.Bindings = append(s.cfg.Bindings, Binding{ID: "transferred-b", Version: 2, FamilyID: "home-b", DeviceID: id, From: cut})
			s.cfg.Accounts[1].InternalAdmin = true
			s.cfg.Accounts[1].Memberships[0].Devices = append(s.cfg.Accounts[1].Memberships[0].Devices, DeviceGrant{DeviceID: id, CanReadBody: true})
			if dualGrant {
				s.cfg.Accounts[0].Memberships = append(s.cfg.Accounts[0].Memberships, Membership{FamilyID: "home-b", Devices: []DeviceGrant{{DeviceID: id, CanReadBody: true}}})
			}
			*now = now.Add(2 * time.Minute)
			a := login(t, s, "family", "alice")
			if w := request(s, "GET", "/api/family/conversations/"+record, "", a); w.Code != 200 {
				t.Fatalf("historical read must retain success: %d %s", w.Code, w.Body)
			}
			rows := auditRows(t, s)
			if len(rows) != 1 {
				t.Fatal(rows)
			}
			for _, viewer := range []string{"alice", "bob", "observer"} {
				w := request(s, "GET", "/api/admin/audit", "", login(t, s, "admin", viewer))
				var p AuditPage
				json.Unmarshal(w.Body.Bytes(), &p)
				if w.Code != 200 || len(p.Items) != 0 {
					t.Errorf("uncertain historical access exposed to %s: %d %s", viewer, w.Code, w.Body)
				}
			}
			// Actual old six-field data stays attributed by its stored timestamp,
			// independent of the device's current family.
			legacy := auditEntry{UTC: cut.Add(-time.Minute), ActorID: "alice", Scope: "family", Action: "read_conversation", DeviceID: id, RecordID: record}
			if err := s.auditRecord(legacy); err != nil {
				t.Fatal(err)
			}
			for _, viewer := range []string{"alice", "bob"} {
				w := request(s, "GET", "/api/admin/audit", "", login(t, s, "admin", viewer))
				var p AuditPage
				json.Unmarshal(w.Body.Bytes(), &p)
				want := 0
				if viewer == "alice" {
					want = 1
				}
				if w.Code != 200 || len(p.Items) != want {
					t.Fatalf("legacy event-time owner %s: %d %s", viewer, w.Code, w.Body)
				}
			}
			// A single-current-family actor's ordinary read still gets a safe,
			// complete modern anchor even on a device transferred from another family.
			currentRecord := "20261109T010200Z-abcdef0123456789abcdef0123456789"
			httpRecord(t, s, currentRecord, id, *now, "")
			if w := request(s, "GET", "/api/family/conversations/"+currentRecord, "", login(t, s, "family", "bob")); w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			rows = auditRows(t, s)
			last := rows[len(rows)-1]
			if last["schemaVersion"] != float64(1) || last["familyID"] != "home-b" || last["bindingID"] != "transferred-b" || last["bindingVersion"] != float64(2) {
				t.Fatalf("normal modern access lacks anchor: %#v", last)
			}
			w := request(s, "GET", "/api/admin/audit", "", login(t, s, "admin", "bob"))
			var p AuditPage
			json.Unmarshal(w.Body.Bytes(), &p)
			if w.Code != 200 || len(p.Items) != 1 || p.Items[0].Status != "recorded" {
				t.Fatal("ordinary current access excluded", w.Code, w.Body)
			}
		})
	}
}

func TestAuditOnlyExactSixFieldLegacyMayInferBinding(t *testing.T) {
	s, now := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	id := s.cfg.Devices[0].ID
	for _, action := range []string{"read_chat", "device_control_requested", "clear_memory_requested", "update_device_settings_requested"} {
		for _, extra := range []map[string]any{nil, {"schemaVersion": 1}, {"schemaVersion": 0}, {"operationID": ""}, {"familyID": "home-a"}, {"bindingID": ""}, {"bindingVersion": 0}, {"field": "volume"}, {"value": 0}, {"actual": 0}, {"outcome": ""}, {"reason": ""}} {
			row := map[string]any{"utc": now.UTC().Format(time.RFC3339Nano), "actorID": "alice", "scope": "admin", "action": action, "deviceID": id, "recordID": "private-old-record"}
			for k, v := range extra {
				row[k] = v
			}
			raw, _ := json.Marshal(row)
			os.WriteFile(s.opts.AuditPath, append(raw, '\n'), 0600)
			w := request(s, "GET", "/api/admin/audit", "", admin)
			var p AuditPage
			json.Unmarshal(w.Body.Bytes(), &p)
			want := 0
			if extra == nil {
				want = 1
			}
			if w.Code != 200 || len(p.Items) != want {
				t.Errorf("partial modern row masquerades as legacy (%v): %d %s", extra, w.Code, w.Body)
			}
		}

	}
}

func TestAuditCompletedLineRequiresDecoderEOF(t *testing.T) {
	s, now := httpFixture(t)
	admin := login(t, s, "admin", "alice")
	row := auditEntry{UTC: *now, ActorID: "alice", Scope: "admin", Action: "read_chat", DeviceID: s.cfg.Devices[0].ID, RecordID: "private-old-record"}
	raw, _ := json.Marshal(row)
	for _, extra := range []string{" junk", " {}", " []", " null", " true", " 42"} {
		os.WriteFile(s.opts.AuditPath, append(append(append([]byte{}, raw...), []byte(extra)...), '\n'), 0600)
		if w := request(s, "GET", "/api/admin/audit", "", admin); w.Code != 503 {
			t.Errorf("trailing completed content accepted %q: %d %s", extra, w.Code, w.Body)
		}
	}
	os.WriteFile(s.opts.AuditPath, append(append([]byte{}, raw...), []byte(" \t\r\n")...), 0600)
	if w := request(s, "GET", "/api/admin/audit", "", admin); w.Code != 200 {
		t.Fatal("trailing JSON whitespace rejected", w.Code, w.Body)
	}
	os.WriteFile(s.opts.AuditPath, append(append(append([]byte{}, raw...), '\n'), []byte(`{"uncompleted":`)...), 0600)
	w := request(s, "GET", "/api/admin/audit", "", admin)
	var p AuditPage
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || !p.Pending || len(p.Items) != 1 {
		t.Fatal("bounded incomplete tail", w.Code, w.Body)
	}
}

func TestAuditLegacyAccessAfterTransferIsUnattributed(t *testing.T) {
	s, now := httpFixture(t)
	id := s.cfg.Devices[0].ID
	old := s.cfg.Bindings[0]
	cut := now.Add(-time.Minute)
	s.cfg.Accounts[1].InternalAdmin = true
	s.cfg.Accounts[1].Memberships[0].Devices = append(s.cfg.Accounts[1].Memberships[0].Devices, DeviceGrant{DeviceID: id, CanReadBody: true})
	cookies := map[string]*http.Cookie{"alice": login(t, s, "admin", "alice"), "bob": login(t, s, "admin", "bob")}
	check := func(action, actor string, at time.Time, alice, bob int) {
		t.Helper()
		row := auditEntry{UTC: at, ActorID: actor, Scope: "family", Action: action, DeviceID: id, RecordID: "private-historical-record"}
		raw, _ := json.Marshal(row)
		raw = append(raw, '\n')
		os.WriteFile(s.opts.AuditPath, raw, 0600)
		for _, viewer := range []string{"alice", "bob"} {
			w := request(s, "GET", "/api/admin/audit", "", cookies[viewer])
			var p AuditPage
			json.Unmarshal(w.Body.Bytes(), &p)
			want := alice
			if viewer == "bob" {
				want = bob
			}
			if w.Code != 200 || len(p.Items) != want {
				t.Errorf("legacy %s actor=%s viewer=%s at=%s: %d %s", action, actor, viewer, at, w.Code, w.Body)
			}
			if want == 1 && action != "read_chat" && action != "read_conversation" && action != "list_chats" && action != "read_memories" && action != "read_memory" && len(p.Items) == 1 && p.Items[0].Status != "unknown" {
				t.Errorf("legacy write fabricated outcome: %s", w.Body)
			}
		}
		after, _ := os.ReadFile(s.opts.AuditPath)
		if !bytes.Equal(raw, after) {
			t.Fatal("legacy log rewritten")
		}
	}
	access := []string{"read_conversation", "read_chat", "list_chats", "read_memories", "read_memory"}
	// Single-family access before any transfer remains visible.
	for _, action := range access {
		check(action, "former-staff", *now, 1, 0)
	}
	old.Until = &cut
	s.cfg.Bindings[0] = old
	s.cfg.Bindings = append(s.cfg.Bindings, Binding{ID: "same-family-rebind", Version: 2, FamilyID: "home-a", DeviceID: id, From: cut})
	for _, action := range access {
		check(action, "former-staff", *now, 1, 0)
	}
	s.cfg.Bindings[len(s.cfg.Bindings)-1].FamilyID = "home-b"
	// Current or removed actor grants cannot disambiguate the old six fields.
	for _, action := range access {
		for _, actor := range []string{"alice", "bob", "former-staff"} {
			check(action, actor, *now, 0, 0)
		}
		check(action, "former-staff", cut.Add(-time.Minute), 1, 0)
	}
	// Existing legacy write requests retain event-UTC ownership and unknown result.
	for _, action := range []string{"device_control_requested", "clear_memory_requested", "update_device_settings_requested"} {
		check(action, "former-staff", *now, 0, 1)
	}
}
