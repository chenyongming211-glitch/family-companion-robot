package voicecompare

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

type Word struct {
	Text           string
	BeginMS, EndMS int64
}

type Call struct {
	Stage string
	Usage map[string]any
}

// A completed TTS task is an indivisible source/PCM boundary when the provider's
// spoken-word coordinates cannot be mapped to the original text.
type TaskBoundary struct {
	Text       string
	EndSamples int64
	Precision  string
}

type Result struct {
	Text           string
	PCM            []byte
	Words          []Word
	Calls          []Call
	TaskBoundaries []TaskBoundary
}

type runLimits struct{ inputSeconds, outputSeconds, promptRunes, textRunes, asrRunes, clauses, tokens int }

var benchmarkLimits = runLimits{10, 30, 4096, 120, 512, 16, 256}
var conversationLimits = runLimits{25, 60, 32768, 4096, 4096, 128, 4096}

func (c Client) limits() runLimits {
	if c.product {
		return conversationLimits
	}
	return benchmarkLimits
}

type Client struct {
	product                                                  bool
	onClause                                                 func(draft, text string, audio []byte) error
	BeforeSubmit                                             func() error // Optional live access check immediately before provider writes.
	Key, RealtimeURL, InferenceURL, TextURL, TTSModel, Voice string
	BeforeCall                                               func(stage string, reserveYuan float64) error
	AfterCall                                                func(stage string, usage map[string]any, err error) error
	// FirstNativeText receives the wait since response.create was written and the
	// validated event type. The text itself stays within the generation client.
	FirstNativeText func(wait time.Duration, eventType string)
	RecordEvent     func(json.RawMessage) error
}

// StreamConversation hands each fully validated and accounted TTS task to the
// consumer before submitting the next. Only the first callback carries the
// original reply. Later errors leave previously delivered audio with the caller.
func (c Client) StreamConversation(ctx context.Context, backend, instructions string, pcm []byte, onClause func(draft, text string, audio []byte) error) error {
	if onClause == nil {
		return errors.New("missing conversation output")
	}
	c.onClause = onClause
	_, err := c.RunConversation(ctx, backend, instructions, pcm)
	return err
}

// RunConversation is the permanent A/B generation path. Run keeps historical
// comparison bounds. Both share the exact same provider and terminal validation.
func (c Client) RunConversation(ctx context.Context, backend, instructions string, pcm []byte) (Result, error) {
	if backend != "native" && backend != "cascade" {
		return Result{}, errors.New("unknown conversation backend")
	}
	c.product = true
	r, err := c.Run(ctx, backend, instructions, pcm, "")
	if err != nil {
		// A websocket deadline can fire just before the context timer is scheduled.
		// Preserve cancellation identity instead of exposing a provider-read failure.
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		} else if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			err = errors.Join(context.DeadlineExceeded, err)
		}
		return r, conversationError{err}
	}
	return r, err
}

type conversationError struct{ cause error }

