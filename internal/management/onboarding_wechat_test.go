package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWeChatOnboardingSourceBoundary(t *testing.T) {
	s := &Server{opts: Options{PublicOrigin: onboardingOrigin}, origin: http.NewCrossOriginProtection()}
	valid := "https://servicewechat.com/wxb3f1745f8ed1fa1d/devtools/page-frame.html"
	for _, tt := range []struct {
		name, method, path, origin, referer, proto, remote string
		want                                               bool
	}{
		{"devtools", "POST", "/api/family/login", "", valid, "https", "127.0.0.1:1", true},
		{"phone", "POST", "/api/family/onboarding", "", strings.Replace(valid, "devtools", "12", 1), "https", "127.0.0.1:1", true},
		{"logout", "POST", "/api/family/logout", "", valid, "https", "127.0.0.1:1", true},
		{"cancel", "DELETE", "/api/family/onboarding/claim_" + strings.Repeat("a", 32), "", valid, "https", "127.0.0.1:1", true},
		{"no-referrer", "POST", "/api/family/login", "", "", "https", "127.0.0.1:1", false},
		{"foreign-origin", "POST", "/api/family/login", "https://evil.test", valid, "https", "127.0.0.1:1", false},
		{"null-origin", "POST", "/api/family/login", "null", valid, "https", "127.0.0.1:1", false},
		{"other-app", "POST", "/api/family/login", "", strings.Replace(valid, "wxb3f1745f8ed1fa1d", "wx1111111111111111", 1), "https", "127.0.0.1:1", false},
		{"foreign-host", "POST", "/api/family/login", "", strings.Replace(valid, "servicewechat.com", "servicewechat.com.evil.test", 1), "https", "127.0.0.1:1", false},
		{"http-referrer", "POST", "/api/family/login", "", strings.Replace(valid, "https:", "http:", 1), "https", "127.0.0.1:1", false},
		{"referrer-port", "POST", "/api/family/login", "", strings.Replace(valid, ".com/", ".com:443/", 1), "https", "127.0.0.1:1", false},
		{"referrer-userinfo", "POST", "/api/family/login", "", strings.Replace(valid, "://", "://user@", 1), "https", "127.0.0.1:1", false},
		{"query", "POST", "/api/family/login", "", valid + "?x=1", "https", "127.0.0.1:1", false},
		{"empty-query", "POST", "/api/family/login", "", valid + "?", "https", "127.0.0.1:1", false},
		{"fragment", "POST", "/api/family/login", "", valid + "#x", "https", "127.0.0.1:1", false},
		{"encoded-path", "POST", "/api/family/login", "", strings.Replace(valid, "devtools", "%64evtools", 1), "https", "127.0.0.1:1", false},
		{"wrong-page", "POST", "/api/family/login", "", strings.Replace(valid, "page-frame.html", "other.html", 1), "https", "127.0.0.1:1", false},
		{"wrong-version", "POST", "/api/family/login", "", strings.Replace(valid, "devtools", "other", 1), "https", "127.0.0.1:1", false},
		{"admin", "POST", "/api/admin/login", "", valid, "https", "127.0.0.1:1", false},
		{"device", "POST", "/api/device/activation", "", valid, "https", "127.0.0.1:1", false},
		{"unrelated-family-write", "POST", "/api/family/devices", "", valid, "https", "127.0.0.1:1", false},
		{"wrong-method", "PUT", "/api/family/onboarding", "", valid, "https", "127.0.0.1:1", false},
		{"invalid-cancel-id", "DELETE", "/api/family/onboarding/claim_bad", "", valid, "https", "127.0.0.1:1", false},
		{"untrusted-proxy", "POST", "/api/family/login", "", valid, "https", "203.0.113.1:1", false},
		{"private-http", "POST", "/api/family/login", "", valid, "", "127.0.0.1:1", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, onboardingOrigin+tt.path, nil)
			r.RemoteAddr = tt.remote
			r.Header.Set("X-Forwarded-Proto", tt.proto)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("Referer", tt.referer)
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			if got := s.checkOrigin(r); got != tt.want {
				t.Fatalf("source accepted=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestWeChatNativeLoginStillRequiresFamilyCredentials(t *testing.T) {
	s, _ := httpFixture(t)
	s.opts.PublicOrigin = onboardingOrigin
	for _, tt := range []struct {
		username, password string
		want               int
	}{
		{"nativepreviewinvalid", "invalidpassword", 401},
		{"alice", "synthetic-password-123", 200},
	} {
		r := httptest.NewRequest("POST", onboardingOrigin+"/api/family/login", strings.NewReader(`{"username":"`+tt.username+`","password":"`+tt.password+`"}`))
		r.RemoteAddr = "127.0.0.1:1"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Referer", "https://servicewechat.com/wxb3f1745f8ed1fa1d/devtools/page-frame.html")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tt.want || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("login status=%d want=%d", w.Code, tt.want)
		}
		cookies := w.Result().Cookies()
		if tt.want == 401 && len(cookies) != 0 {
			t.Fatal("bad credentials received session")
		}
		if tt.want == 200 && (len(cookies) != 1 || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode) {
			t.Fatal("public cookie protection changed")
		}
	}
}
