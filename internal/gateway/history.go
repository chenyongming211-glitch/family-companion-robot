package gateway

import "encoding/json"

type turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
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
	h.Turns = append(h.Turns, turn{role, limitText(text, 512)})
	if len(h.Turns) > 12 {
		h.Turns = h.Turns[len(h.Turns)-12:]
	}
}
func (h *history) context() string { b, _ := json.Marshal(h); return string(b) }
