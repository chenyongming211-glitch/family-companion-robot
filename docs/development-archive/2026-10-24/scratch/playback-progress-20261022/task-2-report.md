# Task 2 — Go 播放账本与暂停任务交付报告

状态：实现已完成；macOS 最终检查见下方。独立规格/代码审查及最终提交后的 Linux 隔离验证由控制者执行，本报告不宣称设备/现场/生产通过。

## 范围与实现

- 唯一线契约：`docs/protocol/playback-progress-v1.md`。仅改动 `internal/gateway`、`internal/protocol`、`internal/qwen` 源码/测试；未修改 firmware、生产配置或设备状态。
- 设备 hello 声明后服务端 hello 回应 `playback_progress_v1=true`；未协商时不发送 playback_id，也不信任原始 playback JSON。音频 v1/v2/v3 二进制封装不变。
- 每个实际 TTS start 分配非零递增 uint32 播放代号；上游 response.created 不消耗播放代号，uint32 耗尽返回连接错误。仅在成功二进制写入后累计每帧 1440 个 24kHz 采样，补零尾帧同样计数。
- 最多 16 个轻量播放记录，加一份暂停快照；上游重连保留设备播放代号/账本/暂停任务，设备 run 生命周期结束清除新增记录。暂停快照被最近账本淘汰后仍能接收同身份最终回报。
- 播放回报走独立严格解析，要求显式 valid、played_samples 等必需字段，校验非零 uint32、整数采样、24k、i2s_dma、session 和实际发送上界。重复幂等，倒退/越界/畸形/旧身份不推进位置。valid=false 锁定 unknown，后续 true 不修复已知缺口；有效最终回报结算后位置不可重写。提前 valid=true finished 不结算仍开放的块，但提前 valid=false 仍锁定缺口。
- 只有正常封口、valid、完整采样最终回报可升级同播放代号的 transport_sent 历史为 playback_confirmed，保留 generated_text 字段。部分/中断回报即便等于 SENT 总量也不会将生成稿升级为完整已播证据；原有 12 条/每条 512 字历史界限保持。
- committed 输入 item_id 与 upstream generation 一起固定请求来源；response.created 早于最终 STT 时，只接受匹配同代次同输入身份的迟到转写回填。工具父回复与续答保留原来源，包括父回复没有音频的情况。没有待绑定新身份的旧测试/旧事件流可使用已实际收到的转写；不拿无关转写补原请求。
- 只在实际播放块中断时创建一份暂停任务；旁支完成/再次中断不会覆盖原任务。原请求限 512 rune，超出时明确 original_request_truncated。未有准确字词标注，始终 text_position=unknown；未知采样位置为 null/position_known=false，不储存“已听文字”。
- 本地 conversation_get_paused_task 与 conversation_update_paused_task 独立于设备 MCP。修改需精确匹配 task_id；replace 仅使用当前实际用户 STT，生成新 task_id 并把新任务播放位置置 unknown，不继承旧位置，也不自动绑定下一条旁支。模型提供的 new_request 不作为任务内容。
- 本地工具复用既有调用去重、代次过滤、最大 8 次调用及输出续答流程。先按设备工具可用性决定设备控制提示，再添加本地工具；没有 MCP 时仍明确音量/屏幕控制不可用。
- playback 遥测不更新 lastActivity。没有新增音频留存、辅助 ASR、外部服务或日志正文。

## TDD 红绿证据

1. 初始 WebSocket 行为用例（生产代码修改前）运行：
   `go test ./internal/gateway -run 'TestPlaybackProgress' -count=1`
   Exit 1：`local context tools missing`；legacy 查询返回 `error:unavailable`；正常完整最终回报没有生成 `delivery=playback_confirmed`。随后实现协商/账本/暂停状态/本地工具，定向转绿。
2. 新边缘行为用例先红：
   `go test ./internal/gateway -run 'TestPausedTaskReceiptSurvivesLedgerEviction|TestToolContinuationWithoutParentAudio' -count=1`
   Exit 1：账本淘汰后暂停最终回报丢失；无父音频工具续答错误采用旁支请求。证据 `/tmp/playback-task2-edge-red.log`。
   修复后定向 Exit 0：`/tmp/playback-task2-edge-green.log`。
