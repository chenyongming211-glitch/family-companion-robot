# Whole-stage P2 sealed-drain 修正报告

修正对象：`final-review.md` 的完整列表唯一 P2，要求及验收按该文档 Required repair and acceptance。实现状态：Go 源码修正完成，最终验证如下；尚待独立复审及控制者的新 Linux 隔离构建。未操作设备、生产、串口、SSH、真实语音或原始录音。

## 根因及修正

正常 `tts.stop` 只封口下发。原实现随后清空 activePlayback/started，设备解码、软件队列或 DMA 仍可处于排空中；forced abort 因而不能保存原任务且缺 playback_id，nonforce speech_started guard 则直接跳过。迟到 interrupted final 虽进入账本，也无法填补此前未创建的暂停任务。

最小修正：

- 增加一个 `devicePlayback` 指针，始终别名引用现有最多16份账本中的最新实际播放块。实际 start 更新该指针，正常 transport seal 保留，未新增第17份播放记录或音频。
- 选择打断对象时先保持现有实际 active 块逻辑（包括 legacy unknown），否则选择尚未正常结算的 negotiated 设备归属。实际 abort/speech 中断入口先保存单份暂停快照，并以该对象的 playback_id 发送 interrupted stop；成功后统一消费 active/device 归属，防止反复选择旧块。
- `valid=false progress` 只锁定未知位置，不表示硬件排空，仍保留可发送匹配 stop 的设备归属。正常非 interrupted finished 才退休该设备归属，正常 settled 完成块不会因后续 abort/speech 复活。
- interrupted finished 可能先于其实际 abort 控制到达：暂时保留同身份供实际入口选取；并非在 receipt 中凭空创建暂停任务。若 final 后到，则只完善同ID既有快照。
- 新实际块更新当前设备归属；旧/新回报仅能作用于各自账本身份，旧 final 不会清除/改选当前设备块。已有原暂停任务不被旁支中断覆盖。
- 成功的 discard/replace 同步退休已经 finished、等待其控制事件的旧设备归属，避免显式任务修改后由旧归属复活任务。若原 paused 的 DMA final 尚未到，原实际 interrupt 已经发送匹配 stop 并消费旧设备归属；之后的旧 final 不会把归属重挂，也不会恢复被 discard/replace 的旧任务。
- onDevice 的 listen.start 触发条件完全未改；未把 devicePlayback 加入该条件。正常 stop 后固件自动 listen.start 不因尾部排空状态而触发 interrupted stop 或制造暂停任务。真正的 speech_started 仍通过扩展后的 interrupt guard。
- 设备 run 结束清空新增指针；上游独立重连不会错误重置设备播放账本。原有 12-turn/512-rune 历史、单份暂停、16份账本、uint32 身份、严格 receipt、永久unknown、每response发送证据及本地工具语义保持。
- 没有新增 PCM/音频留存、ASR、字词映射、外部服务或正文日志。text_position 始终 unknown，不作准确字词续播或物理播放成功声明。

## 先红后绿

1. 原审查原样复现：
   `go test -overlay .superpowers/playback-progress-20261022/final-review-repro/overlay.json ./internal/gateway -run '^TestFinalReviewAbortDuringSealedDeviceDrainPreservesTask$' -count=1`
   修复前 Exit 1：`normal transport seal discarded still-playing original task: map[available:false ok:true text_position:unknown]`。
   证据 `/tmp/playback-final-fix-overlay-red.log`。
2. 正式行为反例在生产源码修改前运行：
   `go test ./internal/gateway -run '^TestSealedDeviceDrainInterruptionPreservesTaskAndStopIdentity$' -count=1`
   Exit 1；abort/no-final、final-before、final-after、invalid-progress 均收到缺 playback_id 的 stop；speech 对应各分支没有发 stop 并超时。全部8个初始分支均失败，直接体现实际入口问题；无测试 sleep。
   证据 `/tmp/playback-final-fix-formal-red.log`。
3. 最小源码修复后，完整验收集合：
   `go test ./internal/gateway -run 'TestSealed|TestNormallySettled|TestNewSealed|TestTaskMutationRetires' -count=1`
   Exit 0，gateway 4.828s。证据 `/tmp/playback-final-fix-first-green.log`。
4. 修复后原审查 overlay 同命令 Exit 0，gateway 2.309s。证据 `/tmp/playback-final-fix-overlay-green.log`。

