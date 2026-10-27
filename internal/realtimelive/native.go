package realtimelive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

func sessionSettings(c Config, prompt string) map[string]any {
	s := map[string]any{"modalities": []string{"text", "audio"}, "instructions": prompt, "turn_detection": nil, "input_audio_transcription": nil,
		"audio": map[string]any{"input": map[string]any{"format": map[string]any{"type": "pcm", "sample_rate": 16000, "sample_format": "s16le", "channels": 1, "packing": "interleaved", "channel_layout": "mono"}}, "output": map[string]any{"voice": c.Voice, "format": map[string]any{"type": "pcm", "sample_rate": 24000}}}}
	if c.Search != nil {
		s["enable_search"] = *c.Search
		if *c.Search {
			s["search_options"] = map[string]any{"enable_source": true}
		}
	}
	if len(c.Tools) > 0 {
		s["modalities"] = []string{"text"}
		s["tools"] = c.Tools
		delete(s, "audio")
	}
	return s
}

type Native struct {
	cfg             Config
	dir             string
	conn            *websocket.Conn
	writeMu         sync.Mutex
	OnSearch        func(map[string]any) error
	searchConfirmed bool
}

func NewNative(c Config, dir string) *Native { return &Native{cfg: c, dir: dir} }
func (n *Native) write(v any) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	if n.conn == nil {
		return errors.New("native_not_connected")
	}
	n.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return n.conn.WriteJSON(v)
}
func (n *Native) Read(ctx context.Context) (map[string]any, error) {
	return n.read(ctx, 35*time.Second)
}
func (n *Native) ReadListener(ctx context.Context) (map[string]any, error) { return n.read(ctx, 0) }
func (n *Native) read(ctx context.Context, timeout time.Duration) (map[string]any, error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	if d, ok := ctx.Deadline(); ok && (deadline.IsZero() || d.Before(deadline)) {
		deadline = d
	}
	n.conn.SetReadDeadline(deadline)
	_, b, e := n.conn.ReadMessage()
	if e != nil {
		return nil, e
	}
	var v map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e = d.Decode(&v); e != nil {
		return nil, e
	}
	if v["type"] == "error" {
		return nil, errors.New("native_provider_error")
	}
	return v, nil
}
func (n *Native) expect(ctx context.Context, kind string) error {
	for range 64 {
		e, err := n.Read(ctx)
		if err != nil {
			return err
		}
		if e["type"] != kind {
			continue
		}
		if kind == "session.updated" && n.cfg.Search != nil {
			s := object(e["session"])
			enabled, present := s["enable_search"]
			if !present && n.searchConfirmed {
				return nil
			}
			if enabled != *n.cfg.Search {
				return errors.New("search_setting_not_confirmed")
			}
			n.searchConfirmed = true
			if n.OnSearch != nil {
				if err = n.OnSearch(map[string]any{"phase": "configured", "enabled": enabled, "source_requested": object(s["search_options"])["enable_source"] == true}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return errors.New("native_expected_event_missing")
}
func (n *Native) Open(ctx context.Context, prompt string, listener bool) (err error) {
	if n.cfg.Search != nil && *n.cfg.Search && len(n.cfg.Tools) > 0 {
		return errors.New("search_tools_incompatible")
	}
	audit, e := newIntent(n.dir, "native_session", n.cfg.Synthetic)
	if e != nil {
		return e
	}
	defer func() {
		if err != nil {
			audit.update(map[string]any{"state": "connection_outcome_unknown"})
			n.Close()
		}
	}()
	openCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	n.conn, _, err = websocket.DefaultDialer.DialContext(openCtx, n.cfg.URL, http.Header{"Authorization": []string{"Bearer " + n.cfg.Key}})
	if err != nil {
		return err
	}
	n.conn.SetReadLimit(1 << 21)
	connection := n.conn
	stopOpen := context.AfterFunc(openCtx, func() { connection.Close() })
	defer stopOpen()
	if err = n.expect(openCtx, "session.created"); err != nil {
		return err
	}
	if err = n.write(map[string]any{"type": "session.update", "session": sessionSettings(n.cfg, prompt)}); err != nil {
		return err
	}
	if err = n.expect(openCtx, "session.updated"); err != nil {
		return err
	}
	if err = audit.update(map[string]any{"state": "connected"}); err != nil {
		return err
	}
	if listener {
		if err = n.write(map[string]any{"type": "session.update", "session": map[string]any{"input_audio_transcription": map[string]any{"model": "qwen3-asr-flash-realtime"}, "turn_detection": map[string]any{"type": "server_vad", "threshold": .5, "silence_duration_ms": 600, "create_response": false, "interrupt_response": false}}}); err != nil {
			return err
		}
		return n.expect(openCtx, "session.updated")
	}
	return nil
}
func (n *Native) Close() {
	if n.conn != nil {
		n.conn.Close()
	}
}
func (n *Native) Append(pcm []byte) error {
	return n.write(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm)})
}
func (n *Native) cancel() {
	if n.conn == nil {
		return
	}
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	n.conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	n.conn.WriteJSON(map[string]any{"type": "response.cancel"})
}
func (n *Native) Stream(ctx context.Context, pcm []byte, emit func(string, []byte) error) (err error) {
	audit, e := newIntent(n.dir, "live_native_turn", n.cfg.Synthetic)
	if e != nil {
		return e
	}
	if e = audit.update(map[string]any{"state": "submission_outcome_unknown", "input_samples": len(pcm) / 2}); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { n.cancel(); n.Close() })
	defer stop()
	identity := ""
	size := 0
	transcript := ""
	completed := false
	defer func() {
		if !completed {
			ae := audit.update(map[string]any{"state": "cancelled_or_outcome_unknown", "received_samples": size / 2})
			if err == nil {
				err = ae
			}
			if identity != "" {
				n.cancel()
			}
		}
	}()
	for offset := 0; offset < len(pcm); offset += 640 {
		end := min(offset+640, len(pcm))
		if err = n.Append(pcm[offset:end]); err != nil {
			return err
		}
	}
	if err = n.write(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		return err
	}
	if err = n.expect(ctx, "input_audio_buffer.committed"); err != nil {
		return err
	}
	if err = n.write(map[string]any{"type": "response.create"}); err != nil {
		return err
	}
	for range 8192 {
		e, er := n.Read(ctx)
		if er != nil {
			return er
		}
		kind, _ := e["type"].(string)
		switch kind {
		case "response.created":
			if identity != "" {
				return errors.New("overlapping_response")
			}
			identity, _ = object(e["response"])["id"].(string)
		case "response.audio.delta":
			if identity == "" || e["response_id"] != identity {
				return errors.New("response_identity")
			}
			raw, _ := e["delta"].(string)
			frame, er := base64.StdEncoding.Strict().DecodeString(raw)
			if er != nil {
				return errors.New("output_encoding")
			}
			size += len(frame)
			if len(frame)%2 != 0 || size > 24000*2*60 {
				return errors.New("output_limit")
			}
			if size == len(frame) {
				if er = audit.update(map[string]any{"first_pcm_ms": time.Since(audit.start).Milliseconds()}); er != nil {
					return er
				}
			}
			if er = emit("pcm", frame); er != nil {
				return er
			}
		case "response.audio_transcript.delta", "response.audio_transcript.done":
			if e["response_id"] != identity {
				continue
			}
			if kind == "response.audio_transcript.delta" {
				v, _ := e["delta"].(string)
				transcript += v
			} else {
				transcript, _ = e["transcript"].(string)
			}
			if utf8.RuneCountInString(transcript) > 4096 {
				return errors.New("transcript_limit")
			}
			if er = emit("text", []byte(transcript)); er != nil {
				return er
			}
		case "response.done":
			r := object(e["response"])
			if r["id"] != identity || r["status"] != "completed" || size == 0 {
				return errors.New("response_terminal")
			}
			search := searchEvidence(r)
			if n.OnSearch != nil && len(search) > 0 {
				search["phase"] = "completed"
				search["response_id"] = identity
				if er = n.OnSearch(search); er != nil {
					return er
				}
				delete(search, "phase")
				delete(search, "response_id")
			}
			usage := map[string]any{}
			for k, v := range object(r["usage"]) {
				if _, ok := v.(json.Number); ok {
					usage[k] = v
				}
			}
			if er = audit.update(map[string]any{"state": "completed", "output_samples": size / 2, "usage": usage, "search": search}); er != nil {
				return er
			}
			completed = true
			return nil
		}
	}
	return errors.New("event_limit")
}
func integer(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, e := n.Int64()
	return i, e == nil
}
func searchEvidence(r map[string]any) map[string]any {
	out := map[string]any{}
	search := object(object(object(r["usage"])["plugins"])["search"])
	if c, ok := integer(search["count"]); ok && c >= 0 {
		out["count"] = c
	}
	if s, ok := search["strategy"].(string); ok {
		out["strategy"] = string([]rune(s)[:min(64, utf8.RuneCountInString(s))])
	}
	results, _ := object(r["search_info"])["search_results"].([]any)
	sources := []any{}
	for _, raw := range results[:min(32, len(results))] {
		s := object(raw)
		title, tok := s["title"].(string)
		link, lok := s["url"].(string)
		if !tok || !lok || utf8.RuneCountInString(link) > 4096 {
			continue
		}
		u, e := url.Parse(link)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(link, "\r\n") {
			continue
		}
		item := map[string]any{"title": string([]rune(title)[:min(512, utf8.RuneCountInString(title))]), "url": link}
		if i, ok := integer(s["index"]); ok {
			item["index"] = i
		}
		sources = append(sources, item)
	}
	if len(sources) > 0 {
		out["sources"] = sources
	}
	return out
}
