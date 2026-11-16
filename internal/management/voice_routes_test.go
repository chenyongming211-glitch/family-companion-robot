package management

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVoiceRouteResolverBoundsHeldConfigLock(t *testing.T) {
	_, _, id, path := voiceRouteFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- (ConfigStore{path}).WithSnapshot(context.Background(), func(Config) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-lockDone:
		t.Fatal("hold config lock", err)
	}
	resolved := make(chan struct{})
	var route string
	var err error
	started := time.Now()
	go func() {
		route, err = NewVoiceRouteResolver(path)(id)
		close(resolved)
	}()
	defer func() {
		close(release)
		if err := <-lockDone; err != nil {
			t.Error("release config lock", err)
		}
		<-resolved
	}()
	select {
	case <-resolved:
		if !errors.Is(err, context.DeadlineExceeded) || route != "" {
			t.Fatalf("held lock must fail without route fallback: route=%q err=%v", route, err)
		}
		if elapsed := time.Since(started); elapsed < 1900*time.Millisecond {
			t.Fatal("resolver deadline is shorter than two seconds", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("resolver retained admission beyond its config-lock deadline")
	}
}

func voiceRouteFixture(t *testing.T) (*Server, *http.Cookie, string, string) {
	t.Helper()
	s, _, id, path := settingsFixture(t)
	return s, login(t, s, "admin", "alice"), id, path
}
func voiceRouteRead(t *testing.T, s *Server, cookie *http.Cookie, id string) map[string]any {
	t.Helper()
	w := request(s, "GET", "/api/admin/device-voice-route/"+id, "", cookie)
	if w.Code != 200 {
		t.Fatalf("route GET %d %s", w.Code, w.Body)
	}
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func voiceRouteBody(route string, version any) string {
	b, _ := json.Marshal(map[string]any{"route": route, "version": version})
	return string(b)
}
func TestVoiceRouteDefaultPersistentRoundTrip(t *testing.T) {
	s, cookie, id, path := voiceRouteFixture(t)
	before, _ := LoadConfig(path)
	for _, route := range []string{"A", "B", "C"} {
		view := voiceRouteRead(t, s, cookie, id)
		if view["deviceId"] != id || view["activeRoute"] != nil || view["connection"] != "unknown" || view["canSwitch"] != true {
			t.Fatal(view)
		}
		if route == "A" && view["selectedRoute"] != "C" {
			t.Fatal(view)
		}
		if v, ok := view["version"].(string); !ok || len(v) != 64 {
			t.Fatal(view)
		}
		w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody(route, view["version"]), cookie)
		var saved map[string]any
		json.Unmarshal(w.Body.Bytes(), &saved)
		if w.Code != 200 || saved["selectedRoute"] != route || saved["disconnectConfirmed"] != false || saved["connection"] != "unknown" {
			t.Fatalf("save %d %s", w.Code, w.Body)
		}
		if after := voiceRouteRead(t, s, cookie, id); after["selectedRoute"] != route || after["version"] != saved["version"] {
			t.Fatal(after)
		}
		// Read protected persisted configuration, independently of the server snapshot.
		fresh, e := LoadConfig(path)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(fresh.Devices[0])
		var device map[string]any
		json.Unmarshal(raw, &device)
		if device["voice_route"] != route {
			t.Fatal(device)
		}
	}
	after, _ := LoadConfig(path)
	after.Devices = before.Devices
	left, _ := json.Marshal(before)
	right, _ := json.Marshal(after)
	if string(left) != string(right) {
		t.Fatal("authority or other settings changed")
	}
}
func TestVoiceRouteStrictRequestsAndIsolation(t *testing.T) {
	s, admin, id, path := voiceRouteFixture(t)
	view := voiceRouteRead(t, s, admin, id)
	valid := voiceRouteBody("A", view["version"])
	for _, body := range []string{`{}`, `{"route":null,"version":"x"}`, voiceRouteBody("a", view["version"]), voiceRouteBody("D", view["version"]), strings.Replace(valid, `"route"`, `"Route"`, 1), strings.Replace(valid, `"version"`, `"Version"`, 1), strings.TrimSuffix(valid, "}") + `,"route":"B"}`, strings.TrimSuffix(valid, "}") + `,"scope":"family"}`, voiceRouteBody("A", "x")} {
		if w := request(s, "PUT", "/api/admin/device-voice-route/"+id, body, admin); w.Code != 400 {
			t.Fatalf("invalid %q: %d %s", body, w.Code, w.Body)
		}
	}
	family := login(t, s, "family", "alice")
	if w := request(s, "GET", "/api/admin/device-voice-route/"+id, "", family); w.Code != 401 {
		t.Fatal("family cookie", w.Code)
	}
	if w := request(s, "PUT", "/api/family/device-voice-route/"+id, valid, family); w.Code != 404 {
		t.Fatal("family route", w.Code)
	}
	if w := request(s, "PUT", "/api/admin/device-voice-route/"+s.cfg.Devices[1].ID, valid, admin); w.Code != 403 {
		t.Fatal("cross family", w.Code)
	}
	if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); e != nil {
		t.Fatal(e)
	}
	if view := voiceRouteRead(t, s, admin, id); view["canSwitch"] != false {
		t.Fatal(view)
	}
	if w := request(s, "PUT", "/api/admin/device-voice-route/"+id, valid, admin); w.Code != 403 {
		t.Fatal("read only", w.Code)
	}
}
func TestVoiceRouteVersionConflictAndAuditFailure(t *testing.T) {
	s, admin, id, path := voiceRouteFixture(t)
	v := voiceRouteRead(t, s, admin, id)["version"]
	if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Bindings[0].Version++; return nil }); e != nil {
		t.Fatal(e)
	}
	if w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody("A", v), admin); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	v = voiceRouteRead(t, s, admin, id)["version"]
	s.auditWrite = func(string, auditEntry) error { return ErrSource }
	if w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody("A", v), admin); w.Code != 503 {
		t.Fatal(w.Code, w.Body)
	}
	c, e := LoadConfig(path)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c.Devices[0])
	if strings.Contains(string(raw), `"voice_route"`) {
		t.Fatal("audit failure mutated choice")
	}
}

