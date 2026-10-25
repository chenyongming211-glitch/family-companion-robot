# LiveKit SDK 接入审计

> **历史验证参考（2026-10-25）。** 此SDK审计属于已弃用的三段式方案，按需保留，不是当前Go服务的运行依赖，也不代表LiveKit接入仍在推进。见[代码生命周期](../../docs/CODE_LIFECYCLE.md)。

服务器上的独立契约探针，不是设备适配器或可通话服务。[真实验证结果](../../docs/verification/2026-10-24-livekit-stage-zero.md)。

## 环境与运行

依赖安装、测试和后续服务都在云服务器执行；Mac 仅编辑代码、同步 Git，并作为之后的测试终端。不要在本机启动语音服务或复制服务器凭据。

本轮独立 checkout：`/opt/family-robot/dev/livekit-stage-zero-20261024`。正式服务、8000 端口和正式记录目录均未修改。在服务器独立 checkout 根目录执行：

```sh
python3.12 -m venv .superpowers/livekit-stage-zero/venv
.superpowers/livekit-stage-zero/venv/bin/python -m pip install -r tools/livekit-audit/requirements.txt
.superpowers/livekit-stage-zero/venv/bin/python -m pip check
.superpowers/livekit-stage-zero/venv/bin/python -m pip freeze --all > .superpowers/livekit-stage-zero/resolved.txt
PYTHONPATH=tools/livekit-audit .superpowers/livekit-stage-zero/venv/bin/python -m unittest discover -s tools/livekit-audit -p 'test_*.py' -v
```

固定 Agents 1.8.8、RTC 1.1.20。原计划的 RTC 1.1.21 与 Agents 发行包的精确依赖冲突；没有使用 `--no-deps` 或修改 SDK。计划引用的 Git 源码 `7a7547e` 与发行包不是逐字一致，受测源码摘要见报告。

## 已验证的契约

- `flush()` 只封口；同一块、范围合法且完整的模拟设备回报才结束正常播放等待。
- `clear_buffer()` 后可用同块的部分前缀结算中断；旧块回报不能完成新块。
- 匹配块收到显式 `valid=False` 回报后，后续有效回报不能升级为实测完成。越界采样数单独拒绝，不使该块永久失效。

测试直接使用真实 SDK 的 `AudioOutput`、`wait_for_playout()` 和 `rtc.AudioFrame`，不 mock SDK。输入是内存中的静音 PCM，回报由测试提交；没有模型调用、网络监听、麦克风、扬声器或手表操作。

## 边界与下一步

只支持一个在途播放段，不保存 PCM。`receipt()` 是进程内测试入口，不是可直接暴露的网络协议。没有生产必需的 250ms 有界结算、unknown 降级、旧回调 FIFO、多段并发、重采样、音频队列和停止指令；无效/缺失回报会继续等待，测试主动取消等待，不能直接承接用户语音。

后续已在[`services/livekit-agent/`](../../services/livekit-agent/README.md)完成真实AgentSession自定义I/O、指定文字不提前入历史、取消后新输出，以及ASR/LLM/TTS实际权限和完整链路验证，见[后续验证报告](../../docs/verification/2026-10-24-livekit-voice-chain.md)。此处单段探针保留为SDK契约回归测试。

接下来接同一新链路的Mac真实收音/播放客户端。工具后抑制重复生成、Go任务/存储接线、自动接续和手表声学与播放坐标仍需分别验收。
