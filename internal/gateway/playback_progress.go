package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"familyrobot/internal/protocol"
)

const maxPlaybackRecords = 16

type requestOrigin struct {
	generation       uint64
	itemID           string
	text             string
	known, truncated bool
}
type playbackRecord struct {
	id                                           uint32
	sent, played                                 uint64
	known, invalid, settled, sealed, interrupted bool
	origin                                       requestOrigin
}
type pausedTask struct {
	id       string
	playback playbackRecord
}

func (s *session) beginPlayback() error {
	if s.playbackSequence == math.MaxUint32 {
		return errors.New("playback identity exhausted")
	}
	s.playbackSequence++
	r := &playbackRecord{id: s.playbackSequence, origin: s.responseOrigin}
	s.playbacks = append(s.playbacks, r)
	if len(s.playbacks) > maxPlaybackRecords {
		s.playbacks = s.playbacks[len(s.playbacks)-maxPlaybackRecords:]
	}
	s.activePlayback = r
	if s.playbackEnabled {
		s.devicePlayback = r
	}
	return nil
}
func (s *session) sealPlayback(interrupted bool) {
	if r := s.activePlayback; r != nil {
		r.sealed = true
		r.interrupted = interrupted
		s.promotePlayback(r)
	}
}

// Device ownership outlives normal transport sealing. It refers only to the
// newest admitted block in the bounded ledger, never a response or draft.
func (s *session) interruptiblePlayback() *playbackRecord {
	if s.started && s.activePlayback != nil {
		return s.activePlayback
	}
	if s.playbackEnabled && s.devicePlayback != nil {
		r := s.devicePlayback
		if !r.settled || r.interrupted {
			return r
		}
	}
	return nil
}
func (s *session) pausePlayback() {
	r := s.interruptiblePlayback()
	if r == nil {
		return
	}
	r.sealed = true
	r.interrupted = true
	if s.paused == nil {
		s.taskSequence++
		s.paused = &pausedTask{id: fmt.Sprintf("task-%d", s.taskSequence), playback: *r}
		s.tracePlayback("paused_created", r)
	} else {
		s.tracePlayback("paused_retained", r)
	}
}
func (s *session) receivePlayback(data []byte) {
	if !s.playbackEnabled {
		return
	}
	p, err := protocol.ParsePlayback(data)
	if err != nil || p.SessionID != s.id {
		return
	}
	records := s.playbacks
	if s.paused != nil {
		records = append(append([]*playbackRecord(nil), records...), &s.paused.playback)
	}
	for _, r := range records {
		if r.id != p.PlaybackID {
			continue
		}
		if r.settled || p.PlayedSamples > r.sent || p.PlayedSamples < r.played {
			return
		}
		// A gap is evidence even before stop; a premature valid final cannot
		// settle a block which the server is still sending.
		if p.State == "finished" && !r.sealed && *p.Valid {
			return
		}
		wasKnown, wasInvalid := r.known, r.invalid
		if !*p.Valid {
			r.invalid = true
			r.known = false
		} else if !r.invalid {
			r.played = p.PlayedSamples
			r.known = true
		}
		if p.State == "finished" && r.sealed {
			r.settled = true
			r.interrupted = r.interrupted || p.Interrupted
		}
		// A normal final resolves device ownership. An interrupted final may
		// precede its abort control; retain that identity until the actual entry
		// point consumes it. invalid progress alone never proves device drain.
		if s.devicePlayback == r && r.settled && !r.interrupted {
			s.devicePlayback = nil
		}
		if s.paused != nil && s.paused.playback.id == r.id {
			s.paused.playback = *r
		}
		switch {
		case p.State == "finished" && r.settled:
			s.tracePlayback("finished", r)
		case !wasInvalid && r.invalid:
			s.tracePlayback("invalid", r)
		case !wasKnown && r.known:
			s.tracePlayback("progress_started", r)
		}
		s.promotePlayback(r)
		return
	}
}
func (s *session) promotePlayback(r *playbackRecord) {
	if r.sealed && !r.interrupted && r.settled && r.known && !r.invalid && r.played == r.sent {
		promoted := false
		for i := range s.hist.Turns {
			t := &s.hist.Turns[i]
			if t.playbackID == r.id && t.Delivery == "transport_sent" {
				t.Delivery = "playback_confirmed"
				promoted = true
			}
		}
		if promoted {
			s.tracePlayback("confirmed", r)
		}
	}
}

