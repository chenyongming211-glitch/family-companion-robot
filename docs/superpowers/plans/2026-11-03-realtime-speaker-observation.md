# Realtime 匿名说话人观测试点实施计划

> **For agentic workers:** 按已获用户批准的设计执行，使用 executing-plans、test-driven-development；独立文件域可按 dispatching-parallel-agents 并行，集成后按 requesting-code-review 独立审查。直接 main，不建分支/worktree。所有 Go/ONNX 运行时测试在云端，Mac只编辑/Git及原生客户端构建和现场测试。

**Goal:** 用户正常与小满语音交流时，云端旁路分析同一条上行音频，Mac显示该发言的匿名访客候选；无需每句手动采样。

**Architecture:** 保持现有 Realtime/VAD/播放；话尾提交有源采样坐标的完整片段，旁路有界队列调用独立 speaker-lab session。新增仅回环的试点命令，固定关闭设备记忆、文字/意图正文落盘和音频诊断。原产品18040/18041及固件不切换。候选只用于观测，不授予人物权限或向模型注入未经分段确认的身份。

**Tech Stack:** Go、已有Linux sherpa-onnx/ERes2Net、私有子进程管道、AppKit/原生AEC/Opus客户端。

## 已确认设计的本轮落实

用户2026-11-02确认识别触发规则；随后明确选择“正常聊天时同步识别”，本计划执行该授权，不重复要求审批。首轮为隔离、受控轮流发言的实验，延续P0，不提前做P1登记。连续流换人/重叠检测仍未具备，不能把VAD当单人证明；显示“候选/不确定”，不进行身份敏感操作。首轮暂不向模型注入候选身份或据此改变回复归属，先验证正常交流时旁路标签及故障不影响语音。

- 有效片段必须具备提供方起止时间、成功上行采样覆盖和完整PCM；坐标不可靠时不做人物推断。
- 每段1.5～8秒，长段不截取一部分后冒充完整身份；不合格显示不确定，原回答仍继续。
- 每会话最多1运行＋1排队，提交不等待推理；会话5分钟/32片段边界由helper限制，达到后识别停止、正常聊天继续，重新开始会话才重新编号。
- 临时特征、音频和文字上下文只在内存，元数据允许保存设备/会话ID及关闭原因，不保存正文、向量或声纹库。既有文字记录和记忆不读取、不回写。
- 试点仅监听服务器127.0.0.1:18052，通过现有SSH授权转发到Mac；不得开放公网或重启现有产品服务。

## Task 1：lab增加Realtime轮流输入模式

**Files:** `tools/speaker-lab/session.go`、`session_test.go`、`main.go`、README。

- [x] 先写云端RED：显式`--realtime-test`仍要求人工单人片段声明；scope为`realtime-test`，允许真实坐标间隙，拒绝重叠/倒序/跨session；完整8秒可接，超过8秒拒绝。原manual-test保持连续坐标/4秒限制和原证据类型。
- [x] sessionOptions增加`realtime bool`；模式证据为`turn_taking_test`，返回仍只含ready/observation/closed、id/label/duration_ms/evidence，无向量/音频/正文。scope固定，首段可从非零采样开始，后段start必须>=上段end。
- [x] 云端普通/race/vet/build及真实模型opt-in通过。不开麦、不操作正式服务、不保存真实输入。

## Task 2：非阻塞子进程桥

**Files:** 新增`internal/speakerlive/observer.go`、`observer_test.go`，只使用标准库。

```go
// 包 speakerlive；Session调用者提供已验证的源坐标。
type Config struct { Binary, Model, SHA256 string }
type Job struct { ID, Start, End int64; PCM []byte }
type Result struct { ID, Start, End int64; State, Label string; ElapsedMS int64 }
func New(ctx context.Context, session string, cfg Config, emit func(Result)) (*Observer, error)
func (o *Observer) Offer(job Job) bool // 复制有界PCM；队列满/关闭返回false，绝不等待模型
func (o *Observer) Close() // cancel、实际杀掉并回收子进程，释放排队PCM
```

