# LiveKit 与当前千问原生实时接口隔离对照

本轮没有迁移正式框架、占用正式端口、播放声音或操作手表。真实模型调用只使用已有机器合成的 `count50.wav` 和 `hello.wav`；音频响应直接由内存消费者丢弃，没有保存响应音频。下述音频字节数表示框架收到的 PCM，不能解释为用户听到或设备播放确认。

## 结论

**固定版本的现成 OpenAI realtime 插件可以建立千问连接并收到模型响应，但不能直接提供可用的 LiveKit 音频/文字流。** `base_url` 可连接不等于协议可用。加入小型、明确标为自定义的适配后，已走通千问原生实时音频、连续两轮、生成中取消和第三轮上下文；这并未解决按实际播放位置继续数数的问题。

不建议据此迁移网关。LiveKit 值得借鉴的部分是取消生命周期、生成与播放分离、输出身份及基于 sink 回报处理历史的契约；当前千问 provider 的事件、配置、请求关联与截断能力仍需要独立适配和验证。

## 固定环境

- LiveKit Agents 源码：[`7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964`](https://github.com/livekit/agents/tree/7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964)，现有隔离源码与 venv；没有安装全局包。
- Linux 6.8.0-71-generic x86_64、Python 3.12.3；`livekit-agents` / `livekit-plugins-openai` 1.8.8、`livekit` 1.1.21、`openai` 3.36.1、`aiohttp` 3.14.5、`pytest` 9.1.1。
- 模型 `qwen3.8-omni-flash-realtime`；语音 Tina；`semantic_vad`；自定义探针明确使用16kHz/单声道/PCM16输入，24kHz/单声道/PCM16输出。没有另行接入辅助 ASR。
- 合成输入时长：数到五十5.06925秒、你好0.992625秒；20ms帧按实时速度上传，每轮后附加1.6秒零 PCM。
- API key 只在服务器 root 子进程内从受保护环境加载，不复制、打印或写入报告。探针无服务监听端口，不读取现场音频。

## 现成插件实测

实际构造固定源码的 `livekit.plugins.openai.realtime.RealtimeModel`，设置当前千问 `base_url`、模型、Tina、semantic VAD；没有修改其协议实现。

1. 收到 `session.created` 与 `session.updated`。发送的配置是 OpenAI GA 的嵌套 `audio.input/output`、24kHz、`output_modalities` 结构；这一步不能声称已协商为千问要求的16kHz上行。
2. 调用 `generate_reply(instructions="只用中文说你好。")` 后，千问返回 `response.created`、`response.output_item.added`、`response.content_part.added`、3个 `response.audio.delta`、2个 `response.audio_transcript.delta`，最后 `response.done`。
3. **LiveKit 消费者收到0字节、0帧和空文字；`generate_reply` 等待8秒超时。** 千问 `response.created.response.metadata` 实测为空。

固定源码提供了直接根因：

- `llm/_realtime/openai.py` 的接收分发识别 GA `response.output_audio.delta` / `response.output_audio_transcript.delta`；beta事件映射只在 `is_azure` 时执行。当前千问返回的是 `response.audio.delta` / `response.audio_transcript.delta`。
- `generate_reply` 用 `metadata.client_event_id` 关联创建请求；当前千问未回传该字段，generation事件虽然出现，请求 future 仍未完成。
- 固定版本上行 `SAMPLE_RATE=24000`，`push_audio` 自带重采样。直接替换连接地址不保证16kHz千问音频契约。

因此不能把“收到 session.updated”和原始网络音频计入现成插件端到端通过。固定源码未找到原生 Qwen realtime 插件；有Qwen3 STT/TTS及兼容文本LLM入口，不能替代原生 realtime 验证。[LiveKit realtime说明](https://docs.livekit.io/agents/models/realtime/)、[OpenAI兼容文本LLM说明](https://docs.livekit.io/agents/models/llm/openai-compatible-llms/)、[千问原生实时说明](https://help.aliyun.com/en/model-studio/realtime)。

## 自定义适配探针

这段适配只存在一次性隔离进程，**不是官方现成插件，不是生产适配器**。它对固定核心做三类显式转换：

1. 将 session.update 换为千问原生扁平字段，明确16kHz输入与24kHz输出；通过 `send_event` 上传原生16kHz帧，绕过OpenAI固定24kHz `push_audio` 路径。
2. 将5个beta事件名映射到GA：audio delta/done、audio_transcript delta/done、conversation.item.created。
3. 仅在恰好一个待确认创建请求时，本地补充请求关联字段。它不能证明千问支持metadata，也没有覆盖并发生成、重连、超时后迟到response等场景，不能直接用于正式环境。

第一轮完整生成验证：数数响应进入LiveKit消费者537600字节/70帧（11.2秒PCM）、文字一至五十；第二轮你好53760字节/7帧（1.12秒PCM）；第三轮询问原请求终点，回答“50”。第一轮取消调用发生在模型生成完成后，故该轮不计入生成中取消通过。

第二轮改为生成开始后尽早取消：取消前已收38400字节，调用后约51ms收到 `response.done.status=cancelled`；最终收46080字节/6帧（0.96秒PCM），生成文字已到“四十八、四…”。这证明**生成文字进度显著领先音频流**，不能以文字末尾代替已播边界。随后你好正常响应，第三轮“继续刚才数数的任务，数到五十为止”生成“四十九、五十”。该轮曾发2000ms truncate，但它超过当轮已收到的960ms音频，因此不能作为有效边界截断验收；保留原始条件并补做500ms边界的独立复测。

第三轮独立复测采用500ms测试边界（小于该次已收800ms音频，仍不是真实播放回报）：取消前30720字节；约43ms收到cancelled；最终38400字节/5帧（0.8秒PCM），生成稿到“三十九”。发送truncate后至最终汇总约19秒内无 `conversation.item.truncated`，也无error事件。旁支你好正常，继续任务生成“四十”至“五十”，仍跟随生成稿末尾而非500ms音频边界。该实验验证了取消和跨轮上下文，但没有验证截断成功；不应对千问宣称原生truncate可用。

此处取消时延仅是隔离Python进程调用到上游取消回执，包含网络条件；它不是设备停声时延。转写是模型生成稿，未核听输出声音，也未测文本与PCM逐字对齐。

## 上游原版测试

| 范围 | 本轮结果 | 说明 |
| --- | --- | --- |
| playback_progress、playback_started_attribution、transcript_sync cancellation/timed/markup、OpenAI realtime model、session_close_user_turn、agent_session | 180 passed / 9.87秒；2条未知 `no_concurrent` mark警告 | 包括frame身份、播放边界、取消、会话状态与provider序列化；不是180项实机验证 |
| realtime reply/say chat_ctx、input_speech_interrupt、chat_ctx_concurrency、OpenAI chat_ctx、overlapping_speech、cancel_tool_reply、interrupt_cancels_generation、wait_for_idle_cancellation、interrupt_protected_speech | 57 passed / 2.59秒；4条未知 `no_concurrent` mark警告 | 假模型/受控时钟的上下文及取消契约，不依赖真实千问 |

合计237项通过。最初一次命令包含不存在的 `test_remote_chat_context.py`，pytest直接退出且没有运行测试；删掉错误文件名后才得到上述有效结果，没有将空跑计入通过。

`agent_activity.py` 只在部分播放且provider声明 `message_truncation` 时，拿输出的 `playback_position` 与 `forwarded_text` 调用provider truncate。OpenAI provider 的音频truncate实际发送 `conversation.item.truncate`。这是一层provider操作，不能使不支持该事件的上游自动获得截断能力；假sink的可信回报也不能直接替代手表I2S播放确认。参见[固定核心源码](https://github.com/livekit/agents/blob/7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964/livekit-agents/livekit/agents/llm/_realtime/openai.py)及[固定agent_activity](https://github.com/livekit/agents/blob/7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964/livekit-agents/livekit/agents/voice/agent_activity.py)。

## 复现与证据

私有日志、固定环境版本、探针源码位于忽略目录 `artifacts/overnight-20261023/livekit/`；探针源码另在 `.superpowers/overnight-20261023/livekit/`。不含密钥或原始用户音频。服务器隔离目录为 `/tmp/family-robot-overnight-livekit/`；源码根目录 `/tmp/family-robot-livekit-audit-7a7547e/agents-7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964`；venv `/tmp/family-robot-livekit-audit-7a7547e/venv`。

在具备受保护环境和合成fixture的同一服务器，用该venv运行 `sudo -n timeout 50 <venv>/bin/python /tmp/family-robot-overnight-livekit/native_probe.py stock`，自定义对照为 `sudo -n timeout 100 <venv>/bin/python /tmp/family-robot-overnight-livekit/native_probe.py custom`。脚本只记录事件配置、类型、取消状态和合成对话生成稿，不记录音频base64。

上游测试在固定源码根执行 `<venv>/bin/pytest -q` 加上述表格对应的 `tests/test_*.py` 路径；完整无密钥命令存于证据目录 `reproduce.sh`。现成插件首次探针 `stock_probe.py` 只用于定位；最终可复现版本为 `native_probe.py`。本轮没有LiveKit房间/SFU/设备端的端到端运行，没有生产重连/并发工具调用/长期内存压力测试，也没有证明按500ms音频准确恢复到某个中文数字。
