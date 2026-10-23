package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/qwen"
)

func routingHarness(t *testing.T) (*session, *toolPeer) {
	t.Helper()
	h := newPlaybackHarness(t)
	s := h.s
	s.responseID = ""
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "continue", known: true, text: "继续"}
	s.inputCommitted = true
	s.paused = &pausedTask{id: "task-1", original: requestOrigin{known: true, text: "数到一百"}, playback: playbackRecord{id: 1, settled: true}}
	s.snapshotRevision = 3
	t.Cleanup(s.cancelRouting)
	return s, attachSnapshotModel(t, s)
}

type classificationRequest struct {
	ctx              context.Context
	original, latest string
	done             <-chan struct{}
	awaiting         bool
}

func controlledClassifier(s *session) (<-chan classificationRequest, chan<- taskRouteResult) {
	requests := make(chan classificationRequest, 4)
	responses := make(chan taskRouteResult, 4)
	s.classifyPausedTask = func(ctx context.Context, cfg qwen.Config, original, latest string, awaiting bool) (string, error) {
		done := make(chan struct{})
		defer close(done)
		requests <- classificationRequest{ctx, original, latest, done, awaiting}
		select {
		case result := <-responses:
			return result.action, result.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return requests, responses
}
func takeClassification(t *testing.T, requests <-chan classificationRequest) classificationRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("classifier not started")
		return classificationRequest{}
	}
}
func routeResult(t *testing.T, s *session) taskRouteResult {
	t.Helper()
	select {
	case result := <-s.routeResults:
		return result
	case <-time.After(time.Second):
		t.Fatal("classifier did not return")
		return taskRouteResult{}
	}
}
func TestIsolatedRouteWaitsForMatchingTranscriptWithoutMainMutation(t *testing.T) {
	s, p := routingHarness(t)
	requests, results := controlledClassifier(s)
	s.currentInput.known = false
	s.currentInput.text = ""
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	p.quiet(t, 20*time.Millisecond)
	s.transcribeOrigin("unrelated", "继续")
	if err := s.advanceGeneration(time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requests:
		t.Fatal("unrelated STT classified")
	default:
	}
	s.transcribeOrigin("continue", "继续")
	if err := s.advanceGeneration(time.Now()); err != nil {
		t.Fatal(err)
	}
	request := takeClassification(t, requests)
	if request.original != "数到一百" || request.latest != "继续" {
		t.Fatal(request)
	}
	p.quiet(t, time.Millisecond)
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "unsolicited"}}); err == nil {
		t.Fatal("voice bypassed classification")
	}
	results <- taskRouteResult{action: "resume"}
	if err := s.finishRouting(routeResult(t, s)); err != nil {
		t.Fatal(err)
	}
	if s.responseTaskID != "task-1" {
		t.Fatal("route did not bind")
	}
	cfg := p.read(t).body["session"].(map[string]any)
	if len(cfg) != 1 || cfg["instructions"] == nil || strings.Contains(cfg["instructions"].(string), "仅输出一个英文枚举值") {
		t.Fatalf("main config contaminated: %v", cfg)
	}
	p.quiet(t, time.Millisecond)
	if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "premature"}}); err == nil {
		t.Fatal("voice bypassed snapshot ACK")
	}
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
}
func TestIsolatedRouteRejectsUnavailableInputAndTranscriptDeadline(t *testing.T) {
	for _, kind := range []string{"anonymous", "truncated", "empty", "whitespace", "original-truncated", "original-missing", "wrong-generation"} {
		t.Run(kind, func(t *testing.T) {
			s, p := routingHarness(t)
			requests, _ := controlledClassifier(s)
			switch kind {
			case "anonymous":
				s.currentInput.itemID = ""
			case "truncated":
				s.currentInput.truncated = true
			case "empty":
				s.currentInput.known = false
				s.currentInput.text = ""
			case "whitespace":
				s.currentInput.text = " \n "
			case "original-truncated":
				s.paused.original.truncated = true
			case "original-missing":
				s.paused.original.known = false
			case "wrong-generation":
				s.currentInput.generation = 0
			}
			err := s.requestResponse()
			if kind == "empty" || kind == "original-missing" {
				if err != nil {
					t.Fatal(err)
				}
				err = s.advanceGeneration(time.Now().Add(taskTranscriptTimeout))
			}
			if err == nil {
				t.Fatal("unavailable routing input accepted")
			}
			select {
			case <-requests:
				t.Fatal("unavailable input classified")
			default:
			}
			p.quiet(t, time.Millisecond)
		})
	}
}
func TestIsolatedRouteStaleRevisionAndFailureShareThreeAttemptBudget(t *testing.T) {
	for _, failure := range []string{"provider", "enum", "revision"} {
		t.Run(failure, func(t *testing.T) {
			s, p := routingHarness(t)
			requests, results := controlledClassifier(s)
			if err := s.requestResponse(); err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= 3; i++ {
				takeClassification(t, requests)
				response := taskRouteResult{action: "resume"}
				switch failure {
				case "provider":
					response.err = errors.New("private provider details")
				case "enum":
					response.action = "resume explanation"
				case "revision":
					s.snapshotRevision++
				}
				results <- response
				err := s.finishRouting(routeResult(t, s))
				if i == 3 {
					if err == nil {
						t.Fatal("unbounded retry")
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if s.responseTaskID != "" || !s.route.active || s.route.decided {
					t.Fatal("failed route released voice")
				}
			}
			if s.route.attempts != 3 {
				t.Fatal(s.route.attempts)
			}
			p.quiet(t, time.Millisecond)
		})
	}
}
func TestIsolatedRouteLateFinalReclassifiesThenACKsCurrentSnapshot(t *testing.T) {
	s, p := routingHarness(t)
	requests, results := controlledClassifier(s)
	s.playbackEnabled = true
	s.paused.playback = playbackRecord{id: 1, sent: 1440, sealed: true, interrupted: true, timedOut: true}
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	takeClassification(t, requests)
	receipt, _ := json.Marshal(map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma", "interrupted": true})
	s.receivePlayback(receipt)
	if s.snapshotRevision != 4 {
		t.Fatal("late final did not revise snapshot")
	}
	results <- taskRouteResult{action: "restart"}
	if err := s.finishRouting(routeResult(t, s)); err != nil {
		t.Fatal(err)
	}
	if s.responseTaskID != "" {
		t.Fatal("stale route bound")
	}
	p.quiet(t, time.Millisecond)
	takeClassification(t, requests)
	results <- taskRouteResult{action: "restart"}
	if err := s.finishRouting(routeResult(t, s)); err != nil {
		t.Fatal(err)
	}
	cfg := p.read(t).body["session"].(map[string]any)
	if !strings.Contains(cfg["instructions"].(string), `"revision":5`) || !strings.Contains(cfg["instructions"].(string), `"played_samples":1440`) {
		t.Fatal(cfg)
	}
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
}
func TestIsolatedRouteCancellationAndObsoleteResultCannotBind(t *testing.T) {
	s, p := routingHarness(t)
	requests, _ := controlledClassifier(s)
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	request := takeClassification(t, requests)
	old := taskRouteResult{token: s.route.token, generation: 1, taskID: s.route.taskID, revision: s.route.revision, input: s.route.input, action: "resume"}
	s.commitOrigin("next") // new input cancels the old worker before resetting state
	select {
	case <-request.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("worker not canceled")
	}
	s.transcribeOrigin("next", "新问题")
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	takeClassification(t, requests)
	if err := s.finishRouting(old); err != nil {
		t.Fatal(err)
	}
	if s.responseTaskID != "" || !s.route.active || s.route.token == old.token {
		t.Fatal("obsolete result escaped token validation")
	}
	p.quiet(t, time.Millisecond)
}
func TestIsolatedRouteValidatesEveryCapturedIdentity(t *testing.T) {
	for _, field := range []string{"generation", "input", "task", "revision", "current-input", "current-task"} {
		t.Run(field, func(t *testing.T) {
			s, p := routingHarness(t)
			requests, results := controlledClassifier(s)
			s.requestResponse()
			takeClassification(t, requests)
			results <- taskRouteResult{action: "resume"}
			result := routeResult(t, s)
			switch field {
			case "generation":
				result.generation++
			case "input":
				result.input.itemID = "wrong"
			case "task":
				result.taskID = "wrong"
			case "revision":
				result.revision++
			case "current-input":
				s.currentInput.text = "changed"
			case "current-task":
				s.paused.id = "changed"
			}
			if err := s.finishRouting(result); err == nil {
				t.Fatal("identity mismatch accepted")
			}
			if s.responseTaskID != "" {
				t.Fatal("mismatch bound")
			}
			p.quiet(t, time.Millisecond)
		})
	}
}
func TestIsolatedRouteSideDiscardReplace(t *testing.T) {
	for _, action := range []string{"side", "discard", "replace"} {
		t.Run(action, func(t *testing.T) {
			s, p := routingHarness(t)
			requests, results := controlledClassifier(s)
			s.requestResponse()
			takeClassification(t, requests)
			results <- taskRouteResult{action: action}
			if err := s.finishRouting(routeResult(t, s)); err != nil {
				t.Fatal(err)
			}
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
					t.Fatal("replacement inherited task")
				}
			}
			cfg := p.read(t).body["session"].(map[string]any)
			if len(cfg) != 1 {
				t.Fatal("main controls changed")
			}
		})
	}
}
func TestIsolatedRouteRepeatedResumeAndDeviceContinuationKeepOriginal(t *testing.T) {
	s, p := routingHarness(t)
	requests, results := controlledClassifier(s)
	original := s.paused.original
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("turn-%d", i)
		s.commitOrigin(id)
		s.transcribeOrigin(id, "继续")
		s.requestResponse()
		takeClassification(t, requests)
		results <- taskRouteResult{action: "restart"}
		if err := s.finishRouting(routeResult(t, s)); err != nil {
			t.Fatal(err)
		}
		p.read(t)
		ackSnapshotWire(t, s, p)
		p.want(t, "response.create")
		if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: id}}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.encoder.Close)
		// A device-tool parent yields a new voice response without rerouting.
		if err := s.callTool(qwen.Event{ResponseID: id, CallID: id, Name: "device_get_status", Arguments: `{}`}); err != nil {
			t.Fatal(err)
		}
		s.responseID = ""
		s.toolParentDone = true
		if err := s.continueTools(); err != nil {
			t.Fatal(err)
		}
		p.want(t, "conversation.item.create")
		p.read(t)
		ackSnapshotWire(t, s, p)
		p.want(t, "response.create")
		if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: id + "-device"}}); err != nil {
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
			t.Fatalf("interruption lost original: %+v", s.paused)
		}
		s.responseID = ""
		s.started = false
		s.activePlayback = nil
		s.devicePlayback = nil
	}
}