func (e conversationError) Error() string { return "conversation generation failed" }
func (e conversationError) Unwrap() error { return e.cause }
func (c Client) Run(ctx context.Context, backend, instructions string, pcm []byte, text string) (Result, error) {
	limits := c.limits()
	var r Result
	if c.BeforeCall == nil || c.AfterCall == nil || c.Key == "" || strings.ContainsAny(c.Key, "\r\n") || !utf8.ValidString(instructions) || utf8.RuneCountInString(instructions) > limits.promptRunes {
		return r, errors.New("invalid configuration or instructions")
	}
	if backend != "tts" && backend != "native" && backend != "cascade" {
		return r, errors.New("unknown backend")
	}
	if backend == "tts" {
		// Han characters can bill as two characters; this bounds all inputs to 512.
		if len(pcm) != 0 || !boundedText(text, 256) {
			return r, errors.New("invalid TTS input")
		}
	} else if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > 16000*2*limits.inputSeconds || text != "" {
		return r, errors.New("invalid PCM input")
	}
	if c.TTSModel == "" || c.Voice == "" || len(c.TTSModel) > 128 || len(c.Voice) > 256 || !safeURL(c.InferenceURL, true) || backend == "native" && !safeURL(c.RealtimeURL, true) || backend == "cascade" && !safeURL(c.TextURL, false) {
		return r, errors.New("invalid endpoint or TTS configuration")
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return r, err
	}
	prompt := instructions + "\n请直接简短回答，不超过120个字符；不调用工具，不搜索。"
	if c.product {
		prompt = instructions + "\n请自然回答当前问题，保留上下文；不调用工具，不搜索。"
	}
	if backend != "tts" {
		var err error
		var call Call
		if backend == "native" {
			text, call, err = c.native(ctx, prompt, pcm)
		} else {
			var input string
			input, call, err = c.asr(ctx, pcm)
			if call.Stage != "" {
				r.Calls = append(r.Calls, call)
			}
			if err != nil {
				return r, err
			}
			text, call, err = c.llm(ctx, prompt, input)
		}
		if call.Stage != "" {
			r.Calls = append(r.Calls, call)
		}
		r.Text = text
		if err != nil {
			return r, err
		}
	}
	r.Text = text
	if c.product {
		text = conversationSpokenText(text)
	}
	clauses := splitClauses(text)
	if len(clauses) == 0 || len(clauses) > limits.clauses {
		return r, errors.New("too many TTS clauses")
	}
	wordPrefix := true
	totalBytes, maxBytes := 0, 24000*2*limits.outputSeconds
	for i, clause := range clauses {
		pcmPart, wordPart, call, err := c.tts(ctx, clause, maxBytes-totalBytes)
		if call.Stage != "" {
			r.Calls = append(r.Calls, call)
		}
		if err != nil {
			return r, err
		}
		totalBytes += len(pcmPart)
		if totalBytes > maxBytes {
			return r, errors.New("TTS output limit exceeded")
		}
		if c.onClause != nil {
			if err := ctx.Err(); err != nil {
				return r, err
			}
			var draft string
			if i == 0 {
				draft = r.Text
			}
			if err := c.onClause(draft, clause, pcmPart); err != nil {
				return r, err
			}
			if err := ctx.Err(); err != nil {
				return r, err
			}
			continue
		}
		offset := (int64(len(r.PCM)/2) + 23) / 24 // Round up: never confirm the next task early.
		precision := "word"
		if len(wordPart) == 0 {
			precision, wordPrefix = "task_boundary", false
		}
		if wordPrefix {
			for _, w := range wordPart {
				w.BeginMS += offset
				w.EndMS += offset
				r.Words = append(r.Words, w)
			}
		}
		r.PCM = append(r.PCM, pcmPart...)
		r.TaskBoundaries = append(r.TaskBoundaries, TaskBoundary{Text: clause, EndSamples: int64(len(r.PCM) / 2), Precision: precision})
	}
	return r, nil
}

func ConfirmedText(words []Word, playedSamples, marginMS int64) string {
	if playedSamples < 0 || marginMS < 0 {
		return ""
	}
	end := int64(0)
	for _, w := range words {
		if w.Text == "" || w.BeginMS < end || w.EndMS <= w.BeginMS {
			return ""
		}
		end = w.EndMS
	}
	cutoff := playedSamples/24 - marginMS
	var text strings.Builder
	for _, w := range words {
		if w.EndMS > cutoff {
			break
		}
		text.WriteString(w.Text)
	}
	return text.String()
}

func boundedText(s string, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= max
}

func safeURL(raw string, ws bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	secure, plain := "https", "http"
	if ws {
		secure, plain = "wss", "ws"
	}
	if u.Scheme == secure {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == plain && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}

// Every submitted operation is persisted, including connection failures and unknown outcomes.
func (c Client) call(ctx context.Context, stage string, reserve float64, run func(*map[string]any) error) (Call, error) {
	if err := ctx.Err(); err != nil {
		return Call{}, err
	}
	if err := c.BeforeCall(stage, reserve); err != nil {
		return Call{}, err
	}
	call := Call{Stage: stage}
	err := run(&call.Usage)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if !validUsage(stage, call.Usage) {
		usageErr := errors.New("provider usage unknown or invalid")
		if stage == "tts" && err == nil {
			err = ttsFailure{"usage_invalid", usageErr}
		} else {
			err = errors.Join(err, usageErr)
		}
	}
	return call, errors.Join(err, c.AfterCall(stage, call.Usage, err))
}

func validUsage(stage string, usage map[string]any) bool {
	number := func(value any, integer bool) bool {
		n, ok := value.(float64)
		return ok && n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) && (!integer || math.Trunc(n) == n)
	}
	counters := func(fields map[string]any, keys ...string) bool {
		for _, key := range keys {
			if !number(fields[key], true) {
				return false
			}
		}
		return true
	}
	switch stage {
	case "tts":
		return counters(usage, "characters")
	case "asr":
		return number(usage["duration"], false)
	case "llm":
		return counters(usage, "prompt_tokens", "completion_tokens", "total_tokens")
	case "native":
		input, ok := usage["input_tokens_details"].(map[string]any)
		if !ok || !counters(usage, "input_tokens", "output_tokens", "total_tokens") || !counters(input, "text_tokens", "audio_tokens") {
			return false
		}
		if raw, present := usage["output_tokens_details"]; present {
			output, ok := raw.(map[string]any)
			if !ok {
				return false
			}
			for _, key := range []string{"text_tokens", "audio_tokens"} {
				if value, present := output[key]; present && (!number(value, true) || key == "audio_tokens" && value.(float64) != 0) {
					return false
				}
			}
		}
		return true
	}
	return false
}

