# 播放中唤醒失败：源码核对与静音对照

## 当前定位

故障点在真实设备播放期间的本地唤醒触发环节。证据最支持自身播放声干扰唤醒，但最终根因尚未完全确认，不能直接定性为小智固件通用缺陷，也不能据此认定某一处代码错误。

## 源码与运行配置

核对小智 `v2.5.2` 官方源码：

- [application.cc：播放状态](https://github.com/78/xiaozhi-esp32/blob/v2.5.2/main/application.cc#L1046)会停止普通语音处理，同时为支持 AFE 的唤醒引擎保留唤醒检测。
- [application.cc：唤醒事件](https://github.com/78/xiaozhi-esp32/blob/v2.5.2/main/application.cc#L897)在 speaking 状态收到唤醒事件后，会调用 `AbortSpeaking`，发送设备打断指令。
- [afe_audio_engine.cc](https://github.com/78/xiaozhi-esp32/blob/v2.5.2/main/audio/engines/afe_audio_engine.cc#L375)只有在 codec 提供播放参考时才启用相应 AEC 路径。

设备启动日志实测为 WakeNet、1 microphone / 0 playback，AFE 管线没有 AEC。它支持待机唤醒，但当前音频路径没有为本地 AEC 提供播放参考。官方源码仅作为对应版本参考，未证明设备二进制与该源码逐字节一致。

## 对照方法

使用同一块手表、同一份已刷入的固件和同一测试脚本，Mac 音量固定为 60、Tingting 音色。等待开场回复结束，再让设备从一数到二十；串口确认计数请求已转写且进入 speaking 后，再等待 1 秒播放“你好小智”，随后播放“请只回答，收到。”。两轮均用串口和 WebSocket/Opus 抓包核对。

静音条件使用独立临时构建的网关，仅在 Opus 编码之前将下行 PCM 置零；保留 TTS 控制、原音频时长和分帧发送节奏，不修改设备固件和设备配置。原始应用源码没有改动。诊断二进制 SHA256 为 `6f430c3b5653ec1bec73685ac660d789b669188afc4f0d9ce8f9bf6bfa58b04d`。服务切换期间配置了限时自动恢复，测试完成后已提前恢复正式版本并停止恢复定时器。

## 结果

| 条件 | 设备唤醒与打断 | 旧回复 | 新问题 |
| --- | --- | --- | --- |
| 静音计数回复，21:18 | speaking 状态识别到“你好小智”，串口记录 `Abort speaking`；抓包收到 `abort / wake_word_detected` | 计数音频仅发送 44 帧后收到打断并下发 stop | 随后转写新测试句并回复“收到” |
| 正常有声计数回复，21:20 | 没有播放中的唤醒日志，也没有设备 abort | 计数回复发送 168 帧，自然结束 | 播放中的后续测试句未转写 |

两份抓包重组的音频流无缺失字节，Opus 解码均成功。静音样例下行解码峰值 2、最大每秒 RMS 0.1，接近全零；正常样例峰值 16107、最大每秒 RMS 3047.5。测试 Mac 音量均已还原为 38。

第一轮静音预试也曾观察到 speaking 状态的唤醒和 abort，但开场回复干扰了预设计数时序，未将它作为严格计数对照的通过样例。

## 能确认与仍需确认

可以确认：设备固件具备播放中唤醒和发出 abort 的可执行路径，网关在静音实机样例中能处理打断并继续新一轮对话。正常有声打断体验仍未通过。

最强假设是自身播放声影响了 WakeNet 识别，当前板级配置缺少 AEC 播放参考与此一致。但 PCM 置零同时改变了 Opus 包和解码负载，模型生成的音频也并非逐字节复用；本实验没有完全排除负载、调度及唤醒状态切换因素。下一项区分性实验应保持下行音频相同，直接控制设备扬声器音量，并记录播放期间的 WakeNet 启停和原始收音，再决定修复 AEC 数据路径还是固件调度。

脱敏证据位于 `artifacts/device-20261021/`：`muted-count-target-summary.json`、`muted-count-target-network-summary.json`、`audible-count-target-summary.json`、`audible-count-target-network-summary.json`。含鉴权的原始抓包和完整串口日志仅保存于私有设备备份目录。

正式服务已恢复原发布目录 `voice-gateway-20261021T094605Z-02f9f39d`，二进制 SHA256 仍为 `94c0032d89b08a5a08ed770de7edcd1163566bb799664b57ca242e4cb4857307`。
