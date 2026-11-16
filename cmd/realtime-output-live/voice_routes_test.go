package main

import (
	"familyrobot/internal/realtimelive"
	"net/http/httptest"
	"testing"
)

func TestVoiceRouteCommandConfigurationAndPrivateDispatch(t *testing.T) {
	cfg := realtimelive.Config{}
	if e := configureVoiceRoutes(&cfg, ""); e != nil || cfg.VoiceRouteForDevice != nil {
		t.Fatal("legacy config", e)
	}
	if e := configureVoiceRoutes(&cfg, "/not/a/config"); e == nil {
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
