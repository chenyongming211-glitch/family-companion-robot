package devicetools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func wantRPCObservation(t *testing.T, u Update, id, expected uint64, phase, reason string) {
	t.Helper()
	want := &RPCObservation{id, expected, phase, reason}
	if !reflect.DeepEqual(u.Observation, want) {
		t.Fatalf("RPC observation = %+v, want %+v", u.Observation, want)
	}
}

func TestRPCObservationTracksDiscoveryAndControlPhases(t *testing.T) {
	b := New(epoch)
	initialize := request(t, b.Start(epoch), "initialize")
	u := reply(b, initialize.ID, map[string]any{}, epoch)
	wantRPCObservation(t, u, initialize.ID, initialize.ID, "initialize", "matched")
	list := request(t, u, "tools/list")
	u = reply(b, list.ID, page([]string{statusName, volumeName}, nil), epoch)
	wantRPCObservation(t, u, list.ID, list.ID, "tools_list", "matched")
	if len(u.Requests)+len(u.Results) != 0 || len(b.Tools()) != 2 {
		t.Fatalf("discovery flow changed: %+v", u)
	}

	setter := request(t, b.Call("set", "device_set_volume", `{"volume":80}`, 1, epoch), "tools/call")
	u = reply(b, setter.ID, ack("true"), epoch)
	wantRPCObservation(t, u, setter.ID, setter.ID, "setter", "matched")
	if len(u.Results) != 0 {
		t.Fatal("setter ACK became verified without readback")
	}
	readback := request(t, u, "tools/call")
	if readback.Params["name"] != statusName {
		t.Fatal("missing readback")
	}
	u = reply(b, readback.ID, status(80, 50), epoch)
	wantRPCObservation(t, u, readback.ID, readback.ID, "readback", "matched")
	if m := output(t, u, "set", 1); m["verified"] != true || len(u.Requests) != 0 {
		t.Fatalf("readback flow changed: %+v", u)
	}
	query := request(t, b.Call("query", "device_get_status", `{}`, 2, epoch), "tools/call")
	u = reply(b, query.ID, status(80, 50), epoch)
	wantRPCObservation(t, u, query.ID, query.ID, "status", "matched")
	if m := output(t, u, "query", 2); m["verified"] != true {
		t.Fatal(m)
	}
}

func TestRPCObservationInvalidAndUnrelatedRepliesKeepPendingCall(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("set", "device_set_volume", `{"volume":80}`, 1, epoch), "tools/call")
	for _, tc := range []struct {
		payload string
		id      uint64
		reason  string
	}{
		{`{`, 0, "invalid_envelope"},
		{`{"jsonrpc":"2.0","id":"private-rpc-id","result":{}}`, 0, "invalid_envelope"},
		{`{"jsonrpc":"2.0","id":0,"result":{}}`, 0, "invalid_envelope"},
		{fmt.Sprintf(`{"jsonrpc":"1.0","id":%d,"result":{}}`, r.ID), r.ID, "invalid_envelope"},
		{`{"jsonrpc":"2.0","id":999,"result":{"private":"never-record"}}`, 999, "ignored_id"},
	} {
		u := b.Receive(json.RawMessage(tc.payload), epoch)
		wantRPCObservation(t, u, tc.id, r.ID, "setter", tc.reason)
		if len(u.Requests)+len(u.Results) != 0 {
			t.Fatalf("invalid/unrelated reply changed call: %+v", u)
		}
	}
	u := reply(b, r.ID, ack("true"), epoch)
	wantRPCObservation(t, u, r.ID, r.ID, "setter", "matched")
	request(t, u, "tools/call")
}

func TestRPCObservationMatchedDoesNotMeanVerified(t *testing.T) {
	b := ready(t)
	r := request(t, b.Call("set", "device_set_volume", `{"volume":80}`, 1, epoch), "tools/call")
	u := reply(b, r.ID, ack("false"), epoch)
	wantRPCObservation(t, u, r.ID, r.ID, "setter", "matched")
	wantError(t, u, "set", "device_error", 1)
	if len(u.Requests) != 0 {
		t.Fatal("failed setter caused readback or retry")
	}
}

func TestRPCObservationUsesStateAfterExpiry(t *testing.T) {
	b := ready(t)
	old := request(t, b.Call("old", "device_set_volume", `{"volume":80}`, 1, epoch), "tools/call")
	b.Call("next", "device_get_status", `{}`, 2, epoch.Add(time.Second))
	u := reply(b, old.ID, ack("true"), epoch.Add(5*time.Second))
	wantError(t, u, "old", "timeout", 1)
	next := request(t, u, "tools/call")
	wantRPCObservation(t, u, old.ID, next.ID, "status", "ignored_id")
	u = reply(b, next.ID, status(80, 50), epoch.Add(5*time.Second))
	wantRPCObservation(t, u, next.ID, next.ID, "status", "matched")
	if m := output(t, u, "next", 2); m["verified"] != true {
		t.Fatal(m)
	}
}

func TestRPCObservationCanceledAndIdleRepliesStayIgnored(t *testing.T) {
	b := ready(t)
	old := request(t, b.Call("old", "device_set_volume", `{"volume":80}`, 1, epoch), "tools/call")
	b.Call("next", "device_get_status", `{}`, 2, epoch)
	u := b.CancelGeneration(1, epoch)
	wantError(t, u, "old", "canceled", 1)
	next := request(t, u, "tools/call")
	u = reply(b, old.ID, ack("true"), epoch)
	wantRPCObservation(t, u, old.ID, next.ID, "status", "ignored_id")
	if len(u.Requests)+len(u.Results) != 0 {
		t.Fatal("canceled ACK changed call")
	}
	u = reply(b, next.ID, status(80, 50), epoch)
	output(t, u, "next", 2)
	u = reply(b, old.ID, ack("true"), epoch)
	wantRPCObservation(t, u, old.ID, 0, "none", "ignored_id")
	if len(u.Requests)+len(u.Results) != 0 {
		t.Fatal("idle late ACK changed flow")
	}
}
