package management

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func httpRecord(t *testing.T, s *Server, id, dev string, at time.Time, body string) {
	t.Helper()
	info, _ := projectionFixture()
	info.SchemaVersion = 1
	info.DeviceID = dev
	info.StartedAt = at
	p := filepath.Join(s.opts.RecordsPath, id)
	if e := os.Mkdir(p, 0700); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(info)
	if e := os.WriteFile(filepath.Join(p, "session.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(p, "events.jsonl"), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestHTTPBodyIsolationAudit(t *testing.T) {
	s, now := httpFixture(t)
	a := login(t, s, "family", "alice")
	m := login(t, s, "family", "metadata")
	o := login(t, s, "admin", "observer")
	id := "20261109T010000Z-0123456789abcdef0123456789abcdef"
	httpRecord(t, s, id, s.cfg.Devices[0].ID, *now, "")
	other := "20261109T010001Z-0123456789abcdef0123456789abcdef"
	httpRecord(t, s, other, s.cfg.Devices[1].ID, *now, "invalid body\n")
	old := "20261108T010001Z-0123456789abcdef0123456789abcdef"
	httpRecord(t, s, old, s.cfg.Devices[0].ID, s.cfg.Bindings[0].From.Add(-time.Second), "invalid body\n")
	for _, tt := range []struct {
		path string
		who  int
		want int
	}{{id, 0, 200}, {id, 1, 403}, {id, 2, 404}, {other, 0, 404}, {old, 0, 404}, {id + "?device=" + s.cfg.Devices[1].ID, 0, 403}} {
		c := a
		scope := "family"
		if tt.who == 1 {
			c = m
		}
		if tt.who == 2 {
			c = o
			scope = "admin"
		}
		w := request(s, "GET", "/api/"+scope+"/conversations/"+tt.path, "", c)
		if w.Code != tt.want {
			t.Fatalf("%s %d: %d %s", tt.path, tt.who, w.Code, w.Body)
		}
	}
	raw, e := os.ReadFile(s.opts.AuditPath)
	if e != nil || !strings.Contains(string(raw), `"actorID":"alice"`) || strings.Contains(string(raw), "turns") || strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("audit %s %v", raw, e)
	}
	s.auditWrite = func(string, auditEntry) error { return errors.New("private-path-secret") }
	w := request(s, "GET", "/api/family/conversations/"+id, "", a)
	if w.Code != 503 || strings.Contains(w.Body.String(), "turns") || strings.Contains(w.Body.String(), "private-path") {
		t.Fatalf("failclosed %d %s", w.Code, w.Body)
	}
	w = request(s, "GET", "/api/family/conversations", "", m)
	if w.Code != 200 || strings.Contains(w.Body.String(), "invalid body") || strings.Contains(w.Body.String(), old) || strings.Contains(w.Body.String(), other) {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
}
func TestHTTPStaticPrivateAndSymlinks(t *testing.T) {
	s, _ := httpFixture(t)
	root := t.TempDir()
	s.opts.StaticDir = root
	os.Mkdir(filepath.Join(root, "assets"), 0700)
	os.Mkdir(filepath.Join(root, "private"), 0700)
	os.WriteFile(filepath.Join(root, "index.html"), []byte("INDEX"), 0600)
	os.WriteFile(filepath.Join(root, "private", "secret.js"), []byte("SECRET"), 0600)
	os.Symlink("../private", filepath.Join(root, "assets", "nested"))
	for _, p := range []string{"/.env", "/config.json", "/assets/", "/assets/nested/secret.js", "/assets/missing.js", "/api/family/unknown"} {
		w := request(s, "GET", p, "", nil)
		if w.Code == 200 || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("private %s %d %s", p, w.Code, w.Body)
		}
	}
	for _, p := range []string{"/", "/family/devices", "/admin/login"} {
		if w := request(s, "GET", p, "", nil); w.Code != 200 || w.Body.String() != "INDEX" {
			t.Fatalf("spa %s: %d", p, w.Code)
		}
	}
}
func TestHTTPSessionAndLimiterCapacity(t *testing.T) {
	s, now := httpFixture(t)
	for i := 0; i < 1024; i++ {
		s.sessions[sha256.Sum256([]byte(fmt.Sprint(i)))] = session{"alice", "family", now.Add(time.Hour)}
	}
	w := request(s, "POST", "/api/family/login", `{"username":"alice","password":"synthetic-password-123"}`, nil)
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("session bound")
	}
	*now = now.Add(time.Hour)
	login(t, s, "family", "alice")
	if len(s.sessions) != 1 {
		t.Fatal("prune")
	}
	s.kdf <- struct{}{}
	s.kdf <- struct{}{}
	w = request(s, "POST", "/api/family/login", `{"username":"alice","password":"synthetic-password-123"}`, nil)
	if w.Code != 429 {
		t.Fatal("kdf bound")
	}
	<-s.kdf
	<-s.kdf
	for i := 0; i < 4096; i++ {
		s.attempts[fmt.Sprint(i)] = loginWindow{*now, 1}
	}
	r := httptest.NewRequest("POST", "http://localhost/api/family/login", strings.NewReader(`{"username":"alice","password":"synthetic-password-123"}`))
	r.RemoteAddr = "10.20.30.40:80"
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, r)
	if rr.Code != 429 {
		t.Fatal("limiter bound")
	}
}
func TestHTTPFamilyMetadataAndUnsupported(t *testing.T) {
	s, _ := httpFixture(t)
	a := login(t, s, "admin", "alice")
	w := request(s, "GET", "/api/admin/families/home-a", "", a)
	for _, secret := range []string{"password", "salt", "digest", "home-b", "bob"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("metadata leak", secret)
		}
	}
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, p := range []string{"devices", "memory", "conversations/abc", "families/home-a"} {
			if w := request(s, method, "/api/admin/"+p, `{}`, a); w.Code != 404 && w.Code != 405 {
				t.Fatal("unsupported", w.Code)
			}
		}
	}
	for _, body := range []string{`null`, `{"username":"alice","username":"bob","password":"synthetic-password-123"}`} {
		if w := request(s, "POST", "/api/admin/login", body, nil); w.Code != 400 {
			t.Fatal("ambiguous JSON", w.Code)
		}
	}
	w = request(s, "POST", "/api/admin/login", `{"username":"bob","password":"synthetic-password-123"}`, nil)
	if w.Code != 401 {
		t.Fatal("non admin login")
	}
}

func TestHTTPBrowserOriginAndContentType(t *testing.T) {
	s, _ := httpFixture(t)
	for _, tt := range []struct {
		origin, site, content string
		want                  int
	}{
		{"http://localhost:18791", "same-origin", "application/json", 401},
		{"", "cross-site", "application/json", 403},
		{"null", "", "application/json", 403},
		{"", "", "text/plain", 400},
	} {
		r := httptest.NewRequest("POST", "http://localhost:18791/api/family/login", strings.NewReader(`{"username":"missing","password":"synthetic-password-123"}`))
		r.Header.Set("Content-Type", tt.content)
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Sec-Fetch-Site", tt.site)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tt.want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%+v: %d", tt, w.Code)
		}
	}
}
