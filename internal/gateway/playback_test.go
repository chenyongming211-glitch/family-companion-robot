package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/protocol"
	"familyrobot/internal/qwen"
	"github.com/gorilla/websocket"
)

// Ticker stamps and processing/write time can advance independently, but every
// observation crosses a real WebSocket.
// A barrier after each call makes absence assertions independent of wall time.
type playbackHarness struct {
	s       *session
	client  *websocket.Conn
	decoder *audio.Decoder
	now     time.Time
}

func newPlaybackHarness(t *testing.T) *playbackHarness {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			accepted <- c
		}
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	device := <-accepted
	t.Cleanup(func() { device.Close() })
	encoder, err := audio.NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(encoder.Close)
	decoder, err := audio.NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(decoder.Close)
	s := newSession(context.Background(), device, Config{}, 1)
	s.encoder = encoder
	s.responseID = "test-response"
	h := &playbackHarness{s: s, client: client, decoder: decoder}
	s.playbackNow = func() time.Time { return h.now }
	return h
}

func (h *playbackHarness) append(t *testing.T, pcm []byte) {
	t.Helper()
	if err := h.s.pending.Append(pcm); err != nil {
		t.Fatal(err)
	}
}

func (h *playbackHarness) play(t *testing.T, now time.Time) []string {
	t.Helper()
	return h.delayedTick(t, now, now)
}

func (h *playbackHarness) delayedTick(t *testing.T, tick, processed time.Time) []string {
	t.Helper()
	if tick.After(processed) {
		t.Fatal("tick cannot be processed before it is stamped")
	}
	h.now = processed
	// Like run, discard the ticker stamp before invoking playback. The
	// scheduler must obtain actual processing time from its own clock.
	if err := h.s.play(); err != nil {
		t.Fatal(err)
	}
	const barrier = "playback-test-barrier"
	if err := h.s.write(websocket.TextMessage, []byte(barrier)); err != nil {
		t.Fatal(err)
	}
	if err := h.client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var observed []string
	for {
		kind, data, err := h.client.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.TextMessage && string(data) == barrier {
			return observed
		}
		if kind == websocket.BinaryMessage {
			packet, _, err := protocol.Unwrap(h.s.version, data)
			if err != nil {
				t.Fatal(err)
			}
			pcm, err := h.decoder.Decode(packet)
			if err != nil {
				t.Fatal(err)
			}
			if len(pcm) != 16000*60/1000*2 {
				t.Fatalf("decoded frame has %d bytes, expected 60ms at 16kHz", len(pcm))
			}
			observed = append(observed, "audio")
			continue
		}
		var control protocol.Control
		if err := json.Unmarshal(data, &control); err != nil {
			t.Fatal(err)
		}
		if control.Type != "tts" {
			t.Fatalf("unexpected control: %s", data)
		}
		observed = append(observed, control.State)
	}
}

func (h *playbackHarness) delayNextAudioWrite(delay time.Duration) {
	write := h.s.writeAudio
	h.s.writeAudio = func(data []byte) error {
		h.s.writeAudio = write
		if err := write(data); err != nil {
			return err
		}
		// Model elapsed encoding/socket-write time before successful return.
		h.now = h.now.Add(delay)
		return nil
	}
}

func expectPlayback(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WebSocket messages = %v, want %v", got, want)
	}
}

func TestPlaybackTickJitterDoesNotAccumulate(t *testing.T) {
	h := newPlaybackHarness(t)
	const frames = 20
	h.append(t, make([]byte, audio.FrameBytes*frames))
	h.s.responseDone = true
	start := time.Unix(100, 0)
	var sent []time.Duration
	var stopped time.Duration
	for tick := 0; tick <= 150; tick++ {
		// Only 0–6us of tick jitter: resetting to now+60ms turns this
		// into almost a whole extra 10ms tick for each subsequent frame.
		elapsed := time.Duration(tick)*10*time.Millisecond + time.Duration(tick%7)*time.Microsecond
		for _, event := range h.play(t, start.Add(elapsed)) {
			switch event {
			case "audio":
				sent = append(sent, elapsed)
			case "stop":
				stopped = elapsed
			}
		}
	}
	if len(sent) != frames {
		t.Fatalf("received %d audio frames, want %d", len(sent), frames)
	}
	idealLast := time.Duration(frames-1) * 60 * time.Millisecond
	if elapsed := sent[len(sent)-1]; elapsed < idealLast || elapsed > idealLast+10*time.Millisecond {
		t.Fatalf("last of %d frames written at %v, want [%v, %v]; tick jitter accumulated", frames, elapsed, idealLast, idealLast+10*time.Millisecond)
	}
	for i, elapsed := range sent {
		ideal := time.Duration(i) * 60 * time.Millisecond
		if elapsed < ideal || elapsed > ideal+10*time.Millisecond {
			t.Fatalf("frame %d written at %v, want [%v, %v]; tick jitter accumulated", i, elapsed, ideal, ideal+10*time.Millisecond)
		}
	}
	earliestStop := sent[len(sent)-1] + 60*time.Millisecond
	if stopped < earliestStop || stopped > earliestStop+10*time.Millisecond {
		t.Fatalf("stop written at %v, want [%v, %v]", stopped, earliestStop, earliestStop+10*time.Millisecond)
	}
}

