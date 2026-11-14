package realtimelive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newEchoAdmissionRig(t *testing.T) *admissionRig {
	t.Helper()
	return newAdmissionRig(t, func(s *Session) {
		s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi"}
	})
}

func echoOutput(t *testing.T, r *admissionRig, opening string, packets int) *liveTurn {
	t.Helper()
	rt := r.s.addTurn()
	r.s.mu.Lock()
	rt.turn.Generated = opening
	rt.turn.Begun = true
	err := r.s.sendLocked(map[string]any{"type": "begin", "id": rt.turn.ID})
	r.s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for range packets {
		if ok, err := r.s.binary(rt, make([]byte, 2880)); err != nil || !ok {
			t.Fatalf("release opening audio: %v", err)
		}
	}
	return rt
}

func echoReceipt(rt *liveTurn, played int64) map[string]any {
	return map[string]any{"id": rt.turn.ID, "played_samples": played, "basis": "i2s_dma", "valid": true, "interrupted": false, "state": "progress"}
}

func TestInputEchoOpeningDoesNotInterruptOrGenerate(t *testing.T) {
	r := newEchoAdmissionRig(t)
	old := echoOutput(t, r, "好的，我们从前有一颗星星讲起。", 19)
	if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
		t.Fatal(err)
	}
	r.start(t, "opening")
	r.preview(t, "opening", "text", "好")
	r.sync(t)
	r.s.mu.Lock()
	if old.turn.Cut || r.s.output != old {
		t.Error("matching opening preview cut the existing output")
	}
	r.s.mu.Unlock()
	r.stop(t, "opening")
	r.sync(t)
	r.s.mu.Lock()
	pending := r.s.lastInput
	pcm := pending.pcm
	r.s.mu.Unlock()
	r.final(t, "opening", "好的")
	r.sync(t)
	r.s.mu.Lock()
	if old.turn.Cut || r.s.output != old || len(r.s.history) != 1 || r.primary.Load() != 0 || r.selector.Load() != 0 {
		t.Error("matching opening final interrupted output or admitted another turn")
	}
	if r.s.lastInput != nil || !pending.retired || pending.pcm != nil {
		t.Error("rejected echo retained pending PCM")
	}
	r.s.mu.Unlock()
	for _, b := range pcm {
		if b != 0 {
			t.Error("rejected echo PCM was not cleared")
			break
		}
	}
	if ok, err := r.s.binary(old, make([]byte, 2880)); err != nil || !ok {
		t.Errorf("existing output cannot continue: %v", err)
	}
	// A duplicate final cannot admit the rejected item later.
	r.final(t, "opening", json.Number("42"))
	r.sync(t)
}

func assertEchoOutputAlive(t *testing.T, r *admissionRig, old *liveTurn) {
	t.Helper()
	r.sync(t)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if old.turn.Cut || r.s.output != old || len(r.s.history) != 1 || r.primary.Load() != 0 || r.selector.Load() != 0 {
		t.Fatal("suspected echo changed existing output or admitted a turn")
	}
}

func assertEchoPreviewCuts(t *testing.T, r *admissionRig, old *liveTurn) {
	t.Helper()
	r.sync(t)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if !old.turn.Cut || r.s.output != nil || r.s.lastInput == nil || !r.s.lastInput.hadPreview {
		t.Fatal("new content or absent echo evidence did not interrupt immediately")
	}
}

func TestInputEchoNormalizedOpeningAndFinalOnly(t *testing.T) {
	for _, tc := range []struct{ name, opening, preview, final string }{
		{"punctuation", "好，的！我们接着讲。", "好", " 好\t的。 "},
		{"case", "HELLO, from the moon.", "he", " Hello! "},
		{"eight_runes", "一二三四五六七八九十", "一二三四五六七八", "一二三四五六七八"},
		{"final_only", "星星正在升起。", "", "星星"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, tc.opening, 19)
			if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "echo")
			if tc.preview != "" {
				r.preview(t, "echo", "stash", tc.preview)
			}
			r.stop(t, "echo")
			r.final(t, "echo", tc.final)
			assertEchoOutputAlive(t, r, old)
		})
	}
}

