# 千问官方与 LiveKit 会话、播放和上下文对照

后续已按批准范围实现设备回报与暂停原任务，见[阶段验证](2026-10-22-device-playback-and-paused-task.md)。本文保留前置审计当时的状态，不能据此判断后续候选是否已实现或已部署。

本轮完成固定版本源码核验、上游原版测试、真实千问隔离探针及首批 Go 契约复用。**接续位置仍未修复；辅助 ASR 未接入。** 正式网关和手表未更新，没有现场收音或新增麦克风录音。

## 结论与选择

1. 复用官方框架的输出身份、结束封口、有界迟到事件隔离；保留现有 Go 网关，不迁移整个框架。
2. 采用 LiveKit 对“音频转发”和“播放确认”的区分。当前只有服务器下发证据，没有设备播放回执，历史必须保留这种不确定性。
3. 同一条千问连接不会自动知道用户听到了哪里。合成对照中只虚拟播放约5.82秒，上游却认为已经数完五十；仅减少重连不能解决接续位置。
4. 当前接口已确认 create/delete 回执，但 truncate 仍没有确认。删除完整回复再插入一个已知文字前缀，也尚未验证正确接续，不能直接作为正式修复。
5. 下一实施重点是设备实际播放回报与独立的暂停任务状态。先具备可信的音频边界、原任务及旁支问答归属，再验证文字位置映射；需要时才选择辅助 ASR。即使辅助 ASR 提供了文字，它也不能代替播放回执和任务状态。

## 固定参考版本

