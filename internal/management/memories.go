package management

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type memorySnapshot struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Data      struct {
		Turns []snapshotTurn `json:"turns"`
	} `json:"data"`
}
type snapshotTurn struct {
	SessionID string    `json:"session_id"`
	TurnID    int64     `json:"turn_id"`
	At        time.Time `json:"at"`
	User      *string   `json:"user"`
	Assistant *string   `json:"assistant_spoken"`
	Boundary  *string   `json:"playback_boundary"`
	Source    *string   `json:"text_source"`
}

var memoryID = regexp.MustCompile(`^mem-([a-f0-9]{32})-([1-9][0-9]{0,18})$`)

func (r Reader) ListMemories(accountID, scope, deviceID string) ([]MemoryDTO, error) {
	if !recordDeviceID.MatchString(deviceID) {
		return nil, ErrInvalid
	}
	now := r.now()
	binding, e := r.Config.Authorize(accountID, scope, deviceID, now, true)
	if e != nil {
		return nil, e
	}
	root, e := openRecordRoot(r.Root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	dir, e := recordDir(root, ".memory")
	if e != nil {
		return nil, e
	}
	defer dir.Close()
	hash := sha256.Sum256([]byte(deviceID))
	raw, e := regular(dir, hex.EncodeToString(hash[:])+".json", 1<<20)
	if errors.Is(e, os.ErrNotExist) {
		return []MemoryDTO{}, nil
	}
	if e != nil {
		return nil, e
	}
	if !utf8.Valid(raw) || recordJSON(raw) != nil {
		return nil, ErrSource
	}
	var snapshot memorySnapshot
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&snapshot) != nil {
		return nil, ErrSource
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || snapshot.Version != 1 || snapshot.UpdatedAt.IsZero() || snapshot.UpdatedAt.After(now) || snapshot.Data.Turns == nil {
		return nil, ErrSource
	}
	if len(snapshot.Data.Turns) > 24 {
		return nil, ErrTooLarge
	}
	seen := map[string]bool{}
	total := 0
	for _, turn := range snapshot.Data.Turns {
		if turn.User == nil || turn.Assistant == nil || turn.Boundary == nil || turn.Source == nil {
			return nil, ErrSource
		}
		if !recordSessionID.MatchString(turn.SessionID) || turn.TurnID < 1 || turn.At.IsZero() || turn.At.After(snapshot.UpdatedAt) {
			return nil, ErrSource
		}
		id := memoryTurnID(turn)
		if seen[id] {
			return nil, ErrSource
		}
		seen[id] = true
		u, a := utf8.RuneCountInString(*turn.User), utf8.RuneCountInString(*turn.Assistant)
		total += u + a
		if u > 512 || a > 1024 || total > 24000 {
			return nil, ErrTooLarge
		}
		if len(*turn.Boundary) > 128 || len(*turn.Source) > 128 {
			return nil, ErrTooLarge
		}
	}
	sources, e := r.sources(accountID, scope)
	if e != nil {
		return nil, e
	}
	eligible := map[string]source{}
	for _, s := range sources {
		if s.info.DeviceID != deviceID || !s.body || s.binding.ID != binding.ID || s.binding.Version != binding.Version {
			continue
		}
		if _, exists := eligible[s.info.SessionID]; exists {
			return nil, ErrSource
		}
		eligible[s.info.SessionID] = s
	}
	// Provenance is an optional authorized projection. Failure to read archived
	// events only disables links; permission errors still fail closed.
	type memoryLink struct{ chatID, turnID string }
	links := map[string]memoryLink{}
	chats, _, _, projectionErr := r.chats(accountID, scope, &deviceID)
	if errors.Is(projectionErr, ErrForbidden) || errors.Is(projectionErr, ErrInvalid) {
		return nil, projectionErr
	}
	if projectionErr == nil {
		for _, chat := range chats {
			if chat.Scope.DeviceID != deviceID || chat.Scope.FamilyID != binding.FamilyID || chat.Scope.BindingID != binding.ID || chat.Scope.BindingVersion != binding.Version {
				continue
			}
			for _, ref := range chat.Turns {
				id := ref.SourceID + ":" + strconv.FormatInt(ref.TurnID, 10)
				links[id] = memoryLink{chat.ID, id}
			}
		}
	}
	out := []MemoryDTO{}
	for _, turn := range snapshot.Data.Turns {
		s, ok := eligible[turn.SessionID]
		if !ok || turn.At.Before(binding.From) || (binding.Until != nil && !turn.At.Before(*binding.Until)) || turn.At.Before(s.info.StartedAt) || turn.At.After(now) || !turn.At.After(now.Add(-7*24*time.Hour)) {
			continue
		}
		if strings.TrimSpace(*turn.User) == "" && strings.TrimSpace(*turn.Assistant) == "" {
			continue
		}
		text := *turn.User
		if text == "" {
			text = *turn.Assistant
		}
		link := links[turn.SessionID+":"+strconv.FormatInt(turn.TurnID, 10)]
		out = append(out, MemoryDTO{ChatID: link.chatID, TurnID: link.turnID, ID: memoryTurnID(turn), DeviceID: deviceID, ConversationID: s.id, Text: text, Date: turn.At.Format(time.RFC3339Nano), User: *turn.User, Assistant: *turn.Assistant, TextSource: *turn.Source, Boundary: *turn.Boundary})
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, out[i].Date)
		b, _ := time.Parse(time.RFC3339Nano, out[j].Date)
		if a.Equal(b) {
			return out[i].ID < out[j].ID
		}
		return a.After(b)
	})
	return out, nil
}
func memoryTurnID(turn snapshotTurn) string {
	return "mem-" + turn.SessionID + "-" + strconv.FormatInt(turn.TurnID, 10)
}
func (r Reader) ReadMemory(accountID, scope, id, deviceID string) (MemoryDTO, error) {
	match := memoryID.FindStringSubmatch(id)
	if match == nil {
		return MemoryDTO{}, ErrInvalid
	}
	if _, e := strconv.ParseInt(match[2], 10, 64); e != nil {
		return MemoryDTO{}, ErrInvalid
	}
	items, e := r.ListMemories(accountID, scope, deviceID)
	if e != nil {
		return MemoryDTO{}, e
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return MemoryDTO{}, ErrNotFound
}
