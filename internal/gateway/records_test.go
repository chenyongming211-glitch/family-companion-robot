package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/qwen"
	"familyrobot/internal/recordstore"
)

func recordSession(t *testing.T, store recordBackend, device string) *session {
	t.Helper()
	s := newSession(context.Background(), nil, Config{RecordRetention: 168 * time.Hour}, 1)
	s.loadRecords(store, device)
	return s
}
func flushRecords(t *testing.T, s *session) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for s.records.pendingCount() != 0 && time.Now().Before(until) {
		s.pollRecords()
		time.Sleep(time.Millisecond)
	}
	s.pollRecords()
	if s.records.pendingCount() != 0 {
		t.Fatal("record writes did not finish")
	}
}
func TestRecordsReopenPreservesTaskWithoutPlaybackReuse(t *testing.T) {
	dir := t.TempDir()
	store, e := recordstore.Open(recordstore.Options{Directory: dir, Retention: 168 * time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	s := recordSession(t, store, "c4:9e:7e:12:99:10")
	s.hist.add("user", "从一完整数到五十")
	s.hist.addInterrupted("一二三四五六七八九")
	s.paused = &pausedTask{id: "old-task", original: requestOrigin{text: "从一完整数到五十", known: true, itemID: "old-input"}, playback: playbackRecord{id: 7, sent: 14400, played: 7200, known: true, settled: true, interrupted: true}}
	s.checkpointRecords()
	flushRecords(t, s)
	if s.recordingCapabilities()["persistence_status"] != "saved" {
		t.Fatal(s.recordingCapabilities())
	}
	if e = store.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	store, e = recordstore.Open(recordstore.Options{Directory: dir, Retention: 168 * time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close(context.Background())
	restored := recordSession(t, store, "c4:9e:7e:12:99:10")
	got := restored.pausedData()
	if got["original_request"] != "从一完整数到五十" || got["text_position"] != "unknown" || got["position_known"] != false || got["played_samples"] != nil {
		t.Fatalf("unsafe restore: %v", got)
	}
	if restored.paused.id == "old-task" || !strings.HasPrefix(restored.paused.id, restored.id+":") {
		t.Fatal("task identity reused")
	}
	if got["historical_playback"] == nil || restored.responseTaskID != "" || restored.awaitingResponse {
		t.Fatal("restore executed task or lost historical evidence")
	}
	if strings.Contains(restored.hist.context(), "一二三四五六七八九") {
		t.Fatal("interrupted draft leaked into context")
	}
	other := recordSession(t, store, "c4:9e:7e:12:99:11")
	if other.paused != nil || len(other.hist.Turns) != 0 {
		t.Fatal("cross-device leak")
	}
	args, _ := json.Marshal(map[string]any{"task_id": restored.paused.id, "expected_revision": restored.snapshotRevision, "action": "discard"})
	restored.conversationTool("conversation_update_paused_task", string(args))
	restored.checkpointRecords()
	flushRecords(t, restored)
	again := recordSession(t, store, "c4:9e:7e:12:99:10")
	if again.paused != nil {
		t.Fatal("discarded task resurrected")
	}
}
func TestRecordsClearRequiresCurrentExplicitRequestAndReceipt(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	s := recordSession(t, st, "one")
	s.hist.add("user", "private words")
	s.checkpointRecords()
	flushRecords(t, s)
	if _, ok := s.startClearRecords(); ok {
		t.Fatal("cleared without explicit input")
	}
	s.generation = 2
	s.currentInput = requestOrigin{generation: 2, itemID: "clear", known: true, text: "请清空本设备的对话记录"}
	receipt, ok := s.startClearRecords()
	if !ok {
		t.Fatal("explicit clear rejected")
	}
	if len(s.hist.Turns) != 0 || s.paused != nil {
		t.Fatal("clear retained live state")
	}
	if s.recordingCapabilities()["persistence_status"] == "saved" {
		t.Fatal("clear claimed success before receipt")
	}
	select {
	case err := <-receipt:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("clear blocked")
	}
	s.pollRecords()
	again := recordSession(t, st, "one")
	if len(again.hist.Turns) != 0 {
		t.Fatal("clear resurrected words")
	}
}

type brokenRecordStore struct {
	err     error
	receipt chan error
	loads   int
}

func (b *brokenRecordStore) Load(context.Context, string) (recordstore.Snapshot, error) {
	b.loads++
	return recordstore.Snapshot{}, b.err
}
func (b *brokenRecordStore) Save(string, recordstore.Snapshot) (<-chan error, error) {
	if b.receipt != nil {
		return b.receipt, nil
	}
	return nil, b.err
}
func (b *brokenRecordStore) Clear(context.Context, string) error { return b.err }
func TestRecordsFailuresAndPendingAreTruthful(t *testing.T) {
	b := &brokenRecordStore{err: errors.New("disk failed")}
	s := recordSession(t, b, "one")
	if s.recordingCapabilities()["recovery_error"] == "" {
		t.Fatal("hidden recovery error")
	}
	s.hist.add("user", "new")
	s.checkpointRecords()
	if s.records.pendingCount() != 0 {
		t.Fatal("overwrote failed load")
	}
	b.err = recordstore.ErrNotFound
	s = recordSession(t, b, "one")
	b.err = recordstore.ErrBusy
	s.hist.add("user", "one")
	s.checkpointRecords()
	if s.recordingCapabilities()["persistence_status"] != "failed" {
		t.Fatal("queue full claimed success")
	}
	b.receipt = make(chan error, 1)
	s.hist.add("user", "two")
	s.checkpointRecords()
	if s.recordingCapabilities()["persistence_status"] != "pending" {
		t.Fatal("admission claimed durable")
	}
	b.receipt <- errors.New("fsync failed")
	flushRecords(t, s)
	if s.recordingCapabilities()["persistence_status"] != "failed" {
		t.Fatal("write failure hidden")
	}
}
func TestRecordsArchiveBoundAndIndividualExpiry(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: 168 * time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	s := recordSession(t, st, "one")
	for i := 0; i < 140; i++ {
		s.hist.add("user", strings.Repeat("字", 600))
	}
	s.checkpointRecords()
	flushRecords(t, s)
	raw, e := st.Load(context.Background(), "one")
	if e != nil {
		t.Fatal(e)
	}
	var data recordData
	if e = json.Unmarshal(raw.Data, &data); e != nil {
		t.Fatal(e)
	}
	if len(data.Turns) != 128 || len([]rune(data.Turns[0].Text)) != 512 {
		t.Fatal("archive not bounded")
	}
	data.Turns[0].At = time.Now().Add(-169 * time.Hour)
	data.Task = &recordTask{At: time.Now().Add(-169 * time.Hour), ID: "expired", Original: recordOrigin{Text: "expired", Known: true}}
	raw.Data, _ = json.Marshal(data)
	receipt, e := st.Save("one", raw)
	if e != nil {
		t.Fatal(e)
	}
	if e = <-receipt; e != nil {
		t.Fatal(e)
	}
	restored := recordSession(t, st, "one")
	if restored.paused != nil || len(restored.hist.archive) != 127 || len(restored.hist.Turns) != 12 {
		t.Fatal("per-item expiry or context bound failed")
	}
}
func TestRecordConfigurationFailsClosed(t *testing.T) {
	cfg := testConfig("ws://127.0.0.1:1")
	cfg.RecordDirectory = filepath.Join(t.TempDir(), "records")
	cfg.RecordRetention = -time.Hour
	if s, e := New(cfg); e == nil {
		s.Close()
		t.Fatal("invalid retention accepted")
	}
}

func TestClearProvenanceRejectsQuotedNegatedAndHypothetical(t *testing.T) {
	for _, text := range []string{"不要清空记录", "不要清空本设备的对话记录", "如果我说清空记录会怎样", "你能清空对话记录吗", "他说：请清空本设备的对话记录", "请朗读：清空对话记录"} {
		if explicitClearRequest(requestOrigin{generation: 1, itemID: "now", known: true, text: text}, 1) {
			t.Fatalf("unsafe clear authorization: %s", text)
		}
	}
	if explicitClearRequest(requestOrigin{generation: 1, itemID: "wake", known: true, text: "清空对话记录", wake: true}, 1) {
		t.Fatal("wake authorized deletion")
	}
}
func TestRecordsDiscardDispositionRemainsAuthoritativeAfterRecovery(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	s := recordSession(t, st, "one")
	s.hist.add("user", "数到五十")
	s.paused = &pausedTask{id: "old", original: requestOrigin{text: "数到五十", known: true}}
	s.conversationTool("conversation_update_paused_task", `{"task_id":"old","expected_revision":0,"action":"discard"}`)
	s.checkpointRecords()
	flushRecords(t, s)
	next := recordSession(t, st, "one")
	instructions := next.snapshotInstructions()
	if !strings.Contains(instructions, `"task_disposition":"discarded"`) || !strings.Contains(instructions, `"archived_tasks_executable":false`) || !strings.Contains(next.hist.context(), "数到五十") {
		t.Fatal("historical background lost or promoted to executable")
	}
}
func TestRecordsReceiptChangeRequiresFreshSnapshotACK(t *testing.T) {
	b := &brokenRecordStore{err: recordstore.ErrNotFound, receipt: make(chan error, 1)}
	s := recordSession(t, b, "one")
	s.hist.add("user", "hello")
	s.checkpointRecords()
	p := attachSnapshotModel(t, s)

	if e := s.requestResponse(); e != nil {
		t.Fatal(e)
	}
	before := p.read(t).body["session"].(map[string]any)["instructions"].(string)
	if !strings.Contains(before, `"persistence_status":"pending"`) {
		t.Fatal(before)
	}
	b.receipt <- nil
	ackSnapshotWire(t, s, p) // ACK of pending facts cannot create a response.
	after := p.read(t).body["session"].(map[string]any)["instructions"].(string)
	if !strings.Contains(after, `"persistence_status":"saved"`) {
		t.Fatal(after)
	}
	ackSnapshotWire(t, s, p)
	p.want(t, "response.create")
}
func TestRecordsWakeTranscriptionNeverPersistsBeforeDetect(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.generation = 1
	if e := s.onModel(context.Background(), qwen.Event{Type: "conversation.item.input_audio_transcription.completed", ItemID: "cached", Transcript: "你好小智"}); e != nil {
		t.Fatal(e)
	}
	if len(s.hist.Turns) != 0 {
		t.Fatal("pre-detect cache retained as business text")
	}
}

func TestRecordsSameDeviceExclusionAndShutdownDrain(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig("ws://127.0.0.1:1")
	cfg.RecordDirectory = dir
	cfg.MaxSessions = 2
	g, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(g.Handler())
	defer server.Close()
	defer g.Close()
	dial := func(mac string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testToken}, "Device-Id": []string{mac}})
	}
	c, _, e := dial("C4-9E-7E-12-99-10")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if other, res, e := dial("c4:9e:7e:12:99:10"); e == nil {
		other.Close()
		t.Fatal("duplicate normalized MAC admitted")
	} else if res.StatusCode != http.StatusConflict {
		t.Fatal(res.Status)
	}
	other, _, e := dial("c4:9e:7e:12:99:11")
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	receipt, e := g.records.Save("one", recordstore.Snapshot{Version: 1, UpdatedAt: time.Now(), Data: json.RawMessage(`{"turns":[]}`)})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { g.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not release active readers")
	}
	if e = <-receipt; e != nil {
		t.Fatal(e)
	}
	reopened, e := recordstore.Open(recordstore.Options{Directory: dir, Retention: 168 * time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close(context.Background())
	if _, e = reopened.Load(context.Background(), "one"); e != nil {
		t.Fatal("accepted write was not drained", e)
	}
	g.Close() // Idempotent including store ownership.
}
func TestRecordsNoopTickDoesNotEnqueue(t *testing.T) {
	b := &brokenRecordStore{err: recordstore.ErrNotFound, receipt: make(chan error, 1)}
	s := recordSession(t, b, "one")
	s.hist.add("user", "one")
	s.checkpointRecords()
	before := s.records.sequence
	for i := 0; i < 100; i++ {
		s.checkpointRecords()
	}
	if s.records.sequence != before {
		t.Fatal("unchanged snapshot repeatedly saved")
	}
}

type slowClosingRecords struct {
	brokenRecordStore
	release chan struct{}
}

func (b *slowClosingRecords) Close(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
		return nil
	}
}
func TestRecordShutdownBoundsSlowStore(t *testing.T) {
	g, e := New(testConfig("ws://127.0.0.1:1"))
	if e != nil {
		t.Fatal(e)
	}
	slow := &slowClosingRecords{release: make(chan struct{})}
	g.records = slow
	defer close(slow.release)
	done := make(chan struct{})
	go func() { g.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("slow store blocked shutdown beyond deadline")
	}
}

func TestRecordsNewTaskAfterDiscardHasActiveDisposition(t *testing.T) {
	s := newSession(context.Background(), nil, Config{}, 1)
	s.taskDisposition = "discarded"
	s.started = true
	s.activePlayback = &playbackRecord{id: 1, origin: requestOrigin{text: "新任务", known: true}}
	s.pausePlayback()
	if s.taskDisposition != "active" {
		t.Fatal("new task inherited discard disposition")
	}
	if !strings.HasPrefix(s.paused.id, s.id+":") {
		t.Fatal("task identity lacks session namespace")
	}
}
func TestRecordsExpireInMemoryBeforeNextModelSnapshot(t *testing.T) {
	s := recordSession(t, &brokenRecordStore{err: recordstore.ErrNotFound}, "one")
	s.hist.appendTurn(turn{At: time.Now().Add(-169 * time.Hour), SessionID: "old", Role: "user", Text: "expired secret"})
	s.paused = &pausedTask{id: "expired", recordAt: time.Now().Add(-169 * time.Hour), original: requestOrigin{text: "expired task", known: true}}
	got := s.generationInstructions()
	if strings.Contains(got, "expired secret") || strings.Contains(got, "expired task") || s.paused != nil {
		t.Fatal("expired memory leaked into generation")
	}
}

type blockedClearStore struct {
	ownedRecordStore
	entered chan struct{}
	release chan struct{}
}

func (b *blockedClearStore) Clear(ctx context.Context, device string) error {
	close(b.entered)
	select {
	case <-b.release:
		return b.ownedRecordStore.Clear(ctx, device)
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestRecordsFinalizationWaitsForClearBeforeNewLoad(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	b := &blockedClearStore{ownedRecordStore: st, entered: make(chan struct{}), release: make(chan struct{})}
	s := recordSession(t, b, "one")
	s.hist.add("user", "private")
	s.checkpointRecords()
	flushRecords(t, s)
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "clear", known: true, text: "请清空本设备对话记录"}
	s.startClearRecords()
	<-b.entered
	done := make(chan struct{})
	go func() { s.finalizeRecords(); close(done) }()
	select {
	case <-done:
		t.Fatal("connection lease could release before clear admission")
	case <-time.After(20 * time.Millisecond):
	}
	close(b.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("clear blocked finalization")
	}
	next := recordSession(t, st, "one")
	if len(next.hist.Turns) != 0 || next.paused != nil {
		t.Fatal("clear race resurrected data")
	}
}
func TestRecordsLatestSegmentAndChosenStartSurviveRecovery(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	s := recordSession(t, st, "one")
	s.paused = &pausedTask{id: "root", original: requestOrigin{text: "数到五十", known: true, itemID: "original"}, playback: playbackRecord{id: 9, origin: requestOrigin{text: "从三十七开始", known: true, itemID: "latest"}}, resumeSource: "user_selected"}
	s.checkpointRecords()
	flushRecords(t, s)
	next := recordSession(t, st, "one")
	got := next.pausedData()
	if got["latest_request"] != "从三十七开始" || got["resume_source"] != "user_selected" {
		t.Fatal("lost selected start provenance", got)
	}
}

func TestRecordsDisconnectDuringClearKeepsDeviceLease(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: 168 * time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	seed := recordSession(t, st, "c4:9e:7e:12:99:10")
	seed.hist.add("user", "private-old-history")
	seed.checkpointRecords()
	flushRecords(t, seed)
	b := &blockedClearStore{ownedRecordStore: st, entered: make(chan struct{}), release: make(chan struct{})}
	models := make(chan *toolPeer, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e == nil {
			models <- newToolPeer(c)
		}
	}))
	defer upstream.Close()
	cfg := testConfig("ws" + strings.TrimPrefix(upstream.URL, "http"))
	cfg.MaxSessions = 2
	g, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	g.records = b
	defer g.Close()
	server := httptest.NewServer(g.Handler())
	defer server.Close()
	dial := func() (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + testToken}, "Device-Id": []string{"c4:9e:7e:12:99:10"}})
	}
	open := func(c *websocket.Conn) *toolPeer {
		d := newToolPeer(c)
		d.send(t, map[string]any{"type": "hello", "version": 1, "transport": "websocket", "audio_params": map[string]any{"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60}})
		d.want(t, "hello")
		return d
	}
	c, _, e := dial()
	if e != nil {
		t.Fatal(e)
	}
	d := open(c)
	var p *toolPeer
	select {
	case p = <-models:
	case <-time.After(time.Second):
		t.Fatal("no model")
	}
	defer p.c.Close()
	p.send(t, map[string]any{"type": "session.created"})
	p.want(t, "session.update")
	d.send(t, map[string]any{"type": "listen", "state": "start", "mode": "manual"})
	p.want(t, "session.update") // Device-start barrier before synthetic provider input.
	p.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "clear"})
	p.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "clear", "transcript": "请清空本设备对话记录"})
	p.want(t, "response.create")
	p.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "clear-response"}})
	toolCall(t, p, "clear-response", "clear-call", "conversation_clear_records", "{}")
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("clear was not dispatched")
	}
	c.Close()
	time.Sleep(20 * time.Millisecond)
	if duplicate, res, e := dial(); e == nil {
		duplicate.Close()
		close(b.release)
		t.Fatal("lease released during unadmitted clear")
	} else if res.StatusCode != http.StatusConflict {
		close(b.release)
		t.Fatal(res.Status)
	}
	close(b.release)
	deadline := time.Now().Add(time.Second)
	var next *websocket.Conn
	for time.Now().Before(deadline) {
		next, _, e = dial()
		if e == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if e != nil {
		t.Fatal("lease not released after clear", e)
	}
	defer next.Close()
	open(next)
	select {
	case p = <-models:
	case <-time.After(time.Second):
		t.Fatal("no reopened model")
	}
	defer p.c.Close()
	p.send(t, map[string]any{"type": "session.created"})
	setup := p.want(t, "session.update")["session"].(map[string]any)["instructions"].(string)
	if strings.Contains(setup, "private-old-history") {
		t.Fatal("reconnect revived deleted history")
	}
}

