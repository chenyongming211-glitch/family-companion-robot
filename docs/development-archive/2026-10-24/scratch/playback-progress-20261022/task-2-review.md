# Task2 独立规格符合性与代码质量审查

审查对象：`17bb1e001f9f25926071a6a8871426ae52235e4a` → `8dfcb3f8b3b90e3a69b4fd8ba2c25d4d77c19d26`。
已核对工作树 HEAD 为该不可变提交。产品源文件未修改；仅新增本报告及忽略目录中的隔离复现材料。

## Verdict

- **规格符合性：需要修正（1 项 P2）**。大部分要求有实际入口、状态转换与行为测试支撑，但零音频工具续答可被错误升级为设备播放确认，违反“仅匹配正常完整音频历史可升级”的限制。
- **代码质量：需要修正（同一项 P2，无其他已确认阻断问题）**。新增状态集中在单线程事件循环中，解析/账本/工具职责清楚，资源和连接生命周期总体合理；历史绑定缺少当前 response 的音频发送证据。

## P2：零音频工具续答借用父块确认，把未下发音频的生成稿升级为 playback_confirmed

位置：`internal/gateway/playback.go:78-79`；后续升级发生在 `internal/gateway/playback_progress.go:104-109`。

工具父回复与续答共用 activePlayback 是正确的，但行 78 的关联条件只检查“块存在”和“转写非空”，没有检查当前 response 是否实际成功发送过音频。父回复已经发过音频时，续答继承 started/activePlayback；若续答只有 `response.audio_transcript.done` 与 `response.done(completed)`，没有 `response.audio.delta`，它的生成稿仍会被赋予父块 playbackID。随后仅确认父音频的完整有效 finished 会把两条历史都提升为 playback_confirmed。设备确认的 1440 个采样没有任何续答音频，不能作为续答的播放证明。

实际最小复现使用现有真实双向 WebSocket harness，经过 `run → onModel → play → onDevice → receivePlayback`，不是直接调用 promotePlayback：

1. 协商 playback_progress_v1，无设备 MCP。
2. 父回复发送 1 帧（1440 samples）音频，转写为 `parent-audio`，调用只读本地工具并正常完成。
3. 工具续答发送 `response.created`、转写 `no-audio-continuation`、completed；发送音频帧数为 0。
4. 收到 stop 后，设备回报父块有效 finished，played_samples=1440。
5. abort 触发上游重连并读取历史，实际发现 `"delivery":"playback_confirmed","generated_text":"no-audio-continuation"`。

复现命令：

```sh
go test -overlay .superpowers/playback-progress-20261022/review-overlay/overlay.json ./internal/gateway -run '^TestReviewNoAudioContinuationMustNotConfirmTranscript$' -count=1
```

结果：**Exit 1**，`review_noaudio_test.go:28: zero-audio continuation promoted using parent's 1440 samples`。测试运行约 0.85s；没有重跑完整套件。测试通过 Go overlay 加入虚拟测试文件，未写入 `internal/`。复现文件位于 `review-overlay/review_noaudio_test.go`。

修正建议：保留父/续答共享块及累计计数，但仅当本 response 有成功发送音频的证据时，为其历史建立可升级的 playbackID 关联。零音频稿可继续保留为 generated_text，不能因父块的确认升级。增加同一 WebSocket 反例，并保留现有父、续答均有音频时两条历史都正确升级的用例。

证据边界：这是合成上游事件序列的确定性复现，未声称已在真实 Qwen 服务上观察到该序列。当前客户端没有拒绝这类序列，新增播放确认状态仍应保守处理无音频证据的转写。

## 已检查并满足的主要要求

- `session.run` 读取设备 hello 后返回同名功能；旧设备不发送 playback_id，不接受原始回报作为协商。`onDevice` 与 MCP discovery 都先独立路由 playback，避免小数/溢出 playback_id 误走 Control 导致会话退出。
- ParsePlayback 严格检查必需字段、显式 valid、整数 uint32/uint64、24k、i2s_dma；账本再校验设备 session、存在的播放身份、发送上界和单调前缀。invalid 锁定、settled 不可重写、提前有效 final 不结算开放块的路径一致。
- `play()` 在实际 start 分配 ID，成功二进制写入后累计 1440；尾帧沿原有 Buffer padding 路径计数。工具父和有音频续答保留同块和编码器；播放代号耗尽返回错误。
- 最近 16 份记录加一份暂停快照；账本淘汰后仍能更新同身份暂停快照。设备 run 结束清理；上游 reconnect 仅重置输入来源代次，不清播放账本与暂停任务。
- `run` 在 onModel 前过滤旧 generation；输入来源由 generation + item_id 构成，committed 固定身份，迟到匹配 STT 回填未知来源，无关 STT 不窃取原任务。工具 continuation 无父音频时也保留 responseOrigin。
- interrupt 在 actual started block 上创建一次快照，包括父回复已排空、等待工具续答的窗口；旁支中断不覆盖原任务，无音频不凭空创建任务。
- 本地工具在 callTool 的 response 匹配、call-id 去重、次数限制后分派，输出沿原有 function_call_output/response.create 编排；工具修改要求匹配 task_id，replace 只取实际 currentInput、生成新任务身份并重置未知位置。上游代次过滤在真实主循环生效。
- toolConfig 先按设备工具可用性选择提示，再加本地工具，保持无 MCP 时普通问答和本地工具可用、设备控制仍显示不可用。
- 请求 512 rune 且显式截断标志；text_position 始终 unknown，未知采样为 null；不把生成稿当已播文字。未新增音频留存或正文日志。playback 遥测不延长 idle。

## 验证及跨任务边界

- 本次独立阅读 brief/context/report、指定 diff 包以及实际生产入口和测试。未把实现报告或已通过测试当作审查结论。
- 控制者报告同一不可变 head 的 Linux/amd64 Go 1.27.3 race/vet/build 均 Exit 0；本审查没有重跑，也没有访问服务器。该证据不覆盖上述新增反例。
- Task1 固件实现、ISR/DMA 连续前缀准确性、原始固件对照变体、锁定 IDF/依赖及固件资源预算由其独立审查负责；本 Go diff 不能证明这些项。
- 真实词级映射、设备声学/AEC 效果、人耳听清、真实 Qwen 事件顺序、设备升级及生产运行均未由此审查验证。未操作设备、串口、正式服务或真实音频。
