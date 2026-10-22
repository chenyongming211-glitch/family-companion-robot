package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

func deliverReceipt(t *testing.T, h *playbackHarness, fields map[string]any) {
	t.Helper()
	b, _ := json.Marshal(fields)
	// Device-to-server and server-to-device both cross actual WebSocket transport.
	if err := h.client.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatal(err)
	}
	kind, data, err := h.s.device.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.s.onDevice(context.Background(), deviceEvent{kind: kind, data: data}); err != nil {
		t.Fatal(err)
	}
}
func TestPlaybackLedgerValidationAndFinalImmutability(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	h.append(t, make([]byte, audio.FrameBytes*3))
	start := time.Unix(100, 0)
	h.play(t, start)
	h.play(t, start.Add(60*time.Millisecond))
	h.play(t, start.Add(120*time.Millisecond))
	packet := func() map[string]any {
		return map[string]any{"type": "playback", "state": "progress", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma"}
	}
	deliverReceipt(t, h, packet())
	for name, alter := range map[string]func(map[string]any){
		"duplicate": func(p map[string]any) {}, "regression": func(p map[string]any) { p["played_samples"] = 10 }, "overbound": func(p map[string]any) { p["played_samples"] = 4321 }, "bad_id": func(p map[string]any) { p["playback_id"] = 1.5 },
		"overflow_id": func(p map[string]any) { p["playback_id"] = uint64(math.MaxUint32) + 1 },
		"fraction":    func(p map[string]any) { p["played_samples"] = 1440.5 }, "missing": func(p map[string]any) { delete(p, "played_samples") }, "missingvalid": func(p map[string]any) { delete(p, "valid") }, "nullvalid": func(p map[string]any) { p["valid"] = nil }, "oldid": func(p map[string]any) { p["playback_id"] = 99 }, "session": func(p map[string]any) { p["session_id"] = "old" }, "rate": func(p map[string]any) { p["sample_rate"] = 16000 }, "basis": func(p map[string]any) { p["basis"] = "clock" },
	} {
		t.Run(name, func(t *testing.T) {
			p := packet()
			alter(p)
			deliverReceipt(t, h, p)
			if s.activePlayback.played != 1440 || !s.activePlayback.known {
				t.Fatal("invalid receipt changed prefix")
			}
		})
	}
	// The identity becomes permanently unknown after a provable gap.
	p := packet()
	p["valid"] = false
	deliverReceipt(t, h, p)
	p = packet()
	p["played_samples"] = 2880
	deliverReceipt(t, h, p)
	if s.activePlayback.known {
		t.Fatal("later receipt healed a known gap")
	}
	s.pausePlayback()
	p["state"] = "finished"
	p["interrupted"] = true
	deliverReceipt(t, h, p)
	before := s.pausedData()
	p["played_samples"] = 4320
	deliverReceipt(t, h, p)
	if s.pausedData()["played_samples"] != before["played_samples"] || s.pausedData()["position_known"] != false || s.pausedData()["settled"] != true {
		t.Fatal(s.pausedData())
	}
}
func TestPlaybackLedgerBoundAndIdentityExhaustion(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	for i := 0; i < 20; i++ {
		s.responseID = fmt.Sprint(i)
		s.responseDone = true
		h.append(t, make([]byte, audio.FrameBytes))
		at := time.Unix(int64(100+i), 0)
		h.play(t, at)
		h.play(t, at.Add(60*time.Millisecond))
	}
	if len(s.playbacks) != 16 || s.playbacks[0].id != 5 || s.playbackSequence != 20 {
		t.Fatal("unbounded or reused playback identities")
	}
	s.playbackSequence = math.MaxUint32
	s.responseID = "exhausted"
	h.append(t, make([]byte, audio.FrameBytes))
	h.now = time.Unix(200, 0)
	if err := s.play(); err == nil {
		t.Fatal("exhausted identity reused")
	}
}
func TestPlaybackProgressToolParentContinuationShareIdentityAndOrigin(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "原任务"})
	wantSTT(t, h.device)
	toolParent(t, p, "parent", 1)
	start := readStart(t, h.device)
	toolCall(t, p, "parent", "inspect", "conversation_get_paused_task", "{}")
	toolDone(t, p, "parent")
	wantToolOutput(t, p, "inspect")
	p.want(t, "response.create")
	// Tool parent has drained but an actual block stays open before continuation.
	h.device.send(t, map[string]any{"type": "abort"})
	stop := readStop(t, h.device)
	if stop["playback_id"] != start["playback_id"] {
		t.Fatal(stop)
	}
	fresh, _ := h.model(t)
	out := localResult(t, fresh, "get", "conversation_get_paused_task", "{}")
	if out["original_request"] != "原任务" || out["playback_id"] != float64(1) {
		t.Fatal(out)
	}
}
func TestPlaybackProgressToolContinuationCumulativeSentSamples(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	toolParent(t, p, "parent", 1)
	start := readStart(t, h.device)
	toolCall(t, p, "parent", "inspect", "conversation_get_paused_task", "{}")
	p.send(t, map[string]any{"type": "response.audio_transcript.done", "response_id": "parent", "transcript": "父回复生成稿"})
	toolDone(t, p, "parent")
	wantToolOutput(t, p, "inspect")
	p.want(t, "response.create")
	toolParent(t, p, "continuation", 1)
	p.send(t, map[string]any{"type": "response.audio_transcript.done", "response_id": "continuation", "transcript": "续答生成稿"})
	toolDone(t, p, "continuation")
	stop := readStop(t, h.device)
	if stop["playback_id"] != start["playback_id"] {
		t.Fatal("continuation opened a different block")
	}
	receipt(h.device, t, start, "finished", 2880, true)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	_, cfg := h.model(t)
	if strings.Count(cfg["instructions"].(string), `"delivery":"playback_confirmed"`) != 2 {
		t.Fatal("cumulative final did not confirm both matching tool turns")
	}
}
func TestPausedTaskLocalMutationAndRequestBounds(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, text: strings.Repeat("字", 512), known: true, truncated: true}
	s.responseOrigin = s.currentInput
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	if s.pausedData()["original_request_truncated"] != true || len([]rune(s.pausedData()["original_request"].(string))) != 512 {
		t.Fatal(s.pausedData())
	}
	id := s.paused.id
	out, _ := s.conversationTool("conversation_update_paused_task", `{"action":"discard","task_id":"wrong"}`)
	if !strings.Contains(out, "task_id_mismatch") || s.paused == nil {
		t.Fatal(out)
	}
	s.currentInput = requestOrigin{generation: 1, text: "实际的新请求", known: true}
	out, _ = s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"action":"replace","task_id":%q,"new_request":"模型自编"}`, id))
	if !strings.Contains(out, "实际的新请求") || strings.Contains(out, "模型自编") || s.paused.id == id {
		t.Fatal(out)
	}
	out, _ = s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"action":"discard","task_id":%q}`, s.paused.id))
	if s.paused != nil || !strings.Contains(out, `"available":false`) {
		t.Fatal(out)
	}
}
func TestRequestOriginDoesNotStealLateUnrelatedSTT(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.generation = 1
	s.commitOrigin("root")
	s.responseOrigin = s.currentInput
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	s.transcribeOrigin("side", "旁支问题")
	if s.paused.playback.origin.known {
		t.Fatal("unrelated input became original task")
	}
	s.transcribeOrigin("root", strings.Repeat("原", 600))
	if !s.paused.playback.origin.known || !s.paused.playback.origin.truncated || len([]rune(s.paused.playback.origin.text)) != 512 {
		t.Fatal("matching late STT lost")
	}
	s.generation = 2
	s.transcribeOrigin("root", "同名新代次")
	if s.paused.playback.origin.text == "同名新代次" {
		t.Fatal("generation identity lost")
	}
}
func TestNoAudioDoesNotManufacturePausedTask(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.pausePlayback()
	if s.paused != nil {
		t.Fatal("created pause before actual TTS")
	}
	// A response.created without committed identity does not invent a request.
	s.responseID = ""
	s.generation = 1
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "unknown"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.encoder.Close)
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	if s.pausedData()["original_request_known"] != false {
		t.Fatal(s.pausedData())
	}
}

