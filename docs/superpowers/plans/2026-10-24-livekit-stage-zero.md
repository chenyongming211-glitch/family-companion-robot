# LiveKit设备接入第一阶段实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 当前默认原线程执行；未获授权不派子代理。

**Goal:** 先用真实LiveKit SDK证明“音频封口不等于设备播完；匹配设备回报才能结算”，为新链式后端锁定接入契约。

**Architecture:** 第一阶段仅增加无监听、无模型调用的独立SDK契约探针，不修改正式Go网关。探针直接继承实际AudioOutput，以模拟设备回报验证等待、取消与无效回报行为；它是单段验证器，不是可部署的设备音频适配器。

**Tech Stack:** Linux Python3.12、livekit-agents1.8.8、livekit1.1.20、标准库unittest；固定参考源码7a7547e。2026-10-24实际安装发现Agents1.8.8精确要求livekit==1.1.20，原计划的1.1.21无法解析；按包依赖修正，不使用--no-deps绕过。

## Global Constraints

- 架构依据：[服务器端LiveKit细化](../specs/2026-10-24-server-livekit-design.md)。框架选择已确定，不重复请求批准。
- 直接main提交/推送，不建分支/worktree。凭据、venv、音频和运行证据不进Git。
- 本阶段不开公网端口、不改安全组、不切设备、不刷机、不调用付费模型。
- 不把SDK探针通过当完整链路、手表听感或自动接续通过。
- 首版真实播放回报映射、账号权限、模型插件和完整会话接线在此探针结果出来后继续细化，不能把下面的单段探针直接作为生产实现。

## 文件与职责

- `tools/livekit-audit/requirements.txt`：本探针直接依赖。
- `tools/livekit-audit/device_sink_probe.py`：无网络/磁盘I/O的真实SDK子类，只验证一个在途播放段。
- `tools/livekit-audit/test_device_sink_probe.py`：有意义的SDK生命周期断言，无模型/硬件替身声学结论。
- `tools/livekit-audit/README.md`：精确环境命令、结果与适用边界。
- ignored `.superpowers/livekit-stage-zero/`：隔离环境及resolved依赖清单。
- `docs/verification/2026-10-24-livekit-stage-zero.md`：执行后才创建，记录真实结果，不预填PASS。

### Task 1: 真实SDK设备回报契约探针

**Consumes:** `livekit.agents.voice.io.AudioOutput`、`AudioOutputCapabilities`、`livekit.rtc.AudioFrame`。
**Produces:** 受测`ReceiptProbe`及三项行为测试；不是网络协议或正式播放状态机。

- [x] **Step 1：建立Linux隔离环境。** 在独立scratch checkout执行，不动正式venv。创建requirements文件内容：

```text
livekit-agents==1.8.8
livekit==1.1.20
```

```sh
python3.12 -m venv .superpowers/livekit-stage-zero/venv
.superpowers/livekit-stage-zero/venv/bin/python -m pip install -r tools/livekit-audit/requirements.txt
.superpowers/livekit-stage-zero/venv/bin/python -m pip freeze --all > .superpowers/livekit-stage-zero/resolved.txt
```

记录Python/系统版本与resolved依赖清单；安装失败保留错误并定位，不升级全局依赖或默默更换SDK。

- [x] **Step 2：先写测试文件，完整内容如下。**

```python
import asyncio
import unittest

from livekit import rtc
from device_sink_probe import ReceiptProbe


def frame():
    return rtc.AudioFrame(
        data=b"\0" * 960,
        sample_rate=24000,
        num_channels=1,
        samples_per_channel=480,
    )


class ReceiptTests(unittest.IsolatedAsyncioTestCase):
    async def test_flush_waits_for_matching_device_finish(self):
        sink = ReceiptProbe()
        await sink.capture_frame(frame())
        await sink.capture_frame(frame())
        block = sink.block_id
        sink.flush()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(waiter.done())
        self.assertFalse(sink.receipt(block + 1, 960))
        self.assertFalse(sink.receipt(block, 1440))
        self.assertFalse(sink.receipt(block, 480))
        self.assertFalse(waiter.done())
        self.assertTrue(sink.receipt(block, 960))
        result = await asyncio.wait_for(waiter, 0.2)
        self.assertAlmostEqual(result.playback_position, 0.04)
        self.assertFalse(result.interrupted)
        self.assertIsNone(result.synchronized_transcript)

    async def test_clear_uses_partial_finish_and_old_id_cannot_finish_new(self):
        sink = ReceiptProbe()
        await sink.capture_frame(frame())
        await sink.capture_frame(frame())
        old = sink.block_id
        sink.clear_buffer()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(waiter.done())
        self.assertTrue(sink.receipt(old, 480))
        result = await asyncio.wait_for(waiter, 0.2)
        self.assertTrue(result.interrupted)
        self.assertAlmostEqual(result.playback_position, 0.02)
        await sink.capture_frame(frame())
        new = sink.block_id
        self.assertNotEqual(new, old)
        sink.flush()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(sink.receipt(old, 480))
        self.assertFalse(waiter.done())
        self.assertTrue(sink.receipt(new, 480))
        await asyncio.wait_for(waiter, 0.2)

    async def test_invalid_receipt_cannot_be_upgraded_to_measured_completion(self):
        sink = ReceiptProbe()
        await sink.capture_frame(frame())
        block = sink.block_id
        sink.flush()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(sink.receipt(block, 0, valid=False))
        self.assertFalse(sink.receipt(block, 480, valid=True))
        self.assertFalse(waiter.done())
        waiter.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await waiter


if __name__ == "__main__":
    unittest.main()
```

