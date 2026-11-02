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
	testRealSessionBridge(t, false)
}

func TestRealRealtimeSessionBridgeOptIn(t *testing.T) {
	testRealSessionBridge(t, true)
}

func testRealSessionBridge(t *testing.T, realtime bool) {
	t.Helper()
	model, sha, wav, binary := os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_WAV"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if model == "" || sha == "" || wav == "" || binary == "" {
		t.Skip("explicit model+digest+synthetic WAV+binary opt-in not set")
	}
	pcm, err := readWAVFileForSession(wav)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(pcm)
	if realtime {
		// Repeat the explicit synthetic fixture in memory to exercise the full
		// eight-second worker path without saving any additional audio.
		full := make([]byte, 128000*2)
		for off := 0; off < len(full); {
			off += copy(full[off:], pcm)
		}
		clear(pcm)
		pcm = full
		defer clear(full)
	} else if len(pcm) > sessionMaxSamples*2 {
		pcm = pcm[:sessionMaxSamples*2]
	}
	if qualityReason(pcm) != "qualified" {
		t.Fatal("fixture must be qualified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"session", "--model", model, "--sha256", sha, "--declared-single-speaker"}
	evidence := "user_declared_test"
	if realtime {
		args = append(args, "--realtime-test")
		evidence = "turn_taking_test"
	}
	cmd := exec.CommandContext(ctx, binary, args...)
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
	if decoder.Decode(&ready) != nil || ready.Event != "ready" || ready.Evidence != evidence {
		t.Fatal("session not ready")
	}
	// Human capture takes longer than the worker idle deadline. The second
	// fresh worker must still accept ID 2 and retain the parent's guest label.
	for i := 0; i < 2; i++ {
		if i == 1 {
			time.Sleep(requestTimeout + 50*time.Millisecond)
		}
		h := sessionFrame(uint64(i+1), uint64(i*len(pcm)/2), len(pcm)/2)
		if realtime {
			h.Scope = "realtime-test"
			h.Start += uint64((i + 1) * 16000)
			h.End = h.Start + uint64(len(pcm)/2)
		}
		h.ModelSHA = sha
		if err = writeRequest(stdin, h, pcm); err != nil {
			t.Fatal(err)
		}
		var event sessionEvent
		if decoder.Decode(&event) != nil || event.Event != "observation" || event.ID != h.ID || event.Label != "guest_1" || event.Evidence != evidence || event.DurationMS != int64(len(pcm)/2*1000/16000) {
			t.Fatalf("bad session result %+v", event)
		}
	}
	_ = stdin.Close()
	var closed sessionEvent
	if decoder.Decode(&closed) != nil || closed.Event != "closed" || closed.Evidence != evidence {
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
	for _, realtime := range []bool{false, true} {
		for _, declared := range []bool{false, true} {
			args := []string{"session", "--model", model, "--sha256", sha}
			if realtime {
				args = append(args, "--realtime-test")
			}
			if declared {
				args = append(args, "--declared-single-speaker")
			}
			var out, errOut bytes.Buffer
			result := runCLIWithInput(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &errOut)
			if declared {
				events := sessionEvents(t, out.Bytes())
				if result != 0 || len(events) != 2 || events[0].Event != "ready" || events[1].Event != "closed" {
					t.Fatalf("explicit session failed realtime=%t stderr=%s", realtime, errOut.String())
				}
			} else if result != 1 || out.Len() != 0 || errOut.String() != "{\"error\":\"session_failed\"}\n" {
				t.Fatalf("missing declaration accepted realtime=%t stderr=%s", realtime, errOut.String())
			}
		}
	}
}

func TestSessionCLIRealtimeCoordinatesAndSchema(t *testing.T) {
	model, sha := modelFixture(t)
	for _, tc := range []struct {
		name       string
		start, gap uint64
		n          int
	}{{"first_nonzero_and_gap", 16000, 48000, 32000}, {"contiguous", 0, 0, 32000}, {"eight_seconds", 32000, 16000, 128000}, {"zero_ms", 1, 1, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			var in, out, errOut bytes.Buffer
			for i := 0; i < 2; i++ {
				h := sessionFrame(uint64(i+1), tc.start+uint64(i)*(uint64(tc.n)+tc.gap), tc.n)
				h.Scope, h.ModelSHA = "realtime-test", sha
				if err := writeRequest(&in, h, make([]byte, h.PCMBytes)); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"session", "--model", model, "--sha256", sha, "--declared-single-speaker", "--realtime-test"}
			if result := runCLIWithInput(context.Background(), args, io.NopCloser(&in), &out, &errOut); result != 0 || errOut.Len() != 0 {
				t.Fatalf("realtime input rejected: status=%d stderr=%s", result, errOut.String())
			}
			events := sessionEvents(t, out.Bytes())
			if len(events) != 4 || events[0].Event != "ready" || events[3].Event != "closed" {
				t.Fatalf("events=%+v", events)
			}
			for i, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
				var fields map[string]any
				if err := json.Unmarshal(line, &fields); err != nil || fields["evidence"] != "turn_taking_test" {
					t.Fatalf("invalid event %s", line)
				}
				if i == 1 || i == 2 {
					if len(fields) != 5 || fields["event"] != "observation" || fields["id"] != float64(i) || fields["label"] != "uncertain" || fields["duration_ms"] != float64(tc.n*1000/16000) {
						t.Fatalf("invalid observation %s", line)
					}
				} else if len(fields) != 2 {
					t.Fatalf("invalid lifecycle schema %s", line)
				}
			}
		})
	}
}