func TestRecordsEmptyResponseDoesNotRewritePreviousIdentity(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.hist.add("user", "real request")
	s.hist.stampLast(s.id, "", "original-user", 0)
	s.responseID = "empty-response"
	s.responseDone = true
	s.transcript = ""
	if e := s.play(); e != nil {
		t.Fatal(e)
	}
	if s.hist.Turns[0].InputID != "original-user" || s.hist.Turns[0].ResponseID != "" {
		t.Fatal("empty generated turn rewrote user provenance")
	}
}

func TestRecordsDraftCountsTowardBound(t *testing.T) {
	s := recordSession(t, &brokenRecordStore{err: recordstore.ErrNotFound}, "one")
	for i := 0; i < 128; i++ {
		s.hist.add("user", "bounded")
	}
	s.responseID = "draft"
	s.transcript = "generated but not sent"
	data := s.recordSnapshot()
	n := len(data.Turns)
	if data.Draft != nil {
		n++
	}
	if n > 128 {
		t.Fatalf("retained %d text records", n)
	}
}

type controlledClearStore struct {
	brokenRecordStore
	release chan error
}

func (b *controlledClearStore) Clear(context.Context, string) error { return <-b.release }
func TestRecordsClearToolReportsOnlyActualDeletionResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			b := &controlledClearStore{brokenRecordStore: brokenRecordStore{err: recordstore.ErrNotFound}, release: make(chan error, 1)}
			s := recordSession(t, b, "one")
			s.generation = 1
			s.responseID = "parent"
			s.currentInput = requestOrigin{generation: 1, itemID: "actual-clear", text: "请清空本设备对话记录", known: true}
			if e := s.callTool(qwen.Event{ResponseID: "parent", CallID: "clear", Name: "conversation_clear_records", Arguments: " { } "}); e != nil {
				t.Fatal(e)
			}
			call := s.toolCalls["1:clear"]
			if call == nil || call.output != "" {
				t.Fatal("clear returned success before storage receipt")
			}
			if failed {
				b.release <- errors.New("delete failed")
			} else {
				b.release <- nil
			}
			flushRecords(t, s)
			var out map[string]any
			json.Unmarshal([]byte(call.output), &out)
			if out["ok"] == failed {
				t.Fatal("clear receipt lied", out)
			}
		})
	}
}
func TestRecordsRestoreKeepsOriginalSourceAcrossMultipleSessions(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	first := recordSession(t, st, "one")
	first.paused = &pausedTask{id: "initial-task", original: requestOrigin{text: "原任务", known: true}, awaitingResumeChoice: true}
	first.checkpointRecords()
	flushRecords(t, first)
	second := recordSession(t, st, "one")
	second.checkpointRecords()
	flushRecords(t, second)
	third := recordSession(t, st, "one")
	if third.paused.sourceTaskID != "initial-task" || !third.paused.awaitingResumeChoice {
		t.Fatal("original source or choice state lost across restore")
	}
}

