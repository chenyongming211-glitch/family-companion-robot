# Realtime Go 迁移验证（进行中）

## 当前边界

方案5417671已审核；冻结Python版本3b89df8，Go实现636ac95，依赖/公开文件摘要见[冻结清单](2026-10-25-realtime-go-baseline.json)。独立Go服务18040已部署，默认18032仍Python，原8000/18010/18020/18030均保持。尚未切换日常入口或停用Python，不宣称迁移验收完成。

## 确定性与受控传输

- 新`realtimeaudio`：Opus AUDIO模式、16k320/24k1440严格帧、持续编解码、10组封尾，以及跨包24k→16k重采样，均与云端Python/libopus逐字节或逐样本一致。9个主测试＋30子测试通过，race/vet通过。
- 新`realtimestate`：494组Python差异用例，固定北京时间完整提示词字节相同；覆盖近期16轮、数字规范化、ASR观察、未播稿隔离和严格receipt。race/vet通过，覆盖88.6%。
- 新`realtimelive`：请求字段、搜索确认/异常来源、意图先落盘、票据/Origin/单会话、首包流式、排空栅栏、取消及失败恢复测试通过。独立审查增加8项短回归×10次、41秒慢建连/首音频预算和65秒静默/心跳回归，race通过。
- 审查修复：监听不继承生成事件35秒超时；下发/ASR feed与封口同锁；旧任务失败只可封口同一回复；Open不侵占40秒包等待；ASR完成先join发送端；退出等待活动会话清理。
- 云端全仓`go vet ./...`、`go build ./...`通过。`go test -race ./...`未全绿：未修改的旧gateway在两次全跑中出现清空会话相关测试不稳定，见下方；不能写成全库通过。
- Python旧入口49项＋原probe8项仍通过。新服务核心无Python调用或跨语言IPC，Python仅用于测试oracle与保留对照服务。

## 旧gateway测试限制

全仓第1次失败`TestRecordsDisconnectDuringClearKeepsDeviceLease`；第2次另出现`TestClearUsesFreshNativeConversation/true`。前一项在冻结3b89df8未修改仓库重复30次也复现，静态分析指向fixture复用队列中残留的已取消模型连接，尚未修改；后者在基线单独重复10次通过。当前Go迁移没有改`internal/gateway`、旧`internal/qwen`或旧`internal/audio`；这两项全套压力下的不稳定仍需单列调查，不用重复跑到绿掩盖失败，也不在本轮顺手改旧清空逻辑。

## 真实模型与播放

最初Go/CoreAudio烟测2轮通过：数学与小满身份，2次完整播放，0客户端错误。普通问答交替完成32轮（Go/Python各16），客户端0错误。服务端VAD话尾至首包：Go中位998.5ms、范围846—1267ms；Python中位1038.5ms、范围893—1163ms。未触发max(100ms,10%)诊断线，不宣称Go普遍提速或P95领先。插话排空与搜索的交替验证也已完成，详细数值见[对照JSON](2026-10-25-realtime-go-comparison.json)；接下来做20分钟持续连接测试；全部使用合成输入及真实CoreAudio回报，不代表真人麦克风/AEC或主观自然度验收。

636ac95的审计曾将正常关窗记成`input_failed/session_error`（每次退出出现），不是这些轮次的真实提供方失败。已用可复现测试修正：正常stop/1000/1001与会话取消后的收音清理不记故障；活跃会话的提供方/协议错误仍保留。新回归race通过，独立复审无P1/P2；待当前批次空闲再更新二进制。

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

启动器实测：选择远端18040完成2轮，再结束并选择18032新配对完成数学和身份2轮，均有CoreAudio完整播放；只清理各自创建的转发，验证按新会话回退，不重试旧请求。默认远端18032没有修改。

真实URLSession正常退出有时直接关闭TCP，出现1006。Go按Python原来的客户端迭代结束语义，将其记录为transport_closed（保留数字close_code），结束会话并清理；不当成提供方失败，也不声称传输健康。JSON/codec/receipt/背压错误仍报session_error，活跃上游故障仍记input_failed。stop/1000/无close帧TCP关闭均有race回归，独立审查通过。

## 部署

- 云端源码与二进制：636ac95；服务`family-robot-realtime-output-go.service`，初始PID905326，回环18040。
- 模型/Key只读服务器`/etc/family-robot/realtime-test.env`，小满档案仍`config/product-identity.json`，搜索默认1。原Python服务PID874598保持。
- Go私有文字/Intent：`/var/lib/family-robot-realtime-test/verification/output-live-go/`；Python仍原`output-live/`。不共写文件，不存原音频，不在Git提交正文或凭据。
- 启动器新增`OUTPUT_LIVE_REMOTE_PORT`；缺省仍18032。Mac音频/Opus/播放回报源码不变。新入口必须同时核对本地转发目标与`/healthz.backend=go`。

## 未通过前禁止切换

功能逐项、普通/搜索等分组时延、10次排空、20分钟持续会话、回退演练和真人听感均需有证据；诊断线保持max(100ms,10%)。模型已知的天气/能力/缺失历史/候选误差不算语言迁移修复。正式设备、跨会话记忆、判题和精确同稿恢复均不在本轮。
