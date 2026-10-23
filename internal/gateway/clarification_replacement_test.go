package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/protocol"
	"familyrobot/internal/qwen"
)

func verifyQuestion(t *testing.T, s *session) {
	t.Helper()
	for _, ev := range []qwen.Event{
		{Type: "response.audio_transcript.done", Transcript: fixedClarificationTest},
		{Type: "response.audio.done"},
		{Type: "response.done", Response: qwen.Response{ID: "clarify", Status: "completed"}},
	} {
		if err := clarificationEvent(t, s, ev); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClarificationGuardTracesArePrivateAndPrecedePlayback(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "verified", false: "rejected"}[valid], func(t *testing.T) {
			h, _, _ := clarificationHarness(t)
			s := h.s
			const private = "NEVER_LOG_CLARIFICATION_PRIVATE"
			s.cfg.DeviceToken, s.cfg.Qwen.APIKey = private, private
			s.paused.original.text = private
			var out lockedTraceBuffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			if err := clarificationPCM(t, s, make([]byte, audio.FrameBytes)); err != nil {
				t.Fatal(err)
			}
			if valid {
				verifyQuestion(t, s)
				expectPlayback(t, h.play(t, time.Now()), "start", "sentence_start", "audio")
			} else {
				if err := clarificationEvent(t, s, qwen.Event{Type: "response.audio_transcript.done", Transcript: private}); err == nil {
					t.Fatal("contaminated transcript accepted")
				}
				if got := h.play(t, time.Now()); len(got) != 0 {
					t.Fatal(got)
				}
			}
			s.traceVoice(private)
			text := out.snapshot()
			if strings.Contains(text, private) || strings.Contains(text, fixedClarificationTest) {
				t.Fatal("trace leaked private or generated content")
			}
			var events []string
			allowed := map[string]bool{"time": true, "level": true, "msg": true, "event": true, "session": true, "mode": true, "upstream_generation": true, "response_open": true, "speaking": true, "pending_ms": true, "input_bytes": true, "snapshot_revision": true, "snapshot_token": true, "task_bound": true}
			for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
				if line == "" {
					continue
				}
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				if record["msg"] != "voice_event" {
					continue
				}
				for key := range record {
					if !allowed[key] {
						t.Fatalf("unexpected trace payload %q", key)
					}
				}
				events = append(events, record["event"].(string))
			}
			got := strings.Join(events, ",")
			if valid && got != "clarification_verified,response_completed,tts_start" {
				t.Fatalf("verification must occur exactly once before TTS: %s", got)
			}
			if !valid && got != "clarification_rejected" {
				t.Fatalf("missing rejection or leaked TTS: %s", got)
			}
		})
	}
}

