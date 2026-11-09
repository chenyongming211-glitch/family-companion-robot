package management

import (
	"encoding/json"
	"errors"
	"familyrobot/internal/deviceprofile"
	"familyrobot/internal/realtimelive"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func projectionFixture() (realtimelive.SessionInfo, func(string, map[string]any) []byte) {
	i := realtimelive.SessionInfo{Ownership: realtimelive.Ownership{SessionID: strings.Repeat("a", 32), DeviceID: "dev_" + strings.Repeat("b", 32), ClientType: "xiaozhi"}}
	return i, func(k string, m map[string]any) []byte {
		m["event"] = k
		m["session_id"] = i.SessionID
		m["device_id"] = i.DeviceID
		m["client_type"] = i.ClientType
		b, _ := json.Marshal(m)
		return append(b, '\n')
	}
}
func TestProjectionTrust(t *testing.T) {
	for _, tc := range []struct {
		name    string
		receipt map[string]any
		bad     bool
		played  bool
	}{
		{"full", map[string]any{"completed": 24000, "accepted": 24000, "sent": 24000, "basis": "i2s_dma", "terminal": "played"}, false, true},
		{"missing basis", map[string]any{"completed": 24000, "accepted": 24000, "sent": 24000, "terminal": "played"}, false, false},
		{"wrong basis", map[string]any{"completed": 24000, "accepted": 24000, "sent": 24000, "terminal": "played", "basis": "fake"}, false, false},
		{"missing accepted", map[string]any{"completed": 24000, "sent": 24000, "terminal": "played", "basis": "i2s_dma"}, false, false},
		{"negative", map[string]any{"completed": -1, "accepted": 24000, "sent": 24000}, true, false},
		{"contradiction", map[string]any{"completed": 12000, "accepted": 24000, "sent": 24000, "terminal": "played", "basis": "i2s_dma"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, e := projectionFixture()
			r := e("user_text", map[string]any{"id": 1, "text": "问"})
			tc.receipt["id"] = 1
			r = append(r, e("playback", tc.receipt)...)
			r = append(r, e("generated", map[string]any{"id": 1, "text": "答", "sent": 24000})...)
			ts, _, err := project(r, i)
			if tc.bad {
				if err == nil {
					t.Fatal("accepted invalid")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (ts[0].Playback == "played") != tc.played {
				t.Fatalf("%+v", ts)
			}
		})
	}
}
func TestProjectionOversizedTail(t *testing.T) {
	i, _ := projectionFixture()
	if _, _, e := project([]byte(strings.Repeat("x", (2<<20)+1)), i); e == nil {
		t.Fatal("oversized tail accepted")
	}
}

func TestProjectionConservativeASR(t *testing.T) {
	i, e := projectionFixture()
	r := e("user_text", map[string]any{"id": 7, "text": "问"})
	r = append(r, e("generated_interrupted", map[string]any{"id": 7, "text": "你好世界", "sent": 24000})...)
	r = append(r, e("playback", map[string]any{"id": 7, "completed": 12000, "accepted": 12000, "sent": 24000, "terminal": "drained", "basis": "i2s_dma"})...)
	r = append(r, e("output_asr_final", map[string]any{"id": 7, "sentence": map[string]any{"sentence_id": 1, "sentence_end": true, "words": []any{map[string]any{"text": "你好", "begin_time": 0, "end_time": 300}, map[string]any{"text": "世界", "begin_time": 300, "end_time": 400}}}})...)
	ts, _, err := project(r, i)
	if err != nil || len(ts) != 1 || ts[0].Played != "你好" || ts[0].Playback != "interrupted" || ts[0].ExactStopKnown || !ts[0].RecognitionMayDiffer || ts[0].TextSource != "output_asr" {
		t.Fatalf("%+v %v", ts, err)
	}
}
func TestProjectionUnknownAndInvalid(t *testing.T) {
	for _, kind := range []string{"generated", "generated_interrupted"} {
		i, e := projectionFixture()
		r := e("user_text", map[string]any{"id": 1, "text": "问"})
		r = append(r, e(kind, map[string]any{"id": 1, "text": "答", "sent": 24000})...)
		ts, _, err := project(r, i)
		if err != nil || ts[0].Played != "" || ts[0].Playback == "played" {
			t.Fatalf("%+v %v", ts, err)
		}
	}
	for _, tc := range []struct {
		name string
		raw  func(func(string, map[string]any) []byte) []byte
	}{
		{"rejected", func(e func(string, map[string]any) []byte) []byte {
			return e("input_rejected", map[string]any{"id": 4})
		}},
		{"preview", func(e func(string, map[string]any) []byte) []byte { return e("input_preview", map[string]any{"id": 4}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, e := projectionFixture()
			ts, _, err := project(tc.raw(e), i)
			if err != nil || len(ts) != 0 {
				t.Fatalf("%+v %v", ts, err)
			}
		})
	}
	i, e := projectionFixture()
	r := e("user_text", map[string]any{"id": 1, "text": "问"})
	r = []byte(strings.ReplaceAll(string(r), i.SessionID, strings.Repeat("f", 32)))
	if _, _, err := project(r, i); err == nil {
		t.Fatal("foreign session accepted")
	}
	for _, id := range []any{0, -1, 1.5, "1"} {
		if _, _, err := project(e("user_text", map[string]any{"id": id, "text": "问"}), i); err == nil {
			t.Fatalf("bad id %v", id)
		}
	}
	r = e("user_text", map[string]any{"id": 1, "text": "问"})
	r = append(r, e("device_playback", map[string]any{"id": 1, "completed": 12000, "accepted": 12000, "basis": "i2s_dma"})...)
	r = append(r, e("output_asr_final", map[string]any{"id": 1, "sentence": map[string]any{"sentence_id": 1, "sentence_end": true, "words": []any{map[string]any{"text": "你好", "begin_time": 0, "end_time": 200}}}})...)
	ts, _, err := project(r, i)
	if err != nil || ts[0].Played != "" || ts[0].Boundary != "unknown_playback" {
		t.Fatalf("missing totals: %+v %v", ts, err)
	}
}
func readerFixture(t *testing.T) (Reader, func(string, string, time.Time)) {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	i, _ := projectionFixture()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	other := "dev_" + strings.Repeat("c", 32)
	c := Config{Accounts: []AccountConfig{{ID: "a", Memberships: []Membership{{FamilyID: "f", Devices: []DeviceGrant{{DeviceID: i.DeviceID, CanReadBody: true}}}}}, {ID: "metadata", Memberships: []Membership{{FamilyID: "f", Devices: []DeviceGrant{{DeviceID: i.DeviceID}}}}}, {ID: "admin", InternalAdmin: true}}, Devices: []DeviceConfig{{ID: i.DeviceID, Name: "默认", Model: "微雪 ESP32-S3"}, {ID: other}}, Bindings: []Binding{{DeviceID: i.DeviceID, FamilyID: "f", From: start}, {DeviceID: other, FamilyID: "g", From: start}}}
	r := Reader{Config: c, Root: root, Now: func() time.Time { return start.Add(time.Hour) }}
	return r, func(id, dev string, at time.Time) {
		t.Helper()
		s := i
		s.SchemaVersion = 1
		s.DeviceID = dev
		s.StartedAt = at
		p := filepath.Join(root, id)
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(s)
		if err := os.WriteFile(filepath.Join(p, "session.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "events.jsonl"), []byte("invalid body\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestReaderIsolationPagingAndMetadata(t *testing.T) {
	r, add := readerFixture(t)
	id := r.Config.Devices[0].ID
	start := r.Config.Bindings[0].From
	add("old", id, start.Add(-time.Second))
	add("one", id, start)
	add("two", id, start.Add(time.Minute))
	add("foreign", r.Config.Devices[1].ID, start)
	p, e := r.ListConversations("metadata", "family", ConversationFilter{Limit: 1})
	if e != nil || len(p.Items) != 1 || p.Items[0].ID != "two" || p.Items[0].BodyAvailable || !p.HasMore || len(p.Items[0].Turns) != 0 {
		t.Fatalf("%+v %v", p, e)
	}
	p, e = r.ListConversations("metadata", "family", ConversationFilter{Limit: 1, Offset: 1})
	if e != nil || len(p.Items) != 1 || p.Items[0].ID != "one" || p.HasMore {
		t.Fatalf("%+v %v", p, e)
	}
	for _, a := range []string{"metadata", "admin"} {
		if _, e := r.ReadConversation(a, "family", "one", nil); e == nil {
			t.Fatalf("body allowed %s", a)
		}
	}
	for _, record := range []string{"old", "foreign"} {
		if _, e := r.ReadConversation("a", "family", record, nil); !errors.Is(e, ErrNotFound) {
			t.Fatalf("%s %v", record, e)
		}
	}
	for _, filter := range []string{"", "bad", r.Config.Devices[1].ID} {
		if _, e := r.ListConversations("a", "family", ConversationFilter{DeviceID: &filter}); e == nil {
			t.Fatal("foreign/invalid filter accepted")
		}
	}
	if _, e := r.ListDevices("a", "admin"); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	d, e := r.GetDevice("a", "family", id)
	if e != nil || d.LastSeen != start.Add(time.Minute).Format(time.RFC3339) || d.Connection != "latest_connection" || d.Battery != nil || d.Volume != nil || d.Brightness != nil {
		t.Fatalf("%+v %v", d, e)
	}
	r.ProfilesPath = filepath.Join(r.Root, "profiles.json")
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "profiles": map[string]any{id: map[string]any{"name": "小满", "revision": 3}}})
	if e := os.WriteFile(r.ProfilesPath, b, 0600); e != nil {
		t.Fatal(e)
	}
	_, pe := deviceprofile.Open(r.ProfilesPath)
	if pe != nil {
		t.Fatal(pe)
	}
	d, e = r.GetDevice("a", "family", id)
	if e != nil || d.Name != "小满" || d.Revision == nil || *d.Revision != 3 {
		t.Fatalf("%+v %v", d, e)
	}
}
func TestReaderSafeFiles(t *testing.T) {
	for _, file := range []string{"session.json", "events.jsonl"} {
		t.Run(file, func(t *testing.T) {
			r, add := readerFixture(t)
			add("one", r.Config.Devices[0].ID, r.Config.Bindings[0].From)
			outside := filepath.Join(t.TempDir(), "private")
			os.WriteFile(outside, []byte("secret"), 0600)
			p := filepath.Join(r.Root, "one", file)
			os.Remove(p)
			if e := os.Symlink(outside, p); e != nil {
				t.Fatal(e)
			}
			if _, e := r.ReadConversation("a", "family", "one", nil); e == nil {
				t.Fatal("symlink accepted")
			}
		})
	}
	r, add := readerFixture(t)
	add("one", r.Config.Devices[0].ID, r.Config.Bindings[0].From)
	for _, id := range []string{"../one", "one/../one", "/one", "one\\x", ""} {
		if _, e := r.ReadConversation("a", "family", id, nil); !errors.Is(e, ErrInvalid) {
			t.Fatalf("%q %v", id, e)
		}
	}
	wrong := r.Config.Devices[1].ID
	if _, e := r.ReadConversation("a", "family", "one", &wrong); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	root, e := openRecordRoot(r.Root)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	if _, e := regular(root, "../private", 10); e == nil {
		t.Fatal("root escape accepted")
	}
	os.WriteFile(filepath.Join(r.Root, "large"), []byte("123456"), 0600)
	if _, e := regular(root, "large", 5); !errors.Is(e, ErrTooLarge) {
		t.Fatal(e)
	}
}

func TestProjectionMoreInvalidEvidence(t *testing.T) {
	for _, name := range []string{"regression", "conflicting_terminal", "oversent", "fractional", "duplicate_owner", "missing_terminal"} {
		t.Run(name, func(t *testing.T) {
			i, e := projectionFixture()
			r := e("user_text", map[string]any{"id": 1, "text": "问"})
			r = append(r, e("generated", map[string]any{"id": 1, "text": "答", "sent": 24000})...)
			m := map[string]any{"id": 1, "completed": 24000, "accepted": 24000, "sent": 24000, "terminal": "played", "basis": "i2s_dma"}
			switch name {
			case "regression":
				r = append(r, e("device_playback", map[string]any{"id": 1, "completed": 24000, "accepted": 24000, "basis": "i2s_dma"})...)
				m["completed"] = 12000
				m["accepted"] = 12000
				m["terminal"] = "drained"
			case "conflicting_terminal":
				r = append(r, e("playback", m)...)
				m["terminal"] = "drained"
			case "oversent":
				m["completed"] = 48000
				m["accepted"] = 48000
			case "fractional":
				m["completed"] = 1.5
			case "duplicate_owner":
				r = []byte(strings.Replace(string(r), `"event":"user_text"`, `"event":"user_text","session_id":"foreign"`, 1))
			case "missing_terminal":
				r = append(r, e("device_playback", m)...)
				ts, _, err := project(r, i)
				if err != nil || ts[0].Playback == "played" || ts[0].Played != "" {
					t.Fatalf("%+v %v", ts, err)
				}
				return
			}
			r = append(r, e("playback", m)...)
			if _, _, err := project(r, i); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
func TestReaderDirectoryAndNonregular(t *testing.T) {
	r, add := readerFixture(t)
	id := r.Config.Devices[0].ID
	add("one", id, r.Config.Bindings[0].From)
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(r.Root, alias); e != nil {
		t.Fatal(e)
	}
	r2 := r
	r2.Root = alias
	if _, e := r2.ListConversations("a", "family", ConversationFilter{}); e == nil {
		t.Fatal("symlink root accepted")
	}
	if e := os.Symlink(filepath.Join(r.Root, "one"), filepath.Join(r.Root, "alias")); e != nil {
		t.Fatal(e)
	}
	if _, e := r.ReadConversation("a", "family", "alias", nil); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	p := filepath.Join(r.Root, "one", "events.jsonl")
	os.Remove(p)
	os.Mkdir(p, 0700)
	if _, e := r.ReadConversation("a", "family", "one", nil); e == nil {
		t.Fatal("nonregular accepted")
	}
}

func TestRecordReplacedReadBound(t *testing.T) {
	rootPath := t.TempDir()
	root, e := openRecordRoot(rootPath)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	p := filepath.Join(rootPath, "changing")
	if e := os.WriteFile(p, []byte("small"), 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			tmp := filepath.Join(rootPath, "replacement")
			raw := []byte("small")
			if i%2 == 0 {
				raw = []byte(strings.Repeat("x", 8192))
			}
			if os.WriteFile(tmp, raw, 0600) == nil {
				os.Rename(tmp, p)
			}
		}
	}()
	for i := 0; i < 100; i++ {
		b, e := regular(root, "changing", 32)
		if e == nil && len(b) > 32 {
			t.Fatal("unbounded replacement read")
		}
	}
	<-done
}

func TestReaderDetailAndSourceType(t *testing.T) {
	r, add := readerFixture(t)
	id := r.Config.Devices[0].ID
	add("one", id, r.Config.Bindings[0].From)
	i, e := projectionFixture()
	raw := e("user_text", map[string]any{"id": 1, "text": "问"})
	raw = append(raw, e("generated", map[string]any{"id": 1, "text": "答", "sent": 24000})...)
	raw = append(raw, e("playback", map[string]any{"id": 1, "completed": 24000, "accepted": 24000, "sent": 24000, "basis": "i2s_dma", "terminal": "played"})...)
	raw = append(raw, []byte("{partial")...)
	if err := os.WriteFile(filepath.Join(r.Root, "one", "events.jsonl"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := r.ReadConversation("a", "family", "one", &id)
	if err != nil || d.RecordStatus != "pending" || len(d.Turns) != 1 || d.Turns[0].Played != "答" {
		t.Fatalf("%+v %v", d, err)
	}
	i.SchemaVersion = 1
	i.StartedAt = r.Config.Bindings[0].From
	i.ClientType = "mac"
	b, _ := json.Marshal(i)
	if err := os.WriteFile(filepath.Join(r.Root, "one", "session.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := r.ListConversations("a", "family", ConversationFilter{})
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
}
func TestProjectionBounds(t *testing.T) {
	i, e := projectionFixture()
	if _, _, err := project([]byte(strings.Repeat("x", (2<<20)+1)+"\n"), i); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	var raw []byte
	for id := 1; id <= 2001; id++ {
		raw = append(raw, e("user_text", map[string]any{"id": id, "text": "问"})...)
	}
	if _, _, err := project(raw, i); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestReaderMixedLegacyRoot(t *testing.T) {
	r, add := readerFixture(t)
	id := r.Config.Devices[0].ID
	start := r.Config.Bindings[0].From
	add("new", id, start)
	_, event := projectionFixture()
	if err := os.WriteFile(filepath.Join(r.Root, "new", "events.jsonl"), event("user_text", map[string]any{"id": 1, "text": "新记录"}), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, client, device string }{{"unattributed", "legacy_unattributed", ""}, {"no_client", "", id}, {"no_device", "xiaozhi", ""}} {
		add(tc.name, id, start)
		info, _ := projectionFixture()
		info.SchemaVersion = 1
		info.StartedAt = start
		info.ClientType = tc.client
		info.DeviceID = tc.device
		raw, _ := json.Marshal(info)
		if err := os.WriteFile(filepath.Join(r.Root, tc.name, "session.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(r.Root, tc.name, "events.jsonl")); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := r.ListDevices("a", "family")
	if err != nil || len(ds) != 1 {
		t.Fatalf("devices %+v %v", ds, err)
	}
	page, err := r.ListConversations("a", "family", ConversationFilter{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "new" {
		t.Fatalf("page %+v %v", page, err)
	}
	d, err := r.ReadConversation("a", "family", "new", &id)
	if err != nil || len(d.Turns) != 1 || d.Turns[0].User != "新记录" {
		t.Fatalf("detail %+v %v", d, err)
	}
	for _, name := range []string{"unattributed", "no_client", "no_device"} {
		if _, err := r.ReadConversation("a", "family", name, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("legacy %s: %v", name, err)
		}
	}
}