func TestVoiceRoutePrivateStatusAndDisconnect(t *testing.T) {
	s, admin, id, _ := voiceRouteFixture(t)
	calls := []string{}
	s.opts.MemoryControlSocket = memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		var in map[string]any
		if json.NewDecoder(r.Body).Decode(&in) != nil || in["actorId"] != "alice" || in["route"] == nil || in["version"] == nil || in["scope"] != nil {
			t.Error("claim", in)
		}
		if r.URL.Path == "/voice-route/status" {
			json.NewEncoder(w).Encode(map[string]any{"deviceId": id, "activeRoute": "C", "connection": "active"})
			return
		}
		if r.URL.Path != "/voice-route/disconnect" {
			t.Error(r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"deviceId": id, "activeRoute": nil, "connection": "idle"})
	}))
	view := voiceRouteRead(t, s, admin, id)
	if view["activeRoute"] != "C" || view["connection"] != "active" {
		t.Fatal(view)
	}
	w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody("A", view["version"]), admin)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out["selectedRoute"] != "A" || out["disconnectConfirmed"] != true || out["connection"] != "idle" || out["activeRoute"] != nil {
		t.Fatal(w.Code, out)
	}
	if strings.Join(calls, ",") != "/voice-route/status,/voice-route/disconnect" {
		t.Fatal(calls)
	}
}
func TestVoiceRouteAfterIPCRechecksAuthorityAndVersion(t *testing.T) {
	for _, kind := range []string{"role", "grant", "binding", "route", "logout"} {
		t.Run(kind, func(t *testing.T) {
			s, admin, id, path := voiceRouteFixture(t)
			v := voiceRouteRead(t, s, admin, id)["version"]
			s.opts.MemoryControlSocket = memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "logout" {
					request(s, "POST", "/api/admin/logout", "{}", admin)
				} else {
					if e := (ConfigStore{path}).Transaction(func(c *Config) error {
						switch kind {
						case "role":
							c.Accounts[0].InternalAdmin = false
						case "grant":
							c.Accounts[0].Memberships[0].Devices = nil
						case "binding":
							c.Bindings[0].Version++
						case "route":
							raw, _ := json.Marshal(c)
							var doc map[string]any
							json.Unmarshal(raw, &doc)
							doc["devices"].([]any)[0].(map[string]any)["voice_route"] = "B"
							raw, _ = json.Marshal(doc)
							json.Unmarshal(raw, c)
						}
						return nil
					}); e != nil {
						t.Error(e)
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"deviceId": id, "activeRoute": nil, "connection": "idle"})
			}))
			w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody("A", v), admin)
			want := 403
			if kind == "binding" || kind == "route" {
				want = 409
			}
			if w.Code != want {
				t.Fatal(kind, w.Code, w.Body)
			}
		})
	}
}

