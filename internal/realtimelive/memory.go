package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/recordstore"
	"strings"
	"sync"
	"time"
)

const memoryRetention = 7 * 24 * time.Hour

// These are conversation excerpts, not a person profile or model instructions.
// Source timestamps survive reconnections; loading never renews an entry's age.
type memoryTurn struct {
	SessionID string    `json:"session_id"`
	TurnID    int64     `json:"turn_id"`
	At        time.Time `json:"at"`
	User      string    `json:"user"`
	Assistant string    `json:"assistant_spoken"`
	Boundary  string    `json:"playback_boundary"`
	Source    string    `json:"text_source"`
}
type memoryData struct {
	Turns []memoryTurn `json:"turns"`
}
type sessionMemory struct {
	mu     sync.Mutex // Serializes load, snapshot and clear; then Session.mu.
	store  *recordstore.Store
	base   []memoryTurn
	loaded bool
	wake   chan struct{}
}

func (s *Server) attachMemory(session *Session) {
	if s.memory != nil && session.ownerErr == nil && session.rec.owner.DeviceID != "" {
		session.memory = &sessionMemory{store: s.memory, wake: make(chan struct{}, 1)}
	}
}

func clipMemory(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

func boundedMemory(turns []memoryTurn, now time.Time) []memoryTurn {
	result := []memoryTurn{}
	for _, turn := range turns {
		if turn.At.IsZero() || !turn.At.After(now.Add(-memoryRetention)) || turn.At.After(now) || len(turn.SessionID) != 32 || turn.TurnID < 1 {
			continue
		}
		turn.User, turn.Assistant = clipMemory(turn.User, 512), clipMemory(turn.Assistant, 1024)
		if strings.TrimSpace(turn.User) == "" && strings.TrimSpace(turn.Assistant) == "" {
			continue
		}
		result = append(result, turn)
	}
	if len(result) > 24 {
		result = result[len(result)-24:]
	}
	total := 0
	for i := len(result) - 1; i >= 0; i-- {
		total += len([]rune(result[i].User)) + len([]rune(result[i].Assistant))
		if total > 24000 {
			return result[i+1:]
		}
	}
	return result
}

func (s *Session) loadMemory() error {
	m := s.memory
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	v, err := m.store.Load(ctx, s.rec.owner.DeviceID)
	if errors.Is(err, recordstore.ErrNotFound) {
		m.loaded = true
		return s.record("memory_loaded", map[string]any{"turns": 0})
	}
	if err != nil {
		return err
	}
	var data memoryData
	if json.Unmarshal(v.Data, &data) != nil || len(data.Turns) > 24 {
		return errors.New("invalid_memory_snapshot")
	}
	m.base = boundedMemory(data.Turns, time.Now())
	m.loaded = true
	return s.record("memory_loaded", map[string]any{"turns": len(m.base)})
}

// Called only with memory.mu held. Snapshot mutations are never done from a
// historical prompt or client-supplied device ID.
func (s *Session) memorySnapshotLocked() memoryData {
	m := s.memory
	turns := append([]memoryTurn(nil), m.base...)
	s.mu.Lock()
	for _, rt := range s.history {
		if rt.turn.ID < s.memoryFloor || (rt.turn.User == "" && rt.turn.Completed == 0) {
			continue
		}
		evidence := realtimestate.PlaybackEvidence(rt.turn)
		assistant := ""
		if rt.turn.Completed > 0 && evidence.SpokenText != nil {
			assistant = *evidence.SpokenText
		}
		turns = append(turns, memoryTurn{s.rec.owner.SessionID, rt.turn.ID, rt.stoppedAt, rt.turn.User, assistant, evidence.Boundary, evidence.TextSource})
	}
	s.mu.Unlock()
	return memoryData{boundedMemory(turns, time.Now())}
}

func (s *Session) saveMemoryLocked(data memoryData) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	receipt, err := s.memory.store.Save(s.rec.owner.DeviceID, recordstore.Snapshot{Version: 1, UpdatedAt: time.Now().UTC(), Data: encoded})
	if err != nil {
		return err
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-receipt:
		return err
	case <-timer.C:
		return errors.New("memory_write_outcome_unknown")
	}
}

