package management

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"io"
	"net"
	"net/http"
	"time"
)

// Management owns the persisted choice and authorization; voice owns actual
// connection state. The command composes these callbacks without reverse imports.
type DeviceVoiceRouteDTO struct {
	DeviceID            string  `json:"deviceId"`
	SelectedRoute       string  `json:"selectedRoute"`
	ActiveRoute         *string `json:"activeRoute"`
	Connection          string  `json:"connection"`
	Version             string  `json:"version"`
	CanSwitch           bool    `json:"canSwitch"`
	DisconnectConfirmed *bool   `json:"disconnectConfirmed,omitempty"`
}

const maxVoiceRouteRevision uint64 = 1<<53 - 1

var errVoiceRouteConflict = errors.New("voice route conflict")

func validVoiceRoute(route string) bool { return route == "A" || route == "B" || route == "C" }
func selectedVoiceRoute(c Config, id string) (string, error) {
	for _, d := range c.Devices {
		if d.ID == id {
			if d.VoiceRoute == "" {
				return "C", nil
			}
			if !validVoiceRoute(d.VoiceRoute) {
				return "", ErrSource
			}
			return d.VoiceRoute, nil
		}
	}
	return "", ErrNotFound
}
func voiceRouteView(c Config, actor, id string, now time.Time) (DeviceVoiceRouteDTO, Binding, error) {
	b, e := c.Authorize(actor, "admin", id, now, false)
	if e != nil {
		return DeviceVoiceRouteDTO{}, Binding{}, e
	}
	route, e := selectedVoiceRoute(c, id)
	if e != nil {
		return DeviceVoiceRouteDTO{}, Binding{}, e
	}
	var revision uint64
	for _, d := range c.Devices {
		if d.ID == id {
			revision = d.VoiceRouteRevision
			break
		}
	}
	// The durable public choice revision prevents old claims becoming valid after
	// A->B->A or an explicit repeat. No secret or unrelated config enters the hash.
	raw, _ := json.Marshal(struct {
		DeviceID, Route string
		Revision        uint64
		Binding         Binding
	}{id, route, revision, b})
	h := sha256.Sum256(raw)
	out := DeviceVoiceRouteDTO{DeviceID: id, SelectedRoute: route, Connection: "unknown", Version: hex.EncodeToString(h[:])}
	a, _ := c.AccountByID(actor)
	for _, m := range a.Memberships {
		if m.FamilyID == b.FamilyID && m.CanAddDevices {
			out.CanSwitch = true
		}
	}
	return out, b, nil
}
func NewVoiceRouteResolver(path string) func(string) (string, error) {
	return func(id string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var route string
		e := (ConfigStore{path}).WithSnapshot(ctx, func(c Config) error { var e error; route, e = selectedVoiceRoute(c, id); return e })
		return route, e
	}
}
func NewVoiceRouteGuard(path string) realtimelive.VoiceRouteGuard {
	return func(ctx context.Context, claim realtimelive.VoiceRouteClaim, write bool, fn func() error) error {
		return (ConfigStore{path}).WithSnapshot(ctx, func(c Config) error {
			out, b, e := voiceRouteView(c, claim.ActorID, claim.DeviceID, time.Now())
			if e != nil || (write && !out.CanSwitch) || b.ID != claim.BindingID || b.Version != claim.BindingVersion || b.FamilyID != claim.FamilyID || out.SelectedRoute != claim.Route || out.Version != claim.Version || !time.Now().Before(claim.Deadline) {
				return realtimelive.ErrVoiceRouteForbidden
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			return fn()
		})
	}
}
func voiceRouteError(w http.ResponseWriter, e error) {
	if errors.Is(e, errVoiceRouteConflict) {
		apiError(w, 409, "voice_route_conflict", "链路选择已改变，请刷新后重试")
		return
	}
	sourceError(w, e)
}
func (s *Server) deviceVoiceRoute(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	if parts[0] != "admin" {
		sourceError(w, ErrNotFound)
		return
	}
	if len(parts) != 3 || !device.MatchString(parts[2]) || r.URL.RawQuery != "" {
		sourceError(w, ErrInvalid)
		return
	}
	if r.Method != "GET" && r.Method != "PUT" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	var route, version string
	write := r.Method == "PUT"
	if write {
		var fields map[string]json.RawMessage
		if !decode(w, r, &fields) {
			return
		}
		if len(fields) != 2 || fields["route"] == nil || fields["version"] == nil || json.Unmarshal(fields["route"], &route) != nil || json.Unmarshal(fields["version"], &version) != nil || !validVoiceRoute(route) || !digest.MatchString(version) {
			sourceError(w, ErrInvalid)
			return
		}
	}
	var out DeviceVoiceRouteDTO
	var claim realtimelive.VoiceRouteClaim
	var entry auditEntry
	suffix, e := randomHex(16)
	if e != nil {
		sourceError(w, e)
		return
	}
	prepare := func(c *Config) error {
		fresh := r.WithContext(context.WithValue(r.Context(), configContextKey{}, *c))
		actor, ok := s.account(fresh, "admin")
		if !ok || actor.ID != a.ID {
			return ErrForbidden
		}
		current, b, e := voiceRouteView(*c, actor.ID, parts[2], s.now())
		if e != nil {
			return e
		}
		if write {
			if !current.CanSwitch {
				return ErrForbidden
			}
			if current.Version != version {
				return errVoiceRouteConflict
			}
			for _, d := range c.Devices {
				if d.ID == parts[2] && d.VoiceRouteRevision == maxVoiceRouteRevision {
					return errVoiceRouteConflict
				}
			}
		}
		action := "read_voice_route_requested"
		if write {
			action = "update_voice_route_requested"
		}
		entry = s.operationAudit(actor.ID, "admin", action, parts[2], "op_"+suffix, b, "voice_route", nil)
		if e = s.auditRecord(entry); e != nil {
			return e
		}
		if write {
			for i := range c.Devices {
				if c.Devices[i].ID == parts[2] {
					c.Devices[i].VoiceRoute = route
					c.Devices[i].VoiceRouteRevision++
				}
			}
		}
		out, b, e = voiceRouteView(*c, actor.ID, parts[2], s.now())
		if e != nil {
			return e
		}
		claim = realtimelive.VoiceRouteClaim{DeviceID: parts[2], ActorID: actor.ID, FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(5 * time.Second), Route: out.SelectedRoute, Version: out.Version}
		return nil
	}
	if write {
		e = (ConfigStore{s.opts.ConfigPath}).Transaction(prepare)
	} else {
		e = (ConfigStore{s.opts.ConfigPath}).WithSnapshot(r.Context(), func(c Config) error { return prepare(&c) })
	}
	if e != nil {
		voiceRouteError(w, e)
		return
	}
	// Release the config lock before voice reacquires it. A failed RPC never
	// rewinds a durable choice or pretends the old conversation ended.
	endpoint := "/voice-route/status"
	if write {
		endpoint = "/voice-route/disconnect"
	}
	state, rpcErr := voiceRouteRPC(r.Context(), s.opts.MemoryControlSocket, endpoint, claim)
	confirmed := rpcErr == nil
	if write {
		entry.UTC = s.now().UTC()
		entry.Action = "update_voice_route_result"
		entry.Outcome = "unknown"
		if confirmed {
			entry.Outcome = "confirmed"
		}
		_ = s.auditRecord(entry)
	}
	e = (ConfigStore{s.opts.ConfigPath}).WithSnapshot(r.Context(), func(c Config) error {
		fresh := r.WithContext(context.WithValue(r.Context(), configContextKey{}, c))
		actor, ok := s.account(fresh, "admin")
		if !ok || actor.ID != a.ID {
			return ErrForbidden
		}
		current, b, e := voiceRouteView(c, actor.ID, parts[2], s.now())
		if e != nil {
			return e
		}
		if write && !current.CanSwitch {
			return ErrForbidden
		}
		if current.Version != claim.Version || b.ID != claim.BindingID || b.Version != claim.BindingVersion || b.FamilyID != claim.FamilyID {
			return errVoiceRouteConflict
		}
		out = current
		if confirmed {
			out.ActiveRoute = state.ActiveRoute
			out.Connection = state.Connection
		}
		if write {
			out.DisconnectConfirmed = &confirmed
		}
		return nil
	})
	if e != nil {
		voiceRouteError(w, e)
		return
	}
	writeJSON(w, out)
}
func voiceRouteRPC(ctx context.Context, path, endpoint string, claim realtimelive.VoiceRouteClaim) (realtimelive.VoiceRouteState, error) {
	var state realtimelive.VoiceRouteState
	if realtimelive.ValidateMemoryControlSocket(path) != nil {
		return state, ErrSource
	}
	ctx, cancel := context.WithDeadline(ctx, claim.Deadline)
	defer cancel()
	raw, e := json.Marshal(claim)
	if e != nil {
		return state, ErrSource
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "POST", "http://voice-control"+endpoint, bytes.NewReader(raw))
	if e != nil {
		return state, ErrSource
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return state, ErrSource
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return state, ErrSource
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if e != nil || len(body) > 4096 || uniqueConfigKeys(json.NewDecoder(bytes.NewReader(body)), 0) != nil {
		return state, ErrSource
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 3 || fields["deviceId"] == nil || fields["activeRoute"] == nil || fields["connection"] == nil || json.Unmarshal(body, &state) != nil || state.DeviceID != claim.DeviceID {
		return state, ErrSource
	}
	switch state.Connection {
	case "idle":
		if state.ActiveRoute != nil {
			return state, ErrSource
		}
	case "connecting":
		if state.ActiveRoute != nil && !validVoiceRoute(*state.ActiveRoute) {
			return state, ErrSource
		}
	case "active":
		if state.ActiveRoute == nil || !validVoiceRoute(*state.ActiveRoute) {
			return state, ErrSource
		}
	default:
		return state, ErrSource
	}
	return state, nil
}
