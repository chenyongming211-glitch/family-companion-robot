package voicecompare

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestStreamConversationDeliversFirstClauseWhileSecondWaits(t *testing.T) {
	const raw = "（表情：开心）\n\n开头。中间。结尾。"
	var requests atomic.Int32
	waiting, release := make(chan struct{}), make(chan struct{})
	c := fixtureClient(wsFixture(t, func(conn *websocket.Conn) {
		if requests.Add(1) == 2 {
			close(waiting)
			<-release
		}
		ttsFixture(t, conn, "")
	}))
	c.RealtimeURL = wsFixture(t, func(conn *websocket.Conn) { productNativeFixture(t, conn, raw, 3200) })
	type clause struct {
		draft, text string
		size        int
	}
	output := make(chan clause, 3)
	done := make(chan error, 1)
	go func() {
		done <- c.StreamConversation(context.Background(), "native", "", make([]byte, 3200), func(draft, text string, audio []byte) error {
			output <- clause{draft, text, len(audio)}
			return nil
		})
	}()
	select {
	case <-waiting:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("second TTS request never started")
	}
	var first clause
	select {
	case first = <-output:
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if first != (clause{raw, "开头。", 9600}) {
		t.Fatal("first verified clause unavailable while second waited", first)
	}
	close(output)
	var rest []clause
	for part := range output {
		rest = append(rest, part)
	}
	if !slices.Equal(rest, []clause{{"", "中间。", 9600}, {"", "结尾。", 9600}}) {
		t.Fatal("late clauses repeated draft, changed order or lost PCM", rest)
	}
}

func TestStreamConversationStopsAtFirstUndeliverableClause(t *testing.T) {
	const raw = "第一句。第二句。第三句。"
	consumerErr := errors.New("consumer stopped")
	for _, tc := range []struct {
		name                        string
		wantRequests, wantCallbacks int
	}{
		{"first_bad_usage", 1, 0},
		{"second_bad_usage", 2, 1},
		{"second_audit_failure", 2, 1},
		{"callback_error", 1, 1},
		{"cancel_after_audit", 1, 0},
		{"cancel_after_delivery", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requests atomic.Int32
			c := fixtureClient(wsFixture(t, func(conn *websocket.Conn) {
				n := requests.Add(1)
				mode := ""
				if tc.name == "first_bad_usage" && n == 1 || tc.name == "second_bad_usage" && n == 2 {
					mode = "bad-usage"
				}
				ttsFixture(t, conn, mode)
			}))
			c.RealtimeURL = wsFixture(t, func(conn *websocket.Conn) { productNativeFixture(t, conn, raw, 3200) })
			var audit []error
			c.AfterCall = func(stage string, _ map[string]any, err error) error {
				if stage == "tts" {
					audit = append(audit, err)
					if tc.name == "cancel_after_audit" {
						cancel()
					}
					if tc.name == "second_audit_failure" && len(audit) == 2 {
						return errors.New("could not record completed task")
					}
				}
				return nil
			}
			callbacks := 0
			err := c.StreamConversation(ctx, "native", "", make([]byte, 3200), func(draft, text string, audio []byte) error {
				callbacks++
				if callbacks != 1 || draft != raw || text != "第一句。" || len(audio) != 9600 {
					t.Errorf("delivered unverified or unexpected fragment %q", text)
				}
				if tc.name == "callback_error" {
					return consumerErr
				}
				if tc.name == "cancel_after_delivery" {
					cancel()
				}
				return nil
			})
			if err == nil || callbacks != tc.wantCallbacks || int(requests.Load()) != tc.wantRequests || len(audit) != tc.wantRequests {
				t.Fatalf("err=%v callbacks=%d requests=%d audit=%v", err, callbacks, requests.Load(), audit)
			}
			switch tc.name {
			case "first_bad_usage", "second_bad_usage":
				if TTSFailureCategory(audit[len(audit)-1]) != "usage_invalid" {
					t.Fatal("failed TTS missing from audit", audit)
				}
			case "callback_error":
				if !errors.Is(err, consumerErr) {
					t.Fatal(err)
				}
			case "cancel_after_audit", "cancel_after_delivery":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestStreamConversationCancelsPendingSecondRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	waiting := make(chan struct{})
	var requests, delivered atomic.Int32
	c := fixtureClient(wsFixture(t, func(conn *websocket.Conn) {
		if requests.Add(1) == 1 {
			ttsFixture(t, conn, "")
			return
		}
		run := readJSON(t, conn)
		id := run["header"].(map[string]any)["task_id"].(string)
		taskEvent(conn, id, "task-started", nil)
		_ = readJSON(t, conn) // continue-task
		_ = readJSON(t, conn) // finish-task
		close(waiting)
		_, _, _ = conn.ReadMessage() // the provider withholds the terminal event
	}))
	c.RealtimeURL = wsFixture(t, func(conn *websocket.Conn) { productNativeFixture(t, conn, "先讲。再讲。最后。", 3200) })
	done := make(chan error, 1)
	go func() {
		done <- c.StreamConversation(ctx, "native", "", make([]byte, 3200), func(_, _ string, _ []byte) error {
			delivered.Add(1)
			return nil
		})
	}()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("second request never reached the provider")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || delivered.Load() != 1 || requests.Load() != 2 {
			t.Fatalf("err=%v delivered=%d requests=%d", err, delivered.Load(), requests.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("cancel failed to release provider read")
	}
}
