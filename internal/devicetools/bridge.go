// Package devicetools translates a small, fixed device-control surface into MCP.
package devicetools

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

const (
	statusName             = "self.get_device_status"
	volumeName             = "self.audio_speaker.set_volume"
	brightnessName         = "self.screen.set_brightness"
	discoveryTimeout       = 3 * time.Second
	callTimeout            = 5 * time.Second
	brightnessPollInterval = 100 * time.Millisecond
	maxPages               = 8
	maxCursorLength        = 1024
	maxCalls               = 4
)

// Completion.Output contains only fixed error tags or filtered device values.
type Completion struct {
	CallID     string
	Generation uint64
	Output     string
}

// Update contains MCP payloads without their transport wrapper and model results.
type Update struct {
	Requests []json.RawMessage
	Results  []Completion
}

type deviceCall struct {
	id         string
	generation uint64
	name       string
	arguments  map[string]int
	deadline   time.Time
	rpcID      uint64
	readback   bool
	readbackAt time.Time
}

// Bridge is a deterministic state machine. Its owner must serialize method calls
// and deliver timer ticks through Expire; it starts no goroutines and performs no I/O.
type Bridge struct {
	started, ready, discovered bool
	discoveryDeadline          time.Time
	discoveryID                uint64
	listing                    bool
	pages                      int
	cursors                    map[string]bool
	found                      map[string]bool
	nextID                     uint64
	seen                       map[string]bool
	active                     *deviceCall
	queue                      []*deviceCall
}

func New(_ time.Time) *Bridge {
	return &Bridge{cursors: make(map[string]bool), found: make(map[string]bool), seen: make(map[string]bool)}
}

func (b *Bridge) Start(now time.Time) Update {
	u := b.Expire(now)
	if b.started {
		return u
	}
	b.started = true
	b.discoveryDeadline = now.Add(discoveryTimeout)
	b.discoveryID = b.rpc(&u, "initialize", map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "familyrobot", "version": "1.0.0"},
	})
	return u
}

func (b *Bridge) Receive(payload json.RawMessage, now time.Time) Update {
	u := b.Expire(now)
	var r struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(payload, &r) != nil || r.JSONRPC != "2.0" || r.ID == 0 {
		return u
	}
	failed := len(r.Error) != 0 && string(r.Error) != "null"
	if !b.ready && b.started && r.ID == b.discoveryID {
		if failed {
			b.finishDiscovery(false)
			return u
		}
		if !b.listing {
			var obj map[string]json.RawMessage
			if json.Unmarshal(r.Result, &obj) != nil || obj == nil {
				b.finishDiscovery(false)
				return u
			}
			b.listing = true
			b.discoveryID = b.rpc(&u, "tools/list", map[string]any{})
			return u
		}
		b.receivePage(r.Result, &u)
		return u
	}
	c := b.active
	if c == nil || r.ID != c.rpcID {
		return u
	}
	if failed {
		b.finishCall(&u, errorOutput("device_error"))
		b.startNext(&u)
		return u
	}
	texts, tag := content(r.Result)
	if tag != "" {
		b.finishCall(&u, errorOutput(tag))
		b.startNext(&u)
		return u
	}
	if c.name != "device_get_status" && !c.readback {
		if len(texts) != 1 || strings.TrimSpace(texts[0]) != "true" {
			b.finishCall(&u, errorOutput("device_error"))
			b.startNext(&u)
			return u
		}
		c.readback = true
		c.rpcID = b.rpc(&u, "tools/call", map[string]any{"name": statusName, "arguments": map[string]int{}})
		return u
	}
	values, ok := statusValues(texts)
	if !ok {
		b.finishCall(&u, errorOutput("invalid_response"))
		b.startNext(&u)
		return u
	}
	if c.readback {
		for field, desired := range c.arguments {
			if actual, exists := values[field]; !exists || actual != desired {
				if c.name == "device_set_brightness" && exists {
					// Firmware reports its current brightness while fading toward the
					// target. Observe again without repeating the mutation or extending
					// the call deadline, and retire this consumed RPC immediately.
					c.rpcID = 0
					c.readbackAt = now.Add(brightnessPollInterval)
					return u
				}
				b.finishCall(&u, errorOutput("verification_failed"))
				b.startNext(&u)
				return u
			}
		}
	}
	out := map[string]any{"ok": true, "verified": true}
	for k, v := range values {
		out[k] = v
	}
	b.finishCall(&u, jsonOutput(out))
	b.startNext(&u)
	return u
}

