package management

import (
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimelive"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func chatFixture(t *testing.T) (Reader, realtimelive.SessionInfo) {
	t.Helper()
	c := authorityFixtureConfig()
	start := c.Bindings[0].From.Add(time.Minute)
	return Reader{Config: c, Root: t.TempDir(), Now: func() time.Time { return start.Add(time.Hour) }}, realtimelive.SessionInfo{SchemaVersion: 1, Ownership: realtimelive.Ownership{SessionID: "0123456789abcdef0123456789abcdef", DeviceID: c.Devices[0].ID, ClientType: "xiaozhi"}, StartedAt: start}
}
func chatWrite(t *testing.T, r Reader, info realtimelive.SessionInfo, dir string, events ...map[string]any) string {
	t.Helper()
	p := filepath.Join(r.Root, dir)
	if e := os.MkdirAll(p, 0700); e != nil {
		t.Fatal(e)
	}
	writeMemoryFixture(t, filepath.Join(p, "session.json"), info)
	var raw []byte
	for _, m := range events {
		m["session_id"] = info.SessionID
		m["device_id"] = info.DeviceID
		m["client_type"] = info.ClientType
		if _, ok := m["at"]; !ok {
			m["at"] = info.StartedAt.Unix()
		}
		b, e := json.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		raw = append(raw, b...)
		raw = append(raw, '\n')
	}
	p = filepath.Join(p, "events.jsonl")
	if e := os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestChatsReaderAndRestart(t *testing.T) {
	r, info := chatFixture(t)
	chatWrite(t, r, info, "source", map[string]any{"event": "user_text", "id": 1, "text": "合成问题"}, map[string]any{"event": "generated", "id": 1, "text": "合成回复", "sent": 0})
	p, e := r.ListChats("alice", "family", ConversationFilter{})
	if e != nil || len(p.Items) != 1 {
		t.Fatalf("%+v %v", p, e)
	}
	c := p.Items[0]
	if c.Status != "已归档" || c.TurnCount != 1 || len(c.Turns) != 0 || len(c.Sources) != 0 {
		t.Fatalf("%+v", c)
	}
	d, e := r.ReadChat("alice", "family", c.ID, nil)
	if e != nil || len(d.Turns) != 1 || len(d.Sources) != 1 || d.Turns[0].SourceRecordID != "source" {
		t.Fatalf("%+v %v", d, e)
	}
	copy := r
	p2, e := copy.ListChats("alice", "family", ConversationFilter{})
	if e != nil || p2.Items[0].ID != c.ID {
		t.Fatal("restart", e)
	}
	if _, e = r.ReadConversation("alice", "family", "source", nil); e != nil {
		t.Fatal(e)
	}
	r.Root = "/missing-synthetic"
	if _, e = r.ListChats("metadata", "family", ConversationFilter{}); !errors.Is(e, ErrForbidden) {
		t.Fatal("permission before source", e)
	}
}

func TestChatsLifecycleEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		fields     map[string]any
		status     string
	}{
		{"pending generation", "generated", map[string]any{"text": "合成回复", "sent": 10}, "待结算"},
		{"missing sent", "turn_error", nil, "待结算"},
		{"queued interrupt", "interrupt", map[string]any{"sent": 10}, "待结算"},
		{"zero interrupt", "interrupt", map[string]any{"sent": 0}, "已归档"},
		{"zero interrupted generation", "generated_interrupted", map[string]any{"sent": 0, "text": ""}, "已归档"},
		{"timeout", "drain_timeout", nil, "已归档"},
		{"progress only", "device_playback", map[string]any{"completed": 10, "accepted": 10, "basis": "i2s_dma"}, "待结算"},
		{"untrusted terminal", "playback", map[string]any{"terminal": "played", "completed": 10, "accepted": 10, "sent": 10, "basis": "other"}, "待结算"},
		{"played terminal", "playback", map[string]any{"terminal": "played", "completed": 10, "accepted": 10, "sent": 10, "basis": "i2s_dma"}, "已归档"},
		{"drained terminal", "playback", map[string]any{"terminal": "drained", "completed": 5, "accepted": 5, "sent": 10, "basis": "i2s_dma"}, "已归档"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, i := chatFixture(t)
			m := tc.fields
			if m == nil {
				m = map[string]any{}
			}
			m["event"] = tc.kind
			m["id"] = 1
			chatWrite(t, r, i, "source", map[string]any{"event": "user_text", "id": 1, "text": "合成"}, m)
			p, e := r.ListChats("alice", "family", ConversationFilter{})
			if e != nil || len(p.Items) != 1 || p.Items[0].Status != tc.status {
				t.Fatalf("%+v %v", p, e)
			}
		})
	}
}
func TestChatsBoundariesReconnectAndSources(t *testing.T) {
	for _, delta := range []time.Duration{29 * time.Minute, 30 * time.Minute, 31 * time.Minute} {
		t.Run(delta.String(), func(t *testing.T) {
			r, i := chatFixture(t)
			r.Now = func() time.Time { return i.StartedAt.Add(3 * time.Hour) }
			chatWrite(t, r, i, "one", map[string]any{"event": "user_text", "id": 1, "text": "合成"}, map[string]any{"event": "generated", "id": 1, "text": "", "sent": 0})
			j := i
			j.SessionID = "1123456789abcdef0123456789abcdef"
			j.StartedAt = i.StartedAt.Add(delta)
			chatWrite(t, r, j, "two", map[string]any{"event": "user_text", "id": 1, "text": "合成二"}, map[string]any{"event": "closed", "reason": "client_stop"})
			p, e := r.ListChats("alice", "family", ConversationFilter{})
			want := 2
			if delta < 30*time.Minute {
				want = 1
			}
			if e != nil || len(p.Items) != want {
				t.Fatalf("%+v %v", p, e)
			}
			if want == 1 && p.Items[0].ConnectionCount != 2 {
				t.Fatal(p)
			}
		})
	}
	r, i := chatFixture(t)
	chatWrite(t, r, i, "one", map[string]any{"event": "user_text", "id": 1, "text": "合成"})
	j := i
	j.SessionID = "1123456789abcdef0123456789abcdef"
	j.StartedAt = i.StartedAt.Add(time.Minute)
	chatWrite(t, r, j, "empty-stop", map[string]any{"event": "closed", "reason": "client_stop"})
	p, e := r.ListChats("alice", "family", ConversationFilter{})
	if e != nil || len(p.Items) != 1 || p.Items[0].Status != "已归档" || p.Items[0].ConnectionCount != 2 {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestChatsPartialAndInvalidTime(t *testing.T) {
	for _, v := range []any{nil, "1", 0, -1, json.Number("1e100"), json.Number("999999999999"), json.Number("0.0000000001")} {
		r, i := chatFixture(t)
		chatWrite(t, r, i, "source", map[string]any{"event": "ping", "at": v})
		if _, e := r.ListChats("alice", "family", ConversationFilter{}); !errors.Is(e, ErrSource) {
			t.Fatalf("%v %v", v, e)
		}
	}
	for _, hasTurn := range []bool{false, true} {
		r, i := chatFixture(t)
		events := []map[string]any{}
		if hasTurn {
			events = append(events, map[string]any{"event": "user_text", "id": 1, "text": "合成"}, map[string]any{"event": "generated", "id": 1, "text": "", "sent": 0})
		}
		p := chatWrite(t, r, i, "source", events...)
		f, e := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		if e != nil {
			t.Fatal(e)
		}
		f.WriteString("{partial")
		f.Close()
		page, e := r.ListChats("alice", "family", ConversationFilter{})
		if e != nil || !page.PendingSources {
			t.Fatalf("%+v %v", page, e)
		}
		if hasTurn && (len(page.Items) != 1 || page.Items[0].Status != "待结算") {
			t.Fatal(page)
		}
		if !hasTurn && len(page.Items) != 0 {
			t.Fatal(page)
		}
	}
	r, i := chatFixture(t)
	chatWrite(t, r, i, "source", map[string]any{"event": "ping", "at": i.StartedAt.Unix() + 2}, map[string]any{"event": "ping", "at": i.StartedAt.Unix() + 1})
	if _, e := r.ListChats("alice", "family", ConversationFilter{}); !errors.Is(e, ErrSource) {
		t.Fatal(e)
	}
}
func TestChatsClosureAndDuplicateSettlement(t *testing.T) {
	for _, reason := range []string{"client_stop", "client_closed", "transport_lost"} {
		r, i := chatFixture(t)
		r.Now = func() time.Time { return i.StartedAt.Add(2 * time.Minute) }
		chatWrite(t, r, i, "source", map[string]any{"event": "user_text", "id": 1, "text": "合成"}, map[string]any{"event": "closed", "reason": reason})
		p, e := r.ListChats("alice", "family", ConversationFilter{})
		want := "进行中"
		if reason == "client_stop" {
			want = "已归档"
		}
		if e != nil || p.Items[0].Status != want {
			t.Fatalf("%+v %v", p, e)
		}
	}
	r, i := chatFixture(t)
	chatWrite(t, r, i, "source", map[string]any{"event": "user_text", "id": 1, "text": "合成"}, map[string]any{"event": "playback", "id": 1, "terminal": "played", "completed": 10, "accepted": 10, "sent": 10, "basis": "i2s_dma"}, map[string]any{"event": "generated", "id": 1, "text": "合成回复", "sent": 10, "at": i.StartedAt.Unix() + 1200}, map[string]any{"event": "playback", "id": 1, "terminal": "played", "completed": 10, "accepted": 10, "sent": 10, "basis": "i2s_dma", "at": i.StartedAt.Unix() + 2400})
	p, e := r.ListChats("alice", "family", ConversationFilter{})
	if e != nil || p.Items[0].EndedAt != i.StartedAt.Add(30*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("%+v %v", p, e)
	}
	d, e := r.ReadChat("alice", "family", p.Items[0].ID, nil)
	if e != nil || d.Turns[0].Played != "合成回复" {
		t.Fatal(d, e)
	}
}
func TestChatsNoUnauthorizedBodiesAndCompleteBudget(t *testing.T) {
	r, i := chatFixture(t)
	p := chatWrite(t, r, i, "source")
	if e := os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(p, 0700); e != nil {
		t.Fatal(e)
	}
	for _, a := range []string{"metadata", "observer"} {
		if _, e := r.ListChats(a, "family", ConversationFilter{}); !errors.Is(e, ErrForbidden) {
			t.Fatal(a, e)
		}
	}
	i.StartedAt = r.Config.Bindings[0].From.Add(-time.Minute)
	writeMemoryFixture(t, filepath.Join(r.Root, "source", "session.json"), i)
	if _, e := r.ListChats("alice", "family", ConversationFilter{}); e != nil {
		t.Fatal("legacy opened", e)
	}
	r, i = chatFixture(t)
	p = chatWrite(t, r, i, "source", map[string]any{"event": "ping"})
	line, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	raw := make([]byte, 0, len(line)*(chatEventLimit+1))
	for n := 0; n <= chatEventLimit; n++ {
		raw = append(raw, line...)
	}
	if e = os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = r.ListChats("alice", "family", ConversationFilter{}); !errors.Is(e, ErrTooLarge) {
		t.Fatal("event limit", e)
	}
}

func TestChatsMidnightBindingAndPagination(t *testing.T) {
	r, i := chatFixture(t)
	i.StartedAt = time.Date(2026, 11, 10, 15, 59, 0, 0, time.UTC)
	r.Now = func() time.Time { return i.StartedAt.Add(3 * time.Hour) }
	// Keep source-time authorization explicit for this midnight fixture.
	r.Config.Bindings[0].From = i.StartedAt.Add(-time.Hour)
	chatWrite(t, r, i, "one", map[string]any{"event": "user_text", "id": 1, "text": "合成"}, map[string]any{"event": "generated", "id": 1, "text": "", "sent": 0})
	j := i
	j.StartedAt = i.StartedAt.Add(2 * time.Minute)
	j.SessionID = "1123456789abcdef0123456789abcdef"
	chatWrite(t, r, j, "two", map[string]any{"event": "user_text", "id": 1, "text": "合成二"}, map[string]any{"event": "generated", "id": 1, "text": "", "sent": 0})
	p, e := r.ListChats("alice", "family", ConversationFilter{Limit: 1})
	if e != nil || len(p.Items) != 1 || p.Items[0].TurnCount != 2 || p.Items[0].Date != "2026-11-10" {
		t.Fatalf("%+v %v", p, e)
	}
	boundary := j.StartedAt
	r.Config.Bindings[0].Until = &boundary
	b := r.Config.Bindings[0]
	b.ID = "next-binding"
	b.Version++
	b.From = boundary
	b.Until = nil
	r.Config.Bindings = append(r.Config.Bindings, b)
	p, e = r.ListChats("alice", "family", ConversationFilter{Limit: 1})
	if e != nil || !p.HasMore || p.Items[0].TurnCount != 1 {
		t.Fatalf("%+v %v", p, e)
	}
	p2, e := r.ListChats("alice", "family", ConversationFilter{Limit: 1, Offset: 1})
	if e != nil || p2.HasMore || len(p2.Items) != 1 || p2.Items[0].ID == p.Items[0].ID {
		t.Fatal(p2, e)
	}
}
func TestChatsManifestFallbackAndInvalidCompleteSources(t *testing.T) {
	for _, kind := range []string{"fallback", "closed-first", "reason-conflict", "malformed", "foreign", "duplicate-key", "future", "symlink", "duplicate-session"} {
		t.Run(kind, func(t *testing.T) {
			r, i := chatFixture(t)
			end := i.StartedAt.Add(time.Minute)
			i.EndedAt = &end
			i.EndReason = "client_stop"
			events := []map[string]any{{"event": "user_text", "id": 1, "text": "合成"}}
			if kind == "closed-first" || kind == "reason-conflict" {
				reason := "client_stop"
				if kind == "reason-conflict" {
					reason = "client_closed"
				}
				events = append(events, map[string]any{"event": "closed", "reason": reason})
			}
			if kind == "future" {
				events[0]["at"] = r.now().Add(time.Hour).Unix()
			}
			p := chatWrite(t, r, i, "source", events...)
			switch kind {
			case "malformed":
				os.WriteFile(p, []byte("{bad}\n"), 0600)
			case "foreign":
				raw, _ := os.ReadFile(p)
				raw = []byte(strings.ReplaceAll(string(raw), i.DeviceID, r.Config.Devices[1].ID))
				os.WriteFile(p, raw, 0600)
			case "duplicate-key":
				raw, _ := os.ReadFile(p)
				raw = append([]byte("{\"at\":1,"), raw[1:]...)
				os.WriteFile(p, raw, 0600)
			case "symlink":
				os.Remove(p)
				os.Symlink("session.json", p)
			case "duplicate-session":
				chatWrite(t, r, i, "another", events...)
			}
			page, e := r.ListChats("alice", "family", ConversationFilter{})
			if kind == "fallback" || kind == "closed-first" {
				want := end
				if kind == "closed-first" {
					want = i.StartedAt
				}
				if e != nil || page.Items[0].EndedAt != want.Format(time.RFC3339Nano) {
					t.Fatalf("%+v %v", page, e)
				}
			} else if !errors.Is(e, ErrSource) {
				t.Fatal(kind, e)
			}
		})
	}
}
func TestChatsTotalTurnAndByteLimits(t *testing.T) {
	for _, kind := range []string{"turns", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			r, i := chatFixture(t)
			for n := 0; n < 6; n++ {
				i.SessionID = fmt.Sprintf("%032x", n+1)
				dir := fmt.Sprintf("source-%d", n)
				if kind == "bytes" {
					p := chatWrite(t, r, i, dir)
					f, e := os.OpenFile(p, os.O_WRONLY, 0)
					if e != nil {
						t.Fatal(e)
					} // Complete valid ignored events fill about 12 MiB per source.
					m := map[string]any{"event": "ping", "at": i.StartedAt.Unix(), "session_id": i.SessionID, "device_id": i.DeviceID, "client_type": i.ClientType, "padding": strings.Repeat("x", 1<<20)}
					line, _ := json.Marshal(m)
					line = append(line, '\n')
					for k := 0; k < 12; k++ {
						if _, e = f.Write(line); e != nil {
							t.Fatal(e)
						}
					}
					f.Close()
				} else {
					events := []map[string]any{}
					for id := 1; id <= 2000; id++ {
						events = append(events, map[string]any{"event": "turn_error", "id": id})
					}
					chatWrite(t, r, i, dir, events...)
				}
			}
			if _, e := r.ListChats("alice", "family", ConversationFilter{}); !errors.Is(e, ErrTooLarge) {
				t.Fatal(kind, e)
			}
		})
	}
}

