package management

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"
)

const auditMaxBytes = 32 << 20
const auditMaxLines = 100000

var auditRFC3339 = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

type AuditFilter struct {
	DeviceID      string
	From, To      *time.Time
	Action        string
	Offset, Limit int
}
type AuditPage struct {
	Items   []AuditItem `json:"items"`
	HasMore bool        `json:"hasMore"`
	Pending bool        `json:"pending"`
}
type AuditItem struct {
	ID            string `json:"id"`
	Time          string `json:"time"`
	Actor         string `json:"actor"`
	DeviceID      string `json:"deviceId"`
	Target        string `json:"target"`
	Action        string `json:"action"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	Result        string `json:"result"`
	Scope         string `json:"scope"`
	CanOpenDevice bool   `json:"canOpenDevice"`
	Field         string `json:"field,omitempty"`
	Value         *int   `json:"value,omitempty"`
	Actual        *int   `json:"actual,omitempty"`
}

func parseAuditFilter(raw string) (AuditFilter, error) {
	f := AuditFilter{Limit: 50}
	q, e := url.ParseQuery(raw)
	if e != nil {
		return f, ErrInvalid
	}
	for k, v := range q {
		if len(v) != 1 || v[0] == "" {
			return f, ErrInvalid
		}
		switch k {
		case "device":
			if !device.MatchString(v[0]) {
				return f, ErrInvalid
			}
			f.DeviceID = v[0]
		case "from", "to":
			if !auditRFC3339.MatchString(v[0]) {
				return f, ErrInvalid
			}
			t, e := time.Parse(time.RFC3339Nano, v[0])
			if e != nil {
				return f, ErrInvalid
			}
			if k == "from" {
				f.From = &t
			} else {
				f.To = &t
			}
		case "action":
			if v[0] != "access" && v[0] != "device_control" && v[0] != "memory_clear" && v[0] != "settings" {
				return f, ErrInvalid
			}
			f.Action = v[0]
		case "offset", "limit":
			for _, c := range v[0] {
				if c < '0' || c > '9' {
					return f, ErrInvalid
				}
			}
			n, e := strconv.Atoi(v[0])
			if e != nil {
				return f, ErrInvalid
			}
			if k == "offset" {
				if n > 10000 {
					return f, ErrInvalid
				}
				f.Offset = n
			} else {
				if n < 1 || n > 100 {
					return f, ErrInvalid
				}
				f.Limit = n
			}
		default:
			return f, ErrInvalid
		}
	}
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		return f, ErrInvalid
	}
	return f, nil
}

// Only the byte snapshot holds auditMu. Parsing, config reads and session locks
// happen afterwards, preserving the runtime -> audit writer lock direction.
func (s *Server) auditSnapshot() ([]byte, error) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	parent, e := os.OpenRoot(filepath.Dir(s.opts.AuditPath))
	if e != nil {
		return nil, ErrSource
	}
	defer parent.Close()
	name := filepath.Base(s.opts.AuditPath)
	before, e := parent.Lstat(name)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, ErrSource
	}
	if !privateAuditFile(before) {
		return nil, ErrSource
	}
	f, e := parent.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, ErrSource
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !privateAuditFile(st) || !os.SameFile(before, st) || st.Size() > auditMaxBytes {
		return nil, ErrSource
	}
	raw, e := io.ReadAll(io.LimitReader(f, auditMaxBytes+1))
	if e != nil || len(raw) > auditMaxBytes {
		return nil, ErrSource
	}
	return raw, nil
}
func privateAuditFile(st os.FileInfo) bool {
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return false
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	return ok && owner.Uid == 0
}
func parseAuditSnapshot(raw []byte) ([]auditEntry, bool, error) {
	lines := bytes.Split(raw, []byte{'\n'})
	pending := len(lines[len(lines)-1]) > 0
	if len(lines)-1 > auditMaxLines || len(lines[len(lines)-1]) > 4096 {
		return nil, false, ErrSource
	}
	entries := make([]auditEntry, 0, len(lines)-1)
	for _, line := range lines[:len(lines)-1] {
		if len(line) == 0 || len(line) > 4096 || uniqueConfigKeys(json.NewDecoder(bytes.NewReader(line)), 0) != nil {
			return nil, false, ErrSource
		}
		var entry auditEntry
		d := json.NewDecoder(bytes.NewReader(line))
		d.DisallowUnknownFields()
		if d.Decode(&entry) != nil || entry.UTC.IsZero() {
			return nil, false, ErrSource
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return nil, false, ErrSource
		}
		// Only an exact original six-key object can use legacy time attribution.
		// Presence of even an empty modern field requires a complete modern anchor.
		var fields map[string]json.RawMessage
		if json.Unmarshal(line, &fields) != nil {
			return nil, false, ErrSource
		}
		entry.legacy = len(fields) == 6
		for _, key := range []string{"utc", "actorID", "scope", "action", "deviceID", "recordID"} {
			if _, ok := fields[key]; !ok {
				entry.legacy = false
			}
		}
		entries = append(entries, entry)
	}
	return entries, pending, nil
}
func auditClass(action string) (class, description string) {
	switch action {
	case "read_conversation", "read_chat", "list_chats", "read_memory", "read_memories", "list_memories":
		return "access", "查看记录"
	case "device_control_requested":
		return "device_control", "调整设备"
	case "clear_memory_requested":
		return "memory_clear", "清除近期记忆"
	case "update_device_settings_requested":
		return "settings", "修改设备信息"
	}
	return "", ""
}
func auditSameTarget(request, result auditEntry) bool {
	expected := "device_control_result"
	if request.Action == "clear_memory_requested" {
		expected = "clear_memory_result"
	}
	return result.Action == expected && request.OperationID == result.OperationID && request.RecordID == result.RecordID && request.ActorID == result.ActorID && request.Scope == result.Scope && request.DeviceID == result.DeviceID && request.FamilyID == result.FamilyID && request.BindingID == result.BindingID && request.BindingVersion == result.BindingVersion && request.Field == result.Field && auditSameInt(request.Value, result.Value)
}
func auditSameInt(a, b *int) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
func auditAnchor(c Config, viewer string, e auditEntry) (Binding, bool) {
	if !device.MatchString(e.DeviceID) || (e.Scope != "family" && e.Scope != "admin") {
		return Binding{}, false
	}
	b, err := c.Authorize(viewer, "admin", e.DeviceID, e.UTC, false)
	if err != nil {
		return b, false
	}
	// Old access rows carry access time but no resource binding. After a
	// cross-family transfer that time cannot prove which household's history
	// was read, regardless of the event actor's current grants or existence.
	if class, _ := auditClass(e.Action); e.legacy && class == "access" {
		for _, other := range c.Bindings {
			if other.DeviceID == e.DeviceID && other.FamilyID != b.FamilyID && !e.UTC.Before(other.From) {
				return b, false
			}
		}
	}
	if !e.legacy && ((e.SchemaVersion != 0 && e.SchemaVersion != 1) || e.FamilyID == "" || e.BindingID == "" || e.BindingVersion < 1) {
		return b, false
	}
	if (e.FamilyID != "" && e.FamilyID != b.FamilyID) || (e.BindingID != "" && e.BindingID != b.ID) || (e.BindingVersion != 0 && e.BindingVersion != b.Version) {
		return b, false
	}
	return b, true
}
func auditProjection(c Config, viewer string, now time.Time, entries []auditEntry, f AuditFilter, pending bool, idKey []byte) AuditPage {
	page := AuditPage{Items: []AuditItem{}, Pending: pending}
	// Result-only rows never create anchors. A reused operation ID with different
	// request metadata is ambiguous and cannot establish a terminal confirmation.
	results := map[string][]auditEntry{}
	requests := map[string][]auditEntry{}
	for _, e := range entries {
		if e.OperationID != "" {
			if e.Action == "device_control_result" || e.Action == "clear_memory_result" {
				results[e.OperationID] = append(results[e.OperationID], e)
			} else {
				requests[e.OperationID] = append(requests[e.OperationID], e)
			}
		}
	}
	type row struct {
		item  AuditItem
		at    time.Time
		order int
	}
	rows := []row{}
	for i, e := range entries {
		class, description := auditClass(e.Action)
		if class == "" || f.DeviceID != "" && e.DeviceID != f.DeviceID || f.Action != "" && class != f.Action || f.From != nil && e.UTC.Before(*f.From) || f.To != nil && !e.UTC.Before(*f.To) {
			continue
		}
		b, ok := auditAnchor(c, viewer, e)
		if !ok {
			continue
		}
		item := AuditItem{Time: e.UTC.UTC().Format(time.RFC3339Nano), Actor: "历史账号", DeviceID: e.DeviceID, Target: "历史设备", Action: class, Description: description, Status: "unknown", Scope: e.Scope}
		// A server-private MAC makes IDs stable during the session without
		// allowing a caller to brute-force the hidden global line position.
		mac := hmac.New(sha256.New, idKey)
		_, _ = mac.Write([]byte(strconv.Itoa(i) + "\x00" + e.UTC.String() + "\x00" + e.OperationID + "\x00" + e.ActorID + "\x00" + e.DeviceID))
		item.ID = "audit_" + hex.EncodeToString(mac.Sum(nil)[:16])
		current, err := c.Authorize(viewer, "admin", e.DeviceID, now, false)
		item.CanOpenDevice = err == nil
		if err == nil && current.FamilyID == b.FamilyID {
			for _, d := range c.Devices {
				if d.ID == e.DeviceID {
					item.Target = d.Name
					if d.DisplayName != "" {
						item.Target = d.DisplayName
					}
				}
			}
			if actor, exists := c.AccountByID(e.ActorID); exists {
				for _, m := range actor.Memberships {
					if m.FamilyID == b.FamilyID {
						for _, g := range m.Devices {
							if g.DeviceID == e.DeviceID {
								item.Actor = actor.Username
							}
						}
					}
				}
			}
		}
		if class == "access" {
			item.Status = "recorded"
		} else {
			if e.Field == "volume" || e.Field == "brightness" {
				item.Field = e.Field
				if e.Value != nil && *e.Value >= 0 && *e.Value <= 100 {
					item.Value = e.Value
				}
			}
			if e.OperationID != "" && len(requests[e.OperationID]) == 1 {
				terminal := ""
				var actual *int
				conflict := false
				for _, result := range results[e.OperationID] {
					if !auditSameTarget(e, result) || result.UTC.Before(e.UTC) {
						continue
					}
					status := result.Outcome
					if status != "confirmed" && status != "failed" && status != "unknown" {
						status = "unknown"
					}
					if class == "device_control" && status == "confirmed" && (item.Value == nil || result.Actual == nil || !auditSameInt(item.Value, result.Actual)) {
						status = "unknown"
					}
					if terminal != "" && (terminal != status || !auditSameInt(actual, result.Actual)) {
						conflict = true
					}
					terminal = status
					actual = result.Actual
				}
				if terminal != "" && !conflict {
					item.Status = terminal
					if actual != nil && *actual >= 0 && *actual <= 100 {
						item.Actual = actual
					}
				}
			}
		}
		switch item.Status {
		case "recorded":
			item.Result = "访问已记录"
		case "confirmed":
			item.Result = "执行已确认"
		case "failed":
			item.Result = "执行失败"
		default:
			item.Result = "执行结果未知"
		}
		rows = append(rows, row{item, e.UTC, i})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].at.Equal(rows[j].at) {
			return rows[i].order > rows[j].order
		}
		return rows[i].at.After(rows[j].at)
	})
	start := f.Offset
	if start > len(rows) {
		start = len(rows)
	}
	end := start + f.Limit
	if end > len(rows) {
		end = len(rows)
	}
	for _, r := range rows[start:end] {
		page.Items = append(page.Items, r.item)
	}
	page.HasMore = end < len(rows)
	return page
}
func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	if parts[0] != "admin" || len(parts) != 2 {
		sourceError(w, ErrNotFound)
		return
	}
	if r.Method != "GET" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	f, e := parseAuditFilter(r.URL.RawQuery)
	if e != nil {
		sourceError(w, e)
		return
	}
	raw, e := s.auditSnapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	entries, pending, e := parseAuditSnapshot(raw)
	if e != nil {
		sourceError(w, e)
		return
	}
	c, e := s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	fresh := r.WithContext(context.WithValue(r.Context(), configContextKey{}, c))
	actor, ok := s.account(fresh, "admin")
	if !ok || actor.ID != a.ID {
		sourceError(w, ErrForbidden)
		return
	}
	// account may wait for the session mutex. Refresh authority after that
	// wait too; a config update during the wait must not retain old grants.
	c, e = s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	actor, ok = c.AccountByID(a.ID)
	if !ok || !actor.InternalAdmin {
		sourceError(w, ErrForbidden)
		return
	}
	writeJSON(w, auditProjection(c, actor.ID, s.now(), entries, f, pending, s.auditIDKey[:]))
}