func (b *Bridge) Expire(now time.Time) Update {
	u := Update{}
	if b.started && !b.ready && !now.Before(b.discoveryDeadline) {
		b.finishDiscovery(false)
	}
	if b.active != nil && !now.Before(b.active.deadline) {
		b.finishCall(&u, errorOutput("timeout"))
	}
	remaining := b.queue[:0]
	for _, c := range b.queue {
		if !now.Before(c.deadline) {
			u.Results = append(u.Results, completion(c, errorOutput("timeout")))
		} else {
			remaining = append(remaining, c)
		}
	}
	b.queue = remaining
	if c := b.active; c != nil && !c.readbackAt.IsZero() && !now.Before(c.readbackAt) {
		c.readbackAt = time.Time{}
		c.rpcID = b.rpc(&u, "tools/call", map[string]any{"name": statusName, "arguments": map[string]int{}})
	}
	b.startNext(&u)
	return u
}

func (b *Bridge) Call(callID, name, arguments string, generation uint64, now time.Time) Update {
	u := b.Expire(now)
	if b.seen[callID] {
		return u
	}
	b.seen[callID] = true
	c := &deviceCall{id: callID, generation: generation, name: name, deadline: now.Add(callTimeout)}
	fail := func(tag string) Update { u.Results = append(u.Results, completion(c, errorOutput(tag))); return u }
	if callID == "" {
		return fail("invalid_arguments")
	}
	if !b.ready {
		return fail("not_ready")
	}
	if !b.available(name) {
		return fail("unavailable")
	}
	args, ok := parseArguments(name, arguments)
	if !ok {
		return fail("invalid_arguments")
	}
	c.arguments = args
	count := len(b.queue)
	if b.active != nil {
		count++
	}
	if count >= maxCalls {
		return fail("queue_full")
	}
	b.queue = append(b.queue, c)
	b.startNext(&u)
	return u
}

// CancelGeneration drops unfinished work. Mutations already sent to the device
// cannot be undone; their eventual replies are ignored because RPC IDs are unique.
func (b *Bridge) CancelGeneration(generation uint64, now time.Time) Update {
	// Cancel before advancing the queue so canceled work can never be started here.
	u := Update{}
	if b.active != nil && b.active.generation == generation {
		b.finishCall(&u, errorOutput("canceled"))
	}
	remaining := b.queue[:0]
	for _, c := range b.queue {
		if c.generation == generation {
			u.Results = append(u.Results, completion(c, errorOutput("canceled")))
		} else {
			remaining = append(remaining, c)
		}
	}
	b.queue = remaining
	expired := b.Expire(now)
	u.Results = append(u.Results, expired.Results...)
	u.Requests = append(u.Requests, expired.Requests...)
	return u
}

// Ready means discovery has finished, including failure or timeout.
func (b *Bridge) Ready() bool { return b.ready }

// Tools never copies device-supplied schemas, descriptions, or unapproved names.
func (b *Bridge) Tools() []map[string]any {
	var tools []map[string]any
	for _, def := range []struct{ name, field, description string }{
		{"device_get_status", "", "读取设备当前音量和屏幕亮度。"},
		{"device_set_volume", "volume", "设置设备音量（0 到 100 的整数），并读取状态核对。"},
		{"device_set_brightness", "brightness", "设置屏幕亮度（0 到 100 的整数），并读取状态核对。"},
	} {
		if !b.available(def.name) {
			continue
		}
		properties := map[string]any{}
		required := []string{}
		if def.field != "" {
			properties[def.field] = map[string]any{"type": "integer", "minimum": 0, "maximum": 100}
			required = append(required, def.field)
		}
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
			"name": def.name, "description": def.description,
			"parameters": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
		}})
	}
	return tools
}

func (b *Bridge) available(name string) bool {
	if !b.discovered || !b.found[statusName] {
		return false
	}
	switch name {
	case "device_get_status":
		return true
	case "device_set_volume":
		return b.found[volumeName]
	case "device_set_brightness":
		return b.found[brightnessName]
	default:
		return false
	}
}

