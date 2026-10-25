package realtimelive

import (
	"context"
	"encoding/json"
	"familyrobot/internal/realtimeaudio"
	"familyrobot/internal/realtimestate"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Block Feed after a packet is written, then race the drain seal against it.
// The emitted packet must enter ASR before Finish queues its terminal marker.
func TestReviewReleasedPacketReachesASRBeforeConcurrentDrain(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	t.Cleanup(func() { s.cancel(); s.wg.Wait() })
	codec, err := realtimeaudio.New(24000, 1440)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	packet, err := codec.Encode(make([]byte, 2880))
	if err != nil {
		t.Fatal(err)
	}
	rt := s.addTurn()
	rt.turn.Begun = true
	a := &OutputASR{queue: make(chan []byte, 1024), Done: make(chan struct{}), cancel: func() {}}
	rt.asr = a
	a.mu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mu.Unlock()
		}
	}()
	released := make(chan error, 1)
	go func() { _, err := s.release(rt, packet, codec, realtimeaudio.NewResampler()); released <- err }()
	client.SetReadDeadline(time.Now().Add(time.Second))
	if kind, _, err := client.ReadMessage(); err != nil || kind != websocket.BinaryMessage {
		t.Fatalf("released packet: kind=%d err=%v", kind, err)
	}
	interrupted := make(chan error, 1)
	go func() { interrupted <- s.interrupt() }()
	select {
	case err := <-interrupted:
		t.Fatalf("drain passed the pending ASR feed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	a.mu.Unlock()
	locked = false
	for _, result := range []<-chan error{released, interrupted} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("release/drain failed to finish")
		}
	}
	if got := a.InputSamples(); got != 960 {
		t.Fatalf("ASR lost released samples: got %d want 960", got)
	}
	if frame := <-a.queue; len(frame) != 1920 {
		t.Fatalf("first ASR entry was not the released 60ms frame: %d bytes", len(frame))
	}
	if frame := <-a.queue; frame != nil {
		t.Fatal("ASR finish did not follow the released frame")
	}
	var drain map[string]any
	if err := client.ReadJSON(&drain); err != nil || drain["sent"] != float64(1440) {
		t.Fatalf("drain boundary: %v %v", drain, err)
	}
}

// Hold the failure callback in its audit write while a later question becomes
// current. The callback must not send a stale failure or cancel that question.
func TestReviewConcurrentOldFailureLeavesNewQuestionActive(t *testing.T) {
	server, client := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	defer s.cancel()
	old := s.addTurn()
	s.rec.mu.Lock()
	done := make(chan struct{})
	go func() { s.turnError(old); close(done) }()
	next := s.addTurn()
	s.rec.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old failure callback blocked")
	}
	s.mu.Lock()
	cut, current := next.turn.Cut, s.output
	s.mu.Unlock()
	if cut || current != next {
		t.Fatal("old callback cancelled the new question")
	}
	client.SetReadDeadline(time.Now().Add(25 * time.Millisecond))
	if _, b, err := client.ReadMessage(); err == nil {
		t.Fatalf("stale client status: %s", b)
	}
}

