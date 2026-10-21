package audio

import (
	"encoding/binary"
	"errors"
	"io"
)

// ReadWAV accepts bounded mono 16kHz signed 16-bit PCM test recordings.
func ReadWAV(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("not a PCM WAV")
	}
	validFormat := false
	var pcm []byte
	for pos := 12; pos+8 <= len(data); {
		kind := string(data[pos : pos+4])
		n := int(binary.LittleEndian.Uint32(data[pos+4:]))
		pos += 8
		if n < 0 || n > len(data)-pos {
			return nil, errors.New("truncated WAV chunk")
		}
		chunk := data[pos : pos+n]
		if kind == "fmt " {
			if len(chunk) < 16 || binary.LittleEndian.Uint16(chunk) != 1 || binary.LittleEndian.Uint16(chunk[2:]) != 1 || binary.LittleEndian.Uint32(chunk[4:]) != 16000 || binary.LittleEndian.Uint16(chunk[14:]) != 16 {
				return nil, errors.New("expected mono 16kHz 16-bit PCM WAV")
			}
			validFormat = true
		}
		if kind == "data" {
			pcm = chunk
		}
		pos += n + (n % 2)
	}
	if !validFormat || len(pcm) == 0 || len(pcm) > 16000*2*30 || len(pcm)%2 != 0 {
		return nil, errors.New("WAV audio must contain 0-30 seconds of mono PCM")
	}
	return pcm, nil
}

// WriteWAV stores decoded smoke-test audio at the decoder's 16kHz rate.
func WriteWAV(w io.Writer, pcm []byte) error {
	if len(pcm)%2 != 0 {
		return errors.New("odd PCM length")
	}
	b := make([]byte, 44)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(pcm)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(pcm)))
	if _, err := w.Write(b); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}
