package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/music"
	"strings"
	"testing"
	"time"
)

func parseMusicFacts(guidance string) (map[string]any, error) {
	for _, line := range strings.Split(guidance, "\n") {
		if raw, ok := strings.CutPrefix(line, "MUSIC_PLAYBACK_FACTS="); ok {
			var facts map[string]any
			if err := json.Unmarshal([]byte(raw), &facts); err != nil {
				return nil, err
			}
			return facts, nil
		}
	}
	return nil, errors.New("current song metadata missing from reply guidance")
}
func musicFacts(t *testing.T, guidance string) map[string]any {
	t.Helper()
	facts, err := parseMusicFacts(guidance)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}
func selectMusicForFacts(t *testing.T, s *Session) *liveTurn {
	t.Helper()
	prepareMusicPlan(t, s, "play", "黄昏", "周传雄")
	old := musicRequest(s, "播放周传雄的黄昏")
	route, err := s.routeMusic(old, `{"recent_turns":[]}`)
	if err != nil || route.Stream == nil {
		t.Fatal("selection", err)
	}
	return old
}
func TestMusicFactsStateUsesActualPlaybackEvidence(t *testing.T) {
	for _, state := range []string{"cut_before_start", "selected_not_started", "sent_unconfirmed", "playing", "completion_unconfirmed", "stop_unconfirmed", "stopped", "finished", "unknown"} {
		t.Run(state, func(t *testing.T) {
			s, _ := musicRouteSession(t)
			old := selectMusicForFacts(t, s)
			s.mu.Lock()
			switch state {
			case "cut_before_start":
				old.turn.Cut = true
			case "sent_unconfirmed":
				old.turn.Begun = true
				old.turn.Sent = 2880
			case "playing":
				old.turn.Begun = true
				old.turn.Sent = 2880
				old.turn.Completed = 1440
				old.turn.PlaybackBasis = "i2s_dma"
			case "completion_unconfirmed":
				old.turn.Begun = true
				old.turn.Sent = 2880
				old.turn.Completed = 1440
				old.turn.PlaybackBasis = "i2s_dma"
				old.turn.Ended = true
			case "stop_unconfirmed":
				old.turn.Begun = true
				old.turn.Sent = 2880
				old.turn.Cut = true
			case "stopped":
				old.turn.Begun = true
				old.turn.Sent = 2880
				old.turn.Completed = 1440
				old.turn.PlaybackBasis = "i2s_dma"
				old.turn.Cut = true
				old.turn.PlaybackFinal = true
			case "finished":
				old.turn.Begun = true
				old.turn.Sent = 2880
				old.turn.Completed = 2880
				old.turn.PlaybackBasis = "i2s_dma"
				old.turn.Ended = true
				old.turn.PlaybackFinal = true
			case "unknown":
				old.turn.Begun = true
				old.turn.Sent = 2880
				old.turn.Completed = 1440
				old.turn.PlaybackBasis = "client_clock"
			}
			s.mu.Unlock()
			prepareMusicPlan(t, s, "none", "", "")
			route, err := s.routeMusic(musicRequest(s, "你唱的什么歌曲，谁唱的？"), `{"recent_turns":[]}`)
			if err != nil || route.Handled || route.Stream != nil {
				t.Fatal("query must remain ordinary speech", err)
			}
			facts := musicFacts(t, route.Guidance)
			expectedState := state
			if state == "cut_before_start" {
				expectedState = "selected_not_started"
			}
			track, ok := facts["selection"].(map[string]any)
			if !ok || track["song_id"] != float64(2) || track["title"] != "黄昏" || track["artist"] != "周传雄" || facts["state"] != expectedState {
				t.Fatal(facts)
			}
		})
	}
}
func TestMusicFactsSurviveCandidateExhaustionAndFailedNewSelection(t *testing.T) {
	s, f := musicRouteSession(t)
	selectMusicForFacts(t, s)
	f.unavailable[3] = true
	prepareMusicPlan(t, s, "next", "", "")
	if r, err := s.routeMusic(musicRequest(s, "下一首"), `{"recent_turns":[]}`); err != nil || !strings.Contains(r.Guidance, "no_more_music") {
		t.Fatal(r, err)
	}
	f.rows = []music.Track{{ID: 99, Title: "另一首", Artist: "别的歌手", Duration: time.Minute}}
	f.unavailable[99] = true
	prepareMusicPlan(t, s, "play", "另一首", "别的歌手")
	if r, err := s.routeMusic(musicRequest(s, "播放另一首"), `{"recent_turns":[]}`); err != nil || !strings.Contains(r.Guidance, "no_playable_resource") {
		t.Fatal(r, err)
	}
	prepareMusicPlan(t, s, "none", "", "")
	r, err := s.routeMusic(musicRequest(s, "刚才什么歌"), `{"recent_turns":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := musicFacts(t, r.Guidance)["selection"].(map[string]any)["song_id"]; got != float64(2) {
		t.Fatal("cursor/failed resolution replaced real selection", got)
	}
	if f.streams != 0 {
		t.Fatal("metadata query started audio", f.streams)
	}
}
func TestMusicFactsAbsentSelectionAndSessionIsolation(t *testing.T) {
	a, _ := musicRouteSession(t)
	selectMusicForFacts(t, a)
	b, f := musicRouteSession(t)
	prepareMusicPlan(t, b, "none", "", "")
	route, err := b.routeMusic(musicRequest(b, "刚才谁唱的"), `{"recent_turns":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	facts := musicFacts(t, route.Guidance)
	if facts["selection"] != nil || facts["state"] != "no_selection" || f.searches != 0 || f.resolves != 0 || f.streams != 0 {
		t.Fatal("unselected Session inherited music", facts)
	}
	b.ownerErr = errors.New("unauthorized")
	route, err = b.routeMusic(musicRequest(b, "你在放什么歌"), `{"recent_turns":[]}`)
	if err != nil || route.Guidance != "" {
		t.Fatal("unauthorized session exposed facts", route, err)
	}
}
func TestMusicFactsQueryReachesRealOrdinaryReplyPrompt(t *testing.T) {
	s, c := expressionSession(t)
	s.cfg.Music = music.New()
	s.rec.owner = Ownership{DeviceID: memoryDeviceA, ClientType: "xiaozhi", SessionID: "query-test"}
	source := &routeMusicSource{rows: []music.Track{{ID: 22, Title: "黄昏 (Live)", Artist: "周传雄", Duration: time.Minute}}, unavailable: map[int64]bool{}}
	s.music = &sessionMusic{source: source}
	old := selectMusicForFacts(t, s)
	s.mu.Lock()
	old.turn.Begun = true
	old.turn.Sent = 1440
	old.turn.Completed = 1440
	old.turn.Cut = true
	old.turn.PlaybackFinal = true
	old.turn.PlaybackBasis = "i2s_dma"
	s.mu.Unlock()
	prepareMusicPlan(t, s, "none", "", "")
	s.generate = func(_ context.Context, _ int64, prompt string, _ []byte, emit func(string, []byte) error) error {
		facts, err := parseMusicFacts(prompt)
		if err != nil {
			t.Error(err)
		} else if facts["selection"].(map[string]any)["title"] != "黄昏 (Live)" || facts["state"] != "stopped" {
			t.Errorf("wrong metadata: %v", facts)
		}
		if err := emit("pcm", make([]byte, 2880)); err != nil {
			return err
		}
		return emit("text", []byte("刚才播放的是周传雄的《黄昏》现场版。"))
	}
	rt := musicRequest(s, "你唱的什么歌曲？")
	done := startMediaReply(t, s, rt)
	for {
		_, _, event := readMediaMessage(t, c)
		if event["type"] == "tts" && event["state"] == "stop" {
			break
		}
	}
	awaitMediaReply(t, done)
	s.mu.Lock()
	defer s.mu.Unlock()
	if rt.media || rt.asr == nil || rt.turn.Generated == "" || source.searches != 1 || source.streams != 0 {
		t.Fatal("ordinary query behavior changed")
	}
}
