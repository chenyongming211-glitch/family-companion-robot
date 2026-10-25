# Realtime Go 迁移验证（Mac默认入口已切Go，Python保留回退）

## 当前边界

方案5417671已审核；冻结Python版本3b89df8，初始Go实现636ac95，当前运行实现b6f6ebd，依赖/公开文件摘要见[冻结清单](2026-10-25-realtime-go-baseline.json)。Go服务18040已部署，Mac默认启动器与客户端缺省地址已切到18040；Python18032继续在线作对照/回退，原8000/18010/18020/18030均保持。本轮现有Realtime语音链路的代码迁移、Mac对照验收与默认入口切换已完成；Python退役和其他设备接入单独处理。

## 确定性与受控传输

- 新`realtimeaudio`：Opus AUDIO模式、16k320/24k1440严格帧、持续编解码、10组封尾，以及跨包24k→16k重采样，均与云端Python/libopus逐字节或逐样本一致。9个主测试＋30子测试通过，race/vet通过。
- 新`realtimestate`：494组Python差异用例，固定北京时间完整提示词字节相同；覆盖近期16轮、数字规范化、ASR观察、未播稿隔离和严格receipt。race/vet通过，覆盖88.6%。
- 新`realtimelive`：请求字段、搜索确认/异常来源、意图先落盘、票据/Origin/单会话、首包流式、排空栅栏、取消及失败恢复测试通过。独立审查增加8项短回归×10次、41秒慢建连/首音频预算和65秒静默/心跳回归，race通过。
- 审查修复：监听不继承生成事件35秒超时；下发/ASR feed与封口同锁；旧任务失败只可封口同一回复；Open不侵占40秒包等待；ASR完成先join发送端；退出等待活动会话清理。
- 最终完整云端副本与仓库17个Go包清单一致：`go test -json -race ./...`通过（580通过、0失败、2个既有opt-in长测跳过；这2项此前已单独以41秒/65秒实测通过），`go vet ./...`、`go build ./...`通过。初期旧gateway测试失败及测试夹具修正见下方，不隐去失败记录。
- Python旧入口49项＋原probe8项仍通过。新服务核心无Python调用或跨语言IPC，Python仅用于测试oracle与保留对照服务。

## 独立的旧gateway测试夹具修复

初期验证出现`TestRecordsDisconnectDuringClearKeepsDeviceLease` EOF、`TestClearUsesFreshNativeConversation/true`超时。冻结3b89df8的第一项也可复现；独立验证副本修正前30次race重复失败16次。

测试修正提交818189a只改`records_test.go`和`clear_native_test.go`，不改gateway/qwen/audio运行代码或业务断言。前一项明确消费、握手并观察被取消的clear替换连接，避免残留peer被错认成设备重连；在Clear仍阻塞时保留409断言，再用session完成屏障替代20ms等待和反复拨号。后一helper对设备/提供方双socket注入增加manual→auto配置请求屏障，仍以auto模式执行原断言。独立审查确认无断言弱化。

修正后目标pair race重复100次通过，全部TestClear*消费者重复10次通过；最终完整17包race、vet、build均通过。真正的uncached全跑中gateway耗时52.4秒，最终门禁在适用时使用测试缓存。初期验证副本漏`tools/voice-audit`/Python参考文件，以及一次备份目录误入包枚举，均作为setup失败留存；最终改用完整git archive，核对清单一致后验证，不能拿不完整副本冒称全仓通过。

云端证据：`/opt/family-robot-livekit-validation/go-migration-ci/`中的red.log、scoped-race.log、clear-helpers-race.log、full-snapshot-race.jsonl、full-vet.log、full-build.log及package inventory。全量私有日志不进Git。

## 真实模型与播放

