package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
	"fmt"
	"strings"
	"testing"
	"time"
)

func routingHarness(t *testing.T) (*session, *toolPeer) {
	t.Helper()
	h := newPlaybackHarness(t)
	s := h.s
	s.responseID = ""
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "continue", known: true, text: "继续"}
	s.paused = &pausedTask{id: "task-1", original: requestOrigin{known: true, text: "数到一百"}, playback: playbackRecord{id: 1, settled: true}}
	s.snapshotRevision = 3
	return s, attachSnapshotModel(t, s)
}
func startRouting(t *testing.T, s *session, p *toolPeer) {
	t.Helper()
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	p.read(t)
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "route"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.encoder.Close)
}
func TestTaskRoutingContentFailsClosedBeforeMutationOrContinuation(t *testing.T) {
	contents := []struct{ name, payload string }{
		{"audio", `{"type":"response.audio.delta","response_id":"route","delta":"` + base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes)) + `"}`},
		{"transcript_delta", `{"type":"response.audio_transcript.delta","response_id":"route","delta":"unheard"}`},
		{"transcript_done_only", `{"type":"response.audio_transcript.done","response_id":"route","transcript":"unheard"}`},
		{"text_delta", `{"type":"response.text.delta","response_id":"route","delta":"unheard"}`},
		{"text_done_only", `{"type":"response.text.done","response_id":"route","text":"unheard"}`},
	}
	for _, content := range contents {
		for _, order := range []string{"content_only", "content_first", "function_first"} {
			for _, action := range []string{"resume", "discard", "replace", "side"} {
				t.Run(content.name+"/"+order+"/"+action, func(t *testing.T) {
					s, p := routingHarness(t)
					startRouting(t, s, p)
					original, revision := *s.paused, s.snapshotRevision
					function := qwen.Event{Type: "response.function_call_arguments.done", ResponseID: "route", CallID: "route-call", Name: taskRouteTool, Arguments: `{"action":"` + action + `"}`}
					if order == "function_first" {
						if err := s.onModel(context.Background(), function); err != nil {
							t.Fatal(err)
						}
					}
					var ev qwen.Event
					if err := json.Unmarshal([]byte(content.payload), &ev); err != nil {
						t.Fatal(err)
					}
					if err := s.onModel(context.Background(), ev); err == nil {
						t.Error("nonempty routing content did not immediately fail closed")
					}
					// Even a caller ignoring the content error must never apply this
					// decision or retry on the contaminated upstream conversation.
					if order == "content_first" {
						_ = s.onModel(context.Background(), function)
					}
					if err := s.onModel(context.Background(), qwen.Event{Type: "response.done", Response: qwen.Response{ID: "route", Status: "completed"}}); err == nil {
						t.Error("contaminated response completion did not fail closed")
					}
					if err := s.finishRouting(); err == nil {
						t.Error("direct routing completion bypassed contamination guard")
					}
					if s.paused == nil || *s.paused != original || s.snapshotRevision != revision || s.responseTaskID != "" || s.route.decided {
						t.Fatal("contaminated route changed task state or binding")
					}
					if err := s.play(); err != nil {
						t.Fatal(err)
					}
					if s.pending.Len() != 0 || s.transcript != "" || s.draftTranscript != "" || s.started || s.activePlayback != nil || len(s.playbacks) != 0 {
						t.Fatal("contaminated route admitted content or playback")
					}
					p.quiet(t, time.Millisecond)
				})
			}
		}
	}
}

func TestTaskRoutingEmptyContentEventsRemainHarmless(t *testing.T) {
	s, p := routingHarness(t)
	startRouting(t, s, p)
	for _, typ := range []string{"response.audio.delta", "response.audio_transcript.delta", "response.audio_transcript.done", "response.text.delta", "response.text.done"} {
		if err := s.onModel(context.Background(), qwen.Event{Type: typ, ResponseID: "route"}); err != nil {
			t.Fatalf("empty %s: %v", typ, err)
		}
	}
	routeDecision(t, s, "resume")
	if s.responseTaskID != "task-1" || !s.route.decided {
		t.Fatal("empty content events blocked clean route")
	}
	p.want(t, "conversation.item.create")
	cfg := p.read(t).body["session"].(map[string]any)
	if cfg["tool_choice"] != "auto" || cfg["modalities"] != nil {
		t.Fatalf("clean route did not restore stable voice controls: %v", cfg)
	}
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
}
func TestTaskRoutingStaleDecisionRetriesWithoutBinding(t *testing.T) {
	s, p := routingHarness(t)
	s.playbackEnabled = true
	s.paused.playback = playbackRecord{id: 1, sent: 1440, sealed: true, interrupted: true, timedOut: true}
	startRouting(t, s, p)
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.function_call_arguments.done", ResponseID: "route", CallID: "route-call", Name: "conversation_route_paused_task", Arguments: `{"action":"resume"}`}); err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma", "interrupted": true})
	s.receivePlayback(receipt) // a real parsed late final revises captured state
	if s.snapshotRevision != 4 {
		t.Fatal("late final did not revise snapshot")
	}
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.done", Response: qwen.Response{ID: "route", Status: "completed"}}); err != nil {
		t.Fatal(err)
	}
	if s.responseTaskID != "" {
		t.Fatal("stale routing decision bound the response")
	}
	if err := s.play(); err != nil {
		t.Fatal(err)
	}
	if err := s.continueTools(); err != nil {
		t.Fatal(err)
	}
	p.want(t, "conversation.item.create")
	update := p.read(t).body
	if update["type"] != "session.update" || update["session"].(map[string]any)["tool_choice"] != "required" {
		t.Fatalf("stale routing did not retry required tool: %v", update)
	}
}

