# TEN Framework：打断后继续位置的源码与模块实测

日期：2026-10-23。范围：只读官方源码，在隔离目录运行上游 Python 控制模块；没有修改产品代码、提交、部署、硬件操作或读取密钥。

## 结论

在本次固定提交的官方 TEN 示例中，没有找到把“用户实际已听到的字词位置”回写为 assistant 历史或独立数数任务游标的完整路径。TEN 的中断/flush 能停止或丢弃工作，但不能据此认定它自动解决“只听到 8，插话算术后从 9 继续”。

- **链式示例（ASR → LLM → TTS）**：assistant 内容在生成 delta 时写入 `contexts`。flush 清队列并 abort 当前请求，不裁剪 `contexts`。实际执行上游控制代码后，数学请求和继续请求都仍携带完整 1–50。
- **原生实时示例（OpenAI speech-to-speech）**：实际 `conversation.item.truncate` 发送逻辑整段被注释。`ResponseAudioDone` 分支是 `pass`；收到服务端 speech-start 后通知主控 flush RTC。没有证明服务器历史已按已播内容裁剪。
- **Qwen 边界**：完整 GitHub API 树内没有 Qwen 原生 realtime/omni mllm 扩展。本仓库存在 `qwen3_tts_python`，它是本地 Torch/Qwen3TTSModel 文本转语音，不是 Qwen Omni Realtime API。未把 OpenAI 可改 URL 的接口推断为 Qwen 原生兼容，未准备冒充 TEN Qwen 集成的连接脚本。

这些证据支持“缺少实际播放进度与任务状态的连接”这一工程根因。它们不能证明任何厂商模型的最终续数起点，也不能证明给出正确已播历史后模型一定可靠。

## 固定来源与依赖

