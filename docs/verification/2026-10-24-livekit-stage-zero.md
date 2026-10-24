# LiveKit P0：首个 SDK 播放回报契约验证

2026-10-24。按最新要求，运行环境与测试放在云服务器；Mac 未启动服务或运行此 SDK 测试。**只确认 P0 中单段 AudioOutput 探针通过，不代表 P0 全部完成或新链路上线。**

## 环境与隔离

- 独立 checkout：`/opt/family-robot/dev/livekit-stage-zero-20261024`，起点源码 `e3e2559`；候选新增源码为 `tools/livekit-audit/`。
- Ubuntu 24.04、Linux `6.8.0-71-generic` x86_64、Python 3.12.3。
- 独立 venv：该 checkout 的 `.superpowers/livekit-stage-zero/venv`；未复用旧审计或正式环境。
- 实际发行包：Agents 1.8.8、RTC 1.1.20、av 19.0.2、aiohttp 3.14.5。
- `pip check` 返回 `No broken requirements found.`。完整解析清单与安装日志留在服务器私有开发目录；`resolved.txt` SHA256：`adf1783acd1a75dc62e0730276e459f06ba254c3c2ee15019b92e328668061ff`。
- 开始前旧 `family-robot.service` active，8000 `/healthz` 返回 `status=ok`、`active_sessions=0`。

没有切换旧服务、修改正式配置、增加公网端口、刷固件、采集麦克风/扬声器或调用模型。现有 Key 只在服务器检查配置存在性，未输出值或复制到开发电脑。

## 计划差异

原计划固定 Agents 1.8.8 与 RTC 1.1.21，实际 pip 解析失败。安装的 Agents 元数据明确要求 `livekit==1.1.20`，因此修正直接依赖，保留 Agents 版本；未跳过依赖检查。

原静态参考提交 `7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964` 与发行包 IO 源码不是逐字一致。参考版本包含额外的 `_CaptureCancelledEvent` 转发等变更。本轮只验证发行包实际提供的单段回报语义，不能把参考提交的取消修复算作发行包能力。取消早于首帧、wrapper FIFO 与完整 AgentSession 接线留作下一项真实 SDK 验证。

| 源码 | SHA256 |
|---|---|
| 受测发行包 `voice/io.py` | `c01fac1628525ef0eb19125fa7e9f37c4406538a24bc441a5b1e1851e99aeeb3` |
| 受测发行包 `voice/agent_session.py` | `f9afc6c0f727724d5d681bb0bb193daee220e1db9f4da1a1155b830e258193b7` |
| GitHub 连接器取得的参考提交 `io.py` | `d9c19270ab676a34ddfd62f4214e1f1ae46f9a5ba61865d610fe088eed74f6cd` |

## 真实红绿结果

在服务器运行：

```sh
PYTHONPATH=tools/livekit-audit .superpowers/livekit-stage-zero/venv/bin/python -m unittest discover -s tools/livekit-audit -p test_device_sink_probe.py -v
```

先仅创建依赖和测试，实际报 `ModuleNotFoundError: No module named 'device_sink_probe'`、`FAILED (errors=1)`，退出 1；此时尚未创建实现。

再创建继承真实 SDK `AudioOutput` 的探针，重跑同一命令：

```text
test_clear_uses_partial_finish_and_old_id_cannot_finish_new ... ok
test_flush_waits_for_matching_device_finish ... ok
test_invalid_receipt_cannot_be_upgraded_to_measured_completion ... ok
Ran 3 tests in 0.150s
OK
GREEN_EXIT=0
```

完整日志在服务器隔离 checkout 的 `.superpowers/livekit-stage-zero/red.log` 与 `green.log`。回报来自进程内模拟，不是 Mac 或手表实测播放。

## 阶段出口

首个 SDK 探针实际通过。完整语音管线、实时麦克风客户端、生产播放适配器、准确自动接续仍未交付。下一步验证真实 AgentSession 生命周期/固定稿接口和三模型能力；随后按固定音频、模拟终端、Mac 耳机、Mac 外放、最终手表的顺序分层验收。