func TestPausedTaskReceiptSurvivesLedgerEviction(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	s.started = false
	s.activePlayback = nil
	for i := 0; i < maxPlaybackRecords; i++ {
		if err := s.beginPlayback(); err != nil {
			t.Fatal(err)
		}
	}
	deliverReceipt(t, h, map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma", "interrupted": true})
	if s.pausedData()["settled"] != true || s.pausedData()["played_samples"] != uint64(1440) {
		t.Fatalf("paused final dropped after recent ledger eviction: %v", s.pausedData())
	}
}
func TestToolContinuationWithoutParentAudioPreservesRequestOrigin(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "root"})
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "root", "transcript": "原任务"})
	wantSTT(t, h.device)
	toolParent(t, p, "parent", 0)
	toolCall(t, p, "parent", "query", "conversation_get_paused_task", "{}")
	// A newer committed item must not replace the request of a tool continuation.
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "side"})
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "side", "transcript": "旁支问题"})
	wantSTT(t, h.device)
	toolDone(t, p, "parent")
	wantToolOutput(t, p, "query")
	p.want(t, "response.create")
	toolParent(t, p, "continuation", 20)
	readStart(t, h.device)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	fresh, _ := h.model(t)
	out := localResult(t, fresh, "get", "conversation_get_paused_task", "{}")
	if out["original_request"] != "原任务" {
		t.Fatal(out)
	}
}

