package realtimelive

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMemoryControlOptionalScopeStrictCompatibility(t *testing.T) {
	raw, _ := json.Marshal(clearClaim())
	base := strings.TrimSuffix(string(raw), "}")
	for _, tc := range []struct {
		name, suffix string
		want         bool
	}{
		{"legacy", "}", true}, {"family", `,"scope":"family"}`, true}, {"admin", `,"scope":"admin"}`, true},
		{"empty", `,"scope":""}`, false}, {"unknown", `,"scope":"other"}`, false}, {"null", `,"scope":null}`, false}, {"duplicate", `,"scope":"admin","scope":"family"}`, false}, {"wrong-case", `,"Scope":"admin"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var claim MemoryClearClaim
			got := strictMemoryClaim([]byte(base+tc.suffix), &claim)
			if got != tc.want {
				t.Fatalf("strict=%v want=%v", got, tc.want)
			}
			if got && !claim.valid() {
				t.Fatal("valid claim refused")
			}
		})
	}
	s := memoryServer(t, t.TempDir())
	handler := s.MemoryControlHandler(allowClear)
	for _, suffix := range []string{`,"scope":"admin"}`, `,"scope":"family"}`, `}`} {
		r := httptest.NewRequest("POST", "/memory/clear", strings.NewReader(base+suffix))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("compatible route %d %s", w.Code, w.Body)
		}
	}
}