func TestClarificationReplacementClosesOnlyAdmittedPlayback(t *testing.T) {
	for _, entry := range []string{"commit", "direct-transcript", "corrected-transcript"} {
		for _, phase := range []string{"held", "verified", "playing"} {
			t.Run(entry+"/"+phase, func(t *testing.T) {
				h, p, _ := clarificationHarness(t)
				s := h.s
				s.playbackEnabled = true
				original, generation := s.paused.original, s.generation
				oldToken := s.sync.sequence
				if err := clarificationPCM(t, s, make([]byte, 3*audio.FrameBytes)); err != nil {
					t.Fatal(err)
				}
				if phase != "held" {
					verifyQuestion(t, s)
				}
				at := time.Now()
				var block *playbackRecord
				if phase == "playing" {
					expectPlayback(t, h.play(t, at), "start", "sentence_start", "audio")
					block = s.activePlayback
				}
				// A mode ACK already in the client FIFO must not authorize the
				// replacement snapshot, even though the old question was verified.
				if err := s.model.SetMode("auto"); err != nil {
					t.Fatal(err)
				}
				if update := p.read(t).body; update["type"] != "session.update" {
					t.Fatal(update)
				}
				requests, results := controlledClassifier(s)
				item := "replacement"
				if entry == "direct-transcript" {
					s.inputCommitted = false
				}
				if entry == "corrected-transcript" {
					item = "continue"
				}
				ev := qwen.Event{Type: "input_audio_buffer.committed", ItemID: item}
				if entry != "commit" {
					ev.Type, ev.Transcript = "conversation.item.input_audio_transcription.completed", "三十七"
				}
				if err := s.onModel(context.Background(), ev); err != nil {
					t.Fatal(err)
				}
				if phase == "playing" {
					var stop protocol.Control
					h.client.SetReadDeadline(time.Now().Add(time.Second))
					if err := h.client.ReadJSON(&stop); err != nil {
						t.Fatalf("missing immediate stop: %v", err)
					}
					if stop.State != "stop" || stop.Reason != "interrupted" || stop.PlaybackID != block.id || stop.SessionID != s.id {
						t.Fatalf("wrong stop: %+v", stop)
					}
					if !block.sealed || !block.interrupted || s.started || s.activePlayback != nil || s.devicePlayback != nil {
						t.Fatal("old admitted block left open")
					}
					if len(s.hist.Turns) == 0 || s.hist.Turns[0].Delivery != "interrupted" || s.hist.Turns[0].GeneratedText != fixedClarificationTest || strings.Contains(s.hist.context(), fixedClarificationTest) {
						t.Fatalf("unsafe interrupted history: %+v", s.hist.Turns)
					}
					// A late final must now settle the sealed old identity, without changing its interrupted status.
					receipt, _ := json.Marshal(map[string]any{"type": "playback", "state": "finished", "session_id": s.id, "playback_id": block.id, "sample_rate": 24000, "played_samples": 1440, "valid": true, "basis": "i2s_dma"})
					s.receivePlayback(receipt)
					if !block.settled || !block.interrupted {
						t.Fatal("old final did not settle interrupted block")
					}
				} else if len(s.playbacks) != 0 || s.started || s.activePlayback != nil {
					t.Fatal("held-only replacement invented playback")
				}
				if entry != "commit" {
					var stt protocol.Control
					h.client.SetReadDeadline(time.Now().Add(time.Second))
					if err := h.client.ReadJSON(&stt); err != nil || stt.Type != "stt" || stt.Text != "三十七" {
						t.Fatalf("new STT lost: %+v, %v", stt, err)
					}
				}
				if s.pending.Len() != 0 || s.clarification.active || s.currentInput.itemID != item || s.generation != generation || s.model == nil || s.paused.original != original || !s.paused.awaitingResumeChoice {
					t.Fatal("replacement lost input, task, choice, or retained old output")
				}
				// Native envelopes from the old response cannot revive its audio or fail the new route.
				for _, stale := range []qwen.Event{{Type: "response.created", Response: qwen.Response{ID: "clarify"}}, {Type: "response.audio.delta", ResponseID: "clarify", Delta: base64.StdEncoding.EncodeToString([]byte{0, 0})}, {Type: "response.done", Response: qwen.Response{ID: "clarify", Status: "completed"}}} {
					if err := s.onModel(context.Background(), stale); err != nil {
						t.Fatal(err)
					}
				}
				if entry == "commit" {
					s.transcribeOrigin(item, "三十七")
				}
				if err := s.advanceGeneration(time.Now()); err != nil {
					t.Fatal(err)
				}
				req := takeClassification(t, requests)
				if !req.awaiting || req.latest != "三十七" || req.original != original.text {
					t.Fatal("new classifier lost current input/choice")
				}
				results <- taskRouteResult{action: "resume_from"}
				if err := s.finishRouting(routeResult(t, s)); err != nil {
					t.Fatal(err)
				}
				// Drain-induced history/revision changes require their own current snapshot.
				if update := p.read(t).body; update["type"] != "session.update" {
					t.Fatal(update)
				}
				if s.sync.token <= oldToken {
					t.Fatal("snapshot sequence reused")
				}
				currentToken := s.sync.token
				ackSnapshotWire(t, s, p)
				p.quiet(t, time.Millisecond)
				if s.voiceRequested || s.sync.token != currentToken {
					t.Fatal("old FIFO ACK released replacement voice")
				}
				ackSnapshotWire(t, s, p)
				p.want(t, "response.create")
				if s.paused.awaitingResumeChoice || s.responseTaskID != s.paused.id {
					t.Fatal("explicit user choice lost")
				}
				if err := s.onModel(context.Background(), qwen.Event{Type: "response.created", Response: qwen.Response{ID: "next"}}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(s.encoder.Close)
				if err := s.onModel(context.Background(), qwen.Event{Type: "response.audio.delta", ResponseID: "next", Delta: base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))}); err != nil {
					t.Fatal(err)
				}
				if err := s.onModel(context.Background(), qwen.Event{Type: "response.done", Response: qwen.Response{ID: "next", Status: "completed"}}); err != nil {
					t.Fatal(err)
				}
				expectPlayback(t, h.play(t, at.Add(time.Second)), "start", "audio")
				expectPlayback(t, h.play(t, at.Add(time.Second+60*time.Millisecond)), "stop")
				if s.started || s.activePlayback != nil {
					t.Fatal("next response left open")
				}
			})
		}
	}
}

