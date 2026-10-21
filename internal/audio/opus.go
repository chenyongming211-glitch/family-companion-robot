// Package audio uses the system libopus implementation, not a custom codec.
package audio

/*
#cgo pkg-config: opus
#include <opus.h>
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"
)

const FrameBytes = 24000 * 60 / 1000 * 2

type Encoder struct {
	state      *C.OpusEncoder
	frameBytes int
}
type Decoder struct{ state *C.OpusDecoder }

func NewEncoder() (*Encoder, error) {
	return NewEncoderAtRate(24000)
}
func NewEncoderAtRate(rate int) (*Encoder, error) {
	if rate != 16000 && rate != 24000 {
		return nil, errors.New("unsupported encoder sample rate")
	}
	var code C.int
	p := C.opus_encoder_create(C.opus_int32(rate), 1, C.OPUS_APPLICATION_VOIP, &code)
	if code != 0 {
		return nil, fmt.Errorf("Opus encoder: %d", code)
	}
	return &Encoder{state: p, frameBytes: rate * 60 / 1000 * 2}, nil
}
func NewDecoder() (*Decoder, error) {
	var code C.int
	p := C.opus_decoder_create(16000, 1, &code)
	if code != 0 {
		return nil, fmt.Errorf("Opus decoder: %d", code)
	}
	return &Decoder{p}, nil
}
func (e *Encoder) Close() {
	if e.state != nil {
		C.opus_encoder_destroy(e.state)
		e.state = nil
	}
}
func (d *Decoder) Close() {
	if d.state != nil {
		C.opus_decoder_destroy(d.state)
		d.state = nil
	}
}
func (e *Encoder) Encode(pcm []byte) ([]byte, error) {
	if e.state == nil || len(pcm) != e.frameBytes {
		return nil, errors.New("expected one 60ms mono PCM frame at the configured rate")
	}
	samples := make([]int16, len(pcm)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	out := make([]byte, 4096)
	n := C.opus_encode(e.state, (*C.opus_int16)(unsafe.Pointer(&samples[0])), C.int(len(samples)), (*C.uchar)(unsafe.Pointer(&out[0])), C.opus_int32(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("Opus encode: %d", n)
	}
	return out[:int(n)], nil
}
func (d *Decoder) Decode(packet []byte) ([]byte, error) {
	if d.state == nil || len(packet) == 0 || len(packet) > 4096 {
		return nil, errors.New("invalid Opus input")
	}
	samples := make([]int16, 1920)
	n := C.opus_decode(d.state, (*C.uchar)(unsafe.Pointer(&packet[0])), C.opus_int32(len(packet)), (*C.opus_int16)(unsafe.Pointer(&samples[0])), C.int(len(samples)), 0)
	if n < 0 {
		return nil, fmt.Errorf("Opus decode: %d", n)
	}
	out := make([]byte, int(n)*2)
	for i, v := range samples[:int(n)] {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out, nil
}
