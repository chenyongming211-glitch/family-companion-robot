package realtimelive

import (
	"bytes"
	"context"
	"encoding/json"
	"familyrobot/internal/music"
	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type guardedMusicSource struct {
	routeMusicSource
	allowed   bool
	ambiguous bool
	live      *atomic.Bool
}

func (g *guardedMusicSource) CanAccessTrack(music.Track) bool {
	if g.live != nil {
		return g.live.Load()
	}
	return g.allowed
}
func (g *guardedMusicSource) Search(ctx context.Context, q music.Query) ([]music.Track, error) {
	if g.ambiguous {
		return nil, music.ErrAmbiguous
	}
	return g.routeMusicSource.Search(ctx, q)
}
func TestMusicPrivateFactsAndQueueRevocation(t *testing.T) {
	s, _ := musicRouteSession(t)
	g := &guardedMusicSource{allowed: true, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "private title", Artist: "private artist", Duration: time.Minute}, {ID: (1 << 52) + 1, Title: "next private", Artist: "private artist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	selectMusicForFacts(t, s)
	s.mu.Lock()
	f := s.musicFactsLocked()
	s.mu.Unlock()
	if !strings.Contains(f, `"source":"private"`) {
		t.Fatal(f)
	}
	g.allowed = false
	s.mu.Lock()
	f = s.musicFactsLocked()
	s.mu.Unlock()
	if strings.Contains(f, "private title") || strings.Contains(f, "private artist") || len(s.music.queue) != 0 {
		t.Fatal("revoked metadata leaked", f)
	}
	prepareMusicPlan(t, s, "next", "", "")
	r, e := s.routeMusic(musicRequest(s, "下一首"), `{"recent_turns":[]}`)
	if e != nil || r.Stream != nil || g.resolves != 1 {
		t.Fatal("revoked queue reused", e, g.resolves)
	}
}
func TestMusicAmbiguityClarifiesWithoutResolve(t *testing.T) {
	s, _ := musicRouteSession(t)
	g := &guardedMusicSource{allowed: true, ambiguous: true}
	s.music = &sessionMusic{source: g}
	prepareMusicPlan(t, s, "play", "黄昏", "")
	r, e := s.routeMusic(musicRequest(s, "播放黄昏"), `{"recent_turns":[]}`)
	if e != nil || r.Stream != nil || g.resolves != 0 || !strings.Contains(r.Guidance, `"outcome":"needs_clarification"`) || !strings.Contains(r.Guidance, `"missing":"artist"`) {
		t.Fatal(r, e)
	}
}
func TestMusicNilFactoryFailsClosed(t *testing.T) {
	s, _ := musicRouteSession(t)
	s.music = nil
	s.cfg.MusicForDevice = func(string) *music.Catalog { return nil }
	prepareMusicPlan(t, s, "play", "黄昏", "")
	r, e := s.routeMusic(musicRequest(s, "播放黄昏"), `{"recent_turns":[]}`)
	if e != nil || r.Stream != nil {
		t.Fatal(r, e)
	}
}
func TestMusicPrivateOnlyConfigFacts(t *testing.T) {
	c := Config{Music: music.NewPrivateOnly(), MusicForDevice: func(string) *music.Catalog { return nil }}
	if c.facts()["music_online"] != false || c.facts()["music_library"] != true {
		t.Fatal(c.facts())
	}
}

func TestMusicPrivateRevocationStopsFurtherPCM(t *testing.T) {
	s, _ := musicRouteSession(t)
	g := &guardedMusicSource{allowed: true, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "Private", Artist: "Artist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	prepareMusicPlan(t, s, "play", "Private", "Artist")
	rt := musicRequest(s, "播放Private")
	r, e := s.routeMusic(rt, `{"recent_turns":[]}`)
	if e != nil || r.Stream == nil {
		t.Fatal(e)
	}
	g.allowed = false
	chunks := 0
	e = r.Stream(rt.ctx, func(kind string, _ []byte) error {
		if kind == "audio" {
			chunks++
		}
		return nil
	})
	if e == nil || chunks != 0 {
		t.Fatal("revoked selection emitted PCM", e, chunks)
	}
}
func TestMusicFactoryFreshPerPlayAndNextKeepsSource(t *testing.T) {
	s, f := musicRouteSession(t)
	calls := 0
	// Existing queue remains owned by its source; next never invokes the factory.
	selectMusicForFacts(t, s)
	s.cfg.MusicForDevice = func(string) *music.Catalog { calls++; return nil }
	prepareMusicPlan(t, s, "next", "", "")
	r, e := s.routeMusic(musicRequest(s, "下一首"), `{"recent_turns":[]}`)
	if e != nil || r.Stream == nil || calls != 0 || f.resolves != 3 {
		t.Fatal("next changed source", e, calls)
	}
	for i := 0; i < 2; i++ {
		prepareMusicPlan(t, s, "play", "New", "Artist")
		r, e = s.routeMusic(musicRequest(s, "播放New"), `{"recent_turns":[]}`)
		if e != nil || r.Stream != nil {
			t.Fatal(e)
		}
	}
	if calls != 2 || f.searches != 1 {
		t.Fatal("play reused old source", calls, f.searches)
	}
}

func TestMusicScopedLibraryRebindNewPlayAndDeletedFacts(t *testing.T) {
	s, _ := musicRouteSession(t)
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "synthetic.flac")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2", "-y", fixture).CombinedOutput(); e != nil {
		t.Fatalf("fixture %v %s", e, b)
	}
	data, e := os.ReadFile(fixture)
	if e != nil {
		t.Fatal(e)
	}
	var allowA atomic.Bool
	allowA.Store(true)
	a, e := music.NewLibrary(root, "family-a")
	if e != nil {
		t.Fatal(e)
	}
	a = a.WithAccessCheck(allowA.Load)
	b, e := music.NewLibrary(root, "family-b")
	if e != nil {
		t.Fatal(e)
	}
	b = b.WithAccessCheck(func() bool { return true })
	for _, title := range []string{"A One", "A Two"} {
		if _, e = a.Import(context.Background(), title, "Artist A", "flac", bytes.NewReader(data)); e != nil {
			t.Fatal(e)
		}
	}
	imported, e := b.Import(context.Background(), "B One", "Artist B", "flac", bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	s.cfg.MusicForDevice = func(device string) *music.Catalog {
		calls++
		if device != s.rec.owner.DeviceID {
			t.Error("incorrect ownership device")
		}
		if allowA.Load() {
			return music.NewPrivateOnly().WithLibrary(a, allowA.Load)
		}
		return music.NewPrivateOnly().WithLibrary(b, func() bool { return true })
	}
	prepareMusicPlan(t, s, "play", "", "Artist A")
	rt := musicRequest(s, "播放Artist A的歌")
	route, e := s.routeMusic(rt, `{"recent_turns":[]}`)
	if e != nil || route.Stream == nil {
		t.Fatal(e)
	}
	if s.music.track.Artist != "Artist A" {
		t.Fatal("wrong family", s.music.track)
	}
	allowA.Store(false)
	prepareMusicPlan(t, s, "none", "", "")
	query, e := s.routeMusic(musicRequest(s, "刚才放的是什么歌"), `{"recent_turns":[]}`)
	if e != nil || musicFacts(t, query.Guidance)["selection"] != nil || strings.Contains(query.Guidance, "Artist A") {
		t.Fatal("old facts leaked", e, query.Guidance)
	}
	prepareMusicPlan(t, s, "next", "", "")
	next, e := s.routeMusic(musicRequest(s, "下一首"), `{"recent_turns":[]}`)
	if e != nil || next.Stream != nil || calls != 1 {
		t.Fatal("next reused old queue", e, calls)
	}
	prepareMusicPlan(t, s, "play", "B One", "Artist B")
	rt = musicRequest(s, "播放Artist B的B One")
	route, e = s.routeMusic(rt, `{"recent_turns":[]}`)
	if e != nil || route.Stream == nil || calls != 2 || s.music.track.Title != "B One" {
		t.Fatal("fresh binding ignored", e, calls)
	}
	ctx, cancel := context.WithTimeout(rt.ctx, 5*time.Second)
	defer cancel()
	chunks := 0
	if e = route.Stream(ctx, func(kind string, _ []byte) error {
		if kind == "audio" {
			chunks++
		}
		return nil
	}); e != nil || chunks == 0 {
		t.Fatal("private decode", e, chunks)
	}
	if strings.Contains(s.musicContext(`{"recent_turns":[{"user":"Artist A"}]}`), "Artist A") {
		t.Fatal("fresh selection reset revoked context")
	}
	if e = b.Delete(imported.ID); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	facts := s.musicFactsLocked()
	s.mu.Unlock()
	if !strings.Contains(facts, "B One") {
		t.Fatal("same-binding historical facts lost", facts)
	}
}

func TestMusicPrivateHistoryRevocationAndNoRecoverableMemory(t *testing.T) {
	s, _ := musicRouteSession(t)
	s.rec.owner.SessionID = strings.Repeat("a", 32)
	before := musicRequest(s, "safe before music")
	before.musicClassified = true
	g := &guardedMusicSource{allowed: true, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "Secret Family Song", Artist: "Secret Artist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	selectMusicForFacts(t, s)
	prior := musicRequest(s, "刚才Secret Family Song是谁唱的")
	prior.musicClassified = true
	s.memory = &sessionMemory{loaded: true}
	s.memory.mu.Lock()
	snapshot := s.memorySnapshotLocked()
	s.memory.mu.Unlock()
	if len(snapshot.Turns) != 1 || snapshot.Turns[0].User != "safe before music" {
		t.Fatal("memory boundary", snapshot)
	}
	for _, v := range snapshot.Turns {
		if strings.Contains(v.User, "Secret Family Song") {
			t.Fatal("private-derived text persisted", snapshot)
		}
	}
	fresh, _ := musicRouteSession(t)
	fresh.memory = &sessionMemory{loaded: true, base: snapshot.Turns}
	restored := fresh.mergeMemory(realtimestate.ContextFor(nil))
	if !strings.Contains(restored, "safe before music") || strings.Contains(restored, "Secret Family Song") {
		t.Fatal("fresh connection rehydrated private context", restored)
	}
	g.allowed = false
	current := musicRequest(s, "刚才谁唱的")
	history, e := s.settle(current.ctx, current)
	if e != nil || strings.Contains(history, "Secret Family Song") {
		t.Fatal("revoked metadata survived context", history, e)
	}
}

func TestMusicRevokedMetadataAbsentFromOrdinaryReplyPrompt(t *testing.T) {
	s, c := expressionSession(t)
	s.cfg.Music = music.New()
	s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi", SessionID: strings.Repeat("b", 32)}
	g := &guardedMusicSource{allowed: true, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "SecretFamilyTitle", Artist: "SecretFamilyArtist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	selectMusicForFacts(t, s)
	prior := musicRequest(s, "刚才SecretFamilyTitle是谁唱的")
	prior.musicClassified = true
	s.memory = &sessionMemory{loaded: true, base: []memoryTurn{{SessionID: strings.Repeat("c", 32), TurnID: 1, At: time.Now(), User: "SecretFamilyTitle", Assistant: "SecretFamilyArtist"}}}
	g.allowed = false
	// Avoid disk side effects in routeMemory; settle already reads the base above.
	priorHistory, e := s.settle(prior.ctx, prior)
	if e != nil || strings.Contains(priorHistory, "SecretFamilyTitle") {
		t.Fatal(priorHistory, e)
	}
	s.memory = nil
	cfg, planner := expressionProvider(t, false, func(c *websocket.Conn) { musicPlannerTerminal(c, `{"action":"none","title":"","artist":""}`, false) })
	s.cfg.URL = cfg.URL
	s.cfg.Synthetic = true
	called := false
	s.generate = func(_ context.Context, _ int64, prompt string, _ []byte, emit func(string, []byte) error) error {
		called = true
		if strings.Contains(prompt, "SecretFamilyTitle") || strings.Contains(prompt, "SecretFamilyArtist") {
			t.Error("revoked metadata reached reply prompt")
		}
		if e := emit("pcm", make([]byte, 2880)); e != nil {
			return e
		}
		return emit("text", []byte("当前没有可查询的选曲。"))
	}
	rt := musicRequest(s, "刚才谁唱的")
	done := startMediaReply(t, s, rt)
	for {
		_, _, event := readMediaMessage(t, c)
		if event["type"] == "tts" && event["state"] == "stop" {
			break
		}
	}
	awaitMediaReply(t, done)
	if !called {
		t.Fatal("ordinary reply did not run")
	}
	input := <-planner.input
	encoded, _ := json.Marshal(input)
	if strings.Contains(string(encoded), "SecretFamilyTitle") || strings.Contains(string(encoded), "SecretFamilyArtist") {
		t.Fatal("revoked metadata reached planner")
	}

}

func TestMusicFactsRecheckedImmediatelyBeforeReply(t *testing.T) {
	s, _ := musicRouteSession(t)
	g := &guardedMusicSource{allowed: true, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "PrivateCachedTitle", Artist: "PrivateArtist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	selectMusicForFacts(t, s)
	prepareMusicPlan(t, s, "clarify", "", "")
	route, e := s.routeMusic(musicRequest(s, "放那首"), `{"recent_turns":[]}`)
	if e != nil || !strings.Contains(route.Guidance, "PrivateCachedTitle") {
		t.Fatal("missing initial authorized facts", e)
	}
	g.allowed = false
	_, guidance, _ := s.musicReplyState("", route)
	if strings.Contains(guidance, "PrivateCachedTitle") || strings.Contains(guidance, "PrivateArtist") || !strings.Contains(guidance, `"outcome":"needs_clarification"`) {
		t.Fatal("reply reused earlier fact snapshot", guidance)
	}
}

func TestMusicOrdinaryReplyRevocationStopsFurtherPCMAndAllowsCleanTurn(t *testing.T) {
	s, c := expressionSession(t)
	s.cfg.Music = music.New()
	s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi", SessionID: strings.Repeat("d", 32)}
	var allowed atomic.Bool
	allowed.Store(true)
	g := &guardedMusicSource{live: &allowed, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "PrivateMidReplyTitle", Artist: "PrivateArtist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	selectMusicForFacts(t, s)
	prepareMusicPlan(t, s, "none", "", "")
	gate := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})
	s.generate = func(ctx context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		if e := emit("pcm", make([]byte, 2880)); e != nil {
			return e
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		if e := emit("text", []byte("PrivateMidReplyTitle")); e != nil {
			return e
		}
		return emit("pcm", make([]byte, 2880))
	}
	rt := musicRequest(s, "刚才放的是什么歌")
	done := startMediaReply(t, s, rt)
	frames := int64(0)
	reason := ""
	for {
		kind, _, event := readMediaMessage(t, c)
		if kind == websocket.BinaryMessage {
			frames++
			if frames == 1 {
				allowed.Store(false)
				close(gate)
			}
		}
		if event["type"] == "tts" && event["state"] == "stop" {
			reason, _ = event["reason"].(string)
			break
		}
	}
	awaitMediaReply(t, done)
	if frames != 1 || reason != "drain" {
		t.Fatalf("revoked ordinary output continued: frames=%d reason=%s", frames, reason)
	}
	mediaReceipt(t, s, rt, 1440, "finished") // Synthetic receipt only; no hearing claim.
	// A later prompt has no old context and must remain usable in this connection.
	prepareMusicPlan(t, s, "none", "", "")
	s.generate = func(_ context.Context, _ int64, prompt string, _ []byte, emit func(string, []byte) error) error {
		if strings.Contains(prompt, "PrivateMidReplyTitle") {
			t.Error("clean turn restored revoked context")
		}
		return emit("pcm", make([]byte, 2880))
	}
	next := musicRequest(s, "你好")
	nextDone := startMediaReply(t, s, next)
	nextFrames := 0
	for {
		kind, _, event := readMediaMessage(t, c)
		if kind == websocket.BinaryMessage {
			nextFrames++
		}
		if event["type"] == "tts" && event["state"] == "stop" {
			break
		}
	}
	awaitMediaReply(t, nextDone)
	if nextFrames != 1 {
		t.Fatal("old guard permanently blocked clean turn", nextFrames)
	}
}

func TestMusicOrdinaryReplyRevocationDropsAlreadyQueuedPCM(t *testing.T) {
	s, c := expressionSession(t)
	s.cfg.Music = music.New()
	s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi", SessionID: strings.Repeat("e", 32)}
	var allowed atomic.Bool
	allowed.Store(true)
	g := &guardedMusicSource{live: &allowed, routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 1 << 52, Title: "QueuedPrivateTitle", Artist: "PrivateArtist", Duration: time.Minute}}}}
	s.music = &sessionMusic{source: g}
	selectMusicForFacts(t, s)
	prepareMusicPlan(t, s, "none", "", "")
	buffered := make(chan struct{})
	s.generate = func(_ context.Context, _ int64, _ string, _ []byte, emit func(string, []byte) error) error {
		defer close(buffered)
		return emit("pcm", make([]byte, 11*2880))
	}
	rt := musicRequest(s, "刚才谁唱的")
	done := startMediaReply(t, s, rt)
	select {
	case <-buffered:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not queue PCM")
	}
	frames := int64(0)
	reason := ""
	for {
		kind, _, event := readMediaMessage(t, c)
		if kind == websocket.BinaryMessage {
			frames++
			if frames == 10 {
				allowed.Store(false)
				mediaReceipt(t, s, rt, 1440, "progress")
			}
		}
		if event["type"] == "tts" && event["state"] == "stop" {
			reason, _ = event["reason"].(string)
			break
		}
	}
	awaitMediaReply(t, done)
	if frames != 10 || reason != "drain" {
		t.Fatalf("queued PCM crossed revoked guard: frames=%d reason=%s", frames, reason)
	}
	mediaReceipt(t, s, rt, 10*1440, "finished")
}
