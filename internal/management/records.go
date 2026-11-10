package management

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"familyrobot/internal/deviceprofile"
	"familyrobot/internal/realtimelive"
	"familyrobot/internal/realtimestate"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (r Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
func (r Reader) ListDevices(accountID, scope string) ([]DeviceDTO, error) {
	if _, ok := r.Config.AccountByID(accountID); !ok {
		return nil, ErrForbidden
	}
	ac, _ := r.Config.AccountByID(accountID)
	if (scope != "family" && scope != "admin") || (scope == "admin" && !ac.InternalAdmin) {
		return nil, ErrForbidden
	}
	ss, e := r.sources(accountID, scope)
	if e != nil {
		return nil, e
	}
	var profiles *deviceprofile.Store
	if r.ProfilesPath != "" {
		profiles, e = deviceprofile.Open(r.ProfilesPath)
		if e != nil {
			return nil, ErrSource
		}
	}
	out := []DeviceDTO{}
	for _, d := range r.Config.Devices {
		b, e := r.Config.Authorize(accountID, scope, d.ID, r.now(), false)
		if e == nil {
			out = append(out, DeviceDTO{ID: d.ID, FamilyID: b.FamilyID, Name: d.Name, Location: d.Location, Model: d.Model, Connection: "unknown", Activity: "unknown"})
		}
	}
	for i := range out {
		d := &out[i]
		if profiles != nil {
			p, e := profiles.Resolve(d.ID, d.Name)
			if e == nil {
				d.Name = p.Name
				d.Revision = &p.Revision
			} else if !errors.Is(e, deviceprofile.ErrNotFound) && !errors.Is(e, deviceprofile.ErrUnavailable) {
				return nil, ErrSource
			}
		}
		for _, configured := range r.Config.Devices {
			if configured.ID == d.ID && configured.DisplayName != "" {
				d.Name = configured.DisplayName
				break
			}
		}
		for _, s := range ss {
			if s.info.DeviceID == d.ID {
				d.LastSeen = s.info.StartedAt.Format(time.RFC3339)
				d.Connection = "latest_connection"
				break
			}
		}
	}
	return out, nil
}
func (r Reader) GetDevice(accountID, scope, id string) (DeviceDTO, error) {
	ds, e := r.ListDevices(accountID, scope)
	if e != nil {
		return DeviceDTO{}, e
	}
	for _, d := range ds {
		if d.ID == id {
			return d, nil
		}
	}
	return DeviceDTO{}, ErrNotFound
}

type source struct {
	id      string
	info    realtimelive.SessionInfo
	binding Binding
	body    bool
}

// Every read is anchored to an opened directory; pathname replacement cannot
// redirect traversal outside it. Check the opened inode and bound reads too.
func regular(root *os.Root, name string, max int64) ([]byte, error) {
	before, e := root.Lstat(name)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, ErrSource
	}
	if !before.Mode().IsRegular() {
		return nil, ErrSource
	}
	if before.Size() > max {
		return nil, ErrTooLarge
	}
	f, e := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, ErrSource
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, ErrSource
	}
	raw, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil {
		return nil, ErrSource
	}
	if int64(len(raw)) > max {
		return nil, ErrTooLarge
	}
	after, e := root.Lstat(name)
	if e != nil || !after.Mode().IsRegular() || !os.SameFile(opened, after) {
		return nil, ErrSource
	}
	return raw, nil
}
func openRecordRoot(path string) (*os.Root, error) {
	if path == "" {
		return nil, ErrSource
	}
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, ErrSource
	}
	initial, e := os.Lstat(abs)
	if e != nil {
		return nil, ErrSource
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, ErrSource
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	rt, e := os.OpenRoot(abs)
	if e != nil {
		return nil, ErrSource
	}
	opened, e := rt.Stat(".")
	if e != nil || !os.SameFile(initial, opened) {
		rt.Close()
		return nil, ErrSource
	}
	return rt, nil
}
func recordDir(root *os.Root, id string) (*os.Root, error) {
	st, e := root.Lstat(id)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSource
	}
	d, e := root.OpenRoot(id)
	if e != nil {
		return nil, ErrSource
	}
	now, e := d.Stat(".")
	if e != nil || !os.SameFile(st, now) {
		d.Close()
		return nil, ErrSource
	}
	return d, nil
}

var recordSessionID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var recordDeviceID = regexp.MustCompile(`^dev_[a-f0-9]{32}$`)

