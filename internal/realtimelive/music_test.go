package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/music"
	"fmt"
	"github.com/gorilla/websocket"
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
