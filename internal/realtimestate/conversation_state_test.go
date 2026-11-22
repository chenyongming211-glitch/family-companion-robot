package realtimestate

import (
	"encoding/json"
	"strings"
	"testing"
)

func completeTurn(id int64, user, reply string) *Turn {
	turn := NewTurn(id)
	turn.User = user
	turn.Generated = reply
	turn.Sent, turn.Completed = 1440, 1440
	turn.Begun, turn.Ended = true, true
	return turn
}

func partialTurn(id int64, user, spoken string) *Turn {
	turn := NewTurn(id)
	turn.User = user
	turn.Generated = spoken + "，后面还有很长一段没有播完的内容。"
	turn.Sent, turn.Completed = 1440*20, 1440*10
	turn.Begun = true
	turn.Tracker.Add(map[string]any{
		"sentence_id": 1, "sentence_end": true,
		"words": []any{map[string]any{"text": spoken, "begin_time": 0.0, "end_time": 400.0}},
	}, 1000)
	return turn
}

func decodeState(t *testing.T, raw string) (map[string]any, []any) {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("state is not JSON: %v (%s)", err, raw)
	}
	turns, _ := state["recent_turns"].([]any)
	return state, turns
}

// Playback diagnostics belong to ledgers and analysis. Repeating them in every
// reply prompt is what let one bad answer become the template for the next.
func TestConversationStateKeepsOnlyPromptFields(t *testing.T) {
	raw := ConversationStateFor([]*Turn{
		completeTurn(1, "讲个故事", "小兔走进森林。"),
		partialTurn(2, "后来呢", "小兔遇到了狐狸"),
	})
	for _, forbidden := range []string{"playback_evidence", "spoken_text", "observed_text", "tail_words", "aligned_prefix", "completed_ms"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("diagnostic field %q leaked into the reply state: %s", forbidden, raw)
		}
	}
	state, turns := decodeState(t, raw)
	allowedTop := map[string]bool{"recent_turns": true, "older_turns_omitted": true}
	for key := range state {
		if !allowedTop[key] {
			t.Fatalf("unexpected top-level field %q: it would survive a memory clear", key)
		}
	}
	if len(turns) != 2 {
		t.Fatalf("expected two turns, got %d", len(turns))
	}
	allowed := map[string]bool{"id": true, "user": true, "assistant_spoken_prefix": true, "position": true, "interrupted": true}
	for _, raw := range turns {
		for key := range raw.(map[string]any) {
			if !allowed[key] {
				t.Fatalf("unexpected field %q in the reply state", key)
			}
		}
	}
	if position := turns[0].(map[string]any)["position"]; position != "complete" {
		t.Fatalf("full playback must read as complete, got %v", position)
	}
	if position := turns[1].(map[string]any)["position"]; position != "candidate" {
		t.Fatalf("partial playback must read as candidate, got %v", position)
	}
	if spoken := turns[0].(map[string]any)["assistant_spoken_prefix"]; spoken != "小兔走进森林。" {
		t.Fatalf("played text lost: %v", spoken)
	}
}

// Clearing memory filters recent_turns by floor, so any text copied outside the
// turns would be handed to the model after the user asked to forget it.
func TestConversationStateCopiesNoTextOutsideTurns(t *testing.T) {
	raw := ConversationStateFor([]*Turn{completeTurn(1, "讲个故事", "小兔走进森林。")})
	if strings.Count(raw, "小兔走进森林。") != 1 {
		t.Fatalf("played text must appear once, inside its turn: %s", raw)
	}
	if strings.Count(raw, "讲个故事") != 1 {
		t.Fatalf("user text must appear once, inside its turn: %s", raw)
	}
}

func TestConversationStateKeepsCutMark(t *testing.T) {
	cut := completeTurn(1, "讲个故事", "小兔走进森林。")
	cut.Cut = true
	_, turns := decodeState(t, ConversationStateFor([]*Turn{cut}))
	if turns[0].(map[string]any)["interrupted"] != true {
		t.Fatalf("an interrupted turn must stay marked: %v", turns[0])
	}
}

// The window and the per-field bounds cap the prompt even when a long story or
// a long user turn would otherwise grow it without limit.
func TestConversationStateBoundsWindowAndText(t *testing.T) {
	turns := []*Turn{}
	for id := int64(1); id <= 12; id++ {
		turns = append(turns, completeTurn(id, strings.Repeat("问", 2000), strings.Repeat("答", 2000)))
	}
	state, recent := decodeState(t, ConversationStateFor(turns))
	if len(recent) != ConversationStateTurnLimit {
		t.Fatalf("window must keep %d turns, got %d", ConversationStateTurnLimit, len(recent))
	}
	if state["older_turns_omitted"] != true {
		t.Fatal("dropping older turns must be declared")
	}
	if first := recent[0].(map[string]any)["id"]; first != float64(len(turns)-ConversationStateTurnLimit+1) {
		t.Fatalf("window must keep the newest turns, got first id %v", first)
	}
	for _, raw := range recent {
		entry := raw.(map[string]any)
		if n := len([]rune(entry["user"].(string))); n > conversationStateTextRunes {
			t.Fatalf("user text not bounded: %d runes", n)
		}
		if n := len([]rune(entry["assistant_spoken_prefix"].(string))); n > conversationStateTextRunes {
			t.Fatalf("spoken text not bounded: %d runes", n)
		}
	}
}

// The whole point is a smaller prompt with the same conversational facts.
func TestConversationStateIsSmallerThanContextFor(t *testing.T) {
	turns := []*Turn{}
	for id := int64(1); id <= 16; id++ {
		turns = append(turns, completeTurn(id, strings.Repeat("用户问话", 6), strings.Repeat("助手的回答", 30)))
	}
	compact := ConversationStateFor(turns)
	full := ContextFor(turns)
	if len(compact)*2 > len(full) {
		t.Fatalf("compact state must stay under half the old projection: compact=%d full=%d", len(compact), len(full))
	}
}

func TestConversationStateMarksMissingTranscript(t *testing.T) {
	_, turns := decodeState(t, ConversationStateFor([]*Turn{completeTurn(1, "", "好的。")}))
	if user := turns[0].(map[string]any)["user"]; !strings.Contains(user.(string), "文字转写尚未就绪") {
		t.Fatalf("a turn without a transcript must say so: %v", user)
	}
}
