# Realtime重复回复：全提交历史审计

审计日期：2026-10-31（北京时间）。冻结起点`a8f605ef5c56dea5bb08dc089e42583c9f312bfe`，截至该提交共237次提交，仓库不是浅克隆，所有本地refs可达提交也为237。本报告自身后续提交不包含在这个冻结计数中。

## 结论

**目前没有找到一个在相同条件下已确认消除“安静时首次回答被截断或自动再回答”的历史提交。** 不能把一次正常、构建通过或没有报错直接标为“不会重复”。

已有历史现场表现较好的完整产品快照中，首选回归基线是**2026-10-25 14:05的`4240bd2`**：此时Mac默认切换Go，实际后台为`b6f6ebd`。原记录有4次真人Go/Python会话、53段输入及明确体验反馈。这是对照候选，不是已定位的无故障版本。

用户在本轮再次开启普通客户端后报告仍有重复，撤回本轮AGC/静音实验和重开客户端不能作为修复结论。此次审计只读历史、源码和云端既有记录，没有启动新语音测试、切换服务或修改产品代码。

## 审计方法和范围

1. 遍历全部237次提交的时间、主题、变更文件，形成末尾完整索引；不是只查看最近几个版本。
2. 重点比较共享Mac采集/播放、Opus、16k/24k转换、Realtime Mac协议、Python链路前身、Go监听/取消及历史验收记录。
3. 对15个关键快照计算相关文件SHA256及`Native.Open`函数指纹；对Go最初版与当前`Session.listen`作逐行差异检查。
4. 只读重查10月25日三次Go真人会话和最新普通Mac会话的服务端事件；不保存或上传真人原音频，不将正文或设备标识放入报告。

完整索引的分类只用于文件路径初筛；“文档/测试/配置资料”包含配置，不表示所有配置均无运行影响。没有对237个版本逐一做真人声学验收，也没有把静态源码审计当成这种验收。

## 关键版本与已有证据

| 提交 / 北京时间 | 改动或验收 | 本问题的证据边界 |
| --- | --- | --- |
| `391c4d9` / 10-24 13:47 | 初版原生Mac音频客户端 | 后来发现开启Voice Processing后默认声道映射输出全零；不能把不收音、不回复的旧实现当成“不重复”修复 |
| `2b507f5` / 10-24 14:14 | 修正多声道→单声道映射，明确取通道0 | 用户确认数学和短故事恢复；当时是三段式现场，不是当前Realtime首次问候防回声验收 |
| `c096c29` / 10-24 21:21 | 旧Go网关的独立Realtime Mac尾音交接修正 | 与当前逐轮生成/输出ASR入口不同；已有合成输入验收，不能直接认定真人无重复 |
| `976a901` / 10-24 23:02 | 当前输出ASR逐轮语音架构的Python前身 | 已有server_vad及speech_started立即interrupt；易被误VAD取消的机制不是最近才添加 |
| `636ac95` / 10-25 12:51 | 当前Go语音核心最初实现 | 继承同样的VAD/取消规则，不是已证明无故障分界 |
| `b6f6ebd` / 10-25 13:07 | Go迁移验收时的实际后台 | 有真人多轮与合成对照；当前回读其三次Go现场42段输入，无input_failed/turn_error/drain_timeout/session_error。无错误不等于逐段已排除误VAD |
| **`4240bd2` / 10-25 14:05** | **真人Go/Python对照后，将Mac默认入口切Go** | **首选完整回归候选**；原报告共4会话53段输入，用户反馈体验接近。没有专门覆盖多次冷启动后的仅问候并静默条件 |
| `c9050e0` / 10-25 14:54 | 增加终端/会话/记录归属 | 10-30旧版后台对照有1会话1轮正常；样本有限，且当时Mac音频客户端没有随后台回到历史版本 |
| `a5a4e54`、`6dc191a` / 10-25～27 | 小智接入、多设备隔离 | 无证据证明这些提交新增或消除了Mac重复；不能仅按时间先后归因 |
| `bda65e2` / 10-28 | 当前设备语音控制 | 未改变VAD起点即取消的规则；未定位为重复问题引入点 |
| `a669f80` / 10-28 | 同设备跨会话记忆 | 增加记忆及转写就绪信号；10-30记忆开/关均有正常样本，仍不能单独排除所有时序影响或证明它是根因 |
| `09d22e6` / 10-29 22:58 | 云端音频诊断；目前运行后台 | 已有多次真实故障；同时也有正常样本，不能标为稳定无重复 |
| `d7a0a95` / 10-30 12:30 | 记录Mac首次多轮误取消/重复 | 此时仓库AudioPipeline仍与`2b507f5`逐字相同；早于下面Mac诊断实现 |
| `851a961` / 10-30 13:59 | 默认关闭的Mac首播诊断 | 发生在已记录的Mac故障之后，不能解释此前故障的最初引入 |
| `76d7f89`、`d475b41` / 10-30～31 | 相关性诊断及时钟修正 | 是诊断实现；撤去未提交实验后仍再次收到重复反馈，未形成修复提交 |

## 源码比对的确定发现

### Mac音频核心在首次故障前长期相同

