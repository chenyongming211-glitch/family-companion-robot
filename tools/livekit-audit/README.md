# LiveKit SDK 接入审计

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

P0 尚需验证真实 AgentSession 自定义 I/O、指定原稿播报、不提前写入历史、抑制工具后重复生成、取消后允许新输出，以及 ASR/LLM/TTS 端点权限、事件与时间信息。之后接同一新链路的 Mac 客户端，最后做手表声学与播放坐标验收。
