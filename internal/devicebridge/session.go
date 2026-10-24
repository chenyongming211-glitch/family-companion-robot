package devicebridge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"familyrobot/internal/audio"
	"github.com/gorilla/websocket"
)

const frameSamples = 1440

type block struct {
	id, epoch                                     int64
	after                                         []int64
	encoder                                       *audio.Encoder
	lookahead                                     int64
	pending                                       []byte
	valid, deviceSent, devicePlayed               int64
	sealed, started, finished, discarded, revoked bool
	finalValid                                    bool
	handoffSeq, revokeSeq                         int64
}

type session struct {
	ctx             context.Context
	client, backend *websocket.Conn
	id              string
	inputMS         int
	decoder         *audio.Decoder
	blocks          map[int64]*block
	order           []int64
	lastID          int64
	controlSeq      int64
}

func newSession(ctx context.Context, c, b *websocket.Conn, id string, inputMS int) (*session, error) {
	d, err := audio.NewDecoder()
	if err != nil {
		return nil, err
	}
	return &session{ctx: ctx, client: c, backend: b, id: id, inputMS: inputMS, decoder: d, blocks: make(map[int64]*block), controlSeq: 2}, nil
}
func (s *session) close() {
	s.decoder.Close()
	for _, b := range s.blocks {
		if b.encoder != nil {
			b.encoder.Close()
		}
	}
}

type message struct {
	backend bool
	kind    int
	data    []byte
	err     error
}