func (r Reader) sources(a, scope string) ([]source, error) {
	if _, ok := r.Config.AccountByID(a); !ok {
		return nil, ErrForbidden
	}
	if scope != "admin" && scope != "family" {
		return nil, ErrForbidden
	}
	if scope == "admin" {
		v, _ := r.Config.AccountByID(a)
		if !v.InternalAdmin {
			return nil, ErrForbidden
		}
	}
	root, e := openRecordRoot(r.Root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	directory, e := root.Open(".")
	if e != nil {
		return nil, ErrSource
	}
	defer directory.Close()
	entries, e := directory.ReadDir(10001)
	if e != nil && e != io.EOF {
		return nil, ErrSource
	}
	if len(entries) > 10000 {
		return nil, ErrTooLarge
	}
	out := []source{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir, e := recordDir(root, entry.Name())
		if e != nil {
			return nil, e
		}
		raw, e := regular(dir, "session.json", 1<<20)
		dir.Close()
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		var info realtimelive.SessionInfo
		if recordJSON(raw) != nil || json.Unmarshal(raw, &info) != nil || info.SchemaVersion != 1 || !recordSessionID.MatchString(info.SessionID) || info.StartedAt.IsZero() {
			return nil, ErrSource
		}
		if info.EndedAt != nil && info.EndedAt.Before(info.StartedAt) {
			return nil, ErrSource
		}
		// Legacy and unattributed manifests are ineligible metadata, not
		// errors in a new authenticated source. Never open their event bodies.
		if info.ClientType != "xiaozhi" || info.DeviceID == "" {
			continue
		}
		if !recordDeviceID.MatchString(info.DeviceID) {
			return nil, ErrSource
		}
		b, e := r.Config.Authorize(a, scope, info.DeviceID, info.StartedAt, false)
		if e != nil {
			continue
		}
		_, e = r.Config.Authorize(a, scope, info.DeviceID, info.StartedAt, true)
		out = append(out, source{entry.Name(), info, b, e == nil})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].info.StartedAt.Equal(out[j].info.StartedAt) {
			return out[i].id < out[j].id
		}
		return out[i].info.StartedAt.After(out[j].info.StartedAt)
	})
	return out, nil
}
func dto(s source) ConversationDTO {
	t := s.info.StartedAt.In(time.FixedZone("CST", 8*3600))
	status := "连接记录"
	if s.info.EndedAt == nil {
		status = "状态未知"
	}
	return ConversationDTO{ID: s.id, DeviceID: s.info.DeviceID, FamilyID: s.binding.FamilyID, Title: "连接记录", Date: t.Format("2006-01-02"), Time: t.Format("15:04"), Status: status, EndReason: s.info.EndReason, BodyAvailable: s.body, Turns: []TurnDTO{}, SourceKind: "connection_record", RecordStatus: "saved"}
}
func (r Reader) ListConversations(a, scope string, f ConversationFilter) (ConversationPage, error) {
	page := ConversationPage{Items: []ConversationDTO{}}
	if f.Offset < 0 || f.Offset > 10000 || f.Limit < 0 || f.Limit > 100 || (f.DeviceID != nil && *f.DeviceID == "") {
		return page, ErrInvalid
	}
	if f.DeviceID != nil {
		if !recordDeviceID.MatchString(*f.DeviceID) {
			return page, ErrInvalid
		}
		if !r.granted(a, scope, *f.DeviceID) {
			return page, ErrForbidden
		}
	}
	if f.Limit == 0 {
		f.Limit = 50
	}
	ss, e := r.sources(a, scope)
	if e != nil {
		return page, e
	}
	for _, s := range ss {
		if f.DeviceID != nil && s.info.DeviceID != *f.DeviceID {
			continue
		}
		if f.Offset > 0 {
			f.Offset--
			continue
		}
		if len(page.Items) == f.Limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, dto(s))
	}
	return page, nil
}
func (r Reader) ReadConversation(a, scope, id string, expected *string) (ConversationDTO, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return ConversationDTO{}, ErrInvalid
	}
	ss, e := r.sources(a, scope)
	if e != nil {
		return ConversationDTO{}, e
	}
	for _, s := range ss {
		if s.id != id {
			continue
		}
		if expected != nil && (*expected == "" || *expected != s.info.DeviceID) {
			return ConversationDTO{}, ErrForbidden
		}
		if !s.body {
			return ConversationDTO{}, ErrForbidden
		}
		d := dto(s)
		root, e := openRecordRoot(r.Root)
		if e != nil {
			return ConversationDTO{}, e
		}
		defer root.Close()
		dir, e := recordDir(root, id)
		if e != nil {
			return ConversationDTO{}, e
		}
		defer dir.Close()
		manifest, e := regular(dir, "session.json", 1<<20)
		if e != nil {
			return ConversationDTO{}, e
		}
		var current realtimelive.SessionInfo
		if recordJSON(manifest) != nil || json.Unmarshal(manifest, &current) != nil || !reflect.DeepEqual(current, s.info) {
			return ConversationDTO{}, ErrSource
		}
		raw, e := regular(dir, "events.jsonl", 32<<20)
		if e != nil {
			if !errors.Is(e, ErrTooLarge) {
				e = ErrSource
			}
			return ConversationDTO{}, e
		}
		turns, pending, e := project(raw, s.info)
		if e != nil {
			return ConversationDTO{}, e
		}
		d.Turns = turns
		if pending {
			d.RecordStatus = "pending"
		}
		return d, nil
	}
	return ConversationDTO{}, ErrNotFound
}
func (r Reader) granted(a, scope, id string) bool {
	ac, ok := r.Config.AccountByID(a)
	if !ok || (scope != "family" && scope != "admin") || (scope == "admin" && !ac.InternalAdmin) {
		return false
	}
	for _, m := range ac.Memberships {
		for _, d := range m.Devices {
			if d.DeviceID == id {
				return true
			}
		}
	}
	return false
}

