# 独立 Realtime 体验测试实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 沿用已部署的小智/Qwen Realtime网关，新增隔离测试实例与Mac原生入口，与三段式交替体验。

**Architecture:** 用户补充现有Realtime已接小智；核对8000服务版本5772dfb，已有1个会话且上限1。使用现有`internal/gateway`/`internal/qwen`/`internal/audio`，不重写语音核心；独立进程18030只加短期票据入口，专用测试设备身份，禁用持久记录。Mac复用现有AEC、Opus和CoreAudio代码，单独实现旧小智协议客户端。

**Tech Stack:** Go1.25+、已有libopus；Swift5/AppKit/AVFoundation。

**Spec:** `docs/superpowers/specs/2026-10-24-dual-voice-backends-design.md`第0节；最新补充以已有Realtime/小智部署为基础。

## Global Constraints

- 直接main；不占用/重启8000已有会话，不改18010/18020或旧Mac客户端。
- 独立进程、回环端口、配置和设备；凭据只留服务器，Mac只获取120秒一次性票据。
- 云端运行Go回归/构建；Mac仅构建运行原生测试终端，不安装依赖或启动本地服务。
- 不接三段式历史、任务、准备回应；不新做统一记忆或双后端路由。
- 采用已有Realtime网关当前的上游取消/重连与任务规则，不新添修复机制；其话术、等待或续讲限制都属于本次实际实现的体验结果。
- Mac不伪装I2S证据；不协商仅接受`i2s_dma`的旧播放回报能力。CoreAudio完成只在独立客户端记录。
- 两边同硬件、16k上行/24k下行、Opus；Realtime沿用原网关立即停播，三段式drain差异单列。

## Review Focus

- 一次性票据、Origin、并发及OTA：不暴露静态凭据，不触发未授权模型会话。
- Mac旧协议接线：不能混用drain/staging消息或假称支持I2S回报。
- 中断后的迟到完成：不能关闭新回复或重新播放旧声音。
- 正常播放结束：等待实际CoreAudio完成，不把tts.stop当最后一帧已播放。
- 关闭/失败/断网：释放麦克风、SSH转发和模型连接，窗口可重新开始。

## Task 1: 复用云端网关的隔离入口

**Files:** `internal/realtimeentry/{server.go,server_test.go}`、`cmd/realtime-mac-test/main.go`、`deploy/family-robot-realtime-test.service`。
**Interfaces:** `New(qwen.Config)`→`Handler()/Close()`，`/v1/pair`短期ticket，`/xiaozhi/v1/`标准小智v1；入口内部映射固定测试MAC和随机网关token。`/healthz`公开模型别名/音色/活动数，不含凭据或正文。

- [x] 写测试并在云端确认缺失New/Server失败。
- [x] 实现票据、认证和网关复用，不引入新的语音处理。
- [x] 云端`go test -race ./internal/realtimeentry ./internal/gateway ./internal/qwen`通过，提交。

## Task 2: 独立小智协议Mac终端

**Files:** `tools/realtime-voice/{build.command,open.command,Info.plist,README.md,Sources/*.swift,Tests/*}`。
**Interfaces:** 复用`tools/native-voice/Sources/{AudioPipeline,MicrophonePCM,Opus,PlaybackLedger}.swift`（最后一个只为已有ClientFailure）；客户端只处理hello/listen/tts/stt/alert及Opus。开始时配对，单按钮持续对话，关闭才退出。

- [x] 先写纯Swift播放状态/endpoint规范测试并验证失败。
- [x] 实现旧协议客户端，正常结束等CoreAudio回报；中断立即结束旧播放器；输入/写队列有界。
- [x] 构建、状态测试、机器合成fixture真实CoreAudio播放通过，提交。

## Task 3: 部署、验证与体验入口

**Files:** 原生合成fixture入口、两份README、`docs/verification/2026-10-24-independent-realtime.md`与方案/交接状态。
**Interfaces:** 使用Task1入口与Task2客户端；合成输入和生成文字断言在内存完成，凭据/真人记录不进Git。

- [x] 独立配置仅保留上游模型/Key/音色/VAD，启动18030新服务；旧服务PID/活动不变。
- [x] 真实模型连续问答、上下文指代与插问新问题通过；保留失败/耗时，准确说明旧网关本身有重连/任务机制。
- [x] 云端全库Go检查和原Python214项回归；Mac原生收播合成fixture。
- [x] 独立审查、提交推送并核对远端main；打开新窗口保留停止收音供用户体验。