`tools/native-voice/Sources/AudioPipeline.swift`在`2b507f5`、`976a901`、`b6f6ebd`、`4240bd2`、`c9050e0`、`bda65e2`、`a669f80`、`09d22e6`和`d7a0a95`的SHA256均为：

`58b4d8c30e91`（前12位，完整值留私有审计输出）。

从10-24声道修正到10-30首次Mac故障记录之间，该文件没有新的提交。`MicrophonePCM.swift`、`Opus.swift`、`OpusBridge.c`在这些阶段也相同。因此回到10-25快照并不会自动获得另一套已证明更好的AEC算法。

10-30之后AudioPipeline有诊断接线变化，当前关闭诊断并不等于二进制完全相同；这里区分源码相同的早期阶段与后续诊断差异，不声称所有运行时开销严格等价。

### VAD设置和起点取消政策并未出现近期切换

从`636ac95`到当前，所查Go快照的`Native.Open`函数只有一个SHA256版本。监听配置持续为`server_vad`、threshold=0.5、silence_duration_ms=600，create_response/interrupt_response均false；这与迁移时冻结配置一致。

初版与当前`Session.listen`的差异只有：两处`interrupt()`变为带来源名的`interruptFrom(nil, ...)`，以及记录用户转写后增加transcriptReady信号。两版都在speech_started先取消、再记起点/通知listening；speech_stopped还会取消并创建新轮。没有找到将“已确认真人才停”改为“检测到声音就停”的近期提交。

`native.go`整体后来为设备工具规划增加了text-only/tools设置分支；这与监听函数本身相同是两个不同层次的事实。记忆、控制、归属可能影响运行时序，本次没有凭静态比较宣布这些模块绝无影响。

Python前身`976a901`的listener也使用相同VAD参数，并在speech_started立即调用interrupt。因此仅退到Python前身也没有“此机制不存在”的源码依据；本轮没有恢复Python产品入口。

### 上次对照并未替换整套历史Mac客户端

10-30的报告明确记录：旧/新两个窗口使用同一份Mac编译产物，只有标签/颜色/位置不同；后台分别运行`c9050e0`和`09d22e6`。

因此那次可以描述为**后台历史版本对照**，不能描述为“旧Mac客户端＋旧后台”与“新Mac客户端＋新后台”的完整对照，更不能凭旧版后台问候正常就排除客户端版本影响。此前对用户说明没有充分突出这个限制，现予以纠正。

## 当前和历史现场记录复核

- 10-25三次Go真人会话：输入13、9、20段，共42段；无上述四类错误。后两轮各有出声前被新输入替换的情况，不能把它们都算已发生声学播放的插话。原报告另包含一次Python真人11段，共53段；本次直接回读的原始Go证据不扩大为重新核验了Python原始会话。
- 10-30后台对照：旧后台1轮正常；当前后台及记忆/客户端入口组合也正常；随后同一普通Mac进程又复现。因此单次正常不构成版本二分的“好版本”标记。
- 本次普通客户端最新已结束会话：2个输入段；首答下发后约1335ms被新VAD取消，只发送1.5秒音频；第二段最终转写为空，随后第二次生成。与用户再次报告重复并存，说明恢复普通客户端后并未可靠解决。空转写不单独证明没有真人开口，也不据此猜测具体AEC缺陷。

上述原始会话标识、详细正文和数值日志只留私有验证目录，报告中不公开。

## 回归/回退边界

如果下一步做完整历史回归，应冻结`4240bd2`的Mac客户端与该时期Go后台一起对照；不是只将后台换成旧版、继续使用当前Mac客户端。要记录客户端构建、实际音频设备/音量、每次音频引擎生命周期及实际后台版本。模型在线实现与系统音频状态不由Git提交完全冻结。

`4240bd2`早于终端归属、小智共享接入、多设备隔离、当前设备控制、同设备跨会话记忆和新版故事提示。整套直接回退会丢掉这些后续能力；现有正式记录/身份格式也不应直接交给旧实例混写。因此先作为独立回归候选，不把整套回退说成不会影响其他功能，也不在本轮擅自切正式服务。

本轮未修改音频/VAD/取消策略，未重新开始麦克风、开关串口、刷固件或重启服务。当前Go仍为`09d22e6`，健康检查ready/记忆就绪且无活动会话。代码历史给出了候选和排查范围，**尚未给出已验证的无重复版本或修复提交**。

相关原始报告：[迁移验收](2026-10-25-realtime-go-migration.md)、[冻结配置](2026-10-25-realtime-go-baseline.json)、[麦克风修正](2026-10-24-native-microphone-fix.md)、[首次故障](2026-10-29-realtime-audio-diagnostics.md)、[后台历史对照](2026-10-30-realtime-version-comparison.md)、[正常/异常同构建](2026-10-30-mac-echo-correlation.md)、[实验撤回](2026-10-31-echo-barge-in-confirmation.md)。

## 全部237次提交索引

按提交拓扑历史中的正序列出；时间统一转换为北京时间。标签来自变更路径，仅用于定位。以下是冻结起点的历史，不包含本报告自身提交。

