package gateway

import (
	"fmt"
	"testing"
)

func sealedDeviceBlock(t *testing.T, h *toolHarness, p *toolPeer, id, request string) map[string]any {
	t.Helper()
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": id + "-input"})
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": id + "-input", "transcript": request})
	wantSTT(t, h.device)
	toolParent(t, p, id, 3)
	start := readStart(t, h.device)
	toolDone(t, p, id)
	stop := readStop(t, h.device)
	if stop["playback_id"] != start["playback_id"] || stop["reason"] != nil {
		t.Fatalf("normal seal identity: %v", stop)
	}
	return start
}
func deviceDrainReceipt(t *testing.T, h *toolHarness, start map[string]any, played int, valid, interrupted bool) {
	t.Helper()
	h.device.send(t, map[string]any{"type": "playback", "state": "finished", "session_id": start["session_id"], "playback_id": start["playback_id"], "sample_rate": 24000, "played_samples": played, "valid": valid, "basis": "i2s_dma", "interrupted": interrupted})
}
func deviceModeBarrier(t *testing.T, h *toolHarness, p *toolPeer, mode string) {
	t.Helper()
	// Same device socket order plus the emitted upstream mode update establishes
	// a causal boundary before subsequent model events; no timing sleep is used.
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": mode})
	p.want(t, "session.update")
}
func interruptSealedBlock(t *testing.T, h *toolHarness, p *toolPeer, trigger string) map[string]any {
	t.Helper()
	if trigger == "abort" {
		h.device.send(t, map[string]any{"type": "abort"})
	} else {
		p.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
	}
	return readStop(t, h.device)
}
func TestSealedDeviceDrainInterruptionPreservesTaskAndStopIdentity(t *testing.T) {
	for _, trigger := range []string{"abort", "speech"} {
		for _, when := range []string{"no-final", "final-before", "final-after", "invalid-progress", "invalid-final-before"} {
			t.Run(trigger+"/"+when, func(t *testing.T) {
				h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
				p, _ := h.model(t)
				start := sealedDeviceBlock(t, h, p, "root", "原来的完整任务")
				if when == "final-before" {
					deviceDrainReceipt(t, h, start, 1440, true, true)
					deviceModeBarrier(t, h, p, "manual")
				}
				if when == "invalid-final-before" {
					deviceDrainReceipt(t, h, start, 0, false, true)
					deviceModeBarrier(t, h, p, "manual")
				}
				if when == "invalid-progress" {
					receipt(h.device, t, start, "progress", 0, false)
					deviceModeBarrier(t, h, p, "manual")
				}
				stop := interruptSealedBlock(t, h, p, trigger)
				if stop["playback_id"] != start["playback_id"] || stop["reason"] != "interrupted" {
					t.Fatalf("missing matched drain stop: %v", stop)
				}
				fresh, _ := h.model(t)
				if when == "final-after" {
					deviceDrainReceipt(t, h, start, 1440, true, true)
					deviceModeBarrier(t, h, fresh, "manual")
				}
				out := localResult(t, fresh, "query", "conversation_get_paused_task", "{}")
				if out["available"] != true || out["original_request"] != "原来的完整任务" || out["playback_id"] != start["playback_id"] || out["sent_samples"] != float64(4320) || out["text_position"] != "unknown" {
					t.Fatal(out)
				}
				if when == "final-before" || when == "final-after" {
					if out["settled"] != true || out["played_samples"] != float64(1440) {
						t.Fatal(out)
					}
				} else if when == "invalid-final-before" {
					if out["settled"] != true || out["position_known"] != false || out["played_samples"] != nil {
						t.Fatal(out)
					}
				} else if out["settled"] != false || out["position_known"] != false {
					t.Fatal(out)
				}
			})
		}
	}
}
func TestNormallySettledDeviceBlockIsNotPausedAgain(t *testing.T) {
	for _, trigger := range []string{"abort", "speech"} {
		t.Run(trigger, func(t *testing.T) {
			h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
			p, _ := h.model(t)
			start := sealedDeviceBlock(t, h, p, "complete", "已经完整结束的任务")
			deviceDrainReceipt(t, h, start, 4320, true, false)
			deviceModeBarrier(t, h, p, "manual")
			if trigger == "abort" {
				stop := interruptSealedBlock(t, h, p, "abort")
				if stop["playback_id"] != nil {
					t.Fatalf("settled block resurrected in stop: %v", stop)
				}
				p, _ = h.model(t)
			} else {
				p.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
				p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "新问题"})
				h.device.want(t, "stt") // A mistaken stop/reconnect fails before this echo.
			}
			out := localResult(t, p, "query", "conversation_get_paused_task", "{}")
			if out["available"] != false {
				t.Fatal(out)
			}
		})
	}
}
func TestSealedSideDrainKeepsOriginalPauseAndDoesNotCrossPlaybackIDs(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	root := sealedDeviceBlock(t, h, p, "root", "需要保留的原任务")
	stop := interruptSealedBlock(t, h, p, "abort")
	if stop["playback_id"] != root["playback_id"] {
		t.Fatal(stop)
	}
	fresh, _ := h.model(t)
	side := sealedDeviceBlock(t, h, fresh, "side", "旁支问题")
	if side["playback_id"] == root["playback_id"] {
		t.Fatal("playback identity reused")
	}
	// An old root final before current side interruption cannot retire/select side.
	deviceDrainReceipt(t, h, root, 1440, true, true)
	stop = interruptSealedBlock(t, h, fresh, "abort")
	if stop["playback_id"] != side["playback_id"] {
		t.Fatalf("old final redirected side stop: %v", stop)
	}
	next, _ := h.model(t)
	deviceDrainReceipt(t, h, side, 2880, true, true)
	deviceModeBarrier(t, h, next, "manual")
	out := localResult(t, next, "query", "conversation_get_paused_task", "{}")
	if out["original_request"] != "需要保留的原任务" || out["playback_id"] != root["playback_id"] || out["played_samples"] != float64(1440) || out["settled"] != true {
		t.Fatal(out)
	}
}
func TestNewSealedDrainOwnsInterruptionAfterOldNormalFinal(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	old := sealedDeviceBlock(t, h, p, "old", "旧任务")
	current := sealedDeviceBlock(t, h, p, "new", "当前任务")
	deviceDrainReceipt(t, h, old, 4320, true, false)
	stop := interruptSealedBlock(t, h, p, "abort")
	if stop["playback_id"] != current["playback_id"] {
		t.Fatal(stop)
	}
	fresh, _ := h.model(t)
	out := localResult(t, fresh, "query", "conversation_get_paused_task", "{}")
	if out["original_request"] != "当前任务" || out["playback_id"] != current["playback_id"] {
		t.Fatal(out)
	}
}
func TestTaskMutationRetiresOldInterruptedDrainOwnership(t *testing.T) {
	for _, action := range []string{"discard", "replace"} {
		t.Run(action, func(t *testing.T) {
			h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
			p, _ := h.model(t)
			root := sealedDeviceBlock(t, h, p, "root", "旧原任务")
			interruptSealedBlock(t, h, p, "abort")
			fresh, _ := h.model(t)
			// This side block has locally finished interrupted; its abort control has
			// not arrived. Mutating the paused task must retire this pending old owner.
			side := sealedDeviceBlock(t, h, fresh, "side", "切换到的实际新请求")
			deviceDrainReceipt(t, h, side, 1440, true, true)
			deviceModeBarrier(t, h, fresh, "manual")
			got := localResult(t, fresh, "get", "conversation_get_paused_task", "{}")
			if got["playback_id"] != root["playback_id"] {
				t.Fatal(got)
			}
			changed := localResult(t, fresh, "mutate", "conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":%q}`, got["task_id"], action))
			if changed["ok"] != true {
				t.Fatal(changed)
			}
			// The mutator's pending tool continuation makes speech an applicable
			// upstream interruption. It must not select the discarded device owner.
			stop := interruptSealedBlock(t, h, fresh, "speech")
			if stop["playback_id"] != nil {
				t.Fatalf("task mutation left interrupted old owner selectable: %v", stop)
			}
			next, _ := h.model(t)
			out := localResult(t, next, "check", "conversation_get_paused_task", "{}")
			if action == "discard" {
				if out["available"] != false {
					t.Fatal(out)
				}
			} else if out["task_id"] != changed["task_id"] || out["original_request"] != "切换到的实际新请求" {
				t.Fatal(out)
			}
		})
	}
}

func TestUnsettledTaskMutationDoesNotResurrectOnLateFinalOrSpeech(t *testing.T) {
	for _, action := range []string{"discard", "replace"} {
		t.Run(action, func(t *testing.T) {
			h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
			p, _ := h.model(t)
			root := sealedDeviceBlock(t, h, p, "root", "尚待DMA最终回报的旧任务")
			initialStop := interruptSealedBlock(t, h, p, "abort")
			if initialStop["playback_id"] != root["playback_id"] {
				t.Fatal(initialStop)
			}
			fresh, _ := h.model(t)
			old := localResult(t, fresh, "get", "conversation_get_paused_task", "{}")
			if old["settled"] != false {
				t.Fatal(old)
			}
			fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "当前实际替换请求"})
			h.device.want(t, "stt")
			changed := localResult(t, fresh, "mutate", "conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":%q}`, old["task_id"], action))
			if changed["ok"] != true {
				t.Fatal(changed)
			}
			deviceDrainReceipt(t, h, root, 1440, true, true)
			// This ordered device abort is also an acknowledgement that the late final
			// has been handled. It must not restore the already-consumed old owner.
			stop := interruptSealedBlock(t, h, fresh, "abort")
			if stop["playback_id"] != nil {
				t.Fatalf("late final revived discarded owner: %v", stop)
			}
			next, _ := h.model(t)
			next.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
			next.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "新的语音"})
			h.device.want(t, "stt")
			out := localResult(t, next, "check", "conversation_get_paused_task", "{}")
			if action == "discard" {
				if out["available"] != false {
					t.Fatal(out)
				}
			} else if out["task_id"] != changed["task_id"] || out["original_request"] != "当前实际替换请求" || out["position_known"] != false {
				t.Fatal(out)
			}
		})
	}
}
