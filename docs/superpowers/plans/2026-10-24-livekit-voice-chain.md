# 服务器 LiveKit 最小语音链路实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline, with TDD and one final independent review. 已获用户明确执行授权，不重复询问执行方式。直接main，不建立分支或worktree。

**Goal:** 在服务器独立测试服务中，实际完成AgentSession接线、ASR→LLM→TTS问答、取消旧输出与后续新回答，并提供可重复的启动/验收入口。

**Architecture:** 独立Python服务使用真实LiveKit Agents、自定义PCM输入/输出；LLM复用官方OpenAI兼容插件，Fun-ASR与Qwen-TTS仅封装百炼实际协议。测试端先在服务器用合成WAV模拟输入/播放，服务绑定loopback；Mac麦克风、Go设备桥、原稿任务接续和手表验收是后续阶段。

**Tech Stack:** 服务器Ubuntu24.04/Python3.12，Agents/官方OpenAI与Silero插件1.8.8、RTC1.1.20，aiohttp；标准库unittest。

**Spec:** `../specs/2026-10-24-server-livekit-design.md`及`../specs/2026-10-24-livekit-task-continuation-design.md`，本轮用户确认范围优先。

## Global Constraints

- 安装、模型调用、运行时测试和服务全部在云服务器；Mac只作代码/Git与后续客户端。凭据只读服务器受保护配置，不写进仓库或聊天。
- 使用独立checkout、venv和空的数据目录；原8000正式服务和手表不切换、不重启。新服务只监听127.0.0.1:18010，不改公网安全组。
- `AgentSession.start(record=False)`；不另存用户正文、录音或trace。合成输入/输出验收文件仅放服务器私有ignored证据目录。
- 所有模型调用显式记录intent、环节、耗时和可用usage；禁用自动重试。异常提交标记unknown，不盲重发。先用短合成输入，不采麦克风。
- 生成完/传输完/模拟终端回报与真实扬声器播放分开标记；无设备字词证据不声称准确接续。

## Review Focus

1. 取消先于首帧、重复取消与旧音频迟到：新输出必须可用，旧代次不得重新开播。
2. 慢消费者、队列满与断线：内存有界，控制停止不等待长音频或模型返回。
3. provider拒绝、超时、坏PCM或错误终态：不得返回成功或自动重复计费请求。
4. 输入结束/取消时ASR final迟到：不得自动给旧输入生成新语音。
5. SDK默认录制、模型错误与日志：明确关闭录制，只输出脱敏类型/计数，不泄露Key或正文。

## 文件职责与接口

- `services/livekit-agent/requirements.txt`：固定直接依赖。
- `family_voice/config.py`：`Settings.from_env(mapping)`，独立三端点、固定模型候选、Key隐藏repr。
- `family_voice/cloud.py`：`Cloud.tts(text)`输出24k PCM异步流；`Cloud.asr(frames)`输出中间/最终识别事件；只做协议、超时、关闭与计数。
- `family_voice/providers.py`：真实SDK STT/TTS薄适配；LLM使用官方插件，所有层max_retry=0。
- `family_voice/audio_io.py`：有界`PCMInput`与`PCMOutput`，输出代次/块ID；flush封口，receipt结算，取消立即退休旧代次；unknown保守解锁SDK，单独保存证据有效性。
- `family_voice/runtime.py`：`VoiceSession.start()/feed()/commit()/interrupt()/close()`，同一AgentSession管理语音；record=False。
- `family_voice/server.py`：loopback HTTP健康与单会话WebSocket；输入16k mono s16、输出24k mono s16；显式开始/结束/取消与播放回报。
- `family_voice/smoke.py`：服务器合成fixture、模型能力与完整链路验收，结果只输出计数/阶段耗时/usage和合成测试结论。
- `tests/test_cloud.py`、`test_audio_io.py`、`test_runtime.py`、`test_server.py`：实际SDK与受控HTTP/WS边界测试，不把假provider当真实模型验收。
- `deploy/family-robot-livekit-dev.service`、服务README、验证报告：启动、停止、SSH转发与后续Mac边界。

### Task 1: 三模型协议与SDK适配

**Consumes:** 现有受保护Key/业务空间、官方Fun-ASR与Qwen-TTS事件、官方兼容LLM插件。
**Produces:** `Settings`、`Cloud`、实际SDK `DashScopeSTT`/`DashScopeTTS`与无自动重试的LLM配置。

- [x] 写失败测试：配置不泄露Key、三个端点独立；TTS等待session.updated再提交、合法PCM与response.done才成功；ASR等待task-started、忽略heartbeat、区分interim/final；错误与取消关闭连接、不重试。
- [x] 在服务器运行测试并记录预期红灯，创建最小实现后重跑同一测试。
- [x] 用现有账号实际验证短TTS、其合成输入的ASR、短文本LLM；保存各环节usage、延迟和脱敏错误，按真实结果调整协议，不能凭握手宣布通过。
- [x] 提交此自洽功能与测试。

### Task 2: AgentSession与播放生命周期

**Consumes:** Task1的真实SDK provider，24k输出PCM。
**Produces:** `VoiceSession`、有界输入/输出与带代次的控制回报。

- [x] 写失败测试：指定PCM播报不自动写入历史；首帧前/播放中/重复取消后可新播报；旧回报与迟到音频不影响新输出；缺回报有限等待后unknown解锁；错误格式/满队列/关闭有界。
- [x] 在服务器真实SDK环境运行红灯，最小实现后重跑整组。
- [x] `record=False`、不修改SDK私有字段；报告发行包取消差异是否实际触发。
- [x] 提交此自洽功能与测试。

### Task 3: 服务器测试入口与真实整链路

**Consumes:** Task2的VoiceSession；Task1的已验证端点。
**Produces:** 单会话loopback服务、CLI smoke、结构化验收结果。

- [x] 写失败测试：未授权/第二会话拒绝、音频边界/限额、取消与断线不遗留生成；随后实现HTTP/WS与CLI。
- [x] 服务器运行全部新增测试、原SDK探针和依赖检查。
- [x] 真实模型链路至少两轮：合成问题→ASR final→LLM正文→TTS PCM→模拟终端回报；取消一段长回复并发新问题，核对旧代次未污染新回答。保留问题是否答对、阶段时间、usage与失败边界。
- [x] 配置非root独立测试服务；仅loopback，健康检查、启动/停止和重启不影响旧网关。独立复核后修正重要问题并再次测试。
- [x] README/交接/验证记录同步实际结果，提交推送main并让服务器采用同一源码；不声称Mac麦克风、手表或自动接续已验收。

## 实施结果

三项任务已实施，新增服务测试21/21与原SDK探针3/3在服务器通过，依赖检查无冲突。独立复核确认取消输入身份缺陷并修复；旧ASR阻塞关注未复现，保留增强回归而不重写provider。真实两轮问答、长回复取消及后续新问答已通过，详见[验证报告](../../verification/2026-10-24-livekit-voice-chain.md)。下一阶段为Mac实时收音/播放客户端；工具/记录/任务接续及手表验收另行推进。