// Existing playback scenarios use the actual independent WebSocket protocol,
// then verify that only a normal snapshot refresh reaches the voice connection.
func completeRouteWire(t *testing.T, p *toolPeer, id, action string) {
	t.Helper()
	classifier, cfg := p.harness.model(t)
	if len(cfg["modalities"].([]any)) != 1 || cfg["modalities"].([]any)[0] != "text" || cfg["input_audio_transcription"] != nil || len(cfg["tools"].([]any)) != 0 {
		t.Fatal("classifier was not isolated")
	}
	create := classifier.want(t, "response.create")
	if !strings.Contains(create["response"].(map[string]any)["instructions"].(string), "latest_user_text") {
		t.Fatal("request missing from response instructions")
	}
	classifier.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
	classifier.send(t, map[string]any{"type": "response.text.delta", "response_id": id, "delta": action})
	classifier.send(t, map[string]any{"type": "response.text.done", "response_id": id, "text": action})
	classifier.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
	update := p.read(t).body
	if update["type"] != "session.update" || len(update["session"].(map[string]any)) != 1 {
		t.Fatalf("voice config changed: %v", update)
	}
	p.send(t, map[string]any{"type": "session.updated"})
	p.want(t, "response.create")
}

func TestIsolatedRouteAbortDisconnectAndNewInputCloseClassifier(t *testing.T) {
	for _, trigger := range []string{"abort", "manual-abort", "disconnect", "new-input"} {
		t.Run(trigger, func(t *testing.T) {
			h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
			p, _ := h.model(t)
			root := sealedDeviceBlock(t, h, p, "root", "原任务")
			interruptSealedBlock(t, h, p, "abort")
			fresh, _ := h.model(t)
			deviceDrainReceipt(t, h, root, 1440, true, true)
			if trigger == "manual-abort" {
				h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "manual"})
				fresh.want(t, "session.update")
			}
			fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "continue"})
			fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "continue", "transcript": "继续"})
			wantSTT(t, h.device)
			classifier, cfg := h.model(t)
			if len(cfg["modalities"].([]any)) != 1 {
				t.Fatal("not a text classifier")
			}
			classifier.want(t, "response.create") // leave worker waiting for a result
			switch trigger {
			case "disconnect":
				h.device.c.Close()
			case "new-input":
				fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "new-input"})
				fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "new-input", "transcript": "一加一是多少"})
				wantSTT(t, h.device)
			default:
				h.device.send(t, map[string]any{"type": "abort"})
				stop := readStop(t, h.device)
				if stop["playback_id"] != nil {
					t.Fatal("classifier opened playback")
				}
				_, cfg = h.model(t)
				if !strings.Contains(cfg["instructions"].(string), `"original_request":"原任务"`) {
					t.Fatal("abort lost task")
				}
			}
			select {
			case event := <-classifier.in:
				if event.err == nil {
					t.Fatal("classifier wrote after cancellation")
				}
			case <-time.After(time.Second):
				t.Fatal("classifier socket survived cancellation")
			}
			if trigger == "new-input" {
				completeRouteWire(t, fresh, "next-route", "side")
			}
		})
	}
}

