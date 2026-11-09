package management

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic test fixture only; never use this value for a real account.
const eightByteTestPassword = "T8-fixt!"

// Build an independent real digest so VerifyPassword is tested even when
// HashPassword rejects the supplied length.
func passwordTestDigest(t *testing.T, password string) PasswordHash {
	t.Helper()
	salt := []byte("synthetic-salt-16")
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		t.Fatal(err)
	}
	return PasswordHash{Salt: hex.EncodeToString(salt), Digest: hex.EncodeToString(key), Iterations: 600000}
}

func TestPasswordByteBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, password string
		valid          bool
	}{
		{"seven", strings.Repeat("x", 7), false},
		{"eight", eightByteTestPassword, true},
		{"long-existing", "synthetic-password-123", true},
		{"max-256", strings.Repeat("x", 256), true},
		{"over-257", strings.Repeat("x", 257), false},
		{"utf8-seven-bytes", "测试x", false},
		{"utf8-eight-bytes", "测试xy", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifyPassword(tt.password, passwordTestDigest(t, tt.password)); got != tt.valid {
				t.Errorf("VerifyPassword accepted=%v, want %v", got, tt.valid)
			}
			h, err := HashPassword(tt.password)
			if !tt.valid {
				if !errors.Is(err, ErrInvalid) || h != (PasswordHash{}) {
					t.Fatalf("invalid length: hash=%+v err=%v", h, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Iterations != 600000 || len(h.Salt) != 32 || len(h.Digest) != 64 {
				t.Fatal("password hash parameters changed")
			}
			if !VerifyPassword(tt.password, h) || VerifyPassword("!"+tt.password[1:], h) {
				t.Fatal("password round trip or same-length wrong-password rejection failed")
			}
		})
	}
}

func TestEightBytePasswordRandomSalt(t *testing.T) {
	a, err := HashPassword(eightByteTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword(eightByteTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if a.Salt == b.Salt || a.Digest == b.Digest || !VerifyPassword(eightByteTestPassword, a) || !VerifyPassword(eightByteTestPassword, b) {
		t.Fatal("independently salted hashes must differ and both verify")
	}
}

func TestEightBytePasswordHTTPFamilySession(t *testing.T) {
	c := authorityFixtureConfig()
	// The username does not grant the existing family member internal access.
	c.Accounts[1].Username = "admin"
	c.Accounts[1].Password = passwordTestDigest(t, eightByteTestPassword)
	s, err := NewServer(c, Options{RecordsPath: t.TempDir(), AuditPath: filepath.Join(t.TempDir(), "audit.jsonl"), PublicOrigin: onboardingOrigin})
	if err != nil {
		t.Fatal(err)
	}
	publicRequest := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, onboardingOrigin+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Origin", onboardingOrigin)
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	for _, password := range []string{"X8-fixt!", strings.Repeat("x", 7), strings.Repeat("x", 257)} {
		w := publicRequest("POST", "/api/family/login", `{"username":"admin","password":"`+password+`"}`, nil)
		if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
			t.Fatalf("wrong password: status=%d cookies=%d", w.Code, len(w.Result().Cookies()))
		}
	}
	w := publicRequest("POST", "/api/family/login", `{"username":"admin","password":"`+eightByteTestPassword+`"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("eight-byte login status=%d, want 200", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "management_family" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/api/" || cookies[0].MaxAge != 28800 {
		t.Fatal("family session cookie protection changed")
	}
	for _, tt := range []struct {
		path string
		want int
	}{
		{"/api/family/session", http.StatusOK},
		{"/api/family/families/home-b", http.StatusOK},
		{"/api/family/families/home-a", http.StatusForbidden},
		{"/api/admin/session", http.StatusUnauthorized},
	} {
		if w := publicRequest("GET", tt.path, "", cookies[0]); w.Code != tt.want {
			t.Errorf("%s status=%d, want %d", tt.path, w.Code, tt.want)
		}
	}
	w = publicRequest("POST", "/api/admin/login", `{"username":"admin","password":"`+eightByteTestPassword+`"}`, nil)
	if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
		t.Fatal("family username must not grant internal admin login")
	}
}