func TestChatsPartialTailClosedSource(t *testing.T) {
	for _, completeClose := range []bool{false, true} {
		for _, reason := range []string{"transport_lost", "client_stop"} {
			t.Run(fmt.Sprintf("%s/complete-close=%v", reason, completeClose), func(t *testing.T) {
				r, i := chatFixture(t)
				end := i.StartedAt.Add(time.Minute)
				i.EndedAt, i.EndReason = &end, reason
				events := []map[string]any{{"event": "user_text", "id": 1, "text": "合成"}, {"event": "generated", "id": 1, "text": "", "sent": 0}}
				if completeClose {
					events = append(events, map[string]any{"event": "closed", "at": end.Unix(), "reason": reason})
				}
				p := chatWrite(t, r, i, "source", events...)
				f, e := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = f.WriteString("{partial"); e != nil {
					t.Fatal(e)
				}
				if e = f.Close(); e != nil {
					t.Fatal(e)
				}
				page, e := r.ListChats("alice", "family", ConversationFilter{})
				if e != nil || len(page.Items) != 1 || !page.PendingSources {
					t.Fatalf("%+v %v", page, e)
				}
				c := page.Items[0]
				if reason == "client_stop" {
					if c.Status != "已归档" || c.ArchiveReason != "explicit_end" || c.EndReason != "explicit_end" || c.EndedAt != end.Format(time.RFC3339Nano) {
						t.Fatalf("%+v", c)
					}
				} else if c.Status != "待结算" || c.EndedAt != "" || c.ArchiveReason != "" {
					t.Fatalf("%+v", c)
				}
			})
		}
	}
}
