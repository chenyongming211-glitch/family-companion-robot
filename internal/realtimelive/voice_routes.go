package realtimelive

import (
	"context"
	"errors"
	"familyrobot/internal/music"
	"familyrobot/internal/voicecompare"
	"math"
	"net/url"
	"strings"
	"time"
)

const voiceRouteTTSModel = "cosyvoice-v3-flash"
const voiceRouteTTSVoice = "longanyang"

// ErrVoiceRouteUnknown separates "this device has no route row" from "the route
// store could not be read". A missing row is a normal state for a Mac terminal,
// which never appears in family device records; an unreadable store is not.
var ErrVoiceRouteUnknown = errors.New("voice_route_unknown")

// Presentation follows this connection's generation route. Native configuration
// remains owned by the common listener and business preflight.
func (s *Session) voiceRouteFacts() map[string]any {
	facts := s.cfg.facts()
	if s.voiceRoute == "A" || s.voiceRoute == "B" {
		facts["QWEN_VOICE"] = voiceRouteTTSVoice
		facts["enable_search"] = false
		if s.voiceRoute == "B" {
			facts["model"] = "qwen-plus"
		}
	}
	return facts
}

func parseVoiceRoute(route string) (string, error) {
	if route == "" {
		return "C", nil
	}
	if route != "A" && route != "B" && route != "C" {
		return "", errors.New("invalid_voice_route")
	}
	return route, nil
}

// Attach cancellation before profile/policy resolution and provider startup.
// The callback belongs to this exact socket/lease, never a device-wide lookup.
func (s *Server) bindVoiceRouteSession(session *Session) {
	if session.ws == nil {
		return
	}
	s.mu.Lock()
	lease := s.leases[ownerKey(session.rec.owner)]
	revoked := false
	if lease != nil && lease.conn == session.ws {
		revoked = lease.revoked
		if !revoked {
			lease.cancel = session.cancel
		}
	}
	s.mu.Unlock()
	if revoked {
		session.ownerErr = errors.New("voice_route_disconnected")
		session.cancel()
	}
}

// Resolving policy belongs after authenticated ownership and before Run. The
// captured generation function changes no business preflight or playback rules.
// developmentTerminal reports the Mac test entry points. They are the same
// process with or without a recorded plan driving the microphone, and neither
// is a managed family device, so neither has a route row. Route A/B testing
// runs through the fixture form, which is why it has to be admitted too.
func developmentTerminal(clientType string) bool {
	return clientType == "mac_native" || clientType == "mac_fixture"
}

