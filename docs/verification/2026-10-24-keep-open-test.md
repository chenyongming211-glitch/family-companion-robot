# 原生语音窗口常开测试

日期：2026-10-24。用户要求持续测试，直到自己关闭窗口。本次基于`2b507f5`，仅调整独立LiveKit测试服务与Mac原生终端。

## 行为

- 独立18010/18020服务启用`VOICE_TEST_KEEP_OPEN=1`，仅drain模式去掉用户闲置、8轮与15分钟自动结束。旧浏览器/manual/continuous预算保持。
- GUI点击开始才申请120秒一次性票据，不落盘或进入argv。停止/连接失败后窗口保留，点开始会建立新会话；关窗停止麦克风、播放与连接。
- 连续发言超过25秒丢弃该段，等待VAD话尾后恢复。不能在丢弃时调用SDK`clear_user_turn`：它会重建STT流，导致余尾被当作新语段识别。
- 每轮模型上下文与独立SDK对话历史裁剪到32项，再允许当前轮追加；已播历史64项，SDK事件尾128项，当前主旁稿独立保留。固定LiveKit1.8.8的事件尾使用私有`_recorded_events`，升级SDK时必须重验。
- 原生播放器完成块即移除进度缓存。专用SSH主连接持续到应用退出，共享主连接仍保留；SSH本身失效需要重新运行入口。

## 已核对证据

- 云端Python全量114项通过，101.763秒；包括超过旧闲置预算、9轮真实SDK/VAD、超长段无ASR/LLM并可继续下一段、300事件回调不丢且存储有界。提供方为受控测试，不能等同真人声学验收。
- Go桥接测试覆盖默认短deadline会结束、keep-open无总deadline、手动断开释放租约与服务Close回收；本轮Go全库race/vet/build通过。
- Mac重新构建通过，账本、单/双/六声道、固定原稿恢复证明和共享SSH清理回归均通过；CoreAudio真实播放自检通过。
- 部署两项独立服务后health正常；8000原`family-robot.service`保持active、PID仍为2261506。
- 原生GUI实际点击开始后显示“对话中 · 可持续测试，结束时关闭窗口”，日志有`session.ready`与持续麦克风帧。实际停止后窗口保留，再点开始重新配对成功；验证完成保留新会话运行，不再关闭窗口。

现场继续测试时，快速重连曾一次返回`event_before_ready`，再次连接成功；一轮真实发言触发`DraftValidationError / failure_code=ending`，客户端显示`server_error`并保留窗口。已重新连接继续测试。本次没有修复这两个独立失败，也未保存该轮发言；常开窗口不代表每次模型生成都成功。

后续已复现并修复以上问题，补充真实云端问答、6次快速重连与原生实际播放验证，见[后续修复](2026-10-24-reply-reconnect-fix.md)。前述段落保留为本次验证当时的事实。

服务器日志位于`.superpowers/drain/keep-open-final-114.log`、`keep-open-events-red.log`、`keep-open-events-green.log`；Mac元数据位于`.superpowers/native-voice/keep-open-live.jsonl`、`keep-open-build-final.log`和`keep-open-selftest-final.jsonl`，均不进入Git。不保存用户录音、识别正文或模型回复。此轮没有重复真人问答验收，前轮真人正常回复结论见[麦克风修复记录](2026-10-24-native-microphone-fix.md)。
