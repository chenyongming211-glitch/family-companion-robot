package realtimelive

import (
	"context"
	"encoding/base64"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// The clip must land at the level the answer speaks at, not at full scale.
// Measured on the same voice: answer peak -9.6 dBFS, voiced RMS -20.6. A clip
// normalised to a -3 dBFS peak sits ~11 dB above that and reads as a second,
// louder person -- the exact failure this pool exists to avoid.
func TestPreparePrefaceClipMatchesTheAnswerLevel(t *testing.T) {
	// A steady tone with a quiet lead-in and tail, like a spoken syllable.
	src := makeTone(1200, 300.0, 0.9) // ms, Hz, amplitude
	clip := preparePrefaceClip(src)
	if len(clip) == 0 {
		t.Fatal("a voiced clip must survive preparation")
	}
	if len(clip)%prefacePoolPacket != 0 {
		t.Fatalf("clip must be whole packets, got %d bytes", len(clip))
	}
	samples := pcmToFloat(clip)
	rms := voicedRMS(samples)
	peak := 0.0
	for _, v := range samples {
		peak = math.Max(peak, math.Abs(float64(v)))
	}
	rmsDB := 20 * math.Log10(rms/32768)
	peakDB := 20 * math.Log10(peak/32768)
	if math.Abs(rmsDB-prefaceTargetVoicedRMS) > 1.5 {
		t.Fatalf("voiced RMS must land near %.1f dBFS, got %.1f", prefaceTargetVoicedRMS, rmsDB)
	}
	if peakDB > prefaceMaxPeak+0.1 {
		t.Fatalf("peak must stay at or below %.1f dBFS, got %.1f", prefaceMaxPeak, peakDB)
	}
}

func TestPreparePrefaceClipTrimsSilenceAndKeepsABreath(t *testing.T) {
	src := makeTone(900, 300.0, 0.9)
	silent := make([]byte, prefacePoolRate/2*2) // 500ms of true silence at the head
	src = append(silent, src...)
	clip := preparePrefaceClip(src)
	if len(clip) == 0 {
		t.Fatal("clip lost")
	}
	seam := prefacePoolRate * prefaceSeamSilenceMS / 1000 * 2
	body := len(clip) - seam
	if body <= 0 {
		t.Fatalf("clip body is only the seam: %d bytes", len(clip))
	}
	// 900ms tone + 12ms of padding each side is well under the untrimmed length
	// of 1400ms; a clip that never got trimmed would be much longer.
	if body > prefacePoolRate*2*1100/1000 {
		t.Fatalf("lead-in silence was not trimmed: body %d bytes", body)
	}
	// The tail must be silence, or two clips fuse into one phrase.
	tail := pcmToFloat(clip[len(clip)-seam:])
	for i, v := range tail {
		if v != 0 {
			t.Fatalf("seam must be silent, sample %d is %v", i, v)
		}
	}
}

func TestPreparePrefaceClipRejectsSilence(t *testing.T) {
	if clip := preparePrefaceClip(make([]byte, 48000)); clip != nil {
		t.Fatalf("silence must not become a clip, got %d bytes", len(clip))
	}
}

// Synthesis must use the same session.update -- and therefore the same voice --
// the answer will use. A different voice here is the one mistake that cannot be
// fixed by tuning levels.
func TestSynthPrefacePoolUsesTheConfiguredVoice(t *testing.T) {
	var seenVoice string
	provider := httptest.NewServer(prefaceWebsocketHandler(t, func(c *websocket.Conn) {
		c.WriteJSON(map[string]any{"type": "session.created"})
		var req map[string]any
		if c.ReadJSON(&req) != nil || req["type"] != "session.update" {
			return
		}
		if out, ok := object(object(req["session"])["audio"])["output"].(map[string]any); ok {
			seenVoice, _ = out["voice"].(string)
		}
		c.WriteJSON(map[string]any{"type": "session.updated"})
		for {
			if c.ReadJSON(&req) != nil {
				return
			}
			switch req["type"] {
			case "conversation.item.create":
				continue
			case "response.create":
				c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
				c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "r1",
					"delta": base64.StdEncoding.EncodeToString(makeTone(400, 300.0, 0.9))})
				c.WriteJSON(map[string]any{"type": "response.audio_transcript.done", "response_id": "r1", "transcript": "嗯。"})
				c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r1", "status": "completed"}})
			}
		}
	}))
	defer provider.Close()

	dir := t.TempDir()
	cfg := Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http"), Key: "k", Voice: "Mei", Model: "test-model"}
	pool, err := SynthPrefacePool(context.Background(), cfg, dir, []prefaceLineSpec{{ID: "a01", Tier: 1, Text: "嗯。"}})
	if err != nil {
		t.Fatal(err)
	}
	if seenVoice != "Mei" {
		t.Fatalf("synthesis must speak in the configured voice, got %q", seenVoice)
	}
	if pool.Lines() != 1 {
		t.Fatalf("want one clip, got %d", pool.Lines())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"voice": "Mei"`) {
		t.Fatalf("manifest must record the voice it was made for: %s", raw)
	}
}

// A pool for one voice must never be served to another: that is the two-people
// failure with no level or timing fix.
func TestPrefaceStoreKeepsOneDirectoryPerVoice(t *testing.T) {
	root := t.TempDir()
	store := NewPrefaceStore()
	pool := newTestPool(testLine("a", 1, 480))
	pool.voice = "Mei"
	store.Put(pool)

	if got := store.For("Mei"); got == nil {
		t.Fatal("the stored voice must be served")
	}
	if got := store.For("Tina"); got != nil {
		t.Fatal("another voice must not be served this pool")
	}
	if _, err := store.Ensure(context.Background(), Config{Voice: "Tina"}, root, nil); err == nil {
		t.Fatal("with no clips and no provider, Ensure must report failure rather than borrow another voice")
	}
	// The cached one still wins without touching the provider.
	got, err := store.Ensure(context.Background(), Config{Voice: "Mei"}, root, nil)
	if err != nil || got == nil {
		t.Fatalf("a cached voice must not need synthesis: %v", err)
	}
}

func TestLoadPrefaceLinesValidatesInput(t *testing.T) {
	if lines, err := LoadPrefaceLines(""); err != nil || len(lines) == 0 {
		t.Fatalf("defaults must load: %v", err)
	}
	dir := t.TempDir()
	cases := map[string]string{
		"tier":      `[{"id":"a","tier":9,"text":"嗯。"}]`,
		"empty":     `[]`,
		"duplicate": `[{"id":"a","tier":1,"text":"嗯。"},{"id":"a","tier":1,"text":"哎。"}]`,
		"text":      `[{"id":"a","tier":1,"text":""}]`,
	}
	for name, body := range cases {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPrefaceLines(path); err == nil {
			t.Fatalf("%s: malformed lines must be rejected", name)
		}
	}
}

// makeTone builds ms milliseconds of a sine at hz, amplitude 0..1, as s16le.
func makeTone(ms int, hz, amplitude float64) []byte {
	n := prefacePoolRate * ms / 1000
	out := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := amplitude * 32767 * math.Sin(2*math.Pi*hz*float64(i)/prefacePoolRate)
		putInt16(out[i*2:], int32(v))
	}
	return out
}
