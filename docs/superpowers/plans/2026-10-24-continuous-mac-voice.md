# 一次唤醒、连续说话的Mac测试终端

> 用户最新明确要求：只保留一个“开始说话”按钮模拟唤醒，随后自由说话、说完自动回复、开口自动打断，后续问题继续回复。该要求覆盖此前点击分轮方案。采用writing-plans、TDD；页面任务用subagent-driven-development，服务器接线由主代理实施；不再请求执行批准。直接main，运行时依赖与测试在服务器，Mac只作浏览器终端。

**Goal:** 点击一次即保持全双工收音，不需结束发送/停播按钮。自动端点、播放中声学插话、随后新问题，使用同一个AgentSession；页面只有一个可操作按钮，活动时同一按钮可结束对话。

**Architecture:** 现有loopback服务/SSH配对保留。新增`?mode=continuous`的浏览器WS模式。AgentSession采用`turn_detection='vad'`、Silero，`TurnSTT`薄适配器将VAD语段交给已有有限音频识别路径；静音不调用云ASR，VAD形成短语段后才识别。SDK独立VAD即时触发打断，不等待整句ASR。禁止把30秒有限云ASR请求直接改成无限长连接，也不在浏览器另建轮次编排。

**SDK evidence:** 服务器Agents1.8.8的StreamAdapter通过VAD END_OF_SPEECH收集frames并调用`STT.recognize`，SDK本身另有VAD打断路径；min_duration可配置，StopResponse可在on_user_turn_completed阻止超额生成。继续使用受测公开API，不改SDK私有字段。

**Implementation ruling:** 初版官方StreamAdapter正常路径通过，但慢ASR两段/三段回归复现旧识别额外触发LLM、下一段被旧终态阻塞。最终改用相同STT/SpeechStream/VAD公开扩展接口的TurnSTT：VAD消费不等待云识别，每段在检测时绑定代次，新开口取消旧任务，最多4个在途/收尾任务，终态再次核对身份，ASR关闭最多等待0.1秒。该变更来自实测反例，保留初版红灯和最终绿灯；未修改SDK核心。

**Boundaries:** 浏览器AEC/降噪用于Mac扬声器，物理误触发和停声须用户现场确认。自动回复新问题不是故事原稿精确续播；任务接续另行实现。保留15分钟/8次回答上限，空闲60秒回待机，持续单次讲话超过25秒结束该测试会话以保护输入边界；无自动计费重试/重发，不保存用户正文或原始录音。

## 固定协议

- 浏览器连接`/v1/browser/audio?mode=continuous`，cookie/Origin/单会话租约与既有接口相同。
- `ready`增加`mode:'continuous'`，包含input_rate16000/output_rate24000/session_seconds900/max_turns8。
- 客户端仅连续发送`{type:'listen.audio',data:<base64 PCM>}`，每帧最多640字节。断开/同一主按钮结束时关闭WS；不发input.start/input.end/cancel，不因新用户语音关闭收音。
- 既有`audio/audio.end/audio.cancel`和带代次playback回报保持。收到audio.cancel立即丢弃旧epoch缓冲；收音继续，新epoch自动可播放。
- `user.state`、`agent.state`显示speaking/listening/thinking等；`turn`携带count/max_turns，更新预算。
- `sleep`携带`code:'idle'|'turn_limit'|'utterance_limit'`，客户端释放mic、结束WS、主按钮回“开始说话”。error仍显示明确原因；不自动重新唤醒。
- 服务器端在LLM生成前检查8轮上限，不能等第9轮模型调用后才关连接。25秒讲话计时和空闲计时均由服务器真实状态管理。
- 旧Bearer `/v1/audio`手动接口仅保留为历史探针兼容，不作为用户页面默认入口。

## Task 1：服务器自动轮次（主代理）

Files: `family_voice/runtime.py`、`providers.py`、`server.py`；`tests/test_continuous.py`、服务协议测试。

- [x] 写失败测试：连续PCM/静音自动成轮、不调用手动commit；真实Silero识别已保存的合成fixture，受控ASR/LLM/TTS协议验证两轮回复。
- [x] 自动模式在VoiceSession中选择官方StreamAdapter和VAD模式，禁用预生成重试，min_speech约0.12s、打断min_duration约0.2s、end silence约0.65s；manual模式保持兼容。
- [x] 挂接公开user/agent状态和on_user_turn_completed；生成前检查8轮，空闲60秒和连续讲话25秒发sleep，关闭有界；有限识别buffer严格限制30秒。
- [x] 协议测试：单次唤醒后listen.audio多轮、非法/超额帧拒绝、mode不允许混入手动提交、断线释放，睡眠/轮数/超时原因明确。
- [x] 真实SDK测试：长输出仍在播放时，输入新的真实语音fixture，触发audio.cancel，第二轮答案/音频自动到达；静音/轻噪声不重复触发模型。

## Task 2：单按钮页面（独立实现任务）

Files: `web/client.mjs`、`index.html`、`client.css`；新增Node客户端协议测试（只在服务器运行）。不修改音频核心、鉴权、后端或Git索引。

- [x] 测试先验证自动ready启用持续收音；PCM只发listen.audio；audio.cancel清旧播放而不停止麦克风；sleep/断线/后台释放资源并回待机；连接防重入和晚到权限防护保留。
- [x] 页面恰好一个button。未唤醒显示“开始说话”，活动时同一按钮“结束对话”；去掉检查麦克风/试听/手动结束发送/立即停播等操作。保留电平、状态、转写、回答与预算，首次点击一次完成配对后的权限/连接/收音。
- [x] user.state/agent.state/turn/sleep更新页面，收音贯穿回复与打断；新epoch正常播放。旧错误仍明确展示，无自动重试/偷发模型请求。
- [x] 自审后交主代理做规格复核，再对整个改动做一次代码质量复核。

## Task 3：联调与发布

- [x] 服务器Python/Node/原SDK探针回归与依赖检查；只运行相关测试。
- [x] 使用既有合成WAV连续实时发送，包括两轮问答、长回复中插入第二问题、再提第三问题；全程不发input.start/end/cancel。真正云接口结果与模拟播放器边界分开记录。
- [x] 部署后在Mac当前内嵌浏览器只点击一次，用户连续问答/自由插话。观察真实AEC自回声、VAD误打断和话尾截断；不能凭按钮/合成数据宣称物理通过。
- [x] 规格/代码复核、文档和验证报告，提交推送main、服务器同源码；旧8000不切换。保留本轮失败与修正证据，不反复要求用户操作多个按钮。

服务器测试命令：`PYTHONPATH=services/livekit-agent .superpowers/livekit-chain/venv/bin/python -m unittest discover -s services/livekit-agent/tests -v`，`node --test services/livekit-agent/tests/*.test.mjs`。真实语音fixture只读服务器私有`/var/lib/family-robot-livekit-dev/smoke-20261024-1`；不进入Git。
