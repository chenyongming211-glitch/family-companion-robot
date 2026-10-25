package realtimelive

import (
	"context"
	"github.com/gorilla/websocket"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// OutputASR receives only decoded packets already released to the terminal.
// Queue, finish, timeout and failure semantics match the frozen output-live path.
type OutputASR struct {
	mu               sync.Mutex
	queue            chan []byte
	Done             chan struct{}
	cancel           context.CancelFunc
	finished, failed bool
	samples          int64
}

func NewOutputASR(parent context.Context, cfg Config, dir string, onFinal func(map[string]any, int64) error) *OutputASR {
	ctx, cancel := context.WithTimeout(parent, 100*time.Second)
	a := &OutputASR{queue: make(chan []byte, 1024), Done: make(chan struct{}), cancel: cancel}
	go a.run(ctx, cfg, dir, onFinal)
	return a
}
func (a *OutputASR) Feed(pcm []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished || a.failed {
		return
	}
	select {
	case a.queue <- append([]byte(nil), pcm...):
		a.samples += int64(len(pcm) / 2)
	default:
		a.failed = true
		a.cancel()
	}
}
func (a *OutputASR) Finish() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return
	}
	a.finished = true
	select {
	case <-a.Done:
		return
	default:
	}
	select {
	case a.queue <- nil:
	default:
		a.failed = true
		a.cancel()
	}
}
func (a *OutputASR) Close()              { a.cancel(); <-a.Done }
func (a *OutputASR) InputSamples() int64 { a.mu.Lock(); defer a.mu.Unlock(); return a.samples }
func (a *OutputASR) run(ctx context.Context, cfg Config, dir string, onFinal func(map[string]any, int64) error) {
	failed := true
	defer func() { a.cancel(); a.mu.Lock(); a.failed = a.failed || failed; a.mu.Unlock(); close(a.Done) }()
	audit, e := newIntent(dir, "live_output_asr", cfg.Synthetic)
	if e != nil {
		return
	}
	var samples atomic.Int64
	completed := false
	defer func() {
		if !completed {
			audit.update(map[string]any{"state": "failed_or_outcome_unknown", "input_samples": samples.Load()})
		}
	}()
	c, _, e := websocket.DefaultDialer.DialContext(ctx, cfg.ASRURL, http.Header{"Authorization": []string{"Bearer " + cfg.Key}})
	if e != nil {
		return
	}
	defer c.Close()
	c.SetReadLimit(1 << 21)
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if audit.update(map[string]any{"state": "submission_outcome_unknown"}) != nil {
		return
	}
	id := randomID()
	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if c.WriteJSON(map[string]any{"header": map[string]any{"action": "run-task", "task_id": id, "streaming": "duplex"}, "payload": map[string]any{"task_group": "audio", "task": "asr", "function": "recognition", "model": "fun-asr-realtime", "parameters": map[string]any{"format": "pcm", "sample_rate": 16000}, "input": map[string]any{}}}) != nil {
		return
	}
	read := func(timeout time.Duration) (map[string]any, error) {
		c.SetReadDeadline(time.Now().Add(timeout))
		_, b, e := c.ReadMessage()
		if e != nil {
			return nil, e
		}
		return decodeObject(b)
	}
	e1, e := read(12 * time.Second)
	if e != nil || object(e1["header"])["event"] != "task-started" {
		return
	}
	sender := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				sender <- ctx.Err()
				return
			case pcm := <-a.queue:
				c.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if pcm == nil {
					sender <- c.WriteJSON(map[string]any{"header": map[string]any{"action": "finish-task", "task_id": id, "streaming": "duplex"}, "payload": map[string]any{"input": map[string]any{}}})
					return
				}
				if e := c.WriteMessage(websocket.BinaryMessage, pcm); e != nil {
					sender <- e
					c.Close()
					return
				}
				samples.Add(int64(len(pcm) / 2))
			}
		}
	}()
	senderJoined := false
	defer func() {
		a.cancel()
		c.Close()
		if !senderJoined {
			<-sender
		}
	}()
	for range 4096 {
		event, er := read(35 * time.Second)
		if er != nil {
			return
		}
		h := object(event["header"])
		if h["task_id"] != id || h["event"] == "task-failed" {
			return
		}
		switch h["event"] {
		case "result-generated":
			s := object(object(object(event["payload"])["output"])["sentence"])
			if s["sentence_end"] == true {
				if onFinal(s, time.Since(audit.start).Milliseconds()) != nil {
					return
				}
			}
		case "task-finished":
			// Require the sender to finish successfully, even if the provider
			// announces completion before it has consumed the queued input.
			select {
			case er := <-sender:
				senderJoined = true
				if er != nil {
					return
				}
			case <-ctx.Done():
				return
			}
			if audit.update(map[string]any{"state": "completed", "input_samples": samples.Load()}) != nil {
				return
			}
			failed = false
			completed = true
			return
		}
	}
}
