package realtimelive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
)

func emitValidatedPart(emit func(string, []byte) error, reference string, pcm []byte) error {
	if err := emit("spoken_reference", []byte(reference)); err != nil {
		return err
	}
	return emit("audio", pcm)
}

func requireSpeechState(t *testing.T, client *websocket.Conn, state string) map[string]any {
	t.Helper()
	_, _, event := readMediaMessage(t, client)
	if event["type"] != "tts" || event["state"] != state {
		t.Fatalf("expected one device TTS %s, got %v", state, event)
	}
	return event
}

func requireSpeechPacket(t *testing.T, client *websocket.Conn) []byte {
	t.Helper()
	kind, data, event := readMediaMessage(t, client)
	if kind != websocket.BinaryMessage {
		t.Fatalf("expected another audio frame in the same playback, got %v", event)
	}
	return data
}

func TestValidatedSpeechPartsShareOnePlayback(t *testing.T) {
	const first = "小兔开门了。"
	const spoken = first + "它看见了雪。"
	for _, tc := range []struct {
		name, route, draft string
		pcm                bool
	}{{"ordinary-opus", "A", spoken, false}, {"filtered-pcm", "B", "（表情：开心）\n" + spoken, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s, client := expressionSession(t)
			s.voiceRoute = tc.route
			s.wire.(*xiaozhiWire).pcm = tc.pcm
			releaseSecond := make(chan struct{})
			raw := pcmSamples(3000)
			s.generate = func(ctx context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
				if err := emit("text", []byte(tc.draft)); err != nil {
					return err
				}
				if err := emitValidatedPart(emit, first, raw[:3000]); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-releaseSecond:
					return emitValidatedPart(emit, spoken, raw[3000:])
				}
			}
			rt := s.addTurn()
			done := startMediaReply(t, s, rt)
			requireSpeechState(t, client, "start")
			audio := requireSpeechPacket(t, client)
			mediaReceipt(t, s, rt, 1440, "progress")
			s.mu.Lock()
			waiting := !rt.turn.Ended && rt.turn.Synthesized == first && realtimestate.PlaybackEvidence(rt.turn).SpokenText == nil
			s.mu.Unlock()
			if !waiting {
				t.Fatal("first verified part was not available independently, or became spoken without matching evidence")
			}

			close(releaseSecond)
			// Two 1500-sample parts are one timeline of three 1440-sample
			// frames, including the single final Opus lookahead or PCM padding.
			audio = append(audio, requireSpeechPacket(t, client)...)
			audio = append(audio, requireSpeechPacket(t, client)...)
			stop := requireSpeechState(t, client, "stop")
			if stop["reason"] != nil || stop["sent_samples"] != float64(4320) {
				t.Fatal("unexpected playback boundary", stop)
			}
			if tc.pcm && (!bytes.Equal(audio[:len(raw)], raw) || !bytes.Equal(audio[len(raw):], make([]byte, 4320*2-len(raw)))) {
				t.Fatal("padding was inserted between parts")
			}
			awaitMediaReply(t, done)
			if evidence := realtimestate.PlaybackEvidence(rt.turn); evidence.ExactStopKnown {
				t.Fatal("normal generation end substituted for a hardware completion receipt")
			}
			mediaReceipt(t, s, rt, 4320, "finished")
			evidence := realtimestate.PlaybackEvidence(rt.turn)
			if evidence.SpokenText == nil || *evidence.SpokenText != spoken || !evidence.ExactStopKnown || rt.turn.Generated != tc.draft {
				t.Fatalf("finished device playback lost the last verified part: %+v", evidence)
			}
			record := generatedRecord(rt.turn)
			if tc.draft == spoken && record["synthesized_text"] != nil || tc.draft != spoken && record["synthesized_text"] != spoken {
				t.Fatalf("archive lost the final cumulative reference: %v", record)
			}
		})
	}
}