- [x] 云端RED验证提交非阻塞、caller缓冲修改不污染、队列上限、输入界限、错误ID/长度/证据拒绝、卡住/坏行/EOF/取消均实际回收；不输出子进程任意错误正文。
- [x] 使用参数数组启动`session --model ... --sha256 ... --declared-single-speaker --realtime-test`，长度前缀JSON+PCM复用协议v1；sanitize环境，stderr丢弃，进程组受控。输出行<=4096字节，ready/观察字段、ID和duration均校验；启动15秒、每段3秒、失败固定unavailable。此预算为隔离诊断，非产品指标。
- [x] 不在模型失败后悄悄重建Tracker，避免同一会话访客A被重新解释；失败后识别不可用而聊天继续。通过后root包无ONNX依赖。

## Task 3：Session、保留边界与客户端接线

**Files:** `internal/realtimelive/{config,session,server,audit,native,asr}.go`；新增`speaker_observation.go`及测试；新增`cmd/realtime-speaker-test/main.go`；`tools/realtime-output-live/{main,LiveClient}.swift`和新增`SpeakerObservation.swift`及SDK测试/构建接线。

- [x] 先云端RED：MemoryOnly时recorder、native/asr意图均不写正文；不影响默认模式，非法memory/diagnostics组合拒绝。原会话元数据可写，跨会话记忆不可加载或保存。
- [x] Config新增MemoryOnly及可选Speaker配置；snapshot复制指针。只在隔离命令使用，正式默认关闭。newIntent可变可选memoryOnly参数让所有Native/ASR调用显式传递，recorder独立阻断落盘。
- [x] 跟踪成功Append的源采样末尾；每次新speech_started退休旧候选，speech_stopped后只把有可靠起止/完整覆盖的PCM送给旁路；无效、短或长段回不确定，不更改原语音处理。结果带session/input_id/span，旧结果不覆盖当前发言。
- [x] 原客户端新增可选`--speaker-test`，独立标签区和会话开关：仅试点ready提供功能标识才接受状态/开关；关闭中正在运行结果被退休，重新打开清空前会话候选需要明确标识重置。首版开关可限定为开始前选择，活动中禁用；关闭组仍走同一试点、同样MemoryOnly，公平比较。
- [x] UI默认不会开麦；用户点击开始才收音。复用现有AEC、Opus、播放和回报，新增候选显示不替换聊天状态。严格校验状态/标签/id/源坐标；可见提示轮流发言、重叠未验证、无持久声纹。SDK构建检查不等于真实听感。

## Task 4：综合测试、隔离部署与现场

- [x] 根Go全测、race/vet/build及嵌套module测试；独立review关闭阻断问题。Mac只做SDK构建和不访问麦克风的事件解析测试。
- [x] 源文件摘要与云端实际源码闭合；受保护服务PID/NRestarts/二进制和env摘要前后不变。
- [x] 独立试点设置5分钟识别、最长会话15分钟；仅回环18052，helper/模型路径固定云端，凭据只读现有server env且不下发Mac。systemd-run独立单元，资源限制，退出清理所有子进程。试点不接小智公网与OTA。
- [x] 云端合成真实音频+真实模型验证正常问答/识别事件/完整播放模拟；超时/停识别后聊天继续。不得以合成音色结果冒充真人准确率。
- [x] 打开独立原生聊天测试窗口并核对AX，当前未开麦；已请用户点开始后正常问3题，无需点采样。窗口交付完成，现场结果保持待反馈，不将工程检查代替真人验收。
- [ ] 收到并记录单人正常聊天的现场结果；有第二位参与者时再做A→B→A，分别验收，不从旧手动采样3句外推。
- [x] 更新handoff/验证报告，提交推送main，核对远端与干净工作区。不宣布持久登记或完整多人能力交付。