func TestInputEchoPreviewUsesTextAndStashTogether(t *testing.T) {
	for _, tc := range []struct {
		name, text, stash string
		held              bool
	}{
		{"continuation", "好", "的", true},
		{"divergent_stash", "好的", "停止", false},
		{"divergent_text", "停止", "好的", false},
		{"combined_too_long", "一二三四五六七八", "九", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			opening := "好的，我们说个故事。"
			if tc.name == "combined_too_long" {
				opening = "一二三四五六七八九十"
			}
			old := echoOutput(t, r, opening, 19)
			if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "combined")
			r.event(t, "conversation.item.input_audio_transcription.delta", "combined", map[string]any{"text": tc.text, "stash": tc.stash})
			if tc.held {
				assertEchoOutputAlive(t, r, old)
				r.s.mu.Lock()
				if r.s.lastInput.hadPreview {
					t.Error("held preview was marked as confirmed interruption")
				}
				r.s.mu.Unlock()
			} else {
				assertEchoPreviewCuts(t, r, old)
			}
		})
	}
}

func TestInputEchoLaterNewContentInterruptsAndMatchingFinalIsAdmitted(t *testing.T) {
	r := newEchoAdmissionRig(t)
	old := echoOutput(t, r, "好的，我们来说故事。", 19)
	if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
		t.Fatal(err)
	}
	r.start(t, "new-content")
	r.preview(t, "new-content", "text", "好")
	assertEchoOutputAlive(t, r, old)
	r.preview(t, "new-content", "stash", "停止")
	assertEchoPreviewCuts(t, r, old)
	// The hardware completes the drain, then a retracted final is still admitted.
	drained := echoReceipt(old, 27360)
	drained["state"] = "finished"
	if err := r.s.receiveReceipt(drained); err != nil {
		t.Fatal(err)
	}
	r.stop(t, "new-content")
	r.final(t, "new-content", "好的")
	r.expectGenerated(t)
	r.sync(t)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if len(r.s.history) != 2 || r.s.output == nil || r.s.output.turn.User != "好的" {
		t.Fatal("matching final after genuine interruption was suppressed")
	}
}

func TestInputEchoUnavailableEvidenceFallsBack(t *testing.T) {
	for _, name := range []string{"no_progress", "stale_progress", "no_reference", "not_prefix", "nine_runes", "media", "cut", "final_playback", "ended_output", "wrong_basis", "invalid_device", "wrong_client", "owner_error", "mac"} {
		t.Run(name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "一二三四五六七八九十。", 19)
			if name != "no_progress" {
				if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
					t.Fatal(err)
				}
			}
			r.s.mu.Lock()
			text := "一二"
			switch name {
			case "stale_progress":
				old.lastPlaybackProgress = time.Now().Add(-time.Second)
			case "no_reference":
				old.turn.Generated = ""
			case "not_prefix":
				text = "停止"
			case "nine_runes":
				text = "一二三四五六七八九"
			case "media":
				old.media = true
			case "cut":
				old.turn.Cut = true
			case "final_playback":
				old.turn.PlaybackFinal = true
			case "ended_output":
				old.turn.Completed = old.turn.Sent
				close(old.done)
			case "wrong_basis":
				old.turn.PlaybackBasis = "CoreAudio_dataPlayedBack"
			case "invalid_device":
				r.s.rec.owner.DeviceID = "invalid"
			case "wrong_client":
				r.s.rec.owner.ClientType = "mac_fixture"
			case "owner_error":
				r.s.ownerErr = errors.New("invalid_session_manifest")
			case "mac":
				r.s.wire = &macWire{ws: r.s.ws}
			}
			r.s.mu.Unlock()
			r.start(t, "fallback")
			r.preview(t, "fallback", "text", text)
			r.sync(t)
			r.s.mu.Lock()
			defer r.s.mu.Unlock()
			if r.s.lastInput == nil || !r.s.lastInput.hadPreview || r.s.output != nil {
				t.Fatal("missing or inapplicable evidence did not use normal preview interruption")
			}
		})
	}
}

func TestInputEchoPlaybackAndPCMBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name             string
		completed, endMS int64
		held             bool
	}{
		{"inclusive_playback_and_pcm", 36000, 1000, true},
		{"over_playback", 37440, 620, false},
		{"over_pcm", 12960, 1001, false},
		{"empty_pcm", 12960, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "星星出来了。", 30)
			if err := r.s.receiveReceipt(echoReceipt(old, tc.completed)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "boundary")
			r.event(t, "input_audio_buffer.speech_stopped", "boundary", map[string]any{"audio_end_ms": tc.endMS})
			r.sync(t)
			if tc.endMS == 0 {
				r.s.mu.Lock()
				clear(r.s.lastInput.pcm)
				r.s.lastInput.pcm = nil
				r.s.mu.Unlock()
			}
			r.final(t, "boundary", "星星")
			r.sync(t)
			if tc.held {
				assertEchoOutputAlive(t, r, old)
			} else {
				r.s.mu.Lock()
				if !old.turn.Cut || len(r.s.history) != 2 {
					t.Error("out-of-bound final was suppressed")
				}
				r.s.mu.Unlock()
			}
		})
	}
}

func TestInputEchoProgressTimestampOnlyAdvancesWithValidatedCompleted(t *testing.T) {
	r := newEchoAdmissionRig(t)
	old := echoOutput(t, r, "月亮升起来。", 19)
	if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
		t.Fatal(err)
	}
	r.s.mu.Lock()
	if old.lastPlaybackProgress.IsZero() {
		t.Error("valid advancing receipt did not record progress")
	}
	stale := time.Now().Add(-time.Second)
	old.lastPlaybackProgress = stale
	r.s.mu.Unlock()
	for _, raw := range []int64{12960, 12961} {
		if err := r.s.receiveReceipt(echoReceipt(old, raw)); err != nil {
			t.Fatal(err)
		}
	}
	invalid := echoReceipt(old, 14400)
	invalid["valid"] = false
	if err := r.s.receiveReceipt(invalid); err == nil {
		t.Fatal("invalid receipt accepted")
	}
	r.s.mu.Lock()
	if !old.lastPlaybackProgress.Equal(stale) {
		t.Error("duplicate, unquantized or invalid receipt refreshed progress")
	}
	r.s.mu.Unlock()
	r.start(t, "stale")
	r.preview(t, "stale", "text", "月亮")
	assertEchoPreviewCuts(t, r, old)
}

func TestInputEchoProgressFreshnessBoundary(t *testing.T) {
	r := newEchoAdmissionRig(t)
	old := echoOutput(t, r, "月亮升起来。", 19)
	if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
		t.Fatal(err)
	}
	r.start(t, "freshness")
	r.sync(t)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	now := time.Now()
	for _, tc := range []struct {
		age  time.Duration
		held bool
	}{{0, true}, {500 * time.Millisecond, true}, {500*time.Millisecond + time.Nanosecond, false}, {-time.Nanosecond, false}} {
		old.lastPlaybackProgress = now.Add(-tc.age)
		if got := r.s.suspectedOpeningEchoLocked(r.s.lastInput, "月亮", now); got != tc.held {
			t.Errorf("progress age %v: echo=%v, want %v", tc.age, got, tc.held)
		}
	}
}

func TestInputEchoEvidenceIsSessionLocal(t *testing.T) {
	a := newEchoAdmissionRig(t)
	old := echoOutput(t, a, "小猫出门啦。", 19)
	if err := a.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
		t.Fatal(err)
	}
	b := newAdmissionRig(t, func(s *Session) {
		s.rec.owner = Ownership{DeviceID: memoryDeviceB, ClientType: "xiaozhi"}
	})
	other := echoOutput(t, b, "小猫出门啦。", 19)
	b.start(t, "other-session")
	b.preview(t, "other-session", "text", "小猫")
	assertEchoPreviewCuts(t, b, other)
	assertEchoOutputAlive(t, a, old)
}

