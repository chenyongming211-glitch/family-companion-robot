# 已刷固件的主机故障测试

固件不能被默认视为正确。本轮对实际已刷版本对应的源码重新验证，发现唤醒缓存音频上传失败后仍继续发送detect并进入聆听的异常路径。它可能影响网络不稳时的唤醒完整性，但本轮没有证明它造成此前“原请求是你好小智”或中文接续位置错误；后两项另有不接手表的网关/官方框架复现。

## 固定对象

已刷源码 `17bb1e001f9f25926071a6a8871426ae52235e4a`，app SHA256 `ed30c3e02eaa21f64d0d9a7669b7e42f826212bcc3bec71fbeb053c0a9803e66`；锁定上游小智v2.5.2，基于此前逐项provenance核验的 `.superpowers/firmware-analysis-20261022/exact-17bb1e0/`。本轮不重新刷机或操作串口，app身份沿用[此前核验](2026-10-22-firmware-wake-source-audit.md)。

## 测试结果

| 范围 | 结果 | 实际覆盖 |
| --- | --- | --- |
| `python3 -m unittest discover -s firmware/tests -v` | 68项通过，14.160秒 | 包含构建准备、配置、证据工具及C++行为测试；不是68项板上测试 |
| 七个C++测试重新编译至已刷版本的精确header/include | 7个程序通过，ASAN/UBSAN | 播放进度、codec适配、WebSocket身份/结束/重连、打断、参考缓存、双工参考、capture激活；与上行68项有重叠，不能累加成75项独立测试 |
| 原版环形唤醒缓存随机测试 | 36000次操作、9091次内容比较通过 | 种子20261023，容量1/17/32000，清空、回绕、过大写、偏移读取、零/null输入、分配失败与初始化 |
| 原版缓存并发调用 | 40000次API调用完成，无ASAN/UBSAN错误 | 三线程Store、Read/Size、Clear；未运行TSAN，不声称已证明没有数据竞争 |
| 原版wake batch/generation guard | 48条断言通过 | 相等、旧batch、旧generation及uint32边界值 |
| 原版 `ContinueWakeWordInvoke` 故障注入 | 8种条件，其中3条上传失败没有被拒绝 | 方法原文提取后编译，调用依赖为明确stub，不是完整FreeRTOS/网络运行 |

缓存最初使用未优化sanitizer与大量长随机写、deque oracle，超过30秒测试上限，没有崩溃或sanitizer输出；改为通常0–200采样、每97次加入超容量写，保留操作数和各容量后完成。该超时是测试驱动成本，不能记为固件实时性失败。

## 已复现异常

`main/application.cc` 的 `ContinueWakeWordInvoke` 在循环中调用 `protocol_->SendAudio(std::move(packet))`，没有检查bool返回值。精确方法SHA256为 `ef06aa9f992aa8ee03850f5a378c55caf79098b04b93d5547c70065c0443ef93`。

在三个缓存包中分别让首包、中间包、末包返回false：三次都仍尝试三个包、发送detect一次、启动聆听一次。相对地，普通持续上行的主循环已有失败处理。`WebsocketProtocol::SendAudio` 会在未连接或底层Send失败时返回false，故该异常并非虚构的API条件。

正常上传、初始状态不符、OpenAudioChannel失败与batch无效的控制条件均执行对应分支。batch无效路径调用真实方法中的SetAecMode，但测试stub只记次数；不能把stub中仍是connecting解释成实际板上卡死。有效但空batch仍发detect/start，记录为边界事实，尚不认定为错误：还需区分没有可用唤醒音频与只有元数据的合法路径。

此外 `SendWakeWordDetected` / `SendStartListening` 当前为void，无法让调用方直接校验文本控制发送结果。这是进一步修复需要覆盖的边界，仅源码审查，本轮没有把文本发送失败算进上述三条已运行故障样本。

最小后续修复应在音频/控制发送失败时终止该唤醒流程、清理连接并回到可再次唤醒状态；不能继续以成功握手路径推进。须补真实状态回调/重连回归和ESP32构建，再现场验收。此次保留故障证据，没有将未验收固件刷进手表。

## 复现和局限

`tools/voice-audit/wake_lifecycle.py` 和 `cache_stress.py` 接受准备好的源码根目录，输出编译参数、源文件摘要、运行结果。唤醒工具在目前已刷源码上退出2，明确表示发现未拒绝的失败；不是测试通过。仓库工具的独立复跑也复现了该结果。完整命令见[工具说明](../../tools/voice-audit/README.md)。证据保存在 `artifacts/overnight-20261023/firmware/`。

没有执行新机器码反汇编：已具备与已刷产物匹配的源码，用原函数行为故障注入比猜测二进制更直接。本轮未测试板上FreeRTOS/ISR真实时序、PSRAM紧张、Wi-Fi硬件丢包、喇叭声学回声、远场唤醒率、充电蜂鸣或实际停声时延。主机sanitizer通过不等于这些问题已经排除。
