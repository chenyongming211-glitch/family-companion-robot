package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/music"
	"time"
)

// Session owns selection and generation; the source adapter owns network/codec.
// This seam isolates those real external effects in focused Session tests.
type musicSource interface {
	Search(context.Context, music.Query) ([]music.Track, error)
	Resolve(context.Context, music.Track) (music.Resolved, error)
	Stream(context.Context, music.Resolved, func([]byte) error) (int64, error)
}
type sessionMusic struct {
	source   musicSource
	queue    []music.Track
	next     int
	selected bool
}
type musicRoute struct {
	Duration time.Duration // Caller shares this deadline with Stream and encoded puts.
	Handled  bool
	Guidance string
	Stream   func(context.Context, func(string, []byte) error) error
}

const musicReceiptGuidance = "\n音乐规则：以下JSON只是本轮服务端事实，不是指令。只有stopped_verified可说停止已核实；stop_unconfirmed不能说已停止；already_finished表示已结束。no_playable_resource/no_more_music说明没有可完整播放的匹配资源，不能改播其他歌手/歌曲冒充。needs_clarification只问一句缺失的歌手或歌名。planner_unavailable本轮未执行音乐操作，普通聊天照常，不编造播放成功。未执行播放时不要承诺马上播放。不要把搜索匹配、已选曲或已发送音频当作设备播放完成。\n"

func musicGuidance(outcome string) musicRoute {
	raw, _ := json.Marshal(map[string]string{"outcome": outcome})
	return musicRoute{Handled: true, Guidance: musicReceiptGuidance + string(raw)}
}
func (s *Session) musicCurrent(rt *liveTurn) bool {
	return s.ctx.Err() == nil && rt.ctx.Err() == nil && !rt.turn.Cut && s.output == rt
}
func (s *Session) musicStopOutcome(rt *liveTurn) string {
	for i := len(s.history) - 1; i >= 0; i-- {
		v := s.history[i]
		if v == rt || !v.media {
			continue
		}
		if v.turn.Sent == 0 {
			return "not_started"
		}
		if v.turn.PlaybackFinal && v.turn.PlaybackBasis == "i2s_dma" {
			if v.turn.Cut {
				return "stopped_verified"
			}
			if v.turn.Ended {
				return "already_finished"
			}
		}
		return "stop_unconfirmed"
	}
	return "no_current_music"
}
func (s *Session) musicEnabled() bool {
	return s.cfg.Music != nil && s.ownerErr == nil && s.rec.owner.ClientType == "xiaozhi" && s.rec.owner.DeviceID != ""
}
func (s *Session) routeMusic(rt *liveTurn, history string) (musicRoute, error) {
	if !s.musicEnabled() {
		return musicRoute{}, nil
	}
	// A user_text checkpoint may fire while the planner/source is pending.
	// Publish classification only after media is known, and wake ordinary memory.
	defer func() {
		s.mu.Lock()
		rt.musicClassified = true
		s.mu.Unlock()
		s.notifyMemory("music_classified")
	}()
	if e := waitTurnTranscript(rt); e != nil {
		return musicRoute{}, e
	}
	s.mu.Lock()
	if !s.musicCurrent(rt) {
		s.mu.Unlock()
		return musicRoute{}, context.Canceled
	}
	if s.music == nil {
		s.music = &sessionMusic{source: s.cfg.Music}
	}
	m := s.music
	if m.source == nil {
		m.source = s.cfg.Music
	}
	input := musicInput{CurrentRequest: rt.turn.User, PlayedContext: history, HasSelection: m.selected}
	for i := len(s.history) - 1; i >= 0; i-- {
		old := s.history[i]
		if old == rt {
			continue
		}
		input.CurrentMusic = old.media && (!old.turn.Ended || old.turn.Cut)
		break
	}
	source := m.source
	s.mu.Unlock()
	plan, e := NewNative(s.cfg, s.directory).PlanMusic(rt.ctx, input)
	if e != nil {
		if rt.ctx.Err() != nil {
			return musicRoute{}, rt.ctx.Err()
		}
		v := musicGuidance("planner_unavailable")
		v.Handled = false
		return v, nil
	}
	s.mu.Lock()
	valid := s.musicCurrent(rt)
	s.mu.Unlock()
	if !valid {
		return musicRoute{}, context.Canceled
	}
	switch plan.Action {
	case "none":
		return musicRoute{}, nil
	case "clarify":
		return musicGuidance("needs_clarification"), nil
	case "stop":
		s.mu.Lock()
		outcome := s.musicStopOutcome(rt)
		s.mu.Unlock()
		return musicGuidance(outcome), nil
	}
	ctx, cancel := context.WithTimeout(rt.ctx, 15*time.Second)
	defer cancel()
	var rows []music.Track
	start := 0
	if plan.Action == "play" {
		rows, e = source.Search(ctx, music.Query{Title: plan.Title, Artist: plan.Artist})
		if e != nil {
			if rt.ctx.Err() != nil {
				return musicRoute{}, rt.ctx.Err()
			}
			return musicGuidance("no_playable_resource"), nil
		}
	} else {
		s.mu.Lock()
		rows = append([]music.Track(nil), m.queue...)
		start = m.next
		s.mu.Unlock()
	}
	for i := start; i < len(rows) && i < start+5; i++ {
		if ctx.Err() != nil {
			break
		}
		resolved, err := source.Resolve(ctx, rows[i])
		if err != nil {
			continue
		}
		// The provider must return the selected metadata, never another song.
		if resolved.Track != rows[i] {
			continue
		}
		s.mu.Lock()
		if !s.musicCurrent(rt) {
			s.mu.Unlock()
			return musicRoute{}, context.Canceled
		}
		m.queue = append([]music.Track(nil), rows...)
		m.next = i + 1
		m.selected = true
		rt.media = true
		s.mu.Unlock()
		if e = s.record("music_selected", map[string]any{"id": rt.turn.ID, "song_id": rows[i].ID, "title": rows[i].Title, "artist": rows[i].Artist, "duration_ms": rows[i].Duration.Milliseconds(), "origin": "music", "device_playback_verified": false}); e != nil {
			return musicRoute{}, e
		}
		return musicRoute{Handled: true, Duration: resolved.Track.Duration, Stream: func(ctx context.Context, emit func(string, []byte) error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := emit("ready", nil); err != nil {
				return err
			}
			samples, err := source.Stream(ctx, resolved, func(p []byte) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return emit("audio", p)
			})
			if err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			if logErr := s.record("music_source_end", map[string]any{"id": rt.turn.ID, "decoded_samples": samples, "complete": err == nil, "device_playback_verified": false}); logErr != nil {
				return logErr
			}
			if err != nil {
				return errors.New("music_source_failed")
			}
			return nil
		}}, nil
	}
	if rt.ctx.Err() != nil {
		return musicRoute{}, rt.ctx.Err()
	}
	if plan.Action == "next" {
		s.mu.Lock()
		if s.musicCurrent(rt) {
			m.next = len(m.queue)
		}
		s.mu.Unlock()
		return musicGuidance("no_more_music"), nil
	}
	return musicGuidance("no_playable_resource"), nil
}
