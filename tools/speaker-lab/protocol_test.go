package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

const testSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func sampleRequest() requestHeader {
	return requestHeader{Version: 1, ID: 1, Session: "session1", Scope: "scope1", Generation: 1, Start: 0, End: 16000, ModelSHA: testSHA, SampleRate: 16000, PCMBytes: 32000}
}
func rawFrame(h []byte, payload []byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(len(h)))
	b.Write(h)
	b.Write(payload)
	return b.Bytes()
}
func TestRequestRoundTrip(t *testing.T) {
	h := sampleRequest()
	pcm := make([]byte, h.PCMBytes)
	pcm[10] = 37
	var b bytes.Buffer
	if err := writeRequest(&b, h, pcm); err != nil {
		t.Fatal(err)
	}
	got, p, err := readRequest(&b, testSHA)
	if err != nil || got != h || !bytes.Equal(p, pcm) {
		t.Fatal("round trip failed", err)
	}
}
func TestRejectMalformedRequestBeforePayload(t *testing.T) {
	good, _ := json.Marshal(sampleRequest())
	cases := map[string][]byte{
		"unknown":   []byte(strings.Replace(string(good), `"version":1`, `"version":1,"path":"secret"`, 1)),
		"duplicate": []byte(strings.Replace(string(good), `"version":1`, `"version":1,"version":1`, 1)),
		"trailing":  append(append([]byte{}, good...), []byte(` {}`)...),
		"null":      []byte(strings.Replace(string(good), `"start":0`, `"start":null`, 1)),
		"wrongtype": []byte(strings.Replace(string(good), `"generation":1`, `"generation":"1"`, 1)),
		"missing":   []byte(strings.Replace(string(good), `"start":0,`, ``, 1)),
	}
	for n, h := range cases {
		t.Run(n, func(t *testing.T) {
			if _, _, err := readRequest(bytes.NewReader(rawFrame(h, make([]byte, 32000))), testSHA); err == nil {
				t.Fatal("accepted malformed header")
			}
		})
	}
	for n, change := range map[string]func(*requestHeader){"version": func(h *requestHeader) { h.Version = 2 }, "rate": func(h *requestHeader) { h.SampleRate = 8000 }, "odd": func(h *requestHeader) { h.PCMBytes = 31999 }, "oversize": func(h *requestHeader) { h.End = 128001; h.PCMBytes = 256002 }, "digest": func(h *requestHeader) { h.ModelSHA = strings.Repeat("b", 64) }, "underflow": func(h *requestHeader) { h.Start = 17000 }, "id": func(h *requestHeader) { h.ID = 0 }} {
		t.Run(n, func(t *testing.T) {
			h := sampleRequest()
			change(&h)
			j, _ := json.Marshal(h)
			if _, _, err := readRequest(bytes.NewReader(rawFrame(j, make([]byte, 256002))), testSHA); err == nil {
				t.Fatal("accepted invalid")
			}
		})
	}
}
func TestTruncatedAndOversizedFrames(t *testing.T) {
	h, _ := json.Marshal(sampleRequest())
	for _, b := range [][]byte{nil, {0, 0}, {0, 0, 0, 0}, {0, 0, 16, 1}, rawFrame(h, nil), rawFrame(h[:8], nil)} {
		if _, _, err := readRequest(bytes.NewReader(b), testSHA); err == nil {
			t.Fatal("accepted frame")
		}
	}
}
func TestResponseMatchesAndRejectsInvalidVectors(t *testing.T) {
	req := sampleRequest()
	for n, v := range map[string][]float32{"good": {1, 2}, "zero": {0, 0}, "nan": {1, float32(math.NaN())}, "inf": {1, float32(math.Inf(1))}, "oversize": make([]float32, 1025)} {
		t.Run(n, func(t *testing.T) {
			var b bytes.Buffer
			err := writeResponse(&b, responseFor(req, 2, 10, "ok"), v)
			if n == "good" {
				if err != nil {
					t.Fatal(err)
				}
				got, _, err := readResponse(&b, req, 2)
				if err != nil || len(got) != 2 {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid vector")
			}
		})
	}
	for n, change := range map[string]func(*responseHeader){"id": func(h *responseHeader) { h.ID++ }, "scope": func(h *responseHeader) { h.Scope = "other" }, "session": func(h *responseHeader) { h.Session = "other" }, "generation": func(h *responseHeader) { h.Generation++ }, "span": func(h *responseHeader) { h.End++ }, "sha": func(h *responseHeader) { h.ModelSHA = strings.Repeat("b", 64) }, "dimension": func(h *responseHeader) { h.Dimension = 3; h.VectorBytes = 12 }} {
		t.Run(n, func(t *testing.T) {
			h := responseFor(req, 2, 1, "ok")
			change(&h)
			j, _ := json.Marshal(h)
			if _, _, err := readResponse(bytes.NewReader(rawFrame(j, []byte{0, 0, 128, 63, 0, 0, 128, 63, 0, 0, 128, 63})), req, 2); err == nil {
				t.Fatal("accepted unmatched response")
			}
		})
	}
}
func TestSequenceRejectsRepeatedIDsAllowsSequentialSessions(t *testing.T) {
	var s requestSequence
	h := sampleRequest()
	if !s.accept(h) {
		t.Fatal("first")
	}
	if s.accept(h) {
		t.Fatal("repeat")
	}
	h.ID++
	h.Session = "session2"
	h.Scope = "scope2"
	h.Generation++
	if !s.accept(h) {
		t.Fatal("cross-session serial request")
	}
	h.ID--
	if s.accept(h) {
		t.Fatal("late ID")
	}
}

func TestRawResponseRejectsNonfiniteZeroAndTruncation(t *testing.T) {
	req := sampleRequest()
	h := responseFor(req, 2, 1, "ok")
	j, _ := json.Marshal(h)
	for n, v := range map[string][]float32{"zero": {0, 0}, "nan": {1, float32(math.NaN())}, "inf": {1, float32(math.Inf(1))}} {
		t.Run(n, func(t *testing.T) {
			p := make([]byte, 8)
			for i, f := range v {
				binary.LittleEndian.PutUint32(p[i*4:], math.Float32bits(f))
			}
			if _, _, e := readResponse(bytes.NewReader(rawFrame(j, p)), req, 2); e == nil {
				t.Fatal("accepted invalid incoming vector")
			}
		})
	}
	if _, _, e := readResponse(bytes.NewReader(rawFrame(j, make([]byte, 7))), req, 2); e == nil {
		t.Fatal("accepted truncated vector")
	}
}
func TestReadyAndResponseHeaderStrictness(t *testing.T) {
	var b bytes.Buffer
	if e := writeReady(&b, testSHA, 192); e != nil {
		t.Fatal(e)
	}
	if dim, e := readReady(&b, testSHA); e != nil || dim != 192 {
		t.Fatal(e)
	}
	ready := responseHeader{Version: 1, Kind: "ready", ModelSHA: testSHA, Dimension: 192, Code: "ok"}
	j, _ := json.Marshal(ready)
	for _, bad := range [][]byte{[]byte(strings.Replace(string(j), `"dimension":192`, `"dimension":1025`, 1)), []byte(strings.Replace(string(j), `"version":1`, `"version":1,"version":1`, 1)), []byte(strings.Replace(string(j), `"id":0`, `"id":null`, 1)), append(append([]byte{}, j...), []byte(`{}`)...)} {
		if _, e := readReady(bytes.NewReader(rawFrame(bad, nil)), testSHA); e == nil {
			t.Fatal("invalid ready accepted")
		}
	}
}
