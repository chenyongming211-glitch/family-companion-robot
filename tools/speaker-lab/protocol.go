//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"regexp"
)

const (
	protocolVersion = 1
	sampleRate      = 16000
	maxSamples      = 128000
	maxPCMBytes     = maxSamples * 2
	maxHeaderBytes  = 4096
	maxDimension    = 1024
)

var errProtocol = errors.New("bad_protocol")
var errUnavailable = errors.New("unavailable")
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type requestHeader struct {
	Version    int    `json:"version"`
	ID         uint64 `json:"id"`
	Session    string `json:"session"`
	Scope      string `json:"scope"`
	Generation uint64 `json:"generation"`
	Start      uint64 `json:"start"`
	End        uint64 `json:"end"`
	ModelSHA   string `json:"model_sha256"`
	SampleRate int    `json:"sample_rate"`
	PCMBytes   int    `json:"pcm_bytes"`
}
type responseHeader struct {
	Version     int    `json:"version"`
	Kind        string `json:"kind"`
	ID          uint64 `json:"id"`
	Session     string `json:"session"`
	Scope       string `json:"scope"`
	Generation  uint64 `json:"generation"`
	Start       uint64 `json:"start"`
	End         uint64 `json:"end"`
	ModelSHA    string `json:"model_sha256"`
	Dimension   int    `json:"dimension"`
	VectorBytes int    `json:"vector_bytes"`
	InferenceUS int64  `json:"inference_us"`
	Code        string `json:"code"`
}