| 提交 | 北京时间 | 路径标签 | 提交说明 |
| --- | --- | --- | --- |
| `9cbe153` | 10-21 23:40 | 旧Go网关/独立Realtime | chore: preserve verified voice gateway before natural barge-in firmware |
| `a3d08ad` | 10-21 23:49 | 文档/测试/配置资料 | docs: plan reproducible StopWatch firmware baseline |
| `0f9456f` | 10-21 23:52 | 固件/设备 | build(firmware): pin and safely prepare stock StopWatch baseline |
| `69b44f9` | 10-21 23:55 | 固件/设备 | fix(firmware): normalize late gzip decoder failures |
| `8437b16` | 10-22 00:04 | 其他实现 | fix(firmware): select current StopWatch board directory |
| `634eca1` | 10-22 00:09 | 其他实现 | chore: use main for direct project development |
| `fe99184` | 10-22 00:11 | 文档/测试/配置资料 | docs: plan explicit interruption stop metadata |
| `353f715` | 10-22 00:12 | 旧Go网关/独立Realtime | feat(protocol): identify interrupted TTS stops |
| `0734591` | 10-22 00:14 | 文档/测试/配置资料 | docs(firmware): stop local builds on SDK mismatch |
| `32df47e` | 10-22 00:20 | 文档/测试/配置资料 | docs: record completed interruption metadata checks |
| `8953c44` | 10-22 00:35 | 其他实现 | ci: add manual artifact storage probe [skip ci] |
| `6899919` | 10-22 00:48 | 文档/测试/配置资料 | docs: record verified stock firmware baseline [skip ci] |
| `0bf3361` | 10-22 00:50 | 文档/测试/配置资料 | docs: plan bounded playback reference history |
| `9010fc3` | 10-22 00:51 | 固件/设备 | feat(firmware): add bounded playback reference history |
| `33b0401` | 10-22 00:58 | 文档/测试/配置资料 | docs: record USB noise investigation and experiment preparation plan |
| `bd9f826` | 10-22 00:59 | 文档/测试/配置资料 | docs: preserve native SDK dependency lock format |
| `76361d0` | 10-22 01:00 | 文档/测试/配置资料 | docs: plan software AEC integration and field gating |
| `3d00bf3` | 10-22 01:01 | 文档/测试/配置资料 | docs: add official StopWatch power and amplifier evidence |
| `1c5b1c6` | 10-22 01:02 | 文档/测试/配置资料 | docs: require DSP generation isolation in AEC adapter |
| `d54991c` | 10-22 01:06 | 固件/设备 | Add atomic experimental firmware source preparation |
| `3062fd4` | 10-22 01:06 | 文档/测试/配置资料 | docs: account for StopWatch USB diagnostic console input |
| `07f825e` | 10-22 01:09 | 文档/测试/配置资料 | docs: include disabled-feature firmware build regression |
| `04f0b94` | 10-22 01:09 | 固件/设备 | Reject overlays claiming generated firmware metadata |
| `4564a30` | 10-22 01:11 | 文档/测试/配置资料 | docs: distinguish inactive capture from actual DMA loss |
| `ad5d513` | 10-22 01:11 | 固件/设备 | Reject overlay paths through patch-created symlinks |
| `fcf6465` | 10-22 01:12 | 文档/测试/配置资料 | docs: mark experimental preparation verified |
| `7bee4db` | 10-22 01:40 | 固件/设备 | feat(firmware): integrate StopWatch DMA software AEC experiment |
| `ccb3edd` | 10-22 01:41 | 文档/测试/配置资料 | docs: preserve wake audio and specify capture and rollback evidence |
| `3f433d0` | 10-22 01:56 | 固件/设备 | fix(firmware): preserve fallback and isolate observer capture boundaries |
| `740947f` | 10-22 01:56 | 文档/测试/配置资料 | docs: distinguish audio power state in USB noise investigation |
| `cb85517` | 10-22 01:59 | 文档/测试/配置资料 | docs: record reviewed AEC source integration milestone |
| `acec6d0` | 10-22 02:08 | 固件/设备 | feat(firmware): deliver bounded AEC collector and experiment CI |
| `026415a` | 10-22 02:20 | 固件/设备 | fix(ci): trust only checkout path for source SHA checks |
| `3ac93b1` | 10-22 02:34 | 固件/设备 | fix(firmware): guard ISR state with SDK mux and consume AFE lock result |
| `530a395` | 10-22 02:35 | 固件/设备 | fix(firmware): retain existing notices in evidence bundle |
| `15fbc73` | 10-22 02:45 | 固件/设备 | fix(firmware): accept identical native SDK config aliases |
| `c6fc6c2` | 10-22 03:00 | 文档/测试/配置资料 | docs: record validated AEC firmware and remaining field gates |
| `5b8af2e` | 10-22 13:10 | 固件/设备 | fix(firmware): restore polling USB diagnostics and record first field trial |
| `ab50c77` | 10-22 13:15 | 文档/测试/配置资料 | docs: defer USB noise investigation and record gateway readiness |
| `1f816c2` | 10-22 13:29 | 文档/测试/配置资料 | docs: record healthy AEC transport and pending acoustic evidence |
| `1744921` | 10-22 13:41 | 旧Go网关/独立Realtime | fix(gateway): retain fast-generated long voice replies within a bounded queue |
| `249d88b` | 10-22 13:46 | 文档/测试/配置资料 | docs: record long-reply deployment and onsite acoustic observations |
| `b2ab6a0` | 10-22 14:08 | 旧Go网关/独立Realtime | diagnostics(gateway): trace voice control boundaries without content |
| `34c8ff8` | 10-22 14:09 | 文档/测试/配置资料 | docs: record verified voice trace build and staged release |
| `7c3a15b` | 10-22 15:31 | 旧Go网关/独立Realtime | fix(gateway): preserve speech prefix when native cancellation precedes VAD |
| `4490a1d` | 10-22 15:36 | 文档/测试/配置资料 | docs: record verified cancellation prefix repair and isolated barge-in trials |
| `756ef07` | 10-22 19:29 | 文档/测试/配置资料 | docs: record two successful onsite interruptions and complete count playback |
| `c5a8f36` | 10-22 19:43 | 旧Go网关/独立Realtime | fix(voice): retain interrupted drafts and original task completion context |
| `7fcb1b9` | 10-22 19:56 | 文档/测试/配置资料 | docs: record reproduced short continuation and verified gateway release |
| `52c9f96` | 10-22 20:08 | 文档/测试/配置资料 | docs: record failed field continuation and playback progress proposal |
| `9148d5c` | 10-22 20:15 | 文档/测试/配置资料 | docs: separate played-context synchronization from transcript display |
| `fcb56a9` | 10-22 20:49 | 旧Go网关/独立Realtime | fix: reuse sealed playback lifecycle and explicit transport history |
| `5bae4ea` | 10-22 20:50 | 文档/测试/配置资料 | docs: close upstream playback contract verification record |
| `28cb072` | 10-22 20:56 | 文档/测试/配置资料 | docs: define device playback receipt and paused task implementation |
| `0a06bc1` | 10-22 21:21 | 固件/设备 | feat(firmware): report identity-bound DMA playback progress |
| `101cb4e` | 10-22 21:48 | 固件/设备 | fix(firmware): preserve playback lifecycle and ordinary output fallback |
| `17bb1e0` | 10-22 21:56 | 文档/测试/配置资料 | test(firmware): clarify fixture guards for GCC warnings |
| `8dfcb3f` | 10-22 22:02 | 旧Go网关/独立Realtime | feat(gateway): track negotiated playback and paused task context |
| `9f02a4d` | 10-22 22:08 | 旧Go网关/独立Realtime | fix(gateway): require response audio evidence for playback confirmation |
| `0b5eab0` | 10-22 22:13 | 文档/测试/配置资料 | docs: record device playback and paused task candidate verification |
| `0d11476` | 10-22 22:27 | 旧Go网关/独立Realtime | fix(gateway): retain interruptible playback ownership during device drain |
| `3a1c373` | 10-22 22:29 | 文档/测试/配置资料 | docs: identify rebuilt gateway after sealed drain repair |
| `901f64d` | 10-22 22:32 | 文档/测试/配置资料 | docs: close playback and paused task candidate review |
| `d2d70e6` | 10-22 22:45 | 旧Go网关/独立Realtime | feat(gateway): trace playback and paused task boundaries without content |
| `cade444` | 10-22 22:47 | 旧Go网关/独立Realtime | fix(gateway): deduplicate invalid final and shared playback diagnostics |
| `fff49ef` | 10-22 23:04 | 文档/测试/配置资料 | docs: record deployed playback candidate and initial device evidence |
| `9cd1edb` | 10-22 23:12 | 旧Go网关/独立Realtime | fix(gateway): keep wake greetings out of paused task ownership |
| `4c51de2` | 10-22 23:18 | 文档/测试/配置资料 | docs: record wake ownership repair and isolate model execution issue |
| `c6f2787` | 10-22 23:40 | 旧Go网关/独立Realtime | fix: execute spoken tasks in the current realtime response |
| `0bc5e3f` | 10-22 23:44 | 文档/测试/配置资料 | docs: record continuous-input fulfillment verification and rollout |
| `8626c27` | 10-22 23:59 | 文档/测试/配置资料 | docs: audit flashed firmware wake and playback ownership paths |
| `c95513e` | 10-23 00:40 | 其他实现 | test: audit realtime frameworks and firmware wake failure paths |
| `d68ec44` | 10-23 09:28 | 旧Go网关/独立Realtime | fix: bind wake origin to committed input identity |
| `1fba13b` | 10-23 09:30 | 固件/设备 | fix: recover firmware wake transport failures safely |
| `66c0788` | 10-23 09:39 | 文档/测试/配置资料 | docs: record wake recovery deployment and verified firmware candidates |
| `97145c7` | 10-23 09:59 | 其他实现 | test: trace resume state and compare voice framework interruption histories |
| `1717bb0` | 10-23 10:44 | 其他实现 | test: add repeated-resume and delayed-final native scenarios |
| `f024bec` | 10-23 10:45 | 文档/测试/配置资料 | docs: explain repeated-resume probe acceptance boundaries |
| `9c066eb` | 10-23 10:49 | 旧Go网关/独立Realtime | fix: synchronize revised paused tasks before native generation |
| `071a23f` | 10-23 10:56 | 旧Go网关/独立Realtime | fix: retain original tasks through delivered clarifications |
| `6a1dd70` | 10-23 10:56 | 其他实现 | Evaluate official forced alignment with conservative boundary validation |
| `8ee67d0` | 10-23 11:14 | 其他实现 | test: reject unbound resume despite successful voice transport |
| `cd625f4` | 10-23 11:17 | 文档/测试/配置资料 | docs: record native routing and tool enforcement failures |
| `4e785d6` | 10-23 11:18 | 旧Go网关/独立Realtime | fix: require paused task routing before voice generation |
| `1bdc325` | 10-23 11:24 | 旧Go网关/独立Realtime | fix: fail closed on routing context contamination |
| `fd01a18` | 10-23 11:34 | 文档/测试/配置资料 | docs: record native routing failures and isolated classifier plan |
| `44357b0` | 10-23 11:42 | 旧Go网关/独立Realtime | fix: isolate paused task classification from voice sessions |
| `5ee8d4b` | 10-23 11:48 | 旧Go网关/独立Realtime | fix: cancel classifier connections during websocket upgrade |
| `c7bd0a2` | 10-23 11:58 | 旧Go网关/独立Realtime | fix: scope snapshot acknowledgements to committed input |
| `d210811` | 10-23 12:03 | 旧Go网关/独立Realtime | docs: clarify retired input snapshot acknowledgement order |
| `ce28a22` | 10-23 12:21 | 其他实现 | test: add native clarification and user choice audit flow |
| `9840416` | 10-23 12:23 | 其他实现 | test: verify unknown position clarification before user choice |
| `2aaf3de` | 10-23 12:29 | 旧Go网关/独立Realtime | Guard unknown-position resume with validated native clarification |
| `07c6549` | 10-23 12:36 | 旧Go网关/独立Realtime | Close replaced clarification playback and emit guard traces |
| `075a432` | 10-23 12:47 | 文档/测试/配置资料 | docs: record verified resume fallback and gateway release |
| `7a9310b` | 10-23 12:48 | 文档/测试/配置资料 | docs: close verified main release checklist |
| `c3fd2c0` | 10-23 12:57 | 文档/测试/配置资料 | docs: record physical barge-in and chosen-start feedback |
| `f0f1870` | 10-23 13:04 | 文档/测试/配置资料 | docs: reassess automatic continuation against official implementations |
| `8f9b312` | 10-23 13:12 | 文档/测试/配置资料 | docs: add conversation records and bounded context recovery to proposal |
| `5dbbf67` | 10-23 13:20 | 文档/测试/配置资料 | docs: plan bounded conversation records and recovery |
| `585372a` | 10-23 13:24 | 其他实现 | feat(recordstore): add bounded asynchronous snapshot persistence |
| `7cb958e` | 10-23 13:27 | 其他实现 | fix(recordstore): close store descriptors on exec |
| `ce51efa` | 10-23 13:45 | 旧Go网关/独立Realtime | feat: persist safe per-device conversation records |
| `8fc16a3` | 10-23 13:47 | 文档/测试/配置资料 | docs: compare alignment and output ASR on interrupted audio |
| `5772dfb` | 10-23 14:06 | 旧Go网关/独立Realtime | fix: isolate cleared native context and expire idle records |
| `b813e53` | 10-23 14:10 | 文档/测试/配置资料 | docs: record native conversation clear and recovery validation |
| `040ce07` | 10-23 14:18 | 文档/测试/配置资料 | docs: close record recovery runtime and deployment checks |
| `66be88c` | 10-23 17:40 | 文档/测试/配置资料 | docs: propose checkpointed story and counting continuation |
| `6ae527c` | 10-24 09:21 | 文档/测试/配置资料 | docs: review OpenDeskBot playback and interruption mechanisms |
| `ca22d0c` | 10-24 09:31 | 文档/测试/配置资料 | docs: detail server-side LiveKit architecture and first integration gate |
| `6ff74ba` | 10-24 09:44 | 文档/测试/配置资料 | docs: define LiveKit task ownership and continuation contracts |
| `e3e2559` | 10-24 09:53 | 其他实现 | docs: preserve local development history and cross-machine handoff |
| `7d3c43f` | 10-24 10:06 | 其他实现 | test: validate LiveKit playback receipt contract on server |
| `dbb9257` | 10-24 10:07 | 文档/测试/配置资料 | docs: record completed server SDK audit and next gates |
| `800795a` | 10-24 10:09 | 文档/测试/配置资料 | docs: verify synchronized server checkout |
| `bc51a27` | 10-24 10:31 | 其他实现 | feat: add audited DashScope adapters for LiveKit voice chain |
| `f820348` | 10-24 10:39 | 其他实现 | feat: connect LiveKit sessions to bounded PCM and cancellation |
| `93b0c4f` | 10-24 10:52 | 其他实现 | feat: run isolated server voice chain with repeatable live smoke tests |
| `5072c57` | 10-24 11:00 | 其他实现 | fix: retire cancelled PCM input before accepting another turn |
| `8df1b44` | 10-24 11:02 | 文档/测试/配置资料 | docs: record verified server voice chain and Mac client next step |
| `64af6a2` | 10-24 11:18 | 其他实现 | feat: add Mac browser microphone and playback client over SSH |
| `0fa2774` | 10-24 11:20 | 文档/测试/配置资料 | docs: record Mac voice test evidence and pending acoustic acceptance |
| `e7bdbaf` | 10-24 11:31 | 其他实现 | fix: expose voice session expiry and preserve a usable Mac test window |
| `f5aa9a6` | 10-24 11:54 | 其他实现 | feat: enable single-wake continuous voice and automatic interruption |
| `38093fb` | 10-24 12:07 | 其他实现 | fix: resume counting from confirmed playback checkpoint |
| `bab7e48` | 10-24 12:28 | 其他实现 | fix: route natural counting resumes through task checkpoints |
| `6a71806` | 10-24 12:43 | 文档/测试/配置资料 | docs: design story and answer playback checkpoints |
| `ddb6515` | 10-24 12:57 | 文档/测试/配置资料 | docs: define drain-first voice handoff and device testing |
| `cd59b0d` | 10-24 13:03 | 文档/测试/配置资料 | docs: design bounded low-latency voice handoff |
| `25ee76a` | 10-24 13:46 | 其他实现 | feat: preserve voice originals across drain interruptions |
| `391c4d9` | 10-24 13:47 | Mac客户端/共享音频 | feat: add native Opus device client and drain gateway |
| `1981c70` | 10-24 13:47 | 文档/测试/配置资料 | docs: record native drain handoff validation and limits |
| `2b507f5` | 10-24 14:14 | Mac客户端/共享音频 | fix: preserve native microphone audio with voice processing |
| `5ef4b3d` | 10-24 14:37 | Mac客户端/共享音频 | feat: keep native voice testing open until manual exit |
| `68d5c90` | 10-24 14:38 | 文档/测试/配置资料 | docs: record keep-open live test limitations |
| `7f988b7` | 10-24 14:50 | Mac客户端/共享音频 | fix: recover voice replies and rapid native reconnects |
| `06bc026` | 10-24 14:54 | 文档/测试/配置资料 | docs: add observed microphone reply and playback evidence |
| `5baff9c` | 10-24 15:07 | Mac客户端/共享音频 | fix: resume failed speech without replaying confirmed prefix |
| `3e51117` | 10-24 15:38 | Mac客户端/共享音频 | feat: persist private voice conversation text on the server |
| `37895dc` | 10-24 17:44 | 其他实现 | feat: connect voice archives to model-led dialogue |
| `69e7e90` | 10-24 19:29 | 其他实现 | fix: bound voice history queries and reduce reply latency |
| `fe364eb` | 10-24 19:53 | 其他实现 | fix: split oversized voice replies and preserve prefetched playback |
| `fffebab` | 10-24 20:14 | 文档/测试/配置资料 | docs: plan parallel model-authored voice confirmation |
| `03f486b` | 10-24 20:42 | 其他实现 | feat: add parallel model-authored voice preparation |
| `ca270b7` | 10-24 20:53 | 文档/测试/配置资料 | docs: record dual backend design and deferred speaker identity |
| `6705674` | 10-24 21:11 | 旧Go网关/独立Realtime | feat: add isolated Mac entrance to existing Realtime gateway |
| `ab9fe31` | 10-24 21:14 | Mac客户端/共享音频 | feat: add standalone Xiaozhi Realtime native test client |
| `c096c29` | 10-24 21:21 | Mac客户端/共享音频 | fix: wait for native Realtime tail before starting next reply |
| `a3b1685` | 10-24 21:21 | 文档/测试/配置资料 | docs: record isolated Realtime implementation and validation |
| `256c429` | 10-24 21:21 | 文档/测试/配置资料 | docs: mark independent Realtime delivery checks complete |
| `46bd2d1` | 10-24 22:41 | 其他实现 | test: probe Realtime spoken-prefix recovery with output ASR |
| `96da738` | 10-24 22:41 | 文档/测试/配置资料 | docs: design Realtime task and specialist model collaboration |
| `976a901` | 10-24 23:02 | Mac客户端/共享音频、Python语音前身/验证脚本 | feat: add isolated live output-ASR playback recovery experiment |
| `94e2370` | 10-25 10:04 | 文档/测试/配置资料 | docs: record and defer Xiaoman product identity feature |
| `5edaf6b` | 10-25 10:06 | 文档/测试/配置资料 | docs: distinguish realtime cancellation from played-audio history truncation |
| `f68a31e` | 10-25 10:39 | Mac客户端/共享音频、Python语音前身/验证脚本 | feat: drain received realtime audio before reply handoff |
| `3c774f4` | 10-25 11:04 | Python语音前身/验证脚本 | fix: retain played realtime history and guard recall claims |
| `cdfa6fc` | 10-25 11:40 | Python语音前身/验证脚本 | fix: stream realtime replies without semantic veto |
| `513dbd7` | 10-25 11:43 | 文档/测试/配置资料 | docs: record natural dialogue deployment and playback checks |
| `67b821d` | 10-25 11:47 | 文档/测试/配置资料 | docs: scope Realtime homework collaboration requirements |
| `9e7e468` | 10-25 11:57 | Python语音前身/验证脚本 | feat: add Xiaoman identity to realtime test entry |
| `7cfa203` | 10-25 12:00 | 文档/测试/配置资料 | docs: record Xiaoman identity deployment and voice checks |
| `8e1111b` | 10-25 12:03 | 文档/测试/配置资料 | docs: plan Go unification after Xiaoman identity delivery |
| `dc3f28d` | 10-25 12:21 | Python语音前身/验证脚本 | feat: enable native realtime search with source evidence |
| `3b89df8` | 10-25 12:24 | 文档/测试/配置资料 | docs: record native search deployment and playback verification |
| `5417671` | 10-25 12:34 | 文档/测试/配置资料 | docs: tighten Go migration to full Realtime behavior parity |
| `a0a743b` | 10-25 12:48 | 当前Go语音核心/上下文 | feat: port realtime audio and played context with Python parity tests |
| `636ac95` | 10-25 12:51 | Mac客户端/共享音频、当前Go语音核心/上下文 | feat: add independent Go realtime live session with drain parity |
| `767d340` | 10-25 12:59 | Mac客户端/共享音频、当前Go语音核心/上下文 | fix: distinguish orderly realtime shutdown and document migration checks |
| `b6f6ebd` | 10-25 13:07 | Mac客户端/共享音频、当前Go语音核心/上下文 | fix: preserve terminal transport closure evidence during realtime cleanup |
| `818189a` | 10-25 13:26 | 文档/测试/配置资料 | test: synchronize gateway clear fixtures without retry polling |
| `d189821` | 10-25 13:36 | Mac客户端/共享音频 | feat: label Go and Python comparison windows and record migration validation |
| `4240bd2` | 10-25 14:05 | Mac客户端/共享音频 | feat: make Go the default realtime entry after comparison acceptance |
| `fa29800` | 10-25 14:25 | Python语音前身/验证脚本 | docs: deprecate legacy Python voice paths and cascade entry points |
| `c9050e0` | 10-25 14:54 | Mac客户端/共享音频、当前Go语音核心/上下文 | feat: bind realtime terminals, sessions and private records |
| `d83fd7f` | 10-25 14:58 | 文档/测试/配置资料 | docs: record terminal ownership validation and handoff |
| `5f2d0db` | 10-25 16:19 | 当前Go语音核心/上下文 | feat: guide complete stories and direct spoken continuation |
| `f65e19b` | 10-25 16:23 | 当前Go语音核心/上下文 | fix: keep story continuation grounded without sample plots |
| `5e6a181` | 10-25 16:26 | 文档/测试/配置资料 | docs: record story prompt verification and remaining limits |
| `a5a4e54` | 10-25 17:00 | 当前Go语音核心/上下文 | feat: add authenticated Xiaozhi entry to shared Go Realtime core |
| `b5c58e0` | 10-25 17:04 | 固件/设备 | feat: add Xiaoman wake variant and preserve queued device playback |
| `3f530fa` | 10-25 17:11 | 固件/设备 | fix: align Xiaoman build configuration with upstream validation |
| `34d0b03` | 10-25 17:14 | 文档/测试/配置资料 | docs: record Xiaozhi integration checks and pending hardware gates |
| `54ec169` | 10-25 22:44 | 固件/设备 | fix: start Xiaoman realtime capture after fresh AEC readiness |
| `1618e13` | 10-25 23:02 | 文档/测试/配置资料 | docs: define Xiaoman watch fluid orb interface |
| `bab92d6` | 10-25 23:03 | 文档/测试/配置资料 | docs: record Xiaoman reboot and Wi-Fi field acceptance |
| `2c234f0` | 10-25 23:40 | 当前Go语音核心/上下文 | chore: preserve rejected watch playback evidence and recovery status |
| `455c633` | 10-25 23:52 | 文档/测试/配置资料 | docs: pause audio fault investigation and plan Xiaoman orb UI |
| `f3e5111` | 10-26 00:04 | 固件/设备 | feat: add Xiaoman fluid orb and truthful voice status display |
| `46ae702` | 10-26 00:13 | 固件/设备 | fix: preserve notification and preview ownership in orb view |
| `2d306a8` | 10-26 00:40 | 固件/设备 | perf: reduce orb scaling cost and recover UI refresh rate |
| `222ea5f` | 10-26 01:00 | 固件/设备 | fix: keep orb status bar transparent across theme changes |
| `04e0c7c` | 10-26 01:13 | 固件/设备 | fix: convert LVGL selectors before combining enum flags |
| `097d17a` | 10-26 01:23 | 文档/测试/配置资料 | docs: record quiet orb validation and pending final device install |
| `700fb13` | 10-26 09:20 | 文档/测试/配置资料 | docs: record orb installation and remaining motion quality gap |
| `3ddbbb5` | 10-26 09:59 | 固件/设备 | fix: stabilize Xiaoman orb layout motion and audio status |
| `26a2b44` | 10-26 10:27 | 文档/测试/配置资料 | docs: record orb fix installation and device validation limits |
| `dcf345c` | 10-26 22:06 | 文档/测试/配置资料 | docs: record orb feedback and stronger motion preview candidates |
| `e61f079` | 10-26 22:53 | 固件/设备 | feat: add approved soft fluid motion for Xiaoman watch |
| `9e932ac` | 10-26 23:27 | 文档/测试/配置资料 | docs: record soft fluid firmware installation and device boundaries |
| `dc9e8bc` | 10-26 23:33 | 文档/测试/配置资料 | docs: record initial live orb rendering observations |
| `657e408` | 10-26 23:41 | 文档/测试/配置资料 | docs: retain current orb motion and note next integration discussion |
| `518e23e` | 10-27 00:25 | 文档/测试/配置资料 | docs: define dual-device integration and server implementation plan |
| `6dc191a` | 10-27 00:49 | 当前Go语音核心/上下文、旧Go网关/独立Realtime | feat: isolate authenticated realtime device sessions |
| `518f84e` | 10-27 01:01 | 文档/测试/配置资料 | docs: record multi-device deployment and silent firmware plan |
| `e2cab21` | 10-27 02:07 | 固件/设备 | feat: add dedicated Waveshare Xiaoman realtime firmware |
| `50f21a2` | 10-27 02:20 | 固件/设备 | fix: include complete font type for Waveshare display |
| `411c26d` | 10-27 02:41 | 固件/设备 | fix: load Waveshare SDK config and match release evidence names |
| `48c0f28` | 10-27 02:56 | 文档/测试/配置资料 | docs: finalize silent dual-device build and verification |
| `20e9fcd` | 10-27 09:25 | 固件/设备 | chore(firmware): diagnose Waveshare capture startup faults |
| `af4a654` | 10-27 09:36 | 文档/测试/配置资料 | docs: record Waveshare install and pending capture diagnosis |
| `ee6e15e` | 10-27 13:23 | 固件/设备 | chore(firmware): trace blocked Waveshare input reads |
| `2978a1a` | 10-27 13:55 | 固件/设备 | chore(firmware): trace Waveshare capture pipeline boundaries |
| `d685ab0` | 10-27 14:20 | 文档/测试/配置资料 | docs: confirm Waveshare voice test and correct AFE diagnosis |
| `5169576` | 10-27 14:21 | 文档/测试/配置资料 | docs: correct aggregate playback receipt count |
| `f0cf2cd` | 10-27 17:55 | 文档/测试/配置资料 | docs: attribute Waveshare session endings to user controls |
| `05aa79b` | 10-27 18:14 | 文档/测试/配置资料 | docs: clarify Waveshare standby and listening wake behavior |
| `2f84064` | 10-27 18:33 | 文档/测试/配置资料 | docs: correct Waveshare long-idle power-state diagnosis |
| `14ed738` | 10-27 23:41 | 文档/测试/配置资料 | docs: plan retirement of legacy Python and cascade voice paths |
| `bda65e2` | 10-28 00:27 | 当前Go语音核心/上下文 | feat(realtime): add verified current-device voice controls |
| `ff124ef` | 10-28 00:32 | 文档/测试/配置资料 | docs: record device-controls deployment and validation |
| `f092346` | 10-28 13:03 | 文档/测试/配置资料 | docs: retain cascade as an optional future voice backend |
| `a669f80` | 10-28 23:44 | 当前Go语音核心/上下文 | feat(realtime): restore recent conversation memory per device |
| `3565c97` | 10-28 23:48 | 文档/测试/配置资料 | docs: record device memory deployment and acceptance boundaries |
| `09d22e6` | 10-29 22:58 | 当前Go语音核心/上下文 | feat(realtime): add bounded per-device cloud audio diagnostics |
| `4d32188` | 10-29 23:02 | 文档/测试/配置资料 | docs: record cloud audio diagnostic deployment and test boundary |
| `aecfd80` | 10-30 12:16 | 文档/测试/配置资料 | docs: record first cloud audio diagnostic reproduction |
| `f454c1b` | 10-30 12:18 | 文档/测试/配置资料 | docs: classify quiet-room truncation as false barge-in |
| `d7a0a95` | 10-30 12:30 | 文档/测试/配置资料 | docs: record Mac playback feedback reproduction |
| `066b99f` | 10-30 12:34 | 文档/测试/配置资料 | docs: rule out repeated wake-word handling on Mac |
| `86eee32` | 10-30 13:12 | 文档/测试/配置资料 | docs: record isolated Realtime version comparison results |
| `c7cd01f` | 10-30 13:26 | 文档/测试/配置资料 | docs: record recurring first-playback interruption evidence |
| `851a961` | 10-30 13:59 | Mac客户端/共享音频 | feat: add opt-in Mac first-playback audio diagnostics |
| `0287651` | 10-30 19:12 | 文档/测试/配置资料 | docs: record two first-playback echo diagnostic sessions |
| `76d7f89` | 10-30 19:37 | Mac客户端/共享音频 | feat: add opt-in playback and uplink correlation diagnostics |
| `d475b41` | 10-31 07:57 | Mac客户端/共享音频 | fix: separate source clocks from SRC delivery in audio diagnostics |
| `a8f605e` | 10-31 08:33 | 文档/测试/配置资料 | docs: record failed echo experiments and client rollback |
