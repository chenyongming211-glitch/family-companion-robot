package realtimelive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectedDeviceReceiptKeepsPlaybackEvidence(t *testing.T) {
	server, _ := testSocket(t)
	dir := t.TempDir()
	s := NewSession(server, Config{}, dir, nil)
	w := &xiaozhiWire{ws: server, scope: "scope", blocks: map[int64]*devicePlayback{1: {sent: 1440}}}
	s.wire = w
	rt := s.addTurn()
	rt.turn.Sent = 1440
	e := s.receiveReceipt(map[string]any{"id": int64(1), "played_samples": json.Number("0"), "state": "progress", "valid": false, "basis": "i2s_dma", "interrupted": true})
	if e == nil || rt.turn.Completed != 0 {
		t.Fatal("invalid evidence was accepted", e)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if !strings.Contains(string(b), `"event":"playback_rejected"`) || !strings.Contains(string(b), `"valid":false`) || !strings.Contains(string(b), `"sent":1440`) {
		t.Fatal("rejection lost the evidence needed to diagnose silent disconnect:", string(b))
	}
}
