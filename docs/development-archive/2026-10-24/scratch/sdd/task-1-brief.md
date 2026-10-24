## Task 1: 设备工具状态机

Files: 新建 `internal/devicetools/bridge.go` 与 `bridge_test.go`。

Interfaces:

```go
type Completion struct { CallID string; Generation uint64; Output string }
type Update struct { Requests []json.RawMessage; Results []Completion }
func New(now time.Time) *Bridge
func (b *Bridge) Start(now time.Time) Update
func (b *Bridge) Receive(payload json.RawMessage, now time.Time) Update
func (b *Bridge) Expire(now time.Time) Update
func (b *Bridge) Call(callID, name, arguments string, generation uint64, now time.Time) Update
func (b *Bridge) CancelGeneration(generation uint64, now time.Time) Update
func (b *Bridge) Ready() bool
func (b *Bridge) Tools() []map[string]any
```

- [ ] 先写测试并观察失败：initialize → tools/list（含分页）→ 白名单发现；调用与状态核对；错误、超时、非法参数、迟到结果、取消和队列上限。
- [ ] 实现上述接口，固定公开名 `device_get_status`、`device_set_volume`、`device_set_brightness`，映射小智同名功能。
- [ ] 发现总超时 3 秒、最多 8 页；调用总超时 5 秒、最多 4 项在途/排队，按顺序执行设置与核对。
- [ ] 状态返回只保留音量和亮度；不把网络 SSID、任意设备描述或原始错误送入模型。
- [ ] 运行 `go test -race ./internal/devicetools`。


## Integration contract

New package only: internal/devicetools. Root handles all other source files. Completion.Output is a JSON string with ok/verified and filtered values or a safe error tag. Bridge.Call uses fixed public names from the brief, arguments is the raw model JSON argument string. Tools returns Qwen session.tools entries using {"type":"function","function":{"name":...,"description":...,"parameters":...}}.
MCP payloads exclude the outer {"type":"mcp","payload":...} wrapper; root supplies wrapper. Incoming JSON RPC IDs are numeric. Ready is true once discovery succeeds OR fails/times out. On discovery failure Tools is empty. Only register setters when status plus that setter were discovered.
Device tools: self.get_device_status, self.audio_speaker.set_volume(volume), self.screen.set_brightness(brightness). MCP replies use result.content[{type:"text",text:"true"}] for setters and a JSON string of {audio_speaker:{volume:70},screen:{brightness:80},network:{ssid:...}} for status. Never forward network/other fields. Verify setters by an additional status readback in the SAME serialized call. Discovery uses initialize then tools/list pages, nextCursor string; notifications/initialized may be emitted, but device ignores notifications.
Calls are FIFO, bounded 4 total; deadline includes queued time, 5 seconds. CancelGeneration cancels unfinished calls for that model generation, ignores late RPC replies, removes queued ones, and returns requests starting any remaining valid call. Do not undo physical mutations already sent. Deduplicate call_id. Validate bounds, integers, unknown fields, trailing JSON, unavailable names. No background goroutines or I/O in package.
Use real deterministic state-machine tests including setter ACK without readback not being success, readback mismatch, errors, timeouts, page/cursor bounds, duplicates, cancellation, privacy filtering. No Git repository: do not commit or initialize Git. Save red/green evidence and self-review to .superpowers/sdd/task-1-report.md and return short status.
