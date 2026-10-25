// Package realtimeaudio preserves the Python Realtime entry's mono PCM and
// libopus behavior. It is separate from the older gateway's VOIP codec defaults.
package realtimeaudio

/*
#cgo pkg-config: opus
#include <opus.h>

static int realtime_encoder_lookahead(OpusEncoder *encoder, int *samples) {
    return opus_encoder_ctl(encoder, OPUS_GET_LOOKAHEAD(samples));
}
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

const maxPacketBytes = 4000

// Codec holds independent persistent encoder and decoder states. All PCM is
// mono signed 16-bit little endian; frame is a sample count, not milliseconds.
// Close may safely race an in-flight method, and is idempotent.
type Codec struct {
	mu      sync.Mutex
	encoder *C.OpusEncoder
	decoder *C.OpusDecoder
	frame   int
}

// New creates the same OPUS_APPLICATION_AUDIO (2049) encoder used by audio.py.
// It changes none of the defaults in internal/audio.
func New(rate, frame int) (*Codec, error) {
	switch rate {
	case 8000, 12000, 16000, 24000, 48000:
	default:
		return nil, errors.New("opus_sample_rate")
	}
	if frame <= 0 || frame > rate*120/1000 {
		return nil, errors.New("opus_frame_size")
	}
	var code C.int
	encoder := C.opus_encoder_create(C.opus_int32(rate), 1, C.OPUS_APPLICATION_AUDIO, &code)
	if encoder == nil || code != C.OPUS_OK {
		return nil, fmt.Errorf("opus_encoder_create: %d", code)
	}
	decoder := C.opus_decoder_create(C.opus_int32(rate), 1, &code)
	if decoder == nil || code != C.OPUS_OK {
		C.opus_encoder_destroy(encoder)
		return nil, fmt.Errorf("opus_decoder_create: %d", code)
	}
	return &Codec{encoder: encoder, decoder: decoder, frame: frame}, nil
}

// Encode consumes exactly one configured frame, preserving encoder state.
func (c *Codec) Encode(pcm []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.encoder == nil {
		return nil, errors.New("closed_opus_codec")
	}
	if len(pcm) != c.frame*2 {
		return nil, errors.New("opus_input_size")
	}
	samples := make([]C.opus_int16, c.frame)
	for i := range samples {
		samples[i] = C.opus_int16(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
	}
	packet := make([]byte, maxPacketBytes)
	n := C.opus_encode(c.encoder, &samples[0], C.int(c.frame), (*C.uchar)(unsafe.Pointer(&packet[0])), maxPacketBytes)
	if n <= 0 {
		return nil, fmt.Errorf("opus_encode: %d", n)
	}
	return packet[:int(n)], nil
}

// Decode accepts 1..4000 bytes and requires exactly the configured frame count.
// A shorter packet must not silently move the 60ms playback/ASR timeline.
func (c *Codec) Decode(packet []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.decoder == nil {
		return nil, errors.New("closed_opus_codec")
	}
	if len(packet) == 0 || len(packet) > maxPacketBytes {
		return nil, errors.New("opus_packet_size")
	}
	samples := make([]C.opus_int16, c.frame)
	n := C.opus_decode(c.decoder, (*C.uchar)(unsafe.Pointer(&packet[0])), C.opus_int32(len(packet)), &samples[0], C.int(c.frame), 0)
	if int(n) != c.frame {
		return nil, fmt.Errorf("opus_frame_size: got %d, expected %d", n, c.frame)
	}
	pcm := make([]byte, c.frame*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
	}
	return pcm, nil
}

// LookaheadSamples reports diagnostics in the configured rate's sample clock.
// The migration's output tail still uses Python server.py's fixed 312 samples.
func (c *Codec) LookaheadSamples() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.encoder == nil {
		return 0, errors.New("closed_opus_codec")
	}
	var samples C.int
	if code := C.realtime_encoder_lookahead(c.encoder, &samples); code != C.OPUS_OK {
		return 0, fmt.Errorf("opus_lookahead: %d", code)
	}
	return int(samples), nil
}

// Close releases both states after any in-flight method finishes.
func (c *Codec) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.encoder != nil {
		C.opus_encoder_destroy(c.encoder)
		c.encoder = nil
	}
	if c.decoder != nil {
		C.opus_decoder_destroy(c.decoder)
		c.decoder = nil
	}
}
