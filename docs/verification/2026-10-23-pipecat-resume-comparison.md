# Pipecat 插话后继续位置：原版框架隔离对照

调查日期：2026-10-23。固定官方 `pipecat-ai/pipecat` main commit：`3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9`，提交时间 `2026-10-23T01:20:19Z`。通过 GitHub connector 读取官方当前 commit，并浅克隆同一 HEAD；未修改上游文件。

## 结论

Pipecat 的框架级控制能清掉未释放的音频/文本，并将已经释放的文本提交给上下文；它没有从“继续”这两个字推导设备究竟播到哪个数字的通用保证。实际实验同时复现了精准、保守和超前三种上下文结果，区别来自文本释放时机和时间轴是否与播放端同步。

该 commit 的 Qwen 官方集成是 **OpenAI-compatible 文本 `QwenLLMService`**，不是 Qwen 原生 Realtime speech-to-speech：`src/pipecat/services/qwen/` 只有 `__init__.py` 和 `llm.py`；类继承 `OpenAILLMService`，默认 `qwen-plus`，使用 HTTPS `compatible-mode/v1`。源码搜索未发现 Qwen Realtime service。因此本调查不声称 Pipecat 原版已真实连接 Qwen Realtime，也没有使用“文本 Qwen”替代原生音频协议做结论。

## 实际执行

- 在 `.superpowers/resume-root-cause-20261023/pipecat/venv` 创建 Python 3.13.3 隔离环境，editable install 固定 checkout。
- 上游测试：`test_base_output_transport.py`、`test_word_timestamp_tracker.py`、`test_aggregated_frame_sequencer.py`、`test_context_aggregators_universal.py`：**305 passed + 11 subtests passed**，35.07 秒，1 条弃用警告。
- 上游 Realtime 测试：`test_openai_realtime_audio_frames.py`、`test_openai_realtime_user_audio.py`：**15 passed**，0.26 秒。
- 自建实验脚本运行 **5 个有断言的场景**，使用真实 `BaseOutputTransport`、`LLMAssistantAggregator`、`LLMContext`、`WordTimestampTracker`、`AggregatedFrameSequencer`、`TTSService._push_spoken_text`、`OpenAIRealtimeLLMService._push_output_transcript_text_frames` 和 `_truncate_current_audio_response`。
- 没有模型调用、没有网络推理、没有读取凭据、没有设备或线上改动、没有提交。没有安装 Torch/CUDA 或下载模型。完整环境见 `pip-freeze.txt`。

## 最小实验的输入与边界

生成文本固定为 `1 2 3 ... 50`。每个数字对应一块 **100 ms 合成标记 PCM**，只在内存创建；没有真实人声，也没有保存原始音频。测试 sink 完成接收第 1–8 块，然后在第 9 块阻塞。触发原版 `InterruptionFrame` 后，框架提交本地 assistant 上下文。随后通过真实 aggregator 输入数学回答 `2`，并向真实 `LLMContext` 添加用户“继续”。

自建 harness 仅替换外部 sink 和 processor 连线；`OfflineProviderTTS` 只填充抽象 provider hook，并且调用该 hook 会直接断言失败。它没有重写被测上下文/截断算法。无 timestamp 两个 TTS 场景调用上游原版 `_push_spoken_text`，将整段文本排在该段音频后。

这里的“只播 1–8”严格表示**模拟 sink 只成功接收 1–8**，没有证明扬声器实际播了哪些字。实验只检查“继续”之前的完整上下文，**没有调用模型生成续数答案**，因此没有声称模型必然从 9 开始。

| 场景 | 模拟 sink 成功接收 | 插话时已写入的计数上下文 | 原因 |
| --- | --- | --- | --- |
| 有逐词 timestamp，PTS 与 sink 时间对齐 | 1–8 | `1 2 3 4 5 6 7 8` | 时钟队列仅释放到第 8 词；9–50 被中断清掉 |
| 有逐词 timestamp，只把 sink 每块接收耗时从 100 ms 改成 200 ms | 1–8 | `1 2 ... 16` | 独立 PTS clock 已走到 1.6 秒，sink 实际仅接收 0.8 秒音频 |
| 无 timestamp，1–50 一整段，文本排在整段音频后 | 1–8 | 没有该轮 assistant 计数消息 | 整段文本仍排在剩余音频后，被一起丢弃 |
| 无 timestamp，每 5 个数字一段，文本排在各段音频后 | 1–8 | `1 2 3 4 5` | 第一段完成已提交，6–10 段尚未完成 |
| 上游 OpenAIRealtime transcript 全文先到，无 timestamp | 1–8 | `1 2 ... 50` | 原版 Realtime transcript 方法生成无 PTS 的 TTSTextFrame，先于音频释放给 aggregator |

