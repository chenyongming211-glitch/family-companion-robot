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
