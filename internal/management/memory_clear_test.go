package management

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryClearRouteRequiresFreshAdministrator(t *testing.T) {
	s, cookie, id, path := settingsFixture(t)
	for _, body := range []string{`{}`, `{"confirm":false}`, `{"confirm":true,"actor":"alice"}`, `{"confirm":true,"confirm":true}`, `{"Confirm":true}`} {
		if w := request(s, "POST", "/api/family/memory-clear/"+id, body, cookie); w.Code != 400 {
			t.Fatalf("body %s: %d %s", body, w.Code, w.Body)
		}
	}
	for _, actor := range []string{"bob", "metadata", "observer"} {
		c := login(t, s, "family", actor)
		if w := request(s, "POST", "/api/family/memory-clear/"+id, `{"confirm":true}`, c); w.Code != 403 {
			t.Fatalf("actor %s: %d %s", actor, w.Code, w.Body)
		}
	}
	if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); e != nil {
		t.Fatal(e)
	}
	if w := request(s, "POST", "/api/family/memory-clear/"+id, `{"confirm":true}`, cookie); w.Code != 403 {
		t.Fatalf("revoked: %d %s", w.Code, w.Body)
	}
}

func TestMemoryProvenanceExactTurnAndOptionalUnavailable(t *testing.T) {
	r, _, _ := memoryFixture(t)
	id := r.Config.Devices[0].ID
	info := realtimelive.SessionInfo{SchemaVersion: 1, Ownership: realtimelive.Ownership{SessionID: "0123456789abcdef0123456789abcdef", DeviceID: id, ClientType: "xiaozhi"}, StartedAt: r.Config.Bindings[0].From.Add(time.Minute)}
	p := chatWrite(t, r, info, "source-a", map[string]any{"event": "user_text", "id": 1, "text": "合成问题"}, map[string]any{"event": "generated", "id": 1, "text": "合成回复", "sent": 0})
	chats, e := r.ListChats("alice", "family", ConversationFilter{DeviceID: &id})
	if e != nil || len(chats.Items) != 1 {
		t.Fatalf("chats %+v %v", chats, e)
	}
	items, e := r.ListMemories("alice", "family", id)
	if e != nil || len(items) != 1 {
		t.Fatalf("memories %+v %v", items, e)
	}
	raw, _ := json.Marshal(items[0])
	var v map[string]any
	json.Unmarshal(raw, &v)
	if v["chatId"] != chats.Items[0].ID || v["turnId"] != info.SessionID+":1" || v["conversationId"] != "source-a" {
		t.Fatalf("provenance %s", raw)
	}
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	items, e = r.ListMemories("alice", "family", id)
	if e != nil || len(items) != 1 {
		t.Fatalf("optional source destroyed memory: %+v %v", items, e)
	}
	raw, _ = json.Marshal(items[0])
	json.Unmarshal(raw, &v)
	var missing map[string]any
	json.Unmarshal(raw, &missing)
	if _, ok := missing["chatId"]; ok {
		t.Fatalf("invented link %s", raw)
	}
	if _, e = os.Stat(filepath.Join(r.Root, "source-a", "session.json")); e != nil {
		t.Fatal(e)
	}
}

func TestMemoryClearGuardFreshAuthorityAndLockLifetime(t *testing.T) {
	for _, kind := range []string{"role", "body", "binding", "actor", "family", "expiry"} {
		t.Run(kind, func(t *testing.T) {
			s, _, id, path := settingsFixture(t)
			b := s.cfg.Bindings[0]
			claim := realtimelive.MemoryClearClaim{DeviceID: id, ActorID: "alice", FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(time.Minute)}
			if kind == "family" {
				claim.FamilyID = "other-family"
			}
			if kind == "expiry" {
				claim.Deadline = time.Now().Add(-time.Second)
			}
			if e := (ConfigStore{path}).Transaction(func(c *Config) error {
				switch kind {
				case "role":
					c.Accounts[0].Memberships[0].CanAddDevices = false
				case "body":
					c.Accounts[0].Memberships[0].Devices[0].CanReadBody = false
				case "binding":
					c.Bindings[0].Version++
				case "actor":
					claim.ActorID = "missing-actor"
				}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			called := false
			e := NewMemoryClearGuard(path)(context.Background(), claim, func() error { called = true; return nil })
			if !errors.Is(e, realtimelive.ErrMemoryClearForbidden) || called {
				t.Fatalf("%s: %v called=%v", kind, e, called)
			}
		})
	}
	s, _, id, path := settingsFixture(t)
	b := s.cfg.Bindings[0]
	claim := realtimelive.MemoryClearClaim{DeviceID: id, ActorID: "alice", FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(time.Minute)}
	before, _ := os.ReadFile(path)
	ctx, cancel := context.WithCancel(context.Background())
	admitted, receipt := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- NewMemoryClearGuard(path)(ctx, claim, func() error { close(admitted); <-receipt; return nil })
	}()
	<-admitted
	cancel()
	wait, cancelWait := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelWait()
	if e := (ConfigStore{path}).WithSnapshot(wait, func(Config) error { t.Error("lock released before receipt"); return nil }); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("lock lifetime", e)
	}
	writer := make(chan error, 1)
	go func() {
		writer <- (ConfigStore{path}).Transaction(func(c *Config) error { c.Bindings[0].Version++; return nil })
	}()
	select {
	case e := <-writer:
		t.Fatal("rebound before receipt", e)
	case <-time.After(30 * time.Millisecond):
	}
	close(receipt)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := <-writer; e != nil {
		t.Fatal(e)
	}
	// A read-only snapshot itself never rewrites configuration bytes or inode.
	current, _ := os.Stat(path)
	raw, _ := os.ReadFile(path)
	if e := (ConfigStore{path}).WithSnapshot(context.Background(), func(Config) error { return nil }); e != nil {
		t.Fatal(e)
	}
	after, _ := os.Stat(path)
	latest, _ := os.ReadFile(path)
	if !os.SameFile(current, after) || string(raw) != string(latest) || len(before) == 0 {
		t.Fatal("snapshot rewrote config")
	}
}

