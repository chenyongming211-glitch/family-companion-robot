package voicecompare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func fixtureClient(url string) Client {
	return Client{Key: "fake-key", RealtimeURL: url, InferenceURL: url, TextURL: strings.Replace(url, "ws:", "http:", 1), TTSModel: "cosyvoice-v3-flash", Voice: "fixture", BeforeCall: func(string, float64) error { return nil }, AfterCall: func(string, map[string]any, error) error { return nil }}
}

func wsFixture(t *testing.T, handler func(*websocket.Conn)) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		handler(conn)
	}))
	t.Cleanup(s.Close)
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

func readJSON(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	var v map[string]any
	if err := c.ReadJSON(&v); err != nil {
		t.Error(err)
	}
	return v
}

func usageFixture(stage string) map[string]any {
	switch stage {
	case "tts":
		return map[string]any{"characters": float64(3)}
	case "asr":
		return map[string]any{"duration": .1}
	case "llm":
		return map[string]any{"prompt_tokens": float64(4), "completion_tokens": float64(3), "total_tokens": float64(7)}
	default:
		return map[string]any{"input_tokens": float64(3), "output_tokens": float64(3), "total_tokens": float64(6), "input_tokens_details": map[string]any{"text_tokens": float64(1), "audio_tokens": float64(2)}}
	}
}

func taskEvent(c *websocket.Conn, id, event string, output any, overrides ...map[string]any) {
	usage := usageFixture("tts")
	if len(overrides) > 0 {
		usage = overrides[0]
	}
	_ = c.WriteJSON(map[string]any{"header": map[string]any{"task_id": id, "event": event}, "payload": map[string]any{"output": output, "usage": usage}})
}

func asrEvent(c *websocket.Conn, id, event string, output any) {
	taskEvent(c, id, event, output, usageFixture("asr"))
}

func ttsFixture(t *testing.T, c *websocket.Conn, mode string) {
	t.Helper()
	run := readJSON(t, c)
	h, ok := run["header"].(map[string]any)
	if !ok {
		return
	}
	id := h["task_id"].(string)
	p := run["payload"].(map[string]any)
	if p["task"] != "tts" || p["model"] != "cosyvoice-v3-flash" {
		t.Errorf("invalid TTS request: %v", p)
	}
	params := p["parameters"].(map[string]any)
	if params["word_timestamp_enabled"] != true || params["sample_rate"] != float64(24000) {
		t.Errorf("invalid TTS parameters: %v", params)
	}
	if mode == "wrong-task" {
		taskEvent(c, "wrong", "task-started", nil)
		return
	}
	taskEvent(c, id, "task-started", nil)
	cont := readJSON(t, c)
	text := cont["payload"].(map[string]any)["input"].(map[string]any)["text"].(string)
	if readJSON(t, c)["header"].(map[string]any)["action"] != "finish-task" {
		t.Error("missing finish-task")
	}
	if mode == "cancel" {
		_, _, _ = c.ReadMessage()
		return
	}
	if mode == "binary-first" {
		_ = c.WriteMessage(websocket.BinaryMessage, make([]byte, 9600))
		return
	}
	out := func(typ string, index int, words any) map[string]any {
		return map[string]any{"type": typ, "original_text": text, "sentence": map[string]any{"index": index, "words": words}}
	}
	taskEvent(c, id, "result-generated", out("sentence-begin", 0, []any{}))
	if mode == "late-metadata" {
		_ = c.WriteMessage(websocket.BinaryMessage, make([]byte, 9600))
		return
	}
	taskEvent(c, id, "result-generated", out("sentence-synthesis", 0, []any{}))
	if mode == "metadata-without-pcm" {
		taskEvent(c, id, "task-finished", nil)
		return
	}
	_ = c.WriteMessage(websocket.BinaryMessage, make([]byte, 9600))
	word := map[string]any{"text": strings.TrimRight(text, "。！？"), "begin_index": 0, "end_index": len([]rune(text)) - 1, "begin_time": 0, "end_time": 200}
	words := []any{word}
	index := 0
	switch mode {
	case "missing-words":
		words = nil
	case "negative":
		word["begin_time"] = -1
	case "out-of-range":
		word["end_time"] = 201
	case "wrong-text":
		word["text"] = "错误"
	case "wrong-index":
		word["end_index"] = 200
	case "other-sentence":
		index = 1
	case "nonmonotonic":
		words = append(words, map[string]any{"text": "好", "begin_index": 1, "end_index": 2, "begin_time": 100, "end_time": 150})
	}
	taskEvent(c, id, "result-generated", out("sentence-end", index, words))
	if mode != "missing-terminal" {
		usage := usageFixture("tts")
		if mode == "bad-usage" {
			usage["characters"] = nil
		}
		taskEvent(c, id, "task-finished", nil, usage)
	}
}

