package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestDeviceEncoderAndWAV(t *testing.T) {
	enc, err := NewEncoderAtRate(16000)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	packet, err := enc.Encode(make([]byte, 1920))
	if err != nil || len(packet) == 0 {
		t.Fatal(err)
	}
	var b bytes.Buffer
	pcm := make([]byte, 1920)
	if err := WriteWAV(&b, pcm); err != nil {
		t.Fatal(err)
	}
	got, err := ReadWAV(bytes.NewReader(b.Bytes()))
	if err != nil || !bytes.Equal(got, pcm) {
		t.Fatalf("WAV roundtrip: %v", err)
	}
	malformed := append([]byte(nil), b.Bytes()...)
	malformed[24] = 0
	if _, err := ReadWAV(bytes.NewReader(malformed)); err == nil {
		t.Fatal("accepted invalid sample rate")
	}
}

func TestRealOpusRoundtrip(t *testing.T) {
	enc, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	pcm := make([]byte, 2880)
	for i := 0; i < len(pcm)/2; i++ {
		v := int16(9000 * math.Sin(2*math.Pi*440*float64(i)/24000))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(v))
	}
	packet, err := enc.Encode(pcm)
	if err != nil || len(packet) == 0 {
		t.Fatalf("encode %v", err)
	}
	got, err := dec.Decode(packet)
	if err != nil || len(got) != 1920 {
		t.Fatalf("decode bytes=%d err=%v", len(got), err)
	}
	var peak int16
	for i := 0; i < len(got); i += 2 {
		v := int16(binary.LittleEndian.Uint16(got[i:]))
		if v > peak {
			peak = v
		}
	}
	if peak < 1000 {
		t.Fatalf("lost waveform peak=%d", peak)
	}
	if _, err := enc.Encode(pcm[:100]); err == nil {
		t.Fatal("accepted invalid frame duration")
	}
}

func TestPCMBufferChunkingPaddingAndLimit(t *testing.T) {
	b := NewBuffer(8)
	if err := b.Append([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if got := b.Pop(4, false); got != nil {
		t.Fatal("emitted partial frame")
	}
	if err := b.Append([]byte{4, 5}); err != nil {
		t.Fatal(err)
	}
	got := b.Pop(4, false)
	if len(got) != 4 || got[3] != 4 {
		t.Fatalf("frame %v", got)
	}
	got = b.Pop(4, true)
	if len(got) != 4 || got[0] != 5 || got[1] != 0 {
		t.Fatalf("padding %v", got)
	}
	if err := b.Append(make([]byte, 9)); err == nil {
		t.Fatal("unbounded buffer")
	}
	b.Append([]byte{7, 8})
	b.Clear()
	if b.Len() != 0 || b.Pop(4, true) != nil {
		t.Fatal("clear retained old audio")
	}
}
