package management

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"
)

var errConflict = errors.New("conflict")
var errExpired = errors.New("expired")
var errLimit = errors.New("limit")
var errAuthentication = errors.New("authentication")

func onboardingError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, errConflict):
		apiError(w, 409, "conflict", "设备正在添加或已经绑定")
	case errors.Is(e, errExpired):
		apiError(w, 410, "expired", "添加任务已过期")
	case errors.Is(e, errLimit):
		apiError(w, 429, "rate_limited", "添加任务过多，请稍后重试")
	case errors.Is(e, errAuthentication):
		apiError(w, 401, "unauthenticated", "设备认证失败")
	default:
		sourceError(w, e)
	}
}
func canAdd(c Config, actor, family string) bool {
	a, ok := c.AccountByID(actor)
	if !ok {
		return false
	}
	for _, m := range a.Memberships {
		if m.FamilyID == family {
			return m.CanAddDevices
		}
	}
	return false
}
func factoryByID(c Config, id string) (FactoryDevice, bool) {
	for _, d := range c.FactoryDevices {
		if d.DeviceID == id {
			return d, true
		}
	}
	return FactoryDevice{}, false
}

// Migration authority is checked against the original binding on every attempt.
// Historical ended bindings remain valid config, but cannot authorize migration.
func migrationBinding(c Config, d FactoryDevice, now time.Time) (Binding, bool) {
	for _, old := range c.Devices {
		if old.ID != d.DeviceID || old.Model != d.Model {
			continue
		}
		for _, b := range c.Bindings {
			if b.ID == d.MigrationBindingID && b.DeviceID == d.DeviceID && !now.Before(b.From) && (b.Until == nil || now.Before(*b.Until)) {
				return b, true
			}
		}
	}
	return Binding{}, false
}
func canConfirmMigration(c Config, d FactoryDevice, actor, family string, now time.Time) bool {
	b, ok := migrationBinding(c, d, now)
	if !ok || b.FamilyID != family || !canAdd(c, actor, family) {
		return false
	}
	authorized, e := c.Authorize(actor, "family", d.DeviceID, now, false)
	return e == nil && authorized.ID == b.ID
}
func claimDTO(c Config, t OnboardingClaim, challenge bool, now time.Time) any {
	d, _ := factoryByID(c, t.DeviceID)
	status := t.Status
	if status == "waiting" && !now.Before(t.ExpiresAt) {
		status = "expired"
	}
	out := map[string]any{"id": t.ID, "status": status, "expires_at": t.ExpiresAt, "serial": d.Serial, "model": d.Model}
	if challenge && status == "waiting" {
		out["challenge"] = t.Challenge
		out["ble_name"] = "FAMILY_" + strings.ReplaceAll(d.MAC, ":", "")
	}
	if t.Name != "" {
		out["name"] = t.Name
	}
	if status == "confirmed" {
		out["device_id"] = t.DeviceID
		out["binding_from"] = t.BindingFrom
	}
	return out
}
func (s *Server) familyOnboarding(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	var result any
	if len(parts) == 2 && r.Method == "POST" {
		var in struct {
			FamilyID  string `json:"family_id"`
			ClaimCode string `json:"claim_code"`
			Name      string `json:"name"`
		}
		if !decode(w, r, &in) {
			return
		}
		if !ident.MatchString(in.FamilyID) || !claimCode.MatchString(in.ClaimCode) || len(in.Name) > 256 || strings.TrimSpace(in.Name) != in.Name {
			sourceError(w, ErrInvalid)
			return
		}
		if !canAdd(requestConfig(r), a.ID, in.FamilyID) {
			sourceError(w, ErrForbidden)
			return
		}
		e := (ConfigStore{s.opts.ConfigPath}).Transaction(func(c *Config) error {
			if !canAdd(*c, a.ID, in.FamilyID) {
				return ErrForbidden
			}
			var d FactoryDevice
			found := false
			for _, v := range c.FactoryDevices {
				if v.ClaimDigest == secretDigest(in.ClaimCode) {
					d = v
					found = true
					break
				}
			}
			if !found {
				return ErrNotFound
			}
			now := s.now().UTC()
			if d.MigrationBindingID != "" && !canConfirmMigration(*c, d, a.ID, in.FamilyID, now) {
				return ErrForbidden
			}
			count := 0
			for i := range c.OnboardingClaims {
				t := &c.OnboardingClaims[i]
				if t.Status == "waiting" && !now.Before(t.ExpiresAt) {
					t.Status = "expired"
				}
				if t.Status == "waiting" && t.ActorID == a.ID {
					count++
				}
				if t.DeviceID != d.DeviceID {
					continue
				}
				if t.Status == "confirmed" || t.Status == "waiting" {
					if t.ActorID != a.ID || t.FamilyID != in.FamilyID {
						return errConflict
					}
					result = claimDTO(*c, *t, true, now)
					return nil
				}
			}
			for _, b := range c.Bindings {
				if b.DeviceID == d.DeviceID && d.MigrationBindingID == "" {
					return errConflict
				}
			}
			if count >= 8 || len(c.OnboardingClaims) >= 4096 {
				return errLimit
			}
			id, e := randomHex(16)
			if e != nil {
				return e
			}
			challenge, e := randomHex(32)
			if e != nil {
				return e
			}
			t := OnboardingClaim{ID: "claim_" + id, ActorID: a.ID, FamilyID: in.FamilyID, DeviceID: d.DeviceID, Challenge: challenge, Status: "waiting", ExpiresAt: now.Add(180 * time.Second), Name: in.Name}
			c.OnboardingClaims = append(c.OnboardingClaims, t)
			result = claimDTO(*c, t, true, now)
			return nil
		})
		if e != nil {
			onboardingError(w, e)
			return
		}
		writeJSON(w, result)
		return
	}
	if len(parts) != 3 || (r.Method != "GET" && r.Method != "DELETE") {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	id := parts[2]
	if !ident.MatchString(id) {
		sourceError(w, ErrNotFound)
		return
	}
	read := func(c *Config) error {
		for i := range c.OnboardingClaims {
			t := &c.OnboardingClaims[i]
			if t.ID != id {
				continue
			}
			if t.ActorID != a.ID {
				return ErrNotFound
			}
			if !canAdd(*c, a.ID, t.FamilyID) {
				return ErrForbidden
			}
			if r.Method == "DELETE" && t.Status == "waiting" && !s.now().Before(t.ExpiresAt) {
				t.Status = "expired"
			}
			if r.Method == "DELETE" && t.Status == "waiting" {
				t.Status = "cancelled"
			}
			result = claimDTO(*c, *t, false, s.now())
			return nil
		}
		return ErrNotFound
	}
	var e error
	if r.Method == "DELETE" {
		e = (ConfigStore{s.opts.ConfigPath}).Transaction(read)
	} else {
		c := requestConfig(r)
		e = read(&c)
	}
	if e != nil {
		onboardingError(w, e)
		return
	}
	writeJSON(w, result)
}
func authenticateFactory(c Config, mac, key string) (FactoryDevice, bool) {
	if !canonicalMAC.MatchString(mac) || len(key) != 43 {
		return FactoryDevice{}, false
	}
	got := secretDigest(key)
	for _, d := range c.FactoryDevices {
		if d.MAC == mac && subtle.ConstantTimeCompare([]byte(d.KeyDigest), []byte(got)) == 1 {
			return d, true
		}
	}
	return FactoryDevice{}, false
}
func activated(c Config, d FactoryDevice, now time.Time) bool {
	for _, t := range c.OnboardingClaims {
		if t.DeviceID != d.DeviceID || t.Status != "confirmed" || t.BindingFrom == nil {
			continue
		}
		for _, b := range c.Bindings {
			if b.DeviceID == d.DeviceID && (d.MigrationBindingID == "" || b.ID == d.MigrationBindingID) && b.FamilyID == t.FamilyID && b.From.Equal(*t.BindingFrom) && !now.Before(b.From) && (b.Until == nil || now.Before(*b.Until)) {
				return true
			}
		}
	}
	return false
}
func (s *Server) deviceEndpoint(w http.ResponseWriter, r *http.Request, c Config) bool {
	p := r.URL.Path
	if p == "/api/internal/device-auth" {
		if !loopback(r) || r.Host == "robot.songguoxue.com" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Forwarded-Proto") != "" {
			sourceError(w, ErrNotFound)
			return true
		}
		if r.Method != "POST" {
			apiError(w, 405, "method_not_allowed", "请求方法不支持")
			return true
		}
		var in struct {
			MAC   string `json:"mac"`
			Token string `json:"token"`
		}
		if !decode(w, r, &in) {
			return true
		}
		d, ok := authenticateFactory(c, in.MAC, in.Token)
		if !ok || !activated(c, d, s.now()) {
			onboardingError(w, errAuthentication)
			return true
		}
		writeJSON(w, map[string]string{"device_id": d.DeviceID, "serial": d.Serial, "model": d.Model})
		return true
	}
	if p != "/api/device/activation" && p != "/api/device/bootstrap" {
		return false
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		onboardingError(w, errAuthentication)
		return true
	}
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	d, ok := authenticateFactory(c, r.Header.Get("Device-Id"), key)
	if !ok {
		onboardingError(w, errAuthentication)
		return true
	}
	if p == "/api/device/bootstrap" {
		if r.Method != "GET" && r.Method != "POST" {
			apiError(w, 405, "method_not_allowed", "请求方法不支持")
			return true
		}
		var metadata map[string]any
		if r.Method == "POST" {
			if !decode(w, r, &metadata) {
				return true
			}
		}
		if !activated(c, d, s.now()) {
			apiError(w, 409, "waiting_activation", "请先完成设备认领")
			return true
		}
		firmware, e := s.firmwareBootstrap(r, d, key, metadata)
		if e != nil {
			onboardingError(w, e)
			return true
		}
		response := map[string]any{"websocket": map[string]any{"url": "wss://robot.songguoxue.com/xiaozhi/v1/", "version": 1}, "server_time": map[string]any{"timestamp": s.now().UnixMilli(), "timezone_offset": 480}}
		if firmware != nil {
			response["firmware"] = firmware
		}
		writeJSON(w, response)
		return true
	}
	if r.Method != "POST" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return true
	}
	var in struct {
		ID        string `json:"id"`
		Challenge string `json:"challenge"`
	}
	if !decode(w, r, &in) {
		return true
	}
	if !ident.MatchString(in.ID) || !digest.MatchString(in.Challenge) {
		sourceError(w, ErrInvalid)
		return true
	}
	var result any
	e := (ConfigStore{s.opts.ConfigPath}).Transaction(func(c *Config) error {
		current, ok := authenticateFactory(*c, d.MAC, key)
		if !ok {
			return errAuthentication
		}
		for i := range c.OnboardingClaims {
			t := &c.OnboardingClaims[i]
			if t.ID != in.ID {
				continue
			}
			if t.DeviceID != current.DeviceID || subtle.ConstantTimeCompare([]byte(t.Challenge), []byte(in.Challenge)) != 1 {
				return errAuthentication
			}
			if !canAdd(*c, t.ActorID, t.FamilyID) || (current.MigrationBindingID != "" && !canConfirmMigration(*c, current, t.ActorID, t.FamilyID, s.now())) {
				return ErrForbidden
			}
			if t.Status == "confirmed" {
				if !activated(*c, current, s.now()) {
					return errConflict
				}
				result = claimDTO(*c, *t, false, s.now())
				return nil
			}
			if t.Status == "expired" || (t.Status == "waiting" && !s.now().Before(t.ExpiresAt)) {
				return errExpired
			}
			if t.Status != "waiting" {
				return errConflict
			}
			if current.MigrationBindingID != "" {
				original, ok := migrationBinding(*c, current, s.now())
				if !ok {
					return errConflict
				}
				t.Status = "confirmed"
				t.BindingFrom = &original.From
				result = claimDTO(*c, *t, false, s.now())
				return nil
			}
			for _, b := range c.Bindings {
				if b.DeviceID == current.DeviceID {
					return errConflict
				}
			}
			now := s.now().UTC()
			t.Status = "confirmed"
			t.BindingFrom = &now
			name := t.Name
			if name == "" {
				name = "小满"
			}
			c.Devices = append(c.Devices, DeviceConfig{ID: current.DeviceID, Name: name, Model: current.Model, Location: "未设置"})
			bid, e := randomHex(16)
			if e != nil {
				return e
			}
			c.Bindings = append(c.Bindings, Binding{ID: "bind_" + bid, Version: 1, FamilyID: t.FamilyID, DeviceID: current.DeviceID, From: now})
			for ai := range c.Accounts {
				if c.Accounts[ai].ID == t.ActorID {
					for mi := range c.Accounts[ai].Memberships {
						m := &c.Accounts[ai].Memberships[mi]
						if m.FamilyID == t.FamilyID {
							m.Devices = append(m.Devices, DeviceGrant{DeviceID: current.DeviceID, CanReadBody: true})
						}
					}
				}
			}
			result = claimDTO(*c, *t, false, now)
			return nil
		}
		return ErrNotFound
	})
	if e != nil {
		onboardingError(w, e)
		return true
	}
	writeJSON(w, result)
	return true
}
