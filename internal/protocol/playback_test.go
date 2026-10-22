package protocol

import (
	"encoding/json"
	"testing"
)

func TestParsePlaybackRequiresExplicitTypedEvidence(t *testing.T) {
	base := map[string]any{"type": "playback", "state": "progress", "session_id": "session", "playback_id": 1, "sample_rate": 24000, "played_samples": 0, "valid": true, "basis": "i2s_dma"}
	for _, field := range []string{"session_id", "playback_id", "sample_rate", "played_samples", "valid", "state", "basis"} {
		t.Run("missing_"+field, func(t *testing.T) {
			p := make(map[string]any)
			for k, v := range base {
				if k != field {
					p[k] = v
				}
			}
			b, _ := json.Marshal(p)
			if _, err := ParsePlayback(b); err == nil {
				t.Fatal("accepted missing evidence")
			}
		})
		t.Run("null_"+field, func(t *testing.T) {
			p := make(map[string]any)
			for k, v := range base {
				p[k] = v
			}
			p[field] = nil
			b, _ := json.Marshal(p)
			if _, err := ParsePlayback(b); err == nil {
				t.Fatal("accepted null evidence")
			}
		})
	}
	for _, valid := range []bool{false, true} {
		base["valid"] = valid
		b, _ := json.Marshal(base)
		p, err := ParsePlayback(b)
		if err != nil || p.Valid == nil || *p.Valid != valid || p.PlayedSamples != 0 {
			t.Fatalf("valid zero prefix: %+v %v", p, err)
		}
	}
}
