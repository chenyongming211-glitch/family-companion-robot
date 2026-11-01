//go:build linux

package main

import (
	"encoding/binary"
	"io"
)

// readWAV permits only a bounded RIFF container with one PCM16/mono/16k fmt and
// one data chunk. Metadata/padding are bounded by 64 KiB in addition to PCM.
func readWAV(r io.Reader) ([]byte, error) {
	const maxWAVBytes = maxPCMBytes + 65536
	b, e := io.ReadAll(io.LimitReader(r, maxWAVBytes+1))
	if e != nil || len(b) < 44 || len(b) > maxWAVBytes {
		return nil, errProtocol
	}
	if string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(b[4:8]))+8 != uint64(len(b)) {
		return nil, errProtocol
	}
	var pcm []byte
	fmtSeen := false
	dataSeen := false
	for pos := 12; pos < len(b); {
		if len(b)-pos < 8 {
			return nil, errProtocol
		}
		kind := string(b[pos : pos+4])
		n := uint64(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		pos += 8
		if n > uint64(len(b)-pos) {
			return nil, errProtocol
		}
		chunk := b[pos : pos+int(n)]
		switch kind {
		case "fmt ":
			if fmtSeen || dataSeen || len(chunk) != 16 {
				return nil, errProtocol
			}
			fmtSeen = true
			if binary.LittleEndian.Uint16(chunk) != 1 || binary.LittleEndian.Uint16(chunk[2:]) != 1 || binary.LittleEndian.Uint32(chunk[4:]) != sampleRate || binary.LittleEndian.Uint32(chunk[8:]) != sampleRate*2 || binary.LittleEndian.Uint16(chunk[12:]) != 2 || binary.LittleEndian.Uint16(chunk[14:]) != 16 {
				return nil, errProtocol
			}
		case "data":
			if !fmtSeen || dataSeen || n == 0 || n > maxPCMBytes || n%2 != 0 {
				return nil, errProtocol
			}
			dataSeen = true
			pcm = chunk
		case "RIFF", "RIFX", "RF64":
			return nil, errProtocol
		}
		pos += int(n)
		if n%2 != 0 {
			if pos >= len(b) {
				return nil, errProtocol
			}
			pos++
		}
	}
	if !fmtSeen || !dataSeen {
		return nil, errProtocol
	}
	return append([]byte(nil), pcm...), nil
}