最初Go/CoreAudio烟测2轮通过：数学与小满身份，2次完整播放，0客户端错误。普通问答交替完成32轮（Go/Python各16），客户端0错误。服务端VAD话尾至首包：Go中位998.5ms、范围846—1267ms；Python中位1038.5ms、范围893—1163ms。未触发max(100ms,10%)诊断线，不宣称Go普遍提速或P95领先。插话排空与搜索的交替验证也已完成，详细数值见[对照JSON](2026-10-25-realtime-go-comparison.json)；长测结果见下面；全部使用合成输入及真实CoreAudio回报，不代表真人麦克风/AEC或主观自然度验收。

636ac95的审计曾将正常关窗记成`input_failed/session_error`（每次退出出现），不是这些轮次的真实提供方失败。已用可复现测试修正：正常stop/1000/1001与会话取消后的收音清理不记故障；活跃会话的提供方/协议错误仍保留。新回归race通过，独立复审无P1/P2；修正已在空闲时更新，当前b6f6ebd实际退出验证见下文。

### 完成的交替对照

| 项目 | Go | Python |
|---|---:|---:|
| 普通问答样本 | 16 | 16 |
| 普通问答话尾→首包中位 | 998.5ms | 1038.5ms |
| 排空次数 | 12 | 12 |
| 封口→排空确认中位/最大 | 279/305ms | 278/312ms |
| 插问新问题首包中位/最大 | 1023.5/1217ms | 1010/1316ms |
| 天气真实查询样本 | 1次查询，17来源 | 1次查询，17来源 |
| 天气话尾→首包 | 2831ms | 2578ms |

这12个客户端批次共86轮（62完整输出＋24次中断排空）均正常完成，未发生turn_error或drain_timeout。两条链路排空均满足同id下发/接收/完成边界；未逐字验收候选ASR和故事续写。天气只有每边1个真实查询，不能据此评价搜索性能或实况准确性。正常退出的误分类日志见上方，不把它写成所有服务端错误计数为0。

启动器实测：选择远端18040完成2轮，再结束并选择18032新配对完成数学和身份2轮，均有CoreAudio完整播放；只清理各自创建的转发，验证按新会话回退，不重试旧请求。上述回退测试时默认远端仍18032；最终切换见后文。

真实URLSession正常退出有时直接关闭TCP，出现1006。Go按Python原来的客户端迭代结束语义，将其记录为transport_closed（保留数字close_code），结束会话并清理；不当成提供方失败，也不声称传输健康。JSON/codec/receipt/背压错误仍报session_error，活跃上游故障仍记input_failed。stop/1000/无close帧TCP关闭均有race回归，独立审查通过。

## 部署

- 云端运行二进制：b6f6ebd（go version -m确认vcs.modified=false）；服务`family-robot-realtime-output-go.service`，当前PID918937，回环18040。后续提交仅文档/测试，不因此重启正在测试的服务。
- 模型/Key只读服务器`/etc/family-robot/realtime-test.env`，小满档案仍`config/product-identity.json`，搜索默认1。原Python服务PID874598保持。
- Go私有文字/Intent：`/var/lib/family-robot-realtime-test/verification/output-live-go/`；Python仍原`output-live/`。不共写文件，不存原音频，不在Git提交正文或凭据。
- 启动器新增`OUTPUT_LIVE_REMOTE_PORT`；当前本地和远端缺省均18040，显式指定18032可回退。Mac音频/Opus/播放回报源码不变。新入口必须同时核对本地转发目标与`/healthz.backend=go`。

## 持续会话与用户调整

会话`20261025T050820-dbdde09c`完成32输入、32次CoreAudio完整播放，下发/接收/完成样本全部相等；最大恢复16轮，older_turns_omitted实际出现。身份插入第8/32轮仍为小满，第16轮表示开发者资料未提供，第24轮区分千问模型与阿里云来源。没有turn_error、drain_timeout、input_failed、session_error。

用户在32轮完成后明确要求“不用满20分钟了，继续后面的吧”，因此提前结束。服务端首次speech_started至closed为1084.305秒（约18分4秒），**不记为20分钟通过**。终止测试进程退出143，transport_closed保留1006；这是本次用户要求结束的证据，不改成客户端fixture_success。详见[长测数据](2026-10-25-realtime-go-soak.json)。

