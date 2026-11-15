package realtimelive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeviceRPCObservationRecordsOwnedMetadataWithoutPayload(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	s.rec.owner = Ownership{DeviceID: "device-observation", SessionID: "session-observation", ClientType: "xiaozhi", ClientVersion: "test"}
	w := newXiaozhiWire(server, 1, 60, "scope")
	w.mcp = true
	s.wire = w
	s.enableDeviceControls()
	s.startDeviceControls()
	t.Cleanup(func() { s.cancel(); s.wg.Wait() })
	discoverControls(t, s, client)
	result := make(chan string, 1)
	go func() {
		v, err := s.controls.Call(context.Background(), 1, "set", "device_set_volume", `{"volume":80}`)
		if err != nil {
			result <- err.Error()
			return
		}
		result <- v
	}()
	setter := nextMCP(t, client)
	answerMCP(t, s, setter, deviceContent("true"))
	readback := nextMCP(t, client)
	select {
	case v := <-result:
		t.Fatalf("setter ACK completed before readback: %s", v)
	default:
	}
	answerMCP(t, s, readback, deviceContent(`{"audio_speaker":{"volume":80},"screen":{"brightness":50},"private":"never-record-rpc-payload"}`))
	if v := <-result; !verifiedControlOutput(v) {
		t.Fatal(v)
	}
	s.cancel()
	s.wg.Wait()
	path := filepath.Join(s.directory, "events.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "never-record-rpc-payload") {
		t.Fatal("raw device response leaked")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("RPC events must remain private", info, err)
	}
	allowed := map[string]bool{"at": true, "event": true, "device_id": true, "session_id": true, "client_type": true, "client_version": true, "rpc_id": true, "expected_rpc_id": true, "phase": true, "reason": true}
	var phases []string
	var dispatchIDs []any
	results := 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "device_tool_dispatch" {
			dispatchIDs = append(dispatchIDs, event["rpc_id"])
		}
		if event["event"] == "device_tool_result" {
			results++
			if event["outcome"] != "verified" {
				t.Fatal(event)
			}
		}
		if event["event"] != "device_tool_reply" {
			continue
		}
		if event["device_id"] != s.rec.owner.DeviceID || event["session_id"] != s.rec.owner.SessionID || event["client_type"] != "xiaozhi" || event["client_version"] != "test" {
			t.Fatal("RPC reply lost ownership", event)
		}
		for key := range event {
			if !allowed[key] {
				t.Fatal("unapproved RPC metadata field", key)
			}
		}
		if _, ok := event["rpc_id"].(float64); !ok || event["rpc_id"] != event["expected_rpc_id"] || event["reason"] != "matched" {
			t.Fatal("bad RPC correlation", event)
		}
		phases = append(phases, event["phase"].(string))
	}
	if strings.Join(phases, ",") != "initialize,tools_list,setter,readback" || results != 1 {
		t.Fatal("RPC metadata/result flow changed", phases, results)
	}
	if len(dispatchIDs) != 2 || dispatchIDs[0] != setter["id"] || dispatchIDs[1] != readback["id"] {
		t.Fatal("outbound RPC ids missing or extra dispatch", dispatchIDs)
	}
}
