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
	if !strings.HasPrefix(got, base+extra) {
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
	if got := composeReplyPrompt("base", "extra", "   "); got != "baseextra" {
		t.Fatalf("empty turn input changed the prompt: %q", got)
	}
}
