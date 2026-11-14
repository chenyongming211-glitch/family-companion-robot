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

type Result struct {
	Text  string
	PCM   []byte
	Words []Word
	Calls []Call
}

type Client struct {
	Key, RealtimeURL, InferenceURL, TextURL, TTSModel, Voice string
	BeforeCall                                               func(stage string, reserveYuan float64) error
	AfterCall                                                func(stage string, usage map[string]any, err error) error
}

func (c Client) Run(ctx context.Context, backend, instructions string, pcm []byte, text string) (Result, error) {
	var r Result
	if c.BeforeCall == nil || c.AfterCall == nil || c.Key == "" || strings.ContainsAny(c.Key, "\r\n") || !utf8.ValidString(instructions) || utf8.RuneCountInString(instructions) > 4096 {
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
	} else if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > 16000*2*10 || text != "" {
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
	clauses := splitClauses(text)
	if len(clauses) == 0 || len(clauses) > 16 {
		return r, errors.New("too many TTS clauses")
	}
	for _, clause := range clauses {
		pcmPart, wordPart, call, err := c.tts(ctx, clause, 24000*2*30-len(r.PCM))
		if call.Stage != "" {
			r.Calls = append(r.Calls, call)
		}
		if err != nil {
			return r, err
		}
		if len(r.PCM)+len(pcmPart) > 24000*2*30 {
			return r, errors.New("TTS output limit exceeded")
		}
		offset := (int64(len(r.PCM)/2) + 23) / 24 // Round up: never confirm the next task early.
		for _, w := range wordPart {
			w.BeginMS += offset
			w.EndMS += offset
			r.Words = append(r.Words, w)
		}
		r.PCM = append(r.PCM, pcmPart...)
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
		err = errors.Join(err, errors.New("provider usage unknown or invalid"))
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
	return &socket{Conn: conn, stop: context.AfterFunc(ctx, func() { _ = conn.Close() })}, nil
}

func (s *socket) close() { s.stop(); _ = s.Conn.Close() }
func (s *socket) send(v any) error {
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
	if k != websocket.TextMessage || json.Unmarshal(b, &ev) != nil {
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
	if ev.Header.TaskID != id || ev.Header.Event == "task-failed" {
		return ev, errors.New("task identity mismatch or failure")
	}
	if ev.Payload.Usage != nil {
		*usage = ev.Payload.Usage
	}
	return ev, nil
}
