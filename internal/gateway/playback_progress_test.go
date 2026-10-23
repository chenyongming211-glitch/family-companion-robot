package gateway

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
)

func localResult(t *testing.T, p *toolPeer, response, name, args string) map[string]any {
	t.Helper()
	toolParent(t, p, response, 0)
	toolCall(t, p, response, response+"-call", name, args)
	toolDone(t, p, response)
	out := wantToolOutput(t, p, response+"-call")
	p.want(t, "response.create")
	return out
}
func readStart(t *testing.T, p *toolPeer) map[string]any {
	t.Helper()
	for {
		v := p.read(t)
		if v.body["type"] == "tts" && v.body["state"] == "start" {
			return v.body
		}
	}
}
func wantSTT(t *testing.T, p *toolPeer) {
	t.Helper()
	for {
		v := p.read(t)
		if v.body["type"] == "stt" {
			return
		}
	}
}
func readStop(t *testing.T, p *toolPeer) map[string]any {
	t.Helper()
	for {
		v := p.read(t)
		if v.body["type"] == "tts" && v.body["state"] == "stop" {
			return v.body
		}
	}
}
func receipt(p *toolPeer, t *testing.T, start map[string]any, state string, n any, valid bool) {
	p.send(t, map[string]any{"type": "playback", "state": state, "session_id": start["session_id"], "playback_id": start["playback_id"], "sample_rate": 24000, "played_samples": n, "valid": valid, "basis": "i2s_dma", "interrupted": false})
}
func TestPlaybackProgressNegotiatedPausedTaskSurvivesReconnect(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, cfg := h.model(t)
	if !strings.Contains(fmt.Sprint(cfg["tools"]), "conversation_get_paused_task") {
		t.Fatal("local context tools missing")
	}
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "original"})
	toolParent(t, p, "root", 20)
	start := readStart(t, h.device)
	if start["playback_id"] != float64(1) {
		t.Fatalf("missing first playback identity: %v", start)
	}
	// Response starts before final STT; it still belongs to the committed item.
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "original", "transcript": "请读完整篇文章"})
	wantSTT(t, h.device)
	receipt(h.device, t, start, "progress", 1440, true)
	h.device.send(t, map[string]any{"type": "abort"})
	stop := readStop(t, h.device)
	if stop["playback_id"] != start["playback_id"] {
		t.Fatal(stop)
	}
	fresh, _ := h.model(t)
	// Wrong generation with reused item id cannot backfill original request.
	fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "original", "transcript": "新问题"})
	wantSTT(t, h.device)
	receipt(h.device, t, start, "finished", 1440, true)
	// The model and device are independent sockets. A device-originated mode
	// update acknowledges that the preceding receipt was handled before query.
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "manual"})
	fresh.want(t, "session.update")
	out := localResult(t, fresh, "query", "conversation_get_paused_task", "{}")
	if out["available"] != true || out["original_request"] != "请读完整篇文章" || out["played_samples"] != float64(1440) || out["settled"] != true || out["text_position"] != "unknown" {
		t.Fatalf("paused task = %v", out)
	}
	id := out["task_id"]
	// A side answer and second interruption preserve the original task.
	toolParent(t, fresh, "side", 20)
	side := readStart(t, h.device)
	if side["playback_id"] != float64(2) {
		t.Fatal(side)
	}
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	next, _ := h.model(t)
	out = localResult(t, next, "query2", "conversation_get_paused_task", "{}")
	if out["task_id"] != id || out["original_request"] != "请读完整篇文章" {
		t.Fatal(out)
	}
}
func TestPlaybackProgressLegacyLocalToolsAndUnknownPosition(t *testing.T) {
	h := newToolHarness(t, false)
	p, cfg := h.model(t)
	if !strings.Contains(cfg["instructions"].(string), controlsUnavailable) {
		t.Fatal("local tools imply device controls")
	}
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "原任务"})
	wantSTT(t, h.device)
	toolParent(t, p, "legacy", 20)
	start := readStart(t, h.device)
	if start["playback_id"] != nil {
		t.Fatal("legacy metadata emitted")
	}
	// Raw receipts do not negotiate support.
	start["playback_id"] = 1
	receipt(h.device, t, start, "progress", 1440, true)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	fresh, _ := h.model(t)
	out := localResult(t, fresh, "get", "conversation_get_paused_task", "{}")
	if out["original_request"] != "原任务" || out["position_known"] != false || out["text_position"] != "unknown" {
		t.Fatal(out)
	}
}
func TestPlaybackProgressFullFinalPromotesOnlyMatchingHistory(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	toolParent(t, p, "first", 0)
	// Half frame is padded, so the transport total is exactly 1440 samples.
	p.send(t, map[string]any{"type": "response.audio.delta", "response_id": "first", "delta": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes/2))})
	p.send(t, map[string]any{"type": "response.audio_transcript.done", "response_id": "first", "transcript": "完整生成稿"})
	toolDone(t, p, "first")
	start := readStart(t, h.device)
	readStop(t, h.device)
	receipt(h.device, t, start, "finished", 1440, true)
	// Barrier uses another model input; abort rebuilds history after the receipt.
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	_, cfg := h.model(t)
	text := cfg["instructions"].(string)
	if !strings.Contains(text, `"delivery":"playback_confirmed"`) || !strings.Contains(text, `"generated_text":"完整生成稿"`) {
		t.Fatal(text)
	}
}
func TestPlaybackProgressDoesNotExtendIdle(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true}, func(c *Config) { c.IdleTimeout = 250 * time.Millisecond })
	h.model(t)
	started := time.Now()
	until := started.Add(650 * time.Millisecond)
	for time.Now().Before(until) {
		_ = h.device.c.WriteJSON(map[string]any{"type": "playback", "state": "progress", "session_id": h.hello["session_id"], "playback_id": 1, "sample_rate": 24000, "played_samples": 0, "valid": true, "basis": "i2s_dma"})
		select {
		case v := <-h.device.in:
			if v.err != nil {
				if time.Since(started) < 120*time.Millisecond {
					t.Fatal("receipt disconnected the session before idle expiry")
				}
				return
			}
		case <-time.After(30 * time.Millisecond):
		}
	}
	t.Fatal("telemetry kept the session alive")
}

