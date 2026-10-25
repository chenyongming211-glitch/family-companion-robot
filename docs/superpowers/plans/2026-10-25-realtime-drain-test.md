# Realtime 播放端排空试验

用户已选择：取消旧生成，停止旧下发；播放端保留已接收音频，播完再播放插问回复。先用 Mac 原生客户端验证可接受的延迟，后续小程序、App、ESP32 分别适配，不刷固件或切换现有服务。

## 边界

- 只改独立 `tools/realtime-output-live/`，云端 loopback 18032。
- 不改三段式和原生网关 8000/18010/18020/18030，不改身份功能。
- 播放端排空只能使实际播放样本与下发边界一致；千问内部的生成历史不会自动回退。本试验继续逐轮新建音频生成连接，只带已播候选前缀，严格 ASR 映射失败仍为 unknown。
- 不等待整句，也不播放服务端尚未下发的旧内容。普通输出完成回报窗口 7200 个 24k 样本（300ms），不把逻辑窗口当成声学延迟上限。

## 实现

1. 为云端增加不依赖模型的排空失败测试，先复现旧版没有排空栅栏。
2. 包发送和封口共用锁。已持锁发送的包计入最终边界；封口后不再放行旧包。取消该轮生成任务，释放未下发队列。
3. 服务器发送 `drain {id, sent}`。播放端核对已接收数与 sent 相同，封口但保持播放器；全部 `dataPlayedBack` 后回 `receipt {id, completed, accepted, basis, terminal:"drained"}`。
4. 完成回报只作用于同一 id，且 `completed == accepted == sent`；普通 progress/played 不冒充封口确认。旧音频正常 end 与中断竞争时也须确认封口。
5. 输入持续接收。下一轮恢复背景与音频放行均等排空证据；新问题收音和旧尾音自然重叠。连续插话可以取消尚未输出的新轮，仍保留更早的排空栅栏。
6. 3秒未回报则可视提示并断开会话，避免把未知播放当成功或重叠放音。用户主动结束/关窗会停止播放，这是结束会话操作，不是插话策略。

## 验证

- 云端状态和无付费会话测试：在途包、迟到包、重复插话、严格终态、旧回报、上下文栅栏、缺失回报超时。
- 云端保留原输出 ASR 探针回归。
- Mac 构建、按真实时长发送合成输入并经过真实千问/Fun-ASR和CoreAudio，检查每个封口的 `completed == accepted == sent`、旧新输出顺序、尾音排空时间和新首包/首完整包回报。
- 合成输入不证明真人 AEC、物理声学停点、逐字接续或用户可接受的等待。最后保持测试窗口，让用户亲测。

## ESP32 静态核对

仓库锁定小智 v2.5.2 / `49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`。`firmware/patches/0001-stopwatch-software-aec.patch` 收到 interrupted stop 会调用 ResetDecoder；[固定上游源码](https://github.com/78/xiaozhi-esp32/blob/49b4380b3faa0ceadd28a3e78aaf0f311c46c57e/main/audio/audio_service.cc#L798) 的 ResetDecoder 清空 decode/playback 队列。因此需要端侧适配才能排空；本轮没有核验手表已烧录版本，也没有对 ESP32/小程序/App 作实机验收。
