// smoke is a bounded, real-Opus Xiaozhi client for synthetic test recordings.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/protocol"
	"github.com/gorilla/websocket"
)

type result struct {
	Status            string   `json:"status"`
	Mode              string   `json:"mode"`
	AbortAfterFrames  int      `json:"abort_after_frames"`
	AudioFrames       []int    `json:"audio_frames"`
	InputTranscripts  []string `json:"input_transcripts"`
	OutputTranscripts []string `json:"output_transcripts"`
	FirstAudioMS      []int64  `json:"first_audio_ms_after_input"`
	AbortStopMS       int64    `json:"abort_stop_ms,omitempty"`
	TotalMS           int64    `json:"total_ms"`
}

func main() {
	input := flag.String("input", "", "mono 16kHz WAV")
	follow := flag.String("followup", "", "second mono 16kHz WAV")
	out := flag.String("out", "", "new result directory")
	mode := flag.String("mode", "manual", "manual or realtime")
	abort := flag.Int("abort-after", 0, "abort first response after N frames")
	flag.Parse()
	if err := run(*input, *follow, *out, *mode, *abort); err != nil {
		fmt.Fprintln(os.Stderr, "smoke failed:", err)
		os.Exit(1)
	}
}
func readInput(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return audio.ReadWAV(f)
}
func run(input, follow, out, mode string, abort int) error {
	if input == "" || follow == "" || out == "" || (mode != "manual" && mode != "realtime") || abort < 0 {
		return errors.New("input, followup and out are required; mode must be manual/realtime")
	}
	first, err := readInput(input)
	if err != nil {
		return err
	}
	second, err := readInput(follow)
	if err != nil {
		return err
	}
	if err := os.Mkdir(out, 0750); err != nil {
		return err
	}
	url := os.Getenv("ROBOT_SMOKE_URL")
	if url == "" {
		url = "ws://127.0.0.1:8000/xiaozhi/v1/"
	}
	token := os.Getenv("ROBOT_DEVICE_TOKEN")
	device := os.Getenv("ROBOT_ALLOWED_DEVICE")
	if device == "" {
		device = "02:00:00:00:00:01"
	}
	if token == "" {
		return errors.New("device token is required in environment")
	}
	c, resp, err := (&websocket.Dialer{HandshakeTimeout: 8 * time.Second}).Dial(url, http.Header{"Authorization": []string{"Bearer " + token}, "Device-Id": []string{device}, "Protocol-Version": []string{"1"}})
	if err != nil {
		if resp != nil {
			resp.Body.Close()
			return fmt.Errorf("gateway handshake HTTP %d", resp.StatusCode)
		}
		return errors.New("gateway connection failed")
	}
	defer c.Close()
	c.SetReadLimit(65536)
	c.SetReadDeadline(time.Now().Add(100 * time.Second))
	c.WriteJSON(protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}})
	var hello protocol.Hello
	if c.ReadJSON(&hello) != nil || hello.Type != "hello" {
		return errors.New("invalid server hello")
	}
	enc, err := audio.NewEncoderAtRate(16000)
	if err != nil {
		return err
	}
	defer enc.Close()
	dec, err := audio.NewDecoder()
	if err != nil {
		return err
	}
	defer dec.Close()
	started := time.Now()
	r := result{Mode: mode, AbortAfterFrames: abort}
	for round, pcm := range [][]byte{first, second} {
		if err := c.WriteJSON(protocol.Control{Type: "listen", State: "start", Mode: mode, SessionID: hello.SessionID}); err != nil {
			return err
		}
		// Input is paced like a device; silence closes the native VAD turn.
		if mode != "manual" {
			pcm = append(append([]byte(nil), pcm...), make([]byte, 32000*2)...)
		}
		for pos := 0; pos < len(pcm); pos += 1920 {
			frame := make([]byte, 1920)
			copy(frame, pcm[pos:])
			packet, err := enc.Encode(frame)
			if err != nil {
				return err
			}
			if c.WriteMessage(websocket.BinaryMessage, packet) != nil {
				return errors.New("input write failed")
			}
			time.Sleep(60 * time.Millisecond)
		}
		submitted := time.Now()
		if mode == "manual" {
			if c.WriteJSON(protocol.Control{Type: "listen", State: "stop", SessionID: hello.SessionID}) != nil {
				return errors.New("stop write failed")
			}
		}
		frames := 0
		playing := false
		var output []byte
		var aborted time.Time
		var firstAudio time.Duration
		transcript := ""
		for {
			kind, data, err := c.ReadMessage()
			if err != nil {
				return errors.New("gateway closed or timed out before response finished")
			}
			if kind == websocket.BinaryMessage {
				if !playing {
					return errors.New("audio arrived outside a TTS response")
				}
				p, _, err := protocol.Unwrap(1, data)
				if err != nil {
					return err
				}
				chunk, err := dec.Decode(p)
				if err != nil {
					return err
				}
				output = append(output, chunk...)
				frames++
				if frames == 1 {
					firstAudio = time.Since(submitted)
				}
				if round == 0 && abort > 0 && frames == abort {
					aborted = time.Now()
					if c.WriteJSON(protocol.Control{Type: "abort", SessionID: hello.SessionID}) != nil {
						return errors.New("abort write failed")
					}
				}
				continue
			}
			var ev protocol.Control
			if json.Unmarshal(data, &ev) != nil {
				return errors.New("bad control event")
			}
			if ev.Type == "alert" {
				return errors.New("gateway reported upstream failure")
			}
			if ev.Type == "stt" {
				r.InputTranscripts = append(r.InputTranscripts, ev.Text)
			}
			if ev.Type == "tts" && ev.State == "start" {
				playing = true
			}
			if ev.Type == "tts" && ev.State == "sentence_start" {
				transcript = ev.Text
			}
			if ev.Type == "tts" && ev.State == "stop" {
				if !aborted.IsZero() {
					r.AbortStopMS = time.Since(aborted).Milliseconds()
				}
				break
			}
		}
		if frames == 0 {
			return errors.New("response contained no audio")
		}
		if round == 0 && abort > 0 && aborted.IsZero() {
			return errors.New("first response ended before requested abort")
		}
		r.AudioFrames = append(r.AudioFrames, frames)
		r.OutputTranscripts = append(r.OutputTranscripts, transcript)
		r.FirstAudioMS = append(r.FirstAudioMS, firstAudio.Milliseconds())
		f, err := os.OpenFile(filepath.Join(out, fmt.Sprintf("reply-%d.wav", round+1)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
		if err != nil {
			return err
		}
		err = audio.WriteWAV(f, output)
		f.Close()
		if err != nil {
			return err
		}
		// A fresh decoder matches the firmware's TTS-start reset.
		dec.Close()
		dec, err = audio.NewDecoder()
		if err != nil {
			return err
		}
	}
	dec.Close()
	r.Status = "passed"
	r.TotalMS = time.Since(started).Milliseconds()
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "result.json"), b, 0640); err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
