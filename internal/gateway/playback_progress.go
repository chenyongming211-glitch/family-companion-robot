package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"familyrobot/internal/protocol"
)

const maxPlaybackRecords = 16

type requestOrigin struct {
	generation       uint64
	itemID           string
	text             string
	known, truncated bool
	wake             bool
}
type playbackRecord struct {
	id                                           uint32
	sent, played                                 uint64
	known, invalid, settled, sealed, interrupted bool
	origin                                       requestOrigin
	taskID                                       string
	settleUntil                                  time.Time
	timedOut, retired                            bool
}
type pausedTask struct {
	recordLatest               recordOrigin
	resumeRequest              *recordOrigin
	recordAt                   time.Time
	recordSource               recordOrigin
	sourceTaskID, resumeSource string
	historicalPlayback         *recordPlayback
	awaitingResumeChoice       bool
	id                         string
	original                   requestOrigin
	playback                   playbackRecord
}

func (s *session) beginPlayback() error {
	if s.playbackSequence == math.MaxUint32 {
		return errors.New("playback identity exhausted")
	}
	s.playbackSequence++
	r := &playbackRecord{id: s.playbackSequence, origin: s.responseOrigin, taskID: s.responseTaskID}
	s.playbacks = append(s.playbacks, r)
	if len(s.playbacks) > maxPlaybackRecords {
		s.playbacks = s.playbacks[len(s.playbacks)-maxPlaybackRecords:]
	}
	s.activePlayback = r
	if s.paused != nil && r.taskID == s.paused.id {
		s.paused.playback = *r
		s.snapshotRevision++
	}
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
		s.refreshTaskPlayback(r)
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
	for _, turns := range [][]turn{s.hist.Turns, s.hist.archive} {
		for i := range turns {
			t := &turns[i]
			if t.playbackID == r.id && t.Role == "assistant" {
				t.Delivery = "interrupted"
			}
		}
	}
	if !r.settled && r.settleUntil.IsZero() {
		r.settleUntil = time.Now().Add(playbackSettlementTimeout)
	}
	// Retirement removes task ownership, not the interruption of the audio or
	// stored tool-parent turns sharing this playback block.
	if r.retired {
		return
	}
	if s.wakeOnly(r.origin) {
		s.tracePlayback("wake_greeting_skipped", r)
		return
	}
	if s.paused == nil {
		s.taskSequence++
		s.taskDisposition = "active"
		s.paused = &pausedTask{id: fmt.Sprintf("%s:task-%d", s.id, s.taskSequence), original: r.origin, playback: *r}
		s.snapshotRevision++
		s.tracePlayback("paused_created", r)
	} else if r.taskID == s.paused.id {
		s.paused.playback = *r
		s.snapshotRevision++
		s.tracePlayback("paused_resumed", r)
	} else {
		s.tracePlayback("paused_retained", r)
	}
}
func (s *session) refreshTaskPlayback(r *playbackRecord) {
	if r != nil && s.paused != nil && r.taskID == s.paused.id && s.paused.playback.id == r.id && s.paused.playback != *r {
		s.paused.playback = *r
		s.snapshotRevision++
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
	var before playbackRecord
	if s.paused != nil {
		before = s.paused.playback
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
			r.timedOut = false
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
			if before != *r {
				s.snapshotRevision++
			}
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
		for _, turns := range [][]turn{s.hist.Turns, s.hist.archive} {
			for i := range turns {
				t := &turns[i]
				if t.playbackID == r.id && t.Delivery == "transport_sent" {
					t.Delivery = "playback_confirmed"
					promoted = true
				}
			}
		}
		if promoted {
			s.tracePlayback("confirmed", r)
		}
	}
}

// Input identities include upstream generation; playback identities instead live
// for the entire device connection, including upstream-only reconnects.
func (s *session) resetInputRequest() error {
	if err := s.supersedeClarification(); err != nil {
		return err
	}
	s.cancelRouting()
	s.toolContinuation = false
	// Retire the old input's request without reusing its token. Its update
	// remains in the model client's ACK FIFO and must consume its own slot.
	s.sync = generationSync{sequence: s.sync.sequence}
	s.voiceRequested = false
	return nil
}

func (s *session) commitOrigin(itemID string) error {
	old := s.currentInput
	if !s.inputCommitted || old.generation != s.generation || old.itemID != itemID {
		if err := s.resetInputRequest(); err != nil {
			return err
		}
	}
	s.currentInput = requestOrigin{generation: s.generation, itemID: itemID}
	if itemID != "" && old.generation == s.generation && old.itemID == itemID {
		s.currentInput = old
	}
	s.inputCommitted = true
	return nil
}
func (s *session) transcribeOrigin(itemID, text string) error {
	origin := requestOrigin{generation: s.generation, itemID: itemID, text: limitText(text, 512), known: text != "", truncated: len([]rune(text)) > 512}
	origin.wake = itemID != "" && itemID == s.wake.inputID
	matches := func(o requestOrigin) bool { return o.generation == s.generation && o.itemID == itemID }
	replacesQuestion := s.clarification.active && (!s.inputCommitted || matches(s.currentInput)) && origin != s.clarification.input
	if replacesQuestion {
		if err := s.resetInputRequest(); err != nil {
			return err
		}
	}
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
		s.snapshotRevision++
	}
	if itemID != "" && s.paused != nil && matches(s.paused.original) && !s.paused.original.known {
		s.paused.original = origin
		s.snapshotRevision++
	}
	if matches(s.wake.origin) && !s.wake.origin.known {
		s.wake.origin = origin
	}
	if replacesQuestion {
		return s.requestResponse()
	}
	return nil
}
func (s *session) pausedData() map[string]any {
	out := map[string]any{"available": s.paused != nil, "text_position": "unknown", "revision": s.snapshotRevision}
	if s.paused == nil {
		return out
	}
	p := s.paused
	r := p.playback
	out["task_id"] = p.id
	if p.historicalPlayback != nil {
		out["historical_playback"] = p.historicalPlayback
		out["source_task_id"] = p.sourceTaskID
		out["original_source"] = p.recordSource
	}
	if p.resumeSource != "" {
		out["resume_source"] = p.resumeSource
		if p.resumeRequest != nil {
			out["resume_request"] = p.resumeRequest
		}
	}
	out["awaiting_resume_choice"] = p.awaitingResumeChoice
	out["original_request"] = p.original.text
	out["original_request_known"] = p.original.known
	out["original_request_truncated"] = p.original.truncated
	out["position_known"] = s.playbackEnabled && r.known && !r.invalid && !r.timedOut
	out["settled"] = r.settled
	out["settlement_timed_out"] = r.timedOut
	out["latest_request"] = r.origin.text
	out["latest_request_known"] = r.origin.known
	out["interrupted"] = r.interrupted
	// This describes the latest digital block only. A fully delivered block
	// may be a clarification; retain the original task until discard/replace.
	out["latest_block_delivery_complete"] = r.sealed && !r.interrupted && r.settled && r.known && !r.invalid && r.played == r.sent
	if s.playbackEnabled && r.id != 0 {
		out["playback_id"] = r.id
		out["session_id"] = s.id
		out["sample_rate"] = 24000
		out["sent_samples"] = r.sent
	}
	if s.playbackEnabled && r.known && !r.invalid && !r.timedOut {
		out["played_samples"] = r.played
	} else {
		out["played_samples"] = nil
	}
	return out
}
func (s *session) retireTaskPlayback() {
	if s.paused == nil {
		return
	}
	for _, r := range s.playbacks {
		if r.id == s.paused.playback.id || r.taskID == s.paused.id {
			r.retired = true
		}
	}
	if s.activePlayback != nil && (s.activePlayback.id == s.paused.playback.id || s.activePlayback.taskID == s.paused.id) {
		s.activePlayback.retired = true
	}
	if s.devicePlayback != nil && s.devicePlayback.retired {
		s.devicePlayback = nil
	}
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
		Action           string  `json:"action"`
		TaskID           string  `json:"task_id"`
		ExpectedRevision *uint64 `json:"expected_revision"`
	}
	if json.Unmarshal([]byte(args), &input) != nil {
		return fail("invalid_arguments")
	}
	if name == "conversation_update_paused_task" {
		if s.paused == nil || input.TaskID != s.paused.id {
			return fail("task_id_mismatch")
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision != s.snapshotRevision {
			return fail("revision_mismatch")
		}
		switch input.Action {
		case "resume":
			s.responseTaskID = s.paused.id
			if s.activePlayback != nil {
				s.activePlayback.taskID = s.paused.id
				s.paused.playback = *s.activePlayback
			}
			s.traceVoice("task_resume_bound")
		case "discard":
			s.taskDisposition = "discarded"
			s.retireTaskPlayback()
			s.responseTaskID = ""
			s.paused = nil
		case "replace":
			if !s.currentInput.known || s.currentInput.generation != s.generation {
				return fail("current_request_unknown")
			}
			s.retireTaskPlayback()
			s.taskSequence++
			s.taskDisposition = "active"
			s.paused = &pausedTask{id: fmt.Sprintf("%s:task-%d", s.id, s.taskSequence), original: s.currentInput, playback: playbackRecord{origin: s.currentInput}}
			s.responseTaskID = s.paused.id
			if s.activePlayback != nil {
				s.activePlayback.taskID = s.paused.id
				s.activePlayback.retired = false
				s.activePlayback.origin = s.currentInput
				s.paused.playback = *s.activePlayback
			}
		default:
			return fail("invalid_action")
		}
		s.snapshotRevision++
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
		{"type": "function", "function": map[string]any{"name": "conversation_update_paused_task", "description": "按匹配 task_id 和 expected_revision 显式 resume 续答、discard 放弃或 replace 切换暂停任务；resume 绑定后续回复属于同一任务；replace 仅使用当前实际用户转写，新任务的播放位置重置为 unknown，不继承旧位置。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}, "expected_revision": map[string]any{"type": "integer", "minimum": 0}, "action": map[string]any{"type": "string", "enum": []string{"resume", "discard", "replace"}}}, "required": []string{"task_id", "action", "expected_revision"}, "additionalProperties": false}}},
	}
}