func TestSessionCLIRealtimeRejectsSequenceAndFraming(t *testing.T) {
	model, sha := modelFixture(t)
	for _, name := range []string{"overlap", "descending", "session", "generation", "scope", "id", "oversize", "digest", "unknown", "duplicate", "null", "missing", "trailing", "wrongtype", "truncated"} {
		t.Run(name, func(t *testing.T) {
			var in, out, errOut bytes.Buffer
			h := sessionFrame(1, 16000, 32000)
			h.Scope, h.ModelSHA = "realtime-test", sha
			if err := writeRequest(&in, h, make([]byte, h.PCMBytes)); err != nil {
				t.Fatal(err)
			}
			h = sessionFrame(2, 64000, 32000)
			h.Scope, h.ModelSHA = "realtime-test", sha
			switch name {
			case "overlap":
				h.Start, h.End = 47999, 79999
			case "descending":
				h.Start, h.End = 1, 32001
			case "session":
				h.Session = "other"
			case "generation":
				h.Generation = 2
			case "scope":
				h.Scope = "manual-test"
			case "id":
				h.ID = 1
			case "oversize":
				h.End, h.PCMBytes = h.Start+128001, 256002
			case "digest":
				h.ModelSHA = strings.Repeat("b", 64)
			}
			data, err := json.Marshal(h)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			switch name {
			case "unknown":
				text = strings.Replace(text, `"version":1`, `"version":1,"private":"secret"`, 1)
			case "duplicate":
				text = strings.Replace(text, `"version":1`, `"version":1,"version":1`, 1)
			case "null":
				text = strings.Replace(text, `"start":64000`, `"start":null`, 1)
			case "missing":
				text = strings.Replace(text, `"start":64000,`, "", 1)
			case "trailing":
				text += " {}"
			case "wrongtype":
				text = strings.Replace(text, `"generation":1`, `"generation":"1"`, 1)
			}
			payload := make([]byte, h.PCMBytes)
			if name == "truncated" {
				payload = payload[:len(payload)-1]
			}
			in.Write(rawFrame([]byte(text), payload))
			args := []string{"session", "--model", model, "--sha256", sha, "--declared-single-speaker", "--realtime-test"}
			if result := runCLIWithInput(context.Background(), args, io.NopCloser(&in), &out, &errOut); result != 1 || errOut.String() != "{\"error\":\"session_failed\"}\n" {
				t.Fatalf("invalid input not rejected: status=%d stderr=%s", result, errOut.String())
			}
			events := sessionEvents(t, out.Bytes())
			if len(events) != 3 || events[0].Event != "ready" || events[1].Event != "observation" || events[1].ID != 1 || events[2].Event != "closed" {
				t.Fatalf("unexpected events=%+v", events)
			}
		})
	}
}

func TestSessionRealtimeTrackerRetainsLabelsAcrossSourceGaps(t *testing.T) {
	var in, out bytes.Buffer
	vectors := [][]float32{{1, 0}, {0, 1}, {1, 0}}
	for i := range vectors {
		h := sessionFrame(uint64(i+1), uint64(16000+i*160000), 128000)
		h.Scope = "realtime-test"
		if err := writeRequest(&in, h, sessionPCM(128000)); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	var retainedPCM [][]byte
	opt := sessionOptions{realtime: true, validate: func(string, string) error { return nil }, extract: func(_ context.Context, h requestHeader, pcm []byte) ([]float32, error) {
		if h.Scope != "realtime-test" || len(pcm) != 128000*2 {
			t.Fatal("clip changed before extraction")
		}
		retainedPCM = append(retainedPCM, pcm)
		v := append([]float32(nil), vectors[calls]...)
		calls++
		return v, nil
	}, lifetime: time.Second}
	if err := runSession(context.Background(), io.NopCloser(&in), &out, "model", strings.Repeat("a", 64), true, opt); err != nil {
		t.Fatal(err)
	}
	events := sessionEvents(t, out.Bytes())
	if calls != 3 || len(events) != 5 {
		t.Fatalf("calls=%d events=%+v", calls, events)
	}
	for i, want := range []string{"guest_1", "guest_2", "guest_1"} {
		if events[i+1].Label != want || events[i+1].Evidence != "turn_taking_test" || events[i+1].DurationMS != 8000 {
			t.Fatalf("events=%+v", events)
		}
	}
	for _, pcm := range retainedPCM {
		for _, b := range pcm {
			if b != 0 {
				t.Fatal("temporary PCM retained")
			}
		}
	}
}