// Headers are flat, fully specified JSON objects. Reject duplicate/unknown keys,
// nulls, missing keys, type coercion, and any trailing value before payload allocation.
func strictHeader(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return errProtocol
	}
	fields := map[string]bool{}
	typ := reflect.TypeOf(dst).Elem()
	for i := 0; i < typ.NumField(); i++ {
		fields[typ.Field(i).Tag.Get("json")] = false
	}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return errProtocol
		}
		key, ok := tok.(string)
		if !ok {
			return errProtocol
		}
		seen, known := fields[key]
		if !known || seen {
			return errProtocol
		}
		fields[key] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errProtocol
		}
	}
	if tok, e = d.Token(); e != nil || tok != json.Delim('}') {
		return errProtocol
	}
	if _, e = d.Token(); e != io.EOF {
		return errProtocol
	}
	for _, seen := range fields {
		if !seen {
			return errProtocol
		}
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return errProtocol
	}
	return nil
}
func readHeader(r io.Reader, dst any) error {
	var size [4]byte
	if _, e := io.ReadFull(r, size[:]); e != nil {
		return errProtocol
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > maxHeaderBytes {
		return errProtocol
	}
	b := make([]byte, int(n))
	if _, e := io.ReadFull(r, b); e != nil {
		return errProtocol
	}
	return strictHeader(b, dst)
}
func writeFrame(w io.Writer, h any, payload []byte) error {
	b, e := json.Marshal(h)
	if e != nil || len(b) == 0 || len(b) > maxHeaderBytes {
		return errProtocol
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(b)))
	for _, p := range [][]byte{prefix[:], b, payload} {
		for len(p) > 0 {
			n, e := w.Write(p)
			if e != nil {
				return e
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			p = p[n:]
		}
	}
	return nil
}
func validRequest(h requestHeader, sha string) bool {
	return h.Version == protocolVersion && h.ID > 0 && tokenPattern.MatchString(h.Session) && tokenPattern.MatchString(h.Scope) && h.Generation > 0 && shaPattern.MatchString(h.ModelSHA) && h.ModelSHA == sha && h.SampleRate == sampleRate && h.End > h.Start && h.End-h.Start <= maxSamples && h.PCMBytes > 0 && h.PCMBytes <= maxPCMBytes && h.PCMBytes%2 == 0 && uint64(h.PCMBytes/2) == h.End-h.Start
}
func readRequest(r io.Reader, sha string) (requestHeader, []byte, error) {
	var h requestHeader
	if readHeader(r, &h) != nil || !validRequest(h, sha) {
		return h, nil, errProtocol
	}
	p := make([]byte, h.PCMBytes)
	if _, e := io.ReadFull(r, p); e != nil {
		clear(p)
		return h, nil, errProtocol
	}
	return h, p, nil
}
func writeRequest(w io.Writer, h requestHeader, pcm []byte) error {
	if !validRequest(h, h.ModelSHA) || len(pcm) != h.PCMBytes {
		return errProtocol
	}
	return writeFrame(w, h, pcm)
}
func validVector(v []float32, dim int) bool {
	if dim < 1 || dim > maxDimension || len(v) != dim {
		return false
	}
	sum := float64(0)
	for _, f := range v {
		n := float64(f)
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return false
		}
		sum += n * n
	}
	return sum > 0 && !math.IsInf(sum, 0)
}
func responseFor(r requestHeader, dim int, us int64, code string) responseHeader {
	return responseHeader{Version: 1, Kind: "result", ID: r.ID, Session: r.Session, Scope: r.Scope, Generation: r.Generation, Start: r.Start, End: r.End, ModelSHA: r.ModelSHA, Dimension: dim, VectorBytes: dim * 4, InferenceUS: us, Code: code}
}
func writeResponse(w io.Writer, h responseHeader, v []float32) error {
	if h.Code != "ok" {
		if h.Code != "unavailable" {
			return errProtocol
		}
		h.VectorBytes = 0
		return writeFrame(w, h, nil)
	}
	if !validVector(v, h.Dimension) || h.VectorBytes != len(v)*4 || h.InferenceUS < 0 {
		return errProtocol
	}
	p := make([]byte, len(v)*4)
	defer clear(p)
	for i, f := range v {
		binary.LittleEndian.PutUint32(p[i*4:], math.Float32bits(f))
	}
	return writeFrame(w, h, p)
}
func readResponse(r io.Reader, req requestHeader, dim int) ([]float32, int64, error) {
	var h responseHeader
	if readHeader(r, &h) != nil {
		return nil, 0, errProtocol
	}
	expected := responseFor(req, dim, h.InferenceUS, h.Code)
	if h.Code == "unavailable" {
		expected.VectorBytes = 0
	}
	if h != expected || h.InferenceUS < 0 || dim < 1 || dim > maxDimension {
		return nil, 0, errProtocol
	}
	if h.Code == "unavailable" {
		return nil, 0, errUnavailable
	}
	if h.Code != "ok" || h.VectorBytes != dim*4 {
		return nil, 0, errProtocol
	}
	p := make([]byte, h.VectorBytes)
	defer clear(p)
	if _, e := io.ReadFull(r, p); e != nil {
		return nil, 0, errProtocol
	}
	v := make([]float32, dim)
	if decodeResponseVector(v, p) != nil {
		return nil, 0, errProtocol
	}
	return v, h.InferenceUS, nil
}
func decodeResponseVector(v []float32, p []byte) error {
	if len(p) != len(v)*4 {
		clear(v)
		return errProtocol
	}
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(p[i*4:]))
	}
	if !validVector(v, len(v)) {
		clear(v)
		return errProtocol
	}
	return nil
}
func writeReady(w io.Writer, sha string, dim int) error {
	if !shaPattern.MatchString(sha) || dim < 1 || dim > maxDimension {
		return errProtocol
	}
	return writeFrame(w, responseHeader{Version: 1, Kind: "ready", ModelSHA: sha, Dimension: dim, Code: "ok"}, nil)
}
func readReady(r io.Reader, sha string) (int, error) {
	var h responseHeader
	if readHeader(r, &h) != nil || h.Dimension < 1 || h.Dimension > maxDimension {
		return 0, errProtocol
	}
	if h != (responseHeader{Version: 1, Kind: "ready", ModelSHA: sha, Dimension: h.Dimension, Code: "ok"}) {
		return 0, errProtocol
	}
	return h.Dimension, nil
}

// Global serial sequence only. Sample ordering belongs to each session's Tracker.
type requestSequence struct{ lastID uint64 }

func (s *requestSequence) accept(h requestHeader) bool {
	if h.ID <= s.lastID {
		return false
	}
	s.lastID = h.ID
	return true
}