func TestIsolatedRouteWorkerDeadlineCannotLeaveGenerationWaiting(t *testing.T) {
	s, p := routingHarness(t)
	requests, _ := controlledClassifier(s)
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		request := takeClassification(t, requests)
		err := s.advanceGeneration(time.Now().Add(snapshotACKTimeout + time.Second))
		if attempt == 3 {
			if err == nil {
				t.Fatal("worker timeout did not fail closed")
			}
		} else if err != nil {
			t.Fatal(err)
		}
		select {
		case <-request.ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("timed out worker retained context")
		}
	}
	if s.responseTaskID != "" || s.route.attempts != 3 {
		t.Fatal("timeout bound task or retried without limit")
	}
	p.quiet(t, time.Millisecond)
}

func TestIsolatedRouteMixedFailuresCannotResetAttemptBudget(t *testing.T) {
	s, p := routingHarness(t)
	requests, results := controlledClassifier(s)
	if err := s.requestResponse(); err != nil {
		t.Fatal(err)
	}
	failures := []taskRouteResult{{action: "resume explanation"}, {action: "resume"}, {err: errors.New("provider failed")}}
	for i, response := range failures {
		takeClassification(t, requests)
		if i == 1 {
			s.snapshotRevision++
		}
		results <- response
		err := s.finishRouting(routeResult(t, s))
		if i == 2 {
			if err == nil {
				t.Fatal("mixed failures reset budget")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if s.route.attempts != 3 || s.responseTaskID != "" {
		t.Fatal("mixed failures escaped bounds")
	}
	p.quiet(t, time.Millisecond)
}
func TestIsolatedRouteRejectsEveryMainOutputBeforeSnapshotACK(t *testing.T) {
	s, p := routingHarness(t)
	requests, results := controlledClassifier(s)
	s.requestResponse()
	takeClassification(t, requests)
	results <- taskRouteResult{action: "resume"}
	if err := s.finishRouting(routeResult(t, s)); err != nil {
		t.Fatal(err)
	}
	p.read(t)
	for _, typ := range []string{"response.created", "response.text.delta", "response.text.done", "response.audio.delta", "response.audio_transcript.done", "response.function_call_arguments.done", "response.done"} {
		if err := s.onModel(context.Background(), qwen.Event{Type: typ, ResponseID: "premature", Delta: "unexpected", Response: qwen.Response{ID: "premature", Status: "completed"}}); err == nil {
			t.Fatalf("%s bypassed main ACK", typ)
		}
	}
	if s.responseID != "" || s.pending.Len() != 0 || s.started || s.activePlayback != nil {
		t.Fatal("premature response admitted content")
	}
	p.quiet(t, time.Millisecond)
}

// An accepted route may still have an unacknowledged snapshot when the provider
// commits another input. Its ACK belongs to that old input, even if the serialized
// instructions remain identical across turns.
func TestIsolatedRouteSnapshotACKCannotReleaseReplacementInput(t *testing.T) {
	for _, schedule := range []string{"before-transcript", "during-classification", "after-new-snapshot", "after-canceled-replacement"} {
		t.Run(schedule, func(t *testing.T) {
			s, p := routingHarness(t)
			requests, results := controlledClassifier(s)
			original := s.paused.original
			initialAction := "restart"
			if schedule == "after-new-snapshot" || schedule == "after-canceled-replacement" {
				initialAction = "side"
			}
			if err := s.requestResponse(); err != nil {
				t.Fatal(err)
			}
			takeClassification(t, requests)
			results <- taskRouteResult{action: initialAction}
			if err := s.finishRouting(routeResult(t, s)); err != nil {
				t.Fatal(err)
			}
			oldUpdate := p.read(t).body
			if oldUpdate["type"] != "session.update" {
				t.Fatal(oldUpdate)
			}
			oldInstructions := oldUpdate["session"].(map[string]any)["instructions"]
			oldToken := s.sync.token
			if !s.route.decided || oldToken == 0 {
				t.Fatal("fixture needs accepted route awaiting snapshot ACK")
			}
			commit := func(id string) {
				t.Helper()
				if err := s.onModel(context.Background(), qwen.Event{Type: "input_audio_buffer.committed", ItemID: id}); err != nil {
					t.Fatal(err)
				}
				if !s.route.active || s.currentInput.known || s.responseTaskID != "" {
					t.Fatal("replacement did not clear accepted binding and await transcription")
				}
			}
			commit("replacement")
			if schedule == "after-canceled-replacement" {
				s.transcribeOrigin("replacement", "旁支问题")
				if err := s.advanceGeneration(time.Now()); err != nil {
					t.Fatal(err)
				}
				canceled := takeClassification(t, requests)
				commit("latest")
				select {
				case <-canceled.ctx.Done():
				case <-time.After(time.Second):
					t.Fatal("replacement classifier survived cancellation")
				}
				select {
				case <-canceled.done:
				case <-time.After(time.Second):
					t.Fatal("canceled classifier did not return")
				}
			}
			item := s.currentInput.itemID
			if schedule == "before-transcript" {
				ackSnapshotWire(t, s, p)
				p.quiet(t, 20*time.Millisecond)
				if s.voiceRequested || !s.route.active {
					t.Fatal("old ACK released untranscribed input")
				}
			}
			// Avoid modifying history: for the side/side case the instruction text is
			// deliberately unchanged, so only input-scoped ACK identity can gate voice.
			s.transcribeOrigin(item, "继续")
			if err := s.advanceGeneration(time.Now()); err != nil {
				t.Fatal(err)
			}
			request := takeClassification(t, requests)
			if request.original != original.text || request.latest != "继续" {
				t.Fatal("new classifier lost original goal or latest input")
			}
			if schedule == "during-classification" {
				ackSnapshotWire(t, s, p)
				p.quiet(t, 20*time.Millisecond)
				if s.voiceRequested || !s.route.active {
					t.Fatal("old ACK released unclassified input")
				}
			}
			action := "restart"
			if schedule == "after-new-snapshot" || schedule == "after-canceled-replacement" {
				action = "side"
			}
			results <- taskRouteResult{action: action}
			for s.route.active {
				if err := s.finishRouting(routeResult(t, s)); err != nil {
					t.Fatal(err)
				}
			}
			latestUpdate := p.read(t).body
			if latestUpdate["type"] != "session.update" {
				t.Fatal(latestUpdate)
			}
			latestInstructions := latestUpdate["session"].(map[string]any)["instructions"]
			if s.sync.token <= oldToken {
				t.Fatal("snapshot token reused across input cancellation")
			}
			if action == "side" && latestInstructions != oldInstructions {
				t.Fatal("fixture must preserve identical instructions")
			}
			p.quiet(t, time.Millisecond)
			if schedule == "after-new-snapshot" || schedule == "after-canceled-replacement" {
				latestToken := s.sync.token
				ackSnapshotWire(t, s, p)
				p.quiet(t, 20*time.Millisecond)
				if s.voiceRequested || s.sync.token != latestToken {
					t.Fatal("old FIFO ACK authorized latest snapshot")
				}
			}
			ackSnapshotWire(t, s, p)
			p.want(t, "response.create")
			p.quiet(t, time.Millisecond)
			if s.paused.original != original || s.responseOrigin.itemID != item || s.route.input.itemID != item {
				t.Fatal("released response lost original goal or latest input")
			}
			if (action == "restart") != (s.responseTaskID == s.paused.id) {
				t.Fatal("released response has wrong task binding")
			}
			if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "latest-voice"}}); err != nil {
				t.Fatalf("valid response disconnected after obsolete ACK: %v", err)
			}
			t.Cleanup(s.encoder.Close)
			if err := s.beginPlayback(); err != nil {
				t.Fatal(err)
			}
			if s.activePlayback.origin.itemID != item || s.activePlayback.taskID != s.responseTaskID {
				t.Fatal("playback lost latest accepted binding")
			}
		})
	}
}
