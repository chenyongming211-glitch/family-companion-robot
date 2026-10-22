# 家庭陪伴机器人：最小 Go 实时语音网关

2026-10-22：已部署设备播放回报与独立暂停任务网关，并通过 USB 刷入候选固件；实机普通对话、完整长回复、直接插话和播放回报已验证，任务内容归属继续现场核验。“继续数”的位置或终点仍不正确，未加入辅助 ASR。最新状态见[现场验证](docs/verification/2026-10-22-playback-field-results.md)，构建来源见[阶段记录](docs/verification/2026-10-22-device-playback-and-paused-task.md)。

自研 Go 会话主干，通过小智 WebSocket 协议连接 M5StopWatch 与千问原生实时语音。本阶段实现对话、打断，以及现有固件的音量和屏幕控制。

```
M5StopWatch（小智固件）
  ↕ 小智 WebSocket / Opus
Go 网关：设备鉴权、编解码、会话调度、有界播放队列、固定设备工具
  ↕ Qwen 原生 Realtime / PCM
千问实时语音
```

## 本地开发

需要 Go 1.25+、C 编译器、libopus 开发文件、pkg-config。

```sh
# macOS
brew install opus pkgconf
# Ubuntu
sudo apt-get install libopus-dev pkg-config gcc

go test -race ./...
go vet ./...
go build -o bin/gateway ./cmd/gateway
go build -o bin/smoke ./cmd/smoke
```

参考 `deploy/server.env.example` 设置环境变量后运行 `bin/gateway`。必须配置独立的设备 token、引导 URL 密钥和上游 API Key；三者不能混用。默认监听 `127.0.0.1:8000`。

## 接口与设备配置

- `GET /healthz`：进程健康及活动会话数。
- `/xiaozhi/ota/<ROBOT_OTA_SECRET>/`：在设备自定义 OTA 地址中填写这个完整地址。收到有效设备元数据后返回 WebSocket 地址和设备 token，不下载或更换固件。
- `/xiaozhi/v1/`：语音 WebSocket，要求 `Authorization: Bearer <ROBOT_DEVICE_TOKEN>`、`Device-Id` 和 `Protocol-Version`。
- M5StopWatch 实读固件为小智 v2.5.2；已直接配置并验证真实多轮语音样例。连续收音稳定性待确认，播放中唤醒词打断未通过远程声学测试，见 [实机记录](docs/verification/2026-10-21-device-connection.md)。

仅支持 16kHz、单声道 Opus 上行，20/40/60ms 帧；支持二进制协议 v1/v2/v3，OTA 下发 v1。下行是 24kHz、单声道、60ms Opus。`manual` 在 listen.stop 后提交，`auto/realtime` 交给千问 VAD；auto 固件通常在播放时停止普通收音，不能据此宣称支持任意语音插话。

## 音量与屏幕控制

设备声明 MCP 后，网关先完成工具发现，再向千问注册实际可用的状态读取、音量和屏幕亮度工具。保留小智 v2.5.2，无需重新刷机。音量、亮度均为 0—100；“关闭屏幕”设置亮度为 0，恢复时可指定亮度。

每次设置后读取设备状态核对，核对失败或超时不返回已核实成功。相对调节要求模型先读当前状态。设备控制仅开放这三个固定工具，不转发设备提供的任意工具描述或网络信息。另有两个独立本地会话工具用于查询、放弃或切换暂停原任务，不依赖设备 MCP。

发现总超时 3 秒，失败后继续纯语音；调用总超时 5 秒，最多 4 项在途/排队。工具等待与音频共用串行事件循环，父回复结束、播放排空、工具结果齐全后才续答。打断取消旧轮次的排队操作及续答；已下发的硬件操作不能撤销。

本次修复的检查、真实模型和实机结果见 [控制与音频验证](docs/verification/2026-10-21-device-controls.md)。

## 打断策略与限制

网关在打断时发送 `{"type":"tts","state":"stop","reason":"interrupted","session_id":"..."}`；正常播放结束的 stop 省略 `reason` 字段。这个可选字段本身不会让当前原版固件清空播放队列或支持自然插话，这些能力仍需定制固件实现并通过实机验收。

