package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/music"
	"fmt"
	"github.com/gorilla/websocket"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type routeMusicSource struct {
	rows                        []music.Track
	unavailable                 map[int64]bool
	searches, resolves, streams int
}

func (f *routeMusicSource) Search(context.Context, music.Query) ([]music.Track, error) {
	f.searches++
	return append([]music.Track(nil), f.rows...), nil
}
func (f *routeMusicSource) Resolve(ctx context.Context, t music.Track) (music.Resolved, error) {
	f.resolves++
	if f.unavailable[t.ID] {
		return music.Resolved{}, music.ErrUnavailable
	}
	return music.Resolved{Track: t}, nil
}
func (f *routeMusicSource) Stream(ctx context.Context, r music.Resolved, emit func([]byte) error) (int64, error) {
	f.streams++
	if e := emit(make([]byte, 2880)); e != nil {
		return 0, e
	}
	return 1440, nil
}

func prepareMusicPlan(t *testing.T, s *Session, action, title, artist string) {
	t.Helper()
	cfg, _ := expressionProvider(t, false, func(c *websocket.Conn) {
		raw, _ := json.Marshal(map[string]string{"action": action, "title": title, "artist": artist})
		musicPlannerTerminal(c, string(raw), false)
	})
	s.cfg.URL = cfg.URL
	s.cfg.Synthetic = true
}
func musicRouteSession(t *testing.T) (*Session, *routeMusicSource) {
	t.Helper()
	ws, _ := testSocket(t)
	s := NewSession(ws, Config{}, t.TempDir(), nil)
	t.Cleanup(s.cancel)
	s.ownerErr = nil
	s.rec.owner = Ownership{DeviceID: "dev_11111111111111111111111111111111", ClientType: "xiaozhi"}
	s.cfg.Music = music.New()
	f := &routeMusicSource{rows: []music.Track{{ID: 1, Title: "黄昏", Artist: "周传雄", Duration: 5 * time.Minute}, {ID: 2, Title: "黄昏", Artist: "周传雄", Duration: 5 * time.Minute}, {ID: 3, Title: "黄昏 (Live)", Artist: "周传雄", Duration: 5 * time.Minute}}, unavailable: map[int64]bool{1: true}}
	s.music = &sessionMusic{source: f}
	return s, f
}
func musicRequest(s *Session, text string) *liveTurn {
	r := s.addTurn()
	r.turn.User = text
	r.transcriptOnce.Do(func() { close(r.transcriptReady) })
	return r
}

func TestMusicPlanRecordsTerminalAtDecision(t *testing.T) {
	for _, tc := range []struct{ name, reason, action string }{
		{"ordinary", "completed", "none"},
		{"requested", "completed", "play"},
		{"timeout", "timeout", ""},
		{"canceled", "canceled", ""},
		{"provider-error", "planner_error", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := musicRouteSession(t)
			rt := musicRequest(s, "放首 private-request")
			cfg, _ := expressionProvider(t, false, func(c *websocket.Conn) {
				switch tc.name {
				case "ordinary":
					musicPlannerTerminal(c, `{"action":"none","title":"","artist":""}`, false)
				case "requested":
					musicPlannerTerminal(c, `{"action":"play","title":"private-title","artist":"private-artist"}`, false)
				case "timeout":
					// Leave the response pending for the planner's own 3-second deadline.
				case "canceled":
					rt.cancel()
				case "provider-error":
					c.WriteJSON(map[string]any{"type": "error", "message": "private-provider-error"})
				}
			})
			s.cfg.URL, s.cfg.Synthetic = cfg.URL, true
			route, err := s.routeMusic(rt, `{"recent_turns":[],"context":"private-context"}`)
			if tc.name == "canceled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("turn cancellation lost: %v", err)
				}
			} else if err != nil || route.Handled != (tc.action == "play") ||
				(tc.reason != "completed") != strings.Contains(route.outcomeGuidance, "planner_unavailable") {
				t.Fatalf("unexpected routing after %s: handled=%t err=%v", tc.name, route.Handled, err)
			}
			events, _ := os.ReadFile(filepath.Join(s.directory, "events.jsonl"))
			count := 0
			for _, line := range strings.Split(string(events), "\n") {
				var event map[string]any
				if json.Unmarshal([]byte(line), &event) != nil || event["event"] != "music_plan" {
					continue
				}
				count++
				duration, measured := event["elapsed_ms"].(float64)
				action, reported := event["action"].(string)
				if event["id"] != float64(rt.turn.ID) || event["reason"] != tc.reason || !measured || duration < 0 ||
					(tc.name == "timeout" && duration < 2900) || reported != (tc.reason == "completed") || action != tc.action ||
					strings.Contains(line, "private-") {
					t.Fatalf("unexpected music decision: %s", line)
				}
			}
			if count != 1 {
				t.Fatalf("want one music decision, got %d", count)
			}
		})
	}
}

