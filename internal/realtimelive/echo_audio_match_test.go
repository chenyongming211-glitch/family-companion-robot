package realtimelive

import (
	"encoding/binary"
	"math"
	"testing"
)

// Model two utterances with the same syllable timing and different waveforms.
// These are deterministic test signals, not recorded family speech.
func echoTestUtterance(samples int, speaker int) []int16 {
	out := make([]int16, samples)
	phase := 0.0
	for i := range out {
		t := float64(i) / 16000
		envelope := math.Pow(math.Abs(math.Sin(2*math.Pi*2.1*t)), 0.8)
		phase += 2 * math.Pi * (123 + 18*float64(speaker) + 22*math.Sin(2*math.Pi*2.9*t)) / 16000
		wave := math.Sin(phase) + 0.54*math.Sin(3*phase+0.3) + 0.36*math.Sin(6*phase+0.03*t)
		out[i] = int16(6000 * envelope * wave)
	}
	return out
}

func echoTestPCM(samples []int16) []byte {
	out := make([]byte, 2*len(samples))
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(sample))
	}
	return out
}

func mixEqualPower(mic, near []int16) {
	var micEnergy, nearEnergy float64
	for i, sample := range mic {
		far, voice := float64(sample), float64(near[i])
		micEnergy += far * far
		nearEnergy += voice * voice
	}
	gain := math.Sqrt(micEnergy / nearEnergy)
	for i, sample := range mic {
		mic[i] = int16(.5*float64(sample) + .5*gain*float64(near[i]))
	}
}

func TestMatchingPlaybackAudio(t *testing.T) {
	ref := echoTestUtterance(2*16000, 0)
	other := echoTestUtterance(len(ref), 1)
	const offset, count = 7131, 13440
	segment := ref[offset : offset+count]
	scaled := make([]int16, count)
	for i, sample := range segment {
		scaled[i] = int16(-0.19*float64(sample)) + 52 // analog gain, polarity and DC
	}
	t.Run("odd sample delay with gain and polarity", func(t *testing.T) {
		if !matchingPlaybackAudio(echoTestPCM(scaled), echoTestPCM(ref)) {
			t.Fatal("speaker waveform at a non-frame-aligned offset was missed")
		}
	})
	t.Run("room reflection through one delayed tap", func(t *testing.T) {
		for _, delay := range []int{16, 32, 48} {
			mic := make([]int16, count)
			for i := range mic {
				direct := float64(ref[offset+i])
				reflected := float64(ref[offset+i-delay])
				mic[i] = int16(.30 * (-direct + .40*reflected))
			}
			if !matchingPlaybackAudio(echoTestPCM(mic), echoTestPCM(ref)) {
				t.Errorf("failed at %d-sample reflection delay", delay)
			}
		}
	})
	t.Run("silence around the overlapping playback", func(t *testing.T) {
		// In particular the start and end of the microphone capture may extend
		// beyond the reference; it is the active sound that needs to align.
		mic := make([]int16, count)
		copy(mic[1341:count-1227], scaled[1341:count-1227])
		if !matchingPlaybackAudio(echoTestPCM(mic), echoTestPCM(ref[offset+1100:offset+count-1080])) {
			t.Fatal("quiet capture boundaries prevented an otherwise strong match")
		}
	})
	t.Run("independent utterance with same syllable rhythm", func(t *testing.T) {
		if matchingPlaybackAudio(echoTestPCM(other[offset:offset+count]), echoTestPCM(ref)) {
			t.Fatal("independent speaker was suppressed")
		}
	})
	t.Run("near end voice mixed with the echo", func(t *testing.T) {
		for _, span := range [][2]int{{0, count}, {count - 2600, count}} {
			mixture := append([]int16(nil), segment...)
			for i := span[0]; i < span[1]; i++ {
				mixture[i] = int16(float64(segment[i])*0.52 + float64(other[offset+i])*0.42)
			}
			if matchingPlaybackAudio(echoTestPCM(mixture), echoTestPCM(ref)) {
				t.Fatal("mixed or late near-end speech was suppressed")
			}
		}
	})
	t.Run("new speech at the capture edges", func(t *testing.T) {
		// A near-end speaker enters in just one short boundary area. The
		// rest of this microphone capture is clean, sample-aligned playback.
		for _, ms := range []int{30, 60, 90} {
			n := 16 * ms
			for _, start := range []int{0, count - n} {
				mic := append([]int16(nil), segment...)
				for j := 0; j < n; j++ {
					mic[start+j] = other[offset+start+j]/3 + other[8300+j]
				}
				if matchingPlaybackAudio(echoTestPCM(mic), echoTestPCM(ref)) {
					t.Errorf("%d ms of new speech at offset %d was suppressed", ms, start)
				}
				// Also exercise simultaneous far- and near-end speech with
				// equal local RMS over this short fragment.
				copy(mic, segment)
				mixEqualPower(mic[start:start+n], other[8300:])
				if matchingPlaybackAudio(echoTestPCM(mic), echoTestPCM(ref)) {
					t.Errorf("%d ms of mixed near-end speech at offset %d was suppressed", ms, start)
				}
			}
		}
	})
	t.Run("short near-end speech across interior windows", func(t *testing.T) {
		mic := append([]int16(nil), ref[7000:7000+count]...)
		start := count / 2
		mixEqualPower(mic[start:start+30*16], other[4821:])
		if matchingPlaybackAudio(echoTestPCM(mic), echoTestPCM(ref)) {
			t.Fatal("30 ms overlapping two central windows was suppressed")
		}
	})
	t.Run("repeated tone and silence are insufficient", func(t *testing.T) {
		tone := make([]int16, len(ref))
		for i := range tone {
			tone[i] = int16(6000 * math.Sin(2*math.Pi*380*float64(i)/16000))
		}
		for _, signal := range [][]int16{tone, make([]int16, len(ref))} {
			if matchingPlaybackAudio(echoTestPCM(signal[offset:offset+count]), echoTestPCM(signal)) {
				t.Fatal("uninformative sound was suppressed")
			}
		}
	})
	t.Run("unusable and over budget captures are ignored", func(t *testing.T) {
		raw := echoTestPCM(ref)
		for _, pair := range [][2][]byte{{nil, raw}, {raw[:401], raw}, {raw[:32002], raw}, {raw[:2000], nil}, {raw[:2000], raw[:1717]}, {raw[:12000], make([]byte, 96002)}} {
			if matchingPlaybackAudio(pair[0], pair[1]) {
				t.Fatal("invalid capture was suppressed")
			}
		}
	})
}

