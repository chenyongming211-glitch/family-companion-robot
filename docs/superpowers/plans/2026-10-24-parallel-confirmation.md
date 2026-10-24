# Parallel Confirmation Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline. Main-only Git and cloud-only runtime execution are explicitly authorized; the user already approved the design.

**Goal:** 模型自然生成快速准备语，与正式回答并行，并保持取消、排空、档案和原稿接续。

**Architecture:** 新confirmation模块持有短模型请求、结构化校验及有界取消任务；content_runtime启动/终止当前代并复用progress槽。主Agent工具流程保持。

**Tech Stack:** Python3.12、LiveKit Agents1.8.8、现有OpenAI兼容模型/TTS、原生Opus/CoreAudio。

### Task 1: 快速生成与取消

- [ ] 添加test_confirmation：完整stop+JSON短句可接纳；空text跳过；截断/并行工具/超长/多句拒绝；超时和取消不交付。快速模型固定48-token、无工具、2秒截止。
- [ ] 云端RED：`PYTHONPATH=services/livekit-agent:services/livekit-agent/tests .superpowers/livekit-chain/venv/bin/python -m unittest test_confirmation -v`。
- [ ] 实现QuickConfirmation.start/cancel/aclose：启动不await，token为(epoch,turn_id)，最多4个含清理任务，审计stage=llm_confirmation，完整关闭流和自有模型client，无固定替代语。
- [ ] 同命令GREEN，验证截止后仍未下发的progress被退休。

### Task 2: 正式回答并行与播放归属

- [ ] 受控HTTP边界将快速JSON请求与正式tool请求分别计数，默认快速返回空text，保持原199项回归断言的含义。
- [ ] 新SDK回归：memory_context被Event阻塞时，快速确认已输出，主/旁快照保持；答案先到撤销慢确认；新语音退休旧结果；快速失败正文仍完成；续讲不晚插确认；查档与快速最多一个提示。
- [ ] RED后在on_user_turn_completed启动快速请求；accept_draft/speak_dialogue/内容控制/生成错误结束当前代准备；新语音、sleep、close取消并回收。
- [ ] progress增加purpose/deadline：准备语3秒PCM、2秒接纳；使用已有输出账本和allow_start门控。确认完整实播入档，未知不作已听事实。
- [ ] 定向GREEN，原稿/取消/unknown回归和独立审查；处理P1/P2后跑全量及pip check。

### Task 3: 真实验证与发布

- [ ] 云端真实LLM/TTS故事、计数、插问和数学：确认与正文分别计时，检查简短自然文本、实质正文正确、快答案不晚插提示。合成档案与真人档案隔离。
- [ ] 更新README、交接与verification报告；公布额外模型请求和两项延迟，不宣称物理时延或普遍正文提速。
- [ ] 空闲时重启独立服务，原生合成故事插话和同稿恢复；窗口保留、收音由用户开启。
- [ ] main提交推送，核对GitHub和云端同一提交；排除其他工作留下的未提交资料。
