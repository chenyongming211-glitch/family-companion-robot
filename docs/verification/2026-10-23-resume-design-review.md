# 自动接续方案复核：公开实现与当前约束

2026-10-23复核。结论：按设备播放进度维护模型上下文的方向合理；把外部强制对齐直接定为唯一实现路径过早。此次为官方文档/源码和既有实测证据的复核，未重跑各框架端到端测试，未改变线上代码、固件、VAD或加入辅助ASR。

## 公开实现采用的机制

| 对象 | 公开机制 | 适用边界 |
|---|---|---|
| OpenAI Realtime | WebSocket客户端停止播放并用audio_end_ms请求conversation.item.truncate，服务端清除未播音频及相应文字；WebRTC/SIP由服务端管理输出缓冲并处理截断 | 官方明确不会提供准确截断后的转写；原生音频上下文截断与外部字词时间表是不同能力，不能由此推断Codex内部实现 |
| LiveKit | 对话中断后维护部分回复；可使用TTS提供的词级时间戳，部分提供方仅句级；同步器也有语速估计分支 | 准确度依赖实际提供方与播放时钟，不能把估计字幕当作ESP32已播证据 |
| Pipecat | 链式语音中把assistant聚合器放在transport.output之后，用TTS文字帧随播放提交已释放前缀；中断清生成与播放队列 | 逐词更新取决于服务时间戳；原生S2S依赖提供方能力。已有慢sink实验表明时钟和消费脱节时文本可超前 |
| 千问Qwen-Live-Harness | 有输出身份、播放完成/清除、生成与播放分离处理 | 当前HEAD仍为之前实测b2d17b8；既有真实调用显示清队列成功仍可能认为任务已完整完成或续到51，不能视为精确接续参考答案 |

本次检查GitHub HEAD：LiveKit bad40f1c836d4ffd9cef87360509aa63d26ee96e，Pipecat 3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9，Qwen-Live-Harness b2d17b8706b226ff7792683ecd8f050f18f9752e。LiveKit当前同步器仍同时含annotated_rate和estimated_rate分支；Pipecat/Qwen与此前固定实验提交一致。没有把本次源码复核描述成新的模型或设备实验。

主要官方来源：

- [OpenAI Realtime中断与截断](https://developers.openai.com/api/docs/guides/realtime-conversations#interruption-and-truncation)与[客户端事件](https://developers.openai.com/api/reference/resources/realtime/client-events#conversation-item-truncate)。
- [LiveKit转写同步](https://docs.livekit.io/agents/multimodality/text/#tts-aligned-transcriptions)、[中断说明](https://docs.livekit.io/agents/logic/turns/)、[本次HEAD同步器源码](https://github.com/livekit/agents/blob/bad40f1c836d4ffd9cef87360509aa63d26ee96e/livekit-agents/livekit/agents/voice/transcription/synchronizer.py)。
- [Pipecat上下文聚合](https://docs.pipecat.ai/pipecat/learn/context-management#assistant-context-aggregator)与[中断处理](https://docs.pipecat.ai/pipecat/fundamentals/interruptions)。
- [千问客户端事件](https://help.aliyun.com/zh/model-studio/client-events)、[服务端事件](https://help.aliyun.com/zh/model-studio/server-events)、[官方Harness固定源码](https://github.com/QwenLM/Qwen-Live-Harness/tree/b2d17b8706b226ff7792683ecd8f050f18f9752e)。

## 对我们现用千问的判断

本次从线上配置只读取非敏感模型名：qwen3.8-omni-flash-realtime。当前官方客户端文档未列conversation.item.truncate；输出音频转写事件未给逐字起止时间。此前真实500ms边界截断未收到有效truncated确认，且续答仍跟随生成稿；这不足以断言所有后端永远不支持，但足以让当前实现不依赖它。该实测未在本次重跑，见[LiveKit对照](2026-10-23-livekit-comparison.md)。

因此不能照抄OpenAI插件就假设具备上游音频历史裁剪，也不能通过换框架补出不存在的时间信息。当前Go原任务/版本/归属与设备I2S回报继续保留。

## 原方案需要修正的地方

1. **外部对齐是候选，不是其他框架共同必需的组件。** 原生可确认音频截断或提供方词时间戳可优先复用；在目前千问限制下仍需自行补定位能力。
2. **完整音频实验不能覆盖早停。** 原生生成可能在正文已领先时被取消，剩下半段音频和完整或超前正文。此前52个模型结果涵盖重复、后端与文本变体，并不是52轮准确接续验收。截断/静音也可产生貌似完整时间戳，不能用它们自动宣布覆盖。audio.done本身也不能证明正常完整生成，千问文档明确取消或不完整时也会发出。
3. **句级恢复同样需要可证实的音频边界。** 按标点切文本或按固定语速估计，不能证明那句话已经播放完。保守恢复可以重读当前词/句，不能把尚未播的部分当成已完成。
4. **资源与异步处理必须验证。** 本机对齐CPU峰值约4.07GiB，线上服务器总内存约3.64GiB；先验证所选部署方式的实际开销。停止播放和收新输入不能等待对齐。

## 建议的下一步实验设计（尚未实施）

保留现有生产链路与未知时询问的退路。在同一批机器人输出音频上离线比较：

- A：已知正文的强制对齐。只对确有对应音频的范围提交已播文字，重点验证正文领先和音频提前取消时的拒绝/降级能力。
- B：识别实际已播机器人音频的辅助ASR，必要时匹配生成稿/时间戳。它也会误识别或产生额外开销；不能免除播放坐标标定、覆盖检查和置信度判断。离线比较不等于决定给生产加ASR。

两条路都以独立标注为真值，覆盖中文数数、普通叙述、数字/专有词、完整生成、很早取消、词/句中间打断、恢复后再次打断，以及设备队列落后与迟到final。比较最后完整单位的误差、漏读/重读、结果准备时间、CPU/内存与实际调用费用；不能只比较有无返回时间戳。

通过后再接入：有效设备final→校准坐标→可信文字前缀→原任务的最新revision→当前快照ACK→恢复。可信度不足保持unknown并询问。优先目标是无需用户报起点、保住原终点、不漏内容，允许重读被截断词或句。

本轮不据此提出框架迁移，也未承诺某个模型已经达到上述目标。实验数据依据见[现有对齐评估](2026-10-23-forced-alignment.md)和[多框架模块对照](2026-10-23-resume-framework-root-cause.md)。