func TestPausedTaskLocalToolsMutateThroughBoundedContinuationWithoutMCP(t *testing.T) {
	h := newToolHarness(t, false)
	p, _ := h.model(t)
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "原任务"})
	wantSTT(t, h.device)
	toolParent(t, p, "root", 20)
	readStart(t, h.device)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	fresh, _ := h.model(t)
	localResult(t, fresh, "get", "conversation_get_paused_task", "{}")
	get := localResult(t, fresh, "get-current", "conversation_get_paused_task", "{}")
	wrong := localResult(t, fresh, "wrong", "conversation_update_paused_task", `{"action":"discard","task_id":"wrong"}`)
	if wrong["error"] != "task_id_mismatch" {
		t.Fatal(wrong)
	}
	noSTT := localResult(t, fresh, "unknown", "conversation_update_paused_task", fmt.Sprintf(`{"action":"replace","task_id":%q,"expected_revision":%v}`, get["task_id"], get["revision"]))
	if noSTT["error"] != "current_request_unknown" {
		t.Fatal(noSTT)
	}
	fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "new"})
	fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "new", "transcript": "实际的新任务"})
	wantSTT(t, h.device)
	completeRouteWire(t, fresh, "replace-route", "replace")
	replace := localResult(t, fresh, "replace-state", "conversation_get_paused_task", `{}`)
	if replace["original_request"] != "实际的新任务" || replace["task_id"] == get["task_id"] || replace["text_position"] != "unknown" || replace["position_known"] != false {
		t.Fatal(replace)
	}
	stale := localResult(t, fresh, "stale", "conversation_update_paused_task", fmt.Sprintf(`{"action":"discard","task_id":%q}`, get["task_id"]))
	if stale["error"] != "routing_already_decided" {
		t.Fatal(stale)
	}

}