func TestVoiceRouteConfigResolverAndValidation(t *testing.T) {
	_, _, id, path := voiceRouteFixture(t)
	resolve := NewVoiceRouteResolver(path)
	if route, e := resolve(id); e != nil || route != "C" {
		t.Fatal(route, e)
	}
	for _, route := range []string{"A", "B", "C"} {
		if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Devices[0].VoiceRoute = route; return nil }); e != nil {
			t.Fatal(e)
		}
		if got, e := resolve(id); e != nil || got != route {
			t.Fatal(got, e)
		}
	}
	if _, e := resolve("dev_" + strings.Repeat("c", 32)); e == nil {
		t.Fatal("missing device defaulted")
	}
	c, _ := LoadConfig(path)
	c.Devices[0].VoiceRoute = "a"
	if ValidateConfig(c) == nil {
		t.Fatal("invalid enum accepted")
	}
	if e := os.WriteFile(path, []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := resolve(id); e == nil {
		t.Fatal("corruption defaulted")
	}
	if _, e := NewVoiceRouteResolver(filepath.Join(t.TempDir(), "missing"))(id); e == nil {
		t.Fatal("unavailable defaulted")
	}
}
func TestVoiceRouteGuardFreshReadWriteAndAnchors(t *testing.T) {
	for _, kind := range []string{"read-only-status", "read-only-write", "route", "version", "binding", "family", "deadline", "role", "grant"} {
		t.Run(kind, func(t *testing.T) {
			s, admin, id, path := voiceRouteFixture(t)
			v := voiceRouteRead(t, s, admin, id)["version"].(string)
			b := s.cfg.Bindings[0]
			claim := realtimelive.VoiceRouteClaim{DeviceID: id, ActorID: "alice", FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(time.Minute), Route: "C", Version: v}
			write := true
			switch kind {
			case "read-only-status", "read-only-write":
				write = kind == "read-only-write"
				if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); e != nil {
					t.Fatal(e)
				}
			case "route":
				claim.Route = "A"
			case "version":
				claim.Version = strings.Repeat("0", 64)
			case "binding":
				claim.BindingVersion++
			case "family":
				claim.FamilyID = "home-b"
			case "deadline":
				claim.Deadline = time.Now().Add(-time.Second)
			case "role", "grant":
				if e := (ConfigStore{path}).Transaction(func(c *Config) error {
					if kind == "role" {
						c.Accounts[0].InternalAdmin = false
					} else {
						c.Accounts[0].Memberships[0].Devices = nil
					}
					return nil
				}); e != nil {
					t.Fatal(e)
				}
			}
			called := false
			e := NewVoiceRouteGuard(path)(context.Background(), claim, write, func() error { called = true; return nil })
			if kind == "read-only-status" {
				if e != nil || !called {
					t.Fatal(e, called)
				}
			} else if e == nil || called {
				t.Fatal("stale guard admitted", e, called)
			}
		})
	}
}
func TestVoiceRouteGuardHoldsConfigSnapshotThroughAction(t *testing.T) {
	s, admin, id, path := voiceRouteFixture(t)
	v := voiceRouteRead(t, s, admin, id)["version"].(string)
	b := s.cfg.Bindings[0]
	claim := realtimelive.VoiceRouteClaim{DeviceID: id, ActorID: "alice", FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(time.Minute), Route: "C", Version: v}
	entered, finish, guardDone, writerDone := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan error, 1)
	go func() {
		guardDone <- NewVoiceRouteGuard(path)(context.Background(), claim, true, func() error { close(entered); <-finish; return nil })
	}()
	<-entered
	go func() {
		writerDone <- (ConfigStore{path}).Transaction(func(c *Config) error { c.Devices[0].VoiceRoute = "A"; return nil })
	}()
	select {
	case e := <-writerDone:
		t.Fatal("writer passed action lock", e)
	case <-time.After(40 * time.Millisecond):
	}
	close(finish)
	if e := <-guardDone; e != nil {
		t.Fatal(e)
	}
	if e := <-writerDone; e != nil {
		t.Fatal(e)
	}
	called := false
	if e := NewVoiceRouteGuard(path)(context.Background(), claim, false, func() error { called = true; return nil }); e == nil || called {
		t.Fatal("stale status allowed")
	}
}