func TestPlaybackLateFinalFrameKeepsFullDuration(t *testing.T) {
	for _, finalBytes := range []int{audio.FrameBytes, audio.FrameBytes / 2} {
		name := "full"
		if finalBytes < audio.FrameBytes {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			h := newPlaybackHarness(t)
			h.append(t, make([]byte, audio.FrameBytes+finalBytes))
			h.s.responseDone = true
			h.s.transcript = "完整回复"
			start := time.Unix(100, 0)
			expectPlayback(t, h.play(t, start), "start", "sentence_start", "audio")
			expectPlayback(t, h.play(t, start.Add(65*time.Millisecond)), "audio")
			// The next absolute deadline is 120ms, but the final packet
			// arrived at 65ms and needs its full duration before stop.
			expectPlayback(t, h.play(t, start.Add(120*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(125*time.Millisecond-time.Nanosecond)))
			if h.s.responseID == "" || len(h.s.hist.Turns) != 0 {
				t.Fatal("response finalized before the final packet duration elapsed")
			}
			expectPlayback(t, h.play(t, start.Add(125*time.Millisecond)), "stop")
			expectPlayback(t, h.play(t, start.Add(130*time.Millisecond)))
		})
	}
}

func TestPlaybackStaleTickKeepsFinalFrameFullDuration(t *testing.T) {
	for _, finalBytes := range []int{audio.FrameBytes, audio.FrameBytes / 2} {
		t.Run(fmt.Sprint(finalBytes), func(t *testing.T) {
			h := newPlaybackHarness(t)
			h.append(t, make([]byte, audio.FrameBytes+finalBytes))
			h.s.responseDone = true
			start := time.Unix(100, 0)
			expectPlayback(t, h.play(t, start), "start", "audio")
			// MCP work held a tick stamped 60ms until 160ms. The final
			// frame must play until 220ms, even when the next tick is fresh.
			expectPlayback(t, h.delayedTick(t, start.Add(60*time.Millisecond), start.Add(160*time.Millisecond)), "audio")
			expectPlayback(t, h.play(t, start.Add(170*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(220*time.Millisecond-time.Nanosecond)))
			expectPlayback(t, h.play(t, start.Add(220*time.Millisecond)), "stop")
		})
	}
}

func TestPlaybackStaleTickReanchorsToProcessingTime(t *testing.T) {
	h := newPlaybackHarness(t)
	h.append(t, make([]byte, audio.FrameBytes*3))
	h.s.responseDone = true
	start := time.Unix(100, 0)
	expectPlayback(t, h.play(t, start), "start", "audio")
	expectPlayback(t, h.delayedTick(t, start.Add(60*time.Millisecond), start.Add(160*time.Millisecond)), "audio")
	expectPlayback(t, h.play(t, start.Add(170*time.Millisecond)))
	expectPlayback(t, h.play(t, start.Add(220*time.Millisecond-time.Nanosecond)))
	expectPlayback(t, h.play(t, start.Add(220*time.Millisecond)), "audio")
	expectPlayback(t, h.play(t, start.Add(280*time.Millisecond)), "stop")
}

func TestPlaybackSlowWriteKeepsFinalFrameFullDuration(t *testing.T) {
	for _, finalBytes := range []int{audio.FrameBytes, audio.FrameBytes / 2} {
		t.Run(fmt.Sprint(finalBytes), func(t *testing.T) {
			h := newPlaybackHarness(t)
			h.append(t, make([]byte, audio.FrameBytes+finalBytes))
			h.s.responseDone = true
			start := time.Unix(100, 0)
			expectPlayback(t, h.play(t, start), "start", "audio")
			h.delayNextAudioWrite(100 * time.Millisecond)
			expectPlayback(t, h.play(t, start.Add(60*time.Millisecond)), "audio")
			expectPlayback(t, h.play(t, start.Add(170*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(220*time.Millisecond-time.Nanosecond)))
			expectPlayback(t, h.play(t, start.Add(220*time.Millisecond)), "stop")
		})
	}
}

func TestPlaybackSlowWriteReanchorsToCompletionTime(t *testing.T) {
	for _, delayedFrame := range []int{0, 1} {
		t.Run(fmt.Sprint(delayedFrame), func(t *testing.T) {
			h := newPlaybackHarness(t)
			h.append(t, make([]byte, audio.FrameBytes*3))
			h.s.responseDone = true
			start := time.Unix(100, 0)
			if delayedFrame == 0 {
				h.delayNextAudioWrite(100 * time.Millisecond)
				expectPlayback(t, h.play(t, start), "start", "audio")
			} else {
				expectPlayback(t, h.play(t, start), "start", "audio")
				h.delayNextAudioWrite(100 * time.Millisecond)
				expectPlayback(t, h.play(t, start.Add(60*time.Millisecond)), "audio")
			}
			completed := time.Duration(delayedFrame)*60*time.Millisecond + 100*time.Millisecond
			expectPlayback(t, h.play(t, start.Add(completed+10*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(completed+60*time.Millisecond-time.Nanosecond)))
			expectPlayback(t, h.play(t, start.Add(completed+60*time.Millisecond)), "audio")
			if delayedFrame == 0 {
				expectPlayback(t, h.play(t, start.Add(completed+120*time.Millisecond)), "audio")
				expectPlayback(t, h.play(t, start.Add(completed+180*time.Millisecond)), "stop")
			} else {
				expectPlayback(t, h.play(t, start.Add(completed+120*time.Millisecond)), "stop")
			}
		})
	}
}

func TestPlaybackNewResponseResetsFinalFrameDeadline(t *testing.T) {
	h := newPlaybackHarness(t)
	h.append(t, make([]byte, audio.FrameBytes))
	start := time.Unix(100, 0)
	expectPlayback(t, h.play(t, start), "start", "audio")
	// Model creation is responsible for resetting playback state between
	// replies. A fresh reply must not inherit any prior completion deadline.
	h.s.responseID = ""
	if err := h.s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "next"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.encoder.Close)
	h.append(t, make([]byte, audio.FrameBytes))
	h.s.responseDone = true
	expectPlayback(t, h.play(t, start.Add(10*time.Millisecond)), "start", "audio")
	expectPlayback(t, h.play(t, start.Add(69*time.Millisecond)))
	expectPlayback(t, h.play(t, start.Add(70*time.Millisecond)), "stop")
}

func TestPlaybackReanchorsAfterStall(t *testing.T) {
	for _, pause := range []time.Duration{120 * time.Millisecond, time.Second} {
		t.Run(pause.String(), func(t *testing.T) {
			h := newPlaybackHarness(t)
			h.append(t, make([]byte, audio.FrameBytes*3))
			h.s.responseDone = true
			start := time.Unix(100, 0)
			expectPlayback(t, h.play(t, start), "start", "audio")
			expectPlayback(t, h.play(t, start.Add(pause)), "audio")
			expectPlayback(t, h.play(t, start.Add(pause+10*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(pause+59*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(pause+60*time.Millisecond)), "audio")
			expectPlayback(t, h.play(t, start.Add(pause+60*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(pause+120*time.Millisecond)), "stop")
		})
	}
}

func TestPlaybackReanchorsAfterBufferUnderrun(t *testing.T) {
	for _, buffered := range []int{0, audio.FrameBytes / 2} {
		name := "empty"
		if buffered != 0 {
			name = "incomplete-frame"
		}
		t.Run(name, func(t *testing.T) {
			h := newPlaybackHarness(t)
			h.append(t, make([]byte, audio.FrameBytes+buffered))
			start := time.Unix(100, 0)
			expectPlayback(t, h.play(t, start), "start", "audio")
			expectPlayback(t, h.play(t, start.Add(65*time.Millisecond)))
			h.append(t, make([]byte, audio.FrameBytes*2-buffered))
			h.s.responseDone = true
			expectPlayback(t, h.play(t, start.Add(70*time.Millisecond)), "audio")
			expectPlayback(t, h.play(t, start.Add(120*time.Millisecond)))
			expectPlayback(t, h.play(t, start.Add(130*time.Millisecond)), "audio")
			expectPlayback(t, h.play(t, start.Add(190*time.Millisecond)), "stop")
		})
	}
}

func TestPlaybackPartialFinalFrameAndStopOrdering(t *testing.T) {
	h := newPlaybackHarness(t)
	h.append(t, make([]byte, audio.FrameBytes+audio.FrameBytes/2))
	h.s.transcript = "完整回复"
	start := time.Unix(100, 0)
	expectPlayback(t, h.play(t, start), "start", "sentence_start", "audio")
	// An incomplete streaming frame waits for completion, then is padded once.
	expectPlayback(t, h.play(t, start.Add(60*time.Millisecond)))
	h.s.responseDone = true
	expectPlayback(t, h.play(t, start.Add(61*time.Millisecond)), "audio")
	if h.s.pending.Len() != 0 {
		t.Fatal("partial final PCM was not drained")
	}
	expectPlayback(t, h.play(t, start.Add(120*time.Millisecond)))
	if len(h.s.hist.Turns) != 0 || h.s.responseID == "" {
		t.Fatal("response finalized before its final 60ms frame elapsed")
	}
	expectPlayback(t, h.play(t, start.Add(121*time.Millisecond)), "stop")
	if !reflect.DeepEqual(h.s.hist.Turns, []turn{{Role: "assistant", Text: "完整回复"}}) {
		t.Fatalf("completed response history = %v", h.s.hist.Turns)
	}
	if h.s.responseID != "" || h.s.started || h.s.responseDone || h.s.transcript != "" {
		t.Fatal("response state was not cleared after stop")
	}
	expectPlayback(t, h.play(t, start.Add(time.Second)))
}