## 正式覆盖

新增 `internal/gateway/playback_drain_test.go`，所有情形均走实际两端 WebSocket、session 事件循环、真实播放/本地工具编排：

- `TestSealedDeviceDrainInterruptionPreservesTaskAndStopIdentity`：abort 与 speech 两入口，分别覆盖尚无 final、interrupted final 在入口前/后、invalid progress、invalid interrupted final 先到；验证匹配 interrupted stop ID、原请求、4320 sent、1440 played 或明确unknown、settled 与 text_position=unknown。
- `TestNormallySettledDeviceBlockIsNotPausedAgain`：正常完整 final 先到后，forced abort 不再携带旧ID、不创建 paused；speech 不发送 stop、不重连或创建 paused。
- `TestSealedSideDrainKeepsOriginalPauseAndDoesNotCrossPlaybackIDs`：原任务已暂停，旁支正常封口仍在排空；原任务的迟到 final 不改选旁支 stop ID，旁支再中断不覆盖原任务，旁支迟到 final 不更新原任务位置。
- `TestNewSealedDrainOwnsInterruptionAfterOldNormalFinal`：新块已开始/封口后旧正常 final 不清除当前新块归属，打断及任务来源保持新ID。
- `TestTaskMutationRetiresOldInterruptedDrainOwnership`：已有原 paused，旁支 interrupted finished 在其实际控制前到达；discard/replace 后后续 speech 不选取已finished旧设备归属，也不复活已放弃原任务。
- `TestUnsettledTaskMutationDoesNotResurrectOnLateFinalOrSpeech`：原实际匹配 stop 已发送、final 尚未到时 discard/replace；旧 interrupted final 后到及后续 speech 均不复活旧任务，replace 的新任务/unknown位置保留。
- 增强既有 `TestPlaybackLedgerBoundAndIdentityExhaustion`：20个播放块后仍16条，devicePlayback 与账本末条指针一致，不额外保活第17条。

跨 socket 因果关系使用同设备 receipt→listen模式变更→上游 session.update 回执，或同设备 receipt→abort→匹配 stop 的顺序建立；没有新增 sleep 或墙钟猜测。listen模式屏障也验证封口后的自动listen兼容路径不会仅因device归属触发中断。

## 定向及最终完整验证

- `go test ./internal/gateway ./internal/protocol ./internal/qwen -count=1`：Exit 0；gateway 25.043s、protocol 0.569s、qwen 1.521s。证据 `/tmp/playback-final-fix-targeted.log`。
- `go test ./internal/gateway -run '^TestUnsettledTaskMutationDoesNotResurrectOnLateFinalOrSpeech$' -count=1`：Exit 0，gateway 1.496s，discard/replace 两个未final变体均通过。证据 `/tmp/playback-final-fix-unsettled.log`。

最终环境 darwin/arm64、Go 1.25.6；全部源代码和测试修改结束后执行：

- `go test -race ./...`：Exit 0；gateway 26.752s，其他包通过/无测试，部分未改包命中缓存。证据 `/tmp/playback-final-fix-race.log`。
- `go vet ./...`：Exit 0，无诊断。证据 `/tmp/playback-final-fix-vet.log`。
- `git diff --check -- internal`：Exit 0。

Darwin cgo链接器仍输出已有 `malformed LC_DYSYMTAB, expected 98 undefined symbols ... found 95 undefined symbols` warning；未为此改项目或系统。上述通过与warning分开报告。

## 自审与交付

- 归属身份、上游生成代次、服务器transport seal与设备finished分别保持独立；代码没有把invalid progress当完成，也没有把normal settled任务重新选作暂停。
- 最小源码变动仅 `internal/gateway/playback_progress.go` 与 `session.go`；正式新行为测试一个文件，既有账本界限断言一个文件。firmware及root文档由控制者持有，未修改/暂存。
- 这次只处理独立审查唯一跨层 P2 及其明确的任务修改生命周期边界；没有扩大本阶段模型或映射架构。
- macOS结果不代替控制者即将执行的不可变新Go提交 Linux race/vet/build，更不代替设备/物理/生产验收。旧Go二进制不能标为本修正候选。
- main直接提交，不push、不创建分支/工作树；提交后冻结源码供原最终reviewer独立复审。

最终修正提交：`0d114768012d5e53008dccd6907abf95c6f41c50`。仅4个internal文件，main，未push；提交后源码工作树干净并冻结。
