//go:build linux

package speakerlive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
)

// protocolTablePath is shared with tools/speaker-lab so the helper's emitted
// pairs and this package's accepted pairs cannot drift apart silently.
const protocolTablePath = "../../testdata/speaker_observation_protocol.json"

type protocolPair struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

func loadProtocolTable(t *testing.T) []protocolPair {
	t.Helper()
	raw, err := os.ReadFile(protocolTablePath)
	if err != nil {
		t.Fatalf("shared protocol table: %v", err)
	}
	var doc struct {
		Comment      string         `json:"comment"`
		Observations []protocolPair `json:"observations"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("shared protocol table: %v", err)
	}
	if len(doc.Observations) == 0 {
		t.Fatal("shared protocol table is empty")
	}
	seen := map[protocolPair]bool{}
	for _, pair := range doc.Observations {
		if seen[pair] {
			t.Fatalf("duplicate pair in shared protocol table: %v", pair)
		}
		seen[pair] = true
	}
	return doc.Observations
}

func observationLine(label, reason string) string {
	return fmt.Sprintf(`{"event":"observation","id":1,"label":%q,"duration_ms":1500,"evidence":"turn_taking_test","reason":%q}`, label, reason)
}

func TestReasonProtocolAcceptsLegacyAndDiagnostic(t *testing.T) {
	base := `{"event":"observation","id":1,"label":"guest_1","duration_ms":1500,"evidence":"turn_taking_test"}`
	if _, err := decodeEvent([]byte(base)); err != nil {
		t.Fatal("legacy output rejected", err)
	}
	for _, pair := range loadProtocolTable(t) {
		event, err := decodeEvent([]byte(observationLine(pair.Label, pair.Reason)))
		if err != nil {
			t.Fatal(pair, err)
		}
		if event.Reason != pair.Reason {
			t.Fatal("sanctioned reason was dropped", pair, event.Reason)
		}
	}
}

// The helper and this observer are separate programs. A readable token this
// build does not sanction must cost the clip its reason, never the session its
// voice filtering: the P0 on 2026-11-26 was exactly this path.
func TestReasonProtocolDropsUnsanctionedButReadableReason(t *testing.T) {
	for _, pair := range []protocolPair{
		{"guest_1", "match_uncertain"}, // sanctioned token, wrong label
		{"guest_1", "worker_unavailable"},
		{"uncertain", "candidate"},
		{"unknown", "low_energy"},
		{"uncertain", "future_reason"},
	} {
		event, err := decodeEvent([]byte(observationLine(pair.Label, pair.Reason)))
		if err != nil {
			t.Fatal("unsanctioned reason ended the observer", pair, err)
		}
		if event.Label != pair.Label || event.Reason != "" {
			t.Fatal("label lost or reason leaked", pair, event)
		}
	}
}

func TestReasonProtocolRejectsUnreadableReason(t *testing.T) {
	for _, raw := range []string{`null`, `10`, `"private user text"`, `""`, `"LOW_ENERGY"`, `"low-energy"`, `"low_energy","reason":"low_energy"`} {
		line := `{"event":"observation","id":1,"label":"uncertain","duration_ms":1500,"evidence":"turn_taking_test","reason":` + raw + `}`
		if _, err := decodeEvent([]byte(line)); err == nil {
			t.Fatal("accepted unreadable reason", raw)
		}
	}
	if _, err := decodeEvent([]byte(`{"event":"ready","evidence":"turn_taking_test","reason":"candidate"}`)); err == nil {
		t.Fatal("ready carried observation metadata")
	}
}

// validHelperReason must accept exactly the shared table: no missing pair and
// no extra pair, so neither side can add a token alone. The probe alphabet is
// the table's own tokens plus a token no build knows; a permissive extra token
// in the whitelist would only be caught if it were in that alphabet, but that
// direction is harmless (it accepts more, never less).
func TestReasonProtocolMatchesSharedTable(t *testing.T) {
	pairs := loadProtocolTable(t)
	sanctioned := map[protocolPair]bool{}
	labels := []string{"uncertain", "unknown"}
	tokens := map[string]bool{"future_reason": true}
	for _, pair := range pairs {
		sanctioned[pair] = true
		tokens[pair.Reason] = true
	}
	for i := 1; i <= 8; i++ {
		labels = append(labels, fmt.Sprintf("guest_%d", i))
	}
	for _, label := range labels {
		for token := range tokens {
			if got, want := validHelperReason(label, token), sanctioned[protocolPair{label, token}]; got != want {
				t.Fatalf("validHelperReason(%q,%q)=%t want %t (shared table)", label, token, got, want)
			}
		}
	}
}

// A drifting helper must not flood the service log: one line per distinct
// (label, token) is enough to see it happened.
func TestUnsupportedReasonLogsOncePerPair(t *testing.T) {
	unsupportedReasons.Delete("guest_8|never_seen_token")
	unsupportedReasons.Delete("guest_7|never_seen_token")
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	for i := 0; i < 3; i++ {
		if _, err := decodeEvent([]byte(observationLine("guest_8", "never_seen_token"))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := decodeEvent([]byte(observationLine("guest_7", "never_seen_token"))); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(buf.String(), "dropping unsupported reason"); got != 2 {
		t.Fatalf("log lines=%d want 2: %s", got, buf.String())
	}
}

func TestObserverCarriesReasonAndDuration(t *testing.T) {
	ch := make(chan Result, 1)
	observer := observerForTest(t, fakeConfig(t, "reason"), func(r Result) { ch <- r }, shortLimits())
	if !observer.Offer(testJob(1)) {
		t.Fatal("job rejected")
	}
	got := receive(t, ch)
	if got.State != "uncertain" || got.Reason != "match_uncertain" || got.AudioMS != 1500 {
		t.Fatal("diagnostic metadata lost", got)
	}
}
