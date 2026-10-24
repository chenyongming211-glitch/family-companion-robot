# 独立 Realtime Mac 测试

运行 `./tools/realtime-voice/open.command`，打开“家庭陪伴 · Realtime 独立测试”。点一次“开始对话”，允许麦克风后自由交谈；点“结束对话”停止收音并释放模型会话，窗口保留；关窗退出。与三段式窗口交替使用，每次先结束另一边的对话。

该入口使用已接小智的 Go/Qwen Realtime 网关代码，云端单独运行在回环18030。8000正式小智服务、18010/18020三段式服务及原客户端不切换。复用静态原生音频代码，仍为16k上行/24k下行、Opus、AVAudioEngine Voice Processing与CoreAudio；不使用浏览器，也不在Mac搭建服务。只依赖本机已有Xcode工具和libopus；构建脚本不安装依赖。

模型Key留服务器，启动器只建立专用SSH转发，开始对话时获取120秒一次性票据。`REALTIME_NATIVE_SSH_CONTROL`可指定已有SSH控制连接，`REALTIME_NATIVE_LOCAL_PORT`可换本机转发端口（云端仍18030）。关闭窗口后只清理本启动器建立的转发。

模型/音色在连接日志中标明，目前按独立配置使用`qwen3.8-omni-flash-realtime`/Tina。此实例不加载正式记录，也不向现有三段式档案写入或读取；结束后重连是新会话。它沿用原Realtime网关的上游会话重建、会话内背景和暂停任务规则，没有把这些机制描述为模型原生能力。

Realtime沿用原网关的中断停播语义；三段式用drain。这里没有给Realtime移植固定原稿检查点，也不承诺精确恢复原句。比较的是两条当前实际链路，包含各自网关、话术和轮次行为。等待感、正文首播和音色偏好分开评价。

旧网关播放回报只接受`i2s_dma`，Mac不协商该功能，也不伪造I2S证据。客户端只在独立诊断日志记录CoreAudio完成样本数；服务端不能据此推断已听文字。普通stdout不打印转写、生成稿、Key或原始音频，不持久保存真人录音。

合成输入验收：`./tools/realtime-voice/open.command --fixtures /absolute/path/plan.json`。JSON数组每项有16k单声道WAV路径`wav`，可选`expected_pattern`检查生成文字；`interrupt_after_samples`在CoreAudio播放达到指定样本后开始下一项。无需麦克风权限，不录制现场音频。正文检查仅针对生成转写，不能当作外部声学核听结果。构建阶段另运行纯Swift协议/播放状态测试。

部署与证据见[本轮验证](../../docs/verification/2026-10-24-independent-realtime.md)。
