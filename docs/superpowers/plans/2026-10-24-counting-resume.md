# 计数任务按完整播放检查点继续

> 用户现场确认连续对话/自动打断/旁支问答正常，但“继续数”从51开始。本计划直接修正该反例，继续main、服务器测试/模型调用；不再请求执行批准。TDD，独立任务实现计数核心，主代理接SDK/播放回报。

**Goal:** 原请求1到50，播放中被新问题打断后保留原任务；“继续数”从首个未确认完整播放的数字恢复，终点仍为50。已经完成时明确结束，不生成51。允许保守重读被打断的当前数字。

**Architecture:** 会话内独立CountPlan保留start/end/next/state，不从LLM生成稿或ChatContext推断已播位置。每个数字为一个固定文本/PCM播放单位，`AgentSession.say(text,audio=...,add_to_chat_ctx=False)`，完整有效playback回报才推进；部分/unknown/过期回报不能推进。最多预取3个数字音频、每个最多5秒，仅在内存保留，暂停时不增加预取，不重发结果未知的合成请求；恢复复用已准备音频。旧播放任务有代次保护。

**Scope ruling:** 当前独立Python语音开发服务先验证会话内计数检查点，核心与SDK通过prepare/play回调隔离，后续Go任务模块可替换。此次不声称Go持久化、跨唤醒/进程恢复或故事字词对齐已实现。不改正式Go8000服务。

## 接口与职责

- `family_voice/counting.py`：`parse_count_request(text)->(start,end)|None`、`is_count_resume(text)`；`CountController(prepare,play,emit,prefetch=3)`，`start(start,end)`、`pause()`、`resume()`、`async close()`，只维护确定性任务/检查点及有界预取。
- CountPlan snapshot至少含`start,end,next,state`；state=playing/paused/completed/failed。范围0..1000，最多100个数字。`next`只在play返回True且同一播放代次时加一。到end后state completed，继续不再调用prepare/play。
- prepare(number)异步返回完整mono24k s16 PCM；play(number,pcm)异步返回是否已获同块完整有效的播放证据。Controller不碰SDK、云API或浏览器。
- `runtime.py`：自动模式在on_user_turn_completed拦截计数/继续请求并抑制LLM；新语音开始暂停计数。已完成的继续请求用固定提示。普通旁支问题走既有ASR→LLM→TTS；计数固定全文不入LLM历史。
- `audio_io.py`：last_observation补充sent_samples；播放单位须完整、valid、未interrupted、块ID新于本单位开始前的序列且played==sent==PCM样本数，才返回True。
- `web/client.mjs/index.html`：只读展示`count.state`与暂停/继续检查点，仍只有一个唤醒/结束按钮。

## 测试与实施

- [x] 核心测试先红后绿：中文/数字计数请求与继续识别，部分播放不推进，暂停/恢复复用准备结果，旧播放代次不推进新任务，未知/合成失败不自动重试，终点不越界，关闭释放有界资源。
- [x] SDK回归：指定数字PCM不自动写入历史，完整回报推进、部分/unknown不推进；真实语音插话暂停，回答二加三后“继续数”从检查点恢复，不依赖LLM猜位置。额外修正取消后等待SDK块结算的竞态。
- [x] 服务器60项Python、24项Node、3项SDK回归通过，规格与质量静态复核通过。
- [x] 真实云端合成数字预取与自动语音链路：暂停4→回答5→恢复4至原终点50→再次继续说明已完成。
- [x] 文档记录真实失败与证据边界，保留会话内/内存/浏览器时钟估算的限制；交付继续直接main并核对服务器受测源码。
- [ ] Mac用户现场再试数到中途→插话问2+3→继续；不预设具体物理断点通过，等待反馈。