- 设备 abort、播放期间重新 listen.start、上游 speech_started 均停止旧回复。
- 音频按既定 60ms 时间线发送，停顿或缺音后重置发送锚点，最后一帧发送满 60ms 后再停止。服务器待播 PCM 最多 120 秒（24kHz/mono16，5.76MB/会话），容纳云端比实际播放更快生成的长回复；仍逐帧发送给设备。打断清除队列，重建上游连接，连接代号和回复 ID 阻止迟到音频进入新轮次。
- 当前千问接口的原生 truncate 尚未验证有效。重建会话时注入最多12条、每条512字的近期背景；生成稿以 generated_text 标记，不冒充已听文字。上游触发插话时最多重放2秒近期输入 PCM。
- 双向协商 playback_progress_v1 后可接收设备按播放块回报的 I2S DMA 完成前缀；未协商时维持未知。正常完整最终回报才升级匹配历史的播放确认，仍无法精确同步已听字词；单份暂停原任务不被旁支覆盖，text_position 保持 unknown。近期状态只在当前设备连接内存保存，设备断线后不恢复。
- 网关不实现声学回声消除；播放中持续收音需要固件支持及实际 AEC 验证。首版支持按钮/唤醒词 abort；任意语音插话是否可用由设备测试确定。
- 默认单设备并发，连续两分钟没有有效对话活动后断开、返回待机；静音音频、自动 listen.start 和播放遥测不延长会话。VAD 语音区间内的收音、手动录音、模型回复和实际下行播放会更新活动时间，播完后重新留出两分钟。单连接最长 30 分钟。
- 初期 HTTP/WS 入口只用于联调，正式使用前配置 HTTPS/WSS。服务器 API Key 始终保存在服务器，不下发给设备。
- 服务日志不包含音频、对话转录、鉴权值或上游原始错误。

## 服务器部署

systemd 模板为 `deploy/family-robot.service`。现有服务器使用 `family-robot` 账户；配置 `/etc/family-robot/server.env` 由 root 管理，权限 `0640 root:family-robot`。发布目录 `/opt/family-robot/releases/<version>/`，`current` 链接指向启用版本。Linux 发布工具链使用官方 SHA256 校验后的 Go 版本，发布前在 Linux 运行完整测试和 vet。

```sh
sudo systemctl status family-robot
sudo journalctl -u family-robot -n 30 --no-pager
curl http://127.0.0.1:8000/healthz
```

回滚时将 `current` 链接指向之前的已验证发布目录，重启 `family-robot` 并验证健康接口；不要覆盖正在使用的二进制。

## 实际模型联调

`cmd/smoke` 模拟小智设备，读取单声道 16kHz/16bit WAV，按设备速度发送真实 Opus；至少两轮，并保存收到的 Opus 解码 WAV 和 JSON 结果。

```sh
# token 从环境读取，不放到命令行；目录必须尚不存在
bin/smoke -input /absolute/first.wav -followup /absolute/second.wav -out /absolute/result -mode manual
bin/smoke -input /absolute/long.wav -followup /absolute/second.wav -out /absolute/abort-result -mode manual -abort-after 10
bin/smoke -input /absolute/first.wav -followup /absolute/second.wav -out /absolute/vad-result -mode realtime
```

这些测试证明网关和真实 API 的链路，不能替代 M5StopWatch 扬声器、麦克风、Wi-Fi 和实际停播延迟的测试。

## 实现来源

协议依据：[小智官方 WebSocket 文档](https://github.com/78/xiaozhi-esp32/blob/main/docs/websocket.md)及对应固件实现。
千问依据：[实时语音](https://help.aliyun.com/zh/model-studio/realtime)、[客户端事件](https://help.aliyun.com/zh/model-studio/client-events)、[服务端事件](https://help.aliyun.com/zh/model-studio/server-events)。

当前未复制小智 Python/Go 服务端源码。直接依赖 Gorilla WebSocket 和系统 libopus，许可证见 `THIRD_PARTY_NOTICES.txt`。
