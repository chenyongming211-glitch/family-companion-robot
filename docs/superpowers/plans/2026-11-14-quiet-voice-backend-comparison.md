# 静默双链路验证：最小实现计划

> **For agentic workers:** 使用 superpowers:subagent-driven-development 实施；直接 main，不创建分支或 worktree。用户已授权按修订方案开发，但正在休息，禁止任何设备连接、播放、开麦、路由分流和正式服务变更。所有运行测试、依赖和真实提供方调用仅在云端。

**Goal:** 提供不带播放或服务入口的云端命令，验证 TTS 文字时间戳，并使用同一 TTS 静默比较原生文字回答与 ASR＋文字 LLM。

**Architecture:** 新增小型 Go 包及单个批处理命令；复用 gorilla/WebSocket、标准库和服务器现有受保护配置，不新增依赖、框架、服务、前端、固件或通用插件层。只处理显式提供的云端输入文件，结果保存私有目录，不连接设备。既有主服务不变。

**Tech Stack:** 现有 Go、gorilla/WebSocket、HTTP JSON、PCM16、标准库测试。

## 1. 固定范围与契约

- `internal/voicecompare/`：有限输入的提供方客户端、TTS 时间戳和保守已播映射。公开契约：`Client{Key, RealtimeURL, InferenceURL, TextURL, TTSModel, Voice string; BeforeCall func(stage string, reserveYuan float64) error; AfterCall func(stage string, usage map[string]any, err error) error}`；`Run(ctx, backend, instructions string, pcm []byte, text string) (Result,error)`，backend 仅 `tts/native/cascade`；`Result{Text string; PCM []byte; Words []Word; Calls []Call}`，`Word{Text string; BeginMS,EndMS int64}`、`Call{Stage string; Usage map[string]any}`；`ConfirmedText(words []Word, playedSamples int64, marginMS int64) string`。
- `cmd/voice-backend-pilot/`：参数、私有目录、费用预留账本及只写文件的输出。`--allow-provider-calls` 默认 false；`--backend` 有限枚举；`--input` 为最大10秒云端16k/mono/PCM16文件，`--text` 仅合成公开短句；`--out` 必须是新目录。命令没有监听、麦克风、播放或设备 URL 参数。
- 原生主回答只用文字输出，主回答≤120个汉字；三段式限定 Fun-ASR＋Qwen Plus 非思考，同一TTS，回复输入和指令限长。每次命令仅一条输入，无自动重试；正常 TTS 最多512个计费字符/30秒输出；取消或不完整时间戳不升级为可信已播。
- TTS 时间戳核对真实 task/句子身份、PCM元数据顺序、时长范围和有序字词；禁止简单把每句局部时间零点当全局。优先每个短句独立任务，以明确PCM偏移累计，避免复杂流式调度。
- 输出 PCM 文件只用于云端分析，不播放。原音、正文、凭据、二进制仅云端0700目录/0600文件，不入Git或普通控制台日志。

## 2. 测试与实施顺序

- [x] 先新增核心保护测试，同步云端、运行目标测试观察缺少实现的RED：正常音频/时间戳、错误task、乱序/越界/缺字词、句子时间偏移、取消、上限和callback拒绝后零调用。
- [x] 最小实现TTS和已播映射；保护测试GREEN、race通过，先规格审查再质量审查。时间戳末端未知保持未知，毫秒字段不承诺物理精度。
- [x] 添加两条文字生成入口及单条ASR；HTTP/WS合成提供方验证原生仅文字、三段式同TTS、usage、取消、失败不重试及不完整上下文不伪称成功。
- [x] 命令保护测试先RED再实现：默认零网络、非法参数/输入拒绝、新私有目录、每个API持久intent和预留、未知不退回预算、结果无播放路径。云端普通/race/vet/build及两阶段审查。
- [x] 真实调用前刷新账户地域/模型/音色和账本；若无可用且支持时间戳的CosyVoice3.5音色，不创建克隆、不借用不支持的系统音色。可仅用明确支持时间戳的CosyVoice3 Flash系统音色作协议验证，A/B保持同一TTS，按其1元/万计费字符单列价格，不冒充3.5成绩。
- [x] 静默真实验证总预留上限1元，最多2条合成短句TTS；通过后最多A/B各1条合成输入回答，任一未知/门控失败即停。真实调用逐项先记intent，无合格非真人输入则跳过主回答，不拿旧真人录音充数。此阶段不是16答实机初筛。
- [x] 保留既有48主回答/8探针账本；最新记录5/26须在云端重读，先占用本轮协议/主要回答名额，不能另起账本绕过总量。费用账本逐API预留，调用数和字数/音频/超时限额共同约束；usage未知按预留保留，不能以预留当实际账单。
- [x] 更新验证、设计与交接，明确静默验证、实机待验及实际费用/未知边界，main提交推送。休息期间不进入微雪初筛，不更改正式Voice、管理、Caddy或固件。

## 3. 后续人工实机阶段（本次不执行）

在用户方便时，先固定音频验证同一微雪播放/回执；再A/B各两段、每段4答，A→B→B→A，共16答初筛。只判定具体实现的适用性，不将更换LLM的结果归因于单一ASR差异。费用上限届时按真实静默usage核定；可信已播覆盖率、错误已播、未知、重读/漏讲、旧话题回流和受扰网络样本均报告。克隆及长聊费用校准在入选路线后开展。

## 执行结果

2026-11-14已完成本阶段实施、云端60项普通/race检查、vet/build、规格/质量复核及一次真实TTS探针。真实探针句子元数据校验失败、usage未知，已触发停止规则；第2探针和A/B真实回答均跳过，未连接设备或更改正式服务。上述勾选表示步骤已执行并遵守门控，不代表真实TTS或产品对齐通过。保留0.02元预留，实际费用未知；全局账本6/8协议、26/48主回答。详细结果见[验证记录](../../verification/2026-11-14-quiet-voice-backend-comparison.md)。
