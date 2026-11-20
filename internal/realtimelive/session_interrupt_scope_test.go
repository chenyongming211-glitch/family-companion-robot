package realtimelive

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"familyrobot/internal/realtimestate"
)

func TestSpokenReferenceDoesNotBecomeAudioOrReplaceTranscript(t *testing.T) {
	server, _ := testSocket(t)
	product, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(server, Config{Model: "test", Voice: "Tina"}, t.TempDir(), product)
	defer s.cancel()
	// A reference exactly one PCM frame long exposes accidental audio emission.
	reference := strings.Repeat("好", 960)
	transcript := "😋" + reference
	s.generate = func(_ context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		if err := emit("text", []byte(transcript)); err != nil {
			return err
		}
		if err := emit("spoken_reference", []byte(reference)); err != nil {
			return err
		}
		return emit("audio", make([]byte, 2880))
	}
	rt := s.addTurn()
	s.reply(rt, make([]byte, 640))
	// One real audio frame and the ordinary Opus tail.
	if rt.turn.Sent != 2*1440 || rt.turn.Generated != transcript || rt.spokenReference != reference {
		t.Fatalf("spoken metadata affected playback or transcript: sent=%d", rt.turn.Sent)
	}
	if rt.asr != nil {
		rt.asr.Close()
	}
}

func TestInputFromEarlierOutputCannotCancelNewTurn(t *testing.T) {
	server, _ := testSocket(t)
	s := NewSession(server, Config{}, t.TempDir(), nil)
	defer s.cancel()
	old := s.addTurn()
	input := &pendingInput{id: 2, outputAtStart: old}
	s.lastInput = input
	current := s.addTurn()
	if err := s.interruptInput(nil, "provider_asr_final", input); err != nil {
		t.Fatal(err)
	}
	if current.turn.Cut || s.output != current || !input.retired || s.lastInput != nil {
		t.Fatal("late recognition retired the wrong output or retained the stale input")
	}
}

func TestUnconfirmedOverlapRetiredWhenItsOutputFails(t *testing.T) {
	r := newEchoAdmissionRig(t)
	old := echoOutput(t, r, "小猫上山。", 19)
	if err := r.s.receiveReceipt(echoReceipt(old, 14400)); err != nil {
		t.Fatal(err)
	}
	r.start(t, "failed-output")
	r.preview(t, "failed-output", "text", "小")
	assertEchoOutputAlive(t, r, old)
	r.s.turnError(old)
	r.stop(t, "failed-output")
	r.final(t, "failed-output", "迟到的输入。")
	r.sync(t)
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.lastInput != nil || len(r.s.history) != 1 || r.s.output != nil {
		t.Fatal("late final from failed playback created another turn")
	}
}

func TestInterruptAuditIdentifiesActualSourceAndInput(t *testing.T) {
	for _, source := range []string{"provider_asr_preview", "provider_asr_final", "device_abort", "generation_error"} {
		t.Run(source, func(t *testing.T) {
			server, _ := testSocket(t)
			dir := t.TempDir()
			s := NewSession(server, Config{}, dir, nil)
			defer s.cancel()
			out := s.addTurn()
			var input *pendingInput
			var expected *liveTurn
			switch source {
			case "provider_asr_preview", "provider_asr_final":
				input = &pendingInput{id: 2, outputAtStart: out}
				s.lastInput = input
			case "generation_error":
				expected = out
			}
			if err := s.interruptInput(expected, source, input); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(filepath.Join(dir, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			scan := bufio.NewScanner(f)
			for scan.Scan() {
				var row map[string]any
				if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
					t.Fatal(err)
				}
				if row["event"] != "interrupt" {
					continue
				}
				if row["source"] != source || row["id"] != float64(out.turn.ID) {
					t.Fatal("missing cancellation source or output ID", row)
				}
				if input != nil && row["input_id"] != float64(input.id) {
					t.Fatal("missing triggering input ID", row)
				}
				return
			}
			t.Fatalf("interrupt audit absent: %v", scan.Err())
		})
	}
}
