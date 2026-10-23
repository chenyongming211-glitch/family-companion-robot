package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"familyrobot/internal/recordstore"
)

// Record I/O is admitted by the session loop; only the store worker touches disk.
// Receipt polling never blocks the audio/stop path and all session mutation stays
// on the loop. A failed load is write-protected until an explicit clear succeeds.
type ownedRecordStore interface {
	recordBackend
	Close(context.Context) error
}
type recordBackend interface {
	Load(context.Context, string) (recordstore.Snapshot, error)
	Save(string, recordstore.Snapshot) (<-chan error, error)
	Clear(context.Context, string) error
}

const recordLoadTimeout = 2 * time.Second
const recordClearTimeout = 5 * time.Second

type recordOrigin struct {
	SessionID string `json:"session_id"`
	ItemID    string `json:"item_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Known     bool   `json:"known"`
	Truncated bool   `json:"truncated"`
}
type recordPlayback struct {
	SessionID   string  `json:"session_id"`
	PlaybackID  uint32  `json:"playback_id"`
	Sent        uint64  `json:"sent_samples"`
	Played      *uint64 `json:"played_samples"`
	Settled     bool    `json:"settled"`
	Interrupted bool    `json:"interrupted"`
	Invalid     bool    `json:"invalid"`
}
type recordTask struct {
	AwaitingResumeChoice bool           `json:"awaiting_resume_choice"`
	Latest               recordOrigin   `json:"latest_request"`
	ResumeRequest        *recordOrigin  `json:"resume_request,omitempty"`
	At                   time.Time      `json:"at"`
	ID                   string         `json:"task_id"`
	Original             recordOrigin   `json:"original"`
	Playback             recordPlayback `json:"historical_playback"`
	SourceTaskID         string         `json:"source_task_id,omitempty"`
	ResumeSource         string         `json:"resume_source,omitempty"`
}
type recordData struct {
	TaskDisposition string      `json:"task_disposition"`
	Turns           []turn      `json:"turns"`
	Task            *recordTask `json:"task,omitempty"`
	Draft           *turn       `json:"draft,omitempty"`
	Interrupted     bool        `json:"previous_reply_interrupted"`
}
type recordReceipt struct {
	sequence uint64
	done     <-chan error
}
type recordClear struct {
	finished   <-chan struct{}
	done       <-chan error
	toolKey    string
	generation uint64
}
type conversationRecords struct {
	store                recordBackend
	device               string
	loaded               bool
	recoveryError        string
	status               string
	lastError            string
	lastData             []byte
	sequence, lastResult uint64
	pending              []recordReceipt
	clear                *recordClear
	suppressResponse     string
}

func (r *conversationRecords) pendingCount() int {
	if r == nil {
		return 0
	}
	n := len(r.pending)
	if r.clear != nil {
		n++
	}
	return n
}
func recordError(err error) string {
	// Do not expose paths, raw disk errors, or text to the upstream model/logs.
	switch {
	case errors.Is(err, recordstore.ErrBusy):
		return "queue_full"
	case errors.Is(err, recordstore.ErrNotFound):
		return "not_found"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, recordstore.ErrCorrupt):
		return "corrupt"
	default:
		return "storage_error"
	}
}
func (s *session) retention() time.Duration {
	if s.cfg.RecordRetention > 0 {
		return s.cfg.RecordRetention
	}
	return 168 * time.Hour
}
func boundedRecordOrigin(o recordOrigin) recordOrigin {
	if len([]rune(o.Text)) > 512 {
		o.Truncated = true
	}
	o.Text = limitText(o.Text, 512)
	o.ItemID = limitText(o.ItemID, 256)
	o.SessionID = limitText(o.SessionID, 128)
	return o
}
func (s *session) loadRecords(store recordBackend, device string) {
	if store == nil {
		return
	}
	s.records = &conversationRecords{store: store, device: device, status: "empty"}
	ctx, cancel := context.WithTimeout(s.ctx, recordLoadTimeout)
	defer cancel()
	raw, err := store.Load(ctx, device)
	if errors.Is(err, recordstore.ErrNotFound) {
		s.records.loaded = true
		s.records.lastData, _ = json.Marshal(recordData{})
		return
	}
	if err != nil {
		s.records.status = "failed"
		s.records.recoveryError = recordError(err)
		return
	}
	var data recordData
	if err = json.Unmarshal(raw.Data, &data); err != nil {
		s.records.status = "failed"
		s.records.recoveryError = "invalid_data"
		return
	}
	s.taskDisposition = data.TaskDisposition
	now := time.Now()
	cutoff := now.Add(-s.retention())
	for _, t := range data.Turns {
		if t.At.After(cutoff) && !t.At.After(now) && t.SessionID != "" && (t.Role == "user" || t.Role == "assistant") {
			t.Text = limitText(t.Text, 512)
			t.GeneratedText = limitText(t.GeneratedText, 512)
			t.playbackID = 0
			s.hist.appendTurn(t)
		}
	}
	if d := data.Draft; d != nil && d.At.After(cutoff) && !d.At.After(now) && d.SessionID != "" {
		d.playbackID = 0
		d.Delivery = "interrupted"
		d.Text = ""
		d.GeneratedText = limitText(d.GeneratedText, 512)
		s.hist.appendTurn(*d)
	}
	s.hist.Interrupted = data.Interrupted || data.Draft != nil
	if p := data.Task; p != nil && p.At.After(cutoff) && !p.At.After(now) {
		p.Original = boundedRecordOrigin(p.Original)
		p.Latest = boundedRecordOrigin(p.Latest)
		if p.ResumeRequest != nil {
			v := boundedRecordOrigin(*p.ResumeRequest)
			p.ResumeRequest = &v
		}
		s.taskSequence++
		source := p.SourceTaskID
		if source == "" {
			source = p.ID
		}
		s.paused = &pausedTask{awaitingResumeChoice: p.AwaitingResumeChoice, id: s.id + ":restored", original: requestOrigin{text: limitText(p.Original.Text, 512), known: p.Original.Known, truncated: p.Original.Truncated}, recordAt: p.At, recordSource: p.Original, recordLatest: p.Latest, resumeRequest: p.ResumeRequest, playback: playbackRecord{origin: requestOrigin{text: p.Latest.Text, known: p.Latest.Known, truncated: p.Latest.Truncated}}, sourceTaskID: source, resumeSource: p.ResumeSource, historicalPlayback: &p.Playback}
		s.snapshotRevision++
	}
	if data.Task != nil && s.paused == nil {
		s.taskDisposition = "expired"
	}
	s.records.loaded = true
	s.records.status = "saved"
}
func (s *session) recordingCapabilities() map[string]any {
	out := map[string]any{"temporary_context": len(s.hist.Turns) > 0, "text_persistence_enabled": s.records != nil, "persistence_status": "disabled", "pending_writes": 0, "retention_seconds": int64(s.retention() / time.Second), "audio_retained": false, "recovery_error": ""}
	if r := s.records; r != nil {
		status := r.status
		if r.pendingCount() > 0 {
			status = "pending"
		}
		out["persistence_status"] = status
		out["pending_writes"] = r.pendingCount()
		out["recovery_error"] = r.recoveryError
		out["persistence_error"] = r.lastError
	}
	return out
}
func (s *session) pruneRecords() {
	cutoff := time.Now().Add(-s.retention())
	var live []turn
	for _, t := range s.hist.Turns {
		if t.At.After(cutoff) {
			live = append(live, t)
		}
	}
	s.hist.Turns = live
	if p := s.paused; p != nil && !p.recordAt.IsZero() && !p.recordAt.After(cutoff) {
		s.retireTaskPlayback()
		s.paused = nil
		s.responseTaskID = ""
		s.taskDisposition = "expired"
		s.snapshotRevision++
	}
}
func (s *session) recordSnapshot() recordData {
	s.pruneRecords()
	cutoff := time.Now().Add(-s.retention())
	s.hist.syncArchive()
	data := recordData{Interrupted: s.hist.Interrupted, TaskDisposition: s.taskDisposition}
	for _, t := range s.hist.archive {
		if t.SessionID == "" {
			t.SessionID = s.id
		}
		if t.At.After(cutoff) {
			data.Turns = append(data.Turns, t)
		}
	}
	// Never keep expired text alive by repeatedly writing a fresh envelope.
	s.hist.archive = append([]turn(nil), data.Turns...)
	var live []turn
	for _, t := range s.hist.Turns {
		if t.At.After(cutoff) {
			live = append(live, t)
		}
	}
	s.hist.Turns = live
	if p := s.paused; p != nil {
		if p.recordAt.IsZero() {
			p.recordAt = time.Now()
		}
		if p.recordAt.After(cutoff) {
			original := p.recordSource
			if original.SessionID == "" {
				original = recordOrigin{SessionID: s.id, ItemID: p.original.itemID, Text: limitText(p.original.text, 512), Known: p.original.known, Truncated: p.original.truncated}
			}
			playback := recordPlayback{SessionID: s.id, PlaybackID: p.playback.id, Sent: p.playback.sent, Settled: p.playback.settled, Interrupted: p.playback.interrupted, Invalid: p.playback.invalid}
			if p.playback.known && !p.playback.invalid {
				n := p.playback.played
				playback.Played = &n
			}
			if p.historicalPlayback != nil && p.playback.id == 0 {
				playback = *p.historicalPlayback
			}
			latest := p.recordLatest
			if p.playback.id != 0 || latest.SessionID == "" {
				latest = recordOrigin{SessionID: s.id, ItemID: p.playback.origin.itemID, Text: limitText(p.playback.origin.text, 512), Known: p.playback.origin.known, Truncated: p.playback.origin.truncated}
			}
			data.Task = &recordTask{AwaitingResumeChoice: p.awaitingResumeChoice, Latest: latest, ResumeRequest: p.resumeRequest, At: p.recordAt, ID: p.id, Original: original, Playback: playback, SourceTaskID: p.sourceTaskID, ResumeSource: p.resumeSource}
		} else {
			s.retireTaskPlayback()
			s.paused = nil
			s.responseTaskID = ""
			s.snapshotRevision++
		}
	}
	if s.responseID != "" && s.responseID != s.records.suppressResponse && s.transcript != "" && !s.responseOrigin.wake {
		at := s.responseAt
		if at.IsZero() {
			at = time.Now()
			s.responseAt = at
		}
		data.Draft = &turn{At: at, SessionID: s.id, ResponseID: s.responseID, Role: "assistant", Delivery: "generated", GeneratedText: limitText(s.transcript, 512)}
	}
	if data.Draft != nil && len(data.Turns) > 127 {
		data.Turns = data.Turns[len(data.Turns)-127:]
	}
	return data
}
func (s *session) checkpointRecords() {
	r := s.records
	if r == nil || !r.loaded || r.clear != nil {
		return
	}
	data, _ := json.Marshal(s.recordSnapshot())
	if bytes.Equal(data, r.lastData) {
		return
	}
	r.sequence++
	receipt, err := r.store.Save(r.device, recordstore.Snapshot{Version: 1, UpdatedAt: time.Now(), Data: data})
	if err != nil {
		r.status = "failed"
		r.lastError = recordError(err)
		r.lastResult = r.sequence
		return
	}
	r.lastData = data
	r.status = "pending"
	r.pending = append(r.pending, recordReceipt{r.sequence, receipt})
}
func (s *session) pollRecords() {
	r := s.records
	if r == nil {
		return
	}
	for len(r.pending) > 0 {
		p := r.pending[0]
		select {
		case err := <-p.done:
			r.pending = r.pending[1:]
			if p.sequence >= r.lastResult {
				r.lastResult = p.sequence
				if err != nil {
					r.status = "failed"
					r.lastError = recordError(err)
				} else {
					r.status = "saved"
					r.lastError = ""
				}
			}
		default:
			return
		}
	}
	if c := r.clear; c != nil {
		select {
		case err := <-c.done:
			r.clear = nil
			output := `{"ok":true,"cleared":true}`
			if err != nil {
				r.status = "failed"
				r.lastError = recordError(err)
				output = `{"ok":false,"error":"clear_failed"}`
			} else {
				r.status = "saved"
				r.lastError = ""
				r.recoveryError = ""
				r.loaded = true
				r.lastData = nil
			}
			if call := s.toolCalls[c.toolKey]; call != nil && call.generation == c.generation {
				call.output = output
			}
			// New input remains live during disk deletion; flush it after that
			// FIFO barrier rather than claiming its text is already durable.
			if err == nil && (len(s.hist.archive) > 0 || s.paused != nil || s.transcript != "" && s.responseID != r.suppressResponse && !s.responseOrigin.wake) {
				s.checkpointRecords()
			}
		default:
		}
	}
}
func explicitClearRequest(o requestOrigin, generation uint64) bool {
	if !o.known || o.truncated || o.wake || o.generation != generation || o.itemID == "" {
		return false
	}
	// Conservative allow-list of full current requests. Embedded quoted/history
	// text, questions about ability and negated requests never authorize deletion.
	text := strings.TrimSpace(o.text)
	text = strings.TrimRight(text, "。！!，, ")
	for _, prefix := range []string{"请帮我", "请", "帮我", ""} {
		v := strings.TrimPrefix(text, prefix)
		for _, verb := range []string{"清空", "删除"} {
			for _, object := range []string{"本设备对话记录", "本设备的对话记录", "本设备所有对话记录", "本设备的所有对话记录", "对话记录", "所有对话记录", "聊天记录", "本设备聊天记录"} {
				if v == verb+object {
					return true
				}
			}
		}
	}
	return false
}
func (s *session) startClearRecords() (<-chan error, bool) {
	r := s.records
	if !explicitClearRequest(s.currentInput, s.generation) || r != nil && r.clear != nil {
		return nil, false
	}
	s.retireTaskPlayback()
	for _, p := range s.playbacks {
		p.retired = true
	}
	s.paused = nil
	s.responseTaskID = ""
	s.snapshotRevision++
	s.hist = history{}
	s.taskDisposition = "cleared"
	s.clearClarification()
	result := make(chan error, 1)
	if r == nil {
		result <- nil
		close(result)
		return result, true
	}
	r.suppressResponse = s.responseID
	done := make(chan error, 1)
	finished := make(chan struct{})
	r.clear = &recordClear{done: done, finished: finished}
	r.status = "pending"
	store, device := r.store, r.device
	go func() {
		defer close(finished)
		ctx, cancel := context.WithTimeout(context.Background(), recordClearTimeout)
		defer cancel()
		err := store.Clear(ctx, device)
		if errors.Is(err, recordstore.ErrNotFound) {
			err = nil
		}
		done <- err
		close(done)
		result <- err
		close(result)
	}()
	return result, true
}

// Run before run() clears task state. Admission precedes releasing the MAC lease;
// the next authenticated connection's FIFO Load observes accepted final writes.
func (s *session) finalizeRecords() {
	if s.records == nil {
		return
	}
	if s.records.clear != nil {
		<-s.records.clear.finished
		s.pollRecords()
	}
	if target := s.interruptiblePlayback(); target != nil {
		s.pausePlayback()
		if !target.retired && !s.wakeOnly(target.origin) {
			s.hist.Interrupted = true
		}
	}
	if s.responseID != "" && s.draftTranscript != "" && !s.responseOrigin.wake && s.responseID != s.records.suppressResponse {
		s.hist.addInterrupted(s.draftTranscript)
		s.hist.Interrupted = true
		s.hist.stampLast(s.id, s.responseID, s.responseOrigin.itemID, s.playbackSequence)
	}
	s.responseID = ""
	s.checkpointRecords()
}
func clearRecordsTool() map[string]any {
	return map[string]any{"type": "function", "function": map[string]any{"name": "conversation_clear_records", "description": "仅在本轮用户明确要求清空本设备对话记录时删除本网关文字记录和暂停任务。等待 ok=true 回执才可回答清空成功；不删除云厂商留存。", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}}
}
