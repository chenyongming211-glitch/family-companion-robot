package protocol

import (
	"encoding/json"
	"errors"
)

// Playback is parsed separately: omitted booleans and fractional counters must
// never silently become evidence of a contiguous hardware prefix.
type Playback struct {
	Type          string `json:"type"`
	State         string `json:"state"`
	SessionID     string `json:"session_id"`
	PlaybackID    uint32 `json:"playback_id"`
	SampleRate    int    `json:"sample_rate"`
	PlayedSamples uint64 `json:"played_samples"`
	Valid         *bool  `json:"valid"`
	Basis         string `json:"basis"`
	Interrupted   bool   `json:"interrupted"`
}

func ParsePlayback(data []byte) (Playback, error) {
	var p Playback
	var fields map[string]json.RawMessage
	if len(data) > 16384 || json.Unmarshal(data, &p) != nil || json.Unmarshal(data, &fields) != nil {
		return p, errors.New("invalid playback receipt")
	}
	for _, key := range []string{"session_id", "playback_id", "sample_rate", "played_samples", "valid", "state", "basis"} {
		if len(fields[key]) == 0 || string(fields[key]) == "null" {
			return p, errors.New("missing playback field")
		}
	}
	if p.Type != "playback" || p.SessionID == "" || p.PlaybackID == 0 || p.SampleRate != 24000 || p.Valid == nil || p.Basis != "i2s_dma" || (p.State != "progress" && p.State != "finished") {
		return p, errors.New("unsupported playback receipt")
	}
	return p, nil
}
