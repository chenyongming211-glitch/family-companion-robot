// Package conversation derives business chats from normalized lifecycle facts.
package conversation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const PolicyVersion = 1
const IdleTimeout = 30 * time.Minute

var ErrInvalid = errors.New("invalid conversation facts")

type Scope struct {
	FamilyID, DeviceID, BindingID string
	BindingVersion                int
}
type Kind string
type Fact struct {
	Kind   Kind
	At     time.Time
	TurnID int64
	Reason string
}
type Source struct {
	ID        string
	Scope     Scope
	StartedAt time.Time
	Facts     []Fact
}
type TurnRef struct {
	SourceID string
	TurnID   int64
}
type Chat struct {
	ID                        string
	Scope                     Scope
	StartedAt, LastActivityAt time.Time
	EndedAt                   *time.Time
	ArchiveReason             string
	Pending                   bool
	Turns                     []TurnRef
	SourceIDs                 []string
}
type event struct {
	source, index int
	fact          Fact
}
type turnState struct {
	admitted bool
	settled  bool
	at       time.Time
	chat     int
}
type sourceState struct {
	turns         map[int64]*turnState
	latest        int
	closed        bool
	uncertainChat int
}
type chatState struct {
	pending   int
	uncertain int
	sources   map[string]bool
}

// Reduce is deterministic and does not mutate sources. Settlements freeze at
// their first occurrence, including when persistence places them before admit.
func Reduce(sources []Source, now time.Time) ([]Chat, error) {
	if !validTime(now) {
		return nil, ErrInvalid
	}
	events := make([]event, 0)
	seen := map[string]bool{}
	for i, s := range sources {
		if len(s.ID) != 32 || seen[s.ID] || s.Scope.FamilyID == "" || s.Scope.DeviceID == "" || s.Scope.BindingID == "" || s.Scope.BindingVersion <= 0 || !validTime(s.StartedAt) || s.StartedAt.After(now) {
			return nil, ErrInvalid
		}
		for _, ch := range s.ID {
			if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
				return nil, ErrInvalid
			}
		}
		seen[s.ID] = true
		prev := s.StartedAt
		if len(s.Facts) > 100000-len(events) {
			return nil, ErrInvalid
		}
		for j, f := range s.Facts {
			if !validTime(f.At) || f.At.Before(prev) || f.At.After(now) {
				return nil, ErrInvalid
			}
			prev = f.At
			switch f.Kind {
			case "admit", "settle":
				if f.TurnID <= 0 {
					return nil, ErrInvalid
				}
			case "close", "uncertain":
				if f.TurnID != 0 {
					return nil, ErrInvalid
				}
			default:
				return nil, ErrInvalid
			}
			events = append(events, event{i, j, f})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if !a.fact.At.Equal(b.fact.At) {
			return a.fact.At.Before(b.fact.At)
		}
		sa, sb := sources[a.source], sources[b.source]
		if !sa.StartedAt.Equal(sb.StartedAt) {
			return sa.StartedAt.Before(sb.StartedAt)
		}
		if sa.ID != sb.ID {
			return sa.ID < sb.ID
		}
		return a.index < b.index
	})
	states := make([]sourceState, len(sources))
	for i := range states {
		states[i] = sourceState{turns: map[int64]*turnState{}, latest: -1, uncertainChat: -1}
	}
	chats := make([]Chat, 0)
	details := make([]chatState, 0)
	active := map[Scope]int{}
	accepted := 0
	archive := func(index int, at time.Time, reason string) {
		c := &chats[index]
		if c.EndedAt != nil {
			return
		}
		end := at
		c.EndedAt = &end
		c.ArchiveReason = reason
		delete(active, c.Scope)
	}
	advance := func(sc Scope, at time.Time) {
		if i, ok := active[sc]; ok && details[i].pending == 0 && details[i].uncertain == 0 {
			deadline := chats[i].LastActivityAt.Add(IdleTimeout)
			if !at.Before(deadline) {
				archive(i, deadline, "inactivity")
			}
		}
	}
	for _, e := range events {
		s := sources[e.source]
		st := &states[e.source]
		f := e.fact
		// Scopes are independent: advancing this scope before each fact and all
		// remaining scopes at now is equivalent to advancing all scopes each time.
		advance(s.Scope, f.At)
		switch f.Kind {
		case "admit":
			if st.closed {
				return nil, ErrInvalid
			}
			ts := st.turns[f.TurnID]
			if ts == nil {
				ts = &turnState{}
				st.turns[f.TurnID] = ts
			}
			if ts.admitted {
				continue
			}
			accepted++
			if accepted > 10000 {
				return nil, ErrInvalid
			}
			i, ok := active[s.Scope]
			if !ok {
				i = len(chats)
				chats = append(chats, Chat{ID: chatID(s.Scope, s.ID, f.TurnID), Scope: s.Scope, StartedAt: f.At, LastActivityAt: f.At})
				details = append(details, chatState{sources: map[string]bool{}})
				active[s.Scope] = i
			}
			if st.uncertainChat >= 0 && chats[st.uncertainChat].EndedAt != nil {
				details[st.uncertainChat].uncertain--
				st.uncertainChat = -1
			}
			ts.admitted = true
			ts.chat = i
			st.latest = i
			c := &chats[i]
			d := &details[i]
			c.Turns = append(c.Turns, TurnRef{s.ID, f.TurnID})
			if !d.sources[s.ID] {
				d.sources[s.ID] = true
				c.SourceIDs = append(c.SourceIDs, s.ID)
			}
			c.LastActivityAt = f.At
			if !ts.settled {
				d.pending++
			}
		case "settle":
			ts := st.turns[f.TurnID]
			if ts == nil {
				ts = &turnState{}
				st.turns[f.TurnID] = ts
			}
			if ts.settled {
				continue
			}
			ts.settled = true
			ts.at = f.At
			if ts.admitted {
				i := ts.chat
				details[i].pending--
				if chats[i].EndedAt == nil && f.At.After(chats[i].LastActivityAt) {
					chats[i].LastActivityAt = f.At
				}
			}
		case "uncertain":
			if st.uncertainChat < 0 && st.latest >= 0 && chats[st.latest].EndedAt == nil {
				st.uncertainChat = st.latest
				details[st.latest].uncertain++
			}
		case "close":
			if st.closed {
				continue
			}
			st.closed = true
			for _, ts := range st.turns {
				if ts.admitted && !ts.settled {
					ts.settled = true
					ts.at = f.At
					i := ts.chat
					details[i].pending--
					if chats[i].EndedAt == nil && f.At.After(chats[i].LastActivityAt) {
						chats[i].LastActivityAt = f.At
					}
				}
			}
			// Closing settles known admitted turns, but cannot resolve an unread
			// source tail. Keep that uncertainty even when explicit end archives.
			if f.Reason == "client_stop" {
				i := st.latest
				if i < 0 {
					if candidate, ok := active[s.Scope]; ok && !s.StartedAt.Before(chats[candidate].StartedAt) {
						i = candidate
						details[i].sources[s.ID] = true
						chats[i].SourceIDs = append(chats[i].SourceIDs, s.ID)
					}
				}
				if i >= 0 && chats[i].EndedAt == nil {
					archive(i, f.At, "explicit_end")
				}
			}
		}
	}
	for sc := range active {
		advance(sc, now)
	}
	for i := range chats {
		chats[i].Pending = details[i].pending > 0 || details[i].uncertain > 0
	}
	return chats, nil
}
func validTime(t time.Time) bool {
	return t.Year() >= 1970 && t.Year() <= 9999 && t.After(time.Unix(0, 0))
}
func chatID(scope Scope, sourceID string, turnID int64) string {
	key := struct {
		Policy   int
		Scope    Scope
		SourceID string
		TurnID   int64
	}{PolicyVersion, scope, sourceID, turnID}
	raw, _ := json.Marshal(key)
	sum := sha256.Sum256(raw)
	return "chat_" + hex.EncodeToString(sum[:])
}