长测调度器只在忽略目录生成测试用Fixture副本，扩展轮数/间隔及等待；正式Swift音频、Opus、播放ledger和LiveClient源码不改。输入全部合成，真实CoreAudio播放；测试后半程同时有云端单测负载，不能用它宣称性能或容量。服务器日志只保存文字和来源，不存原音频。

## 切换前的真人标识问题（历史记录）

此阶段已完成自动对照、每边12次排空、实际搜索、按新会话回退和用户调整后的长测；普通问答诊断线保持max(100ms,10%)。当时Go真人反馈因窗口身份不清暂不计入验收，因此保持Python默认；后续明确反馈与切换见下一节。模型已知的天气/能力/缺失历史/候选误差不算语言迁移修复。正式设备、跨会话记忆、判题和精确同稿恢复均不在本轮。

用户曾回复“体验基本一致”，随后指出两个相同标题的窗口无法区分。因此该反馈不作为已确认Go来源的真人验收结论，未提交默认切换；启动器和无参数客户端缺省仍18032。

已修正测试UI：标题栏与窗口内均常驻“Go测试版·云端18040”（蓝色、左侧）或“Python对照版·云端18032”（橙色、右侧）；标签取启动器的远端映射与健康信息，其他入口不擅自命名。正式音频、Opus、播放ledger及LiveClient源码没有改动。Swift重新编译、bash语法检查通过，原生AX/截图已核对Python标题及橙色常驻标识，Go进程以明确Go/18040参数启动在左侧。两个窗口已重新打开供明确对照，由用户自行关闭；不同时收音。当时等待标识清楚后的真人反馈，Python回退与原四服务保持。

## 标识修正后的真人验收与默认切换

用户已明确分别测试Go和Python，并反馈“体验上差不多，区别不大”，之后额外完成一次Go对话。两条链路的真实麦克风、扬声器、等待、插话与衔接获得本轮主观对比确认；这不代表极端AEC、长期稳定性、天气事实或逐字断点已全面验收。

已复核的真人会话（北京时间）：

| 入口/会话 | 输入语音段 | 完整播放 | 插话排空 | 其他结束方式 |
|---|---:|---:|---:|---|
| Go 13:35—13:37 | 13 | 6 | 6 | 1次回复中结束会话 |
| Python 13:37—13:39 | 11 | 4 | 7 | 无 |
| Go 13:40—13:41 | 9 | 6 | 2 | 1次出声前被新语音替换 |
| Go 13:56—14:00 | 20 | 10 | 6 | 3次出声前被新语音替换、1次回复中结束会话 |

所有终态回报均满足sent=accepted=completed；无turn_error、input_failed、drain_timeout、session_error，无服务重启，连接均以1000结束。审计中的生成/建连取消与新语音触发时间一致，输出ASR未完成与结束会话一致，不标成成功转写；最后一段文字可能不完整。原始通话正文仅在服务器私有目录，不进本报告。

据已批准的“等价迁移→对照验证→切换”顺序，现将`open.command`的本地/远端默认端口及`main.swift`无参数默认端口改为18040。当前窗口和服务器不重启。切换仅改客户端地址及说明；使用独立输出目录重新编译Swift客户端，并在不传`--port`时完成1轮真实模型/CoreAudio数学播放，确认实际连接Go；bash语法检查通过。Go运行实现仍为b6f6ebd，前述全仓race/vet/build证据不变。

回退时先结束当前Go会话，再为新会话显式选择Python：

```sh
OUTPUT_LIVE_LOCAL_PORT=18042 OUTPUT_LIVE_REMOTE_PORT=18032 ./tools/realtime-output-live/open.command
```

Python服务继续在线作短期回退，不承载默认Go请求；服务停用、源码清理和ESP32/小程序接入不是本次默认切换的一部分。Go现在可作为后续开发基线，本轮不新增判题、提醒或设备工具。
