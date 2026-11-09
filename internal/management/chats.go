package management

import (
	"bytes"
	"encoding/json"
	"familyrobot/internal/conversation"
	"familyrobot/internal/realtimelive"
	"math/big"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var businessChatID = regexp.MustCompile(`^chat_[a-f0-9]{64}$`)
var eventDecimal = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,18})?$`)

const chatByteLimit = 64 << 20
const chatEventLimit = 100000
const chatTurnLimit = 10000

type chatSource struct {
	source  source
	turns   map[string]TurnDTO
	pending bool
}

// Body grants are checked before even enumerating source files. Source-time
// authorization is checked again by sources and before each event file read.
func (r Reader) chatGrant(a, scope string, device *string) error {
	ac, ok := r.Config.AccountByID(a)
	if !ok || (scope != "family" && scope != "admin") || (scope == "admin" && !ac.InternalAdmin) {
		return ErrForbidden
	}
	if device != nil && !recordDeviceID.MatchString(*device) {
		return ErrInvalid
	}
	for _, m := range ac.Memberships {
		for _, d := range m.Devices {
			if d.CanReadBody && (device == nil || d.DeviceID == *device) {
				return nil
			}
		}
	}
	return ErrForbidden
}
func (r Reader) ListChats(a, scope string, f ConversationFilter) (ConversationPage, error) {
	page := ConversationPage{Items: []ConversationDTO{}}
	if f.Offset < 0 || f.Offset > 10000 || f.Limit < 0 || f.Limit > 100 {
		return page, ErrInvalid
	}
	if e := r.chatGrant(a, scope, f.DeviceID); e != nil {
		return page, e
	}
	chats, ss, pending, e := r.chats(a, scope, f.DeviceID)
	if e != nil {
		return page, e
	}
	page.PendingSources = pending
	if f.Limit == 0 {
		f.Limit = 50
	}
	for i := f.Offset; i < len(chats); i++ {
		if len(page.Items) == f.Limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, chatDTO(chats[i], ss, false))
	}
	return page, nil
}
func (r Reader) ReadChat(a, scope, id string, expected *string) (ConversationDTO, error) {
	if !businessChatID.MatchString(id) {
		return ConversationDTO{}, ErrInvalid
	}
	if e := r.chatGrant(a, scope, expected); e != nil {
		return ConversationDTO{}, e
	}
	chats, ss, _, e := r.chats(a, scope, expected)
	if e != nil {
		return ConversationDTO{}, e
	}
	for _, c := range chats {
		if c.ID == id {
			return chatDTO(c, ss, true), nil
		}
	}
	return ConversationDTO{}, ErrNotFound
}
func (r Reader) chats(a, scope string, device *string) ([]conversation.Chat, map[string]chatSource, bool, error) {
	ss, e := r.sources(a, scope)
	if e != nil {
		return nil, nil, false, e
	}
	root, e := openRecordRoot(r.Root)
	if e != nil {
		return nil, nil, false, e
	}
	defer root.Close()
	projected := map[string]chatSource{}
	facts := []conversation.Source{}
	pending := false
	totalBytes, totalEvents, totalTurns := 0, 0, 0
	latest := time.Time{}
	for _, s := range ss {
		if !s.body || (device != nil && s.info.DeviceID != *device) {
			continue
		}
		b, e := r.Config.Authorize(a, scope, s.info.DeviceID, s.info.StartedAt, true)
		if e != nil {
			return nil, nil, false, e
		}
		if b != s.binding {
			return nil, nil, false, ErrSource
		}
		if _, ok := projected[s.info.SessionID]; ok {
			return nil, nil, false, ErrSource
		}
		dir, e := recordDir(root, s.id)
		if e != nil {
			return nil, nil, false, e
		}
		manifest, e := regular(dir, "session.json", 1<<20)
		if e != nil {
			dir.Close()
			return nil, nil, false, e
		}
		var current realtimelive.SessionInfo
		if recordJSON(manifest) != nil || json.Unmarshal(manifest, &current) != nil || !reflect.DeepEqual(current, s.info) {
			dir.Close()
			return nil, nil, false, ErrSource
		}
		max := int64(32 << 20)
		if left := int64(chatByteLimit - totalBytes); left < max {
			max = left
		}
		raw, e := regular(dir, "events.jsonl", max)
		dir.Close()
		if e != nil {
			return nil, nil, false, e
		}
		totalBytes += len(raw)
		// Bound complete events before decoding either projection.
		n := bytes.Count(raw, []byte{'\n'})
		totalEvents += n
		if totalEvents > chatEventLimit {
			return nil, nil, false, ErrTooLarge
		}
		turns, partial, e := project(raw, s.info)
		if e != nil {
			return nil, nil, false, e
		}
		normalized, last, e := chatFacts(raw, s, &totalTurns)
		if e != nil {
			return nil, nil, false, e
		}
		if last.After(latest) {
			latest = last
		}
		facts = append(facts, normalized)
		m := map[string]TurnDTO{}
		for _, t := range turns {
			m[t.ID] = t
		}
		projected[s.info.SessionID] = chatSource{s, m, partial}
		pending = pending || partial
	}
	now := r.now()
	if latest.After(now) {
		return nil, nil, false, ErrSource
	}
	chats, e := conversation.Reduce(facts, now)
	if e != nil {
		return nil, nil, false, ErrSource
	}
	sort.Slice(chats, func(i, j int) bool {
		if chats[i].StartedAt.Equal(chats[j].StartedAt) {
			return chats[i].ID < chats[j].ID
		}
		return chats[i].StartedAt.After(chats[j].StartedAt)
	})
	return chats, projected, pending, nil
}
func eventTime(v any) (time.Time, error) {
	n, ok := v.(json.Number)
	if !ok || !eventDecimal.MatchString(n.String()) {
		return time.Time{}, ErrSource
	}
	q, ok := new(big.Rat).SetString(n.String())
	if !ok || q.Sign() <= 0 {
		return time.Time{}, ErrSource
	}
	q.Mul(q, big.NewRat(1000000000, 1))
	ns := new(big.Int).Quo(q.Num(), q.Denom())
	sec, rem := new(big.Int), new(big.Int)
	sec.QuoRem(ns, big.NewInt(1000000000), rem)
	if !sec.IsInt64() {
		return time.Time{}, ErrSource
	}
	t := time.Unix(sec.Int64(), rem.Int64()).UTC()
	if t.Year() > 9999 || !t.After(time.Unix(0, 0)) {
		return time.Time{}, ErrSource
	}
	return t, nil
}

// project has already validated ownership, duplicate JSON keys, turn identities,
// text, sample counts and terminal consistency. This pass extracts time only.
func chatFacts(raw []byte, s source, totalTurns *int) (conversation.Source, time.Time, error) {
	out := conversation.Source{ID: s.info.SessionID, Scope: conversation.Scope{FamilyID: s.binding.FamilyID, DeviceID: s.info.DeviceID, BindingID: s.binding.ID, BindingVersion: s.binding.Version}, StartedAt: s.info.StartedAt}
	partial := len(raw) > 0 && raw[len(raw)-1] != '\n'
	if partial {
		raw = raw[:bytes.LastIndexByte(raw, '\n')+1]
	}
	last := s.info.StartedAt
	seenTurns := map[int64]bool{}
	closed := false
	reason := ""
	for len(raw) > 0 {
		idx := bytes.IndexByte(raw, '\n')
		line := raw[:idx]
		raw = raw[idx+1:]
		var m map[string]any
		d := json.NewDecoder(bytes.NewReader(line))
		d.UseNumber()
		if d.Decode(&m) != nil {
			return out, last, ErrSource
		}
		at, e := eventTime(m["at"])
		if e != nil || at.Before(last) {
			return out, last, ErrSource
		}
		last = at
		id, _, _ := count(m, "id")
		kind, _ := m["event"].(string)
		switch kind {
		case "user_text", "generated", "generated_interrupted", "interrupt", "device_playback", "playback", "output_asr_final", "turn_error", "drain_timeout":
			if !seenTurns[id] {
				seenTurns[id] = true
				*totalTurns++
				if *totalTurns > chatTurnLimit {
					return out, last, ErrTooLarge
				}
			}
		}
		f := conversation.Fact{At: at, TurnID: id}
		switch kind {
		case "user_text":
			f.Kind = "admit"
		case "generated", "generated_interrupted", "interrupt":
			n, known, _ := count(m, "sent")
			if known && n == 0 {
				f.Kind = "settle"
			}
		case "drain_timeout":
			f.Kind = "settle"
		case "playback":
			_, ck, _ := count(m, "completed")
			_, ak, _ := count(m, "accepted")
			if ck && ak && m["basis"] == "i2s_dma" {
				f.Kind = "settle"
			}
		case "closed":
			v, ok := m["reason"].(string)
			if !ok || v == "" {
				return out, last, ErrSource
			}
			if closed {
				if v != reason {
					return out, last, ErrSource
				}
				continue
			}
			closed = true
			reason = v
			f.Kind = "close"
			f.TurnID = 0
			f.Reason = v
		}
		if f.Kind != "" {
			out.Facts = append(out.Facts, f)
		}
	}
	if partial {
		out.Facts = append(out.Facts, conversation.Fact{Kind: "uncertain", At: last})
	}
	if s.info.EndedAt != nil {
		if closed {
			if reason != s.info.EndReason {
				return out, last, ErrSource
			}
		} else {
			if s.info.EndedAt.Before(last) || s.info.EndReason == "" {
				return out, last, ErrSource
			}
			last = *s.info.EndedAt
			out.Facts = append(out.Facts, conversation.Fact{Kind: "close", At: last, Reason: s.info.EndReason})
		}
	}
	return out, last, nil
}
func chatDTO(c conversation.Chat, ss map[string]chatSource, detail bool) ConversationDTO {
	t := c.StartedAt.In(time.FixedZone("CST", 8*3600))
	d := ConversationDTO{ID: c.ID, DeviceID: c.Scope.DeviceID, FamilyID: c.Scope.FamilyID, Title: "聊天记录", Date: t.Format("2006-01-02"), Time: t.Format("15:04"), Status: "进行中", BodyAvailable: true, Turns: []TurnDTO{}, SourceKind: "business_chat", RecordStatus: "saved", StartedAt: c.StartedAt.Format(time.RFC3339Nano), LastActivityAt: c.LastActivityAt.Format(time.RFC3339Nano), ArchiveReason: c.ArchiveReason, TurnCount: len(c.Turns), ConnectionCount: len(c.SourceIDs)}
	if c.EndedAt != nil {
		d.EndedAt = c.EndedAt.Format(time.RFC3339Nano)
		d.Status = "已归档"
		d.EndReason = c.ArchiveReason
	} else if c.Pending {
		d.Status = "待结算"
	}
	for _, id := range c.SourceIDs {
		s := ss[id]
		if s.pending {
			d.RecordStatus = "pending"
		}
		if detail {
			d.Sources = append(d.Sources, ChatSourceDTO{RecordID: s.source.id, SessionID: id, StartedAt: s.source.info.StartedAt.Format(time.RFC3339Nano)})
		}
	}
	if detail {
		for _, ref := range c.Turns {
			s := ss[ref.SourceID]
			id := strconv.FormatInt(ref.TurnID, 10)
			t := s.turns[id]
			t.SourceRecordID = s.source.id
			t.SourceTurnID = id
			t.ID = ref.SourceID + ":" + id
			d.Turns = append(d.Turns, t)
		}
	}
	return d
}