官方仓库：[TEN-framework/ten-framework](https://github.com/TEN-framework/ten-framework/tree/1493ed2394c1c972dc79c0f4d567579954f9addf)，commit `1493ed2394c1c972dc79c0f4d567579954f9addf`，提交时间 `2026-10-22T07:41:16Z`。官方 GitHub recursive tree 共 35,670 项，`truncated=false`。

| 示例 | ten_ai_base 锁定版本 | 锁文件包哈希 | runtime 锁定版本 |
|---|---|---|---|
| `voice-assistant` | `0.7.41` | `ad64eef476fc583a5021559ede87f2071440128180c63d43f03acb7c46245895` | `ten_runtime_python 0.11.66`（本次未运行 native runtime） |
| `voice-assistant-realtime` | `0.7.22` | `27e5467c5dc841757f5a6f52e70b0d2c09903be84ade73b404b45c6cac9888f8` | `ten_runtime_python 0.11.27`（本次未运行 native runtime） |

依赖从 TEN 官方 registry 的精确版本下载，不做升级替换。链式与实时分别在独立 Python 进程加载其锁定 `ten_ai_base`。此前探索性混用 `0.7.41` 的 realtime 输出保留为 `mixed-version-initial-results.json`，不作为官方 realtime 锁定组合的依据。最终结果均来自各自锁定版本。

下载的 41 个官方仓库源码/配置文件，均以 Git blob SHA-1 重算后与固定 tree 中 SHA 对比，41/41 一致。见 `artifacts/resume-root-cause-20261023/ten/source-integrity.json`。包归档 SHA-256：

- `ten_ai_base-0.7.41.tpkg`：`cdff326f88576182143b53582b7e008699b9ed7616dc9b2593f009b38ef1ea6e`
- `ten_ai_base-0.7.22.tpkg`：`407dc81f15a8db5081a5d9f37b51a9c6f6d559ad96314df9813c0a977760d42f`

归档 SHA-256 与 registry 的“包内容 hash”是不同概念，上表单独保留。运行环境是 macOS arm64 / Python 3.13.3，Pydantic 2.14.1（官方包声明 `pydantic>=2`），CPU 依赖精确清单在 `python-packages.txt`；没有安装 Torch、模型权重或 Docker。

## 链式路径追踪

以下路径均相对于固定官方仓库根目录。

| 环节 | 文件与行号 | 行为及含义 |
|---|---|---|
| 插话触发 | `ai_agents/agents/examples/voice-assistant/tenapp/ten_packages/extension/main_python/extension.py:83–95` | ASR final 或文本超过 2 字符调用 `_interrupt()`；final 再送 LLM 输入。 |
| 中断下发 | 同文件 `203–213` | 清 `sentence_fragment`，`agent.flush_llm()`，发 `tts_flush`，向 `agora_rtc` 发 `flush`。 |
| 生成历史 | `.../main_python/agent/llm_exec.py:181–197` | 每个 `LLMResponseMessageDelta` 将已生成累计 `content` 写 assistant 上下文，不等扬声器确认。 |
| 历史替换 | 同文件 `126–149` | `_write_context` 替换最后同角色消息的内容，或追加新消息。 |
| 下轮请求 | 同文件 `151–169` | 从 `self.contexts.copy()` 生成下一次 `LLMRequest.messages`。 |
| flush | 同文件 `66–79` | `input_queue.flush()`、发送 abort、cancel task；没有删除或裁剪 assistant 内容。 |
| TTS 结束回调 | `.../main_python/agent/agent.py:140–154` | 收到 `tts_audio_start` / `tts_audio_end` 直接 `return`。 |
| 事件布线 | `ai_agents/agents/examples/voice-assistant/tenapp/property.json:130–154` | TTS start/end 从 TTS 扩展送 main，不是扬声器播放确认。该示例配置不路由 `tts_text_result`。 |

官方精确源码链接：[LLMExec](https://github.com/TEN-framework/ten-framework/blob/1493ed2394c1c972dc79c0f4d567579954f9addf/ai_agents/agents/examples/voice-assistant/tenapp/ten_packages/extension/main_python/agent/llm_exec.py#L66)、[Agent 忽略 TTS start/end](https://github.com/TEN-framework/ten-framework/blob/1493ed2394c1c972dc79c0f4d567579954f9addf/ai_agents/agents/examples/voice-assistant/tenapp/ten_packages/extension/main_python/agent/agent.py#L151)。

TEN base 有可选字词时间信息：`ten_ai_base/struct.py:14–34` 定义 `TTSWord(word,start_ms,duration_ms)` 和 `TTSTextResult.words`；`tts2.py:444–447` 能发送 `tts_text_result`。但数据类型存在不等于本示例已用其裁剪 LLM history。`tts2.py:494–523` 的 `tts_audio_end` 包含 request 音频总时长、结束原因、metadata，是 TTS 模块输出事件；它本身不证明扬声器已播放完。

## 原生实时路径追踪

文件：`ai_agents/agents/ten_packages/extension/openai_mllm_python/extension.py`。

| 环节 | 行号 | 行为 |
|---|---|---|
| 音频输出 | `352–361` | 若 response_id 未在 flushed，则解 base64 并发送音频帧。 |
| 音频生成结束 | `362–363` | `ResponseAudioDone` 为 `pass`。 |
| speech-start 截断 | `368–382` | `ItemTruncate(audio_end_ms=...)` 与发送语句全部被注释；AST 没有活动 truncate/cancel 调用。注释中的 session 时间也不等于设备实际播放时长。 |
| 中断广播 | `383–386` | `send_server_interrupted()`，主控 realtime 示例 `extension.py:142–144,251–255` 收到后只 flush `agora_rtc`。 |
| transcript 标记 | `387–407` | 使用**已生成** `response_transcript + '[interrupted]'`；本意并非按字词已播位置截断。 |
| 本地上下文恢复 | `545–554` | `_resume_context` 按外部提供的 message_context 逐项发送，未构建独立任务进度。 |

官方链接：[禁用的 truncate 路径](https://github.com/TEN-framework/ten-framework/blob/1493ed2394c1c972dc79c0f4d567579954f9addf/ai_agents/agents/ten_packages/extension/openai_mllm_python/extension.py#L364)、[realtime 主控 flush](https://github.com/TEN-framework/ten-framework/blob/1493ed2394c1c972dc79c0f4d567579954f9addf/ai_agents/agents/examples/voice-assistant-realtime/tenapp/ten_packages/extension/main_python/extension.py#L251)。

附带发现：上述分支 `extension.py:394` 传 `delta=None`，而 realtime 锁定的官方 `ten_ai_base 0.7.22/interface/ten_ai_base/struct.py:319–327` 将 `delta` 声明为 `str`。在声明允许的 Pydantic 2.14.1 下实际触发 `ValidationError`，异常在扩展事件循环中被捕获，因此 `flushed.add(response_id)` 未执行；实验继续送同一 response 的 audio delta，仍输出第二帧。该结论是**固定源码 + 精确 base 版本 + 所列 Pydantic 的 Python 模块组合实测**，不是完整官方 Docker/runtime/真实网络的端到端缺陷验证，也不把它当成设备继续位置问题的唯一根因。没有修补该上游问题。

## 实际运行的隔离实验

运行的是原文件 import 后的方法，未摘抄或重写其 history/flush 逻辑：

- 链式：`MainControlExtension`、`Agent`、`LLMExec`，真实 TEN base `AsyncQueue`、Pydantic struct/parser。
- 实时：`OpenAIRealtime2Extension.start_connection` 的完整 async event loop、真实官方 realtime event dataclass、真实 TEN base `AsyncMLLMBaseExtension` 输出封装。
- 替身：`ten_runtime` 的消息/命令/音频帧壳与收发接口、厂商网络连接、ASR/LLM 返回数据。没有启动 TEN native C runtime、Agora 网络、真实模型或扬声器。
- `harness_heard_through=8` 是外部 fixture。`tts_audio_end.metadata.harness_heard_through` 也不是 TEN 官方播放进度字段；它用于显示现有 handler 忽略该事件，不是设备上报验证。1–50 文本、数学回复 2 都是受控厂商返回，不用于宣称模型能力。
- 模块加载的实际绝对路径逐项写入结果 JSON 的 `environment.loaded_upstream_modules`；源码完整性见独立 hash 清单。

命令（仓库根目录运行）：

```sh
.superpowers/resume-root-cause-20261023/ten/.venv/bin/python \
  .superpowers/resume-root-cause-20261023/ten/test_actual_modules.py 0.7.41 chained
.superpowers/resume-root-cause-20261023/ten/.venv/bin/python \
  .superpowers/resume-root-cause-20261023/ten/test_actual_modules.py 0.7.22 realtime
```

两条命令均退出 0，断言与结果如下（realtime 日志保留预期的上游 ValidationError traceback）：

| 情境 | 实际观察 |
|---|---|
| 链式生成 1–50，fixture 已听 8，插话 `1加1等于几` | 发出 abort/TTS flush/RTC flush；数学请求 history 仍有完整 assistant 1–50。 |
| 数学回复 2，再输入 `继续刚才数数` | 新请求依次含 user 1–50 目标、assistant 完整 1–50、user 数学、assistant 2、user 继续；未携带 heard=8 或 next=9。没有真实调用模型，故不声称模型实际从哪个数继续。 |
| 检查 LLMExec 可见状态 | 只有 `current_task`、`contexts`、`current_request_id`、`current_text` 等；该路径没有已播字词位置或独立数数任务游标。 |
| realtime 全文 transcript 已到 50、响应仍 active，fixture 已听 8 后 speech-start | 发送 `mllm_server_interrupted`；0 个协议 truncate/cancel 请求；触发上述 transcript validation error，后续同 response 音频仍通过（两个 4,800-byte 帧）。 |
| realtime 生成已 done、客户端仍未播完时 speech-start | 发送 interrupt；0 个协议 truncate/cancel 请求；本地 `message_context=[]`，未观察到按已播内容改写。服务器内部历史不在该离线实验可观测范围。 |

结果与日志：

- `artifacts/resume-root-cause-20261023/ten/actual-chained-0.7.41-results.json`
- `artifacts/resume-root-cause-20261023/ten/actual-realtime-0.7.22-results.json`
- `artifacts/resume-root-cause-20261023/ten/actual-module-results.json`（仅合并最终正确锁定版本）
- `artifacts/resume-root-cause-20261023/ten/actual-chained-0.7.41-run.log`
- `artifacts/resume-root-cause-20261023/ten/actual-realtime-0.7.22-run.log`
- `artifacts/resume-root-cause-20261023/ten/source-integrity.json`
- `artifacts/resume-root-cause-20261023/ten/ast-truncate-cancel-calls.json`

未运行 upstream 集成测试、完整 TEN 图或厂商联网测试；选择了可运行的实际框架 Python 模块实验替代。不能从本实验给出“TEN + Qwen 实际连接、续数 PASS”或“精确实播裁剪 PASS”。

## 对当前机器人问题的借鉴边界

flush、取消生成、对话历史修正、实播位置、独立任务状态是不同环节。TEN 示例提供的中断机制可作为停止工作的参考；它没有给出经过验证的“1–50 数数任务在插话后保留目标，按耳朵已听至 8 从 9 恢复”的现成闭环。若要对照当前系统，应分别验证数数目标是否仍在、history 表达的是已生成还是已听、完成事件是否来自真正播放端，以及数学任务结束后恢复哪个任务。仅换成该 TEN 示例并不能根据现有证据保证位置问题消失。
