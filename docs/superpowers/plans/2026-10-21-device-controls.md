# StopWatch Device Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 让真实 StopWatch 执行音量、屏幕亮度/息屏指令并返回核实结果，修复已复现的音频发送节奏漂移。

**Architecture:** 保留小智固件，在 Go 网关桥接设备 MCP 与千问原生 Function Calling。设备工具协议放在独立状态机中，I/O 留在会话事件循环；音频按既定时间线发送。

**Tech Stack:** Go 1.25+、Gorilla WebSocket、libopus、Qwen Realtime、现有小智 v2.5.2。

## Global Constraints

- 仅设备状态、音量 0—100、亮度 0—100；息屏为亮度 0。
- 设置后读取状态，目标不一致或无回执时不得返回已核实成功。
- 不阻塞音频循环；请求有界、超时有界、参数严格校验，迟到结果不得触发新轮次回复。
- 不记录 API Key、设备 token、OTA 引导密钥、原始设备网络信息。
- 当前工作目录没有 Git 仓库，不执行提交、建分支或创建 PR。

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

- [x] 先写测试并观察失败：initialize → tools/list（含分页）→ 白名单发现；调用与状态核对；错误、超时、非法参数、迟到结果、取消和队列上限。
- [x] 实现上述接口，固定公开名 `device_get_status`、`device_set_volume`、`device_set_brightness`，映射小智同名功能。
- [x] 发现总超时 3 秒、最多 8 页；调用总超时 5 秒、最多 4 项在途/排队，按顺序执行设置与核对。
- [x] 状态返回只保留音量和亮度；不把网络 SSID、任意设备描述或原始错误送入模型。
- [x] 运行 `go test -race ./internal/devicetools`。

## Task 2: 音频调度漂移回归与修复

Files: `internal/gateway/playback.go`、新增 `playback_test.go`。

- [x] 先写真实 WebSocket 写出回归，在存在微小 tick 抖动时验证多帧完成时间与既定 60ms 时间线相符；旧代码应因累计推迟而失败。
- [x] 下一帧时间基于原定时间推进，而非每次 `now + 60ms`；明显停顿/缺音后重置锚点，避免突发补发。
- [x] 补停顿恢复及末帧/停止验证，运行 `go test -race ./internal/gateway`。

## Task 3: 千问工具与会话桥接

Files: `internal/qwen/client.go`、`client_test.go`、`internal/gateway/session.go`、新增 `session_tools.go` 与 `session_tools_test.go`。

- [x] 写失败集成测试：支持 MCP 的模拟设备完成发现；Qwen session.update 含固定工具；模型调用 → MCP 设置 → 状态核实 → function_call_output → response.create → 语音回复。
- [x] Qwen Config 增加工具定义；Event 接收函数名、call_id、arguments。使用官方嵌套 `function` 工具结构。
- [x] 设备声明 MCP 时在建立上游前做有界工具发现；等待期间保存最多 128 个设备事件，发现失败继续纯语音并明确控制不可用。
- [x] 完整函数参数事件驱动桥接；父回复完成且音频排空、所有本轮工具结果齐全后，回传工具输出并请求续答。
- [x] 每次用户转写最多 8 次工具调用；取消上游轮次时取消旧代号的排队工具和续答。
- [x] 测试失败回执、超时、重复调用、打断后迟到结果；运行 `go test -race ./...`、`go vet ./...` 和两个命令构建。

## Task 4: 实际模型、设备与发布

- [x] 只读复核实现与测试，修复重要问题。
- [x] Linux 运行完整检查，生成独立版本目录及源码/二进制摘要，保留回退版本。
- [x] 真实千问与模拟 MCP 验证原生调用及相对调节；真实手表验证音量、亮度、息屏和恢复亮度，用户确认实际变化与回复一致。
- [ ] 用户现场恢复音量80、亮度50后的状态回读确认；最后已记录100/50，尚未看到恢复调用。
- [x] 重新抓取音频帧间隔，比较原来平均 64—65ms 与修复后的节奏；不以抓包代替实际杂音听感。
- [x] 更新 README、设计状态与验证记录；分别报告控制执行结果、调度修复和杂音现场验收状态。