func TestMatchingPlaybackAudioCodecBands(t *testing.T) {
	const offset, count = 7131, 13440
	ref := make([]int16, 32000)
	coded, different := make([]int16, count), make([]int16, count)
	phase := 0.0
	for i := range ref {
		sec := float64(i) / 16000
		phase += 2 * math.Pi * (138 + 35*math.Sin(2*math.Pi*2.9*sec)) / 16000
		level := 4000 * math.Pow(math.Abs(math.Sin(2*math.Pi*2.1*sec)), .8)
		sample := func(harmonicPhase, high float64) int16 {
			return int16(level * (math.Sin(phase) + .68*math.Sin(3*high+harmonicPhase) + .50*math.Sin(5*high-harmonicPhase)))
		}
		ref[i] = sample(0, phase)
		j := i - offset
		if j < 0 || j >= count {
			continue
		}
		coded[j], different[j] = ref[i], ref[i]
		// One 43 ms voiced region retains pitch but changes harmonic phase.
		// The control keeps the same pitch and adds independent upper tones.
		if j >= 400*16 && j < 443*16 {
			coded[j] = sample(.7, phase)
			different[j] = sample(0, 2*math.Pi*176*sec)
		}
	}
	if !matchingPlaybackAudio(echoTestPCM(coded), echoTestPCM(ref)) {
		t.Error("missed a locally phase-distorted playback copy")
	}
	if matchingPlaybackAudio(echoTestPCM(different), echoTestPCM(ref)) {
		t.Error("matching pitch hid new upper-band sound")
	}
}

func BenchmarkMatchingPlaybackAudio(b *testing.B) {
	ref := echoTestUtterance(3*16000, 0)
	mic := echoTestPCM(ref[13013 : 13013+16000])
	raw := echoTestPCM(ref)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !matchingPlaybackAudio(mic, raw) {
			b.Fatal("missing echo")
		}
	}
}
