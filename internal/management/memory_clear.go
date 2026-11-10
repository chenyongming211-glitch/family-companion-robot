package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func memoryClearAuthority(c Config, actor, id string, now time.Time) (Binding, error) {
	binding, e := c.Authorize(actor, "family", id, now, true)
	if e != nil {
		return Binding{}, e
	}
	a, ok := c.AccountByID(actor)
	if !ok {
		return Binding{}, ErrForbidden
	}
	for _, m := range a.Memberships {
		if m.FamilyID == binding.FamilyID && m.CanAddDevices {
			return binding, nil
		}
	}
	return Binding{}, ErrForbidden
}

// NewMemoryClearGuard is composed into voice by its command. The snapshot lock
// is the same inode used by every config writer and remains held through fn.
func NewMemoryClearGuard(path string) realtimelive.MemoryClearGuard {
	return func(ctx context.Context, claim realtimelive.MemoryClearClaim, fn func() error) error {
		return (ConfigStore{path}).WithSnapshot(ctx, func(c Config) error {
			b, e := memoryClearAuthority(c, claim.ActorID, claim.DeviceID, time.Now())
			if e != nil || b.ID != claim.BindingID || b.Version != claim.BindingVersion || b.FamilyID != claim.FamilyID || !time.Now().Before(claim.Deadline) {
				return realtimelive.ErrMemoryClearForbidden
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			return fn()
		})
	}
}

func familyMemoryClearRequest(r *http.Request) bool {
	const prefix = "/api/family/memory-clear/"
	return r.Method == "POST" && strings.HasPrefix(r.URL.Path, prefix) && device.MatchString(strings.TrimPrefix(r.URL.Path, prefix)) && r.URL.RawQuery == ""
}
func memoryClearError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
		sourceError(w, err)
		return
	}
	apiError(w, 503, "memory_clear_unconfirmed", "清除结果未确认，请刷新查看")
}

func (s *Server) familyMemoryClear(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	if len(parts) != 3 || !device.MatchString(parts[2]) || r.URL.RawQuery != "" {
		sourceError(w, ErrInvalid)
		return
	}
	if r.Method != "POST" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	// A map enforces exact spelling as well as unknown/duplicate rejection in decode.
	var in map[string]json.RawMessage
	if !decode(w, r, &in) {
		return
	}
	if len(in) != 1 || string(in["confirm"]) != "true" {
		sourceError(w, ErrInvalid)
		return
	}
	var claim realtimelive.MemoryClearClaim
	e := (ConfigStore{s.opts.ConfigPath}).WithSnapshot(r.Context(), func(c Config) error {
		fresh := r.WithContext(context.WithValue(r.Context(), configContextKey{}, c))
		current, ok := s.account(fresh, "family")
		if !ok || current.ID != a.ID {
			return ErrForbidden
		}
		b, e := memoryClearAuthority(c, current.ID, parts[2], s.now())
		if e != nil {
			return e
		}
		claim = realtimelive.MemoryClearClaim{DeviceID: parts[2], ActorID: current.ID, BindingID: b.ID, BindingVersion: b.Version, FamilyID: b.FamilyID, Deadline: time.Now().Add(5 * time.Second)}
		return s.auditAction(current.ID, "family", "clear_memory_requested", parts[2], "")
	})
	if e != nil {
		memoryClearError(w, e)
		return
	}
	// Management must release its config lock before voice acquires the same lock.
	e = clearMemoryRPC(r.Context(), s.opts.MemoryControlSocket, claim)
	switch {
	case e == nil:
		writeJSON(w, map[string]string{"deviceId": parts[2], "status": "confirmed"})
	case errors.Is(e, realtimelive.ErrMemoryBusy):
		apiError(w, 409, "memory_busy", "机器人正在对话，请结束后再清除")
	case errors.Is(e, ErrForbidden):
		sourceError(w, e)
	default:
		apiError(w, 503, "memory_clear_unconfirmed", "清除结果未确认，请刷新查看")
	}
}

func clearMemoryRPC(ctx context.Context, path string, claim realtimelive.MemoryClearClaim) error {
	if realtimelive.ValidateMemoryControlSocket(path) != nil {
		return ErrSource
	}
	ctx, cancel := context.WithDeadline(ctx, claim.Deadline)
	defer cancel()
	raw, e := json.Marshal(claim)
	if e != nil {
		return ErrSource
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "POST", "http://memory-control/memory/clear", bytes.NewReader(raw))
	if e != nil {
		return ErrSource
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return ErrSource
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return realtimelive.ErrMemoryBusy
	}
	if resp.StatusCode == 403 {
		return ErrForbidden
	}
	if resp.StatusCode != 200 {
		return ErrSource
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if e != nil || len(body) > 4096 {
		return ErrSource
	}
	var result map[string]json.RawMessage
	d := json.NewDecoder(bytes.NewReader(body))
	if uniqueConfigKeys(d, 0) != nil {
		return ErrSource
	}
	if json.Unmarshal(body, &result) != nil || len(result) != 2 {
		return ErrSource
	}
	var id, status string
	if json.Unmarshal(result["deviceId"], &id) != nil || json.Unmarshal(result["status"], &status) != nil || id != claim.DeviceID || status != "confirmed" {
		return ErrSource
	}
	return nil
}
