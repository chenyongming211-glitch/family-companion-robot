//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"os/exec"
	"reflect"
	"testing"
)

// Keep the actual caller-owned slices handed to Read/Write, not copies. These
// witnesses remain live after the callee returns, making cleanup observable.
type retainingReader struct {
	data     []byte
	retained [][]byte
	terminal error
	chunk    int
}

func (r *retainingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if r.terminal != nil {
			return 0, r.terminal
		}
		return 0, io.EOF
	}
	if r.chunk > 0 && len(p) > r.chunk {
		p = p[:r.chunk]
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	r.retained = append(r.retained, p[:n])
	return n, nil
}

type retainingWriter struct {
	retained [][]byte
	writes   int
	fail     bool
}

func (w *retainingWriter) Write(p []byte) (int, error) {
	w.writes++
	w.retained = append(w.retained, p)
	if w.fail && w.writes == 3 {
		return len(p) / 2, io.ErrClosedPipe
	}
	return len(p), nil
}
func zeroBytes(p []byte) bool {
	for _, b := range p {
		if b != 0 {
			return false
		}
	}
	return true
}

func TestResponsePrivateByteBuffersCleared(t *testing.T) {
	req := sampleRequest()
	h, _ := json.Marshal(responseFor(req, 2, 17, "ok"))
	for _, mode := range []string{"success", "truncated", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			payload := make([]byte, 8)
			binary.LittleEndian.PutUint32(payload, math.Float32bits(1))
			binary.LittleEndian.PutUint32(payload[4:], math.Float32bits(2))
			if mode == "truncated" {
				payload = payload[:7]
			}
			if mode == "invalid" {
				binary.LittleEndian.PutUint32(payload[4:], math.Float32bits(float32(math.NaN())))
			}
			r := &retainingReader{data: rawFrame(h, payload), chunk: 3}
			v, us, e := readResponse(r, req, 2)
			if mode == "success" {
				if e != nil || us != 17 || !reflect.DeepEqual(v, []float32{1, 2}) {
					t.Fatal("owned result changed", v, e)
				}
				defer clear(v)
			} else if e == nil || v != nil {
				t.Fatal("invalid response accepted")
			}
			if len(r.retained) < 3 {
				t.Fatal("response payload not read")
			}
			for _, p := range r.retained[len(r.retained)-3:] {
				if !zeroBytes(p) {
					t.Fatal("private response byte buffer retained vector bytes")
				}
			}
		})
	}
}
func TestResponseSerializationBufferCleared(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write_error"}[fail], func(t *testing.T) {
			w := &retainingWriter{fail: fail}
			v := []float32{1, 2}
			e := writeResponse(w, responseFor(sampleRequest(), 2, 0, "ok"), v)
			if (e != nil) != fail {
				t.Fatal("unexpected write result", e)
			}
			if len(w.retained) != 3 || !zeroBytes(w.retained[2]) {
				t.Fatal("serialized private vector retained")
			}
			if !reflect.DeepEqual(v, []float32{1, 2}) {
				t.Fatal("writer erased caller-owned vector")
			}
		})
	}
}
func TestWAVPrivateReadBuffersCleared(t *testing.T) {
	for _, mode := range []string{"success", "late_parse_error", "read_error", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			b := wavBytes(32000)
			copy(b[44:], sessionPCM(32000))
			expected := append([]byte(nil), b[44:]...)
			if mode == "late_parse_error" {
				b = append(b, []byte("data\x02\x00\x00\x00\x01\x01")...)
				binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
			}
			if mode == "oversize" {
				b = bytes.Repeat([]byte{37}, maxPCMBytes+65537)
			}
			r := &retainingReader{data: b, chunk: 257}
			if mode == "read_error" {
				r.terminal = errors.New("private read error")
			}
			pcm, e := readWAV(r)
			if mode == "success" {
				if e != nil || !bytes.Equal(pcm, expected) {
					t.Fatal("returned owned PCM changed", e)
				}
				defer clear(pcm)
			} else if e == nil || pcm != nil {
				t.Fatal("invalid WAV accepted")
			}
			for i, p := range r.retained {
				if !zeroBytes(p) {
					t.Fatalf("private WAV read buffer%d retained audio", i)
				}
			}
			if !bytes.Equal(expected, b[44:44+len(expected)]) && mode == "success" {
				t.Fatal("caller-owned input changed")
			}
		})
	}
}

// The decoder finishes successfully and hands a real decoded vector to the
// request owner, then cancellation wins before that owner can return it.
func TestWorkerCancellationAfterCompletedResponseClearsVector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, server := net.Pipe()
	defer server.Close()
	cmd := exec.Command("/bin/sleep", "10")
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	w := &workerProcess{conn: client, sha: testSHA, dimension: 2, cmd: cmd, done: make(chan struct{})}
	defer w.Close()
	go func() { _ = cmd.Wait(); close(w.done) }()
	req := sampleRequest()
	done := make(chan error, 1)
	go func() {
		h, pcm, e := readRequest(server, testSHA)
		clear(pcm)
		if e == nil {
			e = writeResponse(server, responseFor(h, 2, 9, "ok"), []float32{1, 2})
		}
		done <- e
	}()
	var witness []float32
	decode := func(r io.Reader, h requestHeader, dim int) ([]float32, int64, error) {
		v, us, e := readResponse(r, h, dim)
		witness = v
		cancel()
		return v, us, e
	}
	v, _, e := w.requestWithDecoder(ctx, req, make([]byte, req.PCMBytes), decode)
	if e == nil || v != nil {
		t.Fatal("cancelled response transferred ownership")
	}
	if len(witness) != 2 || witness[0] != 0 || witness[1] != 0 {
		t.Fatal("cancelled decoded vector retained", witness)
	}
	<-done
	if w.cmd.ProcessState == nil {
		t.Fatal("cancelled request child not reaped")
	}
}
func TestInvalidDecodedVectorCleared(t *testing.T) {
	p := make([]byte, 8)
	binary.LittleEndian.PutUint32(p, math.Float32bits(1))
	binary.LittleEndian.PutUint32(p[4:], math.Float32bits(float32(math.NaN())))
	v := make([]float32, 2)
	if decodeResponseVector(v, p) == nil {
		t.Fatal("invalid vector accepted")
	}
	if v[0] != 0 || v[1] != 0 {
		t.Fatal("invalid decoded vector retained", v)
	}
}

func TestRequestPartialPCMBufferCleared(t *testing.T) {
	h, _ := json.Marshal(sampleRequest())
	r := &retainingReader{data: rawFrame(h, bytes.Repeat([]byte{37}, 600)), chunk: 257}
	_, pcm, e := readRequest(r, testSHA)
	if e == nil || pcm != nil {
		t.Fatal("partial request accepted")
	}
	if len(r.retained) < 3 {
		t.Fatal("partial request was not read")
	}
	for _, p := range r.retained[len(r.retained)-3:] {
		if !zeroBytes(p) {
			t.Fatal("partial private request retained PCM")
		}
	}
}
