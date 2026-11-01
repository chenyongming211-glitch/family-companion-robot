//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func sessionPCM(n int) []byte {
	p := make([]byte, n*2)
	for i := 0; i < len(p); i += 2 {
		binary.LittleEndian.PutUint16(p[i:], 1000)
	}
	return p
}
func sessionFrame(id, start uint64, n int) requestHeader {
	return requestHeader{Version: 1, ID: id, Session: "manual_1", Scope: "manual-test", Generation: 1, Start: start, End: start + uint64(n), ModelSHA: strings.Repeat("a", 64), SampleRate: 16000, PCMBytes: n * 2}
}
func sessionEvents(t *testing.T, b []byte) []sessionEvent {
	t.Helper()
	var events []sessionEvent
	d := json.NewDecoder(bytes.NewReader(b))
	for {
		var e sessionEvent
		err := d.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	return events
}
func testSession(t *testing.T, headers []requestHeader, vectors [][]float32) ([]sessionEvent, int) {
	t.Helper()
	var input, output bytes.Buffer
	for _, h := range headers {
		if err := writeRequest(&input, h, sessionPCM(h.PCMBytes/2)); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	opt := sessionOptions{validate: func(string, string) error { return nil }, extract: func(ctx context.Context, h requestHeader, p []byte) ([]float32, error) {
		v := append([]float32(nil), vectors[calls%len(vectors)]...)
		calls++
		return v, nil
	}, lifetime: time.Second}
	if err := runSession(context.Background(), io.NopCloser(&input), &output, "model", strings.Repeat("a", 64), true, opt); err != nil {
		t.Fatal(err)
	}
	return sessionEvents(t, output.Bytes()), calls
}
func TestSessionAAAndABA(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vectors [][]float32
		want    []string
	}{{"AA", [][]float32{{1, 0}, {1, 0}}, []string{"guest_1", "guest_1"}}, {"ABA", [][]float32{{1, 0}, {0, 1}, {1, 0}}, []string{"guest_1", "guest_2", "guest_1"}}} {
		t.Run(tc.name, func(t *testing.T) {
			var hs []requestHeader
			for i := range tc.vectors {
				hs = append(hs, sessionFrame(uint64(i+1), uint64(i*32000), 32000))
			}
			ev, c := testSession(t, hs, tc.vectors)
			if c != len(hs) || len(ev) != len(hs)+2 || ev[0].Event != "ready" || ev[len(ev)-1].Event != "closed" {
				t.Fatalf("events=%+v calls=%d", ev, c)
			}
			for i, w := range tc.want {
				if ev[i+1].Label != w || ev[i+1].Evidence != "user_declared_test" {
					t.Fatalf("events=%+v", ev)
				}
			}
		})
	}
}
func TestSessionRejectsSequenceAndFraming(t *testing.T) {
	for _, name := range []string{"scope", "generation", "session", "id", "gap", "first_start", "digest", "truncated", "oversize", "unknown"} {
		t.Run(name, func(t *testing.T) {
			h := sessionFrame(1, 0, 32000)
			var input, output bytes.Buffer
			if name != "first_start" {
				_ = writeRequest(&input, h, sessionPCM(32000))
				h = sessionFrame(2, 32000, 32000)
			}
			switch name {
			case "scope":
				h.Scope = "fixture"
			case "generation":
				h.Generation = 2
			case "session":
				h.Session = "other"
			case "id":
				h.ID = 1
			case "gap":
				h.Start++
				h.End++
			case "first_start":
				h.Start = 1
				h.End++
			case "digest":
				h.ModelSHA = strings.Repeat("b", 64)
			case "oversize":
				h.End = h.Start + 64001
				h.PCMBytes = 128002
			case "unknown":
				input.Write([]byte{0, 0, 0, 2, '{', '}'})
			}
			if name != "unknown" {
				_ = writeFrame(&input, h, sessionPCM(h.PCMBytes/2))
			}
			if name == "truncated" {
				b := append([]byte(nil), input.Bytes()...)
				input.Reset()
				input.Write(b[:len(b)-1])
			}
			opt := sessionOptions{validate: func(string, string) error { return nil }, extract: func(context.Context, requestHeader, []byte) ([]float32, error) { return []float32{1, 0}, nil }, lifetime: time.Second}
			if runSession(context.Background(), io.NopCloser(&input), &output, "model", strings.Repeat("a", 64), true, opt) == nil {
				t.Fatal("accepted invalid input")
			}
			ev := sessionEvents(t, output.Bytes())
			if ev[len(ev)-1].Event != "closed" {
				t.Fatal(ev)
			}
			if strings.Contains(output.String(), "vector") || strings.Contains(output.String(), "model") {
				t.Fatal("sensitive output")
			}
		})
	}
}
func TestSessionQualityGate(t *testing.T) {
	h := sessionFrame(1, 0, 1000)
	ev, c := testSession(t, []requestHeader{h}, [][]float32{{1, 0}})
	if c != 0 || ev[1].Label != "uncertain" {
		t.Fatal(ev, c)
	}
	var input, output bytes.Buffer
	h = sessionFrame(1, 0, 32000)
	_ = writeRequest(&input, h, make([]byte, 64000))
	opt := sessionOptions{validate: func(string, string) error { return nil }, extract: func(context.Context, requestHeader, []byte) ([]float32, error) {
		t.Fatal("silence extracted")
		return nil, nil
	}, lifetime: time.Second}
	if runSession(context.Background(), io.NopCloser(&input), &output, "model", h.ModelSHA, true, opt) != nil {
		t.Fatal("silence error")
	}
	if sessionEvents(t, output.Bytes())[1].Label != "uncertain" {
		t.Fatal(output.String())
	}
}
func TestSessionInvalidStartupNoReady(t *testing.T) {
	for _, declared := range []bool{false, true} {
		var out bytes.Buffer
		opt := sessionOptions{validate: func(string, string) error { return errModel }, lifetime: time.Second}
		if runSession(context.Background(), io.NopCloser(strings.NewReader("")), &out, "private/path", strings.Repeat("a", 64), declared, opt) == nil || out.Len() != 0 {
			t.Fatal("ready before validation")
		}
	}
}
func TestSessionCancelAndIdleCloseInput(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		r, w := io.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		var out bytes.Buffer
		opt := sessionOptions{validate: func(string, string) error { return nil }, lifetime: 30 * time.Millisecond}
		go func() { done <- runSession(ctx, r, &out, "model", strings.Repeat("a", 64), true, opt) }()
		if cancelEarly {
			cancel()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("blocked reader leaked")
		}
		if _, err := w.Write([]byte{0}); err == nil {
			t.Fatal("owned input not closed")
		}
		_ = w.Close()
		ev := sessionEvents(t, out.Bytes())
		if len(ev) > 0 && ev[len(ev)-1].Event != "closed" {
			t.Fatal(ev)
		}
	}
}
func TestSessionClipLimit(t *testing.T) {
	var hs []requestHeader
	for i := 0; i < 33; i++ {
		hs = append(hs, sessionFrame(uint64(i+1), uint64(i*1000), 1000))
	}
	var in, out bytes.Buffer
	for _, h := range hs {
		_ = writeRequest(&in, h, sessionPCM(1000))
	}
	opt := sessionOptions{validate: func(string, string) error { return nil }, lifetime: time.Second}
	if err := runSession(context.Background(), io.NopCloser(&in), &out, "model", strings.Repeat("a", 64), true, opt); err != nil {
		t.Fatal(err)
	}
	ev := sessionEvents(t, out.Bytes())
	if len(ev) != 34 || ev[len(ev)-1].Event != "closed" {
		t.Fatal(len(ev))
	}
}