所有场景的最终上下文结构都是：

```text
user: 从1数到50
assistant: [上表中已经提交的计数文本；整段未完成场景无此消息]
user: 一加一等于几？
assistant: 2
user: 继续
```

完整原文、所有 frame 释放结果和完整消息数组见 `frame-context-results.json`。脚本也断言 sequencer 清理旧 context 后，其迟到的 `9` word event 被原版 stale gate 丢弃。

## 截断上下文与播放进度的差异

原版 `OpenAIRealtimeLLMService._truncate_current_audio_response` 实测产生：

```json
{"type":"conversation.item.truncate","item_id":"simulated-item","content_index":0,"audio_end_ms":800}
```

这是用固定墙钟和 5 秒已生成 PCM 驱动原版方法后截获的**发送事件**。没有真实服务器、没有 truncate ACK，也没有验证远端 history。方法本身不会把本地 `LLMContext` 从全文改写为某个已播前缀。

更关键的是，源码的 `start_time_ms` 在**首次收到模型 audio delta**时记录；截断长度为 `min(墙钟经过时间, 已接收生成 PCM 时长)`。它没有读取 ESP32 的播放 ACK 或 DAC 消费游标。TTS 逐词 timestamp 的起点也在首块 TTS 音频向 pipeline 发出时启动，transport 的 clock queue 和 audio write queue 独立运行。第二个实验证明只要 sink 消费落后，timestamp 文本仍能超前。

对于原生 Realtime，远端 conversation 和本地 LLMContext 还需分开：上游只在初始/重建 conversation 时把完整本地消息转成远端 conversation items；连续 session 的后续 context frame 不会自动重建远端完整会话。不能仅凭本地 transcript 推断远端模型续数位置，也不能仅凭发出的 truncate 事件宣称远端已经按真实播放位置截断。

## 固定源码证据

以下路径均相对于固定上游 checkout；链接锁定同一 commit。

