# Realtime Go 等价迁移实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把冻结的18032 Python功能完整迁到独立Go入口，通过同条件对照后再切换。

**Architecture:** 独立Go会话引擎保留持续监听和逐轮生成连接。状态/身份、音频兼容、上游协议分别实现并差异验证，核心运行不调用Python。按用户约定直接main；采用独立文件域并行，不创建worktree。

**Tech Stack:** Go、gorilla/websocket、libopus、千问原生Realtime、输出Fun-ASR、现有Swift/CoreAudio。

## 1. 冻结与验收

- [x] 核对Python源码3b89df839f3d297fc293f5d68c6b1b225b0cfb82；方案5417671；Python18032 PID874598；18040为候选端口。
- [x] 保存公开基线文件SHA256、有效请求结构、云端依赖版本、配置摘要（绝不保存Key）。
- [x] 记录已知问题：预报误作实况、复合能力误述、缺失历史误解、输出ASR候选误差、非同稿恢复；不以随机正确样本宣称修复。

## 2. 纯逻辑与音频兼容

Files: `internal/realtimestate/{state,history,alignment,identity}.go`及对应测试；`internal/realtimeaudio/`。

- [x] 云端先运行缺实现的测试；覆盖严格receipt类型/整包/单调/封口、35秒ring、16轮历史、完整/候选/unknown、固定时钟完整prompt。
- [x] 用Python公开合成向量与Go结构化结果比较；JSON对象顺序无关，缺省/null/false/0/空数组保持区分。
- [x] AUDIO Opus及strict320/1440解码；有状态24k→16k逐样本对照audioop.ratecv，跨任意包边界不重置；312样本封尾保持。
- [x] 云端 `go test -race ./internal/realtimeaudio ./internal/realtimestate`。

## 3. 原生上游与私有记录

Files: `internal/realtimelive/{config,audit,native,asr}.go`及测试。

- [x] 测试先行：generation请求明确null、listener明确search=false、缺少search确认失败、partial确认继承、tools冲突先于拨号、异常可选来源不终止音频。
- [x] 配置只解析白名单数据，不执行shell；模型/音色/ASR与Python一致。每次付费提交前私有Intent落盘；错误不自动重试。
- [x] 流式每块PCM立即交付；限制60秒/4096字符/8192事件；输出ASR只feed已下发音频，1024队列，失败可结算为候选/unknown。

## 4. 主会话和独立入口

Files: `internal/realtimelive/{session,server}.go`、`cmd/realtime-output-live/main.go`、独立systemd unit。

- [x] 受控传输测试：首PCM后上游挂起仍先发包；封口锁包含在途包；旧音频不越过drain；新轮等待排空，3秒缺确认断开；后续新问题可回复；取消不重复请求。
- [x] HTTP测试：120秒一次票据、Origin、并发单会话、8192消息、错误receipt和关窗清理；实际CoreAudio basis不伪造。
- [x] 保留100/1002/1024队列、7200样本窗口、1.2秒ASR结算、120ms安全余量、输入25秒上限；不改变历史回收或同步日志。
- [x] 云端 `go test -race ./...`、`go vet ./...`、`go build ./...`；原Python49+probe8测试回归。
- [x] 再确认18040空闲，独立部署、不同日志目录；18032与原四服务保持。

## 5. 对照与切换门槛

Files: `tools/realtime-output-live/open.command`、`docs/verification/2026-10-25-realtime-go-migration.md`、`docs/DEVELOPMENT_HANDOFF.md`。

- [x] 启动器增加远端端口选择，初期默认18032；对照与真人确认后已切默认18040，Swift音频/播放器代码不改。
- [x] 同Mac交替对照32普通轮、每边12排空、搜索与重连，记录分组时延；用户后来取消满20分钟等待，长测按实际约18分4秒/32轮记录，不声称20分钟通过。历史/语义续讲体验纳入真人复验。
- [x] 合成CoreAudio与真人反馈分别记录；用户明确分别测试后反馈体验接近，并追加Go复验。普通问答未触发max(100ms,10%)诊断线。
- [x] Go结束→Python新会话配对/播放实际回退演练。
- [x] 对照与真人确认后已切Mac默认入口到Go18040；当前窗口不重启，Python18032继续作回退。
- [ ] 后续观察稳定性、检查消费者后再停用Python；本轮未停旧服务，也未清理Python源码。
- [x] 分阶段提交推送main，报告准确区分已完成与待验收。