func memoryTestListener(t *testing.T, h http.Handler) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("private root socket")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "control.sock")
	ln, e := realtimelive.ListenMemoryControl(path)
	if e != nil {
		t.Fatal(e)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close(); ln.Close() })
	return path
}

func TestMemoryClearPublicUnixBridgeAndAuditBeforeMutation(t *testing.T) {
	for _, kind := range []string{"confirmed", "busy", "audit-failure", "origin", "wrong-device", "malformed", "redirect", "forbidden", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			s, cookie, id, path := settingsFixture(t)
			var calls atomic.Int32
			s.opts.MemoryControlSocket = memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, e := os.ReadFile(s.opts.AuditPath)
				if e != nil || !strings.Contains(string(raw), "clear_memory_requested") {
					t.Error("mutation without durable intent", e)
				}
				var claim realtimelive.MemoryClearClaim
				if json.NewDecoder(r.Body).Decode(&claim) != nil || claim.DeviceID != id || claim.ActorID != "alice" {
					t.Error("bad private claim")
				}
				// This takes the same config flock. A held manager lock would deadlock.
				e = NewMemoryClearGuard(path)(r.Context(), claim, func() error { return nil })
				if e != nil {
					t.Error("guard", e)
				}
				switch kind {
				case "busy":
					w.WriteHeader(409)
				case "forbidden":
					w.WriteHeader(403)
				case "unknown":
					w.WriteHeader(503)
				case "redirect":
					w.Header().Set("Location", "http://forbidden.invalid")
					w.WriteHeader(307)
				case "malformed":
					w.Write([]byte(`{"deviceId":"x","status":"confirmed"}`))
				default:
					json.NewEncoder(w).Encode(map[string]string{"deviceId": id, "status": "confirmed"})
				}
			}))
			if kind == "audit-failure" {
				s.auditWrite = func(string, auditEntry) error { return ErrSource }
			}
			req := httptest.NewRequest("POST", "/api/family/memory-clear/"+id, strings.NewReader(`{"confirm":true}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookie)
			if kind == "origin" {
				req.Header.Set("Origin", "https://evil.invalid")
			}
			if kind == "wrong-device" {
				req.URL.Path = "/api/family/memory-clear/" + s.cfg.Devices[1].ID
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			want := 503
			wantCalls := 1
			switch kind {
			case "confirmed":
				want = 200
			case "busy":
				want = 409
			case "forbidden":
				want = 403
			case "origin", "wrong-device":
				want = 403
				wantCalls = 0
			case "audit-failure":
				wantCalls = 0
			}
			if w.Code != want || calls.Load() != int32(wantCalls) {
				t.Fatalf("%s status=%d calls=%d body=%s", kind, w.Code, calls.Load(), w.Body)
			}
			if kind == "audit-failure" && !strings.Contains(w.Body.String(), `"code":"memory_clear_unconfirmed"`) {
				t.Fatalf("clear-specific error contract: %s", w.Body)
			}
			if kind == "confirmed" && !strings.Contains(w.Body.String(), `"status":"confirmed"`) {
				t.Fatal(w.Body)
			}
		})
	}
}

func TestMemoryClearDelayedRPCRebindCannotClearNewBinding(t *testing.T) {
	s, _, id, path := settingsFixture(t)
	b := s.cfg.Bindings[0]
	claim := realtimelive.MemoryClearClaim{DeviceID: id, ActorID: "alice", FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(time.Minute)}
	arrived, proceed := make(chan struct{}), make(chan struct{})
	var mutated atomic.Bool
	socket := memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-proceed
		e := NewMemoryClearGuard(path)(r.Context(), claim, func() error { mutated.Store(true); return nil })
		if errors.Is(e, realtimelive.ErrMemoryClearForbidden) {
			w.WriteHeader(403)
			return
		}
		t.Error("accepted stale binding", e)
	}))
	done := make(chan error, 1)
	go func() { done <- clearMemoryRPC(context.Background(), socket, claim) }()
	<-arrived
	if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Bindings[0].Version++; return nil }); e != nil {
		t.Fatal(e)
	}
	close(proceed)
	if e := <-done; !errors.Is(e, ErrForbidden) || mutated.Load() {
		t.Fatal("stale clear", e, mutated.Load())
	}
}

func TestMemoryProvenanceDoesNotGuessMissingTurn(t *testing.T) {
	r, _, _ := memoryFixture(t)
	id := r.Config.Devices[0].ID
	info := realtimelive.SessionInfo{SchemaVersion: 1, Ownership: realtimelive.Ownership{SessionID: "0123456789abcdef0123456789abcdef", DeviceID: id, ClientType: "xiaozhi"}, StartedAt: r.Config.Bindings[0].From.Add(time.Minute)}
	chatWrite(t, r, info, "source-a", map[string]any{"event": "user_text", "id": 2, "text": "不同轮次"})
	items, e := r.ListMemories("alice", "family", id)
	if e != nil || len(items) != 1 || items[0].ChatID != "" || items[0].TurnID != "" {
		t.Fatalf("guessed source: %+v %v", items, e)
	}
}

func TestMemoryClearClientCancellationDoesNotRetry(t *testing.T) {
	s, _, id, _ := settingsFixture(t)
	b := s.cfg.Bindings[0]
	claim := realtimelive.MemoryClearClaim{DeviceID: id, ActorID: "alice", FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(time.Minute)}
	arrived, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	socket := memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(arrived)
		<-finish
		json.NewEncoder(w).Encode(map[string]string{"deviceId": id, "status": "confirmed"})
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- clearMemoryRPC(ctx, socket, claim) }()
	<-arrived
	cancel()
	if e := <-done; e == nil {
		t.Fatal("cancellation confirmed")
	}
	if calls.Load() != 1 {
		t.Fatal("retry", calls.Load())
	}
	close(finish)
}

func TestMemoryClearRealUnixStoreKeepsArchive(t *testing.T) {
	s, cookie, id, path := settingsFixture(t)
	r, snapshotPath, _ := memoryFixture(t)
	s.opts.RecordsPath = r.Root
	s.reader.Root = r.Root
	archive := filepath.Join(r.Root, "source-a", "session.json")
	before, e := os.ReadFile(archive)
	if e != nil {
		t.Fatal(e)
	}
	voice := realtimelive.NewServer(realtimelive.Config{MemoryEnabled: true, MaxSessions: 2}, nil, r.Root, "synthetic-memory-test")
	if e = voice.InitializationError(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { voice.CloseActive(context.Background()) })
	s.opts.MemoryControlSocket = memoryTestListener(t, voice.MemoryControlHandler(NewMemoryClearGuard(path)))
	w := request(s, "POST", "/api/family/memory-clear/"+id, `{"confirm":true}`, cookie)
	if w.Code != 200 {
		t.Fatalf("real chain %d %s", w.Code, w.Body)
	}
	raw, e := os.ReadFile(snapshotPath)
	if e != nil {
		t.Fatal(e)
	}
	var snapshot memorySnapshot
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.Data.Turns == nil || len(snapshot.Data.Turns) != 0 {
		t.Fatal("not actually empty", string(raw))
	}
	after, e := os.ReadFile(archive)
	if e != nil || string(before) != string(after) {
		t.Fatal("archive changed", e)
	}
}

func TestMemoryClearWeChatOriginExactRoute(t *testing.T) {
	s, _, id, _ := settingsFixture(t)
	s.opts.PublicOrigin = onboardingOrigin
	r := httptest.NewRequest("POST", onboardingOrigin+"/api/family/memory-clear/"+id, nil)
	r.RemoteAddr = "127.0.0.1:31000"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Referer", "https://servicewechat.com/"+onboardingMiniProgramID+"/devtools/page-frame.html")
	if !s.checkOrigin(r) {
		t.Fatal("legitimate native clear origin rejected")
	}
	for _, kind := range []string{"query", "wrong-platform", "extra-path", "method"} {
		bad := r.Clone(r.Context())
		u := *r.URL
		bad.URL = &u
		switch kind {
		case "query":
			bad.URL.RawQuery = "x=1"
		case "wrong-platform":
			bad.Header.Set("Referer", "https://servicewechat.com/wxbad/devtools/page-frame.html")
		case "extra-path":
			bad.URL.Path += "/extra"
		case "method":
			bad.Method = "PUT"
		}
		if s.checkOrigin(bad) {
			t.Fatal("invalid native route accepted", kind)
		}
	}
}

func TestMemoryClearConfigUnavailableUsesOperationCode(t *testing.T) {
	s, cookie, id, path := settingsFixture(t)
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	w := request(s, "POST", "/api/family/memory-clear/"+id, `{"confirm":true}`, cookie)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"memory_clear_unconfirmed"`) {
		t.Fatalf("clear-specific source error: %d %s", w.Code, w.Body)
	}
}