func (s *session) run() error {
	ctx, cancel := context.WithCancel(s.ctx)
	var readers sync.WaitGroup
	defer func() { cancel(); s.client.Close(); s.backend.Close(); readers.Wait() }()
	events := make(chan message, 8)
	deadline, _ := ctx.Deadline()
	for _, source := range []struct {
		c    *websocket.Conn
		back bool
	}{{s.client, false}, {s.backend, true}} {
		readers.Add(1)
		go func(c *websocket.Conn, back bool) {
			defer readers.Done()
			for {
				readDeadline := deadline
				wireDeadline := time.Now().Add(30 * time.Second)
				if !back && (deadline.IsZero() || wireDeadline.Before(deadline)) {
					readDeadline = wireDeadline
				}
				c.SetReadDeadline(readDeadline)
				kind, data, err := c.ReadMessage()
				select {
				case events <- message{backend: back, kind: kind, data: data, err: err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}(source.c, source.back)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-events:
			if m.err != nil {
				return m.err
			}
			if m.backend {
				if m.kind != websocket.TextMessage {
					return errors.New("backend text required")
				}
				e, err := parseEvent(m.data)
				if err != nil {
					return err
				}
				if err = s.output(e); err != nil {
					return err
				}
			} else if m.kind == websocket.BinaryMessage {
				if err := s.input(m.data); err != nil {
					return err
				}
			} else if m.kind == websocket.TextMessage {
				e, err := parseEvent(m.data)
				if err != nil {
					return err
				}
				if err = s.control(e); err != nil {
					return err
				}
			} else {
				return errors.New("unsupported message")
			}
		}
	}
}

func parseEvent(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var e map[string]any
	if dec.Decode(&e) != nil || e == nil {
		return nil, errors.New("invalid event")
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing event data")
	}
	if kind, ok := e["type"].(string); !ok || kind == "" {
		return nil, errors.New("missing event type")
	}
	return e, nil
}
func integer(e map[string]any, k string) int64 {
	// Local outgoing metadata uses Go integers; decoded inputs use json.Number.
	switch v := e[k].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	n, ok := e[k].(json.Number)
	if !ok {
		return -1
	}
	v, err := n.Int64()
	if err != nil {
		return -1
	}
	return v
}
func numbers(e map[string]any, k string) ([]int64, error) {
	values, ok := e[k].([]any)
	if !ok {
		return nil, errors.New("missing identity list")
	}
	if len(values) > 16 {
		return nil, errors.New("identity list limit")
	}
	out := make([]int64, 0, len(values))
	for _, v := range values {
		n, ok := v.(json.Number)
		if !ok {
			return nil, errors.New("invalid identity")
		}
		i, err := n.Int64()
		if err != nil || i <= 0 {
			return nil, errors.New("invalid identity")
		}
		for _, prev := range out {
			if prev == i {
				return nil, errors.New("duplicate identity")
			}
		}
		out = append(out, i)
	}
	return out, nil
}
func (s *session) frontend(e map[string]any) error {
	s.controlSeq++
	e["control_seq"] = s.controlSeq
	e["session_id"] = s.id
	if id := integer(e, "block_id"); id > 0 {
		e["playback_id"] = id
	}
	return sendJSON(s.client, e)
}

func (s *session) input(packet []byte) error {
	pcm, err := s.decoder.Decode(packet)
	if err != nil {
		return err
	}
	if len(pcm) != 16000*s.inputMS/1000*2 {
		return errors.New("Opus duration differs from negotiated frame")
	}
	for len(pcm) > 0 {
		n := min(len(pcm), 640)
		if err = sendJSON(s.backend, map[string]any{"type": "listen.audio", "data": base64.StdEncoding.EncodeToString(pcm[:n])}); err != nil {
			return err
		}
		pcm = pcm[n:]
	}
	return nil
}

func (s *session) control(e map[string]any) error {
	if id, ok := e["session_id"]; ok && id != s.id {
		return s.frontend(map[string]any{"type": "error", "code": "invalid_playback"})
	}
	switch e["type"] {
	case "listen":
		if e["state"] == "start" {
			return nil
		}
		if e["state"] == "stop" {
			return errors.New("client ended session")
		}
		return errors.New("invalid listen state")
	case "playback", "playback.started":
		if !s.receipt(e) {
			return s.frontend(map[string]any{"type": "error", "code": "invalid_playback"})
		}
		if e["type"] == "playback" && e["valid"] == false {
			return errors.New("playback coordinate unknown")
		}
		return nil
	case "content.snapshot":
		return sendJSON(s.backend, map[string]any{"type": "content.snapshot"})
	case "abort", "hard.stop":
		return sendJSON(s.backend, map[string]any{"type": "hard.stop"})
	default:
		return errors.New("unsupported native control")
	}
}

func (s *session) receipt(e map[string]any) bool {
	if e["session_id"] != s.id || e["basis"] != "coreaudio_playback" || integer(e, "sample_rate") != 24000 {
		return false
	}
	id, epoch := integer(e, "block_id"), integer(e, "epoch")
	b := s.blocks[id]
	if b == nil || b.epoch != epoch {
		return false
	}
	if _, ok := e["playback_id"]; ok && integer(e, "playback_id") != id {
		return false
	}
	depsDone := true
	for _, id := range b.after {
		if dep := s.blocks[id]; dep != nil && !dep.finished {
			depsDone = false
		}
	}
	if e["type"] == "playback.started" {
		observed := integer(e, "observed_control_seq")
		// The two WebSocket directions can cross: local playback may start
		// before the device sees a revoke whose server-side flag is set first.
		// Preserve only that causally earlier observation, never a start made
		// after the terminal applied the revoke.
		beforeRevoke := observed >= b.handoffSeq && observed < b.revokeSeq && observed <= s.controlSeq
		if !depsDone || b.finished || (b.revoked && !b.started && !beforeRevoke) {
			return false
		}
		if sendJSON(s.backend, map[string]any{"type": "playback.started", "block_id": id, "epoch": epoch}) != nil {
			return false
		}
		b.started = true
		return true
	}
	played := integer(e, "played_samples")
	valid, vok := e["valid"].(bool)
	finished, fok := e["finished"].(bool)
	discarded, dok := e["discarded"].(bool)
	if !vok || !fok || !dok || played < 0 || played < b.devicePlayed || played > b.deviceSent {
		return false
	}
	if b.finished && (played != b.devicePlayed || !finished || valid != b.finalValid || discarded != b.discarded) {
		return false
	}
	if discarded && (!b.revoked || len(b.after) == 0 || b.started || played != 0 || !finished) {
		return false
	}
	if !discarded && played > 0 && (!depsDone || !b.started) {
		return false
	}
	if valid && finished && (!b.sealed || (!discarded && played != b.deviceSent)) {
		return false
	}
	mapped := max(int64(0), min(played-b.lookahead, b.valid))
	if discarded {
		mapped = 0
	}
	if sendJSON(s.backend, map[string]any{"type": "playback", "block_id": id, "epoch": epoch, "played_samples": mapped, "valid": valid, "finished": finished && b.sealed, "discarded": discarded}) != nil {
		return false
	}
	b.devicePlayed = played
	if finished {
		b.finished = true
		b.finalValid = valid
		b.discarded = discarded
		b.pending = nil
		if b.encoder != nil {
			b.encoder.Close()
			b.encoder = nil
		}
	}
	return true
}

func (s *session) output(e map[string]any) error {
	switch e["type"] {
	case "output.handoff":
		id, epoch := integer(e, "block_id"), integer(e, "epoch")
		if id <= s.lastID || epoch < 0 {
			return errors.New("invalid backend block identity")
		}
		after, err := numbers(e, "after")
		if err != nil {
			return err
		}
		if integer(e, "input_epoch") < 0 {
			return errors.New("invalid input epoch")
		}
		for _, id := range after {
			b := s.blocks[id]
			if b == nil || b.epoch >= epoch {
				return errors.New("invalid handoff predecessor")
			}
		}
		active := 0
		for _, b := range s.blocks {
			if !b.finished {
				active++
			}
		}
		if active >= 16 {
			return errors.New("playback ledger limit")
		}
		enc, err := audio.NewEncoder()
		if err != nil {
			return err
		}
		look, err := enc.LookaheadSamples()
		if err != nil {
			enc.Close()
			return err
		}
		s.blocks[id] = &block{id: id, epoch: epoch, after: after, encoder: enc, lookahead: int64(look), handoffSeq: s.controlSeq + 1}
		s.lastID = id
		s.order = append(s.order, id)
		s.prune()
		return s.frontend(e)
	case "audio":
		b, err := s.findBlock(e)
		if err != nil || b.sealed || b.finished {
			return errors.New("late backend audio")
		}
		n := integer(e, "samples")
		encoded, ok := e["data"].(string)
		if !ok || n <= 0 || n > 480 {
			return errors.New("invalid backend audio frame")
		}
		pcm, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || int64(len(pcm)) != n*2 || b.valid+n > 24000*12 {
			return errors.New("invalid backend PCM")
		}
		b.valid += n
		b.pending = append(b.pending, pcm...)
		if !s.withinCredits() {
			return errors.New("codec and PCM allowance exceeded")
		}
		return s.encode(b, false)
	case "audio.end":
		b, err := s.findBlock(e)
		if err != nil || b.sealed || integer(e, "sent_samples") != b.valid {
			return errors.New("invalid backend seal")
		}
		if e["policy"] != "natural" && e["policy"] != "drain" {
			return errors.New("unsupported seal policy")
		}
		b.sealed = true
		b.pending = append(b.pending, make([]byte, b.lookahead*2)...)
		if err = s.encode(b, true); err != nil {
			return err
		}
		e["valid_samples"] = b.valid
		e["device_sent_samples"] = b.deviceSent
		e["lookahead_samples"] = b.lookahead
		return s.frontend(e)
	case "output.drain", "output.revoke":
		ids, err := numbers(e, "blocks")
		if err != nil {
			return err
		}
		for _, id := range ids {
			b := s.blocks[id]
			if b == nil || !b.sealed {
				return errors.New("unsealed control block")
			}
			if e["type"] == "output.revoke" && !b.revoked {
				b.revoked = true
				b.revokeSeq = s.controlSeq + 1
			}
		}
		return s.frontend(e)
	case "playback.ack":
		if _, err := s.findBlock(e); err != nil {
			return err
		}
		return s.frontend(e)
	case "audio.packet", "audio.cancel":
		return errors.New("unsupported backend audio command")
	case "error":
		if err := s.frontend(e); err != nil {
			return err
		}
		return errors.New("backend voice session failed")
	default:
		return s.frontend(e)
	}
}

func (s *session) prune() {
	for len(s.order) > 64 {
		found := false
		for i, id := range s.order {
			if s.blocks[id].finished {
				delete(s.blocks, id)
				s.order = append(s.order[:i], s.order[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
}

func (s *session) findBlock(e map[string]any) (*block, error) {
	b := s.blocks[integer(e, "block_id")]
	if b == nil || b.epoch != integer(e, "epoch") {
		return nil, errors.New("unknown backend block")
	}
	return b, nil
}

// Reserve the complete eventual codec tail for every admitted block, so small
// chunks cannot accumulate an unbounded amount of padding outside raw credits.
func (s *session) withinCredits() bool {
	var raw, device int64
	for _, b := range s.blocks {
		if b.finished {
			continue
		}
		mapped := max(int64(0), min(b.devicePlayed-b.lookahead, b.valid))
		raw += b.valid - mapped
		total := (b.valid + b.lookahead + frameSamples - 1) / frameSamples * frameSamples
		device += total - b.devicePlayed
	}
	return raw <= 10080 && device <= 14400
}

func (s *session) encode(b *block, seal bool) error {
	for len(b.pending) >= audio.FrameBytes || seal && len(b.pending) > 0 {
		pcm := make([]byte, audio.FrameBytes)
		n := min(len(b.pending), len(pcm))
		copy(pcm, b.pending[:n])
		b.pending = b.pending[n:]
		packet, err := b.encoder.Encode(pcm)
		if err != nil {
			return err
		}
		// One writer sends this pair without interleaving another control frame.
		if err = s.frontend(map[string]any{"type": "audio.packet", "block_id": b.id, "epoch": b.epoch, "samples": frameSamples}); err != nil {
			return err
		}
		s.client.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
		if err = s.client.WriteMessage(websocket.BinaryMessage, packet); err != nil {
			return err
		}
		b.deviceSent += frameSamples
	}
	return nil
}