- [x] **Step 3：运行红灯。**

```sh
PYTHONPATH=tools/livekit-audit .superpowers/livekit-stage-zero/venv/bin/python -m unittest discover -s tools/livekit-audit -p test_device_sink_probe.py -v
```

预期缺少`device_sink_probe`而失败。若没有实际执行测试，不能记录红灯。

- [x] **Step 4：创建单段探针文件，完整内容如下。**

```python
from livekit import rtc
from livekit.agents.voice import io


class ReceiptProbe(io.AudioOutput):
    """SDK contract probe only: one pending segment, no PCM retention or I/O."""

    def __init__(self):
        super().__init__(
            label="device-receipt-probe",
            capabilities=io.AudioOutputCapabilities(pause=False),
            sample_rate=24000,
        )
        self.block_id = 0
        self.sent_samples = 0
        self._live = False
        self._sealed = False
        self._interrupted = False
        self._invalid = False

    async def capture_frame(self, frame: rtc.AudioFrame):
        if frame.sample_rate != 24000 or frame.num_channels != 1:
            raise ValueError("expected mono 24kHz frame")
        if self._live and self._sealed:
            raise RuntimeError("probe supports only one pending segment")
        if not self._live:
            self.block_id += 1
            self.sent_samples = 0
            self._live = True
            self._sealed = self._interrupted = self._invalid = False
        await super().capture_frame(frame)
        self.sent_samples += frame.samples_per_channel

    def flush(self):
        super().flush()
        if self._live:
            self._sealed = True

    def clear_buffer(self):
        if self._live:
            self._interrupted = True
            self.flush()

    def receipt(self, block_id, played_samples, *, valid=True):
        if not self._live or block_id != self.block_id:
            return False
        if not valid:
            self._invalid = True
            return False
        if self._invalid or not self._sealed:
            return False
        if type(played_samples) is not int or not 0 <= played_samples <= self.sent_samples:
            return False
        if not self._interrupted and played_samples != self.sent_samples:
            return False
        self._live = False
        self.on_playback_finished(
            playback_position=played_samples / 24000,
            interrupted=self._interrupted,
            synchronized_transcript=None,
        )
        return True
```

此处故意没有网络、超时推进、音频队列、重采样或生产unknown终态降级。生产适配器不能因本探针拒绝第二个待结算段而阻塞新用户收音/回答；之后必须单独覆盖250ms结算超时、旧回调FIFO、并发取消及无效位置解锁SDK的策略。

- [x] **Step 5：重跑同一命令得到实际绿灯。** 预期3 tests、OK；若SDK行为与源码核对不符，按实际失败修正接入契约，不修改SDK内部私有字段让测试通过。
- [x] **Step 6：自查、记录与提交。** README写明上述准确命令、固定版本、无模型/设备调用及探针限制；verification文档填实际红绿输出与失败边界。执行`git diff --check`，只提交探针/依赖/说明文件，报告源码SHA与Linux环境；不要求运行无关固件构建。

## 本次执行结果

2026-10-24在云服务器独立checkout完成Task 1：真实红灯退出1、绿灯3项通过、依赖检查通过；具体发行包、参考源码差异和限制见[验证报告](../../verification/2026-10-24-livekit-stage-zero.md)。后续运行环境与服务均在服务器，Mac仅为代码/Git与后续测试终端。Task 1完成不代表P0其余SDK接线/模型能力或麦克风客户端完成。

## 阶段出口与下一份计划的输入

这份计划只把P0中可立即独立实现、验证的SDK契约细化到代码。执行完成后继续核对下列事实，再为完整链路写下一份可执行计划；不把它们预填为通过：

| 输入 | 所需结果 |
|---|---|
| 三模型权限/端点 | 现有业务空间分别可调用ASR、文本LLM、TTS；失败保存脱敏错误类型和具体环节，不打印Key |
| 流式与取消 | ASR final/中间结果、LLM正文/工具、TTS首PCM/终态的真实事件；重复取消和迟到事件行为 |
| TTS时间表 | 是否有真实词/句起止区间；若没有，固定短内容单位方案及停顿数据；不能以输入ASR时间戳替代 |
| 录制与日志 | AgentSession显式record=False，确认无音频/全文trace落盘；正文只进入获准文字存储 |
| 设备绑定 | Go分配playback_id，运行时代次映射、段与tts stop归属、超时unknown处理、先取消后结算 |
| 完整依赖 | 安装实际resolved版本及哈希/来源，正式服务使用独立锁文件和环境，不能共享审计venv |

上述模型探针只用已有机器合成输入，不采现场麦克风；开始实际API调用时记录调用次数、输入时长、usage、阶段耗时和费用依据。具备权限后自主验证；只有确实缺权限/凭据才向用户索取相应信息。随后仍须完成P1～P4，SDK单测不代表故事自动接续已经完成。
