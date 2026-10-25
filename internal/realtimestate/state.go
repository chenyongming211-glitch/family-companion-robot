// Package realtimestate ports the isolated Realtime experiment's state contracts.
// Its caller owns serialization: all mutation must run under the session mutex.
package realtimestate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
)

const RecentTurnLimit = 16
const PacketSamples int64 = 1440
const WindowSamples int64 = 7200

type AudioRing struct {
	Capacity, End int64
	data          []byte
}

func NewAudioRing(capacity ...int64) *AudioRing {
	n := int64(16000 * 35)
	if len(capacity) > 0 {
		n = capacity[0]
	}
	return &AudioRing{Capacity: n}
}
func (r *AudioRing) Append(pcm []byte) error {
	if len(pcm)%2 != 0 {
		return errors.New("unaligned_pcm")
	}
	if r.Capacity < 0 {
		return errors.New("invalid_capacity")
	}
	r.data = append(r.data, pcm...)
	r.End += int64(len(pcm) / 2)
	if int64(len(r.data)) > r.Capacity*2 {
		r.data = r.data[int64(len(r.data))-r.Capacity*2:]
	}
	return nil
}
func (r *AudioRing) Slice(start, end int64) []byte {
	first := r.End - int64(len(r.data)/2)
	if !(first <= start && start < end && end <= r.End) {
		return nil
	}
	return append([]byte(nil), r.data[(start-first)*2:(end-first)*2]...)
}

type Turn struct {
	ID                        int64
	User, Generated           string
	Sent, Completed, Accepted int64
	Cut, Begun, Ended         bool
	DrainTarget               *int64
	DrainDone                 chan struct{}
	Tracker                   *PrefixTracker
	drainSignalled            bool
}

func NewTurn(id int64) *Turn {
	return &Turn{ID: id, DrainDone: make(chan struct{}), Tracker: NewPrefixTracker("", 0)}
}
func (t *Turn) CanSend() bool { return !t.Cut && t.Sent-t.Completed < WindowSamples }
func (t *Turn) SealForDrain() {
	if t.DrainTarget == nil {
		t.Cut = true
		n := t.Sent
		t.DrainTarget = &n
	}
}

// MarkDrainDone also covers cancellation before an audible response began.
func (t *Turn) MarkDrainDone() {
	if !t.drainSignalled {
		if t.DrainDone == nil {
			t.DrainDone = make(chan struct{})
		}
		close(t.DrainDone)
		t.drainSignalled = true
	}
}
func (t *Turn) Receipt(event map[string]any) bool {
	id, idOK := strictInt(event["id"])
	completed, completedOK := strictInt(event["completed"])
	accepted, acceptedOK := strictInt(event["accepted"])
	terminal, exists := event["terminal"]
	if !exists {
		terminal = "progress"
	}
	if !idOK || id != t.ID || event["basis"] != "CoreAudio_dataPlayedBack" || !completedOK || !acceptedOK || !(t.Completed <= completed && completed <= accepted && accepted <= t.Sent) || accepted < t.Accepted || completed%PacketSamples != 0 || accepted%PacketSamples != 0 {
		return false
	}
	label, ok := terminal.(string)
	if !ok || (label != "progress" && label != "played" && label != "drained") {
		return false
	}
	if label == "drained" && (t.DrainTarget == nil || completed != *t.DrainTarget || accepted != *t.DrainTarget) {
		return false
	}
	if label == "played" && (!t.Ended || completed != t.Sent || accepted != t.Sent) {
		return false
	}
	t.Completed, t.Accepted = completed, accepted
	if label == "drained" {
		t.MarkDrainDone()
	}
	return true
}
func (t *Turn) ReceiptJSON(raw []byte) bool {
	event := map[string]any{}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&event) != nil {
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return false
	}
	return t.Receipt(event)
}
func (t *Turn) Prefix() map[string]any {
	t.Tracker.Generated = t.Generated
	t.Tracker.TotalAudioMS = float64(t.Sent) / 24
	return t.Tracker.Candidate(float64(t.Completed) / 24)
}

