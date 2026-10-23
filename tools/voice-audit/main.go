// Isolated synthetic protocol audit. Receipt counters are simulated, never hardware evidence.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/protocol"
	"github.com/gorilla/websocket"
)

type wire struct {
	kind int
	data []byte
	err  error
}
type reply struct {
	Phase      string `json:"phase"`
	StartMS    int64  `json:"start_ms"`
	StopMS     int64  `json:"stop_ms"`
	Reason     string `json:"reason"`
	Text       string `json:"generated_text"`
	Frames     int    `json:"frames"`
	PlaybackID uint32 `json:"playback_id"`
}

func main() {
	dir := flag.String("dir", "", "fixtures")
	out := flag.String("out", "", "result file")
	scenario := flag.String("scenario", "interrupt", "normal, interrupt, wake-interrupt, resume-twice, clarify-choice or idle")
	order := flag.String("order", "cache-first", "cache-first or start-first")
	receipts := flag.Bool("receipts", false, "SIMULATED receipt counters")
	finalDelay := flag.Duration("interrupted-final-delay", 0, "SIMULATED interrupted final receipt delay, 0..5s")
	flag.Parse()
	if *dir == "" || *out == "" || *finalDelay < 0 || *finalDelay > 5*time.Second || (*finalDelay != 0 && !*receipts) || (*order != "cache-first" && *order != "start-first") || (*scenario != "normal" && *scenario != "interrupt" && *scenario != "wake-interrupt" && *scenario != "resume-twice" && *scenario != "clarify-choice" && *scenario != "idle") {
		fmt.Fprintln(os.Stderr, "valid dir, out, order and scenario are required")
		os.Exit(2)
	}
	target, e := url.Parse(os.Getenv("ROBOT_SMOKE_URL"))
	if e != nil || (target.Scheme != "ws" && target.Scheme != "wss") || (target.Hostname() != "127.0.0.1" && target.Hostname() != "localhost" && target.Hostname() != "::1") || target.Port() == "" || target.Port() == "8000" || target.User != nil {
		fmt.Fprintln(os.Stderr, "use an isolated loopback WebSocket port other than8000")
		os.Exit(2)
	}
	resultFile, e := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot create new result file")
		os.Exit(2)
	}
	defer resultFile.Close()
	result := map[string]any{"scenario": *scenario, "wake_order": *order, "simulated_receipts": *receipts, "synthetic_audio_only": true, "interrupted_final_delay_ms": finalDelay.Milliseconds()}
	started := time.Now()
	var replies []reply
	var stt []string
	events := []map[string]any{}
	err := func() error {
		fixtures := map[string][]byte{}
		names := []string{"hello", "count50", "stop", "query", "continue"}
		if *scenario == "resume-twice" {
			names = append(names, "continue_restart")
		}
		if *scenario == "clarify-choice" {
			names = append(names, "choice")
		}
		for _, name := range names {
			f, e := os.Open(filepath.Join(*dir, name+".wav"))
			if e != nil {
				return e
			}
			pcm, e := audio.ReadWAV(f)
			f.Close()
			if e != nil {
				return e
			}
			fixtures[name] = pcm
		}
		c, _, e := websocket.DefaultDialer.Dial(os.Getenv("ROBOT_SMOKE_URL"), http.Header{"Authorization": []string{"Bearer " + os.Getenv("ROBOT_DEVICE_TOKEN")}, "Device-Id": []string{os.Getenv("ROBOT_ALLOWED_DEVICE")}, "Protocol-Version": []string{"1"}})
		if e != nil {
			return fmt.Errorf("dial failed")
		}
		defer c.Close()
		writeJSON := func(v any) error { c.SetWriteDeadline(time.Now().Add(3 * time.Second)); return c.WriteJSON(v) }
		c.SetReadDeadline(time.Now().Add(230 * time.Second))
		if e := writeJSON(protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", Features: map[string]bool{"mcp": true, "playback_progress_v1": *receipts}, AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}}); e != nil {
			return fmt.Errorf("hello write failed")
		}
		c.SetReadLimit(65536)
		var hello protocol.Hello
		if c.ReadJSON(&hello) != nil || hello.Type != "hello" {
			return fmt.Errorf("hello failed")
		}
		for _, method := range []string{"initialize", "tools/list"} {
			var ev struct {
				Type    string `json:"type"`
				Payload struct {
					ID     any    `json:"id"`
					Method string `json:"method"`
				} `json:"payload"`
			}
			if c.ReadJSON(&ev) != nil || ev.Type != "mcp" || ev.Payload.Method != method {
				return fmt.Errorf("discovery failed")
			}
			result := map[string]any{}
			if method == "tools/list" {
				result["tools"] = []map[string]any{{"name": "self.get_device_status"}, {"name": "self.audio_speaker.set_volume"}, {"name": "self.screen.set_brightness"}}
			}
			if writeJSON(map[string]any{"type": "mcp", "session_id": hello.SessionID, "payload": map[string]any{"jsonrpc": "2.0", "id": ev.Payload.ID, "result": result}}) != nil {
				return fmt.Errorf("discovery write failed")
			}
		}
		enc, e := audio.NewEncoderAtRate(16000)
		if e != nil {
			return e
		}
		defer enc.Close()
		dec, e := audio.NewDecoder()
		if e != nil {
			return e
		}
		defer dec.Close()
		sendPCM := func(pcm []byte) error {
			frame := make([]byte, 1920)
			copy(frame, pcm)
			p, e := enc.Encode(frame)
			if e != nil {
				return e
			}
			c.SetWriteDeadline(time.Now().Add(3 * time.Second))
			return c.WriteMessage(websocket.BinaryMessage, p)
		}
		startListen := func() error {
			return writeJSON(protocol.Control{Type: "listen", State: "start", Mode: "realtime", SessionID: hello.SessionID})
		}
		if *order == "start-first" {
			if e := startListen(); e != nil {
				return e
			}
		}
		for pos := 0; pos < len(fixtures["hello"]); pos += 1920 {
			if e := sendPCM(fixtures["hello"][pos:]); e != nil {
				return e
			}
		}
		if e := writeJSON(protocol.Control{Type: "listen", State: "detect", Text: "你好小智", SessionID: hello.SessionID}); e != nil {
			return e
		}
		if *order != "start-first" {
			if e := startListen(); e != nil {
				return e
			}
		}
		incoming := make(chan wire, 256)
		done := make(chan struct{})
		defer close(done)
		go func() {
			for {
				k, d, e := c.ReadMessage()
				select {
				case incoming <- wire{k, d, e}:
				case <-done:
					return
				}
				if e != nil {
					return
				}
			}
		}()
		tick := time.NewTicker(60 * time.Millisecond)
		defer tick.Stop()
		deadline := time.NewTimer(225 * time.Second)
		defer deadline.Stop()
		phase := "greeting"
		var pending []byte
		nextName := ""
		nextAt := time.Time{}
		active := -1
		interjected := false
		secondInterjected := false
		earlyCount := false
		var inputAt time.Time
		type delayedFinal struct {
			reply reply
			at    time.Time
		}
		var finals []delayedFinal
		record := func(event string) {
			events = append(events, map[string]any{"event": event, "phase": phase, "ms": time.Since(started).Milliseconds()})
		}
		receipt := func(state string, r reply) error {
			if !*receipts || r.PlaybackID == 0 {
				return nil
			}
			return writeJSON(map[string]any{"type": "playback", "state": state, "session_id": hello.SessionID, "playback_id": r.PlaybackID, "sample_rate": 24000, "played_samples": r.Frames * 1440, "valid": true, "basis": "i2s_dma", "interrupted": r.Reason == "interrupted"})
		}
		for {
			select {
			case <-deadline.C:
				return fmt.Errorf("scenario deadline phase=%s", phase)
			case <-tick.C:
				for len(finals) > 0 && !time.Now().Before(finals[0].at) {
					if err := receipt("finished", finals[0].reply); err != nil {
						return err
					}
					record("delayed_interrupted_final")
					finals = finals[1:]
				}
				if nextName != "" && time.Now().After(nextAt) {
					pending = append([]byte(nil), fixtures[nextName]...)
					record("input_" + nextName)
					nextName = ""
					inputAt = time.Now()
				}
				frame := make([]byte, 1920)
				if len(pending) > 0 {
					n := copy(frame, pending)
					pending = pending[n:]
				}
				if e := sendPCM(frame); e != nil {
					return fmt.Errorf("pcm write failed")
				}
				if *scenario == "wake-interrupt" && phase == "greeting" && active >= 0 && !earlyCount && replies[active].Frames >= 10 {
					earlyCount = true
					pending = append([]byte(nil), fixtures["count50"]...)
					record("input_count50_during_greeting")
				}
				if (*scenario == "interrupt" || *scenario == "wake-interrupt" || *scenario == "resume-twice" || *scenario == "clarify-choice") && phase == "count" && active >= 0 && !interjected && replies[active].Frames >= 84 {
					interjected = true
					pending = append([]byte(nil), fixtures["stop"]...)
					inputAt = time.Now()
					record("input_stop")
				}
				if *scenario == "resume-twice" && phase == "resume_first" && active >= 0 && !secondInterjected && replies[active].Frames >= 84 {
					secondInterjected = true
					pending = append([]byte(nil), fixtures["stop"]...)
					inputAt = time.Now()
					record("input_stop_second")
				}
			case w := <-incoming:
				if w.err != nil {
					if phase == "idle" {
						elapsed := time.Since(inputAt).Milliseconds()
						result["idle_close_ms_after_reply"] = elapsed
						if elapsed >= 115000 && elapsed < 135000 {
							return nil
						}
					}
					return fmt.Errorf("socket ended phase=%s", phase)
				}
				if w.kind == websocket.BinaryMessage {
					if active < 0 {
						return fmt.Errorf("audio outside reply")
					}
					p, _, e := protocol.Unwrap(1, w.data)
					if e != nil {
						return e
					}
					if _, e := dec.Decode(p); e != nil {
						return e
					}
					replies[active].Frames++
					if replies[active].Frames%10 == 0 {
						if e := receipt("progress", replies[active]); e != nil {
							return e
						}
					}
					continue
				}
				var ev struct {
					Type       string `json:"type"`
					State      string `json:"state"`
					Reason     string `json:"reason"`
					Text       string `json:"text"`
					PlaybackID uint32 `json:"playback_id"`
				}
				if json.Unmarshal(w.data, &ev) != nil {
					return fmt.Errorf("bad control")
				}
				if ev.Type == "alert" {
					return fmt.Errorf("gateway upstream alert")
				}
				if ev.Type == "mcp" {
					return fmt.Errorf("unexpected device actuation request")
				}
				if ev.Type == "stt" {
					stt = append(stt, ev.Text)
					record("stt")
				}
				if ev.Type == "tts" && ev.State == "start" {
					replies = append(replies, reply{Phase: phase, StartMS: time.Since(started).Milliseconds(), PlaybackID: ev.PlaybackID})
					active = len(replies) - 1
					record("tts_start")
				}
				if ev.Type == "tts" && ev.State == "sentence_start" && active >= 0 {
					replies[active].Text = ev.Text
				}
				if ev.Type == "tts" && ev.State == "stop" {
					record("tts_stop_" + ev.Reason)
					if active < 0 {
						continue
					}
					r := &replies[active]
					r.StopMS = time.Since(started).Milliseconds()
					r.Reason = ev.Reason
					if r.Reason == "interrupted" && *finalDelay > 0 {
						finals = append(finals, delayedFinal{*r, time.Now().Add(*finalDelay)})
					} else if e := receipt("finished", *r); e != nil {
						return e
					}
					active = -1
					switch phase {
					case "greeting":
						if *scenario == "wake-interrupt" && (!earlyCount || ev.Reason != "interrupted") {
							return fmt.Errorf("intended greeting overlap not observed")
						}
						if *scenario == "idle" {
							phase = "idle"
							inputAt = time.Now()
							break
						}
						phase = "count"
						if !earlyCount {
							nextName = "count50"
							nextAt = time.Now().Add(1 * time.Second)
						}
					case "count":
						if *scenario == "normal" {
							return nil
						}
						if !interjected {
							return fmt.Errorf("count ended before synthetic interrupt could be queued")
						}
						if ev.Reason != "interrupted" {
							return fmt.Errorf("count ended without observed interruption")
						}
						result["interrupt_stop_ms_from_synthetic_input_queue"] = time.Since(inputAt).Milliseconds()
						phase = "math"
					case "math":
						if *scenario == "resume-twice" {
							phase = "resume_first"
							nextName = "continue_restart"
							nextAt = time.Now().Add(time.Second)
							break
						}
						phase = "query"
						nextName = "query"
						nextAt = time.Now().Add(time.Second)
					case "query":
						phase = "continue"
						nextName = "continue"
						nextAt = time.Now().Add(time.Second)
					case "continue":
						if *scenario == "clarify-choice" {
							phase = "specified_start"
							nextName = "choice"
							nextAt = time.Now().Add(time.Second)
							break
						}
						return nil
					case "specified_start":
						return nil
					case "resume_first":
						if !secondInterjected || ev.Reason != "interrupted" {
							return fmt.Errorf("resumed reply ended without second interruption")
						}
						result["second_interrupt_stop_ms_from_synthetic_input_queue"] = time.Since(inputAt).Milliseconds()
						phase = "math_second"
					case "math_second":
						phase = "query_second"
						nextName = "query"
						nextAt = time.Now().Add(time.Second)
					case "query_second":
						phase = "resume_final"
						nextName = "continue_restart"
						nextAt = time.Now().Add(time.Second)
					case "resume_final":
						return nil
					}
				}
			}
		}
	}()
	result["replies"] = replies
	result["input_transcripts"] = stt
	result["events"] = events
	result["total_ms"] = time.Since(started).Milliseconds()
	result["status"] = "transport_completed"
	if err != nil {
		result["status"] = "failed"
		result["error"] = err.Error()
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	if _, e := resultFile.Write(append(b, '\n')); e != nil {
		fmt.Fprintln(os.Stderr, "result write failed")
		os.Exit(1)
	}
	fmt.Println(string(b))
	if err != nil {
		os.Exit(1)
	}
}