func routeDecision(t *testing.T, s *session, action string) {
	t.Helper()
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.function_call_arguments.done", ResponseID: s.responseID, CallID: "decision-" + s.responseID, Name: taskRouteTool, Arguments: `{"action":"` + action + `"}`}); err != nil {
		t.Fatal(err)
	}
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.done", Response: qwen.Response{ID: s.responseID, Status: "completed"}}); err != nil {
		t.Fatal(err)
	}
}
func TestTaskRoutingWireControlsAndResumeBindingThroughTools(t *testing.T) {
	s, p := routingHarness(t)
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	cfg := p.read(t).body["session"].(map[string]any)
	if cfg["tool_choice"] != "required" || len(cfg["tools"].([]any)) != 1 || cfg["modalities"] != nil {
		t.Fatalf("routing controls: %v", cfg)
	}
	p.quiet(t, 10*time.Millisecond)
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "route"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.encoder.Close)
	routeDecision(t, s, "resume")
	if s.responseTaskID != "task-1" {
		t.Fatal("route did not apply gateway-owned identity")
	}
	p.want(t, "conversation.item.create")
	cfg = p.read(t).body["session"].(map[string]any)
	if cfg["tool_choice"] != "auto" || cfg["modalities"] != nil {
		t.Fatalf("voice controls not restored: %v", cfg)
	}
	for _, tool := range cfg["tools"].([]any) {
		if tool.(map[string]any)["function"].(map[string]any)["name"] == "conversation_update_paused_task" {
			t.Fatal("voice can redo routing")
		}
	}
	p.quiet(t, 10*time.Millisecond)
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "voice"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.encoder.Close)
	if err := s.beginPlayback(); err != nil {
		t.Fatal(err)
	}
	if s.activePlayback.taskID != "task-1" {
		t.Fatal("voice reset binding")
	}
	// Device tools continue the same already-decided user turn.
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.function_call_arguments.done", ResponseID: "voice", CallID: "device", Name: "device_get_status", Arguments: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.done", Response: qwen.Response{ID: "voice", Status: "completed"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.play(); err != nil {
		t.Fatal(err)
	}
	if err := s.continueTools(); err != nil {
		t.Fatal(err)
	}
	p.want(t, "conversation.item.create")
	cfg = p.read(t).body["session"].(map[string]any)
	if cfg["tool_choice"] != "auto" {
		t.Fatal("device continuation rerouted")
	}
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "voice-device"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.encoder.Close)
	if s.responseTaskID != "task-1" {
		t.Fatal("device continuation lost binding")
	}
}
func TestTaskRoutingSideDiscardReplace(t *testing.T) {
	for _, action := range []string{"side", "discard", "replace"} {
		t.Run(action, func(t *testing.T) {
			s, p := routingHarness(t)
			startRouting(t, s, p)
			routeDecision(t, s, action)
			switch action {
			case "side":
				if s.paused.id != "task-1" || s.responseTaskID != "" {
					t.Fatal("side stole task")
				}
			case "discard":
				if s.paused != nil || s.responseTaskID != "" {
					t.Fatal("discard retained task")
				}
			case "replace":
				if s.paused.original != s.currentInput || s.paused.playback.id != 0 || s.responseTaskID != s.paused.id {
					t.Fatal("replacement inherited old playback")
				}
			}
		})
	}
}
func TestTaskRoutingRejectedAndMissingRetryBound(t *testing.T) {
	for _, args := range []string{"", `{"action":"bogus"}`, `{"action":"resume","task_id":"model-owned"}`} {
		t.Run(args, func(t *testing.T) {
			s, p := routingHarness(t)
			startRouting(t, s, p)
			for attempt := 1; attempt <= maxTaskRouteAttempts; attempt++ {
				if args != "" {
					if err := s.onModel(context.Background(), qwen.Event{Type: "response.function_call_arguments.done", ResponseID: s.responseID, CallID: fmt.Sprintf("call-%d", attempt), Name: taskRouteTool, Arguments: args}); err != nil {
						t.Fatal(err)
					}
				}
				err := s.onModel(context.Background(), qwen.Event{Type: "response.done", Response: qwen.Response{ID: s.responseID, Status: "completed"}})
				if attempt == maxTaskRouteAttempts {
					if err == nil {
						t.Fatal("unbounded retry")
					}
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if args != "" {
					p.want(t, "conversation.item.create")
				}
				cfg := p.read(t).body["session"].(map[string]any)
				if cfg["tool_choice"] != "required" {
					t.Fatal("rejection released voice")
				}
				ackSnapshotWire(t, s, p)
				p.want(t, "response.create")
				if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: fmt.Sprintf("route-%d", attempt)}}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(s.encoder.Close)
			}
			if s.responseTaskID != "" || s.pending.Len() != 0 {
				t.Fatal("rejected route bound or played")
			}
		})
	}
}
func TestTaskRoutingUnsolicitedResponseAndNewInputFailClosed(t *testing.T) {
	s, p := routingHarness(t)
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	p.read(t)
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "early"}}); err == nil {
		t.Fatal("route bypassed snapshot ACK")
	}
	s.inputCommitted = true
	if err := s.onModel(context.Background(), qwen.Event{Type: "input_audio_buffer.committed", ItemID: "new-input"}); err == nil {
		t.Fatal("new input reused old route")
	}
}

