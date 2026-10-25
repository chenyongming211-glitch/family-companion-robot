package realtimestate

import (
	"strings"
	"testing"
)

func TestRingAndReceiptBoundaries(t *testing.T) {
	ring := NewAudioRing(10)
	pcm := make([]byte, 40)
	for i := range pcm {
		pcm[i] = byte(i)
	}
	if err := ring.Append(pcm); err != nil {
		t.Fatal(err)
	}
	if ring.End != 20 || ring.Slice(0, 5) != nil || string(ring.Slice(10, 20)) != string(pcm[20:]) {
		t.Fatal("ring sample bounds")
	}
	if ring.Append([]byte{1}) == nil {
		t.Fatal("odd PCM accepted")
	}
	turn := NewTurn(1)
	turn.Sent = 7200
	if turn.CanSend() {
		t.Fatal("window permits sixth packet")
	}
	for _, raw := range []string{
		`{"id":true,"completed":1440,"accepted":7200}`,
		`{"id":1.0,"completed":1440,"accepted":7200}`,
		`{"id":1,"completed":1440.0,"accepted":7200}`,
		`{"id":1,"completed":true,"accepted":7200}`,
		`{"id":1,"completed":1440,"accepted":9223372036854775808}`,
		`{"id":1,"completed":1,"accepted":7200}`,
	} {
		encoded := strings.TrimSuffix(raw, "}") + `,"basis":"CoreAudio_dataPlayedBack"}`
		if turn.ReceiptJSON([]byte(encoded)) {
			t.Fatalf("invalid receipt accepted: %s", encoded)
		}
	}
	if !turn.ReceiptJSON([]byte(`{"id":1,"completed":1440,"accepted":7200,"basis":"CoreAudio_dataPlayedBack"}`)) || !turn.CanSend() {
		t.Fatal("valid receipt did not open window")
	}
	if turn.ReceiptJSON([]byte(`{"id":1,"completed":0,"accepted":7200,"basis":"CoreAudio_dataPlayedBack"}`)) {
		t.Fatal("backward receipt accepted")
	}
	turn.SealForDrain()
	turn.Sent = 8640 // seal remains immutable even if a caller changes sent.
	turn.SealForDrain()
	if *turn.DrainTarget != 7200 || !turn.Cut {
		t.Fatal("drain seal changed")
	}
	if !turn.ReceiptJSON([]byte(`{"id":1,"completed":7200,"accepted":7200,"basis":"CoreAudio_dataPlayedBack","terminal":"drained"}`)) {
		t.Fatal("valid drain rejected")
	}
	select {
	case <-turn.DrainDone:
	default:
		t.Fatal("drain not signalled")
	}
	if !turn.ReceiptJSON([]byte(`{"id":1,"completed":7200,"accepted":7200,"basis":"CoreAudio_dataPlayedBack","terminal":"drained"}`)) {
		t.Fatal("duplicate drain rejected")
	}
}

func TestUnplayedDraftAndSafeObservation(t *testing.T) {
	turn := NewTurn(1)
	turn.Generated = "刚才念到十七。接下来十八。"
	turn.Sent = 57600
	turn.Completed = 57600
	turn.Cut = true
	turn.Tracker.Add(map[string]any{"sentence_id": 1, "sentence_end": true, "words": []any{
		map[string]any{"text": "刚才念到", "begin_time": 100, "end_time": 500},
		map[string]any{"text": "17", "begin_time": 500, "end_time": 1000},
	}}, 1100)
	evidence := PlaybackEvidence(turn)
	if evidence.SpokenText == nil || *evidence.SpokenText != "刚才念到十七。" || evidence.ExactStopKnown {
		t.Fatalf("unsafe evidence: %+v", evidence)
	}
	turn.Ended = true
	if PlaybackEvidence(turn).TextSource != "native_transcript_full_playback" {
		t.Fatal("complete native transcript missing")
	}
}

func TestDrainDoneBeforeBeginIsIdempotent(t *testing.T) {
	turn := NewTurn(1)
	turn.SealForDrain()
	turn.MarkDrainDone()
	turn.MarkDrainDone()
	if !turn.ReceiptJSON([]byte(`{"id":1,"completed":0,"accepted":0,"basis":"CoreAudio_dataPlayedBack","terminal":"drained"}`)) {
		t.Fatal("valid zero drain rejected")
	}
	select {
	case <-turn.DrainDone:
	default:
		t.Fatal("drain not closed")
	}
}

func TestProbeReceiptAndPrefix(t *testing.T) {
	receipt := map[string]any{"type": "cut", "id": "one", "basis": "CoreAudio_dataPlayedBack", "completed_samples": 4320, "accepted_samples": 5761, "microphone": false}
	if ms, err := ValidateReceipt(receipt, "one", 6000); err != nil || ms != 180 {
		t.Fatalf("probe receipt: %v %v", ms, err)
	}
	receipt["completed_samples"] = true
	if _, err := ValidateReceipt(receipt, "one", 6000); err == nil {
		t.Fatal("probe accepted bool samples")
	}
	turn := NewTurn(1)
	turn.Generated = "小兔。后面。"
	turn.Sent = 48000
	turn.Completed = 28800
	turn.Tracker.Add(map[string]any{"sentence_id": 1, "sentence_end": true, "words": []any{map[string]any{"text": "小兔", "begin_time": 100, "end_time": 500}}}, 1000)
	if result := turn.Prefix(); result["prefix"] != "小兔。" || result["status"] != "candidate" {
		t.Fatal(result)
	}
}
