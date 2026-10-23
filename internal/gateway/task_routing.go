package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"familyrobot/internal/qwen"
)

const maxTaskRouteAttempts = 3
const taskTranscriptTimeout = 8 * time.Second

type taskRouting struct {
	startedAt       time.Time
	active, decided bool
	awaitingChoice  bool
	taskID          string
	revision        uint64
	input           requestOrigin
	action          string
	attempts        int
	deadline        time.Time
	token           uint64
	cancel          context.CancelFunc
}
type taskRouteResult struct {
	token, generation uint64
	taskID            string
	revision          uint64
	input             requestOrigin
	action            string
	err               error
}

func (s *session) cancelRouting() {
	if s.route.cancel != nil {
		s.route.cancel()
	}
	s.route = taskRouting{}
}

// This method runs only on the session loop. The worker receives immutable
// copies and can only send a result; it cannot bind tasks or write voice state.
func (s *session) advanceRouting(now time.Time) error {
	r := &s.route
	if !r.active {
		return nil
	}
	if r.token != 0 {
		if !now.Before(r.deadline) {
			return s.finishRouting(taskRouteResult{token: r.token, generation: s.generation, taskID: r.taskID, revision: r.revision, input: r.input, err: errors.New("classifier timed out")})
		}
		return nil
	}
	if !r.deadline.IsZero() && !now.Before(r.deadline) {
		return errors.New("paused task transcription timed out")
	}
	if s.currentInput.generation != s.generation || s.currentInput.itemID == "" {
		return errors.New("paused task input identity missing")
	}
	if !s.currentInput.known || s.paused != nil && !s.paused.original.known {
		return nil
	}
	if s.paused == nil || s.currentInput.truncated || s.paused.original.truncated || strings.TrimSpace(s.currentInput.text) == "" || strings.TrimSpace(s.paused.original.text) == "" {
		return errors.New("paused task routing input unavailable")
	}
	if r.attempts >= maxTaskRouteAttempts {
		return errors.New("paused task routing failed closed")
	}
	s.routeSequence++
	r.token = s.routeSequence
	r.taskID = s.paused.id
	r.awaitingChoice = s.paused.awaitingResumeChoice
	r.revision = s.snapshotRevision
	r.input = s.currentInput
	r.attempts++
	r.deadline = now.Add(snapshotACKTimeout)
	if s.routeResults == nil {
		s.routeResults = make(chan taskRouteResult, 1)
	}
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, snapshotACKTimeout)
	r.cancel = cancel
	result := taskRouteResult{token: r.token, generation: s.generation, taskID: r.taskID, revision: r.revision, input: r.input}
	cfg := qwen.Config{URL: s.cfg.Qwen.URL, APIKey: s.cfg.Qwen.APIKey}
	classify := s.classifyPausedTask
	if classify == nil {
		classify = qwen.ClassifyPausedTask
	}
	awaiting := r.awaitingChoice
	original, latest, results := s.paused.original.text, s.currentInput.text, s.routeResults
	s.traceTaskRoute("requested")
	go func() {
		defer cancel()
		result.action, result.err = classify(ctx, cfg, original, latest, awaiting)
		select {
		case results <- result:
		case <-ctx.Done():
		}
	}()
	return nil
}
func (s *session) finishRouting(result taskRouteResult) error {
	r := &s.route
	if !r.active || r.token == 0 || result.token != r.token {
		return nil
	}
	r.cancel()
	r.cancel = nil
	r.token = 0
	if result.generation != s.generation || result.taskID != r.taskID || result.revision != r.revision || result.input != r.input || s.currentInput != result.input || s.paused == nil || s.paused.id != result.taskID {
		return errors.New("paused task routing identity changed")
	}
	if !time.Now().Before(r.deadline) {
		result.err = errors.New("classifier timed out")
	}
	outcome := "accepted"
	switch result.action {
	case "side", "resume", "restart", "resume_from", "discard", "replace":
	default:
		outcome = "rejected"
	}
	if result.err != nil {
		outcome = "rejected"
	}
	if s.snapshotRevision != result.revision || s.paused.awaitingResumeChoice != r.awaitingChoice {
		outcome = "stale"
	}
	if outcome != "accepted" {
		s.traceTaskRoute(outcome)
		if r.attempts >= maxTaskRouteAttempts {
			return errors.New("paused task routing failed closed")
		}
		// A late playback final changes the authoritative revision. Reclassify on
		// a fresh connection; the same total budget covers provider/parser failures.
		r.deadline = time.Time{}
		return s.advanceRouting(time.Now())
	}
	r.action = result.action
	if r.action != "side" {
		action := r.action
		if action == "restart" || action == "resume_from" {
			action = "resume"
		}
		args, _ := json.Marshal(map[string]any{"task_id": r.taskID, "expected_revision": r.revision, "action": action})
		output, _ := s.conversationTool("conversation_update_paused_task", string(args))
		var accepted struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal([]byte(output), &accepted) != nil || !accepted.OK {
			return errors.New("paused task routing action failed")
		}
	}
	if s.paused != nil && (r.action == "resume" || r.action == "restart" || r.action == "resume_from") {
		if r.action == "resume_from" || r.action == "restart" {
			s.paused.resumeRequest = &recordOrigin{SessionID: s.id, ItemID: s.currentInput.itemID, Text: s.currentInput.text, Known: s.currentInput.known, Truncated: s.currentInput.truncated}
		}
		if r.action == "resume_from" {
			s.paused.resumeSource = "user_selected"
		} else if r.action == "restart" {
			s.paused.resumeSource = "user_restart"
		}
		awaiting := r.action == "resume"
		if s.paused.awaitingResumeChoice != awaiting {
			s.paused.awaitingResumeChoice = awaiting
			s.snapshotRevision++
		}
	}
	s.traceTaskRoute("accepted")
	r.active = false
	r.decided = true
	r.deadline = time.Time{}
	// The following voice response and its device-tool continuations share the
	// accepted binding. No classifier text or function output enters voice context.
	s.toolContinuation = true
	s.responseOrigin = s.currentInput
	return s.advanceGeneration(time.Now())
}
