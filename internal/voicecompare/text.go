package voicecompare

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type outputItem struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func validTextItem(item outputItem, text string, complete bool) bool {
	if item.Type != "message" || item.Role != "assistant" || len(item.Content) > 1 || complete && len(item.Content) != 1 {
		return false
	}
	return len(item.Content) == 0 || item.Content[0].Type == "text" && (!complete || item.Content[0].Text == text)
}

func (c Client) native(ctx context.Context, prompt string, pcm []byte) (text string, call Call, err error) {
	// max_tokens truncates returned output, not internal generation or billing.
	// Reserve for up to 65,536 generated text tokens plus bounded audio/prompt.
	call, err = c.call(ctx, "native", .4, func(usage *map[string]any) error {
		s, e := c.dial(ctx, c.RealtimeURL)
		if e != nil {
			return e
		}
		defer s.close()
		ev, e := s.event()
		if e != nil {
			return e
		}
		if ev.Type != "session.created" {
			return errors.New("missing native session")
		}
		session := map[string]any{"modalities": []string{"text"}, "turn_detection": nil, "input_audio_transcription": nil, "instructions": prompt, "enable_search": false, "max_tokens": 256, "tools": []any{}, "audio": map[string]any{"input": map[string]any{"format": map[string]any{"type": "pcm", "sample_rate": 16000, "sample_format": "s16le", "channels": 1, "packing": "interleaved", "channel_layout": "mono"}}}}
		if e = s.send(map[string]any{"type": "session.update", "session": session}); e != nil {
			return e
		}
		ev, e = s.event()
		if e != nil {
			return e
		}
		if ev.Type != "session.updated" || !sessionAcknowledged(ev.Session, session) {
			return errors.New("missing or unsafe native session update")
		}
		for _, v := range []any{map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm)}, map[string]any{"type": "input_audio_buffer.commit"}} {
			if e = s.send(v); e != nil {
				return e
			}
		}
		for {
			ev, e = s.event()
			if e != nil {
				return e
			}
			if ev.Type == "input_audio_buffer.committed" {
				break
			}
			if ev.Type != "conversation.item.created" || !validAudioInput(ev.Item) {
				return errors.New("missing native committed ACK")
			}
		}
		if e = s.send(map[string]any{"type": "response.create", "response": map[string]any{"modalities": []string{"text"}, "instructions": prompt}}); e != nil {
			return e
		}
		id, delta := "", ""
		done := false
		for {
			ev, e = s.event()
			if e != nil {
				return e
			}
			if ev.Type == "conversation.item.created" && id == "" {
				if !validAudioInput(ev.Item) {
					return errors.New("unexpected native input item")
				}
				continue
			}
			if ev.Type == "response.created" {
				var v struct {
					ID         string            `json:"id"`
					Output     []json.RawMessage `json:"output"`
					Modalities []string          `json:"modalities"`
				}
				if id != "" || json.Unmarshal(ev.Response, &v) != nil || v.ID == "" || len(v.Output) != 0 || len(v.Modalities) > 0 && (len(v.Modalities) != 1 || v.Modalities[0] != "text") {
					return errors.New("invalid native response identity")
				}
				id = v.ID
				continue
			}
			if id == "" {
				return errors.New("missing native response identity")
			}
			if ev.Type != "response.done" && (ev.ResponseID != id || ev.OutputIndex != nil && *ev.OutputIndex != 0 || ev.ContentIndex != nil && *ev.ContentIndex != 0) {
				return errors.New("native response identity mismatch")
			}
			switch ev.Type {
			case "response.text.delta":
				if done || len(delta)+len(ev.Delta) > 480 {
					return errors.New("native reply limit exceeded")
				}
				delta += ev.Delta
			case "response.text.done":
				if done || !boundedText(ev.Text, 120) || delta != "" && delta != ev.Text {
					return errors.New("invalid native complete text")
				}
				text = ev.Text
				done = true
			case "response.output_item.added", "response.output_item.done":
				var item outputItem
				complete := ev.Type == "response.output_item.done"
				if json.Unmarshal(ev.Item, &item) != nil || !validTextItem(item, text, complete) || complete && !done {
					return errors.New("invalid native output item")
				}
			case "response.content_part.added", "response.content_part.done":
				var p struct{ Type, Text string }
				if json.Unmarshal(ev.Part, &p) != nil || p.Type != "text" || ev.Type == "response.content_part.done" && (!done || p.Text != text) {
					return errors.New("invalid native content part")
				}
			case "response.done":
				var v struct {
					ID, Status string
					Output     []outputItem
					Usage      map[string]any
				}
				if json.Unmarshal(ev.Response, &v) != nil {
					return errors.New("invalid native terminal")
				}
				*usage = v.Usage
				if v.ID != id || v.Status != "completed" || !done || len(v.Output) != 1 || !validTextItem(v.Output[0], text, true) {
					return errors.New("incomplete native terminal")
				}
				return nil
			default:
				return errors.New("unexpected native event or non-text output")
			}
		}
	})
	return
}