func TestReviewASRPrematureCompletionWaitsForSender(t *testing.T) {
	announced := make(chan struct{})
	received := make(chan int, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var start map[string]any
		if c.ReadJSON(&start) != nil {
			return
		}
		id := object(start["header"])["task_id"]
		if c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-started", "task_id": id}}) != nil {
			return
		}
		if c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-finished", "task_id": id}}) != nil {
			return
		}
		close(announced)
		samples := 0
		for {
			kind, b, err := c.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.BinaryMessage {
				samples += len(b) / 2
				continue
			}
			var event map[string]any
			if json.Unmarshal(b, &event) == nil && object(event["header"])["action"] == "finish-task" {
				received <- samples
				return
			}
		}
	}))
	defer provider.Close()
	dir := t.TempDir()
	a := NewOutputASR(context.Background(), Config{ASRURL: "ws" + strings.TrimPrefix(provider.URL, "http"), Synthetic: true}, dir, func(map[string]any, int64) error { return nil })
	defer a.Close()
	select {
	case <-announced:
	case <-time.After(time.Second):
		t.Fatal("provider did not announce completion")
	}
	select {
	case <-a.Done:
		t.Fatal("ASR marked success before its sender finished")
	case <-time.After(25 * time.Millisecond):
	}
	a.Feed(make([]byte, 640))
	a.Finish()
	select {
	case <-a.Done:
	case <-time.After(time.Second):
		t.Fatal("ASR did not join sender")
	}
	select {
	case samples := <-received:
		if samples != 320 {
			t.Fatalf("provider received %d samples", samples)
		}
	case <-time.After(time.Second):
		t.Fatal("finish-task was not sent")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "live_output_asr-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("audit paths: %v %v", paths, err)
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	audit, err := decodeObject(b)
	if err != nil || audit["state"] != "completed" || audit["input_samples"] != json.Number("320") {
		t.Fatalf("final audit: %v %v", audit, err)
	}
}

func TestReviewASRPrematureCompletionStillCancels(t *testing.T) {
	announced := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var start map[string]any
		if c.ReadJSON(&start) != nil {
			return
		}
		id := object(start["header"])["task_id"]
		for _, event := range []string{"task-started", "task-finished"} {
			if c.WriteJSON(map[string]any{"header": map[string]any{"event": event, "task_id": id}}) != nil {
				return
			}
		}
		close(announced)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer provider.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := NewOutputASR(ctx, Config{ASRURL: "ws" + strings.TrimPrefix(provider.URL, "http"), Synthetic: true}, t.TempDir(), func(map[string]any, int64) error { return nil })
	defer a.Close()
	select {
	case <-announced:
	case <-time.After(time.Second):
		t.Fatal("provider handshake blocked")
	}
	cancel()
	select {
	case <-a.Done:
	case <-time.After(time.Second):
		t.Fatal("ASR sender join ignored cancellation")
	}
	a.mu.Lock()
	failed := a.failed
	a.mu.Unlock()
	if !failed {
		t.Fatal("cancelled, unfinished ASR was marked successful")
	}
}

func TestReviewNativeOpenCancellationClosesQuietConnection(t *testing.T) {
	waiting, disconnected := make(chan struct{}), make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if c.WriteJSON(map[string]any{"type": "session.created"}) != nil {
			return
		}
		var update map[string]any
		if c.ReadJSON(&update) != nil {
			return
		}
		close(waiting)
		c.ReadMessage()
		close(disconnected)
	}))
	defer provider.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http"), Synthetic: true}, t.TempDir())
	defer n.Close()
	done := make(chan error, 1)
	go func() { done <- n.Open(ctx, "公开合成测试", false) }()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("native handshake blocked")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled open succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled open waited for its 12s timeout")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("cancelled native connection leaked")
	}
}

