package realtimelive

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSpeakingWeakPreviewAndShortFinalKeepPlaying(t *testing.T) {
	for _, reference := range []string{"小花猫在阁楼发现麻雀。", ""} {
		r := newEchoAdmissionRig(t)
		old := echoOutput(t, r, reference, 19)
		if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
			t.Fatal(err)
		}
		r.start(t, "overlap")
		r.preview(t, "overlap", "stash", "想")
		r.preview(t, "overlap", "stash", "想")
		assertEchoOutputAlive(t, r, old)
		r.event(t, "input_audio_buffer.speech_stopped", "overlap", map[string]any{"audio_end_ms": 760})
		r.final(t, "overlap", "想了吗？")
		assertEchoOutputAlive(t, r, old)
		r.s.mu.Lock()
		if r.s.lastInput != nil {
			t.Error("rejected overlap retained input")
		}
		r.s.mu.Unlock()
	}
}

func TestSpeakingNearEndInterruptions(t *testing.T) {
	for _, tc := range []struct{ a, b string }{{"停", ""}, {"等", "等等"}, {"你", "你好"}, {"什", "什么"}, {"为", "为什么"}, {"今", "今天会下雨吗"}} {
		t.Run(tc.a+tc.b, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "小花猫在阁楼发现麻雀。", 19)
			if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "human")
			r.preview(t, "human", "text", tc.a)
			if tc.b != "" {
				assertEchoOutputAlive(t, r, old)
				r.preview(t, "human", "text", tc.b)
			}
			assertEchoPreviewCuts(t, r, old)
		})
	}
}

func TestSpeakingOwnCommandAndMiddleOfOpening(t *testing.T) {
	for _, tc := range []struct{ ref, text string }{{"停，听我讲故事。", "停"}, {"小花猫在阁楼发现麻雀。", "花猫"}} {
		r := newEchoAdmissionRig(t)
		old := echoOutput(t, r, tc.ref, 19)
		if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
			t.Fatal(err)
		}
		r.start(t, "self")
		r.preview(t, "self", "text", tc.text)
		assertEchoOutputAlive(t, r, old)
		r.stop(t, "self")
		r.final(t, "self", tc.text)
		assertEchoOutputAlive(t, r, old)
	}
}

func TestSpeakingWeakRecognitionAcrossProviderPackets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		previews [][2]string
	}{
		{"final_only", nil},
		{"split_weak", [][2]string{{"想", "了"}}},
		{"split_repeated", [][2]string{{"想", "想"}, {"想", "想"}}},
		{"one_short_partial", [][2]string{{"想了", ""}}},
		{"particles_extend", [][2]string{{"", "想"}, {"想了", ""}, {"想了吗", ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "小花猫在阁楼发现麻雀。", 19)
			if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "weak")
			for _, preview := range tc.previews {
				r.event(t, "conversation.item.input_audio_transcription.delta", "weak", map[string]any{"text": preview[0], "stash": preview[1]})
				assertEchoOutputAlive(t, r, old)
			}
			r.stop(t, "weak")
			r.final(t, "weak", "想了吗？")
			assertEchoOutputAlive(t, r, old)
		})
	}
}

func TestSpeakingShortQuestionsSurviveFinalPacketization(t *testing.T) {
	for _, word := range []string{"停", "你好", "什么", "为什么"} {
		for _, preview := range []string{"", "stash"} {
			t.Run(word+preview, func(t *testing.T) {
				r := newEchoAdmissionRig(t)
				old := echoOutput(t, r, "小花猫在阁楼发现麻雀。", 19)
				if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
					t.Fatal(err)
				}
				r.start(t, "person")
				if preview != "" {
					r.preview(t, "person", preview, word)
					r.preview(t, "person", preview, word)
				}
				r.stop(t, "person")
				r.final(t, "person", word)
				r.sync(t)
				r.s.mu.Lock()
				defer r.s.mu.Unlock()
				if !old.turn.Cut || r.s.output == nil || r.s.output.turn.User != word {
					t.Error("legitimate short final lost")
				}
			})
		}
	}
}

func TestSpeakingReferencesTrackAudibleWords(t *testing.T) {
	for _, name := range []string{"spoken_form", "mid_story", "novel_mid_story", "new_question_mid_story"} {
		t.Run(name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "😋我是小满，遥远森林中住着狐狸。", 32)
			r.s.mu.Lock()
			old.spokenReference = "我是小满，遥远森林中住着狐狸。"
			if name != "spoken_form" {
				old.turn.Tracker.Add(map[string]any{"sentence_end": true, "sentence_id": json.Number("1"), "words": []any{
					map[string]any{"text": "停", "begin_time": json.Number("1450"), "end_time": json.Number("1550")},
				}}, 1560)
			}
			r.s.mu.Unlock()
			completed := int64(14400)
			if name != "spoken_form" {
				completed = 40320
			}
			if err := r.s.receiveReceipt(echoReceipt(old, completed)); err != nil {
				t.Fatal(err)
			}
			r.start(t, "audible")
			switch name {
			case "spoken_form", "mid_story":
				text := "我是小满"
				if name == "mid_story" {
					text = "停"
				}
				r.preview(t, "audible", "text", text)
				assertEchoOutputAlive(t, r, old)
				r.stop(t, "audible")
				r.final(t, "audible", text)
				assertEchoOutputAlive(t, r, old)
			case "new_question_mid_story":
				r.preview(t, "audible", "text", "你")
				assertEchoOutputAlive(t, r, old)
				r.preview(t, "audible", "text", "你叫什么名字")
				assertEchoPreviewCuts(t, r, old)
			default:
				r.preview(t, "audible", "stash", "来")
				assertEchoOutputAlive(t, r, old)
				r.stop(t, "audible")
				r.final(t, "audible", "来吗")
				r.sync(t)
				r.s.mu.Lock()
				defer r.s.mu.Unlock()
				if !old.turn.Cut || r.s.output == nil || r.s.output.turn.User != "来吗" {
					t.Error("lost unrelated short input outside opening")
				}
			}
		})
	}
}

func TestSpeakingWeakFinalNeedsFreshPlaybackDuringCapture(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reject bool
	}{{"no_receipt", false}, {"stale_receipt", false}, {"new_receipt_before_stop", true}, {"new_receipt_after_stop", false}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEchoAdmissionRig(t)
			old := echoOutput(t, r, "小花猫在阁楼", 19)
			if tc.name == "stale_receipt" {
				if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
					t.Fatal(err)
				}
				r.s.mu.Lock()
				old.lastPlaybackProgress = time.Now().Add(-time.Second)
				r.s.mu.Unlock()
			}
			r.start(t, "weak")
			r.preview(t, "weak", "stash", "好")
			assertEchoOutputAlive(t, r, old)
			if tc.name == "new_receipt_before_stop" {
				if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
					t.Fatal(err)
				}
			}
			r.stop(t, "weak")
			r.sync(t)
			if tc.name == "new_receipt_after_stop" {
				if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
					t.Fatal(err)
				}
			}
			r.final(t, "weak", "好")
			r.sync(t)
			r.s.mu.Lock()
			defer r.s.mu.Unlock()
			if tc.reject {
				if old.turn.Cut || r.s.lastInput != nil {
					t.Error("missed actual overlap")
				}
			} else if !old.turn.Cut || r.s.output == nil || r.s.output.turn.User != "好" {
				t.Error("unverified playback discarded real short final")
			}
		})
	}
}