func TestTTSOffsetsAndConfirmedText(t *testing.T) {
	url := wsFixture(t, func(c *websocket.Conn) { ttsFixture(t, c, "") })
	c := fixtureClient(url)
	var stages []string
	c.BeforeCall = func(stage string, reserve float64) error {
		stages = append(stages, stage)
		if reserve <= 0 || reserve > .1 {
			t.Error("invalid reserve")
		}
		return nil
	}
	r, err := c.Run(context.Background(), "tts", "", nil, "你好。再见。")
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "你好。再见。" || len(r.PCM) != 19200 || len(r.Words) != 2 || r.Words[1].BeginMS != 200 || r.Words[1].EndMS != 400 || len(r.Calls) != 2 {
		t.Fatalf("invalid result: %+v", r)
	}
	if got := ConfirmedText(r.Words, 7200, 50); got != "你好。" {
		t.Errorf("confirmed %q", got)
	}
	if fmt.Sprint(stages) != "[tts tts]" {
		t.Error(stages)
	}
	if got := ConfirmedText([]Word{{Text: "a", BeginMS: 0, EndMS: 100}, {Text: "b", BeginMS: 50, EndMS: 200}}, 24000, 0); got != "" {
		t.Errorf("invalid timestamps accepted: %q", got)
	}
}

func TestTTSRejectsUnverifiedProtocol(t *testing.T) {
	for _, mode := range []string{"wrong-task", "binary-first", "late-metadata", "metadata-without-pcm", "missing-words", "negative", "out-of-range", "wrong-text", "wrong-index", "other-sentence", "nonmonotonic", "missing-terminal", "bad-usage"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			url := wsFixture(t, func(c *websocket.Conn) { calls.Add(1); ttsFixture(t, c, mode) })
			c := fixtureClient(url)
			after := false
			c.AfterCall = func(stage string, usage map[string]any, err error) error {
				after = true
				if err == nil {
					t.Error("expected persisted error")
				}
				return nil
			}
			r, err := c.Run(context.Background(), "tts", "", nil, "你好。")
			if err == nil || len(r.PCM) != 0 || !after || calls.Load() != 1 {
				t.Errorf("accepted invalid protocol: err=%v after=%v calls=%d", err, after, calls.Load())
			}
		})
	}
}

func TestInputAndCostGateBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer s.Close()
	c := fixtureClient("ws" + strings.TrimPrefix(s.URL, "http"))
	c.BeforeCall = func(string, float64) error { return errors.New("budget refused") }
	if _, err := c.Run(context.Background(), "tts", "", nil, "你好。"); err == nil {
		t.Error("gate ignored")
	}
	intents := 0
	c.BeforeCall = func(string, float64) error { intents++; return errors.New("unexpected intent") }
	for _, tc := range []struct {
		backend, text string
		pcm           []byte
	}{{"unknown", "你好", nil}, {"tts", strings.Repeat("字", 257), nil}, {"native", "", []byte{1}}, {"cascade", "", make([]byte, 320002)}} {
		if _, err := c.Run(context.Background(), tc.backend, "", tc.pcm, tc.text); err == nil {
			t.Errorf("accepted invalid %s", tc.backend)
		}
	}
	for _, bad := range []string{"ws://example.com/x", "http://example.com/x", "wss://user:pass@example.com/x", "wss://example.com/x#fragment"} {
		c.InferenceURL = bad
		if _, err := c.Run(context.Background(), "tts", "", nil, "你好。"); err == nil {
			t.Errorf("accepted unsafe endpoint %s", bad)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("gate made %d network calls", requests.Load())
	}
	if intents != 0 {
		t.Errorf("invalid inputs created %d intents", intents)
	}
}

func TestTimestampWordsKeepExactSourceSeparators(t *testing.T) {
	if parts := splitClauses("-1.2。"); len(parts) != 1 {
		t.Errorf("decimal split into %q", parts)
	}
	for _, text := range []string{"an other.", "-1.2。"} {
		var raw []timestampWord
		positions := [][2]int64{{0, 2}, {3, 8}}
		if text == "-1.2。" {
			positions = [][2]int64{{1, 2}, {3, 4}}
		}
		runes := []rune(text)
		for i, p := range positions {
			begin, end := int64(i*100), int64((i+1)*100)
			b, e := p[0], p[1]
			raw = append(raw, timestampWord{Text: string(runes[b:e]), Begin: &begin, End: &end, BeginIndex: &b, EndIndex: &e})
		}
		words, err := validateWords(text, raw, 200)
		if err != nil {
			t.Fatal(err)
		}
		if got := ConfirmedText(words, 4800, 0); got != text {
			t.Errorf("source formatting changed: got %q want %q", got, text)
		}
	}
}

