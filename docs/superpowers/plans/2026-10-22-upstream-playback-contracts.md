# 上游播放契约复用实施记录

> **For agentic workers:** Use superpowers:executing-plans to implement the confirmed scope inline, task by task. Do not create branches or worktrees.

**Goal:** 将已核验的千问输出封口和迟到事件隔离规则用于现有 Go 网关，并明确历史中的传输完成不等于设备已播放。

**Architecture:** 保持现有千问连接策略和60ms下发节奏。借鉴 Qwen-Live-Harness 的显式结束标记、有界完成代号集合；借鉴 LiveKit 的播放确认语义，正常下发只记录 `delivery=transport_sent`，不制造硬件确认或逐字进度。此改动是后续接续同步的基础，不能单独修复接续位置。

**Tech Stack:** Go、Gorilla WebSocket、libopus；参考项目固定提交见验证记录。

## Global Constraints

- 仅在 `main` 开发；不改正式服务、不刷机、不启用设备收音和额外录音。
- 不接辅助 ASR，不切模型，不迁移 LiveKit，不把无 ACK 的 truncate 当成受支持能力。
- 保持原有120秒待播上限、12条历史上限和512字单条历史上限。
- 已完成回复代号仅保留当前上游连接最近256个；换连接清空，旧连接仍由现有 generation 隔离。
- 不复制完整框架；在第三方记录中标明参考来源和许可证。

## Task 1: 播放生命周期与历史语义

Files: `internal/gateway/response_lifecycle.go`、`session.go`、`playback.go`、`history.go`、`response_lifecycle_test.go`、`playback_test.go`、`internal/qwen/client.go`。

- [x] 先添加行为回归：结束后迟到音频/文字不能增加队列或改写稿件；完成的旧 `response.created` 不能打开第二个 TTS 块；正常发送不产生“已播放确认”的历史。
- [x] 用 `go test ./internal/gateway -run 'TestSealed|TestRetired|TestTransportHistory' -count=1` 观察失败，保留 RED 证据。
- [x] 实施最小改动：`responseDone` 作为结束封口；最近完成代号 map + FIFO 上限256；正常结束记 `delivery=transport_sent`，上下文提示明确没有硬件 ACK。重复结束事件忽略；真正的新重叠回复仍报错。
- [x] 对当前 `generation` 内的旧代号检查；正常排空后退休；`connect()` 切代清空集合。不得让集合跨连接无限增长。
- [x] 跑回归及 `go test -race ./...`、`go vet ./...`；在 Linux 隔离源目录再跑完整测试。

## Task 2: 证据与边界

- [x] 写入固定上游提交、原版测试范围、真实千问合成探针结果及源码到 Go 的映射。
- [x] 明确 create/delete 已 ACK、truncate 8秒无 ACK；单连接恢复仍错误；0.66秒合成短语没有触发上游 VAD，不能误记为上下文 API 失败。
- [x] 记录当前没有设备播放确认，LiveKit 无时间标注时会估算文本位置；下一实施重点是设备播放回报和上下文修正，再评估辅助 ASR。
- [ ] 检查 diff、敏感数据及正式服务身份，提交并推送 `main`。不把单测通过写成实机接续通过。