func TestVoiceRouteRejectedWritesNeverCallPrivateControl(t *testing.T) {
	for _, kind := range []string{"readonly", "grant", "role", "binding", "audit", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			s, admin, id, path := voiceRouteFixture(t)
			version := voiceRouteRead(t, s, admin, id)["version"]
			calls := 0
			s.opts.MemoryControlSocket = memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
			if kind == "audit" {
				s.auditWrite = func(string, auditEntry) error { return ErrSource }
			} else if kind != "invalid" {
				if e := (ConfigStore{path}).Transaction(func(c *Config) error {
					switch kind {
					case "readonly":
						c.Accounts[0].Memberships[0].CanAddDevices = false
					case "grant":
						c.Accounts[0].Memberships[0].Devices = nil
					case "role":
						c.Accounts[0].InternalAdmin = false
					case "binding":
						c.Bindings[0].Version++
					}
					return nil
				}); e != nil {
					t.Fatal(e)
				}
			}
			route := "A"
			if kind == "invalid" {
				route = "a"
			}
			w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody(route, version), admin)
			if w.Code < 400 || calls != 0 {
				t.Fatal("rejection side effect", w.Code, calls, w.Body)
			}
		})
	}
}
func TestVoiceRouteVersionIgnoresSecretsAndOtherSettings(t *testing.T) {
	s, admin, id, path := voiceRouteFixture(t)
	v := voiceRouteRead(t, s, admin, id)["version"]
	if e := (ConfigStore{path}).Transaction(func(c *Config) error {
		c.Accounts[1].Password.Digest = strings.Repeat("ab", 32)
		c.Devices[0].Location = "别处"
		c.Devices[0].DisplayName = "伙伴"
		c.Devices[1].Name = "其他设备"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if got := voiceRouteRead(t, s, admin, id)["version"]; got != v {
		t.Fatal("unrelated version", got, v)
	}
}
func TestVoiceRouteMalformedPrivateStateStaysUnknown(t *testing.T) {
	for _, body := range []string{`{"deviceId":"other","activeRoute":null,"connection":"idle"}`, `{"deviceId":"ID","activeRoute":"D","connection":"active"}`, `{"deviceId":"ID","activeRoute":"A","connection":"idle"}`, `{"deviceId":"ID","activeRoute":null,"connection":"active"}`, `{"deviceId":"ID","activeRoute":null,"connection":"unknown"}`, `{"deviceId":"ID","activeRoute":null,"connection":"idle","connection":"idle"}`, `{"deviceId":"ID","activeRoute":null,"connection":"idle"} {}`} {
		t.Run(body, func(t *testing.T) {
			s, admin, id, _ := voiceRouteFixture(t)
			s.opts.MemoryControlSocket = memoryTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.ReplaceAll(body, "ID", id))) }))
			view := voiceRouteRead(t, s, admin, id)
			if view["connection"] != "unknown" || view["activeRoute"] != nil {
				t.Fatal(view)
			}
		})
	}
}

func TestVoiceRouteRealPrivateGuardReadOnlyStatusAndIdleDisconnect(t *testing.T) {
	s, admin, id, path := voiceRouteFixture(t)
	voice := realtimelive.NewServer(realtimelive.Config{MaxSessions: 2}, nil, t.TempDir(), "synthetic-route-test")
	t.Cleanup(func() { voice.CloseActive(context.Background()) })
	s.opts.MemoryControlSocket = memoryTestListener(t, voice.VoiceRouteControlHandler(NewVoiceRouteGuard(path)))
	view := voiceRouteRead(t, s, admin, id)
	if view["connection"] != "idle" || view["activeRoute"] != nil {
		t.Fatal(view)
	}
	w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody("A", view["version"]), admin)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out["disconnectConfirmed"] != true || out["selectedRoute"] != "A" {
		t.Fatal(w.Code, out)
	}
	if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil }); e != nil {
		t.Fatal(e)
	}
	view = voiceRouteRead(t, s, admin, id)
	if view["canSwitch"] != false || view["connection"] != "idle" {
		t.Fatal(view)
	}
}