func TestInputEchoFinalRechecksProgressAndOutput(t *testing.T) {
	for _, name := range []string{"stale", "replaced", "finished", "absent_output_at_start"} {
		t.Run(name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			var old *liveTurn
			if name == "absent_output_at_start" {
				r.start(t, "late")
				r.sync(t)
			}
			old = echoOutput(t, r, "星星亮起来。", 19)
			if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
				t.Fatal(err)
			}
			if name != "absent_output_at_start" {
				r.start(t, "late")
				r.preview(t, "late", "text", "星")
				assertEchoOutputAlive(t, r, old)
			}
			r.stop(t, "late")
			r.sync(t)
			if name == "replaced" {
				old = echoOutput(t, r, "星星亮起来。", 19)
				if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
					t.Fatal(err)
				}
			}
			r.s.mu.Lock()
			historyBefore := len(r.s.history)
			if name == "stale" {
				old.lastPlaybackProgress = time.Now().Add(-time.Second)
			}
			if name == "finished" {
				old.turn.PlaybackFinal = true
				old.turn.Completed = old.turn.Sent
			}
			r.s.mu.Unlock()
			r.final(t, "late", "星星")
			r.sync(t)
			r.s.mu.Lock()
			defer r.s.mu.Unlock()
			if len(r.s.history) != historyBefore+1 || r.s.output == old {
				t.Fatal("final reused expired or another output's echo evidence")
			}
		})
	}
}

func TestInputEchoNewCaptureLateEventsAndDeviceAbortAreIsolated(t *testing.T) {
	for _, name := range []string{"new_capture", "device_abort"} {
		t.Run(name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "小猫出门啦。", 19)
			if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "old-item")
			r.preview(t, "old-item", "text", "小")
			r.stop(t, "old-item")
			assertEchoOutputAlive(t, r, old)
			if name == "device_abort" {
				if err := r.s.interruptFrom(nil, "device_abort"); err != nil {
					t.Fatal(err)
				}
			}
			r.start(t, "new-item")
			r.final(t, "old-item", "停止")
			r.preview(t, "old-item", "text", "迟到")
			r.sync(t)
			r.s.mu.Lock()
			if len(r.s.history) != 1 || r.s.lastInput == nil || r.s.lastInput.item != "new-item" || r.s.lastInput.hadPreview {
				t.Error("late events affected new capture")
			}
			if (name == "device_abort") != old.turn.Cut {
				t.Error("device abort did not preserve independent interruption behavior")
			}
			r.s.mu.Unlock()
		})
	}
}

func TestInputEchoRejectsWithoutTextToolsOrMemorySideEffects(t *testing.T) {
	r := newAdmissionRig(t, func(s *Session) {
		// Configure a fresh persistent recorder before the listener starts. Both
		// deployed and current sources share these fields.
		s.rec = recorder{dir: s.directory, owner: Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi"}}
		s.memory = &sessionMemory{wake: make(chan struct{}, 8)}
	})
	old := echoOutput(t, r, "清除聊天记忆，我们继续。", 19)
	if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
		t.Fatal(err)
	}
	<-r.s.memory.wake // Consume the existing playback evidence notification.
	r.start(t, "memory-echo")
	r.stop(t, "memory-echo")
	r.final(t, "memory-echo", "清除聊天记忆")
	assertEchoOutputAlive(t, r, old)
	select {
	case <-r.s.memory.wake:
		t.Fatal("echo admission scheduled memory persistence")
	default:
	}
	b, err := os.ReadFile(filepath.Join(r.s.directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rejected := false
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e["event"] == "user_text" || e["event"] == "memory_cleared" || strings.Contains(string(line), "清除聊天记忆") {
			t.Fatal("echo text or operation entered records", line)
		}
		if e["event"] == "input_rejected" && e["reason"] == "suspected_opening_echo" {
			rejected = true
			if e["text"] != nil || e["transcript"] != nil || e["chars"] != float64(6) || e["samples"] != float64(9920) {
				t.Error("echo rejection must contain bounded counts without text", e)
			}
		}
	}
	if !rejected {
		t.Error("missing text-free echo rejection reason")
	}
}
