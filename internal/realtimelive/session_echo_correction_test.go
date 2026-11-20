package realtimelive

import (
	"bytes"
	"encoding/binary"
	"familyrobot/internal/realtimestate"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Both field interruptions started with a matching one-character preview. Later
// recognition changed after VAD stop and the playhead crossed the opening window.
func TestEndedEchoPreviewWaitsForFinalBeforeCut(t *testing.T) {
	for _, tc := range []struct {
		name, weak, preview, final string
		played, atDecision, endMs  int64
	}{
		{"later_preview", "我在上", "我在上班", "我在上班。", 15840, 40320, 840},
		{"later_final", "我就", "", "我就是。", 17280, 37440, 780},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "我叫小满。", 38)
			if err := r.s.receiveReceipt(echoReceipt(old, tc.played)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "overlap")
			r.preview(t, "overlap", "stash", "我")
			r.preview(t, "overlap", "stash", tc.weak)
			r.event(t, "input_audio_buffer.speech_stopped", "overlap", map[string]any{"audio_end_ms": tc.endMs})
			r.sync(t)
			if err := r.s.receiveReceipt(echoReceipt(old, tc.atDecision)); err != nil {
				t.Fatal(err)
			}
			if tc.preview != "" {
				r.preview(t, "overlap", "stash", tc.preview)
				assertEchoOutputAlive(t, r, old)
			}
			r.final(t, "overlap", tc.final)
			r.sync(t)
			// No matching playback PCM in this fixture. Even identical ASR text must
			// remain interruptible when it actually came from another speaker.
			r.s.mu.Lock()
			defer r.s.mu.Unlock()
			if !old.turn.Cut || len(r.s.history) != 2 || r.s.history[1].turn.User != tc.final {
				t.Fatal("real or unverified speech suppressed")
			}
		})
	}
}

// The same ASR transcript is replayed with echo, independent speech and
// double-talk. Only the microphone signal changes across the three cases.
func TestCorrectedInputUsesPlaybackWaveform(t *testing.T) {
	for _, tc := range []struct {
		label, weak, late, final string
		from, to, ms             int64
	}{
		{"preview", "我在上", "我在上班", "我在上班。", 15840, 40320, 840},
		{"final", "我就", "", "我就是。", 17280, 37440, 780},
	} {
		for _, mode := range []string{"echo", "independent", "double-talk"} {
			t.Run(tc.label+"/"+mode, func(t *testing.T) {
				r := newAdmissionRig(t, func(s *Session) {
					s.rec = recorder{dir: s.directory, owner: Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi"}}
				})
				audio := correctionSignal(32000, 41)
				mic := append([]byte(nil), audio[2*7680:2*(7680+16*tc.ms)]...)
				other := correctionSignal(len(mic)/2, 97)
				for i := 0; i < len(mic); i += 2 {
					v := int16(binary.LittleEndian.Uint16(mic[i:])) / 2
					near := int16(binary.LittleEndian.Uint16(other[i:]))
					switch mode {
					case "independent":
						v = near
					case "double-talk":
						v += near
					}
					binary.LittleEndian.PutUint16(mic[i:], uint16(v))
				}
				old := echoOutput(t, r, "我叫小满。", 0)
				// Feed consecutive 60 ms converted frames. Playback receipts below bound
				// which of them the decision may consult.
				for end := 1920; end <= len(audio); end += 1920 {
					if ok, err := r.s.binary(old, make([]byte, 2880)); err != nil || !ok {
						t.Fatal("output setup failed", err)
					}
					r.s.mu.Lock()
					r.s.rememberEchoPCMLocked(old, audio[end-1920:end])
					r.s.mu.Unlock()
				}
				r.s.mu.Lock()
				r.s.ring = realtimestate.NewAudioRing()
				err := r.s.ring.Append(mic)
				r.s.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				if err = r.s.receiveReceipt(echoReceipt(old, tc.from)); err != nil {
					t.Fatal(err)
				}
				r.start(t, "overlap")
				r.preview(t, "overlap", "stash", "我")
				r.preview(t, "overlap", "stash", tc.weak)
				r.event(t, "input_audio_buffer.speech_stopped", "overlap", map[string]any{"audio_end_ms": tc.ms})
				r.sync(t)
				if err = r.s.receiveReceipt(echoReceipt(old, tc.to)); err != nil {
					t.Fatal(err)
				}
				if tc.late != "" {
					r.preview(t, "overlap", "stash", tc.late)
					assertEchoOutputAlive(t, r, old)
				}
				r.final(t, "overlap", tc.final)
				r.sync(t)
				r.s.mu.Lock()
				kept := !old.turn.Cut && len(r.s.history) == 1
				admitted := old.turn.Cut && len(r.s.history) == 2 && r.s.history[1].turn.User == tc.final
				r.s.mu.Unlock()
				if mode == "echo" {
					if !kept {
						t.Fatal("echo admitted despite matching playback")
					}
					data, e := os.ReadFile(filepath.Join(r.s.directory, "events.jsonl"))
					if e != nil {
						t.Fatal(e)
					}
					if !bytes.Contains(data, []byte("confirmed_playback_echo")) {
						t.Fatal("missing waveform decision")
					}
				} else if !admitted {
					t.Fatal("independent input suppressed")
				}
			})
		}
	}
}

func correctionSignal(samples int, seed int64) []byte {
	rng := rand.New(rand.NewSource(seed))
	pcm := make([]byte, 2*samples)
	var smooth float64
	for i := 0; i < samples; i++ {
		smooth = .83*smooth + .17*(rng.Float64()-.5)
		level := .4 + .6*math.Abs(math.Sin(2*math.Pi*3.7*float64(i)/16000))
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(26000*level*smooth)))
	}
	return pcm
}
