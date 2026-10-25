# Realtime实播边界与上下文截断：公开协议复核

日期：2026-10-25。本轮核对当前代码、仓库既有探针报告及官方公开文档，不调用付费模型、不切换服务。文档支持不等于本账号/终端已实测；“未确认”不表示所有版本均不支持。

## 当前实验如何定位

范围为`tools/realtime-output-live/`、云端回环18032的实验入口。Mac按回复序号记录CoreAudio `dataPlayedBack`已完成的连续完整音频包，24kHz下以完成采样数除以24000换算秒数；每包60ms，不是外部声学逐字测量。

仅实际下发的输出Opus包在服务端解码，再旁路送Fun-ASR。最终ASR结果提供字词起止时间。`alignment.py`选择结束时间不晚于已播时长减120ms的前缀，与生成转写去标点后校验；120ms为未标定的实验保护量。数字表示差异、词组粒度和识别错误仍会使定位保守或unknown，不能把候选前缀当作人耳最后一个字的确定证明。

当前以应用侧重建上下文实现隔离：取消旧生成及下发、客户端停播并回报，下一轮新建千问生成会话，只携带最近对话的候选已播前缀和当前原音频。不是调用千问原生按毫秒截断现有音频历史。

## 必须区分的能力

1. 停止继续生成。
2. 客户端清除尚未播放的音频缓冲。
3. 修改模型会话历史，去除终端尚未播放的部分。
4. 将实际播放时间可靠映射到完整字词/句子，并能查询或从同一原稿继续。

前三者也不自动证明第四者；上下文长度压缩、删除整轮、网络下发结束均不等同于按终端实播边界截断。

## 官方资料对照

| 模型/服务 | 本轮可确认 | 尚未确认或限制 |
|---|---|---|
| OpenAI Realtime | WebSocket客户端用`conversation.item.truncate`及`audio_end_ms`同步播放边界，成功有truncated回执 | 客户端仍须提供正确时间；官方说明不返回精确裁剪后的文字转写，不保证同稿逐字续播 |
| Gemini Live | VAD中断取消生成；标准流程保留已发送给客户端的内容 | 已发送不等于已播；不能把此流程宣称为与OpenAI实播毫秒截断等价。参考页有playback status reporting提示，但本轮未取得可据此直接实施同等回滚的完整公开契约 |
| Qwen3.8-Omni-Flash-Realtime | response.cancel等事件、工具结果回传 | 当前客户端事件文档未列原生实播毫秒截断；仓库10月22—23日的探针未收到truncate有效确认。今天未重跑该探针 |
| StepAudio 3 / 2.5 Realtime | 官方协议列取消生成、创建/删除对话项 | 未在所读协议中找到按输出实播毫秒截断的事件；speech_stopped里的audio_end_ms是用户输入语音边界，不能当成输出截断字段 |
| GLM-Realtime | 官方SDK文档列服务端/客户端VAD中断和上下文添加 | 未在所读公开协议中找到终端输出实播毫秒截断契约 |
| 豆包实时语音3.0（Seeduplex） | 官方检索内容列conversation.item.update及上下文查询/更新 | 完整文档是JS页面，本轮文本获取受限、浏览器读取超时；只将检索可见的上下文更新列为线索，不确认原生实播毫秒截断 |
| 腾讯云媒体处理S2S接入服务 | 文档列出truncate事件形状，同时明确部分能力返回not_supported | 服务/后端别名不能自动视为某个国产模型的支持证明；cancel加clear能停播，但文档的替代建议本身不足以证明历史按实播毫秒修改 |

## 来源

- [OpenAI Realtime会话管理](https://developers.openai.com/api/docs/guides/realtime-conversations)、[客户端事件](https://developers.openai.com/api/reference/resources/realtime/client-events)。
- [Gemini Live能力指南：VAD](https://ai.google.dev/gemini-api/docs/live-api/capabilities)、[Live API参考](https://ai.google.dev/api/live)、[Firebase Live中断说明](https://firebase.google.com/docs/ai-logic/live-api/configuration)。
- [千问客户端事件](https://help.aliyun.com/zh/model-studio/client-events)；仓库既有实测见[上游契约审计](2026-10-22-upstream-realtime-contracts.md)与[10月23日比较](2026-10-23-realtime-comparison-results.md)。
- [StepFun双向Realtime协议](https://platform.stepfun.ai/docs/en/api-reference/realtime/chat)、[StepAudio3实时示例](https://platform.stepfun.ai/cookbook/stepaudio-3-realtime)。
- [MetaGLM官方SDK协议](https://github.com/MetaGLM/glm-realtime-sdk/blob/main/GLM-Realtime-doc-for-llm.md)。
- [豆包3.0官方文档](https://docs.volcengine.com/docs/DoubaoVoice/endtoend-realtime-voice-full-duplex-version?lang=zh)；仅检索摘要可读的限制如上。
- [腾讯云S2S接入协议](https://cloud.tencent.com/document/product/862/137503)。

本轮纠正此前将Gemini与OpenAI统称为“均能按实际已播位置截断”的表述。国内后续验证应固定模型版本与账号，检查明确事件/回执、历史实际变化、不同缓冲长度下的真人接续；仅看到compatible、支持打断、支持上下文管理不算验收。当前不切换模型、不扩展身份功能开发。
