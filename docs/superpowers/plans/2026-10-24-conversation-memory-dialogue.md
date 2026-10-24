# Conversation memory and model clarification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Follow the user-approved main-only and cloud-runtime workflow. No additional agents are required.

**Goal:** 让模型读取真实通话文字并自行理解、回答和反问，保持程序管理播放断点。

**Architecture:** 独立conversation_memory模块安全投影档案并提供有界上下文和回查；content工具返回事实，临时dialogue控制器承载模型问题。原有播放身份、排空和主旁稿保持权威。

**Tech Stack:** Python标准库、既有LiveKit 1.8.8与云端受控ASR/LLM/TTS测试。

---

### Task 1: 档案记忆与回查

Files: create `services/livekit-agent/family_voice/conversation_memory.py` and `tests/test_conversation_memory.py`.

- [x] 添加失败测试：
  ```python
  self.assertNotIn('未播秘密', json.dumps(memory.context(current_id, '之前聊了什么'), ensure_ascii=False))
  self.assertIn('早期已播事实', json.dumps(memory.recall('read', current_id, after=0), ensure_ascii=False))
  ```
- [x] 云端运行 `PYTHONPATH=services/livekit-agent .superpowers/livekit-chain/venv/bin/python -m unittest discover -s services/livekit-agent/tests -p test_conversation_memory.py -v` 确认缺失功能失败。
- [x] 实现安全有界扫描、同单位unknown→confirmed投影、近期正文/提取式摘要、历史列表与分页/search；响应含来源、范围、截断和下一页。
- [x] 原命令通过，覆盖损坏文件、路径、大小/输出额度及未播分离。

### Task 2: 模型理解和自然反问

Files: modify `family_voice/content_runtime.py`, `tests/test_content_runtime.py`.

- [x] 受控LLM按调用序列输出工具与自然文本；添加失败断言：
  ```python
  self.assertEqual(result['status'], 'needs_clarification')
  self.assertEqual(self.tts_texts, [])
  self.assertEqual([voice.main.snapshot, voice.side.snapshot], before)
  ```
- [x] 云端定向测试失败后，移除direct_action拦截；恢复成功StopResponse，其他状态以工具结果交回模型。
- [x] 接 `conversation_history` 与 `ask_clarification`；独立dialogue控制器保存实播问题并保留两份原稿；输入代次约束覆盖异步回读和工具。
- [x] 每轮flush后在线程中读取有界记忆；任务快照保留权威，历史作为只读资料。
- [x] 更新原有直达恢复测试为模型工具恢复，保留同稿、句级前缀、迟到回报等原断言。

### Task 3: 集成验证与文档

Files: service README, handoff and `docs/verification/2026-10-24-conversation-memory-dialogue.md`.

- [x] 云端全Python检查、依赖检查通过；有失败时只修本次相关回归。
- [x] 独立合成存档验证真实模型读取旧内容、跨会话问答与自然澄清；正常插话→旁答→同稿续讲。按测试层级记录实际证据与未验收边界。
- [x] 更新README与交接中的“存档不接入模型”和旧32项限制描述，准确说明提取式摘要和原文回查。

### Task 4: 发布

- [x] 自审diff和规格覆盖；本地main提交推送，云端同步相同版本。
- [x] 仅重启独立18010/18020服务；检查health、text_recording及8000服务未受影响。
- [x] 保留原生客户端窗口；下一次连接使用新逻辑，不擅自开始用户已停止的收音。