func (s *Session) checkpointMemory() error {
	m := s.memory
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return errors.New("memory_not_loaded")
	}
	return s.saveMemoryLocked(s.memorySnapshotLocked())
}

// record() only schedules work. Disk latency never holds the audio/session lock.
func (s *Session) notifyMemory(event string) {
	if s.memory == nil {
		return
	}
	switch event {
	case "user_text", "device_playback", "playback", "output_asr_final", "generated", "generated_interrupted":
		select {
		case s.memory.wake <- struct{}{}:
		default:
		}
	}
}
func (s *Session) runMemory() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.memory.wake:
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		// Coalesce progress events without losing any later notification.
		select {
		case <-s.memory.wake:
		default:
		}
		if err := s.checkpointMemory(); err != nil {
			s.record("memory_write", map[string]any{"outcome": "failed_or_unknown"})
		} else {
			s.record("memory_write", map[string]any{"outcome": "saved"})
		}
	}
}

func memoryClearCommand(text string) bool {
	text = strings.TrimSpace(text)
	// Questions, quotes, compound requests and other targets never become clear.
	if strings.ContainsAny(text, "?？\"“”‘’\n") {
		return false
	}
	text = strings.TrimRight(text, "。.!！ ")
	if strings.HasPrefix(text, "小满") {
		text = strings.TrimLeft(strings.TrimPrefix(text, "小满"), "，, ")
	}
	for _, prefix := range []string{"请帮我", "帮我", "请"} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}
	switch text {
	case "清除聊天记忆", "清空聊天记忆", "删除聊天记忆", "清除这台设备的聊天记忆", "清空这台设备的聊天记忆", "删除这台设备的聊天记忆", "忘记之前的聊天", "忘掉之前的聊天", "忘记我们之前的聊天":
		return true
	}
	return false
}