type socket struct {
	*websocket.Conn
	stop         func() bool
	count, total int
	record       func(json.RawMessage) error
	beforeWrite  func() error
}

func (c Client) dial(ctx context.Context, endpoint string) (*socket, error) {
	d := websocket.Dialer{HandshakeTimeout: 8 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	conn, resp, err := d.DialContext(ctx, endpoint, http.Header{"Authorization": []string{"Bearer " + c.Key}})
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, errors.New("provider connection failed")
	}
	conn.SetReadLimit(2 << 20)
	deadline := time.Now().Add(35 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	return &socket{Conn: conn, stop: context.AfterFunc(ctx, func() { _ = conn.Close() }), record: c.RecordEvent, beforeWrite: c.BeforeSubmit}, nil
}

func (s *socket) close() { s.stop(); _ = s.Conn.Close() }
func (s *socket) send(v any) error {
	if s.beforeWrite != nil {
		if err := s.beforeWrite(); err != nil {
			return err
		}
	}
	if s.WriteJSON(v) != nil {
		return errors.New("provider write failed")
	}
	return nil
}
func (s *socket) read() (int, []byte, error) {
	k, b, e := s.ReadMessage()
	s.count++
	s.total += len(b)
	if e != nil || s.count > 4096 || s.total > 8<<20 {
		return 0, nil, errors.New("provider read failed or event limit exceeded")
	}
	return k, b, nil
}

type event struct {
	Header struct {
		Event  string `json:"event"`
		TaskID string `json:"task_id"`
	} `json:"header"`
	Payload struct {
		Output json.RawMessage `json:"output"`
		Usage  map[string]any  `json:"usage"`
	} `json:"payload"`
	Type         string          `json:"type"`
	ResponseID   string          `json:"response_id"`
	Text         string          `json:"text"`
	Delta        string          `json:"delta"`
	Response     json.RawMessage `json:"response"`
	Session      json.RawMessage `json:"session"`
	Item         json.RawMessage `json:"item"`
	Part         json.RawMessage `json:"part"`
	OutputIndex  *int            `json:"output_index"`
	ContentIndex *int            `json:"content_index"`
}

func (s *socket) event() (event, error) {
	var ev event
	k, b, err := s.read()
	if err != nil {
		return ev, err
	}
	if k != websocket.TextMessage || !json.Valid(b) {
		return ev, errors.New("invalid provider event")
	}
	if s.record != nil {
		if err = s.record(json.RawMessage(b)); err != nil {
			return ev, errors.New("provider event recording failed")
		}
	}
	if json.Unmarshal(b, &ev) != nil {
		return ev, errors.New("invalid provider event")
	}
	return ev, nil
}

func taskCommand(action, id string, payload any) any {
	return map[string]any{"header": map[string]any{"action": action, "task_id": id, "streaming": "duplex"}, "payload": payload}
}
func taskID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (s *socket) taskEvent(id string, usage *map[string]any) (event, error) {
	ev, err := s.event()
	if err != nil {
		return ev, err
	}
	if ev.Header.TaskID != id {
		return ev, errors.New("task identity mismatch")
	}
	if ev.Header.Event == "task-failed" {
		return ev, errTaskRejected
	}
	if ev.Payload.Usage != nil {
		*usage = ev.Payload.Usage
	}
	return ev, nil
}
