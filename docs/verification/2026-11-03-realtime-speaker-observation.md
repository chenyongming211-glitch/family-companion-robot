# 正常Realtime聊天中的匿名说话人观测试点

日期：2026-11-03（Asia/Shanghai）。代码：`6b5c63d`；状态：**入口过期原因已确认并恢复；现场已建立会话并返回候选，识别准确性及听感待用户反馈。**

用户已明确选择正常与小满聊天时同步识别，而非每句点击采样。本轮落实[已确认的识别规则](../superpowers/specs/2026-11-01-go-realtime-speaker-identity-design.md#81-单人多人场景的识别触发规则2026-11-02用户确认)，按[实施计划](../superpowers/plans/2026-11-03-realtime-speaker-observation.md)执行。

## 1. 实际交付

- 沿用原Realtime语音、VAD、AEC、Opus与播放回报；客户端点一次“开始对话”即可正常聊天。
- 每个新发言退休旧候选；话尾对有可靠源采样坐标、完整PCM且1.5～8秒的片段进行异步观察。推理不在音频上行锁内，普通回答不等待标签。
- 独立云端Go桥通过私有管道调用既有Linux sherpa-onnx／ERes2Net；每会话最多1运行＋1排队。模型、音频、临时向量均留云端受控处理；Mac不运行后端或复制模型API Key。
- Mac独立窗口显示“访客A（候选）／不确定／不可用”，可在开始前切换本会话的识别开关。连接中开关禁用，结束后可切换再开，标签重新编号。
- 这仍是受控、轮流发言的匿名观测：候选不注入模型、不决定权限，不登记姓名、声纹库或个人记忆。没有可靠分段/重叠检测，不能把一次VAD等同于确定单人。

## 2. 留存和资源边界

两个试点均固定`MemoryOnly=true`、`MemoryEnabled=false`、关闭音频诊断。原有设备记忆不读取、不回写；recorder以及Native/ASR意图写入均阻断，聊天正文、音频、特征和候选不写文件。只允许私有设备认证元数据、会话ID、时间与关闭原因。

本轮真实模型合成联调后检查：开启和关闭目录各2份元数据文件，仅`devices.json`和`session.json`，没有正文或意图文件。客户端默认不收音，试点清除trace/correlation配置，App输出丢弃；程序不承诺可证明擦除所有运行时／内核副本或改变系统swap。

识别上限5分钟／32片段；失效后不悄悄重建Tracker，显示不可用、普通聊天继续。单次helper请求3秒、启动15秒的上限在后台执行。每片段仍新建受控模型子进程，因此约0.6～0.7秒的观测墙钟耗时包含冷启动，不能当作产品最终推理指标。

整场测试连接最长15分钟；两个服务使用相同CPUQuota=100%、MemoryMax=768MiB、TasksMax=96、LimitCORE=0及私有临时目录。最初部署的服务级12小时上限已因等待测试期间到期而取消，当前明确设置`RuntimeMaxSec=infinity`；每次识别及连接的上述边界保留。资源限制不是容量或长稳验收。

## 3. 自动化测试和审查

| 检查 | 实际结果 |
|---|---|
| 最终冻结根Go全仓race | 986通过测试节点、2项既有opt-in跳过、0失败；15个有测试的包通过；vet/build exit0 |
| 最终嵌套lab race | 121通过、5项原生opt-in默认跳过；vet/build exit0 |
| 两模型原生opt-in另行执行 | CAM++与ERes2Net各5通过，包括真实8秒片段；不把默认skip算通过 |
| Go子进程桥独立验证 | 39节点通过，普通/race/vet/build通过；覆盖队列、复制、错误协议、阻塞I/O、超时、清零和kill/reap |
| 会话接线 | 真实WebSocket假提供方的VAD→完整源区间→旁路；旁路从不返回身份时，普通生成仍继续 |
| 隐私 | MemoryOnly recorder/Native意图不落正文、拒绝同时开启跨会话记忆；默认原产品存档行为保留 |
| Mac SDK | 普通、试点、soak三种构建exit0，解析测试、非法CLI、签名/plist/脚本检查通过，无麦克风参与 |
| 独立质量审查 | 最终试点代码PASS，无剩余P1/P2；不覆盖真人准确率、听感或实体终端 |

先出现RED，再完成实现及回归。审查另外闭合三项状态错误：helper晚到的终止必须更新全局不可用；短/长/缺失PCM不能覆盖不可用或永久留下pending；Offer与helper失败同时发生时，不确定不能覆盖终态。均有确定性RED→GREEN和race证据。

首次普通根回归曾有旧`internal/gateway`测试`TestClearNewNativeGenerationDoesNotInheritRetiredResponseIDs`失败：收到`response.cancel`，预期`response.create`。对照确认gateway及依赖57份源文件/测试/模块摘要与原基线一致，也不依赖新增包；本轮未修改其源码。后续完整race曾985通过，最后冻结源码完整race为986通过/2跳过/0失败。保留原失败证据，不宣称该旧时序问题已修复。

## 4. 真实模型＋合成输入联调

输入为此前锁定的合成WAV，不使用用户真实录音。云端探针经真实Opus上行、千问Realtime问答及ERes2Net执行；持续静音维持音频时钟，不使用实际音频设备。

| 组别 | 结果 |
|---|---|
| 开启 | 4轮问答均生成并完整模拟播放；3个A音色片段均guest_1，B音色最终为uncertain，无强行误归；没有不可用或排空事件 |
| 关闭 | 1轮问答完整模拟播放，0条说话人事件 |

开启组出现5次VAD起点、4次最终回答，当前B输入存在分轮/拒识结果，因此**未验收不同说话人的可靠区分**。没有为这一次拒识降低阈值。A三段一致仅说明这批合成输入的一致性，不是家庭真人准确率。

播放回报为立即模拟的`CoreAudio_dataPlayedBack`协议字段，探针报告明确`no_CoreAudio_or_audio_device`；没有Mac扬声器播放、真人麦克风、手表I2S或声学验证。两组仅少量不同回答，不构成真实延迟或无性能影响的结论。

## 5. 部署、入口及停用

独立根：`/opt/family-robot-validation/speaker-live-20261103-050464b/`。受控二进制在`evidence/`，源在`source/`，元数据在`records-on/`与`records-off/`；模型复用原云端只读固定产物，不进Git。

- `family-robot-speaker-pilot-on.service`：仅`127.0.0.1:18052`，开启。
- `family-robot-speaker-pilot-off.service`：仅`127.0.0.1:18053`，关闭。
- 两入口health均ready，build=`6b5c63d`，`device_memory=false`、`diagnostics_active=false`；探针结束均active0。实际监听地址已核实，没有公网新增监听。
- Mac使用[独立启动器](../../tools/realtime-speaker-test/open.command)，通过现有SSH身份转发两回环端口。独立App bundle=`com.familycompanion.speakerlive`，不替换原产品客户端身份。

停止试点仅在云端执行：

```sh
systemctl stop family-robot-speaker-pilot-on.service family-robot-speaker-pilot-off.service
```

关Mac窗口会停止自己的App/SSH；云端不再设置服务累计运行上限，人工停止或服务器重启后仍须重新部署/核验。这不是系统启动持久单元，下次打开前仍需检查存活和ready。停用试点不回退或重启原产品服务。

保护快照前后：原Go服务PID1304344、网关PID4139310、NRestarts均0；两二进制、两env及产品身份5项摘要保持。原18040/18041、固件、设备网络和声学策略未改。

## 6. 真人现场状态

已通过AX核对“小满 · Realtime 声纹聊天测试”：开始按钮可用、识别开关已勾选、候选为空，代理未点击开始或麦克风权限。首次请用户直接正常问3题后，用户反馈“无法开始聊天，提示连接未成功”；没有取得有效真人识别或听感结果。服务恢复后再次核对窗口为开始按钮可用、开关勾选、候选为空，已请用户先问“小满，给我推荐一种简单的运动”核实连接及标签，随后只读AX确认开始按钮已变为“结束对话”、状态为“对话中”、识别开关活动期间禁用，并已有候选返回；云端开启组active_sessions=1。这证明连接已经建立，不证明候选对应正确人物。**当前等待用户对准确性和听感的反馈。**

连接恢复后，用户确认“正常回答，但不确定或声纹不可用”。只读AX精确显示为“不确定”，云端session识别进程仍运行，现场会话约125秒、未达到5分钟上限。用户随后确认存在其他人或电视等人声。该轮未形成单人基线，不能据此认定同一人误分，也不能把通用不确定提示当作可靠的多人/重叠检测。已请用户暂时排除其他人声、结束重开清空临时模板，再独自正常问3题，单人标签一致性结果待反馈；没有改阈值或保存真实片段。

多人A→B→A、短语、连续流中换人/重叠、噪声/距离/儿童、微雪/M5/C3、开关实际流畅度和长期运行均未验收；不能因为此窗口可用就宣布人物登记、按人记忆或完整多人产品交付。

## 7. 私有证据索引

云端`evidence/`：`final-root-race.jsonl`、`final-root-vet.txt`、`final-root-build.txt`、`final-lab-race.jsonl`、`final-lab-vet.txt`、`source.sha256`（26个变化源文件与云端匹配）、`binaries.sha256`、`pilot-probe-both.json`、`protection-{before,after}.json`及各状态问题RED/GREEN。旁路及lab独立证据分别在兄弟`transport-evidence/`、`lab-evidence/`；原生模型opt-in结果亦在lab证据目录。

这些证据不包含真实声音片段、向量或聊天正文，不作为Git附件提交。

## 8. 首轮无法连接的诊断与恢复（2026-11-03）

用户明确反馈无法开始聊天、提示连接未成功。现场核对：两个试点单元已被collect回收、回环18052/18053连接被拒绝、Mac试点App及其转发进程已退出；原产品18040仍ready。试点目录此前仅有合成探针的mac_fixture会话，没有真人已建立会话的证据。

服务日志绑定同日时间：00:30:02启动，12:30:02记录`Service reached runtime time limit. Stopping.`和`result=timeout`，对应原部署的12小时上限。这解释当时入口不可连接，不能把它记为声纹模型误识别或真人识别失败。

恢复仅使用摘要核验一致的6b5c63d服务器及原helper/模型，保留设备认证元数据与所有语音参数。首次把取消上限写成`RuntimeMaxSec=0`，12:54:41再次立即到期；该恢复错误未掩盖。最终显式改为`RuntimeMaxSec=infinity`，并从实际unit属性读取`RuntimeMaxUSec=infinity`与active状态后才继续。

恢复后逐个验证两入口：health、设备测试身份登记、鉴权pair、WebSocket及提供方模型会话ready全部通过，随后正常stop。核验没有采集麦克风、没有发送任何上行音频，也不代表真人语音效果已通过。原生客户端已通过规范启动器重开；用户随后建立会话，只读AX确认“对话中”和候选返回，云端active_sessions=1。未由代理开启麦克风，也未记录真实片段、文本或候选序列。候选是否正确、回答是否正常仍待用户确认。

私有证据在原`evidence/`：`reconnect-units.json`、`reconnect-handshake.json`、`reconnect-protection-{before,after}.json`；服务停止原因保留在对应systemd journal。永久运行上限取消后仍只监听回环，内存/CPU/任务数/核心转储保护、5分钟识别/32片段及15分钟单连接上限保持。