func (s *Session) clearMemory(rt *liveTurn) error {
	m := s.memory
	if m == nil {
		return errors.New("memory_unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := rt.ctx.Err(); err != nil {
		return err
	}
	// Reset before admission, including unknown writes. Old in-flight progress
	// must never repopulate the local context after a deletion was submitted.
	s.mu.Lock()
	s.memoryFloor = max(s.memoryFloor, rt.turn.ID+1)
	s.mu.Unlock()
	m.base = nil
	err := s.saveMemoryLocked(memoryData{Turns: []memoryTurn{}})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := m.store.Load(ctx, s.rec.owner.DeviceID)
	if err != nil {
		return err
	}
	var data memoryData
	if json.Unmarshal(v.Data, &data) != nil || len(data.Turns) != 0 {
		return errors.New("memory_clear_unverified")
	}
	m.loaded = true
	return nil
}

// mergeMemory is bounded and extractive. It never reads the audit log or sends
// any extra model request. Historical utterances are data, not tool requests.
func (s *Session) mergeMemory(text string) string {
	m := s.memory
	if m == nil {
		return text
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// A newer clear can finish after settle prepared the current-session JSON.
	// Revalidate that JSON while holding the clear's lock, even with no base.
	s.mu.Lock()
	floor := s.memoryFloor
	s.mu.Unlock()
	if floor > 0 {
		data, err := decodeObject([]byte(text))
		if err != nil {
			return realtimestate.ContextFor(nil)
		}
		kept := []any{}
		for _, raw := range listMemoryTurns(data["recent_turns"]) {
			turn, _ := raw.(map[string]any)
			if id, ok := integer(turn["id"]); ok && id >= floor {
				kept = append(kept, raw)
			}
		}
		data["recent_turns"] = kept
		delete(data, "prior_session_memory")
		encoded, _ := json.Marshal(data)
		text = string(encoded)
	}
	if !m.loaded {
		return text
	}
	turns := boundedMemory(m.base, time.Now())
	if len(turns) == 0 {
		return text
	}
	recent := []memoryTurn{}
	budget := 6000
	start := len(turns)
	for i := len(turns) - 1; i >= max(0, len(turns)-8); i-- {
		n := len([]rune(turns[i].User)) + len([]rune(turns[i].Assistant))
		if n > budget {
			break
		}
		budget -= n
		start = i
		recent = append([]memoryTurn{turns[i]}, recent...)
	}
	excerpts := []memoryTurn{}
	for _, turn := range turns[max(0, start-6):start] {
		turn.User, turn.Assistant = clipMemory(turn.User, 100), clipMemory(turn.Assistant, 180)
		excerpts = append(excerpts, turn)
	}
	var base map[string]any
	if json.Unmarshal([]byte(text), &base) != nil {
		return text
	}
	base["prior_session_memory"] = map[string]any{"scope": "current_authenticated_device", "retention_days": 7, "summary_mode": "extractive", "older_excerpts": excerpts, "recent_turns": recent, "recognition_may_differ": true}
	encoded, _ := json.Marshal(base)
	return string(encoded)
}

func listMemoryTurns(value any) []any { turns, _ := value.([]any); return turns }

func (s *Session) routeMemory(rt *liveTurn, history string) (string, string, error) {
	if s.memory == nil {
		return history, "", nil
	}
	started := time.Now()
	if err := waitTurnTranscript(rt); err != nil {
		return "", "", err
	}
	s.mu.Lock()
	text := rt.turn.User
	s.mu.Unlock()
	if !memoryClearCommand(text) {
		outcome := "transcript_unavailable"
		if text != "" {
			outcome = "verified_saved"
			if err := s.checkpointMemory(); err != nil {
				outcome = "failed_or_unknown"
			}
		}
		if e := s.record("memory_checkpoint", map[string]any{"id": rt.turn.ID, "outcome": outcome, "elapsed_ms": time.Since(started).Milliseconds()}); e != nil {
			return "", "", e
		}
		if rt.ctx.Err() != nil {
			return "", "", rt.ctx.Err()
		}
		status := "本轮新输入尚未核实保存，不要承诺已经记住。"
		if outcome == "verified_saved" {
			status = "本轮新输入已核实保存到当前设备的有界近期记忆。"
		}
		return history, "\n本轮用户输入的近期记忆写入回执：" + outcome + "。" + status + "不保证永久记住，不编造保存或删除成功。不要念后台状态，依据已有对话自然回答。\n", nil
	}
	err := s.clearMemory(rt)
	outcome := "verified_cleared"
	if err != nil {
		outcome = "failed_or_unknown"
	}
	if e := s.record("memory_clear", map[string]any{"id": rt.turn.ID, "outcome": outcome, "elapsed_ms": time.Since(started).Milliseconds()}); e != nil {
		return "", "", e
	}
	if rt.ctx.Err() != nil {
		return "", "", rt.ctx.Err()
	}
	// Never reuse the already-built context after resetting the clear boundary.
	history = realtimestate.ContextFor(nil)
	status := "本轮清除失败或结果未核实，不声称已清除。"
	if outcome == "verified_cleared" {
		status = "当前设备可恢复的聊天记忆已清除并回读核实。"
	}
	guidance := "\n本轮记忆操作回执：" + outcome + "。" + status + "当前清除操作不删除服务器原始文字日志，不清除其他设备，也不影响今后新对话的记忆。以上是内部操作事实，不要照读。用自己的话简短自然地说明聊天记忆是否清除，不念状态码，不说回读、核实、可恢复等后台术语；不能将聊天记忆说成全部聊天记录或原始日志已删除。除非用户询问删除范围，不主动解释存储机制。\n"
	return history, guidance, nil
}

func waitTurnTranscript(rt *liveTurn) error {
	rt.transcriptWait.Do(func() {
		timer := time.NewTimer(300 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-rt.ctx.Done():
		case <-rt.transcriptReady:
		case <-timer.C:
		}
	})
	return rt.ctx.Err()
}
