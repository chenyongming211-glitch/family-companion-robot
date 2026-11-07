# 电脑麦克风候选素材采集

macOS 13+ AppKit 小窗口；每次手动录制固定 4 秒。打开窗口不连接、不申请权限、不启动麦克风。每段先通过独立 SSH 启动云端接收器，校验 ready 后才请求麦克风权限；录完停止麦克风，再上传并等云端确认。取消和关闭会停止麦克风并回收本客户端的 SSH 子进程，没有自动重试。

使用系统当前输入原生 Float32、实际整数采样率 8000–96000 Hz、全部 1–8 声道，保留交错小端数据；不启用新的 Voice Processing、不重采样、不做 VAD、声纹或模型推理。声音仅存在于有界内存和 SSH 管道，不写 Mac 文件；不播放声音。云端保留 `source.f32le` 全声道原件，用固定 ffmpeg 选第 0 声道转换 16 kHz 单声道 s16le WAV，并记录格式、摘要和质量指标。

所有素材都是 `private_human` / `computer_enrollment_candidate`，身份、人类及生产验收标记均 false。所有片段由同一位配置的匿名参与者完成。先采 6 段再核对云端质量，如需补录使用后续不同短句；每窗口最多 12 段，到上限后禁止继续录制，提示句不会循环。候选数量不能当成 20 秒净人声或质量通过。电脑登记 → 机器人查询是本轮条件；手机条件仍待验。

先仅构建（不启动窗口或麦克风）：

```sh
tools/computer-voice-capture/build.command
```

云端部署 `receive_computer.py` 与同目录 `capture.py`，预先创建当前用户拥有的 0700 私有根目录。接收器每次只处理一个随机 clip；目录和文件排他创建，分别 0700/0600，不覆盖旧素材，不开放网络服务。Python、ffmpeg 和所有接收器测试只在云端运行。

配置以下环境变量后手动运行 `open.command`（只打开窗口，仍须手动点录制）：

```sh
COMPUTER_CAPTURE_SSH_TARGET='root@your-cloud-host' \
COMPUTER_CAPTURE_SSH_KEY='/absolute/path/to/existing-key' \
COMPUTER_CAPTURE_RECEIVER='/absolute/private/source/receive_computer.py' \
COMPUTER_CAPTURE_ROOT='/absolute/private/clips' \
COMPUTER_CAPTURE_PARTICIPANT='M01' \
tools/computer-voice-capture/open.command
```

参与者只允许 M01–M05，不输入姓名。现有 SSH known_hosts 必须已信任目标，严格校验开启，禁止复用 SSH 主连接。不要在仓库保存私钥、录音、生成的 App 或云端素材。

无麦克风 SDK 检查：

```sh
mkdir -p .superpowers/computer-voice-capture
xcrun swiftc -swift-version 5 tools/computer-voice-capture/State.swift tools/computer-voice-capture/NativeCapture.swift tools/computer-voice-capture/Transport.swift tools/computer-voice-capture/Tests/main.swift -o .superpowers/computer-voice-capture/SDKTests
.superpowers/computer-voice-capture/SDKTests
```

线协议版本 1：receiver 先输出只含 schema_version/event/clip 的 ready JSON；客户端再发送 4 字节大端头长度（1–2048）、固定 JSON 头、恰好 4 秒全部声道的 f32le，然后关闭 stdin。总读时限 60 秒，最大 PCM 16 MiB；必须 EOF，无多余字节。成功响应仅含 schema_version/event/clip/samples/duration_seconds/rms。失败仅固定 incomplete 原因，不输出任意异常、音频或正文。源为操作员声明的 AVFoundation 输入，不能称为原始 ADC。
