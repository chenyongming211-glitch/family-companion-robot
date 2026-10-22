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
		"response_open": true, "speaking": true, "pending_ms": true, "input_bytes": true}
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
