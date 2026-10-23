package gateway

import (
	"log/slog"
	"time"
)

// Only control boundaries are recorded, never audio packets, text, upstream IDs
// or raw event bodies. Values describe this loop's observed state, not physical
// speaker timing or an acoustic acceptance result.
func (s *session) traceVoice(event string) {
	switch event {
	case "listen_start", "listen_stop", "device_abort", "vad_started", "vad_stopped",
		"input_committed", "transcription_ready", "response_created", "response_completed",
		"response_cancelled", "tts_start", "tts_stop", "interrupt", "snapshot_sent", "snapshot_acknowledged", "response_requested", "task_resume_bound", "task_delivery_complete",
		"clarification_verified", "clarification_rejected":
	default:
		return
	}
	mode := s.mode
	switch mode {
	case "auto", "manual", "realtime":
	default:
		mode = "unknown"
	}
	pendingMS := 0
	if s.pending != nil {
		pendingMS = s.pending.Len() / (24000 * 2 / 1000)
	}
	slog.Info("voice_event", "event", event, "session", s.id, "mode", mode,
		"upstream_generation", s.generation, "response_open", s.responseID != "",
		"speaking", s.started, "pending_ms", pendingMS, "input_bytes", s.inputBytes, "snapshot_revision", s.snapshotRevision, "snapshot_token", s.sync.token, "task_bound", s.responseTaskID != "")
}

// Record only ledger boundaries, never each periodic receipt or request text.
// A DMA position is digital completion evidence, not a word or acoustic clock.
func (s *session) tracePlayback(event string, r *playbackRecord) {
	if r == nil {
		return
	}
	switch event {
	case "progress_started", "invalid", "finished", "paused_created", "paused_retained", "paused_resumed", "confirmed",
		"wake_greeting_skipped", "wake_pause_discarded":
	default:
		return
	}
	known := s.playbackEnabled && r.known && !r.invalid && !r.timedOut
	var played any
	if known {
		played = r.played
	}
	args := []any{"event", event, "session", s.id, "playback_id", r.id,
		"sent_samples", r.sent, "played_samples", played, "position_known", known,
		"settled", r.settled, "interrupted", r.interrupted, "text_position", "unknown"}
	if s.paused != nil {
		args = append(args, "paused_task_id", s.paused.id,
			"paused_playback_id", s.paused.playback.id,
			"original_request_known", s.paused.original.known)
	}
	slog.Info("playback_event", args...)
}

// Fixed enums and counters only: no arguments, text, or upstream identities.
func (s *session) traceTaskRoute(outcome string) {
	elapsed := int64(0)
	if !s.route.startedAt.IsZero() {
		elapsed = time.Since(s.route.startedAt).Milliseconds()
	}
	slog.Info("task_route", "session", s.id, "upstream_generation", s.generation, "route_elapsed_ms", elapsed, "outcome", outcome, "action", s.route.action, "attempt", s.route.attempts, "snapshot_revision", s.snapshotRevision, "captured_revision", s.route.revision, "task_bound", s.responseTaskID != "")
}
