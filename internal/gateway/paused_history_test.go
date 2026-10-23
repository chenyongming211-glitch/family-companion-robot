package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
)

func TestInterruptedDraftSurvivesInterveningAnswerAsUnplayedData(t *testing.T) {
	for _, complete := range []bool{false, true} {
		name := "partial-transcript"
		if complete {
			name = "final-transcript"
		}
		t.Run(name, func(t *testing.T) {
			h := newToolHarness(t, false)
			old, _ := h.model(t)
			old.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "请完整读完这篇文章"})
			h.device.want(t, "stt")
			toolParent(t, old, "reading", 500)
			if v := h.device.want(t, "tts"); v["state"] != "start" {
				t.Fatal(v)
			}
			const draft = "文章开头，然后是尚未播放的正文和结尾"
			old.send(t, map[string]any{"type": "response.audio_transcript.delta", "response_id": "stale-reply", "delta": "must not leak into the active draft"})
			kind, field := "response.audio_transcript.delta", "delta"
			if complete {
				kind, field = "response.audio_transcript.done", "transcript"
			}
			old.send(t, map[string]any{"type": kind, "response_id": "reading", field: draft})
			old.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
			for {
				v := h.device.read(t)
				if v.body["type"] == "tts" && v.body["state"] == "stop" {
					break
				}
			}
			fresh, setup := h.model(t)
			instructions := setup["instructions"].(string)
			if !strings.Contains(instructions, `"delivery":"interrupted"`) || strings.Contains(instructions, draft) {
				t.Fatal("reconnection must preserve interruption marker and omit unplayed draft")
			}
			if strings.Contains(instructions, `"text":"`+draft+`"`) {
				t.Fatal("generated but unplayed words became an ordinary completed assistant turn")
			}
			// A completed side question must not erase the earlier paused reply.
			fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "一加一等于几"})
			h.device.want(t, "stt")
			toolParent(t, fresh, "math", 1)
			fresh.send(t, map[string]any{"type": "response.audio_transcript.done", "response_id": "math", "transcript": "等于二"})
			toolDone(t, fresh, "math")
			for {
				v := h.device.read(t)
				if v.body["type"] == "tts" && v.body["state"] == "stop" {
					break
				}
			}
			// A second rebuild exposes the history after that side answer drained.
			h.device.send(t, map[string]any{"type": "abort"})
			h.device.want(t, "tts")
			_, setup = h.model(t)
			instructions = setup["instructions"].(string)
			if strings.Contains(instructions, draft) || !strings.Contains(instructions, `"delivery":"transport_sent","generated_text":"等于二"`) {
				t.Fatal("answering the side question discarded the unfinished original task")
			}
		})
	}
}

func TestInterruptedDraftsShareTheExistingHistoryBudget(t *testing.T) {
	var h history
	for i := 0; i < 20; i++ {
		h.add("user", strings.Repeat("问", 1000))
		h.addInterrupted(strings.Repeat("稿", 1000))
		h.addSent(strings.Repeat("答", 1000))
	}
	var decoded struct {
		Turns []map[string]any `json:"turns"`
	}
	if err := json.Unmarshal([]byte(h.context()), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Turns) != 12 {
		t.Fatal("interrupted drafts escaped the existing twelve-turn bound")
	}
	for _, v := range decoded.Turns {
		if draft, ok := v["generated_text"].(string); ok {
			if len([]rune(draft)) != 512 || (v["delivery"] != "interrupted" && v["delivery"] != "transport_sent") || v["text"] != nil {
				t.Fatal("unbounded draft or generated words presented as completed speech")
			}
		}
	}
}

func TestRetiredSharedToolPlaybackStillSanitizesInterruptedParentHistory(t *testing.T) {
	h := newPlaybackHarness(t)
	s := h.s
	s.playbackEnabled = true
	s.responseOrigin = requestOrigin{known: true, text: "读完原任务"}
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(100, 0))
	s.pausePlayback()
	s.started = false
	s.activePlayback = nil
	s.devicePlayback = nil
	s.responseID = "tool-parent"
	s.responseOrigin = requestOrigin{known: true, text: "继续"}
	out, _ := s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":"resume","expected_revision":%d}`, s.paused.id, s.snapshotRevision))
	if !strings.Contains(out, `"ok":true`) {
		t.Fatal(out)
	}
	const draft = "unplayed-shared-tool-parent-secret"
	s.transcript = draft
	s.responseDone = true
	s.toolCalls = map[string]*toolCallState{"discard": {}}
	h.append(t, make([]byte, audio.FrameBytes))
	h.play(t, time.Unix(101, 0))
	h.play(t, time.Unix(101, 0).Add(60*time.Millisecond))
	r := s.activePlayback
	if !s.started || !s.toolPlayback || r == nil || len(s.hist.Turns) != 1 || s.hist.Turns[0].Delivery != "transport_sent" || s.hist.Turns[0].playbackID != r.id {
		t.Fatal("fixture did not store a completed tool parent in an open shared TTS block")
	}
	out, _ = s.conversationTool("conversation_update_paused_task", fmt.Sprintf(`{"task_id":%q,"action":"discard","expected_revision":%d}`, s.paused.id, s.snapshotRevision))
	if !strings.Contains(out, `"ok":true`) || !r.retired {
		t.Fatal("shared block was not retired by explicit discard")
	}
	// The continuation shares the retired block. Interruption still applies to
	// its stored parent draft even though that block may no longer own a task.
	s.pausePlayback()
	if s.paused != nil {
		t.Error("retired shared playback resurrected the discarded task")
	}
	if !r.interrupted || !r.sealed || r.settleUntil.IsZero() {
		t.Error("retired block skipped interruption and settlement bookkeeping")
	}
	if s.hist.Turns[0].Delivery != "interrupted" || strings.Contains(s.hist.context(), draft) {
		t.Error("retired shared block leaked its unplayed tool parent into rebuilt context")
	}
	if s.hist.Turns[0].GeneratedText != draft {
		t.Error("internal diagnostic draft was lost")
	}
}