func TestRecordsRestoredTaskTextRemainsBounded(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	data := recordData{Task: &recordTask{At: time.Now(), ID: "task", Original: recordOrigin{SessionID: "old", Text: strings.Repeat("字", 600), Known: true}, Latest: recordOrigin{SessionID: "old", Text: strings.Repeat("字", 600), Known: true}}}
	body, _ := json.Marshal(data)
	receipt, e := st.Save("one", recordstore.Snapshot{Version: 1, UpdatedAt: time.Now(), Data: body})
	if e != nil {
		t.Fatal(e)
	}
	if e = <-receipt; e != nil {
		t.Fatal(e)
	}
	s := recordSession(t, st, "one")
	if len([]rune(s.paused.recordSource.Text)) > 512 || len([]rune(s.paused.playback.origin.text)) > 512 || !s.paused.original.truncated {
		t.Fatal("restored text bypassed per-message bound")
	}
}

func TestRecordsNewInputDuringClearPersistsAfterReceipt(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	b := &blockedClearStore{ownedRecordStore: st, entered: make(chan struct{}), release: make(chan struct{})}
	s := recordSession(t, b, "one")
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "clear", known: true, text: "清空对话记录"}
	s.startClearRecords()
	<-b.entered
	s.hist.add("user", "new input after clear")
	s.hist.stampLast(s.id, "", "new-user", 0)
	s.checkpointRecords()
	close(b.release)
	flushRecords(t, s)
	next := recordSession(t, st, "one")
	if !strings.Contains(next.hist.context(), "new input after clear") {
		t.Fatal("input accepted during clear was never saved after receipt")
	}
}

