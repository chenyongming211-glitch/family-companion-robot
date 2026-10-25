package realtimeaudio

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

type referenceRequest struct {
	Operation string   `json:"operation"`
	Rate      int      `json:"rate,omitempty"`
	Frame     int      `json:"frame,omitempty"`
	Chunks    [][]byte `json:"chunks"`
}

type referenceResult struct {
	Packets   [][]byte `json:"packets"`
	Decoded   [][]byte `json:"decoded"`
	Converted [][]byte `json:"converted"`
	Lookahead int      `json:"lookahead"`
	Version   string   `json:"version"`
}

func pythonReference(t *testing.T, request referenceRequest) referenceResult {
	t.Helper()
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "-B", "-W", "ignore::DeprecationWarning", filepath.Join("testdata", "reference.py"))
	command.Stdin = bytes.NewReader(input)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Python reference: %v\n%s", err, output)
	}
	var result referenceResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Python reference JSON: %v\n%s", err, output)
	}
	return result
}

// Synthetic values deliberately include negative odd sums, extrema and impulses.
// No recorded or generated speech is used in these numeric fixtures.
func numericPCM(samples, offset int) []byte {
	pcm := make([]byte, samples*2)
	for i := range samples {
		x := int16(((i+offset)*7919+12345)%65536 - 32768)
		if i%47 == 0 {
			x = -32768
		}
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(x))
	}
	return pcm
}

func TestCodecMatchesPythonPersistentState(t *testing.T) {
	for _, config := range [][2]int{{16000, 320}, {24000, 1440}} {
		t.Run(strconv.Itoa(config[0]), func(t *testing.T) {
			chunks := [][]byte{make([]byte, config[1]*2), numericPCM(config[1], 0), numericPCM(config[1], 932), make([]byte, config[1]*2)}
			want := pythonReference(t, referenceRequest{Operation: "opus", Rate: config[0], Frame: config[1], Chunks: chunks})
			codec, err := New(config[0], config[1])
			if err != nil {
				t.Fatal(err)
			}
			defer codec.Close()
			lookahead, err := codec.LookaheadSamples()
			if err != nil || lookahead != want.Lookahead {
				t.Fatalf("lookahead = %d, %v; Python = %d", lookahead, err, want.Lookahead)
			}
			t.Logf("libopus=%s rate=%d frame=%d application=2049 lookahead=%d", want.Version, config[0], config[1], lookahead)
			for i, chunk := range chunks {
				packet, err := codec.Encode(chunk)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(packet, want.Packets[i]) {
					t.Fatalf("packet %d differs from Python: Go=%d bytes Python=%d", i, len(packet), len(want.Packets[i]))
				}
				decoded, err := codec.Decode(packet)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(decoded, want.Decoded[i]) {
					t.Fatalf("decoded PCM %d differs from Python", i)
				}
			}
		})
	}
}

func TestCodecRejectsWrongFrameAndPacketSizes(t *testing.T) {
	codec, err := New(24000, 1440)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	for _, size := range []int{0, 1, 2878, 2881, 5760} {
		if _, err := codec.Encode(make([]byte, size)); err == nil {
			t.Fatalf("accepted %d PCM bytes", size)
		}
	}
	for _, packet := range [][]byte{nil, make([]byte, 4001), {0xff}} {
		if _, err := codec.Decode(packet); err == nil {
			t.Fatalf("accepted invalid packet %v", packet)
		}
	}
	short, err := New(24000, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer short.Close()
	packet, err := short.Encode(make([]byte, 960))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(packet); err == nil {
		t.Fatal("accepted a 20ms packet for a 60ms decoder")
	}
	packet, err = codec.Encode(make([]byte, 2880))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := short.Decode(packet); err == nil {
		t.Fatal("accepted a 60ms packet for a 20ms decoder")
	}
}

func TestCodecCloseAndInvalidConstruction(t *testing.T) {
	for _, config := range [][2]int{{0, 320}, {22050, 320}, {16000, 0}, {16000, -1}} {
		if codec, err := New(config[0], config[1]); err == nil {
			codec.Close()
			t.Fatalf("accepted invalid codec config %v", config)
		}
	}
	codec, err := New(16000, 320)
	if err != nil {
		t.Fatal(err)
	}
	codec.Close()
	codec.Close()
	if _, err := codec.Encode(make([]byte, 640)); err == nil {
		t.Fatal("encoded with closed codec")
	}
	if _, err := codec.Decode([]byte{1}); err == nil {
		t.Fatal("decoded with closed codec")
	}
	if _, err := codec.LookaheadSamples(); err == nil {
		t.Fatal("queried closed codec")
	}
}

func TestPythonTailPaddingTimeline(t *testing.T) {
	for _, total := range []int{0, 1, 1128, 1129, 1439, 1440, 1441, 2800, 2880, 5000} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			input := numericPCM(total, 17)
			want := pythonReference(t, referenceRequest{Operation: "tail", Rate: 24000, Frame: 1440, Chunks: [][]byte{input}})
			codec, err := New(24000, 1440)
			if err != nil {
				t.Fatal(err)
			}
			defer codec.Close()
			// The session preserves Python's 312-sample constant even though
			// libopus reports its lookahead in the configured sample rate.
			frames := (total + 312 + 1439) / 1440
			if len(want.Packets) != frames {
				t.Fatalf("Python emitted %d frames, expected %d", len(want.Packets), frames)
			}
			for i := range frames {
				chunk := make([]byte, 2880)
				if start := i * 2880; start < len(input) {
					copy(chunk, input[start:])
				}
				packet, err := codec.Encode(chunk)
				if err != nil || !bytes.Equal(packet, want.Packets[i]) {
					t.Fatalf("tail packet %d differs: %v", i, err)
				}
				decoded, err := codec.Decode(packet)
				if err != nil || len(decoded) != 2880 || !bytes.Equal(decoded, want.Decoded[i]) {
					t.Fatalf("tail decoded frame %d differs: %v", i, err)
				}
			}
			t.Logf("source=%d padding=%d packets=%d decoded_samples=%d", total, frames*1440-total, frames, frames*1440)
		})
	}
}

func TestCodecCloseDuringUse(t *testing.T) {
	codec, err := New(16000, 320)
	if err != nil {
		t.Fatal(err)
	}
	pcm := numericPCM(320, 0)
	packet, err := codec.Encode(pcm)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		<-start
		for range 50 {
			if _, err := codec.Encode(pcm); err != nil {
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 50 {
			if _, err := codec.Decode(packet); err != nil {
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		codec.Close()
	}()
	close(start)
	workers.Wait()
	codec.Close()
}
