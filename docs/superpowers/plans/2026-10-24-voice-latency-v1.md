# Voice latency v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline. User-approved main-only development and cloud-only runtime checks apply.

**Goal:** 修复历史查询循环，支持模型自然查询提示，减少换话题的模型往返并降低首句等待。

**Architecture:** 每轮只读工具预算和状态去重独立成dialogue_turn模块；content_runtime负责模型工具筛选、原子正文替换和独立progress播放。现有排空/播放回报协议保持。

**Tech Stack:** Python、LiveKit1.8.8、既有云端模型及原生CoreAudio终端。

---

### Task 1: 查询终止与一次正文交付

Files: `family_voice/dialogue_turn.py`, `family_voice/content_runtime.py`, `tests/test_dialogue_turn.py`, `tests/test_content_runtime.py`.

- [x] 添加失败回归：query仍可见时受控模型一直查询，隐藏后respond；最终无turn.error且只查一次。轮内只读额度用完后工具集合只能发声。播放状态改变后允许重新查询。
- [x] 云端RED：`PYTHONPATH=services/livekit-agent:services/livekit-agent/tests .superpowers/livekit-chain/venv/bin/python -m unittest test_dialogue_turn -v`。
- [x] 实现`DialogueTurn`的allowed/query/history去重和读额度；输入完成时重置。模型调用始终预留最终回答，历史查询语义在prompt/tool描述中明确。
- [x] respond(text, action='replace')验证正文后调用新原稿接纳，清理旧任务；默认reply保留原故事。
- [x] 云端定向GREEN，检查普通数学问答、两份原稿和已完成状态。

### Task 2: 模型查询提示并行

Files: `content_runtime.py`, `tests/test_content_runtime.py`.

- [x] 添加失败回归：受控read被Event阻塞时，模型给定ack已经开始输出；两份原稿不变。
- [x] progress独立槽；`play_content_unit`进度提示绕过等待整个工具轮次结束的屏障，沿用SDK语音调度和play_lock。
- [x] ack只播一次，过期/未开始提示取消；6秒/3秒准备额度和失败隔离；所有实际完整播完的提示照常入档。
- [x] 测试新语音取消、旧读结果退休、最终答案不被迟到提示抢播及原有播放结算。补充恢复/重播入口、PCM屏障与handoff期间的撤销竞态。

### Task 3: 实测与发布

Files: 服务README、交接、`docs/verification/2026-10-24-voice-latency-v1.md`。

- [x] 云端全Python回归及pip check；独立审查并处理P1/P2。
- [x] 同一合成语境的基线/候选真实云端对照；保存元数据，不写用户正文/音频到Git。提示与实质答案分别计时。换题3组中位减少21%，历史查档样本未提速，均如实记录。
- [x] 部署独立服务，原生真实播放/插话/同稿恢复，合成档案隔离。
- [x] 按main提交推送并同步云端同一版本；原生窗口可供用户下一轮测试。