func TestRecordsDisconnectMarksLatestReplyInterrupted(t *testing.T) {
	s := recordSession(t, &brokenRecordStore{err: recordstore.ErrNotFound}, "one")
	s.responseID = "unfinished"
	s.draftTranscript = "unfinished draft"
	s.started = true
	s.activePlayback = &playbackRecord{id: 1, origin: requestOrigin{text: "原任务", known: true}}
	s.finalizeRecords()
	if !s.hist.Interrupted || len(s.hist.Turns) != 1 || s.hist.Turns[0].Delivery != "interrupted" {
		t.Fatal("disconnect lost interruption state")
	}
}

func TestRecordsClearDoesNotRestoreRetiredPlaybackFlagsOnDisconnect(t *testing.T) {
	st, e := recordstore.Open(recordstore.Options{Directory: t.TempDir(), Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close(context.Background())
	s := recordSession(t, st, "one")
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "clear", known: true, text: "清空对话记录"}
	r := &playbackRecord{id: 1, origin: requestOrigin{text: "old task", known: true}}
	s.playbacks = []*playbackRecord{r}
	s.activePlayback = r
	s.started = true
	s.responseID = "old"
	s.draftTranscript = "old private draft"
	s.startClearRecords()
	flushRecords(t, s)
	s.finalizeRecords()
	flushRecords(t, s)
	next := recordSession(t, st, "one")
	if next.hist.Interrupted || len(next.hist.Turns) != 0 || next.paused != nil {
		t.Fatal("cleared playback state resurrected on disconnect")
	}
}

func TestRecordsClearDisabledDoesNotRestoreParentText(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "clear-now", known: true, text: "清空对话记录"}
	s.responseID = "clear-parent"
	s.transcript = "此前的私密内容"
	s.responseDone = true
	receipt, ok := s.startClearRecords()
	if !ok {
		t.Fatal("clear was rejected")
	}
	if err := <-receipt; err != nil {
		t.Fatal(err)
	}
	if err := s.play(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.hist.context(), "此前的私密内容") {
		t.Fatal("successful clear restored the old parent response into temporary context")
	}
}

