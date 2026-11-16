package realtimelive

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// Management owns authorization and configuration; voice owns connection leases.
type VoiceRouteClaim struct {
	DeviceID       string    `json:"deviceId"`
	ActorID        string    `json:"actorId"`
	FamilyID       string    `json:"familyId"`
	BindingID      string    `json:"bindingId"`
	BindingVersion int       `json:"bindingVersion"`
	Deadline       time.Time `json:"deadline"`
	Route          string    `json:"route"`
	Version        string    `json:"version"`
}
type VoiceRouteState struct {
	DeviceID    string  `json:"deviceId"`
	ActiveRoute *string `json:"activeRoute"`
	Connection  string  `json:"connection"`
}
type VoiceRouteGuard func(context.Context, VoiceRouteClaim, bool, func() error) error

var ErrVoiceRouteForbidden = errors.New("voice_route_forbidden")
var errVoiceRouteUnavailable = errors.New("voice_route_unavailable")

func (c VoiceRouteClaim) valid() bool {
	_, err := hex.DecodeString(c.Version)
	return memoryControlDevice.MatchString(c.DeviceID) && memoryControlIdent.MatchString(c.ActorID) && memoryControlIdent.MatchString(c.FamilyID) && memoryControlIdent.MatchString(c.BindingID) && c.BindingVersion > 0 && time.Now().Before(c.Deadline) && (c.Route == "A" || c.Route == "B" || c.Route == "C") && len(c.Version) == 64 && err == nil
}

// Caller holds Server.mu. A reserved/initializing connection has no actual route yet.
func (s *Server) voiceRouteStateLocked(device string) VoiceRouteState {
	state := VoiceRouteState{DeviceID: device, Connection: "idle"}
	if lease := s.leases[device]; lease != nil {
		state.Connection = "connecting"
		if lease.route != "" {
			route := lease.route
			state.ActiveRoute = &route
			state.Connection = "active"
		}
	}
	return state
}
func (s *Server) voiceRouteStatus(ctx context.Context, claim VoiceRouteClaim, guard VoiceRouteGuard) (state VoiceRouteState, err error) {
	if !claim.valid() || guard == nil {
		return state, ErrVoiceRouteForbidden
	}
	if err = ctx.Err(); err != nil {
		return
	}
	err = guard(ctx, claim, false, func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if !claim.valid() {
			return ErrVoiceRouteForbidden
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stopping {
			return errVoiceRouteUnavailable
		}
		state = s.voiceRouteStateLocked(claim.DeviceID)
		return nil
	})
	return
}
func (s *Server) disconnectVoiceRoute(ctx context.Context, claim VoiceRouteClaim, guard VoiceRouteGuard) (state VoiceRouteState, err error) {
	if !claim.valid() || guard == nil {
		return state, ErrVoiceRouteForbidden
	}
	if err = ctx.Err(); err != nil {
		return
	}
	var lease *sessionLease
	// Lock order is configuration guard -> Server.mu. Never wait on network or a
	// preparing connection while either lock is held (the resolver needs config).
	err = guard(ctx, claim, true, func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if !claim.valid() {
			return ErrVoiceRouteForbidden
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stopping || s.routeDisconnecting[claim.DeviceID] {
			return errVoiceRouteUnavailable
		}
		if s.routeDisconnecting == nil {
			s.routeDisconnecting = map[string]bool{}
		}
		s.routeDisconnecting[claim.DeviceID] = true
		lease = s.leases[claim.DeviceID]
		if lease != nil {
			lease.revoked = true
		}
		return nil
	})
	if err != nil {
		return
	}
	defer func() { s.mu.Lock(); delete(s.routeDisconnecting, claim.DeviceID); s.mu.Unlock() }()
	if lease != nil {
		// Attach and revoke are serialized: after revocation no late upgrader can
		// attach. Snapshot under mu; Close and completion wait remain outside it.
		s.mu.Lock()
		conn := lease.conn
		cancel := lease.cancel
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if conn != nil {
			_ = conn.Close()
		}
		select {
		case <-lease.done:
		case <-ctx.Done():
			return state, ctx.Err()
		}
	}
	s.mu.Lock()
	state = s.voiceRouteStateLocked(claim.DeviceID)
	s.mu.Unlock()
	return
}

// VoiceRouteControlHandler is composed only on the existing private Unix listener.
func (s *Server) VoiceRouteControlHandler(guard VoiceRouteGuard) http.Handler {
	mux := http.NewServeMux()
	for _, path := range []string{"/voice-route/status", "/voice-route/disconnect"} {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			var claim VoiceRouteClaim
			raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if e != nil || !strictVoiceRouteClaim(raw, &claim) || !claim.valid() {
				respond(w, 400, map[string]string{"code": "invalid_request"})
				return
			}
			ctx, cancel := context.WithDeadline(r.Context(), claim.Deadline)
			defer cancel()
			var state VoiceRouteState
			if r.URL.Path == "/voice-route/status" {
				state, e = s.voiceRouteStatus(ctx, claim, guard)
			} else {
				state, e = s.disconnectVoiceRoute(ctx, claim, guard)
			}
			switch {
			case e == nil:
				respond(w, 200, state)
			case errors.Is(e, ErrVoiceRouteForbidden):
				respond(w, 403, map[string]string{"code": "forbidden"})
			default:
				respond(w, 503, map[string]string{"code": "voice_route_unconfirmed"})
			}
		})
	}
	return mux
}
func strictVoiceRouteClaim(raw []byte, c *VoiceRouteClaim) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return false
	}
	allowed := map[string]bool{"deviceId": true, "actorId": true, "familyId": true, "bindingId": true, "bindingVersion": true, "deadline": true, "route": true, "version": true}
	seen := map[string]bool{}
	for d.More() {
		tok, e = d.Token()
		key, ok := tok.(string)
		if e != nil || !ok || seen[key] || !allowed[key] {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return false
		}
	}
	if _, e = d.Token(); e != nil || len(seen) != len(allowed) {
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return false
	}
	return json.Unmarshal(raw, c) == nil
}