func TestVerifiedFirstPartSurvivesLaterFailureOrInterruption(t *testing.T) {
	const first = "小兔开门了。"
	const draft = "（表情：开心）\n" + first + "它看见了雪。"
	for _, reason := range []string{"generation_error", "provider_asr_final"} {
		t.Run(reason, func(t *testing.T) {
			s, client := expressionSession(t)
			s.voiceRoute = "B"
			fail := make(chan struct{})
			failedAtSource := make(chan struct{})
			s.generate = func(ctx context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
				if err := emit("text", []byte(draft)); err != nil {
					return err
				}
				if err := emitValidatedPart(emit, first, pcmSamples(6*1440+12)); err != nil {
					return err
				}
				select {
				case <-fail:
					close(failedAtSource)
					return errors.New("later_tts_failed")
				case <-ctx.Done():
					// A provider callback already in flight must not submit more
					// metadata or PCM after a real interruption.
					return emitValidatedPart(emit, first+"迟到。", pcmSamples(1440))
				}
			}
			rt := s.addTurn()
			done := startMediaReply(t, s, rt)
			requireSpeechState(t, client, "start")
			for n := int64(1440); n <= 6*1440; n += 1440 {
				requireSpeechPacket(t, client)
				if n == 1440 && reason == "generation_error" {
					// The sixth frame of the valid first part cannot have
					// reached the wire before any credit is returned.
					close(fail)
					select {
					case <-failedAtSource:
					case <-time.After(2 * time.Second):
						t.Fatal("next part did not fail while the first was queued")
					}
				}
				mediaReceipt(t, s, rt, n, "progress")
			}
			s.mu.Lock()
			rt.turn.Tracker.Add(map[string]any{"sentence_id": 1, "sentence_end": true, "words": []any{
				map[string]any{"text": "小兔开门了", "begin_time": 0, "end_time": 120},
			}}, 200)
			s.mu.Unlock()
			if reason != "generation_error" {
				input := &pendingInput{id: rt.turn.ID + 1, outputAtStart: rt}
				s.mu.Lock()
				s.lastInput = input
				s.mu.Unlock()
				if err := s.interruptInput(nil, reason, input); err != nil {
					t.Fatal(err)
				}
			}
			stop := requireSpeechState(t, client, "stop")
			if stop["reason"] != "drain" || stop["sent_samples"] != float64(6*1440) {
				t.Fatal("failed/interrupted synthesis added a tail or sent a normal end", stop)
			}
			awaitMediaReply(t, done)
			mediaReceipt(t, s, rt, 6*1440, "finished")
			evidence := realtimestate.PlaybackEvidence(rt.turn)
			if rt.turn.Ended || !rt.turn.Cut || evidence.ExactStopKnown || evidence.SpokenText == nil || *evidence.SpokenText != first || rt.turn.Synthesized != first || rt.turn.Generated != draft {
				t.Fatalf("wrong interrupted evidence %+v; reference %q", evidence, rt.turn.Synthesized)
			}
			if ok, err := s.binary(rt, pcmSamples(1440)); ok || err != nil {
				t.Fatalf("audio escaped the sealed turn: %v, %v", ok, err)
			}
			f, err := os.ReadFile(filepath.Join(s.directory, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			interrupted := false
			for _, line := range strings.Split(strings.TrimSpace(string(f)), "\n") {
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatal(err)
				}
				switch row["event"] {
				case "generated":
					t.Fatal("incomplete synthesis archived as complete")
				case "generated_interrupted":
					interrupted = row["text"] == draft && row["synthesized_text"] == first
				}
			}
			if !interrupted {
				t.Fatal("missing interrupted archive with original draft and verified opening")
			}
		})
	}
}

func TestSpeechInterruptionReleasesBackpressuredEncoder(t *testing.T) {
	s, client := expressionSession(t)
	s.voiceRoute = "A"
	s.wire.(*xiaozhiWire).pcm = false
	queueFull := make(chan struct{})
	encoderResult := make(chan error, 1)
	s.generate = func(ctx context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		if err := emit("text", []byte("第一段。第二段。")); err != nil {
			return err
		}
		if err := emitValidatedPart(emit, "第一段。", pcmSamples(1440)); err != nil {
			return err
		}
		if err := emit("spoken_reference", []byte("第一段。第二段。")); err != nil {
			return err
		}
		frame := pcmSamples(1440)
		for sent := 1; ; sent++ {
			// One frame is held by the consumer waiting for device credit.
			if sent == int(realtimestate.WindowSamples/1440)+1+speechQueueFrames {
				close(queueFull)
			}
			if err := emit("audio", frame); err != nil {
				encoderResult <- err
				return err
			}
		}
	}
	rt := s.addTurn()
	done := startMediaReply(t, s, rt)
	requireSpeechState(t, client, "start")
	// Fill precisely the negotiated device window, withholding all receipts.
	for n := int64(0); n < realtimestate.WindowSamples; n += 1440 {
		requireSpeechPacket(t, client)
	}
	select {
	case <-queueFull:
	case <-time.After(2 * time.Second):
		t.Fatal("encoder did not reach backpressure")
	}
	if err := s.interruptFrom(rt, "provider_asr_final"); err != nil {
		t.Fatal(err)
	}
	stop := requireSpeechState(t, client, "stop")
	if stop["reason"] != "drain" || stop["sent_samples"] != float64(realtimestate.WindowSamples) {
		t.Fatal("audio sent beyond the stopped device window", stop)
	}
	awaitMediaReply(t, done)
	if err := <-encoderResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("backpressured encoder escaped cancellation: %v", err)
	}
	mediaReceipt(t, s, rt, realtimestate.WindowSamples, "finished")
	if got := realtimestate.PlaybackEvidence(rt.turn); got.SpokenText != nil || got.ExactStopKnown {
		t.Fatalf("buffered reference became spoken without matching evidence: %+v", got)
	}
}
