//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// The observer accepts exactly the pairs in this shared file. Reading the same
// file from both sides is what stops a token from being added to one side only:
// the 2026-11-26 P0 was worker_unavailable sent by this helper and rejected by
// the observer, which took the whole session's voice filtering down with it.
const protocolTablePath = "../../testdata/speaker_observation_protocol.json"

type protocolPair struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

func loadProtocolTable(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(protocolTablePath)
	if err != nil {
		t.Fatalf("shared protocol table: %v", err)
	}
	var doc struct {
		Comment      string         `json:"comment"`
		Observations []protocolPair `json:"observations"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("shared protocol table: %v", err)
	}
	if len(doc.Observations) == 0 {
		t.Fatal("shared protocol table is empty")
	}
	table := make(map[string]bool, len(doc.Observations))
	for _, pair := range doc.Observations {
		if table[pair.Label+"|"+pair.Reason] {
			t.Fatalf("duplicate pair in shared protocol table: %v", pair)
		}
		table[pair.Label+"|"+pair.Reason] = true
	}
	return table
}

func protocolKey(event sessionEvent) string { return event.Label + "|" + event.Reason }

// runProtocolClips drives one realtime session over explicit clips and returns
// the events the helper wrote, together with the session's own error.
func runProtocolClips(t *testing.T, pcms [][]byte, opt sessionOptions) ([]sessionEvent, error) {
	t.Helper()
	var in, out bytes.Buffer
	var start uint64
	for i, pcm := range pcms {
		h := sessionFrame(uint64(i+1), start, len(pcm)/2)
		h.Scope = "realtime-test"
		start = h.End
		if err := writeRequest(&in, h, pcm); err != nil {
			t.Fatal(err)
		}
	}
	opt.realtime = true
	opt.validate = func(string, string) error { return nil }
	opt.lifetime = time.Second
	err := runSession(context.Background(), io.NopCloser(&in), &out, "model", strings.Repeat("a", 64), true, opt)
	return sessionEvents(t, out.Bytes()), err
}

func constantVectors(vectors [][]float32) func(context.Context, requestHeader, []byte) ([]float32, error) {
	calls := 0
	return func(context.Context, requestHeader, []byte) ([]float32, error) {
		v := vectors[calls%len(vectors)]
		calls++
		return append([]float32(nil), v...), nil
	}
}

func TestSessionEmittedPairsMatchSharedTable(t *testing.T) {
	table := loadProtocolTable(t)
	produced := map[string]bool{}
	collect := func(events []sessionEvent, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Event != "observation" {
				continue
			}
			if event.Reason == "" {
				t.Fatalf("realtime observation without a reason: %+v", event)
			}
			key := protocolKey(event)
			if !table[key] {
				t.Fatalf("helper emitted %q, which is not in the shared protocol table", key)
			}
			produced[key] = true
		}
	}
	speech := sessionPCM(32000)
	silence := make([]byte, 32000*2)
	// Quality gate: too short, then below the energy floor.
	collect(runProtocolClips(t, [][]byte{sessionPCM(23999)}, sessionOptions{extract: constantVectors([][]float32{{1, 0}})}))
	collect(runProtocolClips(t, [][]byte{silence}, sessionOptions{extract: constantVectors([][]float32{{1, 0}})}))
	// Tracker: a settled speaker, a gray-zone match, and eight seats plus capacity.
	collect(runProtocolClips(t, [][]byte{speech, speech}, sessionOptions{extract: constantVectors([][]float32{{1, 0}})}))
	collect(runProtocolClips(t, [][]byte{speech, speech}, sessionOptions{extract: constantVectors([][]float32{{1, 0}, {.65, .76}})}))
	distinct := make([][]float32, 9)
	for i := range distinct {
		distinct[i] = make([]float32, 9)
		distinct[i][i] = 1
	}
	nine := make([][]byte, 9)
	for i := range nine {
		nine[i] = speech
	}
	collect(runProtocolClips(t, nine, sessionOptions{extract: constantVectors(distinct)}))
	// Per-clip worker failure.
	collect(runProtocolClips(t, [][]byte{speech}, sessionOptions{extract: func(context.Context, requestHeader, []byte) ([]float32, error) {
		return nil, errClipUnavailable
	}}))
	for key := range table {
		if !produced[key] {
			t.Fatalf("shared protocol table lists %q, but no scenario emits it", key)
		}
	}
	if len(produced) != len(table) {
		t.Fatalf("produced %d pairs, shared table has %d", len(produced), len(table))
	}
}

// A worker that cannot start still gets three clips before the session ends:
// the best-effort prewarm does not consume one of the three strikes.
func TestSessionPrewarmFailureDoesNotConsumeStrike(t *testing.T) {
	errStart := errors.New("worker could not start")
	starts := 0
	var in, out bytes.Buffer
	for i := 0; i < 3; i++ {
		h := sessionFrame(uint64(i+1), uint64(i*32000), 32000)
		h.Scope = "realtime-test"
		if err := writeRequest(&in, h, sessionPCM(32000)); err != nil {
			t.Fatal(err)
		}
	}
	opt := sessionOptions{realtime: true, validate: func(string, string) error { return nil }, lifetime: time.Second,
		start: func(context.Context, string, string) (*workerProcess, error) {
			starts++
			return nil, errStart
		}}
	err := runSession(context.Background(), io.NopCloser(&in), &out, "model", strings.Repeat("a", 64), true, opt)
	// Four attempts: the prewarm plus three clips, and only the third clip ends
	// the session.
	if err != errWorker || starts != 4 {
		t.Fatalf("err=%v starts=%d want errWorker/4", err, starts)
	}
	events := sessionEvents(t, out.Bytes())
	if len(events) != 5 {
		t.Fatalf("events=%+v", events)
	}
	for i := 1; i <= 3; i++ {
		if events[i].Label != "uncertain" || events[i].Reason != "worker_unavailable" {
			t.Fatalf("clip %d must degrade, not end the session: %+v", i, events[i])
		}
	}
}
