package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func wavBytes(samples int) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+samples*2))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	for _, v := range []uint16{1, 1} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	binary.Write(&b, binary.LittleEndian, uint32(16000))
	binary.Write(&b, binary.LittleEndian, uint32(32000))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(samples*2))
	b.Write(make([]byte, samples*2))
	return b.Bytes()
}
func TestWAVBoundedPCM(t *testing.T) {
	good := wavBytes(16000)
	got, e := readWAV(bytes.NewReader(good))
	if e != nil || len(got) != 32000 {
		t.Fatal(e)
	}
	for n, edit := range map[string]func([]byte) []byte{"truncated": func(b []byte) []byte { return b[:len(b)-1] }, "rate": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[24:28], 8000); return b }, "channels": func(b []byte) []byte { b[22] = 2; return b }, "bits": func(b []byte) []byte { b[34] = 8; return b }, "float": func(b []byte) []byte { b[20] = 3; return b }, "oversize": func(b []byte) []byte { return wavBytes(128001) }, "riffsize": func(b []byte) []byte { b[4] = 255; return b }, "trailing": func(b []byte) []byte { return append(b, 0) }, "odd": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[40:44], 31999); return b }} {
		t.Run(n, func(t *testing.T) {
			if _, e := readWAV(bytes.NewReader(edit(append([]byte{}, good...)))); e == nil {
				t.Fatal("accepted bad WAV")
			}
		})
	}
}
