package realtimelive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMusicPlanStrictArguments(t *testing.T) {
	for _, raw := range []string{
		`{"action":"play","title":"黄昏","artist":"周传雄"}`,
		`{"action":"play","title":"","artist":"周传雄"}`,
		`{"action":"stop","title":"","artist":""}`,
		`{"action":"next","title":"","artist":""}`,
		`{"action":"none","title":"","artist":""}`,
		`{"action":"clarify","title":"","artist":""}`,
	} {
		if _, e := musicArguments(raw); e != nil {
			t.Errorf("valid: %s: %v", raw, e)
		}
	}
	for _, raw := range []string{
		`{"action":"play","title":"","artist":""}`,
		`{"action":"stop","title":"黄昏","artist":""}`,
		`{"action":"next","title":"","artist":"周传雄"}`,
		`{"action":"none","title":"","artist":"","url":"https://example.com/a.mp3"}`,
		`{"action":"play","action":"stop","title":"黄昏","artist":"周传雄"}`,
		`{"action":"play","title":null,"artist":"周传雄"}`,
		`{"Action":"play","title":"黄昏","artist":"周传雄"}`,
		`{"action":"play","title":"黄\u0000昏","artist":""}`,
		`{"action":"pause","title":"","artist":""}`,
		`{"action":"play","artist":"周传雄"}`,
		`{"action":"play","title":"` + strings.Repeat("歌", 121) + `","artist":""}`,
		`{"action":"play","title":"黄昏","artist":"周传雄"} {}`,
	} {
		if _, e := musicArguments(raw); e == nil {
			t.Errorf("invalid accepted: %s", raw)
		}
	}
}

func musicPlannerTerminal(c *websocket.Conn, raw string, duplicate bool) {
	c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "music-r1"}})
	c.WriteJSON(map[string]any{"type": "response.function_call_arguments.done", "response_id": "music-r1", "call_id": "music-c1", "name": "submit_music_plan", "arguments": raw})
	output := []any{map[string]any{"type": "function_call", "call_id": "music-c1", "name": "submit_music_plan", "arguments": raw}}
	if duplicate {
		output = append(output, output[0])
	}
	c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "music-r1", "status": "completed", "output": output}})
}

func TestMusicPlannerNativeToolContract(t *testing.T) {
	// Fixtures establish transport/validation; semantic behavior has separate real-model checks.
	for _, tc := range []struct{ text, action, title, artist string }{
		{"播放周传雄的黄昏", "play", "黄昏", "周传雄"},
		{"播放周传雄的歌", "play", "", "周传雄"},
		{"下一首", "next", "", ""},
		{"停一下", "stop", "", ""},
		{"他说播放黄昏是什么意思", "none", "", ""},
		{"别播放音乐", "none", "", ""},
	} {
		t.Run(tc.text, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"action": tc.action, "title": tc.title, "artist": tc.artist})
			cfg, fake := expressionProvider(t, false, func(c *websocket.Conn) { musicPlannerTerminal(c, string(raw), false) })
			p, e := NewNative(cfg, t.TempDir()).PlanMusic(context.Background(), musicInput{CurrentRequest: tc.text, PlayedContext: `{"recent_turns":[]}`, HasSelection: true})
			if e != nil || p.Action != tc.action || p.Title != tc.title || p.Artist != tc.artist {
				t.Fatalf("%+v %v", p, e)
			}
			<-fake.closed
			settings := <-fake.settings
			if settings["enable_search"] != false || settings["audio"] != nil {
				t.Fatal("not silent text-only planner")
			}
			tools, _ := settings["tools"].([]any)
			if len(tools) != 1 || object(object(tools[0])["function"])["name"] != "submit_music_plan" {
				t.Fatal(settings)
			}
			if fake.requests.Load() != 1 {
				t.Fatal("planner must submit once")
			}
		})
	}
}

func TestMusicPlannerRejectsMultipleCallsAndTimesOut(t *testing.T) {
	cfg, f := expressionProvider(t, false, func(c *websocket.Conn) { musicPlannerTerminal(c, `{"action":"stop","title":"","artist":""}`, true) })
	if _, e := NewNative(cfg, t.TempDir()).PlanMusic(context.Background(), musicInput{CurrentRequest: "停一下", PlayedContext: `{"recent_turns":[]}`}); e == nil {
		t.Fatal("duplicate accepted")
	}
	<-f.closed
	cfg, f = expressionProvider(t, true, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, e := NewNative(cfg, t.TempDir()).PlanMusic(ctx, musicInput{CurrentRequest: "播放黄昏", PlayedContext: `{"recent_turns":[]}`}); e == nil {
		t.Fatal("stalled planner accepted")
	}
	<-f.closed
}

func TestMusicPlannerPreservesExplicitVersion(t *testing.T) {
	for _, tc := range []struct {
		request, title string
		fail           bool
	}{
		{"播放周传雄黄昏现场版", "黄昏现场版", false},
		{"播放周传雄黄昏现场版", "黄昏", true},
		{"播放黄昏 Live", "黄昏 Live", false},
		{"播放黄昏 Live", "黄昏", true},
		{"播放黄昏", "黄昏", false},
		{"播放黄昏", "黄昏 Live", true},
	} {
		t.Run(tc.request+tc.title, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"action": "play", "title": tc.title, "artist": "周传雄"})
			cfg, _ := expressionProvider(t, false, func(c *websocket.Conn) { musicPlannerTerminal(c, string(raw), false) })
			p, e := NewNative(cfg, t.TempDir()).PlanMusic(context.Background(), musicInput{CurrentRequest: tc.request, PlayedContext: `{"recent_turns":[]}`})
			if (e != nil) != tc.fail {
				t.Fatal("version not preserved", p, e)
			}
			if !tc.fail && p.Title != tc.title {
				t.Fatal("version changed", p)
			}
		})
	}
}
