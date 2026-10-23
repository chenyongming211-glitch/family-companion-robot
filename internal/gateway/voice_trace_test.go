package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

type lockedTraceBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *lockedTraceBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedTraceBuffer) snapshot() string {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.String()
}

func TestVoiceTraceHasTimingMetadataWithoutConversationOrCredentials(t *testing.T) {
	h := newPlaybackHarness(t)
	h.s.responseID = ""
	const private = "NEVER_LOG_VOICE_PRIVATE_CONTENT"
	h.s.cfg.DeviceToken = private + "-device-token"
	h.s.cfg.Qwen.APIKey = private + "-api-key"
	var output lockedTraceBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Cleanup(func() { h.s.encoder.Close() })
	ctx := context.Background()
	if err := h.s.onDevice(ctx, deviceEvent{kind: websocket.TextMessage,
		data: []byte(`{"type":"listen","state":"start","mode":"auto"}`)}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []qwen.Event{
		{Type: "input_audio_buffer.speech_started"},
		{Type: "input_audio_buffer.speech_stopped"},
		{Type: "conversation.item.input_audio_transcription.completed", Transcript: private},
		{Type: "response.created", Response: qwen.Response{ID: private + "-response"}},
		{Type: "response.audio_transcript.delta", ResponseID: private + "-response", Delta: private},
		{Type: "response.audio.delta", ResponseID: private + "-response",
			Delta: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{37, 66}, 32))},
		{Type: "response.done", Response: qwen.Response{ID: private + "-response", Status: "completed"}},
		{Type: private, Transcript: private},
	} {
		if err := h.s.onModel(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.s.tts("start", private); err != nil {
		t.Fatal(err)
	}
	if err := h.s.tts("stop", private); err != nil {
		t.Fatal(err)
	}
	text := output.snapshot()
	if strings.Contains(text, private) {
		t.Fatal("voice trace exposed conversation, response ID or credentials")
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("no metadata recorded for the real voice event handlers")
	}
	allowed := map[string]bool{"time": true, "level": true, "msg": true,
		"event": true, "session": true, "mode": true, "upstream_generation": true,
		"response_open": true, "speaking": true, "pending_ms": true, "input_bytes": true, "snapshot_revision": true, "snapshot_token": true, "task_bound": true}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		for key := range record {
			if !allowed[key] {
				t.Fatalf("voice trace has an unexpected payload field %q", key)
			}
		}
		if record["event"] == nil || record["mode"] == nil {
			t.Fatal("voice trace is missing event or mode metadata")
		}
	}
}

func TestPlaybackTraceReportsBoundariesWithoutPrivateContentOrProgressFlood(t *testing.T) {
	h := newPlaybackHarness(t)
	const private = "NEVER_LOG_PLAYBACK_PRIVATE_CONTENT"
	h.s.playbackEnabled = true
	h.s.responseOrigin = requestOrigin{itemID: private, text: private, known: true}
	var output lockedTraceBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if err := h.s.beginPlayback(); err != nil {
		t.Fatal(err)
	}
	h.s.started = true
	h.s.activePlayback.sent = 14400
	report := func(state string, n int, valid bool) {
		data, err := json.Marshal(map[string]any{"type": "playback", "state": state,
			"session_id": h.s.id, "playback_id": 1, "sample_rate": 24000,
			"played_samples": n, "valid": valid, "basis": "i2s_dma", "interrupted": true,
			"untrusted_extra": private})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.s.onDevice(context.Background(), deviceEvent{kind: websocket.TextMessage, data: data}); err != nil {
			t.Fatal(err)
		}
	}
	for n := 2400; n <= 12000; n += 2400 {
		report("progress", n, true)
	}
	h.s.pausePlayback()
	report("finished", 12240, true)
	text := output.snapshot()
	if strings.Contains(text, private) {
		t.Fatal("playback trace leaked private content")
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("missing playback boundary diagnostics")
	}
	var events []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["msg"] != "playback_event" {
			continue
		}
		events = append(events, record["event"].(string))
		if record["playback_id"] != float64(1) || record["sent_samples"] != float64(14400) || record["text_position"] != "unknown" {
			t.Fatal(record)
		}
		if record["event"] == "finished" && (record["played_samples"] != float64(12240) || record["settled"] != true || record["position_known"] != true || record["paused_task_id"] != "task-1") {
			t.Fatal(record)
		}
	}
	if strings.Join(events, ",") != "progress_started,paused_created,finished" {
		t.Fatalf("missing or unbounded playback diagnostics: %v", events)
	}
}

func TestPlaybackTraceRepeatedInvalidFinalDoesNotFloodOrClaimSettlement(t *testing.T) {
	h := newPlaybackHarness(t)
	h.s.playbackEnabled = true
	var output lockedTraceBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if err := h.s.beginPlayback(); err != nil {
		t.Fatal(err)
	}
	h.s.activePlayback.sent = 1440
	data, _ := json.Marshal(map[string]any{"type": "playback", "state": "finished", "session_id": h.s.id,
		"playback_id": 1, "sample_rate": 24000, "played_samples": 0, "valid": false, "basis": "i2s_dma"})
	for range 20 {
		h.s.receivePlayback(data)
	}
	if h.s.activePlayback.settled {
		t.Fatal("premature invalid final settled playback")
	}
	h.s.sealPlayback(false)
	for range 20 {
		h.s.receivePlayback(data)
	}
	var events []string
	for _, line := range strings.Split(strings.TrimSpace(output.snapshot()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		events = append(events, record["event"].(string))
		if record["position_known"] != false || record["played_samples"] != nil {
			t.Fatal(record)
		}
		if record["event"] == "invalid" && record["settled"] != false {
			t.Fatal(record)
		}
	}
	if strings.Join(events, ",") != "invalid,finished" {
		t.Fatalf("repeated invalid report logged as a final boundary: %v", events)
	}
}

func TestPlaybackTraceSharedBlockConfirmsHistoryOnce(t *testing.T) {
	h := newPlaybackHarness(t)
	h.s.playbackEnabled = true
	var output lockedTraceBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if err := h.s.beginPlayback(); err != nil {
		t.Fatal(err)
	}
	h.s.activePlayback.sent = 1440
	for range 2 {
		h.s.hist.addSent("private tool continuation")
		h.s.hist.Turns[len(h.s.hist.Turns)-1].playbackID = 1
	}
	h.s.sealPlayback(false)
	data, _ := json.Marshal(map[string]any{"type": "playback", "state": "finished", "session_id": h.s.id,
		"playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma"})
	h.s.receivePlayback(data)
	if n := strings.Count(output.snapshot(), `"event":"confirmed"`); n != 1 {
		t.Fatalf("one block confirmed %d times", n)
	}
	for _, turn := range h.s.hist.Turns {
		if turn.Delivery != "playback_confirmed" {
			t.Fatal("history upgrade changed")
		}
	}
}