3. 畸形身份行为先红：
   `go test ./internal/gateway -run TestPlaybackLedgerValidationAndFinalImmutability -count=1`
   Exit 1：fractional/overflow playback_id 被通用控制解析器返回 `invalid device control JSON`，错误中止会话。证据 `/tmp/playback-task2-malformed-red.log`。修复为按 type 先路由独立 playback 解析，定向转绿。
4. 输入身份有界先红：
   `go test ./internal/gateway -run TestRequestOriginRejectsUnboundedUpstreamIdentity -count=1`
   Exit 1：committed/STT 可保留超长身份。证据 `/tmp/playback-task2-bounds-red.log`。增加 256 字节输入身份界限后同命令 Exit 0。
5. 无效提前 final 锁定先红：
   `go test ./internal/gateway -run TestPlaybackInvalidFinalBeforeServerStopCannotHeal -count=1`
   Exit 1：未封口 valid=false finished 可被后续 true progress 修复。证据 `/tmp/playback-task2-invalid-final-red.log`。修复后同命令 Exit 0。

## 覆盖与定向验证

- `go test ./internal/gateway ./internal/protocol ./internal/qwen -count=1`：Exit 0，三包通过（`/tmp/playback-task2-all-targeted.log`）。
- `go test ./internal/gateway -run 'TestPausedTaskLocalToolsMutateThroughBoundedContinuationWithoutMCP|TestPlaybackProgressDoesNotExtendIdle' -count=1`：Exit 0（`/tmp/playback-task2-tools.log`）。
- `go test -race ./internal/gateway -run TestPlaybackProgressToolContinuationCumulativeSentSamples -count=1`：Exit 0；通过同一个 2880 采样 final 验证父/续两条历史同时升级，而不是只检查发出 stop（`/tmp/playback-task2-cumulative.log`）。
- 真 WebSocket traces 覆盖：双向协商、legacy 无协商、补零尾帧、正常完整/部分/中断/unknown final、迟到回报不影响新块、工具同块累计采样、工具父已排空但块未关闭时中断、上游重连后暂停保留、同字符串 item_id 的新代次不回填旧任务、本地工具 discard/replace/task_id 防陈旧修改、周期遥测不延长闲置。
- 复杂验证边界用真实 WebSocket 包加状态断言覆盖：漏字段/null/小数、错误采样率/依据、旧会话/代号、重复/倒退/超界、valid=false 不可治愈、最终位置不可重写、16 条淘汰/计数耗尽、输入身份/请求长度界限。

## 最终 macOS 验证

环境：darwin/arm64，Go 1.25.6。

- 最后一次 `go test -race ./...`：Exit 0；gateway 21.064s，audio/devicetools/protocol/qwen 通过（部分无变更包命中 Go cache）；cmd/gateway、cmd/smoke 无测试。日志 `/tmp/playback-task2-final-race.log`。
- 最后一次 `go vet ./...`：Exit 0、无诊断。日志 `/tmp/playback-task2-final-vet.log`。
- `git diff --check -- internal`：Exit 0。

该最终完整检查在全部源代码与新增的累计采样确认断言之后执行；之后仅写交付报告和提交，不再改源码。

Darwin cgo 链接器出现既有 warning：`malformed LC_DYSYMTAB, expected 98 undefined symbols ... found 95 undefined symbols`。未为此修改项目或系统；测试 Exit 0 与该 warning 分开报告。

## 自审与交付界限

