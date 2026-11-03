package speakerlive

import (
	"fmt"
	"testing"
)

func TestReasonProtocolAcceptsLegacyAndDiagnostic(t *testing.T) {
	base := `{"event":"observation","id":1,"label":"guest_1","duration_ms":1500,"evidence":"turn_taking_test"}`
	if _, err := decodeEvent([]byte(base)); err != nil {
		t.Fatal("legacy output rejected", err)
	}
	withReason := base[:len(base)-1] + `,"reason":"candidate"}`
	if _, err := decodeEvent([]byte(withReason)); err != nil {
		t.Fatal("diagnostic output rejected", err)
	}
	for _, tc := range []struct{ label, reason string }{
		{"guest_1", "candidate"}, {"uncertain", "too_short"}, {"uncertain", "low_energy"}, {"uncertain", "match_uncertain"}, {"unknown", "capacity"},
	} {
		line := fmt.Sprintf(`{"event":"observation","id":1,"label":%q,"duration_ms":1500,"evidence":"turn_taking_test","reason":%q}`, tc.label, tc.reason)
		if _, err := decodeEvent([]byte(line)); err != nil {
			t.Fatal(tc, err)
		}
	}
}
func TestReasonProtocolRejectsUntrustedAndContradictoryReason(t *testing.T) {
	for _, raw := range []string{`null`, `10`, `"private user text"`, `""`, `"low_energy","reason":"low_energy"`} {
		line := `{"event":"observation","id":1,"label":"uncertain","duration_ms":1500,"evidence":"turn_taking_test","reason":` + raw + `}`
		if _, err := decodeEvent([]byte(line)); err == nil {
			t.Fatal("accepted invalid reason", raw)
		}
	}
	for _, tc := range []struct{ label, reason string }{{"guest_1", "match_uncertain"}, {"uncertain", "candidate"}, {"unknown", "low_energy"}} {
		line := fmt.Sprintf(`{"event":"observation","id":1,"label":%q,"duration_ms":1500,"evidence":"turn_taking_test","reason":%q}`, tc.label, tc.reason)
		if _, err := decodeEvent([]byte(line)); err == nil {
			t.Fatal("accepted contradictory reason", tc)
		}
	}
	if _, err := decodeEvent([]byte(`{"event":"ready","evidence":"turn_taking_test","reason":"candidate"}`)); err == nil {
		t.Fatal("ready carried observation metadata")
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
