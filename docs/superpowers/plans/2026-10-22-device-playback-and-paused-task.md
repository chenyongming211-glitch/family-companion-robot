# 设备播放回报与暂停任务实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development task by task, with independent task reviews. Execute directly on main under the user's existing instruction; no extra worktree or branch.

**Goal:** 将服务器回复与设备实际 I2S TX 完成边界对应，并保存不被旁支问答覆盖的暂停任务；随后验证文字位置契约，不接辅助 ASR。

**Architecture:** 固件为每段解码/输出保留回复身份，由完成回调累计连续前缀，任务线程发送回报。网关按协商启用，区分已下发、已确认及最终未知，独立保存原任务。现有千问重连和音量/屏幕工具保持；当前不使用未验证的原生 truncate 或消息重排。

**Tech Stack:** Go、C++17、ESP-IDF6.1 固定提交、ES8311/StopWatch、现有小智 WebSocket。

## Global Constraints

- 用户本轮明确批准“设备实际播放回报和暂停任务状态，再验证文字位置映射；暂不加入辅助 ASR”。按该范围执行，无须再次请求普通实施许可。
- 只在 main 开发、提交及推送；不创建分支/工作树。不改生产服务、不刷机、不操作串口、不采集现场音频；先交付已核验构建候选。
- 固件上游 `49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`、IDF `fff9895c82d744c7237be8847347bdd1b07c6643`、依赖锁保持；原始对照变体应继续可构建。
- 统一线协议见 `docs/protocol/playback-progress-v1.md`，修改先由控制者同步两个任务。进度依据必须是 I2S TX 完成并对应回复连续前缀。
- 不添加 ASR/家庭业务/长期记忆/逐数字专用计数器；不凭文本长度比例估算精确停字位置。
- 不记录/提交凭据、现场音频、设备备份和生成固件。保留 AEC 与断句收音的现有行为；物理效果不由 CI 代替。

### Task 1: 固件完成边界、身份与回报

**Files:** 新增 `firmware/patches/0002-playback-progress.patch`；修改软件 AEC codec overlays；新增独立可主机测试的固定容量 DMA 跟踪头及 C++/Python 测试。必要时新增固件原生集成测试、配置和验证元数据。

**Consumes:** 线协议；现有 `SoftwareAecCodec::Complete` 与 `DuplexReference`；固定 IDF 实现。
**Produces:** 具备协商、逐帧归属、200ms进度和最终回报的可编译固件源码；普通对话降级可用；源码测试与实际目标编译分开报告。

- [x] 读完整 codec/音频任务/协议/AEC 路径与固定 SDK `i2s_dma_tx_callback`。设计并说明每一份 PCM 的身份如何到达真实完成回调，不能以完成总计或 PCM 相似度匹配代替归属。
- [x] 首选使用 SDK 已支持的 DMA 回调重填模式：软件 AEC 变体禁用 callback 后自动清零，由固定容量 PCM 队列在完成回调中先记录旧 DMA 内容与身份，再填下一轮 PCM 或零。不调用 `i2s_channel_write` 同时写这些 DMA；原始变体维持原路径。若采用其他实现，必须提供同等严格的归属证明，不修改 SDK 外部文件。
- [x] 使用小而明确的核心接口，例如 `Submit(identity, first_sample, pcm, count)`、`Complete(dma_address, completed_pcm, count)`、`Seal(identity, interrupted)`、`Snapshot()`；身份至少包含本地连接代次与服务端播放代号。线程包装与纯状态机分开，ISR固定容量、无分配/网络/日志/嵌套锁。
- [x] 先写并跑失败用例：提交后无完成不增长；6个 DMA 槽轮转、非零与全零语音、提示音/空档不计数；中断清队列与 DMA 尾部；正常封口排空；旧代次、乱序/遗漏、格式/写入失败；临时断流；新回复紧随中断；硬件重开及计数边界。C++测试必须调用实际生产核心，不能只搜索源码。
- [x] 把身份从接收帧贯穿到在途解码/输出，处理 TTS start 与异步主线程状态切换、stop/reset/new start 的竞态。队列满/解码错误使该块位置无效；不能借下一块代号重标旧任务。
- [x] 发送回报走正常任务，有界且有连接身份检查；在原始服务器上不带回报也能正常唤醒、对话和控制。验证 esp_codec_dev 输出音量路径是否包含必要处理；如绕过写入，明确保证硬件音量行为并保留 AEC 的物理参考。
- [x] 跑 `python3 -m unittest discover -s firmware/tests -v`；新鲜准备树并跑上游脚本测试。记录新增 SRAM 上界及锁/回调路径，再提交自己的文件到 main，不推送。
- [x] 独立规格与代码评审；修复后再评审。只有无未处理重要问题时进入任务2。

### Task 2: Go 播放账本与暂停任务

**Files:** `internal/protocol/protocol.go`、新播放回报解析/测试；`internal/gateway` 新播放账本与暂停任务模块/测试，接入 `session.go`、`playback.go`、工具与历史。固件文件不由此任务修改。

**Consumes:** 任务1固定线协议与现有 transport_sent/retired response 规则。
**Produces:** 最多16播放记录、一份暂停任务；本地上下文工具；未获文字映射时明确 unknown；旧固件兼容。

- [x] 先写 WebSocket 行为测试：协商前不信任回报；正确代号/会话的有效进度；重复/倒退/越界/漏字段/小数/旧代次；正常与中断最终回报；工具共享TTS块；完成或断开清理。
- [x] 为实际 TTS 块分配 uint32 递增代号，记录已成功发送的24k采样数。只在双方协商后携带元数据；保持旧二进制封装。最终有效且完整的回报才可升级相应历史为播放确认；普通 transport_sent 不自动升级。
- [x] 在回复生成开始时固定原用户请求；中断时创建暂停快照，回报可在模型重连后继续完善旧快照。旁支问答及其再次中断保留原任务，不用生成稿尾部填入已播文字。
- [x] 提供本地 `conversation_get_paused_task` 只读工具及 `conversation_update_paused_task`（discard/replace）工具；修改必须携带匹配 task_id，replace 采用当前实际用户转写。接入既有函数调用编排，与设备工具独立可用，不增加外部服务。
- [x] 无词级标注时返回 `text_position=unknown` 和已知/未知的采样位置，工具不得声称已能精确继续。原任务请求最多512字，音频不新增留存，日志不写内容；连接生命周期清理。
- [x] 先观察失败再最小实现；跑针对性及完整 `go test -race ./...`、`go vet ./...`，Linux隔离再测/构建。独立评审后提交。

### Task 3: 构建、文字位置契约验证与交付

- [x] 在已确认采样边界条件下，以受控文字时间标注验证映射取“已完成的完整词/短语”，边界前后、缺标注、空洞及旧代次都不能误判；不将这类标注称为当前千问已有的能力。当前真实模型没有标注时保持 unknown。
- [x] 进行全改动独立评审；逐项解决重要问题。核对上下游线协议和固件回报与 Go 状态衔接。
- [x] 推送main，验证本次确切提交的实验/原始两变体 CI 及构建产物身份；检查清单和 SHA256，保留待刷候选及回退信息。不得把构建成功写成实机成功。
- [x] 更新证据文档与计划：代码、主机测试、目标构建、现场验证分别记录。当前现场验证仍待后续提前告知的用户配合窗口，本轮不直接刷机或开启录音。
