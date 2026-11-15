# 固定 TTS 微雪播放核对

**新增：** 用户已要求A/B/C三链路共同对比，当前新增资产为`abc_comparison_test.go.txt`，见本文末节及[三链路计划](../../docs/superpowers/plans/2026-11-15-abc-waveshare-comparison.md)。固定TTS和规划并行资产保留为独立历史验证，不将它们的结果称作三链路对比。

这是显式启用的云端验证资产，不是产品后端或自动测试中的发声入口。仅支持微雪 ESP32-S3 2.16；不改固件或正式服务。关联[实施计划](../../docs/superpowers/plans/2026-11-15-fixed-tts-waveshare-playback.md)。

`fixed_tts_test.go.txt` 默认不参与构建。仅在独立云端源码显式复制为 `internal/realtimelive/fixed_tts_pilot_test.go` 后测试；Mac不运行。普通测试使用合成fixture，Serve默认skip，只有精确环境值才可启用现场入口。服务器凭据、固定源PCM、完整结果和设备记录均不进入Git。

现场版本通过 `Server.runDevice` / `Session.generate` 播放已验证的公开合成TTS，复用公共鉴权、Opus、流控、中断和I2S回执；没有新主回答或TTS调用。保留输入监听和OutputASR，辅助API仍有费用，不将本测试当作成品省钱效果。

保护包括：仅一个指定MAC/鉴权owner、Opus、单会话，持久账本跨连接最多两次播放，先fsync intent再emit，失败/未知不重播；会话120秒、服务6分钟，退出CloseActive。固定输入及SHA仅指向云端本轮验证源；不能通过替换文件重用旧intent。

结束时记录 `tts_playback_alignment`：从公共turn冻结已播/下发样本、cut/ended/final及证据来源，减实际encoder lookahead，再用120ms余量映射TTS字词。完整硬件终态且覆盖源才确认全文，其他尾部保持unknown；这是只读审计，不修改产品上下文或记忆。内部回执不能独自证明物理逐字精度。

云端保护检查（默认无提供方/设备调用）：

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go test -count=1 ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio
/opt/family-robot/toolchains/go1.27.3/bin/go test -race -count=1 ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio
/opt/family-robot/toolchains/go1.27.3/bin/go vet ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio
```

现场操作必须先告知用户，并完成新一轮exact-route准备、源/二进制证明与自动清理。复用云端 `native_route_guard_v4.py`（SHA `cdb9438c4be936834a95238005fcc18f5e2b80e9bb33114609fe55f2ebd48f8b`）的原子apply/restore；仅更换本阶段临时unit及health契约，不重用既有apply intent。不可直接运行历史路由命令。结束后关闭临时实例、核对Caddy file/admin和正式语音/管理基线，外部配置漂移时拒绝覆盖。

用户操作：唤醒说“开始”，第一次完整听完；再说“再来一次”，第二次听到中段时说“停一下”。固定内容为“早上好！今天天气不错，适合出门走走。”，约5.89秒。记录声音是否连续、有无截尾及实际停止位置。本阶段不验收连续A/B交流或自动续讲。

## 起播规划并行候选

`parallel_planning_test.go.txt`是独立现场资产，显式复制至云端`internal/realtimelive/parallel_planning_pilot_test.go`后检查。`TestParallelPlanningPilotServe`仅在`FAMILY_PARALLEL_PLANNING_PILOT_ALLOW=waveshare-parallel-planning-20261115`时开放loopback 18818；普通检查只运行合成保护测试，Serve跳过。见[验证状态](../../docs/verification/2026-11-15-voice-startup-control-candidate.md)。

复用当前主模型、公共Session/Opus/MCP/表情/在线音乐，启用默认关闭的ParallelPlanning；指定微雪和鉴权owner，单并发，每会话180秒、服务10分钟，memory=false。沿用全局账本，本阶段最多4个主回答，未知/失败保留。辅助规划、监听和控制也可能调用API，4不是所有API或硬件动作的总上限；并行本身不保证省费。

先完成云端普通/race/vet/build、源/二进制冻结和fresh路由准备；用户在场且入口就绪后按四轮测试：自我介绍、音量调到80、查询当前音量、短故事。第四轮结束后停止，不追加第五条命令。控制器限时并在第四个主回答播放结算后停止自身unit、guarded restore，核对正式进程和Caddy file/admin。2026-11-15 14:02–14:03已完成此独立候选四轮，用户反馈整体正常、四轮完整played；入口已恢复，旧音量超时根因未宣称修复，见[现场记录](../../docs/verification/2026-11-15-voice-startup-control-candidate.md)。该C带辅助功能样本不代替新ABC统一条件对比。

## A/B/C 三链路对比

最新build为`abc-comparison-20261115-v3`：数字口语化或标点分合导致词索引不再对应原稿时，保留严格协议/音频/终态检查，使用显式完整TTS任务PCM边界并标`task_boundary`，不伪造Words或数字内部断点。源字段和样本范围须通过校验；公共OutputASR历史保持，未增加辅助API调用。三份真实失败元数据已静默重放，976普通/976竞态检查通过，完整故事仍待新设备样本。B-v5已恢复，B-v6只prepare；旧待机与C漏拦仍未解决。

`abc_comparison_test.go.txt`显式复制到云端`internal/realtimelive/abc_comparison_pilot_test.go`后检查。`TestABCComparisonPilotServe`仅在`FAMILY_ABC_PILOT_ALLOW=waveshare-abc-20261115`且`FAMILY_ABC_PILOT_ROUTE=A|B|C`时运行，普通检查默认skip。每次会话固定一路，失败不回退C：A为Omni原音理解→文字→专用TTS，B为Fun-ASR-Realtime→Qwen Plus非思考→同一TTS，C为原生直接语音。A/B实际TTS为cosyvoice-v3-flash/longanyang，C使用原配置音色。

复用公共微雪鉴权、Opus、输入监听、OutputASR、流控和中断；辅助工具/音乐/表情/搜索/声纹/跨会话记忆首轮统一关闭。A/B当前完整收齐并校验后播放，C保持流式，元数据明确buffered/native_streaming，初轮不当作最终流式延迟排名。TTS字词只读审计，不替换公共已播历史。

目标每路线4个人工问题；安全上限为每路线12次主回答、阶段36、原全局账本累计74，旧48默认工具不改。首轮历史primary49（原38+A4+B3+C4）及失败/未知全部保留，不自动跑满额度。每答可能包含多次TTS分句调用，另有公共输入/输出识别；B的主链路ASR与公共监听是两项调用，不能漏算或合并。音频最长10秒、回答120字、输出30秒；单并发、会话480秒、实例10分钟，仅指定MAC/owner/Opus/loopback18818。

源/二进制和规格/质量复核通过后才启服务。按路线分别通知用户，再fresh路由apply；v3取消“4次模型调用自动结束”，用户文字确认完成人工问题后停止本轮unit并guarded restore，另保留9分钟窗口保护，未准备下一条时不自动切换。首轮A播放正常但偏好问题分段；B因`～`句界失败已最小修复待实测；C自身回声误打断，仅已补私有诊断、尚未修复。三条各自的真实结果、失败、用量、用户反馈和恢复必须分开记录，见[当前状态](../../docs/verification/2026-11-15-abc-waveshare-comparison.md)。
