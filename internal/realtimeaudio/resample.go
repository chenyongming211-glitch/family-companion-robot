package realtimeaudio

import (
	"encoding/binary"
	"errors"
)

// Resampler implements audioop.ratecv(pcm, 2, 1, 24000, 16000, state)'s default
// interpolation. Keep one instance for each output stream; it is single-owner.
// It retains phase and adjacent samples across packet boundaries, and never
// pads or flushes samples at the end of a stream. Its zero value is ready to use.
type Resampler struct {
	initialized bool
	phase       int64
	previous    int64
	current     int64
}

func NewResampler() *Resampler { return &Resampler{} }

// Process converts mono s16le PCM from 24kHz to 16kHz. A partial sample is an
// error and leaves the stream state intact; an empty packet is a no-op.
func (r *Resampler) Process(pcm []byte) ([]byte, error) {
	if len(pcm)%2 != 0 {
		return nil, errors.New("ratecv_partial_sample")
	}
	if len(pcm) == 0 {
		return []byte{}, nil
	}
	if !r.initialized {
		r.phase = -2
		r.initialized = true
	}
	output := make([]byte, 0, (len(pcm)/3+1)*2)
	for i := 0; i < len(pcm); i += 2 {
		r.previous = r.current
		// audioop promotes s16 samples to signed 32-bit left-aligned values.
		// int64 intermediates preserve C's integer division followed by the
		// arithmetic right shift, including negative half-sample rounding.
		r.current = int64(int16(binary.LittleEndian.Uint16(pcm[i:]))) << 16
		r.phase += 2
		for r.phase >= 0 {
			mixed := (r.previous*r.phase + r.current*(2-r.phase)) / 2
			sample := uint16(int16(mixed >> 16))
			output = append(output, byte(sample), byte(sample>>8))
			r.phase -= 3
		}
	}
	return output, nil
}