| 证据 | 文件/行号 |
| --- | --- |
| Qwen 仅文本 OpenAI-compatible，默认 HTTPS/qwen-plus | [`services/qwen/llm.py:24–77`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/qwen/llm.py#L24-L77) |
| output 根据 `frame.pts` 分时钟队列/音频同步队列 | [`transports/base_output.py:393–423`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/transports/base_output.py#L393-L423) |
| 中断清 interruptible audio/clock 队列，已开始写出的当前块不取消 | [`transports/base_output.py:609–641`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/transports/base_output.py#L609-L641) |
| 音频写成功才向下游发音频 frame | [`transports/base_output.py:1019–1050`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/transports/base_output.py#L1019-L1050) |
| timed frame 按 clock 到期即向下游发出，不等 audio sink ACK | [`transports/base_output.py:1206–1231`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/transports/base_output.py#L1206-L1231) |
| TTSTextFrame 类型/字段 | [`frames/frames.py:518–528`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/frames/frames.py#L518-L528) |
| assistant 中断先 flush 聚合，再 reset | [`processors/aggregators/llm_response_universal.py:2020–2022`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/processors/aggregators/llm_response_universal.py#L2020-L2022) |
| assistant text 的 append_to_context / raw_text 聚合 | [`processors/aggregators/llm_response_universal.py:2386–2430`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/processors/aggregators/llm_response_universal.py#L2386-L2430) |
| 聚合提交 assistant context message | [`processors/aggregators/llm_response_universal.py:1946–1982`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/processors/aggregators/llm_response_universal.py#L1946-L1982) |
| 无逐词 timestamp，文本在本段音频后追加 | [`services/tts_service.py:1704–1730`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/tts_service.py#L1704-L1730) |
| TTS 首个 audio item 时启动 word clock，再向下游推音频 | [`services/tts_service.py:1919–1964`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/tts_service.py#L1919-L1964) |
| 词时间转 PTS / stale word gate / 带 context 的 word frame | [`utils/context/aggregated_frame_sequencer.py:457–589`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/utils/context/aggregated_frame_sequencer.py#L457-L589), [`:923–955`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/utils/context/aggregated_frame_sequencer.py#L923-L955) |
| Realtime transcript 生成无 PTS TTSTextFrame | [`services/openai/realtime/llm.py:1154–1170`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/openai/realtime/llm.py#L1154-L1170) |
| Realtime audio 首次接收墙钟作为截断起点 | [`services/openai/realtime/llm.py:976–1020`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/openai/realtime/llm.py#L976-L1020) |
| Realtime 截断公式和发送事件 | [`services/openai/realtime/llm.py:620–657`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/openai/realtime/llm.py#L620-L657) |
| 连续 context 更新与初始 conversation setup 的不同 | [`services/openai/realtime/llm.py:729–753`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/openai/realtime/llm.py#L729-L753), [`:1292–1310`](https://github.com/pipecat-ai/pipecat/blob/3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9/src/pipecat/services/openai/realtime/llm.py#L1292-L1310) |

## 复现命令

```bash
cd '/Users/chen/code/家庭陪伴机器人/.superpowers/resume-root-cause-20261023/pipecat'
uv venv --python 3.13 venv
env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
  uv pip install --python venv/bin/python -e ./upstream pytest pytest-asyncio pytest-xdist
./venv/bin/python frame_context_probe.py
cd upstream
../venv/bin/python -m pytest -n 0 \
  tests/test_base_output_transport.py tests/test_word_timestamp_tracker.py \
  tests/test_aggregated_frame_sequencer.py tests/test_context_aggregators_universal.py
../venv/bin/python -m pytest -n 0 \
  tests/test_openai_realtime_audio_frames.py tests/test_openai_realtime_user_audio.py
```

首次安装受当前 shell 代理拒绝连接影响；仅在安装子进程去掉常见 proxy 变量后成功，未查看环境值。Python venv 不带 pip，环境清单使用 `uv pip freeze --python venv/bin/python` 保存。

## 对本项目根因的可迁移判断

这组实验支持把问题拆成三项独立证据：模型已生成文本、framework/media timeline 已释放文本、设备真正消费的音频游标。Pipecat 原版提供前两项的队列和时间调度机制；没有自动把它们变成第三项。数学答案 `2` 追加后也不会自动修复此前计数历史。要判断本项目的“继续”位置，需要进一步对齐本项目远端会话 item/截断确认与设备消费进度；不能从本报告直接宣布原生 Qwen 或实机通过。

## 独立诊断脚本的归档要求

`frame_context_probe.py` 可以作为本仓库 `tools/voice-audit` 下的独立离线诊断脚本归档；当前调查未修改该目录。运行它只需要 Python 3.11+（实际验证 3.13.3）和上述固定 commit 的 Pipecat 基础包，不需要任何 provider extras、pytest、模型、API key、设备连接。`loguru`、`openai`、音频 resampler 等由 Pipecat 基础依赖提供。重跑上游测试时才另外需要 `pytest`、`pytest-asyncio`、`pytest-xdist`。构建 Pipecat 源码需要其声明的 `setuptools>=64`、`setuptools_scm>=8`。

归档时应把输出目录改为显式参数，保留无凭据运行和模拟边界说明，并在源码 manifest 记录 upstream URL、完整 commit 和被测文件 hash。不要只复制几个 Pipecat 类：仅导入此脚本就在当前环境实际加载了 151 个 Pipecat 源码文件，运行依赖其正常包结构和传递依赖。最小可靠复现单位是**本仓库自编 harness + 固定 commit 的完整 Pipecat 包**。

脚本直接使用的主要上游路径是 `frames/frames.py`、`clocks/system_clock.py`、`processors/frame_processor.py`、`processors/aggregators/llm_context.py`、`processors/aggregators/llm_response_universal.py`、`transports/base_output.py`、`transports/base_transport.py`、`services/tts_service.py`、`services/openai/realtime/llm.py`、`utils/asyncio/task_manager.py`、`utils/context/aggregated_frame_sequencer.py`、`utils/context/word_timestamp_tracker.py`、`utils/text/base_text_aggregator.py`；`services/openai/realtime/events.py` 和基础工具模块也属于传递依赖。Qwen 原生支持的审计证据还应保留 `services/qwen/llm.py` 的固定 commit/hash。

官方 Pipecat `LICENSE` 为 **BSD-2-Clause，Copyright (c) 2024–2026, Daily**。自编 harness 可以按本仓库的 MIT 许可归档；Pipecat 仍保留自己的 BSD-2-Clause 许可和版权，不应被重新标为 MIT。若只归档自编脚本并由用户安装固定上游包，可在第三方说明中记录来源/commit/许可；若另外分发上游源码或二进制，则按该 LICENSE 保留版权、两项条件和完整免责条款。不要将 venv、第三方源码 checkout、生成音频或固件一并提交。

## 归档后复验

上述“未提交/可归档”描述原调查阶段。随后父任务已将带source/out参数的副本保存为[pipecat_resume_probe.py](../../tools/voice-audit/pipecat_resume_probe.py)，并在同一固定checkout/venv重跑5个场景，断言全部通过。原始脚本与portable副本身份分别保留；没有新增模型调用或上游源码改动。
