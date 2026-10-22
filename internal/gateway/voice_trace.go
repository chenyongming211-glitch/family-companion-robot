package gateway

import "log/slog"

// Only control boundaries are recorded, never audio packets, text, upstream IDs
// or raw event bodies. Values describe this loop's observed state, not physical
// speaker timing or an acoustic acceptance result.
func (s *session) traceVoice(event string) {
	switch event {
	case "listen_start", "listen_stop", "device_abort", "vad_started", "vad_stopped",
		"input_committed", "transcription_ready", "response_created", "response_completed",
		"response_cancelled", "tts_start", "tts_stop", "interrupt":
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
		"speaking", s.started, "pending_ms", pendingMS, "input_bytes", s.inputBytes)
}

// Record only ledger boundaries, never each periodic receipt or request text.
// A DMA position is digital completion evidence, not a word or acoustic clock.
func (s *session) tracePlayback(event string, r *playbackRecord) {
	if r == nil {
		return
	}
	switch event {
	case "progress_started", "invalid", "finished", "paused_created", "paused_retained", "confirmed":
	default:
		return
	}
	known := s.playbackEnabled && r.known && !r.invalid
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
			"original_request_known", s.paused.playback.origin.known)
	}
	slog.Info("playback_event", args...)
}