- 生成稿仍是生成稿；I2S 发送确认不代表人听清，也不等于逐字文本对齐。无精确继续位置承诺。
- replace 的未知播放位置是明确的保守语义，控制者已确认本阶段不要求绑定已打开的新任务块。
- 已复核 stale generation 主循环过滤、本地 call-id 去重、工具连续 TTS block、晚到回报与原任务身份、设备控制不可用提示、遥测不延长活动时间。
- 未接触 SSH/生产/串口/真实语音；Linux race/vet/build 将由控制者在最终不可变 Go commit 后执行。
- 根 docs 的设计/计划文件是控制者的未提交改动，不会暂存。该报告位于忽略目录，交付给控制者而不加入源码提交。
- 提交仅含本任务 internal source/tests，直接 main，不 push，不新建分支/工作树。

最终源码提交：`8dfcb3f`（完整 SHA 见控制者交付消息）。提交后冻结源码，未 push。

# 独立审查 P2 修正（2026-10-22）

审查输入：`task-2-review.md`，基准 `8dfcb3f8b3b90e3a69b4fd8ba2c25d4d77c19d26`。完整问题列表只有一项：共享块存在不代表当前 response 曾实际发送音频；零音频工具续答不能借父块采样确认升级历史。

## 红绿与最小修复

- 先原样运行审查 overlay：
  `go test -overlay .superpowers/playback-progress-20261022/review-overlay/overlay.json ./internal/gateway -run '^TestReviewNoAudioContinuationMustNotConfirmTranscript$' -count=1`
  Exit 1，`zero-audio continuation promoted using parent's 1440 samples`（`/tmp/playback-task2-review-red.log`）。
- 将反例加入正式 `TestPlaybackZeroAudioContinuationDoesNotBorrowParentConfirmation`，额外断言父音频仍 playback_confirmed、零音频生成稿仍 transport_sent。修复前同名定向测试 Exit 1，`zero-audio generated draft was lost or relabeled`（`/tmp/playback-task2-formal-review-red.log`）。
- 新增一个每 response 的成功下发证据 bool，仅在二进制 Write 成功后置 true；在每次 response.created（包括工具续答）、完成排空、取消、上游重连时清 false。只有该证据存在时，为本 response 的历史绑定可升级 playbackID。块本身的累计计数、父/续答共享身份、final校验均保持原逻辑。
- 零音频生成稿保留 generated_text/transport_sent，不丢弃，也不继承父音频的确认。
- 修复后原审查 overlay 同命令 Exit 0（`/tmp/playback-task2-review-overlay-green.log`）。
- 定向覆盖：
  `go test ./internal/gateway -run 'TestPlayback|TestPausedTask|TestRequestOrigin|TestNoAudio|TestToolContinuation|TestDeviceTools' -count=1`
  最终 Exit 0，gateway 11.930s（`/tmp/playback-task2-review-targeted.log`）。包含零音频反例及双方有音频时累计 2880 样本确认两条历史的既有用例。

第一次该定向集合另暴露已有测试的两个独立 socket 排序问题：设备 finished 与模型 tool query 没有先后屏障，偶发 query 先读到 settled=false；该失败并非本次发送证据变更引起。仅在 `TestPlaybackProgressNegotiatedPausedTaskSurvivesReconnect` 用设备 listen 模式更新回执建立因果屏障，保证前面的同设备 receipt 已处理后再发模型查询；未改生产行为。补屏障后上述定向集合通过。

## 修正后最终完整检查

- 最后一次 `go test -race ./...`：Exit 0；gateway 22.008s，其他包通过/无测试，部分无变更包命中缓存。日志 `/tmp/playback-task2-review-race.log`。
- 最后一次 `go vet ./...`：Exit 0，无诊断。日志 `/tmp/playback-task2-review-vet.log`。
- `git diff --check -- internal`：Exit 0。
- 上述最后完整检查在本次全部 source/tests 修改之后执行。

仍有既有 Darwin cgo `malformed LC_DYSYMTAB` 链接 warning，未为此改代码或系统。本次只修一项 P2 和必要测试排序；未改 firmware、任何 root docs 或生产配置，没有设备/真实语音/串口/SSH操作。提交只包含4个 internal 文件、不 push；提交后冻结供独立复审与控制者 Linux 验证。

Whole-stage sealed-drain P2 的完整修正及验证见同目录 `final-fix-report.md`。