func TestRecordsClearSuppressionDoesNotCrossNativeGeneration(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "clear", known: true, text: "清空对话记录"}
	s.responseID = "reused-native-id"
	receipt, ok := s.startClearRecords()
	if !ok {
		t.Fatal("clear rejected")
	}
	<-receipt
	// Response IDs are scoped by the upstream generation, as are late events.
	s.generation++
	s.responseID = "reused-native-id"
	s.transcript = "new allowed response"
	s.responseDone = true
	if err := s.play(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.hist.context(), "new allowed response") {
		t.Fatal("old generation suppression discarded a fresh response")
	}
}

func TestRecordsClearDisabledSuppressesInterruptedParent(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.generation = 1
	s.currentInput = requestOrigin{generation: 1, itemID: "clear", known: true, text: "清空对话记录"}
	s.responseID = "clear-parent"
	s.started = true
	s.responseDone = true
	s.draftTranscript = "private parent draft"
	receipt, ok := s.startClearRecords()
	if !ok {
		t.Fatal("clear rejected")
	}
	<-receipt
	// The storage-free fixture has no upstream URL; reconnect fails after the
	// real stop/history path. That failure must not revive the cleared transcript.
	if err := s.interrupt(context.Background(), false, true); err == nil {
		t.Fatal("expected unavailable upstream")
	}
	if len(s.hist.Turns) != 0 || len(s.hist.archive) != 0 {
		t.Fatal("interrupted parent returned to temporary history")
	}
}