func TestPersistenceFailureStopsFurtherTasks(t *testing.T) {
	var requests atomic.Int32
	c := fixtureClient(wsFixture(t, func(conn *websocket.Conn) { requests.Add(1); ttsFixture(t, conn, "") }))
	c.AfterCall = func(string, map[string]any, error) error { return errors.New("ledger unavailable") }
	r, err := c.Run(context.Background(), "tts", "", nil, "你好。再见。")
	if err == nil || len(r.PCM) != 0 || requests.Load() != 1 || len(r.Calls) != 1 {
		t.Fatalf("persistence failure did not stop: %v calls=%d", err, requests.Load())
	}
}

func TestUnknownUsageIsPersistedAsError(t *testing.T) {
	for _, usage := range []map[string]any{nil, {}} {
		c := fixtureClient("")
		var outcome error
		c.AfterCall = func(_ string, _ map[string]any, err error) error { outcome = err; return nil }
		_, err := c.call(context.Background(), "native", .1, func(out *map[string]any) error { *out = usage; return nil })
		if err == nil || outcome == nil {
			t.Errorf("unknown usage accepted: err=%v persisted=%v", err, outcome)
		}
	}
}

func TestRequiredUsageCountersAreFiniteAndNumeric(t *testing.T) {
	for _, stage := range []string{"tts", "asr", "llm", "native"} {
		cases := []map[string]any{nil, {}, {"unexpected": float64(0)}, usageFixture(stage)}
		field := map[string]string{"tts": "characters", "asr": "duration", "llm": "completion_tokens", "native": "output_tokens"}[stage]
		for _, bad := range []any{nil, "1", float64(-1), math.NaN(), math.Inf(1)} {
			u := usageFixture(stage)
			u[field] = bad
			cases = append(cases, u)
		}
		if stage != "asr" {
			u := usageFixture(stage)
			u[field] = .5
			cases = append(cases, u)
		}
		if stage == "native" {
			u := usageFixture(stage)
			u["input_tokens_details"] = map[string]any{"text_tokens": float64(1)}
			cases = append(cases, u)
			u = usageFixture(stage)
			u["output_tokens_details"] = map[string]any{"audio_tokens": float64(1)}
			cases = append(cases, u)
		}
		for i, u := range cases {
			c := fixtureClient("")
			var persisted error
			c.AfterCall = func(_ string, _ map[string]any, e error) error { persisted = e; return nil }
			_, err := c.call(context.Background(), stage, .1, func(out *map[string]any) error { *out = u; return nil })
			valid := i == 3
			if (err == nil) != valid || (persisted == nil) != valid {
				t.Errorf("stage=%s case=%d malformed usage err=%v persisted=%v", stage, i, err, persisted)
			}
		}
	}
}

func TestEarlierVerifiedClausesSurviveLaterFailure(t *testing.T) {
	for _, fail := range []string{"budget", "protocol"} {
		t.Run(fail, func(t *testing.T) {
			var requests atomic.Int32
			c := fixtureClient(wsFixture(t, func(conn *websocket.Conn) {
				mode := ""
				if requests.Add(1) == 2 {
					mode = "missing-terminal"
				}
				ttsFixture(t, conn, mode)
			}))
			intents := 0
			c.BeforeCall = func(string, float64) error {
				intents++
				if intents == 2 && fail == "budget" {
					return errors.New("budget")
				}
				return nil
			}
			r, err := c.Run(context.Background(), "tts", "", nil, "你好。再见。")
			if err == nil || r.Text != "你好。再见。" || len(r.PCM) != 9600 || len(r.Words) != 1 || ConfirmedText(r.Words, 4800, 0) != "你好。" {
				t.Errorf("verified partial result lost: err=%v text=%q pcm=%d words=%v", err, r.Text, len(r.PCM), r.Words)
			}
		})
	}
}

