package realtimelive

import (
	"strings"
	"testing"
)

// A reply hands the user's words to the provider as audio only. When that audio
// turn yields no input the model falls back to replaying a line from the
// restored context, which the user hears as a repeated answer, so the settled
// transcript has to be stated as this turn's input and must come last.
func TestComposeReplyPromptStatesCurrentTurnInputLast(t *testing.T) {
	base := "身份。\n对话记录：{\"recent_turns\":[]}"
	extra := "\n控制：无\n"
	got := composeReplyPrompt(base, extra, "  某个国家的情况。 ")
	if !strings.HasPrefix(got, base+compactStateNote+extra) {
		t.Fatalf("background must precede the turn input: %q", got)
	}
	if !strings.HasSuffix(got, "某个国家的情况。\n") {
		t.Fatalf("turn input missing or not last: %q", got)
	}
	if strings.Count(got, "某个国家的情况。") != 1 {
		t.Fatalf("turn input repeated: %q", got)
	}
}

// A turn with no settled transcript must not gain an empty instruction line.
func TestComposeReplyPromptOmitsEmptyTurnInput(t *testing.T) {
	if got := composeReplyPrompt("base", "extra", "   "); got != "base"+compactStateNote+"extra" {
		t.Fatalf("empty turn input changed the prompt: %q", got)
	}
}

// The projection no longer carries playback evidence, so the prompt has to name
// the fields it does carry and the one rule the old dump could not state: do not
// restart with an opening or a passage that was already used.
func TestComposeReplyPromptExplainsCompactState(t *testing.T) {
	for _, required := range []string{"精简投影", "assistant_spoken_prefix", "position=complete", "candidate", "interrupted", "不要复述"} {
		if !strings.Contains(compactStateNote, required) {
			t.Fatalf("state note must explain %q: %s", required, compactStateNote)
		}
	}
	for _, stale := range []string{"playback_evidence", "said_openings", "spoken_text"} {
		if strings.Contains(compactStateNote, stale) {
			t.Fatalf("the note must not describe %q, which the projection no longer carries", stale)
		}
	}
}