func TestPlaybackFinalCannotPromotePartialOrInterruptedText(t *testing.T) {
	for _, tc := range []struct {
		name        string
		played      uint64
		interrupted bool
		valid       bool
	}{
		{"partial", 1439, false, true}, {"interrupted", 1440, true, true}, {"gap", 1440, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlaybackHarness(t)
			s := h.s
			s.playbackEnabled = true
			s.transcript = "生成稿"
			s.responseDone = true
			h.append(t, make([]byte, audio.FrameBytes))
			at := time.Unix(100, 0)
			h.play(t, at)
			h.play(t, at.Add(60*time.Millisecond))
			p := map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": tc.played, "valid": tc.valid, "basis": "i2s_dma", "interrupted": tc.interrupted}
			deliverReceipt(t, h, p)
			p["played_samples"] = 1440
			p["interrupted"] = false
			p["valid"] = true
			deliverReceipt(t, h, p)
			if s.hist.Turns[0].Delivery != "transport_sent" {
				t.Fatal("partial/interrupted/gap final promoted generation as fully played")
			}
		})
	}
}
func TestPlaybackDelayedFinalOnlyPromotesMatchingClosedBlock(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	s.transcript = "第一块"
	s.responseDone = true
	h.append(t, make([]byte, audio.FrameBytes))
	at := time.Unix(100, 0)
	h.play(t, at)
	h.play(t, at.Add(60*time.Millisecond))
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "second"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.encoder.Close)
	s.transcript = "第二块"
	s.responseDone = true
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, at.Add(time.Second))
	deliverReceipt(t, h, map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma"})
	if s.hist.Turns[0].Delivery != "playback_confirmed" || s.activePlayback.id != 2 || s.activePlayback.known {
		t.Fatal("delayed final crossed block identity")
	}
	h.play(t, at.Add(time.Second+60*time.Millisecond))
	if s.hist.Turns[1].Delivery != "transport_sent" {
		t.Fatal("new block inherited old confirmation")
	}
}

func TestRequestOriginRejectsUnboundedUpstreamIdentity(t *testing.T) {
	for _, typ := range []string{"input_audio_buffer.committed", "conversation.item.input_audio_transcription.completed"} {
		t.Run(typ, func(t *testing.T) {
			h := newPlaybackHarness(t)
			if err := h.s.onModel(context.Background(), qwen.Event{Type: typ, ItemID: strings.Repeat("x", 257)}); err == nil {
				t.Fatal("unbounded input identity retained")
			}
		})
	}
}

func TestPlaybackInvalidFinalBeforeServerStopCannotHeal(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	p := map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 0, "valid": false, "basis": "i2s_dma"}
	deliverReceipt(t, h, p)
	p["state"] = "progress"
	p["valid"] = true
	p["played_samples"] = 1440
	deliverReceipt(t, h, p)
	if s.activePlayback.known || !s.activePlayback.invalid {
		t.Fatal("invalid final before server stop healed into a valid prefix")
	}
}
