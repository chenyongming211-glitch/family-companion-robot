package management

import (
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"testing"
)

func TestProjectionEvidenceAndTail(t *testing.T) {
	info := realtimelive.SessionInfo{Ownership: realtimelive.Ownership{SessionID: "0123456789abcdef0123456789abcdef", DeviceID: "dev_0123456789abcdef0123456789abcdef", ClientType: "xiaozhi"}}
	event := func(kind string, fields map[string]any) []byte {
		fields["event"] = kind
		fields["session_id"] = info.SessionID
		fields["device_id"] = info.DeviceID
		fields["client_type"] = info.ClientType
		b, _ := json.Marshal(fields)
		return append(b, '\n')
	}
	raw := append(event("user_text", map[string]any{"id": 1, "text": "你好"}), event("generated", map[string]any{"id": 1, "text": "你好呀", "sent": 1440})...)
	raw = append(raw, event("playback", map[string]any{"id": 1, "completed": 1440, "accepted": 1440, "sent": 1440, "terminal": "played", "basis": "i2s_dma"})...)
	turns, pending, err := project(append(raw, []byte("{partial")...), info)
	if err != nil || !pending || len(turns) != 1 || turns[0].Played != "你好呀" || turns[0].Playback != "played" {
		t.Fatalf("%+v %v %v", turns, pending, err)
	}
	_, _, err = project(append(raw, []byte("{bad}\n")...), info)
	if !errors.Is(err, ErrSource) {
		t.Fatal(err)
	}
}

func TestProjectionUsesSynthesizedTextOnlyAfterPlayback(t *testing.T) {
	const spoken = "小刺猬出门了。它带了一把伞。"
	const draft = "（表情：开心）\n\n" + spoken
	info, event := projectionFixture()
	base := event("user_text", map[string]any{"id": 1, "text": "讲个故事"})
	full := append(base, event("generated", map[string]any{"id": 1, "text": draft, "synthesized_text": spoken, "sent": 24000})...)
	projectTurn := func(raw []byte) TurnDTO {
		t.Helper()
		turns, _, err := project(raw, info)
		if err != nil || len(turns) != 1 || turns[0].Generated != draft {
			t.Fatalf("projection lost original: %+v, %v", turns, err)
		}
		return turns[0]
	}
	if got := projectTurn(full); got.Played != "" || got.ExactStopKnown {
		t.Fatal("synthesis alone became playback evidence", got)
	}
	played := event("playback", map[string]any{"id": 1, "completed": 24000, "accepted": 24000, "sent": 24000, "terminal": "played", "basis": "i2s_dma"})
	if got := projectTurn(append(full, played...)); got.Played != spoken || !got.ExactStopKnown || got.TextSource != "tts_input_full_playback" {
		t.Fatal("full receipt did not confirm the synthesized body", got)
	}
	cut := append(base, event("generated_interrupted", map[string]any{"id": 1, "text": draft, "synthesized_text": spoken, "sent": 24000})...)
	cut = append(cut, event("playback", map[string]any{"id": 1, "completed": 12000, "accepted": 12000, "sent": 24000, "terminal": "drained", "basis": "i2s_dma"})...)
	if got := projectTurn(cut); got.Played != "" || got.ExactStopKnown {
		t.Fatal("partial receipt credited unheard speech", got)
	}
	cut = append(cut, event("output_asr_final", map[string]any{"id": 1, "sentence": map[string]any{"sentence_id": 1, "sentence_end": true, "words": []any{map[string]any{"text": "小刺猬出门了", "begin_time": 0, "end_time": 200}}}})...)
	if got := projectTurn(cut); got.Played != "小刺猬出门了。" || got.ExactStopKnown || got.TextSource != "output_asr" {
		t.Fatal("partial receipt did not align precisely the heard opening", got)
	}
	legacy := append(base, event("generated", map[string]any{"id": 1, "text": draft, "sent": 24000})...)
	if got := projectTurn(append(legacy, played...)); got.Played != draft || got.TextSource != "native_transcript_full_playback" {
		t.Fatal("changed the meaning of a preexisting recording", got)
	}
}

func TestProjectionRejectsInvalidSynthesizedArchiveField(t *testing.T) {
	info, event := projectionFixture()
	generated := func(text any) []byte {
		return event("generated", map[string]any{"id": 1, "text": "原稿包含正文", "synthesized_text": text})
	}
	for _, raw := range [][]byte{generated(nil), generated(""), generated(1), append(generated("正文"), generated("原稿")...)} {
		if _, _, err := project(raw, info); !errors.Is(err, ErrSource) {
			t.Fatalf("accepted invalid or conflicting TTS metadata: %v", err)
		}
	}
}