func TestClarificationDuplicateAndUnrelatedInputDoNotStopCurrentQuestion(t *testing.T) {
	h, p, _ := clarificationHarness(t)
	s := h.s
	clarificationPCM(t, s, make([]byte, 3*audio.FrameBytes))
	verifyQuestion(t, s)
	at := time.Now()
	h.play(t, at)
	block, input, sequence := s.activePlayback, s.currentInput, s.sync.sequence
	if err := s.onModel(context.Background(), qwen.Event{Type: "input_audio_buffer.committed", ItemID: input.itemID}); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []qwen.Event{
		{Type: "conversation.item.input_audio_transcription.completed", ItemID: "unrelated", Transcript: "旁支"},
		{Type: "conversation.item.input_audio_transcription.completed", ItemID: input.itemID, Transcript: input.text},
	} {
		if err := s.onModel(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		var stt protocol.Control
		h.client.SetReadDeadline(time.Now().Add(time.Second))
		if err := h.client.ReadJSON(&stt); err != nil || stt.Type != "stt" {
			t.Fatalf("duplicate/unrelated input stopped question: %+v %v", stt, err)
		}
	}
	if s.currentInput != input || s.activePlayback != block || block.interrupted || !s.clarification.verified || s.sync.sequence != sequence {
		t.Fatal("nonreplacement changed current question")
	}
	expectPlayback(t, h.play(t, at.Add(60*time.Millisecond)), "audio")
	p.quiet(t, time.Millisecond)
}

func TestClarificationReplacementPropagatesDeviceStopWriteFailure(t *testing.T) {
	for _, entry := range []string{"commit", "transcript"} {
		t.Run(entry, func(t *testing.T) {
			h, p, _ := clarificationHarness(t)
			s := h.s
			clarificationPCM(t, s, make([]byte, 2*audio.FrameBytes))
			verifyQuestion(t, s)
			h.play(t, time.Now())
			old := s.currentInput
			s.device.Close()
			ev := qwen.Event{Type: "input_audio_buffer.committed", ItemID: "replacement"}
			if entry == "transcript" {
				s.inputCommitted = false
				ev.Type, ev.Transcript = "conversation.item.input_audio_transcription.completed", "三十七"
			}
			if err := s.onModel(context.Background(), ev); err == nil || err.Error() != "device write failed" {
				t.Fatalf("stop failure swallowed: %v", err)
			}
			if s.currentInput != old {
				t.Fatal("failed stop accepted replacement")
			}
			p.quiet(t, time.Millisecond)
		})
	}
}
