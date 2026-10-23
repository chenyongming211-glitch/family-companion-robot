package gateway

import (
	"context"
	"encoding/json"
	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResumeStateAutoContextAndInterruptedDraftIsolation(t *testing.T) {
	h := newToolHarness(t, false)
	p, _ := h.model(t)
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "数到一百"})
	wantSTT(t, h.device)
	toolParent(t, p, "root", 20)
	readStart(t, h.device)
	p.send(t, map[string]any{"type": "response.audio_transcript.delta", "response_id": "root", "delta": "unplayed-secret-draft"})
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "数到一百"})
	wantSTT(t, h.device)
	h.device.send(t, map[string]any{"type": "abort"})
	readStop(t, h.device)
	_, cfg := h.model(t)
	instructions := cfg["instructions"].(string)
	if !strings.Contains(instructions, `"paused_task"`) || !strings.Contains(instructions, `"original_request":"数到一百"`) {
		t.Errorf("authoritative auto snapshot absent: %s", instructions)
	}
	if strings.Contains(instructions, "unplayed-secret-draft") {
		t.Error("unplayed interrupted draft leaked into rebuilt context")
	}
}
func TestResumeStateRevisionAndSecondInterruption(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	s.generation = 1
	s.responseOrigin = requestOrigin{generation: 1, itemID: "original", known: true, text: "数到一百"}
	h.append(t, make([]byte, audio.FrameBytes*2))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	first := s.pausedData()
	id := first["task_id"]
	if first["revision"] == nil {
		t.Error("snapshot has no revision")
	}
	stale, _ := s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":"resume","expected_revision":999}`, id))
	if !strings.Contains(stale, "revision_mismatch") {
		t.Errorf("stale resume not rejected: %s", stale)
	}
	args, _ := json.Marshal(map[string]any{"task_id": id, "action": "resume", "expected_revision": first["revision"]})
	resumed, _ := s.conversationTool("conversation_update_paused_task", string(args))
	if !strings.Contains(resumed, `"ok":true`) {
		t.Fatalf("resume failed: %s", resumed)
	}
	s.started = false
	s.activePlayback = nil
	s.devicePlayback = nil
	s.responseOrigin = requestOrigin{generation: 1, itemID: "continue", known: true, text: "继续"}
	if err := s.beginPlayback(); err != nil {
		t.Fatal(err)
	}
	s.started = true
	s.activePlayback.sent = 2880
	s.pausePlayback()
	latest := s.pausedData()
	if latest["task_id"] != id || latest["original_request"] != "数到一百" || latest["playback_id"] != uint32(2) {
		t.Errorf("resumed segment did not preserve original task and update latest playback: %v", latest)
	}
}
func TestResumeStateLateFinalRevisionAndSideIsolation(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	s.responseOrigin = requestOrigin{known: true, text: "原任务"}
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	before := s.pausedData()["revision"]
	deliverReceipt(t, h, map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma", "interrupted": true})
	if s.pausedData()["revision"] == before {
		t.Error("late final did not revise snapshot")
	}
	s.started = false
	s.activePlayback = nil
	s.devicePlayback = nil
	s.responseOrigin = requestOrigin{known: true, text: "旁支"}
	s.beginPlayback()
	s.started = true
	s.activePlayback.sent = 1440
	s.pausePlayback()
	if s.pausedData()["playback_id"] != uint32(1) {
		t.Fatal("unbound side answer stole paused task")
	}
}
func TestResumeStateNativeGateWaitsForSnapshotACK(t *testing.T) {
	h := newToolHarness(t, false)
	p, cfg := h.model(t)
	detection := cfg["turn_detection"].(map[string]any)
	if detection["create_response"] != false || detection["interrupt_response"] != true {
		t.Errorf("native VAD still creates responses: %v", detection)
	}
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "turn"})
	update := p.read(t).body
	if update["type"] != "session.update" {
		t.Fatalf("generation did not synchronize snapshot first: %v", update)
	}
	session := update["session"].(map[string]any)
	if len(session) != 1 || session["instructions"] == nil {
		t.Fatal("refresh resent immutable configuration")
	}
	p.quiet(t, 40*time.Millisecond)
	p.send(t, map[string]any{"type": "session.updated"})
	p.want(t, "response.create")
}