func TestASRRequiresMatchingFinalAndTerminal(t *testing.T) {
	for _, mode := range []string{"wrong-task", "provisional-only", "missing-terminal"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			c := fixtureClient(wsFixture(t, func(conn *websocket.Conn) {
				requests.Add(1)
				run := readJSON(t, conn)
				id := run["header"].(map[string]any)["task_id"].(string)
				if mode == "wrong-task" {
					asrEvent(conn, "wrong", "task-started", nil)
					return
				}
				asrEvent(conn, id, "task-started", nil)
				_, _, _ = conn.ReadMessage()
				readJSON(t, conn)
				asrEvent(conn, id, "result-generated", map[string]any{"sentence": map[string]any{"text": "你好", "sentence_end": mode != "provisional-only"}})
				if mode != "missing-terminal" {
					asrEvent(conn, id, "task-finished", nil)
				}
			}))
			_, _, err := c.asr(context.Background(), make([]byte, 3200))
			if err == nil || requests.Load() != 1 {
				t.Errorf("accepted incomplete ASR: %v calls=%d", err, requests.Load())
			}
		})
	}
}

func TestLLMRejectsIncompleteToolsOversizeAndRedirect(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"你好"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"你好","tool_calls":[{}]}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"` + strings.Repeat("字", 121) + `"}}]}`,
		strings.Repeat("x", 65537),
		"redirect",
	} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			var requests atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if body == "redirect" {
					http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
					return
				}
				_, _ = fmt.Fprint(w, body)
			}))
			defer s.Close()
			c := fixtureClient("")
			c.TextURL = s.URL
			_, _, err := c.llm(context.Background(), "简短回答", "你好")
			if err == nil || requests.Load() != 1 {
				t.Errorf("accepted unsafe reply: %v calls=%d", err, requests.Load())
			}
		})
	}
}

func TestCancellationClosesWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	url := wsFixture(t, func(c *websocket.Conn) { calls.Add(1); ttsFixture(t, c, "cancel") })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := fixtureClient(url).Run(ctx, "tts", "", nil, "你好。")
	if err == nil || time.Since(start) > time.Second || calls.Load() != 1 {
		t.Errorf("cancellation err=%v calls=%d", err, calls.Load())
	}
}

func nativeFixture(t *testing.T, c *websocket.Conn, bad string) {
	_ = c.WriteJSON(map[string]any{"type": "session.created"})
	update := readJSON(t, c)["session"].(map[string]any)
	if value, ok := update["input_audio_transcription"]; !ok || value != nil {
		t.Error("auxiliary ASR not explicitly disabled")
	}
	if fmt.Sprint(update["modalities"]) != "[text]" || update["turn_detection"] != nil || update["enable_search"] != false || update["max_tokens"] != float64(256) || update["audio"].(map[string]any)["input"].(map[string]any)["format"].(map[string]any)["sample_rate"] != float64(16000) {
		t.Errorf("invalid text-only session %v", update)
	}
	if bad == "audio-ack" {
		update["modalities"] = []string{"text", "audio"}
	}
	if bad == "search-ack" {
		update["enable_search"] = true
	}
	if bad == "tokens-ack" {
		delete(update, "max_tokens")
	}
	if bad == "asr-ack" {
		update["input_audio_transcription"] = map[string]any{"model": "qwen3-asr-flash-realtime"}
	}
	if bad == "defaults" {
		update["audio"].(map[string]any)["output"] = map[string]any{"voice": "Tina", "format": map[string]any{"type": "pcm", "sample_rate": 24000}}
	}
	if bad == "input-format-ack" {
		update["audio"].(map[string]any)["input"].(map[string]any)["format"].(map[string]any)["sample_rate"] = 24000
	}
	if bad == "missing-null-ack" {
		delete(update, "input_audio_transcription")
	}
	ack := map[string]any{"type": "session.updated", "session": update}
	if bad == "missing-ack" {
		delete(ack, "session")
	}
	_ = c.WriteJSON(ack)
	if strings.HasSuffix(bad, "-ack") {
		if _, _, err := c.ReadMessage(); err == nil {
			t.Error("sent audio after unsafe session ACK")
		}
		return
	}
	for _, typ := range []string{"input_audio_buffer.append", "input_audio_buffer.commit"} {
		if got := readJSON(t, c)["type"]; got != typ {
			t.Errorf("got %v want %s", got, typ)
		}
	}
	request := make(chan map[string]any, 1)
	go func() { var v map[string]any; _ = c.ReadJSON(&v); request <- v }()
	var response map[string]any
	select {
	case response = <-request:
		t.Error("response.create sent before committed ACK")
	case <-time.After(10 * time.Millisecond):
	}
	_ = c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
	if response == nil {
		response = <-request
	}
	if response["type"] != "response.create" {
		t.Error("missing response.create after committed ACK")
	}
	_ = c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
	id := "r1"
	if bad == "wrong-response" {
		id = "other"
	}
	if bad == "audio" {
		_ = c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": id, "delta": "AAAA"})
		return
	}
	_ = c.WriteJSON(map[string]any{"type": "response.text.done", "response_id": id, "text": "你好。"})
	status := "completed"
	if bad == "incomplete" {
		status = "incomplete"
	}
	usage := usageFixture("native")
	if bad == "unknown-usage" {
		usage = nil
	}
	if bad == "bad-usage" {
		usage["input_tokens"] = nil
	}
	_ = c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r1", "status": status, "usage": usage, "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": "你好。"}}}}}})
}

func TestNativeTextOnlyThenSameTTS(t *testing.T) {
	for _, bad := range []string{"", "defaults", "audio", "wrong-response", "incomplete", "audio-ack", "search-ack", "tokens-ack", "asr-ack", "missing-null-ack", "input-format-ack", "missing-ack", "unknown-usage", "bad-usage"} {
		t.Run(bad, func(t *testing.T) {
			c := fixtureClient(wsFixture(t, func(c *websocket.Conn) { ttsFixture(t, c, "") }))
			c.RealtimeURL = wsFixture(t, func(c *websocket.Conn) { nativeFixture(t, c, bad) })
			var stages []string
			c.BeforeCall = func(stage string, _ float64) error { stages = append(stages, stage); return nil }
			r, err := c.Run(context.Background(), "native", "简短回答", make([]byte, 3200), "")
			if bad != "" && bad != "defaults" {
				if err == nil || len(r.PCM) > 0 {
					t.Error("accepted invalid native reply")
				}
				if bad == "unknown-usage" && r.Text != "你好。" {
					t.Error("completed generated text lost after unknown usage")
				}
				if bad == "bad-usage" && fmt.Sprint(stages) != "[native]" {
					t.Errorf("paid stages continued after invalid usage: %v", stages)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Text != "你好。" || len(r.Calls) != 2 || r.Calls[0].Usage["input_tokens"] != float64(3) {
				t.Errorf("invalid native result %+v", r)
			}
		})
	}
}

func TestCascadeFinalASRCompletedLLMThenTTS(t *testing.T) {
	c := fixtureClient("")
	c.InferenceURL = wsFixture(t, func(conn *websocket.Conn) {
		run := readJSON(t, conn)
		id := run["header"].(map[string]any)["task_id"].(string)
		p := run["payload"].(map[string]any)
		if p["task"] == "tts" {
			taskEvent(conn, id, "task-started", nil)
			readJSON(t, conn)
			readJSON(t, conn)
			out := func(typ string, words any) any {
				return map[string]any{"type": typ, "original_text": "你好。", "sentence": map[string]any{"index": 0, "words": words}}
			}
			taskEvent(conn, id, "result-generated", out("sentence-begin", []any{}))
			taskEvent(conn, id, "result-generated", out("sentence-synthesis", []any{}))
			_ = conn.WriteMessage(websocket.BinaryMessage, make([]byte, 9600))
			taskEvent(conn, id, "result-generated", out("sentence-end", []any{map[string]any{"text": "你好", "begin_index": 0, "end_index": 2, "begin_time": 0, "end_time": 200}}))
			taskEvent(conn, id, "task-finished", nil)
			return
		}
		if p["model"] != "fun-asr-realtime" {
			t.Error("wrong ASR model")
		}
		asrEvent(conn, id, "task-started", nil)
		kind, pcm, err := conn.ReadMessage()
		if kind != websocket.BinaryMessage || len(pcm) != 3200 || err != nil {
			t.Error("wrong input")
		}
		readJSON(t, conn)
		asrEvent(conn, id, "result-generated", map[string]any{"sentence": map[string]any{"text": "问候一下", "sentence_end": true}})
		asrEvent(conn, id, "task-finished", nil)
	})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req["model"] != "qwen-plus" || req["enable_thinking"] != false || req["stream"] != false {
			t.Error("invalid LLM options")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"你好。"}}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`)
	}))
	defer s.Close()
	c.TextURL = s.URL
	r, err := c.Run(context.Background(), "cascade", "简短回答", make([]byte, 3200), "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "你好。" || len(r.Calls) != 3 || r.Calls[0].Stage != "asr" || r.Calls[1].Stage != "llm" || r.Calls[2].Stage != "tts" {
		t.Errorf("wrong cascade %+v", r)
	}
}
