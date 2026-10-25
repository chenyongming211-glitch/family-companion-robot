# 千问Realtime配置项与18032状态

核对日期：2026-10-25。模型：Qwen3.8-Omni-Flash-Realtime。下表是本轮版本的配置；运行验证另见[联网搜索记录](2026-10-25-realtime-search.md)。不是把所有选项同时打开。

| 配置 | 作用 | 18032本轮设置/实现 |
|---|---|---|
| `enable_search` | 模型自主判断是否联网搜索，官方默认false | 默认true；`OUTPUT_LIVE_ENABLE_SEARCH=0`可回退关闭，重启后生效 |
| `search_options.enable_source` | 请求返回搜索结果来源列表 | 搜索开启时true，来源保存服务器私有审计/事件；当前窗口不增加来源列表UI |
| `input_audio_transcription` | 输入语音转文字，也可设null关闭 | 监听连接开启qwen3-asr-flash-realtime，回复连接关闭；不是输入Fun-ASR |
| `turn_detection.type` | server_vad声学断句或semantic_vad语义断句 | 监听用server_vad；回复连接为null，由服务端触发 |
| `turn_detection.threshold` | VAD触发灵敏度 | 0.5，未改 |
| `turn_detection.silence_duration_ms` | 停顿多久视为一句结束 | 600ms，未改；更短可能拆开停顿中的一句话 |
| `create_response` / `interrupt_response` | VAD是否自动生成/取消回复 | 监听两项false，当前服务自行处理回复、中断和排空；不是没有自动插话功能 |
| `modalities` | 输出文本或文本＋音频 | text＋audio，未改 |
| `audio.output.voice` | 输出音色 | Tina，未改 |
| `audio.*.format` | PCM/WAV、采样率、声道等 | 当前模型接口输入16k单声道PCM、输出24k；终端侧仍用Opus，未改 |
| `tools` | Function Calling及MCP工具 | 当前未配；与内置enable_search不可同时开启 |
| `video.representation_compact` | Qwen3.8视频输入表征压缩 | 未配置；当前客户端没有图片/视频上传链路，不能仅开参数就具备看图功能 |
| `temperature` / `top_p` / `top_k` | 生成多样性、采样范围 | 未显式调整，沿用提供方默认；不是联网/记忆开关 |
| `seed` | 尽量提高相同配置下生成的可复现性 | 未设置；官方不承诺完全确定 |
| `max_response_output_tokens` | 限制单次输出Token | 未显式调整；应用自身60秒音频、4096字符等边界仍在 |
| `instructions` | 身份、风格、能力与上下文说明 | 每轮注入小满、实际入口能力、已播上下文；联网开启时添加当前北京时间和检索说明 |

## 版本限制

- `turn_detection.idle_timeout_ms`：本次官方文档只标注适用于Qwen3.5 Omni Plus/Flash Realtime且server_vad，不能据此宣称当前Qwen3.8支持主动闲聊超时开关。
- `smooth_output`：本次官方文档标注仅适用于`qwen3-omni-flash-realtime-2025-09-15`，不把它作为当前3.8的“更自然说话”开关。
- 当前公开客户端事件页未列`enable_thinking`，不能照搬其他千问接口的同名选项。
- 输入多声道、视频、MCP等需相应数据通道/工具实现和各自验收，不是配置字段存在就已接入产品。
- 回声消除、唤醒词、播放器排空、已播文字对齐与日志记忆范围是应用/设备功能，不由联网开关提供。

## 后续工具接入

判题或设备控制若走Function Calling/MCP，必须处理`tools`与内置联网搜索互斥。后续Go统一入口迁移要保留本轮搜索配置和来源证据，另行决定把天气/检索也接成工具，或采用可明确隔离的会话方式；本轮不实施这些改动。

来源：[官方客户端事件](https://help.aliyun.com/zh/model-studio/client-events)、[官方服务端事件](https://help.aliyun.com/zh/model-studio/server-events)。官方配置支持与当前账号实测分别记录，不能用接口参数存在代替运行验证。