func reviewPair(t *testing.T, endpoint string) string {
	t.Helper()
	r, err := http.Post(endpoint+"/pair", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.StatusCode != 200 {
		t.Fatalf("pair: %d %v", r.StatusCode, err)
	}
	return body["ticket"]
}

func TestReviewShutdownWaitsForSessionCleanupAndRejectsAdmission(t *testing.T) {
	app := NewServer(Config{}, nil, t.TempDir(), "review")
	entered, disconnected, cleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})
	app.run = func(c *websocket.Conn, _ string) { close(entered); c.ReadMessage(); close(disconnected); <-cleanup }
	h := httptest.NewServer(app.Handler())
	defer h.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/live", http.Header{"Authorization": []string{"Bearer " + reviewPair(t, h.URL)}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.CloseActive(ctx) }()
	select {
	case <-disconnected:
	case <-ctx.Done():
		close(cleanup)
		t.Fatal("shutdown did not close active websocket")
	}
	select {
	case err := <-done:
		close(cleanup)
		t.Fatalf("shutdown returned before session cleanup: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	r, err := http.Post(h.URL+"/pair", "application/json", nil)
	if err != nil {
		close(cleanup)
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusConflict {
		close(cleanup)
		t.Fatalf("shutdown admitted a new pair: %d", r.StatusCode)
	}
	close(cleanup)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown never observed cleanup completion")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.busy || app.active != nil {
		t.Fatal("shutdown left stale session ownership")
	}
}

func TestReviewFailedUpgradeAlsoSignalsCleanup(t *testing.T) {
	app := NewServer(Config{}, nil, t.TempDir(), "review")
	h := httptest.NewServer(app.Handler())
	defer h.Close()
	req, _ := http.NewRequest("GET", h.URL+"/live", nil)
	req.Header.Set("Authorization", "Bearer "+reviewPair(t, h.URL))
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("upgrade status: %d", r.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := app.CloseActive(ctx); err != nil {
		t.Fatalf("failed upgrade left cleanup unresolved: %v", err)
	}
}

// Opt in on the cloud runner: these cross the actual frozen 35/40/60s
// boundaries instead of adding production timing knobs solely for tests.
func reviewLongTest(t *testing.T) {
	t.Helper()
	if os.Getenv("OUTPUT_LIVE_REVIEW_LONG_TESTS") != "1" {
		t.Skip("set OUTPUT_LIVE_REVIEW_LONG_TESTS=1 on the cloud runner")
	}
	t.Parallel()
}

func TestReviewQuietListenerAndTerminalSurviveSixtySeconds(t *testing.T) {
	reviewLongTest(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if c.WriteJSON(map[string]any{"type": "session.created"}) != nil {
			return
		}
		for i := 0; i < 2; i++ {
			var update map[string]any
			if c.ReadJSON(&update) != nil {
				return
			}
			session := map[string]any{}
			if i == 0 {
				session["enable_search"] = false
			}
			if c.WriteJSON(map[string]any{"type": "session.updated", "session": session}) != nil {
				return
			}
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer provider.Close()
	server, client := testSocket(t)
	s := NewSession(server, Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http"), Synthetic: true}, t.TempDir(), nil)
	defer s.fail()
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	client.SetReadDeadline(time.Now().Add(75 * time.Second))
	var ready map[string]any
	if err := client.ReadJSON(&ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("ready: %v %v", ready, err)
	}
	readDone := make(chan error, 1)
	go func() { _, _, err := client.ReadMessage(); readDone <- err }()
	select {
	case err := <-done:
		t.Fatalf("quiet session ended before 65 seconds: %v", err)
	case err := <-readDone:
		t.Fatalf("quiet terminal disconnected: %v", err)
	case <-time.After(65 * time.Second):
	}
	if err := client.WriteJSON(map[string]any{"type": "stop"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("quiet session failed to stop")
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("terminal reader leaked")
	}
}

func TestReviewOpenBudgetIsSeparateFromFirstPacketBudget(t *testing.T) {
	reviewLongTest(t)
	server, client := testSocket(t)
	product, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(server, Config{Model: "test", Voice: "Tina", Synthetic: true}, t.TempDir(), product)
	defer s.cancel()
	wait := func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	s.generate = func(ctx context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		// Both waits fit their separate 12s open / 35s provider-event budgets.
		if err := wait(ctx, 10*time.Second); err != nil {
			return err
		}
		if err := emit("ready", nil); err != nil {
			return err
		}
		if err := wait(ctx, 31*time.Second); err != nil {
			return err
		}
		if err := emit("pcm", make([]byte, 2880)); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
	rt := s.addTurn()
	done := make(chan struct{})
	go func() { s.reply(rt, make([]byte, 640)); close(done) }()
	defer func() {
		s.cancel()
		<-done
		s.mu.Lock()
		a := rt.asr
		s.mu.Unlock()
		if a != nil {
			a.Close()
		}
	}()
	client.SetReadDeadline(time.Now().Add(45 * time.Second))
	for {
		kind, b, err := client.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.BinaryMessage {
			break
		}
		var event map[string]any
		if json.Unmarshal(b, &event) == nil && event["code"] == "turn_failed" {
			t.Fatal("open consumed the first-packet budget")
		}
	}
}
