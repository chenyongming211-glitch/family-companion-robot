package realtimestate

import "strings"

// The reply prompt only needs enough of the past to know what was said, what the
// user actually heard, and where each reply stopped. Playback diagnostics
// (evidence structs, observed text, tail words, timing) belong to ledgers and
// analysis: repeating them every turn grows the prompt to several thousand
// characters and lets one bad answer become the template for the next one, which
// the user hears as a repeated reply.
//
// Nothing here may be derived and stored beside recent_turns. Context passes
// through memory-floor filtering and clearing, so a top-level copy of turn text
// would survive a clear and leak content the user asked to forget.
const (
	ConversationStateTurnLimit = 8
	conversationStateTextRunes = 512
)

type StateTurn struct {
	ID                    int64   `json:"id"`
	User                  string  `json:"user"`
	AssistantSpokenPrefix *string `json:"assistant_spoken_prefix"`
	Position              string  `json:"position"`
	Interrupted           bool    `json:"interrupted,omitempty"`
}

type ConversationState struct {
	RecentTurns       []StateTurn `json:"recent_turns"`
	OlderTurnsOmitted bool        `json:"older_turns_omitted"`
}

// ConversationStateFor projects the played turns into the compact shape used by
// the reply prompt. Position keeps its ContextFor meaning: complete means the
// whole response played, candidate means only part of it did, unknown means the
// boundary could not be established. The caller must own the turns' mutation.
func ConversationStateFor(turns []*Turn) string {
	start := max(0, len(turns)-ConversationStateTurnLimit)
	state := ConversationState{RecentTurns: []StateTurn{}, OlderTurnsOmitted: len(turns) > ConversationStateTurnLimit}
	for _, turn := range turns[start:] {
		evidence := PlaybackEvidence(turn)
		position := "unknown"
		if evidence.Boundary == "complete_response" {
			position = "complete"
		} else if evidence.SpokenText != nil && *evidence.SpokenText != "" {
			position = "candidate"
		}
		user := strings.TrimSpace(clipRunes(turn.User, conversationStateTextRunes))
		if user == "" {
			user = "[原始语音已交给模型，文字转写尚未就绪]"
		}
		var spoken *string
		if evidence.SpokenText != nil {
			if text := strings.TrimSpace(clipRunes(*evidence.SpokenText, conversationStateTextRunes)); text != "" {
				spoken = &text
			}
		}
		state.RecentTurns = append(state.RecentTurns, StateTurn{ID: turn.ID, User: user, AssistantSpokenPrefix: spoken, Position: position, Interrupted: turn.Cut})
	}
	return marshalPython(state, false)
}

// ProjectionCounts reports what the reply prompt actually carried. The event log
// needs this rather than len(turns): a session with sixteen turns still sends
// eight, and recording sixteen made the cap look broken.
func ProjectionCounts(turns []*Turn) (kept int, omitted bool) {
	return min(len(turns), ConversationStateTurnLimit), len(turns) > ConversationStateTurnLimit
}

func clipRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