func (b *Bridge) receivePage(raw json.RawMessage, u *Update) {
	var p struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		NextCursor json.RawMessage `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Tools == nil {
		b.finishDiscovery(false)
		return
	}
	b.pages++
	for _, tool := range p.Tools {
		switch tool.Name {
		case statusName, volumeName, brightnessName:
			b.found[tool.Name] = true
		}
	}
	cursor := ""
	if len(p.NextCursor) > 0 && string(p.NextCursor) != "null" {
		if json.Unmarshal(p.NextCursor, &cursor) != nil {
			b.finishDiscovery(false)
			return
		}
	}
	if cursor == "" {
		b.finishDiscovery(true)
		return
	}
	if b.pages >= maxPages || len(cursor) > maxCursorLength || b.cursors[cursor] {
		b.finishDiscovery(false)
		return
	}
	b.cursors[cursor] = true
	b.discoveryID = b.rpc(u, "tools/list", map[string]any{"cursor": cursor})
}

func (b *Bridge) finishDiscovery(ok bool) {
	b.ready, b.discovered, b.discoveryID = true, ok, 0
	if !ok {
		clear(b.found)
	}
}

func (b *Bridge) startNext(u *Update) {
	if b.active != nil || len(b.queue) == 0 {
		return
	}
	c := b.queue[0]
	b.queue[0] = nil
	b.queue = b.queue[1:]
	b.active = c
	deviceName := statusName
	switch c.name {
	case "device_set_volume":
		deviceName = volumeName
	case "device_set_brightness":
		deviceName = brightnessName
	}
	c.rpcID = b.rpc(u, "tools/call", map[string]any{"name": deviceName, "arguments": c.arguments})
}

func (b *Bridge) rpc(u *Update, method string, params any) uint64 {
	b.nextID++
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": b.nextID, "method": method, "params": params})
	u.Requests = append(u.Requests, raw)
	return b.nextID
}

func (b *Bridge) finishCall(u *Update, output string) {
	u.Results = append(u.Results, completion(b.active, output))
	b.active = nil
}

func completion(c *deviceCall, output string) Completion {
	return Completion{CallID: c.id, Generation: c.generation, Output: output}
}
func jsonOutput(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func errorOutput(tag string) string {
	return jsonOutput(map[string]any{"ok": false, "verified": false, "error": tag})
}

// ValidateArguments lets a planner validate a complete batch before dispatching
// any call, using the same strict validator as the device bridge.
func ValidateArguments(name, arguments string) bool {
	_, ok := parseArguments(name, arguments)
	return ok
}

func parseArguments(name, arguments string) (map[string]int, bool) {
	decoder := json.NewDecoder(strings.NewReader(arguments))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, false
	}
	args := map[string]int{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		field, ok := key.(string)
		if !ok {
			return nil, false
		}
		if _, exists := args[field]; exists {
			return nil, false
		}
		if !((name == "device_set_volume" && field == "volume") || (name == "device_set_brightness" && field == "brightness")) {
			return nil, false
		}
		var raw json.RawMessage
		var value int
		if decoder.Decode(&raw) != nil || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || value < 0 || value > 100 {
			return nil, false
		}
		args[field] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	return args, name == "device_get_status" || len(args) == 1
}

func content(raw json.RawMessage) ([]string, string) {
	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return nil, "invalid_response"
	}
	if r.IsError {
		return nil, "device_error"
	}
	var texts []string
	for _, item := range r.Content {
		if item.Type == "text" {
			texts = append(texts, item.Text)
		}
	}
	if len(texts) == 0 {
		return nil, "invalid_response"
	}
	return texts, ""
}

func statusValues(texts []string) (map[string]int, bool) {
	if len(texts) != 1 {
		return nil, false
	}
	var raw struct {
		Speaker map[string]json.RawMessage `json:"audio_speaker"`
		Screen  map[string]json.RawMessage `json:"screen"`
	}
	if json.Unmarshal([]byte(texts[0]), &raw) != nil {
		return nil, false
	}
	values := map[string]int{}
	for field, value := range map[string]json.RawMessage{"volume": raw.Speaker["volume"], "brightness": raw.Screen["brightness"]} {
		var n int
		if len(value) > 0 && !bytes.Equal(value, []byte("null")) && json.Unmarshal(value, &n) == nil && n >= 0 && n <= 100 {
			values[field] = n
		}
	}
	return values, len(values) > 0
}
