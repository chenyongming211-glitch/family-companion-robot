package main

import (
	"errors"
	"familyrobot/internal/realtimelive"
	"net/http/httptest"
	"testing"
)

func TestVoiceRouteCommandConfigurationAndPrivateDispatch(t *testing.T) {
	cfg := realtimelive.Config{}
	if e := configureVoiceRoutes(&cfg, "", ""); e != nil || cfg.VoiceRouteForDevice != nil {
		t.Fatal("legacy config", e)
	}
	if e := configureVoiceRoutes(&cfg, "/not/a/config", ""); e == nil {
		t.Fatal("unavailable config accepted")
	}
	app := realtimelive.NewServer(realtimelive.Config{}, nil, t.TempDir(), "test")
	h := privateControlHandler(app, "/not/a/config")
	for _, path := range []string{"/voice-route/status/", "/voice-route/disconnect/extra", "/other"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/voice-route/status", "/voice-route/disconnect", "/memory/clear"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code == 404 {
			t.Fatal("private route missing", path)
		}
		public := httptest.NewRecorder()
		app.Handler().ServeHTTP(public, httptest.NewRequest("POST", path, nil))
		if public.Code != 404 {
			t.Fatal("private route public", path, public.Code)
		}
	}
}

func TestDevVoiceRouteOverridesOnlyUnknownDevices(t *testing.T) {
	for _, route := range []string{"A", "B", "C"} {
		known := errors.New("voice_route_read_failed")
		resolve, err := withDevVoiceRoute(func(id string) (string, error) {
			switch id {
			case "terminal":
				return "", realtimelive.ErrVoiceRouteUnknown
			case "family":
				return "B", nil
			default:
				return "", known
			}
		}, route)
		if err != nil {
			t.Fatal(route, err)
		}
		if got, err := resolve("terminal"); err != nil || got != route {
			t.Fatalf("%s: unmanaged terminal must reach the override, got %q %v", route, got, err)
		}
		// A provisioned device already chose its own route. The override must not
		// move it: this flag exists for the development terminal, not for families.
		if got, err := resolve("family"); err != nil || got != "B" {
			t.Fatalf("%s: managed device route was overridden, got %q %v", route, got, err)
		}
		// A store that cannot be read is a real fault, not an unknown device.
		if _, err := resolve("broken"); !errors.Is(err, known) {
			t.Fatalf("%s: read failure was hidden, got %v", route, err)
		}
	}
	if _, err := withDevVoiceRoute(nil, "D"); err == nil {
		t.Fatal("invalid route accepted")
	}
	plain := func(string) (string, error) { return "A", nil }
	if got, err := withDevVoiceRoute(plain, ""); err != nil || got == nil {
		t.Fatal("empty override must leave the resolver untouched", err)
	}
}
