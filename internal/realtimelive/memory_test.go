package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/recordstore"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const memoryDeviceA = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const memoryDeviceB = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func memoryServer(t *testing.T, root string) *Server {
	t.Helper()
	s := NewServer(Config{MemoryEnabled: true, MaxSessions: 2}, nil, root, "memory-test")
	if s.InitializationError() != nil || s.memory == nil {
		t.Fatalf("memory initialization: %v", s.InitializationError())
	}
	t.Cleanup(func() {
		if err := s.CloseActive(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return s
}

func memorySession(t *testing.T, server *Server, device string) *Session {
	t.Helper()
	dir, err := server.sessionDirectory(Ownership{DeviceID: device, ClientType: "mac_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(nil, server.cfg, dir, nil)
	server.attachMemory(s)
	if err = s.loadMemory(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.cancel)
	return s
}

func rememberedTurn(s *Session, user, assistant string, played bool) *liveTurn {
	rt := s.addTurn()
	rt.turn.User, rt.turn.Generated = user, assistant
	rt.turn.Sent = 24000
	if played {
		rt.turn.Completed, rt.turn.Ended = 24000, true
	}
	rt.transcriptOnce.Do(func() { close(rt.transcriptReady) })
	return rt
}

func memoryContext(t *testing.T, s *Session) string {
	t.Helper()
	rt := s.addTurn()
	text, err := s.settle(context.Background(), rt)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestMemoryReconnectRestartAndDeviceIsolation(t *testing.T) {
	root := t.TempDir()
	server := memoryServer(t, root)
	a := memorySession(t, server, memoryDeviceA)
	rememberedTurn(a, "我的临时暗号是蓝色风筝", "我听到了蓝色风筝", true)
	rememberedTurn(a, "讲故事", "未播秘密后半稿", false)
	if err := a.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	b := memorySession(t, server, memoryDeviceB)
	if got := memoryContext(t, b); strings.Contains(got, "蓝色风筝") {
		t.Fatal("cross-device leak", got)
	}
	if err := server.CloseActive(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := memoryServer(t, root)
	next := memorySession(t, restarted, memoryDeviceA)
	got := memoryContext(t, next)
	if !strings.Contains(got, "蓝色风筝") || !strings.Contains(got, "prior_session_memory") || strings.Contains(got, "未播秘密后半稿") {
		t.Fatal("restore must include only evidenced text", got)
	}
}

func TestMemoryPartialEvidenceAndLatePlaybackAfterClear(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	rt := rememberedTurn(s, "讲故事", "小猫出门。藏起来的未播秘密。", false)
	rt.turn.Completed, rt.turn.Cut = 12000, true
	rt.turn.Tracker.Add(map[string]any{"sentence_id": 1, "sentence_end": true, "text": "小猫出门。", "words": []any{map[string]any{"text": "小猫出门。", "begin_time": 0, "end_time": 200}}}, 300)
	if err := s.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	next := memorySession(t, server, memoryDeviceA)
	got := memoryContext(t, next)
	if !strings.Contains(got, "小猫出门") || strings.Contains(got, "藏起来的未播秘密") {
		t.Fatal(got)
	}
	clear := rememberedTurn(s, "清除聊天记忆", "", false)
	if err := s.clearMemory(clear); err != nil {
		t.Fatal(err)
	}
	// The player may report an old turn after the clear. It must not revive it.
	rt.turn.Completed, rt.turn.Ended, rt.turn.Cut = rt.turn.Sent, true, false
	if err := s.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	if got := memoryContext(t, s); strings.Contains(got, "小猫出门") {
		t.Fatal("current context revived", got)
	}
	again := memorySession(t, server, memoryDeviceA)
	if got := memoryContext(t, again); strings.Contains(got, "小猫出门") {
		t.Fatal("persistent memory revived", got)
	}
	rememberedTurn(s, "清除以后新暗号是橙子", "橙子", true)
	if err := s.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	if got := memoryContext(t, memorySession(t, server, memoryDeviceA)); !strings.Contains(got, "橙子") {
		t.Fatal("new memory disabled", got)
	}
}

func TestMemoryExplicitClearCommandsOnly(t *testing.T) {
	for _, text := range []string{"清除聊天记忆", "小满，请清空聊天记忆。", "请帮我清除这台设备的聊天记忆", "忘记我们之前的聊天"} {
		if !memoryClearCommand(text) {
			t.Errorf("clear rejected: %s", text)
		}
	}
	for _, text := range []string{"从头开始", "不要清除聊天记忆", "他说清除聊天记忆", "清除聊天记忆吗", "清除聊天记忆？", "清除微雪的聊天记忆", "清除另一台设备的聊天记忆", "“清除聊天记忆”", "清除聊天记忆是什么意思", "清除聊天记忆然后调音量"} {
		if memoryClearCommand(text) {
			t.Errorf("unsafe clear accepted: %s", text)
		}
	}
}

func TestMemoryClearSerializedWithCheckpoint(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	rememberedTurn(s, "旧内容不能复活", "旧内容", true)
	clear := rememberedTurn(s, "清除聊天记忆", "", false)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.checkpointMemory(); err != nil {
				t.Error(err)
			}
		}()
	}
	if err := s.clearMemory(clear); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if got := memoryContext(t, memorySession(t, server, memoryDeviceA)); strings.Contains(got, "旧内容") {
		t.Fatal(got)
	}
}

func TestMemoryBoundsAgesAndSourceIdentity(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	for i := range 40 {
		rt := rememberedTurn(s, strings.Repeat("用", 700), strings.Repeat("答", 1600), true)
		if i < 10 {
			rt.stoppedAt = time.Now().Add(-8 * 24 * time.Hour)
			rt.turn.User = "过期内容"
		}
	}
	if err := s.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	v, err := server.memory.Load(context.Background(), memoryDeviceA)
	if err != nil {
		t.Fatal(err)
	}
	var data memoryData
	if err = json.Unmarshal(v.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Turns) == 0 || len(data.Turns) > 24 || strings.Contains(string(v.Data), "过期内容") {
		t.Fatal("unbounded or expired", len(data.Turns))
	}
	for _, turn := range data.Turns {
		if turn.SessionID != s.rec.owner.SessionID || turn.TurnID == 0 || turn.At.IsZero() {
			t.Fatal("missing provenance", turn)
		}
	}
	got := memoryContext(t, memorySession(t, server, memoryDeviceA))
	if len([]rune(got)) > 14000 || !strings.Contains(got, "extractive") {
		t.Fatal("unbounded view", len([]rune(got)))
	}
}

func TestMemoryNoRenewalOfOldTurns(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	rt := rememberedTurn(s, "昨天的原始内容", "已经说过", true)
	rt.stoppedAt = time.Now().Add(-24 * time.Hour)
	if err := s.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	next := memorySession(t, server, memoryDeviceA)
	if err := next.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	v, err := server.memory.Load(context.Background(), memoryDeviceA)
	if err != nil {
		t.Fatal(err)
	}
	var data memoryData
	json.Unmarshal(v.Data, &data)
	if len(data.Turns) != 1 || !data.Turns[0].At.Equal(rt.stoppedAt) {
		t.Fatal("renewed old memory", string(v.Data))
	}
}

func TestMemoryFailureAndUnattributedClientDoNotClaimSuccess(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	rt := rememberedTurn(s, "清除聊天记忆", "", false)
	if err := server.memory.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.clearMemory(rt); err == nil {
		t.Fatal("failed clear reported success")
	}
	legacy := NewSession(nil, server.cfg, t.TempDir(), nil)
	server.attachMemory(legacy)
	if legacy.memory != nil {
		t.Fatal("unattributed client received memory")
	}
	legacy.cancel()
}

func TestMemoryDirectoryDoesNotAppearAsConversation(t *testing.T) {
	root := t.TempDir()
	server := memoryServer(t, root)
	s := memorySession(t, server, memoryDeviceA)
	rememberedTurn(s, "test", "test", true)
	if err := s.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	entries, err := ListRecords(root, "", "")
	if err != nil || len(entries) != 1 {
		t.Fatalf("record listing: %d %v", len(entries), err)
	}
	info, err := os.Stat(filepath.Join(root, ".memory"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("memory directory permissions", err)
	}
}

func TestMemoryClipHasExactBound(t *testing.T) {
	if got := clipMemory(strings.Repeat("字", 1000), 512); len([]rune(got)) != 512 {
		t.Fatal("clip exceeds advertised bound", len([]rune(got)))
	}
}

func TestMemoryBackgroundCheckpointAndCanceledClear(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	s.spawn(s.runMemory)
	rt := rememberedTurn(s, "后台保存的暗号是风车", "风车", true)
	if err := s.record("playback", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		v, err := server.memory.Load(context.Background(), memoryDeviceA)
		if err == nil && strings.Contains(string(v.Data), "风车") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("checkpoint never saved", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	rt.cancel()
	if err := s.clearMemory(rt); err == nil {
		t.Fatal("canceled clear executed")
	}
	if got := memoryContext(t, memorySession(t, server, memoryDeviceA)); !strings.Contains(got, "风车") {
		t.Fatal("canceled clear erased state", got)
	}
	s.cancel()
	s.wg.Wait()
}

func TestMemoryRouteClearRebuildsContextAndRecordsReceipt(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	rememberedTurn(s, "不能泄漏的旧问题", "不能泄漏的旧答案", true)
	rt := rememberedTurn(s, "清除聊天记忆", "", false)
	history, receipt, err := s.routeMemory(rt, memoryContext(t, s))
	if err != nil || strings.Contains(history, "不能泄漏") || !strings.Contains(receipt, "verified_cleared") {
		t.Fatal(history, receipt, err)
	}
	data, err := os.ReadFile(filepath.Join(s.directory, "events.jsonl"))
	if err != nil || !strings.Contains(string(data), `"event":"memory_clear"`) {
		t.Fatal("missing operation receipt", err)
	}
}

func TestMemoryReplyPromptUsesRestoredContextAndVerifiedClear(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	old := memorySession(t, server, memoryDeviceA)
	rememberedTurn(old, "旧暗号蓝色风筝", "蓝色风筝", true)
	if err := old.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	s := memorySession(t, server, memoryDeviceA)
	ws, _ := testSocket(t)
	s.ws, s.wire = ws, &macWire{ws: ws}
	product, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	s.product = product
	prompts := []string{}
	s.generate = func(ctx context.Context, id int64, prompt string, pcm []byte, emit func(string, []byte) error) error {
		prompts = append(prompts, prompt)
		return context.Canceled
	}
	query := rememberedTurn(s, "上次的暗号是什么", "", false)
	s.reply(query, make([]byte, 640))
	if len(prompts) != 1 || !strings.Contains(prompts[0], "蓝色风筝") || strings.Contains(prompts[0], "不能查询以前会话") {
		t.Fatal("missing bounded memory capability", prompts)
	}
	clear := rememberedTurn(s, "清除聊天记忆", "", false)
	s.reply(clear, make([]byte, 640))
	if len(prompts) != 2 || strings.Contains(prompts[1], "蓝色风筝") || !strings.Contains(prompts[1], "verified_cleared") {
		t.Fatal("clear used stale prompt", prompts)
	}
}

func TestMemoryRouteAcknowledgesOnlyPersistedCurrentInput(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	rt := rememberedTurn(s, "请记住偏好是紫色", "", false)
	_, receipt, err := s.routeMemory(rt, realtimestate.ContextFor(nil))
	if err != nil || !strings.Contains(receipt, "verified_saved") {
		t.Fatal("no write receipt", receipt, err)
	}
	v, err := server.memory.Load(context.Background(), memoryDeviceA)
	if err != nil || !strings.Contains(string(v.Data), "紫色") {
		t.Fatal("acknowledged before durable save", err)
	}
	if err := server.memory.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := rememberedTurn(s, "另一个新偏好是绿色", "", false)
	_, receipt, err = s.routeMemory(next, realtimestate.ContextFor(nil))
	if err != nil || !strings.Contains(receipt, "failed_or_unknown") || strings.Contains(receipt, "verified_saved") {
		t.Fatal("failed save claimed success", receipt, err)
	}
}

func TestMemoryAndControlsShareTranscriptWaitBudget(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	s.controls = &deviceControls{} // No text means no discovery or model request.
	rt := s.addTurn()
	started := time.Now()
	history, _, err := s.routeMemory(rt, realtimestate.ContextFor(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.routeDeviceControls(rt, history); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatal("stacked transcription waits", elapsed)
	}
}

func TestMemoryCannotOverflowExistingControlContextBudget(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"recent_turns": []any{map[string]any{"user": strings.Repeat("a", 55000)}}, "prior_session_memory": map[string]any{"summary_mode": "extractive", "recent_turns": strings.Repeat("b", 15000)}})
	got := controlHistory(string(data))
	if len(got) > 65536 || !strings.Contains(got, strings.Repeat("a", 55000)) || strings.Contains(got, "prior_session_memory") {
		t.Fatal("restored memory broke control planner budget", len(got))
	}
	if got := controlHistory(`{"recent_turns":[]}`); got != `{"recent_turns":[]}` {
		t.Fatal("changed existing control history", got)
	}
}

func TestMemoryClearInvalidatesContextPreparedBeforeClear(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	old := rememberedTurn(s, "不能回流的旧暗号", "不能回流的旧回答", true)
	prepared := realtimestate.ContextFor([]*realtimestate.Turn{old.turn})
	clear := rememberedTurn(s, "清除聊天记忆", "", false)
	if err := s.clearMemory(clear); err != nil {
		t.Fatal(err)
	}
	// A new reply can have prepared context while a previous clear is being
	// synced. Revalidate its boundary under the same lock as that clear.
	if got := s.mergeMemory(prepared); strings.Contains(got, "不能回流") {
		t.Fatal("in-flight context survived a verified clear", got)
	}
}

func TestMemoryCorruptPayloadIsUnavailableUntilExplicitClear(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	bad := json.RawMessage(`{"turns":"invalid schema"}`)
	receipt, err := server.memory.Save(memoryDeviceA, recordstore.Snapshot{Version: 1, UpdatedAt: time.Now(), Data: bad})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-receipt; err != nil {
		t.Fatal(err)
	}
	dir, err := server.sessionDirectory(Ownership{DeviceID: memoryDeviceA, ClientType: "mac_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(nil, server.cfg, dir, nil)
	defer s.cancel()
	server.attachMemory(s)
	if s.loadMemory() == nil || s.sessionFacts()["device_memory_enabled"] == true {
		t.Fatal("invalid payload advertised as memory")
	}
	if s.checkpointMemory() == nil {
		t.Fatal("unavailable snapshot silently overwritten")
	}
	rt := rememberedTurn(s, "清除聊天记忆", "", false)
	if err := s.clearMemory(rt); err != nil {
		t.Fatal(err)
	}
	if s.sessionFacts()["device_memory_enabled"] != true {
		t.Fatal("explicit clear did not restore clean memory capability")
	}
}