func TestSessionClearsTemporaryVectors(t *testing.T) {
	var in, out bytes.Buffer
	for i := 0; i < 2; i++ {
		h := sessionFrame(uint64(i+1), uint64(i*32000), 32000)
		_ = writeRequest(&in, h, sessionPCM(32000))
	}
	var vectors [][]float32
	opt := sessionOptions{validate: func(string, string) error { return nil }, extract: func(context.Context, requestHeader, []byte) ([]float32, error) {
		v := []float32{1, 0}
		vectors = append(vectors, v)
		return v, nil
	}, lifetime: time.Second}
	if err := runSession(context.Background(), io.NopCloser(&in), &out, "model", strings.Repeat("a", 64), true, opt); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		for _, x := range v {
			if x != 0 {
				t.Fatal("temporary vector retained")
			}
		}
	}
}

func TestRealSessionBridgeOptIn(t *testing.T) {
	model, sha, wav, binary := os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_WAV"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if model == "" || sha == "" || wav == "" || binary == "" {
		t.Skip("explicit model+digest+synthetic WAV+binary opt-in not set")
	}
	pcm, err := readWAVFileForSession(wav)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(pcm)
	if len(pcm) > sessionMaxSamples*2 {
		pcm = pcm[:sessionMaxSamples*2]
	}
	if qualityReason(pcm) != "qualified" {
		t.Fatal("fixture must be qualified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "session", "--model", model, "--sha256", sha, "--declared-single-speaker")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	decoder := json.NewDecoder(stdout)
	var ready sessionEvent
	if decoder.Decode(&ready) != nil || ready.Event != "ready" {
		t.Fatal("session not ready")
	}
	// Human capture takes longer than the worker idle deadline. The second
	// fresh worker must still accept ID 2 and retain the parent's guest label.
	for i := 0; i < 2; i++ {
		if i == 1 {
			time.Sleep(requestTimeout + 50*time.Millisecond)
		}
		h := sessionFrame(uint64(i+1), uint64(i*len(pcm)/2), len(pcm)/2)
		h.ModelSHA = sha
		if err = writeRequest(stdin, h, pcm); err != nil {
			t.Fatal(err)
		}
		var event sessionEvent
		if decoder.Decode(&event) != nil || event.Event != "observation" || event.ID != h.ID || event.Label != "guest_1" {
			t.Fatalf("bad session result %+v", event)
		}
	}
	_ = stdin.Close()
	var closed sessionEvent
	if decoder.Decode(&closed) != nil || closed.Event != "closed" {
		t.Fatal("missing close")
	}
	if err = cmd.Wait(); err != nil || stderr.Len() != 0 {
		t.Fatal("session failed")
	}
}
func readWAVFileForSession(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readWAV(f)
}

func TestSessionZeroDurationHasExplicitObservationSchema(t *testing.T) {
	for _, n := range []int{1, 32000} {
		var in, out bytes.Buffer
		h := sessionFrame(1, 0, n)
		_ = writeRequest(&in, h, make([]byte, n*2))
		opt := sessionOptions{validate: func(string, string) error { return nil }, lifetime: time.Second}
		if err := runSession(context.Background(), io.NopCloser(&in), &out, "model", h.ModelSHA, true, opt); err != nil {
			t.Fatal(err)
		}
		lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
		var observation map[string]any
		if json.Unmarshal(lines[1], &observation) != nil || len(observation) != 5 || observation["label"] != "uncertain" || observation["duration_ms"] != float64(n*1000/16000) {
			t.Fatal("bad observation schema", string(lines[1]))
		}
	}
}

func TestSessionCLIExplicitDeclaration(t *testing.T) {
	model, sha := modelFixture(t)
	for _, declared := range []bool{false, true} {
		args := []string{"session", "--model", model, "--sha256", sha}
		if declared {
			args = append(args, "--declared-single-speaker")
		}
		var out, errOut bytes.Buffer
		result := runCLIWithInput(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &errOut)
		if declared {
			events := sessionEvents(t, out.Bytes())
			if result != 0 || len(events) != 2 || events[0].Event != "ready" || events[1].Event != "closed" {
				t.Fatal("explicit session failed")
			}
		} else if result != 1 || out.Len() != 0 || errOut.String() != "{\"error\":\"session_failed\"}\n" {
			t.Fatal("missing declaration accepted")
		}
	}
}
