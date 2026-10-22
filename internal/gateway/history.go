package gateway

import "encoding/json"

type turn struct {
	Role          string `json:"role"`
	Text          string `json:"text,omitempty"`
	Delivery      string `json:"delivery,omitempty"`
	GeneratedText string `json:"generated_text,omitempty"`
}
type history struct {
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

// Keep an unfinished reply as draft data, never as words known to be heard.
// A completed side answer may clear the latest-interruption flag, but this
// chronological record remains available for an explicit request to continue.
func (h *history) addInterrupted(generated string) {
	h.appendTurn(turn{Role: "assistant", Delivery: "interrupted", GeneratedText: limitText(generated, 512)})
}

func (h *history) appendTurn(t turn) {
	h.Turns = append(h.Turns, t)
	if len(h.Turns) > 12 {
		h.Turns = h.Turns[len(h.Turns)-12:]
	}
}
func (h *history) context() string { b, _ := json.Marshal(h); return string(b) }