// Full fake-provider route exchange, used when legacy playback fixtures now
// commit a fresh user input while a task exists.
func completeRouteWire(t *testing.T, p *toolPeer, id, action string) {
	t.Helper()
	cfg := p.read(t).body
	if cfg["type"] != "session.update" || cfg["session"].(map[string]any)["tool_choice"] != "required" {
		t.Fatalf("expected required route update: %v", cfg)
	}
	p.send(t, map[string]any{"type": "session.updated"})
	p.want(t, "response.create")
	p.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
	p.send(t, map[string]any{"type": "response.function_call_arguments.done", "response_id": id, "call_id": "call-" + id, "name": taskRouteTool, "arguments": `{"action":"` + action + `"}`})
	toolDone(t, p, id)
	output := p.want(t, "conversation.item.create")
	if !strings.Contains(output["item"].(map[string]any)["output"].(string), `"ok":true`) {
		t.Fatalf("route failed: %v", output)
	}
	cfg = p.read(t).body
	if cfg["type"] != "session.update" || cfg["session"].(map[string]any)["tool_choice"] != "auto" {
		t.Fatalf("expected restored voice update: %v", cfg)
	}
	p.send(t, map[string]any{"type": "session.updated"})
	p.want(t, "response.create")
}

func TestTaskRoutingRepeatedResumeInterruptionsKeepTask(t *testing.T) {
	s, p := routingHarness(t)
	original := s.paused.original
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("turn-%d", i)
		s.commitOrigin(id)
		s.transcribeOrigin(id, "继续")
		if err := s.requestResponse(); err != nil {
			t.Fatal(err)
		}
		p.read(t)
		ackSnapshotWire(t, s, p)
		p.want(t, "response.create")
		if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: id + "-route"}}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.encoder.Close)
		routeDecision(t, s, "resume")
		p.want(t, "conversation.item.create")
		p.read(t)
		ackSnapshotWire(t, s, p)
		p.want(t, "response.create")
		if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: id + "-voice"}}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.encoder.Close)
		if err := s.beginPlayback(); err != nil {
			t.Fatal(err)
		}
		s.started = true
		s.activePlayback.sent = 1440
		s.activePlayback.settled = true
		s.pausePlayback()
		if s.paused.id != "task-1" || s.paused.original != original || s.paused.playback.id != uint32(i+1) || s.paused.playback.origin.itemID != id {
			t.Fatalf("interruption %d lost task: %+v", i, s.paused)
		}
		s.responseID = ""
		s.started = false
		s.activePlayback = nil
		s.devicePlayback = nil
	}
}
func TestTaskRoutingAbortDropsRouteAndOldGeneration(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	p, _ := h.model(t)
	root := sealedDeviceBlock(t, h, p, "root", "原任务")
	interruptSealedBlock(t, h, p, "abort")
	fresh, _ := h.model(t)
	deviceDrainReceipt(t, h, root, 1440, true, true)
	fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "continue"})
	fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "continue", "transcript": "继续"})
	wantSTT(t, h.device)
	update := fresh.read(t).body
	if update["session"].(map[string]any)["tool_choice"] != "required" {
		t.Fatal(update)
	}
	fresh.send(t, map[string]any{"type": "session.updated"})
	fresh.want(t, "response.create")
	toolParent(t, fresh, "route-aborted", 0) // abort an unresolved, content-free route
	h.device.send(t, map[string]any{"type": "abort"})
	stop := readStop(t, h.device)
	if stop["playback_id"] != nil {
		t.Fatal("route audio opened a playback block")
	}
	_, cfg := h.model(t)
	if !strings.Contains(cfg["instructions"].(string), `"original_request":"原任务"`) {
		t.Fatal("abort lost original task")
	}
}

func TestTaskRoutingVoiceCannotBypassRestoreACK(t *testing.T) {
	s, p := routingHarness(t)
	startRouting(t, s, p)
	routeDecision(t, s, "resume")
	p.want(t, "conversation.item.create")
	p.read(t)
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "premature-voice"}}); err == nil {
		t.Fatal("voice bypassed restore/snapshot ACK")
	}
	if s.pending.Len() != 0 {
		t.Fatal("premature voice buffered audio")
	}
}