func validAudioInput(raw json.RawMessage) bool {
	var item outputItem
	return json.Unmarshal(raw, &item) == nil && item.Type == "message" && item.Role == "user" && len(item.Content) == 1 && item.Content[0].Type == "input_audio"
}

func sessionAcknowledged(raw json.RawMessage, want map[string]any) bool {
	var got map[string]any
	if json.Unmarshal(raw, &got) != nil {
		return false
	}
	return requestedFieldsMatch(got, want)
}

func requestedFieldsMatch(got, want any) bool {
	if fields, ok := want.(map[string]any); ok {
		actual, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range fields {
			v, present := actual[key]
			if !present || !requestedFieldsMatch(v, value) {
				return false
			}
		}
		return true
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	return bytes.Equal(a, b)
}

func (c Client) asr(ctx context.Context, pcm []byte) (text string, call Call, err error) {
	call, err = c.call(ctx, "asr", .02, func(usage *map[string]any) error {
		s, e := c.dial(ctx, c.InferenceURL)
		if e != nil {
			return e
		}
		defer s.close()
		id := taskID()
		if e = s.send(taskCommand("run-task", id, map[string]any{"task_group": "audio", "task": "asr", "function": "recognition", "model": "fun-asr-realtime", "parameters": map[string]any{"format": "pcm", "sample_rate": 16000}, "input": map[string]any{}})); e != nil {
			return e
		}
		ev, e := s.taskEvent(id, usage)
		if e != nil {
			return e
		}
		if ev.Header.Event != "task-started" {
			return errors.New("missing ASR task-started")
		}
		for begin := 0; begin < len(pcm); begin += 3200 {
			end := min(begin+3200, len(pcm))
			if s.WriteMessage(websocket.BinaryMessage, pcm[begin:end]) != nil {
				return errors.New("ASR write failed")
			}
		}
		if e = s.send(taskCommand("finish-task", id, map[string]any{"input": map[string]any{}})); e != nil {
			return e
		}
		for {
			ev, e = s.taskEvent(id, usage)
			if e != nil {
				return e
			}
			if ev.Header.Event == "task-finished" {
				if !boundedText(text, 512) {
					return errors.New("missing ASR final text")
				}
				return nil
			}
			if ev.Header.Event != "result-generated" {
				return errors.New("unexpected ASR event")
			}
			var out struct {
				Sentence struct {
					Text      string `json:"text"`
					Final     bool   `json:"sentence_end"`
					Heartbeat bool   `json:"heartbeat"`
				} `json:"sentence"`
			}
			if json.Unmarshal(ev.Payload.Output, &out) != nil || len(out.Sentence.Text) > 2048 {
				return errors.New("invalid ASR result")
			}
			if out.Sentence.Final && !out.Sentence.Heartbeat {
				if !boundedText(out.Sentence.Text, 512) {
					return errors.New("empty ASR final")
				}
				text += out.Sentence.Text
				if !boundedText(text, 512) {
					return errors.New("ASR text limit exceeded")
				}
			}
		}
	})
	return
}

func (c Client) llm(ctx context.Context, prompt, input string) (text string, call Call, err error) {
	call, err = c.call(ctx, "llm", .05, func(usage *map[string]any) error {
		body, _ := json.Marshal(map[string]any{"model": "qwen-plus", "enable_thinking": false, "enable_search": false, "stream": false, "max_tokens": 256, "messages": []any{map[string]any{"role": "system", "content": prompt}, map[string]any{"role": "user", "content": input}}})
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.TextURL, bytes.NewReader(body))
		if e != nil {
			return errors.New("invalid text request")
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")
		h := http.Client{Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, e := h.Do(req)
		if e != nil {
			return errors.New("text request failed")
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
		if e != nil || len(b) > 65536 {
			return errors.New("text response limit exceeded")
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New("text provider rejected request")
		}
		var v struct {
			Choices []struct {
				Finish  string `json:"finish_reason"`
				Message struct {
					Role     string            `json:"role"`
					Content  string            `json:"content"`
					Tools    []json.RawMessage `json:"tool_calls"`
					Function json.RawMessage   `json:"function_call"`
				} `json:"message"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
		}
		if json.Unmarshal(b, &v) != nil {
			return errors.New("invalid text response")
		}
		*usage = v.Usage
		if len(v.Choices) != 1 {
			return errors.New("invalid text choice count")
		}
		choice := v.Choices[0]
		if choice.Finish != "stop" || choice.Message.Role != "assistant" || len(choice.Message.Tools) != 0 || len(choice.Message.Function) > 0 && string(choice.Message.Function) != "null" || !boundedText(choice.Message.Content, 120) {
			return errors.New("incomplete or unsafe text response")
		}
		text = strings.TrimSpace(choice.Message.Content)
		return nil
	})
	return
}
