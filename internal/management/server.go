package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Options struct {
	RecordsPath, ProfilesPath, StaticDir, AuditPath, Build string
	ConfigPath, PublicOrigin                               string
	Now                                                    func() time.Time
}
type Server struct {
	runtimeMu  sync.Mutex
	runtimes   map[string]*runtimeState
	cfg        Config
	opts       Options
	reader     Reader
	now        func() time.Time
	mu         sync.Mutex
	sessions   map[[32]byte]session
	attempts   map[string]loginWindow
	kdf        chan struct{}
	dummy      PasswordHash
	origin     *http.CrossOriginProtection
	auditMu    sync.Mutex
	auditWrite func(string, auditEntry) error
}

func NewServer(c Config, o Options) (*Server, error) {
	if ValidateConfig(c) != nil || o.RecordsPath == "" || o.AuditPath == "" {
		return nil, ErrInvalid
	}
	if o.PublicOrigin != "" && o.PublicOrigin != onboardingOrigin {
		return nil, ErrInvalid
	}
	if o.ConfigPath != "" {
		if _, e := LoadConfig(o.ConfigPath); e != nil {
			return nil, e
		}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	dummy := PasswordHash{Salt: strings.Repeat("00", 16), Digest: strings.Repeat("00", 32), Iterations: 600000}
	s := &Server{cfg: c, opts: o, reader: Reader{Config: c, Root: o.RecordsPath, ProfilesPath: o.ProfilesPath, Now: o.Now}, now: o.Now, sessions: map[[32]byte]session{}, attempts: map[string]loginWindow{}, kdf: make(chan struct{}, 2), dummy: dummy, origin: http.NewCrossOriginProtection(), auditWrite: appendAudit}
	return s, nil
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
func sourceError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, ErrInvalid):
		apiError(w, 400, "invalid_request", "请求参数无效")
	case errors.Is(e, ErrForbidden):
		apiError(w, 403, "forbidden", "无权访问此资源")
	case errors.Is(e, ErrNotFound):
		apiError(w, 404, "not_found", "资源不存在")
	default:
		apiError(w, 503, "source_unavailable", "记录暂时不可用，请稍后重试")
	}
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || mt != "application/json" {
		sourceError(w, ErrInvalid)
		return false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if e != nil || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' || uniqueConfigKeys(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		sourceError(w, ErrInvalid)
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		sourceError(w, ErrInvalid)
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		sourceError(w, ErrInvalid)
		return false
	}
	return true
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		s.static(w, r)
		return
	}
	c, e := s.snapshot()
	if e != nil {
		sourceError(w, ErrSource)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), configContextKey{}, c))
	reader := s.reader
	reader.Config = c
	if !s.checkOrigin(r) {
		sourceError(w, ErrForbidden)
		return
	}
	if r.URL.Path == "/api/device/state" {
		s.deviceState(w, r, c)
		return
	}
	if s.deviceEndpoint(w, r, c) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	if len(parts) < 2 || len(parts) > 3 || (parts[0] != "admin" && parts[0] != "family") {
		sourceError(w, ErrNotFound)
		return
	}
	scope, resource := parts[0], parts[1]
	if resource == "login" && len(parts) == 2 {
		if r.Method != "POST" {
			apiError(w, 405, "method_not_allowed", "请求方法不支持")
			return
		}
		s.login(w, r, scope)
		return
	}
	a, ok := s.account(r, scope)
	if !ok {
		apiError(w, 401, "unauthenticated", "请先登录")
		return
	}
	if resource == "logout" && len(parts) == 2 {
		if r.Method != "POST" {
			apiError(w, 405, "method_not_allowed", "请求方法不支持")
			return
		}
		s.logout(w, r, scope)
		return
	}
	if resource == "onboarding" && scope == "family" {
		s.familyOnboarding(w, r, a, parts)
		return
	}
	if resource == "device-runtime" && scope == "family" {
		s.deviceRuntime(w, r, a, parts)
		return
	}
	if resource == "device-settings" && scope == "family" {
		s.deviceSettings(w, r, a, parts)
		return
	}
	if r.Method != "GET" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	id := ""
	if len(parts) == 3 {
		id = parts[2]
		if id == "" {
			sourceError(w, ErrNotFound)
			return
		}
	}
	var result any
	switch resource {
	case "session":
		if len(parts) != 2 {
			e = ErrNotFound
		} else {
			result = frontendAccount(a, scope)
		}
	case "devices":
		if id == "" {
			result, e = reader.ListDevices(a.ID, scope)
		} else {
			result, e = reader.GetDevice(a.ID, scope, id)
		}
	case "conversations":
		var f ConversationFilter
		f, e = parseFilter(r, id != "")
		if e == nil {
			if id == "" {
				result, e = reader.ListConversations(a.ID, scope, f)
			} else {
				var c ConversationDTO
				c, e = reader.ReadConversation(a.ID, scope, id, f.DeviceID)
				if e == nil {
					e = s.audit(a.ID, scope, c.DeviceID, c.ID)
				}
				result = c
			}
		}
	case "chats":
		var f ConversationFilter
		f, e = parseFilter(r, id != "")
		if e == nil {
			action, device := "list_chats", ""
			if f.DeviceID != nil {
				device = *f.DeviceID
			}
			if id == "" {
				result, e = reader.ListChats(a.ID, scope, f)
			} else {
				action = "read_chat"
				var c ConversationDTO
				c, e = reader.ReadChat(a.ID, scope, id, f.DeviceID)
				result, device = c, c.DeviceID
			}
			if e == nil {
				e = s.auditAction(a.ID, scope, action, device, id)
			}
		}
	case "memories":
		var f ConversationFilter
		f, e = parseFilter(r, true)
		if e == nil && f.DeviceID == nil {
			e = ErrInvalid
		}
		if e == nil {
			if id == "" {
				result, e = reader.ListMemories(a.ID, scope, *f.DeviceID)
			} else {
				result, e = reader.ReadMemory(a.ID, scope, id, *f.DeviceID)
			}
			if e == nil {
				action := "read_memories"
				if id != "" {
					action = "read_memory"
				}
				e = s.auditAction(a.ID, scope, action, *f.DeviceID, id)
			}
		}
	case "families":
		result, e = s.families(c, a, id)
	case "accounts":
		if scope != "admin" {
			e = ErrForbidden
		} else if id != "" {
			e = ErrNotFound
		} else {
			out := []map[string]string{}
			for _, ac := range c.Accounts {
				if ac.InternalAdmin {
					out = append(out, map[string]string{"name": ac.Username, "email": ac.Username, "role": "内部管理员", "access": "内部工作权限"})
				}
			}
			result = out
		}
	default:
		e = ErrNotFound
	}
	if e != nil {
		sourceError(w, e)
		return
	}
	writeJSON(w, result)
}
func parseFilter(r *http.Request, detail bool) (ConversationFilter, error) {
	f := ConversationFilter{Limit: 50}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return f, ErrInvalid
	}
	for k, v := range q {
		if len(v) != 1 {
			return f, ErrInvalid
		}
		switch k {
		case "device":
			d := v[0]
			f.DeviceID = &d
		case "offset", "limit":
			if detail {
				return f, ErrInvalid
			}
			n, e := strconv.Atoi(v[0])
			if e != nil || n < 0 {
				return f, ErrInvalid
			}
			if k == "offset" {
				f.Offset = n
			} else {
				f.Limit = n
			}
		default:
			return f, ErrInvalid
		}
	}
	if f.Limit < 1 || f.Limit > 100 || f.Offset > 10000 {
		return f, ErrInvalid
	}
	return f, nil
}
func (s *Server) families(c Config, a AccountConfig, id string) (any, error) {
	out := []map[string]any{}
	for _, m := range a.Memberships {
		for _, f := range c.Families {
			if f.ID != m.FamilyID || (id != "" && id != f.ID) {
				continue
			}
			members := []map[string]string{}
			if id != "" {
				for _, ac := range c.Accounts {
					for _, am := range ac.Memberships {
						if am.FamilyID == id {
							members = append(members, map[string]string{"id": ac.ID, "name": ac.Username, "email": ac.Username, "role": "家庭成员", "access": "已配置设备权限", "status": "有效"})
						}
					}
				}
			}
			out = append(out, map[string]any{"id": f.ID, "name": f.Name, "members": members, "canAddDevices": m.CanAddDevices})
		}
	}
	if id != "" {
		if len(out) == 0 {
			return nil, ErrForbidden
		}
		return out[0], nil
	}
	return out, nil
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.NotFound(w, r)
		return
	}
	p := r.URL.Path
	if s.opts.StaticDir == "" || strings.Contains(p, "\\") || path.Clean(p) != p && p != "/" {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		name = "index.html"
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			http.NotFound(w, r)
			return
		}
	}
	if name != "index.html" && name != "family.html" && !strings.HasPrefix(name, "assets/") {
		if !strings.Contains(path.Base(name), ".") {
			name = "index.html"
		} else {
			http.NotFound(w, r)
			return
		}
	}
	root, e := os.OpenRoot(s.opts.StaticDir)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	segments := strings.Split(name, "/")
	for _, segment := range segments[:len(segments)-1] {
		st, e := root.Lstat(segment)
		if e != nil || !st.IsDir() {
			http.NotFound(w, r)
			return
		}
		next, e := root.OpenRoot(segment)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		defer next.Close()
		opened, e := next.Stat(".")
		if e != nil || !os.SameFile(st, opened) {
			http.NotFound(w, r)
			return
		}
		root = next
	}
	leaf := segments[len(segments)-1]
	st, e := root.Lstat(leaf)
	if e != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, e := root.OpenFile(leaf, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !opened.Mode().IsRegular() || !os.SameFile(st, opened) || opened.Size() > 16<<20 {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, st.ModTime(), f)
}
