package management

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var hashOnce sync.Once
var fixtureHash PasswordHash

func httpFixture(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	hashOnce.Do(func() {
		var e error
		fixtureHash, e = HashPassword("synthetic-password-123")
		if e != nil {
			panic(e)
		}
	})
	c := authorityFixtureConfig()
	for i := range c.Accounts {
		c.Accounts[i].Password = fixtureHash
	}
	now := c.Bindings[0].From.Add(time.Hour)
	s, e := NewServer(c, Options{RecordsPath: t.TempDir(), AuditPath: filepath.Join(t.TempDir(), "audit.jsonl"), Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	return s, &now
}
func request(s *Server, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost:18791"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func login(t *testing.T, s *Server, scope, user string) *http.Cookie {
	t.Helper()
	w := request(s, "POST", "/api/"+scope+"/login", `{"username":"`+user+`","password":"synthetic-password-123"}`, nil)
	if w.Code != 200 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode || cs[0].Path != "/api/" {
		t.Fatal(cs)
	}
	return cs[0]
}
func TestHTTPSessionsIsolationExpiry(t *testing.T) {
	s, now := httpFixture(t)
	a := login(t, s, "admin", "alice")
	f := login(t, s, "family", "alice")
	for _, tt := range []struct {
		path string
		c    *http.Cookie
		want int
	}{{"/api/admin/session", a, 200}, {"/api/family/session", a, 401}, {"/api/family/families/home-b", f, 403}, {"/api/family/accounts", f, 403}, {"/api/admin/accounts", a, 200}, {"/api/admin/families", a, 200}} {
		w := request(s, "GET", tt.path, "", tt.c)
		if w.Code != tt.want {
			t.Fatalf("%s: %d %s", tt.path, w.Code, w.Body)
		}
	}
	if request(s, "POST", "/api/admin/logout", `{}`, a).Code != 200 {
		t.Fatal("logout")
	}
	if request(s, "GET", "/api/admin/session", "", a).Code != 401 || request(s, "GET", "/api/family/session", "", f).Code != 200 {
		t.Fatal("scope logout")
	}
	*now = now.Add(8 * time.Hour)
	if request(s, "GET", "/api/family/session", "", f).Code != 401 {
		t.Fatal("expiry")
	}
}
func TestHTTPGuards(t *testing.T) {
	s, _ := httpFixture(t)
	c := login(t, s, "family", "alice")
	for _, p := range []string{"?offset=-1", "?limit=101", "?offset=x", "?limit=1&limit=2", "?device=", "?device=x&device=y"} {
		w := request(s, "GET", "/api/family/conversations"+p, "", c)
		if w.Code != 400 && w.Code != 403 {
			t.Fatalf("%s %d", p, w.Code)
		}
	}
	for _, origin := range []string{"http://evil.test", "http://localhost:18790", "https://localhost:18791"} {
		r := httptest.NewRequest("POST", "http://localhost:18791/api/family/logout", strings.NewReader(`{}`))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(c)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("origin %s: %d", origin, w.Code)
		}
	}
	for _, body := range []string{`{} {}`, `{"extra":1}`, strings.Repeat("x", 4097)} {
		if request(s, "POST", "/api/family/logout", body, c).Code != 400 {
			t.Fatal("body")
		}
	}
	if request(s, "POST", "/api/family/devices", `{}`, c).Code != 405 {
		t.Fatal("write")
	}
}
func TestPasswordAndLoginLimit(t *testing.T) {
	s, _ := httpFixture(t)
	if VerifyPassword("wrong", fixtureHash) || !VerifyPassword("synthetic-password-123", fixtureHash) {
		t.Fatal("hash")
	}
	if _, e := HashPassword("short"); e == nil {
		t.Fatal("short")
	}
	for i := 0; i < 11; i++ {
		w := request(s, "POST", "/api/family/login", `{"username":"absent","password":"wrong-password"}`, nil)
		want := 401
		if i == 10 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("%d: %d", i, w.Code)
		}
	}
}