func TestResumeStateSealedInterruptedDraftIsOmitted(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	s.responseDone = true
	s.transcript = "unplayed-tail"
	h.append(t, make([]byte, audio.FrameBytes))
	at := time.Unix(100, 0)
	h.play(t, at)
	h.play(t, at.Add(60*time.Millisecond))
	s.pausePlayback()
	if strings.Contains(s.hist.context(), "unplayed-tail") {
		t.Fatal("interrupt after transport seal leaked unplayed draft")
	}
	if s.hist.Turns[0].GeneratedText != "unplayed-tail" {
		t.Fatal("diagnostic draft unexpectedly discarded")
	}
}
func TestResumeStateResumeExistingBlockAndFullFinalReleasesTask(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	s.responseOrigin = requestOrigin{known: true, text: "原目标"}
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	s.started = false
	s.activePlayback = nil
	s.devicePlayback = nil
	s.responseTaskID = ""
	s.responseOrigin = requestOrigin{known: true, text: "继续"}
	s.beginPlayback()
	s.started = true
	s.activePlayback.sent = 1440
	args := fmt.Sprintf(`{"task_id":%q,"action":"resume","expected_revision":%d}`, s.paused.id, s.snapshotRevision)
	out, _ := s.conversationTool("conversation_update_paused_task", args)
	if !strings.Contains(out, `"ok":true`) {
		t.Fatal(out)
	}
	if s.activePlayback.taskID != s.paused.id || s.paused.playback.id != 2 {
		t.Fatal("resume before tool continuation left open TTS block unbound")
	}
	// A late final from the original block cannot move the new task cursor back.
	deliverReceipt(t, h, map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma", "interrupted": true})
	if s.paused.playback.id != 2 {
		t.Fatal("old final overwrote newest resumed block")
	}
	s.sealPlayback(false)
	deliverReceipt(t, h, map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 2, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma"})
	if s.paused != nil || s.responseTaskID != "" {
		t.Fatal("fully delivered resumed block kept stale task ownership")
	}
}

func attachSnapshotModel(t *testing.T, s *session) *toolPeer {
	t.Helper()
	peers := make(chan *toolPeer, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		p := newToolPeer(c)
		p.send(t, map[string]any{"type": "session.created"})
		p.want(t, "session.update")
		peers <- p
	}))
	t.Cleanup(server.Close)
	model, err := qwen.Open(context.Background(), qwen.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http"), VAD: "semantic_vad"}, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.Close)
	s.model = model
	p := <-peers
	t.Cleanup(func() { p.c.Close() })
	return p
}
func ackSnapshotWire(t *testing.T, s *session, p *toolPeer) {
	t.Helper()
	p.send(t, map[string]any{"type": "session.updated"})
	ev, err := s.model.Read()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.onModel(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
}
func TestResumeStateLateFinalRefreshesInFlightSnapshotAndModeACKCannotAuthorize(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	s.responseOrigin = requestOrigin{known: true, text: "数到一百"}
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	s.responseID = ""
	s.started = false
	s.activePlayback = nil
	s.devicePlayback = nil
	p := attachSnapshotModel(t, s)
	if err := s.model.SetMode("manual"); err != nil {
		t.Fatal(err)
	}
	mode := p.read(t).body
	if mode["type"] != "session.update" {
		t.Fatal(mode)
	}
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	p.quiet(t, 20*time.Millisecond)
	if err := s.advanceGeneration(time.Now().Add(playbackSettlementTimeout)); err != nil {
		t.Fatal(err)
	}
	first := p.read(t).body
	if first["type"] != "session.update" || !strings.Contains(fmt.Sprint(first), `"settlement_timed_out":true`) {
		t.Fatal(first)
	}
	ackSnapshotWire(t, s, p)
	p.quiet(t, 20*time.Millisecond) // only the mode ACK
	// Progress without a final cannot repair the timeout fallback.
	deliverReceipt(t, h, map[string]any{"type": "playback", "state": "progress", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma"})
	if s.pausedData()["position_known"] != false || s.pausedData()["settlement_timed_out"] != true {
		t.Fatal("late progress healed final timeout")
	}
	deliverReceipt(t, h, map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma", "interrupted": true})
	ackSnapshotWire(t, s, p)
	current := p.read(t).body
	if current["type"] != "session.update" || !strings.Contains(fmt.Sprint(current), `"settled":true`) || !strings.Contains(fmt.Sprint(current), `"played_samples":1440`) {
		t.Fatalf("stale ACK created instead of refreshing: %v", current)
	}
	p.quiet(t, 20*time.Millisecond)
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
}

func TestResumeStateDiscardDoesNotResurrectBoundActiveAudio(t *testing.T) {
	for _, action := range []string{"discard", "replace"} {
		t.Run(action, func(t *testing.T) {
			h := newPlaybackHarness(t)
			s := h.s
			s.generation = 1
			s.playbackEnabled = true
			s.responseOrigin = requestOrigin{generation: 1, known: true, text: "旧目标"}
			h.append(t, make([]byte, audio.FrameBytes))
			h.play(t, time.Unix(100, 0))
			s.pausePlayback()
			s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":"resume","expected_revision":%d}`, s.paused.id, s.snapshotRevision))
			oldID := s.paused.id
			s.currentInput = requestOrigin{generation: 1, known: true, text: "新目标"}
			s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":%q,"expected_revision":%d}`, oldID, action, s.snapshotRevision))
			s.pausePlayback()
			if action == "discard" && s.paused != nil {
				t.Fatal("discarded bound audio resurrected task")
			}
			if action == "replace" && s.pausedData()["original_request"] != "新目标" {
				t.Fatal("retired audio overwrote replacement")
			}
		})
	}
}
func TestResumeStateMissingOrStaleRevisionCannotMutate(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.responseOrigin = requestOrigin{known: true, text: "原目标"}
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	for _, action := range []string{"resume", "discard", "replace"} {
		for _, revision := range []string{"", `,"expected_revision":0`} {
			out, _ := s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":%q%s}`, s.paused.id, action, revision))
			if !strings.Contains(out, "revision_mismatch") {
				t.Fatalf("%s accepted stale/missing revision: %s", action, out)
			}
		}
	}
}
func TestResumeStateSnapshotACKDeadlineFailsClosed(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.responseID = ""
	p := attachSnapshotModel(t, s)
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	p.read(t)
	if err := s.advanceGeneration(time.Now().Add(snapshotACKTimeout)); err == nil {
		t.Fatal("missing session.updated did not fail closed")
	}
	p.quiet(t, 20*time.Millisecond)
}