| 项目 | 固定提交 | 本轮使用范围 |
| --- | --- | --- |
| [Qwen-Live-Harness](https://github.com/QwenLM/Qwen-Live-Harness/tree/b2d17b8706b226ff7792683ecd8f050f18f9752e) | `b2d17b8706b226ff7792683ecd8f050f18f9752e` | realtime session、host coordinator、输出队列、音频引擎、dialogue collector 及测试 |
| [LiveKit Agents](https://github.com/livekit/agents/tree/7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964) | `7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964` | voice I/O、agent activity、转写同步及播放/同步测试 |

两者根许可证均为 Apache-2.0；来源与本地适配说明在 `THIRD_PARTY_NOTICES.txt`，许可证在 `licenses/Apache-2.0.txt`。选取的24份源码逐份验证了 Git blob SHA；LiveKit 测试使用固定提交源码建立的独立 Python venv，没有修改正式运行环境。

## 源码核验与复用映射

| 问题 | 上游实现与限制 | 本项目处理 |
| --- | --- | --- |
| 生成完成是否等于播放完成 | Qwen `audio-output-queue.ts` 需要结束标记和活动帧归零；临时断流不会结束整个输出 | 现有60ms队列已等待 `response.done` 和排空；本轮补充结束后的音频/文字封口 |
| 迟到事件能否重开旧输出 | Qwen 使用 epoch/outputId、对象身份及最多256个完成代号，清队列后旧回调无效 | 沿用现有连接 generation 和 responseId，新增当前连接最近256个完成代号；旧 created/delta 不会重开播放 |
| 什么是已播放 | Qwen host 有 started/completed 回执，但没有逐字播放位置；其浏览器 started 回调还与音频调度有关 | 不把它的浏览器回调直接等同于手表扬声器出声；固件暂未新增播放回报 |
| 上下文如何截断 | LiveKit `agent_activity.py` 根据输出 sink 回报的位置处理，并以 provider capability 控制原生 truncate | 当前千问 truncate 未确认，不发送这个操作到正式链路 |
| 转写如何同步 | LiveKit `PlaybackFinishedEvent` 的 synchronized_transcript 可以为空；同步器优先时间标注，也可按语速估算 | 本轮只复用已知/未知语义，不把估算当成精确听到的文字。未接入 LiveKit 语速模型或新增 ASR |
| 官方是否天然保存已听前缀 | Qwen dialogue collector 保存生成文本及 interrupted 标记；未发现其按本设备的实际已播边界裁剪文字的实现 | 不能将官方框架的对话记录当成现成的精确接续方案 |

LiveKit 的播放进度可以按连续区段报告；被清除的队列及暂停形成的空洞不能简单累加成完整音频前缀。后续设备回报必须与回复代号对应，不得直接使用全局 I2S 计数、网络接收计数或通知播放器的提交回调充当实际完成证据。

## 实际测试

### 上游原版测试

| 环境 | 原版测试范围 | 结果与局限 |
| --- | --- | --- |
| 本机 Node 原生 TypeScript | Qwen `audio-output-queue.test.ts` | 10/10，通过长回复、结束标记、清队列后迟到回调、有限代号窗口和时钟累计误差场景 |
| 隔离 Linux / Python3.12 | LiveKit `test_playback_progress.py`、`test_playback_started_attribution.py`、`test_transcription_filter.py` | 454/454；包括参数化过滤测试，不能解读为454项实机播放测试。pytest 有一项未知 concurrent mark 警告 |
| 同一隔离环境 | LiveKit `test_transcript_sync_cancellation.py`、`test_transcript_sync_timed.py`、`test_transcript_sync_markup.py` | 6/6，验证取消和转写时序相关行为 |

这些测试使用合成帧、假音频 sink 或受控时钟，没有验证手表 DMA、喇叭停声延迟或中文逐字对齐。

### 当前千问接口探针

模型保持 `qwen3.8-omni-flash-realtime`，音频16kHz上行、24kHz下行。只上传本轮或既有的机器合成语音；没有上传现场录音、改变正式服务或启用设备。

| 操作 | 实测结果 | 可得结论 |
| --- | --- | --- |
| `conversation.item.create` | 收到 created 回执 | 确认该操作基本可用；不代表指定历史位置及所有字段都被执行 |
| `conversation.item.delete` | 对生成的回复收到对应 itemId 的 deleted 回执 | 确认删除回执；单独 ACK 不保证接续语义正确 |
| `conversation.item.truncate`，生成回复后请求2000ms边界 | 等待8秒无 truncated 回执；此前两次独立探针同样没有确认 | 本次不能依赖原生按播放毫秒截断；不推断所有模型/未来版本均不支持 |
| 同一连接：数数 → 约5秒后插话 → 数学回答 → 继续 | 0.66秒的“继续数”合成短语三次均未产生 speech_started，不算有效上下文对照；换约2.4秒短语后，完整走通三轮，上游却回答已经数到五十 | 有效对照仍接续失败；需要已播边界同步，不能仅保留连接 |
| 同一连接：完整生成数数 → 数学回答 → 删除旧回复 → 以人工已知前缀“一至七”替换 → 继续 | 删除和新文字创建均 ACK；返回的新消息 ID 及 previous_item_id 未对应请求值；最终生成稿从三继续到五十 | 本轮没有验证将前缀插回原历史位置，也没有实现从八继续。暂不用于正式上下文补正 |

最后一项前缀是测试人为指定的正确条件，不来自辅助 ASR、比例估算或手表回报。初版探针要求回执保留请求的 ID/位置，因此将两轮记录为未匹配；进一步记录回执后确认创建有 ACK，只是请求的身份/位置条件没有成立。不能把初版的等待超时误报成 create 不支持。最后生成稿的文本检查也不能替代真实音频完整性检查。

官方 [Omni 客户端事件](https://www.alibabacloud.com/help/en/model-studio/client-events)与[服务端事件](https://www.alibabacloud.com/help/en/model-studio/server-events)没有提供可直接用于此设备的逐词输出时间戳；本次实际 audio/transcript delta 字段也没有时间标注。本轮没有凭其他厂商同名 API 或“OpenAI兼容”推断千问能力。

## 首批 Go 改动与验证

- `response_lifecycle.go`：有界完成代号集合，每次上游连接切代重置；现有 generation 继续隔离旧连接事件。
- `session.go`：结束标记后不再接纳旧音频/文字；重复结束不破坏正在排空的回复；旧回复代号不能重开，新重叠回复仍失败关闭。
- `history.go` / `playback.go`：普通下发结束记录为 `delivery=transport_sent` 和 `generated_text`，不把它写成已确认听过的文字；中断历史仍单独保留，旁支答案不抹去原稿。
- `qwen/client.go`：解释新增传输状态的含义，不新增接续提示词猜测，也不宣称已有硬件回执。

新增5项契约回归先观察失败，再完成最小改动并通过。完整回归最初暴露旧测试仍按普通 `text` 读取旁支答案，按新的明确传输语义更新后，macOS 与隔离 Linux 均通过 `go test -race ./...`、`go vet ./...`；Linux `go build -trimpath ./cmd/gateway` 成功。macOS race 链接仍有既有 LC_DYSYMTAB 警告，不能写成无警告构建；Linux 本次没有该警告。

这些改动只完成协议基础，不改变当前重连策略，没有新增设备进度消息、上下文删除/插入或 ASR。正式发布仍为 `c5a8f36`，手表固件仍为 `5b8af2e`。现场“打断后从50继续并越过终点”的缺陷保持打开。

私有证据保存在忽略目录 `artifacts/voice-reference-audit-20261022/`：上游测试、隔离探针、RED/GREEN、macOS/Linux 回归、源码与产物摘要；源码参考副本和一次性探针位于 `.superpowers/`，没有进入 Git。下一阶段完整目标仍见[播放进度接续候选方案](../superpowers/specs/2026-10-22-resume-playback-progress-design.md)，其中辅助 ASR 仅是条件候选。
