# Voice Gateway Implementation Plan

> Execute inline in the current session. The user approved implementation and deployment of the minimal gateway; no additional design approval is pending.

**Goal:** M5StopWatch 小智固件通过自研 Go 网关连接千问实时语音，支持对话及打断。

**Architecture:** 有界、单循环会话调度，libopus 转换音频；独立 protocol/audio/qwen/gateway 包。打断清队列、重建模型会话并抑制旧连接事件。

**Tech Stack:** Go 1.25、Gorilla WebSocket、系统 libopus、Ubuntu systemd。

## Global Constraints

- 不引入家庭业务、数据库、AIRI、Mem0 或通用 agent 框架。
- 不记录密钥、原始上游错误、音频或用户转录。
- 每个行为先写测试并运行失败，再实现；只将实际验证过的范围报告为通过。
- 当前目录尚无 Git 仓库，不为计划文档自动创建或提交仓库。

## Tasks

- [x] protocol: `internal/protocol` 定义小智 hello、控制消息及 v1/v2/v3 封装。测试 `go test ./internal/protocol` 覆盖长度错误、版本不匹配、网络字节序和往返。
- [x] audio: `internal/audio` 包装 libopus 与 60 ms PCM 队列。测试 `go test ./internal/audio` 覆盖真实编码解码、分块残帧、清空和上限。
- [x] qwen: `internal/qwen` 定义事件及带超时的 session.update 握手；`go test ./internal/qwen` 使用假 WebSocket 服务验证鉴权、参数、取消和错误遮蔽。
- [x] gateway: `internal/gateway` 实现鉴权、OTA、WebSocket 会话、按时播放、打断重建、输入重放和断线释放；`go test -race ./internal/gateway` 使用真实本地 WebSocket 验证迟到帧和新轮次。
- [x] runtime: `cmd/gateway`、环境变量样例、systemd 文件和使用文档；`go test -race ./...`、`go vet ./...`、`go build ./cmd/gateway`。
- [x] smoke: `cmd/smoke` 模拟小智设备，以现有 WAV 转成 Opus 发送，接收 Opus 并保存 WAV/JSON；远程部署后验证两轮真实音频、一次 abort 和新连接可用。
- [x] report: 记录部署版本、接口、测试证据和真实硬件待验项目，提供设备配置步骤。
