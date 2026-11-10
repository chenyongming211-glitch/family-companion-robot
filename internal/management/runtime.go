package management

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var runtimeBoot = regexp.MustCompile(`^[a-f0-9]{32}$`)
var runtimeCommandID = regexp.MustCompile(`^cmd_[a-f0-9]{32}$`)

type runtimeCommand struct {
	ID        string    `json:"id"`
	Field     string    `json:"field"`
	Value     int       `json:"value"`
	Status    string    `json:"status"`
	Actual    *int      `json:"actual"`
	ExpiresAt time.Time `json:"expiresAt"`
	actor     string
	binding   Binding
	boot      string
}
type runtimeState struct {
	boot               string
	seq                uint64
	seen               time.Time
	volume, brightness *int
	retired            []string
	command            *runtimeCommand
}
type RuntimeDTO struct {
	DeviceID   string          `json:"deviceId"`
	Connection string          `json:"connection"`
	SeenAt     string          `json:"seenAt"`
	Boot       string          `json:"boot"`
	Volume     *int            `json:"volume"`
	Brightness *int            `json:"brightness"`
	CanControl bool            `json:"canControl"`
	Command    *runtimeCommand `json:"command"`
}

func runtimePending(c *runtimeCommand) bool {
	return c != nil && (c.Status == "queued" || c.Status == "sent")
}
func runtimeExpire(v *runtimeState, now time.Time) {
	if runtimePending(v.command) && !now.Before(v.command.ExpiresAt) {
		v.command.Status = "unknown"
	}
	if !v.seen.IsZero() && now.Sub(v.seen) >= 20*time.Second {
		v.volume = nil
		v.brightness = nil
	}
}
func runtimePermission(c Config, actor, id string, now time.Time) (Binding, bool, error) {
	b, e := c.Authorize(actor, "family", id, now, false)
	if e != nil {
		return b, false, e
	}
	a, _ := c.AccountByID(actor)
	for _, m := range a.Memberships {
		if m.FamilyID == b.FamilyID && m.CanAddDevices {
			return b, true, nil
		}
	}
	return b, false, nil
}
func (s *Server) runtimeView(id string, v *runtimeState, can bool) RuntimeDTO {
	out := RuntimeDTO{DeviceID: id, Connection: "unknown", CanControl: can}
	if v == nil {
		return out
	}
	runtimeExpire(v, s.now())
	out.Boot = v.boot
	out.Command = v.command
	out.Volume = v.volume
	out.Brightness = v.brightness
	if !v.seen.IsZero() {
		out.SeenAt = v.seen.UTC().Format(time.RFC3339Nano)
		out.Connection = "offline"
		if s.now().Sub(v.seen) < 20*time.Second {
			out.Connection = "online"
		}
	}
	return out
}
func runtimeFields(w http.ResponseWriter, r *http.Request, keys ...string) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if !decode(w, r, &m) {
		return nil, false
	}
	if len(m) != len(keys) {
		sourceError(w, ErrInvalid)
		return nil, false
	}
	for _, k := range keys {
		if m[k] == nil {
			sourceError(w, ErrInvalid)
			return nil, false
		}
	}
	return m, true
}
func runtimeInt(raw json.RawMessage, nullable bool) (*int, bool) {
	if string(raw) == "null" {
		return nil, nullable
	}
	var n int
	if json.Unmarshal(raw, &n) != nil || n < 0 || n > 100 {
		return nil, false
	}
	return &n, true
}
func (s *Server) deviceRuntime(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	if len(parts) != 3 || !device.MatchString(parts[2]) || r.URL.RawQuery != "" {
		sourceError(w, ErrInvalid)
		return
	}
	if r.Method != "GET" && r.Method != "PUT" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	id := parts[2]
	c, e := s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	binding, can, e := runtimePermission(c, a.ID, id, s.now())
	if e != nil {
		sourceError(w, e)
		return
	}
	var field, boot string
	var value *int
	if r.Method == "PUT" {
		if !can {
			sourceError(w, ErrForbidden)
			return
		}
		m, ok := runtimeFields(w, r, "field", "value", "boot")
		if !ok {
			return
		}
		value, ok = runtimeInt(m["value"], false)
		if !ok || json.Unmarshal(m["field"], &field) != nil || (field != "volume" && field != "brightness") || json.Unmarshal(m["boot"], &boot) != nil || !runtimeBoot.MatchString(boot) {
			sourceError(w, ErrInvalid)
			return
		}
	}
	// Refresh after decoding an untrusted body, before taking runtimeMu. Config
	// and session locks are never acquired while holding the runtime lock.
	c, e = s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	fresh := r.WithContext(context.WithValue(r.Context(), configContextKey{}, c))
	actor, valid := s.account(fresh, "family")
	if !valid || actor.ID != a.ID {
		sourceError(w, ErrForbidden)
		return
	}
	binding, can, e = runtimePermission(c, a.ID, id, s.now())
	if e != nil {
		sourceError(w, e)
		return
	}
	if r.Method == "PUT" && !can {
		sourceError(w, ErrForbidden)
		return
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	v := s.runtimes[id]
	out := s.runtimeView(id, v, can)
	if r.Method == "PUT" {
		if v == nil || out.Connection != "online" || v.boot != boot || runtimePending(v.command) || (field == "volume" && v.volume == nil) || (field == "brightness" && v.brightness == nil) {
			apiError(w, 409, "runtime_conflict", "设备状态已变化，请刷新后重试")
			return
		}
		suffix, e := randomHex(16)
		if e != nil {
			sourceError(w, e)
			return
		}
		if e = s.auditAction(a.ID, "family", "device_control_requested", id, ""); e != nil {
			sourceError(w, e)
			return
		}
		v.command = &runtimeCommand{ID: "cmd_" + suffix, Field: field, Value: *value, Status: "queued", ExpiresAt: s.now().Add(45 * time.Second), actor: a.ID, binding: binding, boot: boot}
		out = s.runtimeView(id, v, can)
	}
	writeJSON(w, out)
}
func (s *Server) deviceState(w http.ResponseWriter, r *http.Request, c Config) {
	if r.Method != "POST" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	if r.URL.RawQuery != "" {
		sourceError(w, ErrInvalid)
		return
	}
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	d, ok := authenticateFactory(c, r.Header.Get("Device-Id"), key)
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !ok || !activated(c, d, s.now()) {
		onboardingError(w, errAuthentication)
		return
	}
	var m map[string]json.RawMessage
	if !decode(w, r, &m) {
		return
	}
	if len(m) != 4 && len(m) != 5 {
		sourceError(w, ErrInvalid)
		return
	}
	for k := range m {
		if k != "boot" && k != "seq" && k != "volume" && k != "brightness" && k != "ack" {
			sourceError(w, ErrInvalid)
			return
		}
	}
	var boot string
	var seq uint64
	volume, vok := runtimeInt(m["volume"], true)
	brightness, bok := runtimeInt(m["brightness"], true)
	if !vok || !bok || json.Unmarshal(m["boot"], &boot) != nil || !runtimeBoot.MatchString(boot) || json.Unmarshal(m["seq"], &seq) != nil || seq == 0 || seq > 9007199254740991 {
		sourceError(w, ErrInvalid)
		return
	}
	var ack struct {
		ID, Status string
		Actual     *int
	}
	hasAck := m["ack"] != nil
	if hasAck {
		var a map[string]json.RawMessage
		if json.Unmarshal(m["ack"], &a) != nil || len(a) != 3 || json.Unmarshal(a["id"], &ack.ID) != nil || !runtimeCommandID.MatchString(ack.ID) || json.Unmarshal(a["status"], &ack.Status) != nil || (ack.Status != "applied" && ack.Status != "failed") {
			sourceError(w, ErrInvalid)
			return
		}
		var valid bool
		ack.Actual, valid = runtimeInt(a["actual"], true)
		if !valid {
			sourceError(w, ErrInvalid)
			return
		}
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	// A state request can wait behind command creation/audit. Read authority
	// after that wait, before updating the heartbeat or dispatching a command.
	// snapshot only reads the atomic config file; it takes no session/config
	// writer mutex, and runtimePermission is pure, so this adds no lock edge.
	c, err := s.snapshot()
	if err != nil {
		sourceError(w, err)
		return
	}
	d, ok = authenticateFactory(c, r.Header.Get("Device-Id"), key)
	if !ok || !activated(c, d, s.now()) {
		onboardingError(w, errAuthentication)
		return
	}
	if s.runtimes == nil {
		s.runtimes = map[string]*runtimeState{}
	}
	v := s.runtimes[d.DeviceID]
	if v == nil {
		if len(s.runtimes) >= 1024 {
			apiError(w, 503, "runtime_capacity", "设备状态暂不可用")
			return
		}
		v = &runtimeState{}
		s.runtimes[d.DeviceID] = v
	}
	if v.boot == boot && seq <= v.seq {
		apiError(w, 409, "stale_state", "设备状态已过期")
		return
	}
	for _, old := range v.retired {
		if old == boot {
			apiError(w, 409, "stale_state", "设备状态已过期")
			return
		}
	}
	runtimeExpire(v, s.now())
	if v.boot != "" && v.boot != boot {
		v.retired = append(v.retired, v.boot)
		if len(v.retired) > 4 {
			v.retired = v.retired[len(v.retired)-4:]
		}
		if runtimePending(v.command) {
			v.command.Status = "unknown"
		}
	}
	v.boot = boot
	v.seq = seq
	v.seen = s.now()
	v.volume = volume
	v.brightness = brightness
	cmd := v.command
	if runtimePending(cmd) {
		b, allowed, e := runtimePermission(c, cmd.actor, d.DeviceID, s.now())
		if e != nil || !allowed || b.ID != cmd.binding.ID || b.Version != cmd.binding.Version || !b.From.Equal(cmd.binding.From) || b.FamilyID != cmd.binding.FamilyID {
			cmd.Status = "unknown"
		}
	}
	if hasAck && runtimePending(cmd) && cmd.ID == ack.ID && cmd.boot == boot {
		cmd.Actual = ack.Actual
		actual := volume
		if cmd.Field == "brightness" {
			actual = brightness
		}
		cmd.Status = "failed"
		if ack.Status == "applied" && ack.Actual != nil && actual != nil && *actual == cmd.Value && *ack.Actual == cmd.Value {
			cmd.Status = "confirmed"
		}
	}
	var command any
	if runtimePending(cmd) {
		actual := volume
		if cmd.Field == "brightness" {
			actual = brightness
		}
		if actual == nil {
			cmd.Status = "unknown"
		} else {
			cmd.Status = "sent"
			command = map[string]any{"id": cmd.ID, "field": cmd.Field, "value": cmd.Value}
		}
	}
	writeJSON(w, map[string]any{"command": command})
}
