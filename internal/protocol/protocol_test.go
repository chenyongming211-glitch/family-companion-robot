package protocol

import (
	"bytes"
	"testing"
)

func TestAudioEnvelope(t *testing.T) {
	for _, v := range []int{1, 2, 3} {
		p := []byte{0xf8, 0xff, 0xfe}
		data, err := Wrap(v, p, 1234)
		if err != nil {
			t.Fatal(err)
		}
		got, ts, err := Unwrap(v, data)
		if err != nil || !bytes.Equal(got, p) {
			t.Fatalf("v%d: %x %v", v, got, err)
		}
		if v == 2 && ts != 1234 {
			t.Fatalf("timestamp %d", ts)
		}
		if v > 1 {
			data[len(data)-1] = 0
			data = data[:len(data)-1]
			if _, _, err := Unwrap(v, data); err == nil {
				t.Fatal("accepted truncated envelope")
			}
		}
	}
}

func TestEnvelopeRejectsWrongVersionAndType(t *testing.T) {
	p, _ := Wrap(2, []byte{1}, 0)
	p[1] = 3
	if _, _, err := Unwrap(2, p); err == nil {
		t.Fatal("accepted wrong version")
	}
	p, _ = Wrap(3, []byte{1}, 0)
	p[0] = 1
	if _, _, err := Unwrap(3, p); err == nil {
		t.Fatal("accepted non audio type")
	}
	if _, _, err := Unwrap(1, nil); err == nil {
		t.Fatal("accepted empty audio")
	}
}

func TestHelloValidatesDeviceFormat(t *testing.T) {
	good := []byte(`{"type":"hello","version":1,"transport":"websocket","audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60}}`)
	h, err := ParseHello(good, 1)
	if err != nil || h.Version != 1 {
		t.Fatalf("hello: %+v %v", h, err)
	}
	for _, bad := range [][]byte{bytes.Replace(good, []byte("opus"), []byte("pcm"), 1), bytes.Replace(good, []byte("16000"), []byte("44100"), 1), []byte(`{"type":"listen"}`)} {
		if _, err := ParseHello(bad, 1); err == nil {
			t.Fatal("accepted invalid hello")
		}
	}
	if _, err := ParseHello(good, 3); err == nil {
		t.Fatal("accepted mismatched protocol header")
	}
}
