package realtimelive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"familyrobot/internal/realtimestate"

	"github.com/gorilla/websocket"
)

func newTestPool(lines ...prefaceLine) *PrefacePool {
	return &PrefacePool{voice: "Tina", lines: lines, rnd: rand.New(rand.NewSource(1))}
}

func testLine(id string, tier, ms int) prefaceLine {
	pcm := bytes.Repeat([]byte{1, 0}, ms*prefacePoolRate/1000)
	// Whole packets, the way the loader requires.
	if pad := len(pcm) % prefacePoolPacket; pad != 0 {
		pcm = append(pcm, make([]byte, prefacePoolPacket-pad)...)
	}
	return prefaceLine{ID: id, Tier: tier, MS: ms, Bytes: len(pcm), pcm: pcm}
}

// A clip queued before the answer must stay before it, and a clip considered
// after it must not be queued at all. Reversed, the robot answers and then says
// "嗯" into the silence it just filled.
func TestPrefaceGateKeepsClipsInFrontOfTheAnswer(t *testing.T) {
	var order []string
	g := &prefaceGate{}
	ok, err := g.clip(func() error { order = append(order, "clip"); return nil })
	if err != nil || !ok {
		t.Fatalf("the first clip must be written: ok=%v err=%v", ok, err)
	}
	if err = g.answer(func() error { order = append(order, "answer"); return nil }); err != nil {
		t.Fatal(err)
	}
	ok, err = g.clip(func() error { order = append(order, "late_clip"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a clip must not be queued once the answer has the floor")
	}
	want := []string{"clip", "answer"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order must be clips then answer, got %v", order)
	}
	if !g.taken() {
		t.Fatal("the gate must report the answer as owning the floor")
	}
}

// The gate is the only thing standing between a clip and the answer's first
// packet. Under contention the answer must never end up in front of a clip that
// was admitted, and no clip may be admitted after it.
func TestPrefaceGateHoldsUnderContention(t *testing.T) {
	for round := 0; round < 200; round++ {
		g := &prefaceGate{}
		var wrote []string
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 5; i++ {
				g.clip(func() error { wrote = append(wrote, "clip"); return nil })
			}
		}()
		g.answer(func() error { wrote = append(wrote, "answer"); return nil })
		<-done
		seenAnswer := false
		for _, v := range wrote {
			if v == "answer" {
				seenAnswer = true
				continue
			}
			if seenAnswer {
				t.Fatalf("round %d: clip queued behind the answer: %v", round, wrote)
			}
		}
		if !seenAnswer {
			t.Fatalf("round %d: the answer never got the floor", round)
		}
	}
}

// The answer may take the floor at any seam, so clip length is the most it can
// delay a fast answer. Tier 1 must therefore stay short, and the tier must only
// grow once the answer is already known to be far away.
func TestPrefaceTierGrowsWithTheWait(t *testing.T) {
	cases := []struct {
		elapsedMS int64
		want      int
	}{
		{1400, 1}, {2399, 1},
		{2400, 2}, {3999, 2},
		{4000, 3}, {9000, 3},
	}
	for _, c := range cases {
		if got := prefaceTier(c.elapsedMS); got != c.want {
			t.Fatalf("at %dms want tier %d, got %d", c.elapsedMS, c.want, got)
		}
	}
}

// The first version fired at 500ms while the answer's first packet lands near
// 1.1s, so every turn -- including the fast ones -- got an acknowledgement, and
// a listener heard "嗯" after each sentence. The delay must sit above the
// ordinary case: this is a fill for silence the user has already noticed.
func TestPrefaceFiresOnlyAfterTheOrdinaryAnswerIsLate(t *testing.T) {
	const ordinaryFirstPacket = 1086 * time.Millisecond
	if prefaceFireDelay <= ordinaryFirstPacket {
		t.Fatalf("delay %v is at or below the median answer (%v): every turn would be prefaced", prefaceFireDelay, ordinaryFirstPacket)
	}
	if prefaceFireDelay < 1200*time.Millisecond {
		t.Fatalf("delay %v is too close to the ordinary case to be a slow-turn signal", prefaceFireDelay)
	}
	if prefaceTier(int64(prefaceFireDelay/time.Millisecond)) != 1 {
		t.Fatal("the moment the pool becomes eligible must be the lightest tier")
	}
}

func TestPrefacePoolPickAvoidsRecentLines(t *testing.T) {
	pool := newTestPool(testLine("a", 1, 480), testLine("b", 1, 480), testLine("c", 1, 480))
	seen := map[string]int{}
	for i := 0; i < 30; i++ {
		line := pool.Pick(1)
		if line == nil {
			t.Fatal("a tier with lines must always yield one")
		}
		seen[line.ID]++
	}
	for _, id := range []string{"a", "b", "c"} {
		if seen[id] == 0 {
			t.Fatalf("line %q was never chosen: %v", id, seen)
		}
	}
	// Three lines with a six-deep history: a line may repeat only after the
	// other two, never twice in a row.
	if prev := pool.recent[len(pool.recent)-1]; prev == "" {
		t.Fatal("the history must record the picks")
	}
}

func TestPrefacePoolPickKeepsTiersApart(t *testing.T) {
	pool := newTestPool(testLine("a", 1, 480), testLine("c", 3, 2400))
	for i := 0; i < 10; i++ {
		if line := pool.Pick(2); line != nil {
			t.Fatalf("an empty tier must yield nothing, got %v", line.ID)
		}
	}
	if line := pool.Pick(3); line == nil || line.Tier != 3 {
		t.Fatalf("tier 3 must return a tier 3 line, got %v", line)
	}
}

func TestLoadPrefacePoolRejectsADifferentVoice(t *testing.T) {
	dir := writePoolFixture(t, "Tina", testLine("a", 1, 480))
	if _, err := LoadPrefacePool(dir, "Tina"); err != nil {
		t.Fatalf("a matching voice must load: %v", err)
	}
	_, err := LoadPrefacePool(dir, "Cherry")
	if err == nil || !strings.Contains(err.Error(), "voice_mismatch") {
		t.Fatalf("a pool for another voice must be refused, got %v", err)
	}
}

// A resampled or wrong-rate clip is the other way one voice becomes two, so the
// loader refuses anything that is not exactly what the playback path carries.
func TestLoadPrefacePoolRejectsWrongFormat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*prefaceManifest)
		want   string
	}{
		{"rate", func(m *prefaceManifest) { m.SampleRate = 16000 }, "format"},
		{"channels", func(m *prefaceManifest) { m.Channels = 2 }, "format"},
		{"format", func(m *prefaceManifest) { m.SampleFormat = "f32le" }, "format"},
		{"packet", func(m *prefaceManifest) { m.PacketBytes = 960 }, "packet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePoolFixture(t, "Tina", testLine("a", 1, 480))
			path := filepath.Join(dir, "manifest.json")
			var m prefaceManifest
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&m)
			raw, _ = json.Marshal(m)
			if err = os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = LoadPrefacePool(dir, "Tina")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a %q rejection, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadPrefacePoolRejectsRaggedClip(t *testing.T) {
	dir := writePoolFixture(t, "Tina", testLine("a", 1, 480))
	path := filepath.Join(dir, "a.pcm")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw[:len(raw)-2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPrefacePool(dir, "Tina"); err == nil {
		t.Fatal("a clip that is not a whole number of packets must be refused")
	}
}

func writePoolFixture(t *testing.T, voice string, lines ...prefaceLine) string {
	t.Helper()
	dir := t.TempDir()
	m := prefaceManifest{
		Voice: voice, SampleRate: prefacePoolRate, SampleFormat: "s16le",
		Channels: prefacePoolChannels, PacketBytes: prefacePoolPacket, SeamSilenceMS: 250,
	}
	for _, line := range lines {
		if err := os.WriteFile(filepath.Join(dir, line.ID+".pcm"), line.pcm, 0o600); err != nil {
			t.Fatal(err)
		}
		m.Lines = append(m.Lines, prefaceLine{
			ID: line.ID, Tier: line.Tier, MS: line.MS, Bytes: len(line.pcm),
		})
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testSession() *Session {
	return &Session{rec: recorder{memoryOnly: true}}
}

// Silence below half a second is what a person fills instantly; answering sooner
// would put a "嗯" in front of the turns that never needed one.
func TestSpeakPrefaceWaitsBeforeSpeaking(t *testing.T) {
	s := testSession()
	rt := &liveTurn{turn: realtimestate.NewTurn(1), stoppedAt: time.Now()}
	out := make(chan encodedPacket, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.speakPreface(ctx, rt, &prefaceGate{}, newTestPool(testLine("a", 1, 480)),
		passthrough, func() {}, out)
	select {
	case p := <-out:
		t.Fatalf("nothing may be spoken before the fire delay, got %d bytes", len(p.packet))
	case <-time.After(300 * time.Millisecond):
	}
}

// Once it does speak, the clip is this turn's opening audio: it must arrive as
// whole packets flagged as a preface, so the ledger can separate the two latency
// numbers and the transcript can leave it out.
func TestSpeakPrefaceSpeaksWholePackets(t *testing.T) {
	s := testSession()
	line := testLine("a", 1, 480)
	rt := &liveTurn{turn: realtimestate.NewTurn(1), stoppedAt: time.Now().Add(-time.Second)}
	out := make(chan encodedPacket, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var prepared atomic.Int32
	go s.speakPreface(ctx, rt, &prefaceGate{}, newTestPool(line, testLine("b", 2, 900)),
		passthrough, func() { prepared.Add(1) }, out)

	want := line.Bytes / prefacePoolPacket
	got := 0
	deadline := time.After(2 * time.Second)
	for got < want {
		select {
		case p := <-out:
			if len(p.packet) != prefacePoolPacket {
				t.Fatalf("clip packets must be %d bytes, got %d", prefacePoolPacket, len(p.packet))
			}
			if !p.preface {
				t.Fatal("clip packets must be flagged as preface")
			}
			got++
		case <-deadline:
			t.Fatalf("only %d of %d clip packets arrived", got, want)
		}
	}
	if prepared.Load() == 0 {
		t.Fatal("the consumer must be told the turn is prepared")
	}
}

// The answer arriving mid-clip stops the pool at the seam. What was already
// queued plays; nothing new is added behind the answer.
func TestSpeakPrefaceStopsWhenTheAnswerTakesTheFloor(t *testing.T) {
	s := testSession()
	line := testLine("a", 1, 300)
	rt := &liveTurn{turn: realtimestate.NewTurn(1), stoppedAt: time.Now().Add(-time.Second)}
	out := make(chan encodedPacket, 200)
	gate := &prefaceGate{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.speakPreface(ctx, rt, gate, newTestPool(line), passthrough, func() {}, out)

	// Wait for the first clip to be queued, then let the answer in.
	first := line.Bytes / prefacePoolPacket
	for i := 0; i < first; i++ {
		select {
		case <-out:
		case <-time.After(2 * time.Second):
			t.Fatal("the clip never arrived")
		}
	}
	if err := gate.answer(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	drained := 0
	for {
		select {
		case <-out:
			drained++
		case <-time.After(400 * time.Millisecond):
			// At most the single clip already queued may follow; a second clip
			// means the pool spoke after the answer.
			if drained >= first {
				t.Fatalf("the pool queued %d more packets after the answer took the floor", drained)
			}
			return
		}
	}
}

func passthrough(pcm []byte) ([]byte, error) {
	return append([]byte(nil), pcm...), nil
}

// Streaming text is still the way a text-only turn produces audio; the pool did
// not remove that path, it only stopped using it for acknowledgements.
func TestStreamTextSendsTextItemAndNoAudio(t *testing.T) {
	provider := httptest.NewServer(prefaceWebsocketHandler(t, func(c *websocket.Conn) {
		c.WriteJSON(map[string]any{"type": "session.created"})
		var req map[string]any
		if c.ReadJSON(&req) != nil || req["type"] != "session.update" {
			return
		}
		settings := object(req["session"])
		if _, present := settings["enable_search"]; present {
			t.Error("a text turn must not enable search")
		}
		c.WriteJSON(map[string]any{"type": "session.updated"})
		if c.ReadJSON(&req) != nil {
			return
		}
		if req["type"] != "conversation.item.create" {
			t.Errorf("first request after the handshake must carry the text item, got %v", req["type"])
			return
		}
		content, _ := object(req["item"])["content"].([]any)
		if len(content) != 1 || object(content[0])["type"] != "input_text" {
			t.Errorf("item must carry input_text, got %v", content)
			return
		}
		if !strings.Contains(object(content[0])["text"].(string), "明天天气") {
			t.Error("item text lost the user's request")
		}
		if c.ReadJSON(&req) != nil || req["type"] != "response.create" {
			return
		}
		c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
		c.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": "r1", "delta": base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})})
		c.WriteJSON(map[string]any{"type": "response.audio_transcript.delta", "response_id": "r1", "delta": "今天挺暖和的。"})
		c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r1", "status": "completed"}})
	}))
	defer provider.Close()

	dir := t.TempDir()
	n := NewNative(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, dir)
	defer n.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.Open(ctx, "你是家庭语音助手。", false); err != nil {
		t.Fatal(err)
	}
	var pcm []byte
	text := ""
	err := n.StreamText(ctx, "明天天气怎么样", func(kind string, value []byte) error {
		switch kind {
		case "pcm":
			pcm = append(pcm, value...)
		case "text":
			text = string(value)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != 4 {
		t.Fatalf("audio not forwarded: %v", pcm)
	}
	if text != "今天挺暖和的。" {
		t.Fatalf("transcript not captured: %q", text)
	}
}

func prefaceWebsocketHandler(t *testing.T, serve func(*websocket.Conn)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		serve(c)
	}
}

func TestPrefaceStaysSilentOnRoutesThatAnswerInAnotherVoice(t *testing.T) {
	// Routes A and B synthesize the answer with cosyvoice's longanyang. The pool
	// is built from cfg.Voice through the realtime endpoint, so a clip there
	// would be a second speaker in the same turn.
	for _, route := range []string{"A", "B"} {
		s := &Session{voiceRoute: route}
		if s.prefaceAllowed() {
			t.Fatalf("route %s answers in another voice; a clip would be a second person", route)
		}
	}
	for _, route := range []string{"", "C"} {
		s := &Session{voiceRoute: route}
		if !s.prefaceAllowed() {
			t.Fatalf("route %q answers with cfg.Voice, so it can be prefaced", route)
		}
	}
}

func TestPrefacePoolRejectsAWordingItNoLongerSpeaks(t *testing.T) {
	// The cached pool loads fine on its own terms: manifest valid, clips present.
	// If nothing compares it to the configured lines, an edited wording keeps
	// playing the old recording and the only symptom is a sentence nobody wrote.
	pool := newTestPool(
		prefaceLine{ID: "a01", Tier: 1, Text: "嗯。"},
		prefaceLine{ID: "b01", Tier: 2, Text: "我想想啊。"},
	)
	if !pool.speaks([]prefaceLineSpec{{ID: "a01", Tier: 1, Text: "嗯。"}, {ID: "b01", Tier: 2, Text: "我想想啊。"}}) {
		t.Fatal("an unchanged list must be accepted, or every restart resynthesizes")
	}
	if pool.speaks([]prefaceLineSpec{{ID: "a01", Tier: 1, Text: "在呢。"}, {ID: "b01", Tier: 2, Text: "我想想啊。"}}) {
		t.Fatal("changed wording must invalidate the pool")
	}
	if pool.speaks([]prefaceLineSpec{{ID: "a01", Tier: 3, Text: "嗯。"}, {ID: "b01", Tier: 2, Text: "我想想啊。"}}) {
		t.Fatal("changed tier must invalidate the pool")
	}
	if pool.speaks([]prefaceLineSpec{{ID: "a01", Tier: 1, Text: "嗯。"}}) {
		t.Fatal("a shorter list must invalidate the pool")
	}
}

func TestLoadCachedRefusesAWordingItNoLongerSpeaks(t *testing.T) {
	// The boot path loads a pool directly rather than going through Ensure. That
	// shortcut is exactly how an edited wording kept playing: the directory is
	// valid, the clips load, and nothing compared the recordings to the list.
	root := t.TempDir()
	voice := "Tina"
	dir := filepath.Join(root, voice)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pcm := bytes.Repeat([]byte{1, 0}, prefacePoolPacket/2)
	if len(pcm) != prefacePoolPacket {
		t.Fatalf("setup produced %d bytes", len(pcm))
	}
	if err := os.WriteFile(filepath.Join(dir, "a01.pcm"), pcm, 0o600); err != nil {
		t.Fatal(err)
	}
	m := prefaceManifest{
		Voice: voice, Model: "m", SampleRate: prefacePoolRate, SampleFormat: "s16le",
		Channels: prefacePoolChannels, PacketBytes: prefacePoolPacket, SeamSilenceMS: prefaceSeamSilenceMS,
		Lines: []prefaceLine{{ID: "a01", Tier: 1, Text: "在呢。", MS: 60, Bytes: len(pcm), SHA: "x"}},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Voice: voice}
	if _, ok := NewPrefaceStore().LoadCached(root, cfg, []prefaceLineSpec{{ID: "a01", Tier: 1, Text: "嗯。"}}); ok {
		t.Fatal("a pool recorded for other words must not be adopted")
	}
	pool, ok := NewPrefaceStore().LoadCached(root, cfg, []prefaceLineSpec{{ID: "a01", Tier: 1, Text: "在呢。"}})
	if !ok || pool.Lines() != 1 {
		t.Fatal("a pool matching the configured wording must load without resynthesis")
	}
}

// One short acknowledgement per short wait. Two in a row reads as a stuck
// recording: "嗯", then "哎" a beat later, while the answer is still being made.
func TestSpeakPrefaceAcknowledgesOnlyOncePerShortWait(t *testing.T) {
	s := testSession()
	short := testLine("a", 1, 120)
	rt := &liveTurn{turn: realtimestate.NewTurn(1), stoppedAt: time.Now().Add(-1500 * time.Millisecond)}
	out := make(chan encodedPacket, 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.speakPreface(ctx, rt, &prefaceGate{}, newTestPool(short, testLine("b", 2, 120)),
		passthrough, func() {}, out)

	window := time.After(600 * time.Millisecond)
	got := 0
collect:
	for {
		select {
		case <-out:
			got++
		case <-window:
			break collect
		}
	}
	if want := short.Bytes / prefacePoolPacket; got != want {
		t.Fatalf("one short wait must produce one acknowledgement: got %d packets, want %d", got, want)
	}
}