type ContextTurn struct {
	ID                    int64    `json:"id"`
	User                  string   `json:"user"`
	AssistantSpokenPrefix *string  `json:"assistant_spoken_prefix"`
	Position              string   `json:"position"`
	PlaybackEvidence      Evidence `json:"playback_evidence"`
	Interrupted           bool     `json:"interrupted"`
}
type Context struct {
	RecentTurns       []ContextTurn `json:"recent_turns"`
	OlderTurnsOmitted bool          `json:"older_turns_omitted"`
}

func ContextFor(turns []*Turn) string {
	context := Context{RecentTurns: []ContextTurn{}, OlderTurnsOmitted: len(turns) > RecentTurnLimit}
	start := 0
	if len(turns) > RecentTurnLimit {
		start = len(turns) - RecentTurnLimit
	}
	for _, turn := range turns[start:] {
		evidence := PlaybackEvidence(turn)
		position := "unknown"
		if evidence.Boundary == "complete_response" {
			position = "complete"
		} else if evidence.SpokenText != nil && *evidence.SpokenText != "" {
			position = "candidate"
		}
		user := []rune(turn.User)
		if len(user) > 2048 {
			user = user[:2048]
		}
		text := string(user)
		if text == "" {
			text = "[原始语音已交给模型，文字转写尚未就绪]"
		}
		context.RecentTurns = append(context.RecentTurns, ContextTurn{turn.ID, text, evidence.SpokenText, position, evidence, turn.Cut})
	}
	return marshalPython(context, false)
}

// strictInt intentionally rejects bool, decimal/exponent JSON numbers and Go floats.
func strictInt(value any) (int64, bool) {
	switch n := value.(type) {
	case json.Number:
		if strings.ContainsAny(string(n), ".eE") {
			return 0, false
		}
		v, e := strconv.ParseInt(string(n), 10, 64)
		return v, e == nil
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		if uint64(n) > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	default:
		return 0, false
	}
}
func numeric(value any) (float64, bool) {
	var v float64
	switch n := value.(type) {
	case json.Number:
		var e error
		v, e = n.Float64()
		if e != nil {
			return 0, false
		}
	case float64:
		v = n
	case float32:
		v = float64(n)
	default:
		integer, ok := strictInt(value)
		if !ok {
			return 0, false
		}
		v = float64(integer)
	}
	return v, !math.IsNaN(v) && !math.IsInf(v, 0)
}
func object(value any) map[string]any { v, _ := value.(map[string]any); return v }
func list(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		r := make([]any, len(v))
		for i := range v {
			r[i] = v[i]
		}
		return r
	default:
		return nil
	}
}

type pythonFloat float64

func (n pythonFloat) MarshalJSON() ([]byte, error) {
	s := strconv.FormatFloat(float64(n), 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return []byte(s), nil
}
func marshalPython(value any, compact bool) string {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		panic(err)
	}
	raw := bytes.TrimSuffix(out.Bytes(), []byte{'\n'})
	// Python ensure_ascii=False also leaves the two JavaScript line separators literal.
	var unicodeJSON bytes.Buffer
	for i := 0; i < len(raw); {
		if raw[i] == '\\' && i+1 < len(raw) {
			if i+6 <= len(raw) && (string(raw[i:i+6]) == `\u2028` || string(raw[i:i+6]) == `\u2029`) {
				if raw[i+5] == '8' {
					unicodeJSON.WriteRune('\u2028')
				} else {
					unicodeJSON.WriteRune('\u2029')
				}
				i += 6
			} else {
				unicodeJSON.Write(raw[i : i+2])
				i += 2
			}
		} else {
			unicodeJSON.WriteByte(raw[i])
			i++
		}
	}
	raw = unicodeJSON.Bytes()
	if compact {
		return string(raw)
	}
	var spaced strings.Builder
	quoted, escaped := false, false
	for _, c := range string(raw) {
		spaced.WriteRune(c)
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		} else if c == '"' {
			quoted = true
		} else if c == ',' || c == ':' {
			spaced.WriteByte(' ')
		}
	}
	return spaced.String()
}