// Input identities include upstream generation; playback identities instead live
// for the entire device connection, including upstream-only reconnects.
func (s *session) commitOrigin(itemID string) {
	s.currentInput = requestOrigin{generation: s.generation, itemID: itemID}
	s.inputCommitted = true
}
func (s *session) transcribeOrigin(itemID, text string) {
	origin := requestOrigin{generation: s.generation, itemID: itemID, text: limitText(text, 512), known: text != "", truncated: len([]rune(text)) > 512}
	matches := func(o requestOrigin) bool { return o.generation == s.generation && o.itemID == itemID }
	// With an outstanding committed identity an unrelated or anonymous STT must
	// not replace it. Old fake peers without item IDs retain conservative support.
	if !s.inputCommitted || matches(s.currentInput) {
		s.currentInput = origin
	}
	if itemID != "" && matches(s.responseOrigin) && !s.responseOrigin.known {
		s.responseOrigin = origin
	}
	for _, r := range s.playbacks {
		if itemID != "" && matches(r.origin) && !r.origin.known {
			r.origin = origin
		}
	}
	if itemID != "" && s.paused != nil && matches(s.paused.playback.origin) && !s.paused.playback.origin.known {
		s.paused.playback.origin = origin
	}
}
func (s *session) pausedData() map[string]any {
	out := map[string]any{"available": s.paused != nil, "text_position": "unknown"}
	if s.paused == nil {
		return out
	}
	p := s.paused
	r := p.playback
	out["task_id"] = p.id
	out["original_request"] = r.origin.text
	out["original_request_known"] = r.origin.known
	out["original_request_truncated"] = r.origin.truncated
	out["position_known"] = s.playbackEnabled && r.known && !r.invalid
	out["settled"] = r.settled
	out["interrupted"] = r.interrupted
	if s.playbackEnabled && r.id != 0 {
		out["playback_id"] = r.id
		out["session_id"] = s.id
		out["sample_rate"] = 24000
		out["sent_samples"] = r.sent
	}
	if s.playbackEnabled && r.known && !r.invalid {
		out["played_samples"] = r.played
	} else {
		out["played_samples"] = nil
	}
	return out
}
func (s *session) conversationTool(name, args string) (string, bool) {
	if name != "conversation_get_paused_task" && name != "conversation_update_paused_task" {
		return "", false
	}
	fail := func(message string) (string, bool) {
		b, _ := json.Marshal(map[string]any{"ok": false, "error": message})
		return string(b), true
	}
	var input struct {
		Action string `json:"action"`
		TaskID string `json:"task_id"`
	}
	if json.Unmarshal([]byte(args), &input) != nil {
		return fail("invalid_arguments")
	}
	if name == "conversation_update_paused_task" {
		if s.paused == nil || input.TaskID != s.paused.id {
			return fail("task_id_mismatch")
		}
		switch input.Action {
		case "discard":
			s.paused = nil
		case "replace":
			if !s.currentInput.known || s.currentInput.generation != s.generation {
				return fail("current_request_unknown")
			}
			s.taskSequence++
			s.paused = &pausedTask{id: fmt.Sprintf("task-%d", s.taskSequence), playback: playbackRecord{origin: s.currentInput}}
		default:
			return fail("invalid_action")
		}
		// An explicit task mutation supersedes any already-finished interruption
		// awaiting its control event; that event must not resurrect discarded work.
		if s.devicePlayback != nil && s.devicePlayback.settled {
			s.devicePlayback = nil
		}
	}
	out := s.pausedData()
	out["ok"] = true
	b, _ := json.Marshal(out)
	return string(b), true
}
func conversationTools() []map[string]any {
	return []map[string]any{
		{"type": "function", "function": map[string]any{"name": "conversation_get_paused_task", "description": "读取暂停原任务和设备采样位置；text_position=unknown，不代表已知道暂停字词。", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}},
		{"type": "function", "function": map[string]any{"name": "conversation_update_paused_task", "description": "按匹配 task_id 显式放弃或切换暂停任务；replace 仅使用当前实际用户转写，新任务的播放位置重置为 unknown，不继承旧位置。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}, "action": map[string]any{"type": "string", "enum": []string{"discard", "replace"}}}, "required": []string{"task_id", "action"}, "additionalProperties": false}}},
	}
}
