package realtimeaudio

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"testing"
)

func TestResamplerExactPhaseAndNegativeRounding(t *testing.T) {
	input := make([]byte, 12)
	for i, sample := range []int16{1, -3, -2, 7, 32767, -32768} {
		binary.LittleEndian.PutUint16(input[2*i:], uint16(sample))
	}
	converted, err := NewResampler().Process(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []int16{1, -3, 7, -1}
	if len(converted) != len(want)*2 {
		t.Fatalf("got %d output bytes", len(converted))
	}
	for i, sample := range want {
		if got := int16(binary.LittleEndian.Uint16(converted[2*i:])); got != sample {
			t.Fatalf("output %d = %d, want %d", i, got, sample)
		}
	}
}

func TestResamplerMatchesPythonAcrossPacketBoundaries(t *testing.T) {
	cases := map[string][]byte{
		"empty":   nil,
		"silence": make([]byte, 1440*2*4),
		"numeric": numericPCM(1440*7+13, 31),
		"single":  numericPCM(1, 7),
	}
	impulse := make([]byte, 4096)
	binary.LittleEndian.PutUint16(impulse[0:], 32767)
	binary.LittleEndian.PutUint16(impulse[1440*2:], 32768)
	cases["impulse"] = impulse
	sine := make([]byte, 24000*2)
	for i := range len(sine) / 2 {
		value := int16(math.Round(27000 * math.Sin(2*math.Pi*997*float64(i)/24000)))
		binary.LittleEndian.PutUint16(sine[i*2:], uint16(value))
	}
	cases["sine"] = sine
	for name, input := range cases {
		for _, sizes := range [][]int{{1440}, {1}, {0, 2, 7, 1439, 3, 0, 1440, 960}} {
			t.Run(name+"/"+strconv.Itoa(sizes[len(sizes)-1]), func(t *testing.T) {
				chunks := chunkSamples(input, sizes)
				want := pythonReference(t, referenceRequest{Operation: "resample", Chunks: chunks})
				r := NewResampler()
				var joined []byte
				for i, chunk := range chunks {
					got, err := r.Process(chunk)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want.Converted[i]) {
						t.Fatalf("chunk %d differs: got %d bytes, Python %d", i, len(got), len(want.Converted[i]))
					}
					joined = append(joined, got...)
				}
				whole, err := NewResampler().Process(input)
				if err != nil || !bytes.Equal(joined, whole) {
					t.Fatalf("chunking changed output: %v", err)
				}
				wantSamples := 0
				if samples := len(input) / 2; samples > 0 {
					wantSamples = 1 + (samples-1)*2/3
				}
				if len(joined) != wantSamples*2 {
					t.Fatalf("got %d samples, want %d", len(joined)/2, wantSamples)
				}
			})
		}
	}
}

func TestResamplerZeroValue(t *testing.T) {
	var zero Resampler
	input := numericPCM(1440, 0)
	got, err := zero.Process(input)
	if err != nil {
		t.Fatal(err)
	}
	want, err := NewResampler().Process(input)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("zero value differs: %v", err)
	}
}

func TestResamplerInvalidInputPreservesState(t *testing.T) {
	r := NewResampler()
	first, err := r.Process(numericPCM(5, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Process([]byte{1, 2, 3}); err == nil {
		t.Fatal("accepted partial s16 sample")
	}
	if empty, err := r.Process(nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty input = %v, %v", empty, err)
	}
	second, err := r.Process(numericPCM(7, 5))
	if err != nil {
		t.Fatal(err)
	}
	joinedInput := append(numericPCM(5, 0), numericPCM(7, 5)...)
	want, err := NewResampler().Process(joinedInput)
	if err != nil || !bytes.Equal(append(first, second...), want) {
		t.Fatalf("invalid/empty input changed phase: %v", err)
	}
}

func chunkSamples(input []byte, sizes []int) [][]byte {
	chunks := [][]byte{nil}
	for pos, step := 0, 0; pos < len(input); step++ {
		size := sizes[step%len(sizes)] * 2
		if size > len(input)-pos {
			size = len(input) - pos
		}
		chunks = append(chunks, input[pos:pos+size])
		pos += size
	}
	return append(chunks, nil)
}