type projectedTurn struct {
	turn                                       *realtimestate.Turn
	saved, sentKnown, progressKnown, untrusted bool
	terminal                                   string
	maxCount                                   int64
	terminalCompleted                          int64
	terminalCompletedKnown                     bool
}

func count(m map[string]any, key string) (int64, bool, error) {
	v, ok := m[key]
	if !ok {
		return 0, false, nil
	}
	n, ok := v.(json.Number)
	if !ok {
		return 0, false, ErrSource
	}
	i, e := n.Int64()
	if e != nil || i < 0 {
		return 0, false, ErrSource
	}
	return i, true, nil
}
func project(raw []byte, info realtimelive.SessionInfo) ([]TurnDTO, bool, error) {
	if len(raw) > 32<<20 {
		return nil, false, ErrTooLarge
	}
	pending := len(raw) > 0 && raw[len(raw)-1] != '\n'
	if pending {
		i := bytes.LastIndexByte(raw, '\n')
		if len(raw)-i-1 > 2<<20 {
			return nil, false, ErrTooLarge
		}
		raw = raw[:i+1]
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), (2<<20)+1)
	turns := map[int64]*projectedTurn{}
	for scanner.Scan() {
		if len(scanner.Bytes()) > 2<<20 {
			return nil, false, ErrTooLarge
		}
		if recordJSON(scanner.Bytes()) != nil {
			return nil, false, ErrSource
		}
		dec := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		dec.UseNumber()
		var m map[string]any
		if dec.Decode(&m) != nil || m == nil {
			return nil, false, ErrSource
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return nil, false, ErrSource
		}
		if m["session_id"] != info.SessionID || m["device_id"] != info.DeviceID || m["client_type"] != info.ClientType {
			return nil, false, ErrSource
		}
		if v, ok := m["client_version"]; ok && v != info.ClientVersion {
			return nil, false, ErrSource
		}
		kind, ok := m["event"].(string)
		if !ok || kind == "" {
			return nil, false, ErrSource
		}
		relevant := false
		switch kind {
		case "user_text", "generated", "generated_interrupted", "interrupt", "device_playback", "playback", "output_asr_final", "turn_error", "drain_timeout":
			relevant = true
		}
		id, known, e := count(m, "id")
		if e != nil || (known && id == 0) {
			return nil, false, ErrSource
		}
		if !relevant {
			continue
		}
		if !known {
			return nil, false, ErrSource
		}
		p := turns[id]
		if p == nil {
			if len(turns) >= 2000 {
				return nil, false, ErrTooLarge
			}
			p = &projectedTurn{turn: realtimestate.NewTurn(id)}
			p.turn.PlaybackBasis = "i2s_dma"
			turns[id] = p
		}
		t := p.turn
		if kind == "generated" || kind == "generated_interrupted" || kind == "interrupt" || kind == "playback" || kind == "turn_error" || kind == "drain_timeout" {
			n, ok, e := count(m, "sent")
			if e != nil {
				return nil, false, e
			}
			if ok {
				if p.sentKnown && n != t.Sent {
					return nil, false, ErrSource
				}
				t.Sent = n
				p.sentKnown = true
			}
		}
		switch kind {
		case "user_text":
			v, ok := m["text"].(string)
			if !ok || strings.TrimSpace(v) == "" || (p.saved && v != t.User) {
				return nil, false, ErrSource
			}
			t.User = v
			p.saved = true
		case "generated", "generated_interrupted":
			v, ok := m["text"].(string)
			if !ok {
				return nil, false, ErrSource
			}
			if t.Generated != "" && t.Generated != v {
				return nil, false, ErrSource
			}
			t.Generated = v
			t.Ended = kind == "generated"
			if kind == "generated_interrupted" {
				t.Cut = true
			}
		case "interrupt":
			t.Cut = true
		case "playback", "device_playback":
			completed, ck, e := count(m, "completed")
			if e != nil {
				return nil, false, e
			}
			accepted, ak, e := count(m, "accepted")
			if e != nil {
				return nil, false, e
			}
			if ck && ak && completed > accepted {
				return nil, false, ErrSource
			}
			if ck && completed > p.maxCount {
				p.maxCount = completed
			}
			if ak && accepted > p.maxCount {
				p.maxCount = accepted
			}
			trustworthy := ck && ak && m["basis"] == "i2s_dma"
			if !trustworthy {
				p.untrusted = true
			} else {
				if p.progressKnown && (completed < t.Completed || accepted < t.Accepted) {
					return nil, false, ErrSource
				}
				if p.terminal != "" && (completed != t.Completed || accepted != t.Accepted) {
					return nil, false, ErrSource
				}
				t.Completed = completed
				t.Accepted = accepted
				p.progressKnown = true
			}
			if kind == "playback" {
				terminal, ok := m["terminal"].(string)
				if !ok || (terminal != "played" && terminal != "drained") || (p.terminal != "" && p.terminal != terminal) {
					return nil, false, ErrSource
				}
				p.terminal = terminal
				p.terminalCompleted = completed
				p.terminalCompletedKnown = ck
				t.PlaybackFinal = trustworthy
				if terminal == "drained" {
					t.Cut = true
				}
			}
		case "output_asr_final":
			sentence, ok := m["sentence"].(map[string]any)
			if !ok {
				return nil, false, ErrSource
			}
			t.Tracker.Add(sentence, 0)
		}
	}
	if scanner.Err() != nil {
		return nil, false, ErrTooLarge
	}
	ids := []int64{}
	for id, p := range turns {
		t := p.turn
		if p.sentKnown && p.maxCount > t.Sent {
			return nil, false, ErrSource
		}
		if p.terminal == "played" && p.sentKnown && p.terminalCompletedKnown && p.terminalCompleted != t.Sent {
			return nil, false, ErrSource
		}
		if p.progressKnown && p.sentKnown && (t.Completed > t.Accepted || t.Accepted > t.Sent) {
			return nil, false, ErrSource
		}
		if p.terminal == "played" && p.progressKnown && p.sentKnown && t.Completed != t.Sent {
			return nil, false, ErrSource
		}
		if p.terminal == "played" && t.Cut {
			return nil, false, ErrSource
		}
		if p.saved {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []TurnDTO{}
	for _, id := range ids {
		p := turns[id]
		t := p.turn
		d := TurnDTO{ID: strconv.FormatInt(id, 10), User: t.User, Generated: t.Generated, Saved: true, Playback: "unknown", TextSource: "none", Boundary: "unknown_playback"}
		if t.Cut {
			d.Playback = "interrupted"
			t.Ended = false
		}
		// No generation or ASR text is evidence of speaker playback by itself.
		if p.sentKnown && p.progressKnown && !p.untrusted && t.Sent > 0 {
			ev := realtimestate.PlaybackEvidence(t)
			d.TextSource = ev.TextSource
			d.Boundary = ev.Boundary
			d.ExactStopKnown = ev.ExactStopKnown
			d.RecognitionMayDiffer = ev.RecognitionMayDiffer
			if ev.SpokenText != nil {
				d.Played = *ev.SpokenText
			}
			if ev.Boundary == "complete_response" {
				d.Playback = "played"
			}
		}
		out = append(out, d)
	}
	return out, pending, nil
}

// Reject duplicate keys rather than allowing a later owner/count to shadow one.
func recordJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if recordKeys(d, 0) != nil {
		return ErrSource
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrSource
	}
	return nil
}
func recordKeys(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrSource
	}
	v, e := d.Token()
	if e != nil {
		return ErrSource
	}
	delim, ok := v.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			v, e := d.Token()
			k, ok := v.(string)
			if e != nil || !ok || seen[k] {
				return ErrSource
			}
			seen[k] = true
			if recordKeys(d, depth+1) != nil {
				return ErrSource
			}
		}
	case '[':
		for d.More() {
			if recordKeys(d, depth+1) != nil {
				return ErrSource
			}
		}
	default:
		return ErrSource
	}
	_, e = d.Token()
	return e
}