func TestMusicRouteDisabledAndOrdinaryNoAction(t *testing.T) {
	ws, _ := testSocket(t)
	s := NewSession(ws, Config{}, t.TempDir(), nil)
	defer s.cancel()
	rt := musicRequest(s, "你好")
	got, e := s.routeMusic(rt, `{"recent_turns":[]}`)
	if e != nil || got.Handled || got.Stream != nil {
		t.Fatal(got, e)
	}
	s, f := musicRouteSession(t)
	prepareMusicPlan(t, s, "none", "", "")
	got, e = s.routeMusic(musicRequest(s, "他说黄昏很好听"), `{"recent_turns":[]}`)
	if e != nil || got.Handled || got.Stream != nil || f.searches != 0 {
		t.Fatal(got, e, f)
	}
}
func TestMusicRouteSelectsCompleteResourceAndNext(t *testing.T) {
	s, f := musicRouteSession(t)
	prepareMusicPlan(t, s, "play", "黄昏", "周传雄")
	rt := musicRequest(s, "播放周传雄的黄昏")
	got, e := s.routeMusic(rt, `{"recent_turns":[]}`)
	if e != nil || !got.Handled || got.Stream == nil || !rt.media {
		t.Fatal(got, e)
	}
	if f.searches != 1 || f.resolves != 2 {
		t.Fatal("unavailable candidates not skipped", f)
	}
	chunks := 0
	if e = got.Stream(rt.ctx, func(kind string, b []byte) error {
		if kind == "audio" {
			chunks++
			if len(b) != 2880 {
				t.Fatal(len(b))
			}
		}
		return nil
	}); e != nil || chunks != 1 {
		t.Fatal(e, chunks)
	}
	prepareMusicPlan(t, s, "next", "", "")
	got, e = s.routeMusic(musicRequest(s, "下一首"), `{"recent_turns":[]}`)
	if e != nil || got.Stream == nil || f.searches != 1 || f.resolves != 3 {
		t.Fatal(got, e, f)
	}
}
func TestMusicRouteStopRequiresActualFinalEvidence(t *testing.T) {
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprint(final), func(t *testing.T) {
			s, _ := musicRouteSession(t)
			old := musicRequest(s, "播放黄昏")
			old.media = true
			old.turn.Begun = true
			old.turn.Sent = 1440
			old.turn.Cut = true
			old.turn.PlaybackBasis = "i2s_dma"
			old.turn.PlaybackFinal = final
			prepareMusicPlan(t, s, "stop", "", "")
			got, e := s.routeMusic(musicRequest(s, "停一下"), `{"recent_turns":[]}`)
			if e != nil || !got.Handled || got.Stream != nil {
				t.Fatal(got, e)
			}
			if final && !strings.Contains(got.Guidance, `"outcome":"stopped_verified"`) {
				t.Fatal(got.Guidance)
			}
			if !final && !strings.Contains(got.Guidance, `"outcome":"stop_unconfirmed"`) {
				t.Fatal(got.Guidance)
			}
		})
	}
}
func TestMusicRouteNoResultCancelAndSourceFailure(t *testing.T) {
	s, f := musicRouteSession(t)
	f.rows = nil
	prepareMusicPlan(t, s, "play", "不存在", "")
	rt := musicRequest(s, "播放不存在")
	got, e := s.routeMusic(rt, `{"recent_turns":[]}`)
	if e != nil || got.Stream != nil || rt.media || !strings.Contains(got.Guidance, "no_playable_resource") {
		t.Fatal(got, e)
	}
	s, f = musicRouteSession(t)
	prepareMusicPlan(t, s, "play", "黄昏", "周传雄")
	rt = musicRequest(s, "播放黄昏")
	got, e = s.routeMusic(rt, `{"recent_turns":[]}`)
	if e != nil {
		t.Fatal(e)
	}
	rt.cancel()
	if e = got.Stream(rt.ctx, func(string, []byte) error { return context.Canceled }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if f.streams != 0 {
		t.Fatal("cancelled source started")
	}
}

// Model and source resolution may outlast the asynchronous memory checkpoint.
// Pending music classification must not persist a future media turn.
type delayedMusicResolution struct {
	routeMusicSource
	started, release chan struct{}
}

func (d *delayedMusicResolution) Resolve(ctx context.Context, row music.Track) (music.Resolved, error) {
	close(d.started)
	select {
	case <-ctx.Done():
		return music.Resolved{}, ctx.Err()
	case <-d.release:
	}
	return music.Resolved{Track: row}, nil
}
func TestMusicMemoryWaitsForClassification(t *testing.T) {
	server := memoryServer(t, t.TempDir())
	s := memorySession(t, server, memoryDeviceA)
	s.rec.owner.ClientType = "xiaozhi"
	s.cfg.Music = music.New()
	source := &delayedMusicResolution{routeMusicSource: routeMusicSource{rows: []music.Track{{ID: 7, Title: "黄昏", Artist: "周传雄", Duration: time.Minute}}}, started: make(chan struct{}), release: make(chan struct{})}
	s.music = &sessionMusic{source: source}
	prepareMusicPlan(t, s, "play", "黄昏", "周传雄")
	rt := musicRequest(s, "播放周传雄的黄昏")
	done := make(chan struct{})
	var routeErr error
	go func() { defer close(done); _, routeErr = s.routeMusic(rt, `{"recent_turns":[]}`) }()
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("resolution did not start")
	}
	t.Cleanup(func() {
		s.cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("route not reaped")
		}
	})
	check := func(want string) {
		t.Helper()
		if err := s.checkpointMemory(); err != nil {
			t.Fatal(err)
		}
		snapshot, err := s.memory.store.Load(context.Background(), memoryDeviceA)
		if err != nil {
			t.Fatal(err)
		}
		var data memoryData
		if json.Unmarshal(snapshot.Data, &data) != nil {
			t.Fatal("invalid memory")
		}
		if want == "" {
			if len(data.Turns) != 0 {
				t.Fatalf("pending/media request persisted: %+v", data.Turns)
			}
		} else if len(data.Turns) != 1 || data.Turns[0].User != want {
			t.Fatal("ordinary memory lost", data.Turns)
		}
	}
	check("") // Same checkpoint used by the 250ms coalescing worker, during Resolve.
	close(source.release)
	select {
	case <-done:
		if routeErr != nil {
			t.Fatal(routeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("route did not return")
	}
	check("")
	prepareMusicPlan(t, s, "none", "", "")
	if _, err := s.routeMusic(musicRequest(s, "你好"), `{"recent_turns":[]}`); err != nil {
		t.Fatal(err)
	}
	check("你好")
}

// The gate exists to remove a fixed 0.7-0.9s model round-trip from ordinary
// turns, so the assertion must be that no planner connection happened at all.
func TestMusicPlannerSkippedWithoutIntent(t *testing.T) {
	s, _ := musicRouteSession(t)
	reached := 0
	cfg, _ := expressionProvider(t, false, func(c *websocket.Conn) {
		reached++
		musicPlannerTerminal(c, `{"action":"none","title":"","artist":""}`, false)
	})
	s.cfg.URL, s.cfg.Synthetic = cfg.URL, true
	for _, text := range []string{"今天天气不错", "讲个故事", "他说黄昏很好听", "帮我设个闹钟"} {
		got, err := s.routeMusic(musicRequest(s, text), `{"recent_turns":[]}`)
		if err != nil || got.Handled || got.Stream != nil {
			t.Fatalf("%q: unexpected route handled=%t err=%v", text, got.Handled, err)
		}
	}
	if reached != 0 {
		t.Fatalf("planner connected %d times for turns without music intent", reached)
	}
}

// Any instruction during playback may switch or stop the track without naming
// music, so the selected-track case must keep the planner.
func TestMusicPlannerKeptWhileMediaPlaying(t *testing.T) {
	s, _ := musicRouteSession(t)
	old := musicRequest(s, "播放黄昏")
	old.media = true
	old.turn.Begun = true
	old.turn.Sent = 1440
	reached := 0
	cfg, _ := expressionProvider(t, false, func(c *websocket.Conn) {
		reached++
		musicPlannerTerminal(c, `{"action":"stop","title":"","artist":""}`, false)
	})
	s.cfg.URL, s.cfg.Synthetic = cfg.URL, true
	got, err := s.routeMusic(musicRequest(s, "关掉吧"), `{"recent_turns":[]}`)
	if err != nil || !got.Handled || reached != 1 {
		t.Fatalf("instruction during playback lost the planner: handled=%t err=%v calls=%d", got.Handled, err, reached)
	}
}

func TestMemoryRecallIntent(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"今天天气不错", false},
		{"讲个故事", false},
		{"帮我设个闹钟", false},
		{"你还记得那个暗号吗", true},
		{"上次说的那个", true},
		{"接着讲刚才的故事", true},
		{"我之前跟你说过", true},
	} {
		if got := memoryRecallIntent(tc.text); got != tc.want {
			t.Errorf("%q: want %t got %t", tc.text, tc.want, got)
		}
	}
}