func (s *Server) configureVoiceRoute(session *Session) {
	defer func() {
		if session.ownerErr != nil {
			session.cancel()
		}
	}()
	if session.ownerErr != nil {
		return
	}
	route := "C"
	if session.rec.owner.DeviceID != "" && s.cfg.VoiceRouteForDevice != nil {
		selected, err := s.cfg.VoiceRouteForDevice(session.rec.owner.DeviceID)
		if errors.Is(err, ErrVoiceRouteUnknown) && developmentTerminal(session.rec.owner.ClientType) {
			// The development terminal is not a managed family device, so it has
			// no route row. Native route C needs no managed state; product
			// devices below still refuse to connect without their row.
			route = "C"
		} else if err != nil {
			session.ownerErr = errors.New("voice_route_read_failed")
			return
		} else {
			route, err = parseVoiceRoute(selected)
			if err != nil {
				session.ownerErr = err
				return
			}
		}
	}
	session.voiceRoute = route
	if route == "A" || route == "B" {
		session.generate = session.generateConversation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lease := s.leases[ownerKey(session.rec.owner)]
	if lease != nil && session.ws != nil && lease.conn == session.ws {
		if lease.revoked {
			session.ownerErr = errors.New("voice_route_disconnected")
			return
		}
		lease.route = route
	}
}
func (s *Session) generateConversation(ctx context.Context, id int64, prompt string, pcm []byte, emit func(string, []byte) error) error {
	backend := "native"
	if s.voiceRoute == "B" {
		backend = "cascade"
	}
	inference, err := url.Parse(s.cfg.ASRURL)
	if err != nil || inference.Host == "" {
		return errors.New("invalid_voice_endpoint")
	}
	textURL := *inference
	textURL.Path = "/compatible-mode/v1/chat/completions"
	textURL.RawQuery = ""
	textURL.Fragment = ""
	switch textURL.Scheme {
	case "wss":
		textURL.Scheme = "https"
	case "ws":
		textURL.Scheme = "http"
	default:
		return errors.New("invalid_voice_endpoint")
	}
	allowed := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if check, ok := ctx.Value(musicSubmissionGuardKey{}).(func() bool); ok && !check() {
			return music.ErrLibraryForbidden
		}
		return nil
	}
	var audit *intent
	ttsPart := 0
	c := voicecompare.Client{Key: s.cfg.Key, RealtimeURL: s.cfg.URL, InferenceURL: s.cfg.ASRURL, TextURL: textURL.String(), TTSModel: voiceRouteTTSModel, Voice: voiceRouteTTSVoice, BeforeSubmit: allowed}
	var firstTextWait time.Duration
	var firstTextSource string
	c.FirstNativeText = func(wait time.Duration, source string) {
		firstTextWait, firstTextSource = wait, source
	}
	c.BeforeCall = func(stage string, _ float64) error {
		if err := allowed(); err != nil {
			return err
		}
		var err error
		audit, err = newIntent(s.directory, "voice-route-"+stage, s.cfg.Synthetic, s.cfg.MemoryOnly)
		if err != nil {
			return err
		}
		model := map[string]string{"native": s.cfg.Model, "asr": "fun-asr-realtime", "llm": "qwen-plus", "tts": c.TTSModel}[stage]
		fields := map[string]any{"id": id, "route": s.voiceRoute, "model": model, "usage_source": "voice_route_" + stage, "usage_state": "unknown"}
		if stage == "tts" {
			ttsPart++
			fields["tts_part"] = ttsPart
		}
		return audit.update(fields)
	}
	c.AfterCall = func(stage string, usage map[string]any, callErr error) error {
		if audit == nil {
			return errors.New("missing_voice_route_intent")
		}
		state := "completed"
		if callErr != nil {
			state = "failed_or_unknown"
		}
		numeric := voiceRouteUsage(usage)
		usageState := "complete"
		fields := map[string][]string{"native": {"input_tokens", "output_tokens", "total_tokens"}, "asr": {"duration"}, "llm": {"prompt_tokens", "completion_tokens", "total_tokens"}, "tts": {"characters"}}[stage]
		for _, key := range fields {
			if _, ok := numeric[key]; !ok {
				usageState = "unknown"
			}
		}
		result := map[string]any{"state": state, "usage": numeric, "usage_state": usageState}
		if stage == "native" && firstTextSource != "" {
			result["response_create_to_first_text_ms"] = firstTextWait.Milliseconds()
			result["first_text_source"] = firstTextSource
		}
		if stage == "tts" && callErr != nil {
			result["failure_category"] = voicecompare.TTSFailureCategory(callErr)
		}
		return audit.update(result)
	}
	if err := allowed(); err != nil {
		return err
	}
	var spoken strings.Builder
	return c.StreamConversation(ctx, backend, prompt, pcm, func(draft, clause string, audio []byte) error {
		if err := allowed(); err != nil {
			return err
		}
		if draft != "" {
			if err := emit("text", []byte(draft)); err != nil {
				return err
			}
		}
		// Keep one display transcript and grow the playback reference only as
		// synthesis succeeds. The session owns the continuous PCM/Opus stream.
		spoken.WriteString(clause)
		if err := emit("spoken_reference", []byte(spoken.String())); err != nil {
			return err
		}
		for begin := 0; begin < len(audio); begin += 2880 {
			if err := allowed(); err != nil {
				return err
			}
			if err := emit("audio", audio[begin:min(begin+2880, len(audio))]); err != nil {
				return err
			}
		}
		return nil
	})
}
func voiceRouteUsage(raw map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"characters", "duration", "prompt_tokens", "completion_tokens", "input_tokens", "output_tokens", "total_tokens", "text_tokens", "audio_tokens"} {
		if n, ok := raw[key].(float64); ok && n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) {
			out[key] = n
		}
	}
	for _, key := range []string{"input_tokens_details", "output_tokens_details"} {
		if fields, ok := raw[key].(map[string]any); ok {
			out[key] = voiceRouteUsage(fields)
		}
	}
	return out
}
