package management

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	passwordIterations = 600000
	passwordMinBytes   = 8
)

func HashPassword(password string) (PasswordHash, error) {
	if len(password) < passwordMinBytes || len(password) > 256 {
		return PasswordHash{}, ErrInvalid
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return PasswordHash{}, ErrSource
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if e != nil {
		return PasswordHash{}, ErrSource
	}
	return PasswordHash{hex.EncodeToString(salt), hex.EncodeToString(key), passwordIterations}, nil
}
func VerifyPassword(password string, h PasswordHash) bool {
	if len(password) < passwordMinBytes || len(password) > 256 || h.Iterations < 600000 || h.Iterations > 2000000 {
		return false
	}
	salt, e := hex.DecodeString(h.Salt)
	if e != nil || len(salt) < 16 || len(salt) > 64 {
		return false
	}
	want, e := hex.DecodeString(h.Digest)
	if e != nil || len(want) != 32 {
		return false
	}
	got, e := pbkdf2.Key(sha256.New, password, salt, h.Iterations, 32)
	return e == nil && subtle.ConstantTimeCompare(got, want) == 1
}

type session struct {
	actor, scope string
	expires      time.Time
}
type loginWindow struct {
	start time.Time
	count int
}

func cookieName(scope string) string { return "management_" + scope }
func (s *Server) prune() {
	now := s.now()
	for k, v := range s.sessions {
		if !now.Before(v.expires) {
			delete(s.sessions, k)
		}
	}
	for k, v := range s.attempts {
		if !now.Before(v.start.Add(time.Minute)) {
			delete(s.attempts, k)
		}
	}
}
func (s *Server) account(r *http.Request, scope string) (AccountConfig, bool) {
	c, e := r.Cookie(cookieName(scope))
	if e != nil || len(c.Value) != 64 {
		return AccountConfig{}, false
	}
	key := sha256.Sum256([]byte(c.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	v, ok := s.sessions[key]
	if !ok || v.scope != scope {
		return AccountConfig{}, false
	}
	a, ok := requestConfig(r).AccountByID(v.actor)
	return a, ok && (scope != "admin" || a.InternalAdmin)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request, scope string) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	// Only the explicitly configured HTTPS proxy is trusted to identify its
	// immediate client. Use the final entry, never an arbitrary supplied prefix.
	if s.publicRequest(r) {
		forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if parsed := net.ParseIP(strings.TrimSpace(forwarded[len(forwarded)-1])); parsed != nil {
			ip = parsed.String()
		}
	}
	s.mu.Lock()
	s.prune()
	v, exists := s.attempts[ip]
	if (!exists && len(s.attempts) >= 4096) || v.count >= 10 {
		s.mu.Unlock()
		apiError(w, 429, "rate_limited", "尝试过于频繁，请稍后重试")
		return
	}
	if !exists {
		v.start = s.now()
	}
	v.count++
	s.attempts[ip] = v
	s.mu.Unlock()
	select {
	case s.kdf <- struct{}{}:
		defer func() { <-s.kdf }()
	default:
		apiError(w, 429, "rate_limited", "尝试过于频繁，请稍后重试")
		return
	}
	a, ok := requestConfig(r).FindAccount(in.Username)
	h := s.dummy
	if ok {
		h = a.Password
	}
	valid := VerifyPassword(in.Password, h)
	if !valid || !ok || (scope == "admin" && !a.InternalAdmin) {
		apiError(w, 401, "invalid_login", "账号或密码不正确")
		return
	}
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		sourceError(w, ErrSource)
		return
	}
	token := hex.EncodeToString(raw)
	s.mu.Lock()
	s.prune()
	if len(s.sessions) >= 1024 {
		s.mu.Unlock()
		sourceError(w, ErrSource)
		return
	}
	s.sessions[sha256.Sum256([]byte(token))] = session{a.ID, scope, s.now().Add(8 * time.Hour)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName(scope), Value: token, Path: "/api/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.publicRequest(r), MaxAge: 28800})
	writeJSON(w, frontendAccount(a, scope))
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request, scope string) {
	var in struct{}
	if !decode(w, r, &in) {
		return
	}
	c, _ := r.Cookie(cookieName(scope))
	s.mu.Lock()
	delete(s.sessions, sha256.Sum256([]byte(c.Value)))
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName(scope), Path: "/api/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.publicRequest(r), MaxAge: -1})
	writeJSON(w, struct{}{})
}
func frontendAccount(a AccountConfig, scope string) any {
	role := "家庭成员"
	if scope == "admin" {
		role = "内部管理员"
	}
	return struct {
		Email string `json:"email"`
		Role  string `json:"role"`
		Scope string `json:"scope"`
	}{a.Username, role, scope}
}
