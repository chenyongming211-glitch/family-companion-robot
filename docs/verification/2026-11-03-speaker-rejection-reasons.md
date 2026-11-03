# Realtime 匿名说话人拒识诊断

日期：2026-11-03（Asia/Shanghai）。诊断源码与试点构建：`bd4320c`。

**当前结论：单人三句的现场结果为“访客 A、不确定、不确定”，尚未通过正常聊天中的单人一致性验收。已补齐仅内存的拒识原因并部署隔离试点；新诊断版尚无真人原因结果，不能宣称识别问题已修复。**

前阶段的接线、入口过期与恢复见[聊天观测试点报告](2026-11-03-realtime-speaker-observation.md)。本轮按[拒识诊断计划](../superpowers/plans/2026-11-03-speaker-rejection-reasons.md)执行，没有更换模型或调整识别阈值。

## 1. 已取得的真人证据

用户确认受控这一轮只有自己说话；每次在回答后保留连接，由代理逐句读取 Mac 窗口，避免依赖事后记忆。结果如下：

| 次序 | 指定测试问题 | 窗口读取 |
|---|---|---|
| 1 | 小满，给我推荐一种简单的运动。 | 访客 A（候选） |
| 2 | 每天在家运动多久比较合适？ | 不确定 |
| 3 | 小满，用一句话告诉我运动前怎么热身。 | 不确定 |

第三句读取后已结束收音。此前有电视或其他人声的会话单独保留为环境不受控观察，不作为本轮单人基线。

“不确定”是拒绝给出可靠人物标签，不等于已识别成另一个人。旧事件将片段边界、长度、能量和特征匹配等不同拒识情况合并显示，且没有保留具体原因、原始音频或特征；无法追溯这两句各自在哪一层被拒绝，也不能据此认定是匹配阈值、回声或多人重叠导致。

## 2. 本轮实际改动

只增加固定 `reason` 和片段总时长 `audio_ms` 的传递与展示。模型仍为云端 sherpa-onnx／3D-Speaker ERes2Net，原模型摘要、识别阈值、VAD、AEC、队列、识别窗口与临时身份策略保持。

| 原因码 | 表示的拒识或候选阶段 |
|---|---|
| `candidate` | 临时人物候选 |
| `incomplete_span` | 源采样边界或完整片段未确认 |
| `too_short` | 总片段不足 1.5 秒 |
| `too_long` | 总片段超过 8 秒 |
| `low_energy` | 未通过原有 RMS 能量门禁 |
| `match_uncertain` | Tracker 未给出可靠匹配 |
| `capacity` | 临时访客容量已满 |
| `not_queued` | 本段未被旁路队列接纳 |
| `observer_unavailable` | 识别旁路不可用 |

`audio_ms` 是所持 PCM 的总片段时长，**不是有效人声长度，也不是识别耗时**；边界未知时可能为 0。时长与原因只能用于定位拒识阶段，不能作为人物准确率的证明。

- lab 只在 `--realtime-test` 的 observation 增加原因；原手动采样 observation 严格保留 5 字段，ready/closed 保留 2 字段。
- Go 桥兼容旧 5 字段和新 6 字段；严格校验原因枚举及状态/标签一致性，拒绝未知、重复、null 和错误类型。
- Session 补齐自身拒识原因与长度，保持永久不可用优先和迟到结果保护。完整但超长的输入不再被诊断成“边界缺失／0ms”；仍在排队、复制与推理前拒绝超过 8 秒的片段。
- Mac 兼容旧事件，改为显示具体解释和片段长度；删除“不确定（请轮流说话）”这一可能被误解为已检测重叠的通用提示。
- 只传固定枚举及长度；不新增原始音频、向量、分数、转写、任意错误字符串或结果日志。临时身份不注入模型，不登记姓名或建立声纹库；普通 Realtime 回答继续与识别并行。

## 3. 自动验证范围

Go、ONNX 和真实模型运行均在云端。以下为诊断源码的专项验证，不能把前阶段全仓 986 节点 race 结果当成本轮重新执行的全仓回归。

| 检查 | 实际结果 |
|---|---|
| 根模块受影响包最终 race | 411 测试节点通过、2 项既有可选测试跳过、0 失败；覆盖 speaker、speakerlive、realtimelive |
| 根模块 vet / build | 均 exit 0 |
| 独立 lab 普通 / race | 各 134 节点通过、5 项原生 opt-in 默认跳过、0 失败；vet/build 通过 |
| 另行执行 ERes2Net 原生测试 | 5 项通过、0 跳过 |
| Mac SDK 解析 | 新增 149 项原因/长度断言及原有断言通过；无麦克风参与 |
| Mac SDK 构建 | 普通、试点、soak 构建及签名、plist、脚本检查通过 |
| 源码绑定 | 11 个变化文件与云端测试源摘要一致 |
| 独立质量复核 | 诊断改动 PASS，无剩余 P1/P2；不覆盖真人准确率或听感 |

