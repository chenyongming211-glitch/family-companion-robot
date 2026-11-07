# 微雪真实音频采集实施计划

> **For agentic workers:** 使用superpowers:subagent-driven-development实施并做规格、质量独立复核。用户已要求执行并选择微雪且现场有人，无需重复请求执行许可。直接main，无分支或worktree。

**Goal:** 从微雪正常业务上行取得一条来源可核查的真实音频，先验证采集完整性，再推进既定两条件评测。

**Architecture:** 独立云端被动tcpdump管道→有界TCP/WebSocket解析→原始Opus及16kHz WAV。正式服务、固件和音频策略保持；所有录音、凭据和运行证据仅云端私有存放。

**Tech Stack:** 云端Python标准库、tcpdump、既有libopus；不新增产品运行依赖。

## Task 1: 被动采集工具

**Files:** 新增tools/robot-audio-capture/capture.py、test_capture.py、README.md。接口及限制遵照[设计](../specs/2026-11-07-robot-audio-capture-design.md)。

- [ ] 云端先运行协议测试取得缺少功能的RED；实现有界pcap/TCP/WebSocket解析、鉴权及session隔离、Opus原件／WAV排他私有输出。
- [ ] 云端执行`python3 -m unittest discover -s tools/robot-audio-capture -p 'test_*.py' -v`取得GREEN。测试必须证明TCP分段／重复／缺口、非目标鉴权、WebSocket掩码／格式、唤醒排除、首回答边界、原生Opus960样本、抓包丢弃失效和文件权限。
- [ ] 独立规格复核，修复后再做独立质量复核；不得在复核前抓真人音频。

## Task 2: 真人链路验证

- [ ] 保存正式两服务字段／五保护摘要和云端代码摘要，核对设备来源；启动180秒有界被动监听，未见ready不能让用户开始。
- [ ] 通知用户手动唤醒微雪、说一句3～5秒内容、等待回答并结束。仅取得本轮目标完整流才导出原件与WAV；核对原因、样本数、摘要和质量。
- [ ] 保留真实成功／失败及边界，关闭采集及其子进程；复查正式保护状态。没有采到不能写成采集器已实机通过。

## Task 3: 收尾及两条件验收

- [ ] 更新验证报告和交接；代码与报告提交推送main，核对远端和工作区。
- [ ] 按既定协议组织五人两条件登记和新机器人查询，以及20陌生人×10句。参与者或新录音不足时明确逐项待采，不缩小标准，不用旧公开素材替代。
