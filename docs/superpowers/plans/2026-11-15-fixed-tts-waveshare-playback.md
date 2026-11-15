# 固定 TTS 微雪播放验证实施计划

> **For agentic workers:** 使用 superpowers:subagent-driven-development 执行并分别复核规格和质量。用户已明确继续测试、在现场可配合；直接 main，不创建分支或 worktree。运行仅云端，现场分流由主代理操作，发声前告知用户。

**Goal:** 用已验证的固定 TTS 音频核对微雪完整播放和真人中断的 I2S 已播边界，为后续实时 A/B 接入提供证据。

**Architecture:** 新增一个默认不运行的测试源资产，显式复制进独立云端源码后编译。复用 `Server.runDevice` / `Session.generate` 注入固定 PCM，完整保留公共 Session 鉴权、Opus、600ms 流控、中断和回执。只读分析 TTS 已播前缀，不改变产品历史或部署正式后端。

**Tech Stack:** 现有 Go、标准库、realtimelive、realtimeaudio、voicecompare；已有 Caddy 原子分流操作器。

## 1. 测试资产

Create `tools/tts-playback-pilot/fixed_tts_test.go.txt`，package realtimelive。

- [x] 先写无真实网络的保护检查，云端运行 RED，再实现 GREEN：默认 Serve skip；只接指定 MAC/owner；最多两次固定音频生成，持久 intent 先于 emit，未知不重播；取消不发音频；输入文件/结果不完整或摘要不匹配则拒绝；保守样本映射边界。
- [x] `TestFixedTTSPlaybackServe` 仅显式环境值启用，监听 `127.0.0.1:18818`；加载服务器原有受保护模型配置及 admission，用现有管理端 factory authority。指定设备 `44:bd:8d:79:1f:e8` / `dev_ce26243f0d2f0b8019bfe45796307b48`，复核现有绑定，不接其他设备。关闭记忆、音乐、工具和表情，max_sessions=1；保留输入监听与 OutputASR 作独立检查，不声称零辅助费用。
- [x] 固定源为云端 `final-ab-20261115/native-v1/result.json` / `output.pcm`，必须 completed / 24k mono PCM16 / 样本数及已冻结SHA匹配。两次播放共用该源，无新主回答/TTS调用。emit 仅 `text` 和 `audio`，复用原 `reply` 编码/尾补零，不能另写设备读循环。
- [x] 每次在私有JSONL先记录固定播放 intent，跨连接和进程保留两次上限。每个会话最多120秒，服务最多6分钟；超限只结束会话，不再生成第三条声音。SIGTERM/超时调用 `CloseActive` 后退出。
- [x] 记录每个实际播放 turn 的源标识、源样本数、encoder lookahead。结束时从公共 turn 中冻结 completed/sent/cut/ended/final/basis，输出只读 `tts_playback_alignment` 事件。仅 i2s_dma 有硬件证据；`sourcePlayed=clamp(completed-lookahead,0,sourceSamples)`，再用120ms余量确认字词；完整 ended、非cut、final、completed==sent 且映射覆盖源才确认全文。无 final 则保留 unknown 标记；不将结果写回产品记忆。

关键映射使用已有 API：

```go
codec, err := realtimeaudio.New(24000, 1440)
lookahead, err := codec.LookaheadSamples()
sourcePlayed := max(int64(0), min(sourceSamples, completed-int64(lookahead)))
prefix := voicecompare.ConfirmedText(words, sourcePlayed, 120)
```

## 2. 云端验证与现场入口

- [x] 仅复制明确源码文件到独立云端目录，保留原私有重放和模型文件在服务器；执行保护测试、公共 Session/codec 相关测试、race、vet、编译。任何 Serve 测试均默认 skip，无设备/提供方调用。规格复核通过后做质量复核。
- [x] 主代理复用已验证 `native_route_guard_v4.py` 的 Operator，使用新准备目录/基线/health常量及新临时 unit；其 exact-route proof、保护配置摘要、不可重试 intent、apply/restore并发保护不变。先 fresh prepare/validate，确认原正式语音732503、管理635398及配置无变化。不能复用以前已消费的意图。
- [x] 启动前冻结测试资产、二进制、源PCM/元数据SHA和测试结果；仅 loopback 服务，无自动开麦。用 guarded apply 分流一个目标设备；独立超时清理在 finally 中先关闭临时实例再 guarded restore，若外部配置变化则拒绝覆盖并保留证据。
- [ ] 提示用户唤醒说“开始”，第一次听完；再说“再来一次”，第二次听到中段时说“停一下”。记录用户实际听感和停声位置；不把回执独自当声学逐字精度证明。
- [x] 收尾核对入口/file/admin恢复、临时实例停止、正式服务身份/config保持，保存私有记录及公开脱敏报告。此阶段不实现连续A/B对话、续讲上下文或克隆，也不宣布这些能力完成。

## 3. 费用和后续

固定PCM重放不重复收费，但输入端监听和OutputASR继续产生实际API费用并记录。成品比较按每天30分钟统计全部调用；专用TTS是否能替代常规输出ASR，必须在对齐效果满足功能后再决定。测试实施与产品最终选型分开记录。


当前云端检查/构建/规格与质量复核通过，18项路由保护通过；首次现场窗口已告知并开放，到时自动关闭并guarded restore；本轮0个有效会话/0次播放intent，正式服务/配置保持，实机结果仍待用户操作。详见[本轮记录](../../verification/2026-11-15-fixed-tts-waveshare-playback.md)。
