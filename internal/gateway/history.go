package gateway

import (
	"encoding/json"
	"time"
)

type turn struct {
	At            time.Time `json:"at"`
	SessionID     string    `json:"session_id,omitempty"`
	ResponseID    string    `json:"response_id,omitempty"`
	InputID       string    `json:"input_id,omitempty"`
	PlaybackID    uint32    `json:"playback_id,omitempty"`
	playbackID    uint32
	Role          string `json:"role"`
	Text          string `json:"text,omitempty"`
	Delivery      string `json:"delivery,omitempty"`
	GeneratedText string `json:"generated_text,omitempty"`
}
type history struct {
	archive     []turn
	Turns       []turn `json:"turns"`
	Interrupted bool   `json:"previous_reply_interrupted"`
}

func limitText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
func (h *history) add(role, text string) {
	if text == "" {
		return
	}
	h.appendTurn(turn{Role: role, Text: limitText(text, 512)})
}

// Like LiveKit's optional synchronized_transcript, unknown playback must remain
// unknown. Sending the final packet is not evidence of words heard on hardware.
func (h *history) addSent(generated string) {
	if generated != "" {
		h.appendTurn(turn{Role: "assistant", Delivery: "transport_sent", GeneratedText: limitText(generated, 512)})
	}
}

// Keep an unfinished reply as draft data, never as words known to be heard.
// A completed side answer may clear the latest-interruption flag, but this
// chronological record remains available for an explicit request to continue.
func (h *history) addInterrupted(generated string) {
	h.appendTurn(turn{Role: "assistant", Delivery: "interrupted", GeneratedText: limitText(generated, 512)})
}

func (h *history) appendTurn(t turn) {
	if t.At.IsZero() {
		t.At = time.Now()
	}
	h.archive = append(h.archive, t)
	if len(h.archive) > 128 {
		h.archive = h.archive[len(h.archive)-128:]
	}
	h.Turns = append(h.Turns, t)
	if len(h.Turns) > 12 {
		h.Turns = h.Turns[len(h.Turns)-12:]
	}
}
func (h *history) context() string {
	// Retain generated drafts internally for diagnostics. Interrupted drafts have
	// no validated text boundary, so never put their unplayed content in context.
	safe := history{Interrupted: h.Interrupted, Turns: make([]turn, 0, len(h.Turns))}
	for _, t := range h.Turns {
		if t.Delivery == "interrupted" {
			t.GeneratedText = ""
			t.Text = ""
		}
		safe.Turns = append(safe.Turns, t)
	}
	b, _ := json.Marshal(safe)
	return string(b)
}

func (h *history) syncArchive() {
	for _, live := range h.Turns {
		for i := range h.archive {
			if h.archive[i].At == live.At {
				h.archive[i] = live
				break
			}
		}
	}
}
func (h *history) stampLast(sessionID, responseID, inputID string, playbackID uint32) {
	if len(h.Turns) == 0 {
		return
	}
	t := &h.Turns[len(h.Turns)-1]
	t.SessionID = sessionID
	t.ResponseID = responseID
	t.InputID = inputID
	t.PlaybackID = playbackID
	h.syncArchive()
}
