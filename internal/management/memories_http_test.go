package management

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestMemoryHTTPScopesQueriesAndAudit(t *testing.T) {
	s, _ := httpFixture(t)
	r, p, _ := memoryFixture(t)
	s.reader.Root = r.Root
	d := r.Config.Devices[0].ID
	other := r.Config.Devices[1].ID
	for _, scope := range []string{"family", "admin"} {
		cookie := login(t, s, scope, "alice")
		base := "/api/" + scope + "/memories"
		for _, tc := range []struct {
			path   string
			status int
		}{
			{base, 400}, {base + "?device=", 400}, {base + "?device=x", 400}, {base + "?device=" + d + "&device=" + d, 400}, {base + "?device=" + d + "&limit=1", 400},
			{base + "?device=" + other, 403}, {base + "?device=" + d, 200}, {base + "/mem-0123456789abcdef0123456789abcdef-1?device=" + d, 200}, {base + "/mem-0123456789abcdef0123456789abcdef-2?device=" + d, 404}, {base + "/bad?device=" + d, 400},
		} {
			w := request(s, "GET", tc.path, "", cookie)
			if w.Code != tc.status {
				t.Fatalf("%s %d %s", tc.path, w.Code, w.Body)
			}
		}
	}
	for _, user := range []string{"bob", "metadata", "observer"} {
		c := login(t, s, "family", user)
		if w := request(s, "GET", "/api/family/memories?device="+d, "", c); w.Code != 403 {
			t.Fatalf("%s %d", user, w.Code)
		}
	}
	audit, e := os.ReadFile(s.opts.AuditPath)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(audit), "read_memories") || !strings.Contains(string(audit), "read_memory") || strings.Contains(string(audit), "合成") {
		t.Fatal("audit fields")
	}
	c := login(t, s, "family", "alice")
	s.auditWrite = func(string, auditEntry) error { return errors.New("synthetic failure") }
	for _, suffix := range []string{"", "/mem-0123456789abcdef0123456789abcdef-1"} {
		w := request(s, "GET", "/api/family/memories"+suffix+"?device="+d, "", c)
		if w.Code != 503 || strings.Contains(w.Body.String(), "合成") {
			t.Fatal("audit leaked", w.Code)
		}
	}
	if e := os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if w := request(s, "GET", "/api/family/memories?device="+d, "", c); w.Code != 503 {
		t.Fatal("empty skips audit", w.Code)
	}
}

func TestMemoryHTTPRequiredTurnStrings(t *testing.T) {
	s, _ := httpFixture(t)
	cookie := login(t, s, "family", "alice")
	for _, field := range []string{"user", "assistant_spoken", "text_source", "playback_boundary"} {
		for _, value := range []string{"missing", "null"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				r, p, snapshot := memoryFixture(t)
				s.reader.Root = r.Root
				turn := snapshot["data"].(map[string]any)["turns"].([]any)[0].(map[string]any)
				if value == "missing" {
					delete(turn, field)
				} else {
					turn[field] = nil
				}
				writeMemoryFixture(t, p, snapshot)
				for _, suffix := range []string{"", "/mem-0123456789abcdef0123456789abcdef-1"} {
					w := request(s, "GET", "/api/family/memories"+suffix+"?device="+r.Config.Devices[0].ID, "", cookie)
					if w.Code != 503 || strings.Contains(w.Body.String(), "合成") || strings.Contains(w.Body.String(), "source-a") || strings.Contains(w.Body.String(), "output_asr") {
						t.Fatalf("damaged snapshot response: status=%d body=%s", w.Code, w.Body)
					}
				}
			})
		}
	}
}