func TestVoiceRouteAuditUsesExistingSettingsCategory(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "confirmed"}[confirmed], func(t *testing.T) {
			s, admin, id, path := voiceRouteFixture(t)
			if confirmed {
				voice := realtimelive.NewServer(realtimelive.Config{}, nil, t.TempDir(), "audit-route-test")
				t.Cleanup(func() { voice.CloseActive(context.Background()) })
				s.opts.MemoryControlSocket = memoryTestListener(t, voice.VoiceRouteControlHandler(NewVoiceRouteGuard(path)))
			}
			version := voiceRouteRead(t, s, admin, id)["version"]
			if w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody("A", version), admin); w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			raw, e := os.ReadFile(s.opts.AuditPath)
			if e != nil {
				t.Fatal(e)
			}
			entries, pending, e := parseAuditSnapshot(raw)
			if e != nil {
				t.Fatal(e)
			}
			c, e := LoadConfig(path)
			if e != nil {
				t.Fatal(e)
			}
			page := auditProjection(c, "alice", s.now(), entries, AuditFilter{Action: "settings", Limit: 50}, pending, make([]byte, 32))
			status := "unknown"
			result := "旧对话结束未确认"
			if confirmed {
				status = "confirmed"
				result = "旧对话结束已确认"
			}
			if len(page.Items) != 1 || page.Items[0].Status != status || page.Items[0].Description != "切换语音链路" || page.Items[0].Result != result {
				t.Fatal(page)
			}
		})
	}
}

func TestVoiceRouteRevisionPreventsABAAndSameRouteStaleClaims(t *testing.T) {
	s, admin, id, path := voiceRouteFixture(t)
	current := voiceRouteRead(t, s, admin, id)
	save := func(route string) map[string]any {
		t.Helper()
		w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody(route, current["version"]), admin)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		current = out
		return out
	}
	first := save("A")
	b := s.cfg.Bindings[0]
	claim := realtimelive.VoiceRouteClaim{DeviceID: id, ActorID: "alice", FamilyID: b.FamilyID, BindingID: b.ID, BindingVersion: b.Version, Deadline: time.Now().Add(time.Minute), Route: "A", Version: first["version"].(string)}
	save("B")
	save("A")
	called := false
	if e := NewVoiceRouteGuard(path)(context.Background(), claim, true, func() error { called = true; return nil }); e == nil || called {
		t.Fatal("old A claim admitted after A-B-A", e, called)
	}
	previous := current["version"]
	save("A")
	if current["version"] == previous {
		t.Fatal("accepted same-route choice did not advance version")
	}
	c, e := LoadConfig(path)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c.Devices[0])
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if fields["voice_route_revision"] != float64(4) {
		t.Fatal("revision not durable", fields)
	}
}
func TestVoiceRouteRevisionConfigValidation(t *testing.T) {
	c := authorityFixtureConfig()
	raw, _ := json.Marshal(c)
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	path := filepath.Join(t.TempDir(), "revision.json")
	for _, tt := range []struct {
		revision any
		valid    bool
	}{{float64(0), true}, {float64(9007199254740991), true}, {float64(9007199254740992), false}, {float64(-1), false}, {1.5, false}} {
		doc["devices"].([]any)[0].(map[string]any)["voice_route_revision"] = tt.revision
		raw, _ = json.Marshal(doc)
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
		_, e := LoadConfig(path)
		if (e == nil) != tt.valid {
			t.Fatal(tt, e)
		}
	}
}

func TestVoiceRouteRevisionOverflowRejectsWithoutMutation(t *testing.T) {
	s, admin, id, path := voiceRouteFixture(t)
	if e := (ConfigStore{path}).Transaction(func(c *Config) error { c.Devices[0].VoiceRouteRevision = maxVoiceRouteRevision; return nil }); e != nil {
		t.Fatal(e)
	}
	version := voiceRouteRead(t, s, admin, id)["version"]
	w := request(s, "PUT", "/api/admin/device-voice-route/"+id, voiceRouteBody("A", version), admin)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	c, e := LoadConfig(path)
	if e != nil || c.Devices[0].VoiceRouteRevision != maxVoiceRouteRevision || c.Devices[0].VoiceRoute != "" {
		t.Fatal(c.Devices[0], e)
	}
}