先执行协议、Session 及客户端失败断言，再实现原因字段。完整超长片段的诊断错误亦有独立 RED→GREEN 证据；没有为获得候选而放宽识别门禁。

## 4. 真实模型与合成输入联调

使用既有锁定合成 WAV，经实际 Opus 上行、千问 Realtime 和 ERes2Net；没有使用真人麦克风或新录音。本轮单次执行、没有挑选重试结果：

| 合成输入 | input_id | 候选 | reason | audio_ms |
|---|---:|---|---|---:|
| A1 | 1 | guest_1 | candidate | 4120 |
| A2 | 2 | guest_1 | candidate | 3980 |
| B1 | 4 | guest_2 | candidate | 2560 |
| A3 | 5 | guest_1 | candidate | 3840 |

开启组 4 次回答完整模拟播放；关闭组 1 次回答完整模拟播放、0 条说话人事件。开启组另有 input_id=3 的 pending，没有最终候选；其原因未确认。播放回报来自立即模拟，明确没有 CoreAudio、扬声器或实体设备。

这证明新字段能贯穿链路，不能代替单人真人复验或多人 A→B→A 验收。前一构建的合成 B 曾拒识，本轮得到 B 候选；模型和参数没有改变，分段可能变化，不能把两次差异称为准确率改善或修复。

## 5. 部署与产品保护

新私有根：`/opt/family-robot-validation/speaker-reasons-20261103-a65f1fc/`。`source/` 与 `evidence/` 保存测试源和二进制；独立 lab 源与证据在 `lab-source/`、`lab-evidence/`。原试点文件保留作回退。

两个隔离单元仍为 `family-robot-speaker-pilot-on.service`（回环 18052）及 `family-robot-speaker-pilot-off.service`（回环 18053），在 active0 时成对替换 server/helper。两端 health 均为 ready、build=`bd4320c`，开启/关闭状态正确，`device_memory=false`、`diagnostics_active=false`；探针清理后 active0，文档收尾时复查仍为 active0。

为保留客户端配对，元数据仍使用原 `speaker-live-20261103-050464b/records-on` 和 `records-off`，没有改成新空目录。扫描仅有 `devices.json` 与 `session.json` 等允许的认证/会话元数据，没有正文或意图文件。

累计运行上限仍显式为 `RuntimeMaxSec=infinity`；CPUQuota100%、MemoryMax768MiB、TasksMax96、LimitCORE0、NoNewPrivileges、PrivateTmp、ProtectSystem=strict 保留。识别仍为单会话 5 分钟／32 段，连接最长 15 分钟。单元是临时部署，服务器重启后需重新核验。

部署前后产品保护快照一致：正式 Go 仍 `58c14cd`、PID1304344，网关 PID4139310，NRestarts 均 0；两二进制、两 env 及产品身份共 5 项摘要保持。没有修改原产品入口、跨会话记忆、固件、设备网络、串口或音频控制。

## 6. 待完成的针对性真人核验

当前 Mac 锁屏，规范诊断入口尚未重新打开核对。此前没有参数的空闲 App 被识别为“未核对入口”，不应当作有效测试窗口；解锁后须先关闭它，再通过规范启动器打开“小满 · Realtime 声纹聊天测试”，核实 18052/18053 与开关，代理不自动开启麦克风。

下一轮须在**同一新连接**中先用第一句建立临时 A，再测曾拒识的第二句，每句读取原因后再决定下一步；新连接的首个合格样本会建立 A，只把第二句放到新会话首句不能验证已有模板的匹配。无需再盲目重复整套三句。

待取得 `reason/audio_ms` 后，才沿相应的边界、质量或 Tracker 路径继续调查。多人、重叠、儿童、距离/噪声、实体设备、开关流畅度、长稳及持久登记均未验收。

## 7. 私有证据索引

新根 `evidence/`：`final-affected-race.jsonl`、`final-affected-race.stderr`、`final-vet.txt`、`final-build.txt`、`source.sha256`、`source-verify.txt`、`binaries.sha256`、`reason-protocol-red.txt`、`session-reason-red.txt`、`long-reason-{red,green}.txt`、`pilot-probe-both.json`、`pilot-probe-cleanup-health.json`、`protection-{before,after}.json`。

`lab-evidence/`：`summary.json`、`red-reasons.log`、`green-{test,race,vet,build}.log`、`real-eres2net.log`、`model-verification.log`、`source.sha256`。报告只人工汇总匿名标签和指定问题，不附真实音频、声纹向量、分数、正文或认证材料。
