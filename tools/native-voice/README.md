# Mac 原生语音测试终端

Swift / AppKit / AVAudioEngine / 系统 CoreAudio / libopus。只运行测试终端；语音服务、模型 Key 和运行时依赖留在云端。默认连接经 SSH 转发的 `127.0.0.1:18020`，不使用浏览器。

## 构建与开始

```sh
./tools/native-voice/open.command
```

此入口使用已有 SSH 授权连接 `ubuntu@82.156.48.111`，创建回环转发，再在服务器 loopback 请求120秒、一次性配对票据。票据直接经标准输入交给客户端，不进入参数、文件或日志。点“开始对话”才请求麦克风权限并开始连续收音；点同一个按钮“结束对话”会停止收音和播放。单次票据不能复用，再次对话请重新运行入口。窗口关闭也结束客户端。服务端正常待机 `sleep` 或显式硬停止 `output.stop` 会关闭麦克风并结束本次连接；再次唤醒需重新运行入口。合成用例未完成就收到此事件会以失败退出。

若当前工作区已有有效的 `.superpowers/ssh/cloud.sock`，入口会复用已授权连接；也可用 `VOICE_NATIVE_SSH_CONTROL` 指定自己的 ControlMaster。结束时只取消本次成功创建的转发，保留共享主连接和其他转发。没有可复用连接时按普通 SSH 登录。配对后两分钟内点开始，票据过期则重新运行入口。

运行环境为 macOS13+、Apple Command Line Tools、Python3 和已有 libopus。默认 libopus 根目录 `/usr/local/Cellar/opus/1.6.1`，其他位置可通过 `NATIVE_OPUS_ROOT` 指定。脚本用 `xcrun swiftc` 避免 PATH 中同名工具；不安装依赖。生成 `.app`、对象文件和测试程序都放在 Git 忽略的 `.superpowers/native-voice/`。应用使用固定 bundle identifier 与本机临时签名；系统设置可管理其麦克风权限。

```sh
./tools/native-voice/build.command
.superpowers/native-voice/NativeVoice.app/Contents/MacOS/NativeVoice --self-test
```

`--self-test` 不读取票据、不访问网络、不请求麦克风。实际播放两个低音量合成音：旧段排空，再释放预置新段；第三段在起播前撤销。使用与在线会话相同的 Opus decoder、CoreAudio player、身份账本和回报代码。测试刻意丢弃第一次最终 ACK，验证100ms重发与随后结算。成功只在真实 `.dataPlayedBack` 回调覆盖所有已接受编码采样后输出 `selftest.success`；8秒未完成则失败退出，不用 sleep 伪造播放。

## 云端合成输入验证

先由负责人准备三个**私有合成**、16kHz单声道、每段不超过20秒的 WAV，分别是故事请求、插话问题和继续请求：

```sh
./tools/native-voice/open.command --fixtures /private/story.wav,/private/question.wav,/private/resume.wav
```

不打开麦克风。首段发送完持续发送静音；故事首段真实播放进度达到120ms时注入第二段；收到旁支内容完成后注入第三段；主内容完成且所有最终回报获 ACK 后退出。180秒硬超时失败。成功还要求原主任务 ID、revision、总段数保持不变，插话后观察到 draining/paused，旁支完成后出现 resume main 动作；恢复的 playing 检查点与首个 handoff 的 unit_index 必须都等于最新确认检查点。`fixture.success` 输出 `resumed_from` 与 `original_identity_verified`。只输出事件、采样位置和状态元数据；不输出识别文本、模型回复、原稿 snapshot 或票据，也不保存音频。此模式是原生设备链路的合成输入验证，不等同于真人麦克风、扬声器回声或 M5 手表验收。

也可由已建立的 SSH 通道把一行配对 JSON 或票据通过 stdin 交给 `.app/Contents/MacOS/NativeVoice`。客户端只接受 `ws://127.0.0.1:<port>/xiaozhi/v1/`，票据不能放在命令行。自定义本机转发端口用 `VOICE_NATIVE_LOCAL_PORT`。

## 播放与回声处理边界

- 上行16k单声道20ms Opus，麦克风经 AVAudioConverter 转换；保持全双工，普通开口只发送输入音频，由云端 VAD 决定封口与交接。普通插话不调用硬停止。
- 下行24k单声道60ms Opus。每个 block/epoch 保留独立压缩队列；只对当前块建立 decoder 和 player 时间线。新 handoff 不重置旧 decoder。
- 所有控制消息按递增 `control_seq` 在串行状态中应用；`playback.started.observed_control_seq` 固定记录实际起播时已应用的序号，避免跨方向撤销竞态。
- 新段的 `after` 全部本地实际完成才放行；该栅栏不等云端 ACK。未起播的已撤销块回报 discarded / played0；已起播块继续排空。输入代次必须保持一致，过期授权失败关闭。
- 每50ms从 player render clock 读进度，减去输出 presentation latency 和保守的512采样渲染余量。显式分段调度避免把 underrun 的静音计成内容。最终完成只能来自所有 buffer 的 `.dataPlayedBack` 回调，覆盖 Opus lookahead 与末帧补零后的完整编码时间线。
- 最终回报每100ms重发，最多2.5秒等待身份匹配 ACK；未知坐标、路由变化、资源超限或超时会停止会话。CoreAudio系统回调是本测试的播放证据；没有外部麦克风/电气回采，不能证明扬声器声压波形或人的听感。
- 合计压缩待播与已解码未播部分最多14400采样/600ms；每块最多12秒，未结算块16个，历史64个，消息16KiB，发送队列8项、写入期限250ms。
- 麦克风启动时请求系统 Voice Processing，读取 input/output 的实际启用状态。`audio.capability` 报告是否启用及 presentation latency。请求成功不等于本机声学 AEC 已通过；状态为 false 时界面会明确提示。

状态测试与原稿接续证明测试在每次构建时运行，覆盖本地排空栅栏、部分播放、无需 ACK 的本地交接、撤销、旧代次、采样倒退和总缓冲额度。在线端点的协议定义见 `cmd/livekit-device-gateway/README.md`。

服务器受控待机回归：`Tests/standby-probe.py`只在云端loopback18121运行一次性WS对端，无模型或录音；通过SSH转发让原生fixture连接，验证正常sleep结束不会被误报为网络错误。未完成fixture仍须退出失败。
