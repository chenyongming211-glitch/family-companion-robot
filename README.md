# 家庭陪伴机器人：最小 Go 实时语音网关

换电脑继续开发先读[开发交接说明](docs/DEVELOPMENT_HANDOFF.md)：环境恢复、服务器约定、已完成/未完成、新电脑麦克风接入的下一步，以及本机历史实验资料的归档位置。

2026-10-24：服务器已实现独立LiveKit最小语音链路，真实AgentSession接入Fun-ASR→文本LLM→Qwen TTS，完成连续问答、取消和取消后新问题实测，见[链路验证报告](docs/verification/2026-10-24-livekit-voice-chain.md)。[测试服务](services/livekit-agent/README.md)只监听服务器127.0.0.1:18010；依赖、自动化测试和服务均在服务器。现已提供[Mac实时收音/播放客户端](tools/mac-voice/README.md)：浏览器经SSH转发连接服务器，点击分轮和本地立即停播；真实验收与限制见[客户端验证](docs/verification/2026-10-24-mac-voice-client.md)。自动端点、声学插话、自动接续与手表验收分别推进。

当前方向为服务器端LiveKit Agents＋ASR→LLM→TTS：[架构细化](docs/superpowers/specs/2026-10-24-server-livekit-design.md)、[SDK探针结果](docs/verification/2026-10-24-livekit-stage-zero.md)、[最小链路实施计划](docs/superpowers/plans/2026-10-24-livekit-voice-chain.md)。手表保留小智协议，Go接入层逐步收缩为设备适配；独立SFU媒体服务不作为本阶段必需项。8000上的正式服务仍为下述千问原生链路。

任务层的原请求、原稿、播放账本、暂停/恢复与持久化职责见[实时语音与任务接续细化](docs/superpowers/specs/2026-10-24-livekit-task-continuation-design.md)。LiveKit Agents管理实时语音，Go任务模块持有播放事实；Eino为任务编排候选，不作为首个闭环的必需依赖。本文档更新未切换正式服务。

最新文字记录、跨连接/进程恢复、清空隔离及发布状态见[记录与恢复验证](docs/verification/2026-10-23-record-recovery-results.md)。网关不保存原始录音，准确文字断点仍为unknown；[三种离线定位/识别对照](docs/verification/2026-10-23-record-recovery-alignment.md)与生产启用分别验收。

2026-10-23本轮实施：原请求/最新播放段分离，独立文字归属判断、输入身份与快照ACK门控已实现；未知文字断点时先澄清起点，再接受明确重读或用户指定起点；复用原生语音已有转写，未增加辅助ASR，未刷手表固件。最终构建、真实接口复验和发布状态见[本轮实施结果](docs/verification/2026-10-23-resume-state-and-alignment.md)。准确已播文字仍保持unknown，官方强制对齐目前完成离线评估。

此前唤醒来源修复与固件发送失败恢复见[上一轮实施结果](docs/verification/2026-10-23-wake-recovery-results.md)；Pipecat、TEN、LiveKit和小智模块对照见[根因调查](docs/verification/2026-10-23-resume-framework-root-cause.md)。既有实机证据见[现场验证](docs/verification/2026-10-22-playback-field-results.md)，不能冒充本轮候选的现场验收。

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
- M5StopWatch 沿用基于小智v2.5.2的现有实验固件；此前连续对话/直接插话的现场记录见[现场验证](docs/verification/2026-10-22-playback-field-results.md)。本轮只验证网关，准确字词接续仍未通过实机验收。

仅支持 16kHz、单声道 Opus 上行，20/40/60ms 帧；支持二进制协议 v1/v2/v3，OTA 下发 v1。下行是 24kHz、单声道、60ms Opus。`manual` 在 listen.stop 后提交，`auto/realtime` 交给千问 VAD；auto 固件通常在播放时停止普通收音，不能据此宣称支持任意语音插话。

## 音量与屏幕控制

设备声明 MCP 后，网关先完成工具发现，再向千问注册实际可用的状态读取、音量和屏幕亮度工具。这些固定控制工具兼容小智v2.5.2；本轮无需重新刷机。音量、亮度均为 0—100；“关闭屏幕”设置亮度为 0，恢复时可指定亮度。

每次设置后读取设备状态核对，核对失败或超时不返回已核实成功。相对调节要求模型先读当前状态。设备控制仅开放这三个固定工具，不转发设备提供的任意工具描述或网络信息。普通语音阶段另提供本地暂停任务查询工具；恢复、放弃、切换先经过独立归属判断，再由网关应用任务身份和版本，不依赖设备 MCP。

发现总超时 3 秒，失败后继续纯语音；调用总超时 5 秒，最多 4 项在途/排队。工具等待与音频共用串行事件循环，父回复结束、播放排空、工具结果齐全后才续答。打断取消旧轮次的排队操作及续答；已下发的硬件操作不能撤销。

本次修复的检查、真实模型和实机结果见 [控制与音频验证](docs/verification/2026-10-21-device-controls.md)。

## 打断策略与限制

网关在打断时发送 `{"type":"tts","state":"stop","reason":"interrupted","session_id":"..."}`；正常播放结束的 stop 省略 `reason` 字段。这个可选字段本身不会让当前原版固件清空播放队列或支持自然插话，这些能力仍需定制固件实现并通过实机验收。

- 设备 abort、播放期间重新 listen.start、上游 speech_started 均停止旧回复。
- 音频按既定 60ms 时间线发送，停顿或缺音后重置发送锚点，最后一帧发送满 60ms 后再停止。服务器待播 PCM 最多 120 秒（24kHz/mono16，5.76MB/会话），容纳云端比实际播放更快生成的长回复；仍逐帧发送给设备。打断清除队列，重建上游连接，连接代号和回复 ID 阻止迟到音频进入新轮次。
- 当前千问接口的原生 truncate 尚未验证有效。重建会话时注入最多12条、每条512字的近期背景；生成稿以 generated_text 标记，不冒充已听文字。上游触发插话时最多重放2秒近期输入 PCM。
- 双向协商 playback_progress_v1 后可接收设备按播放块回报的 I2S DMA 完成前缀；未协商时维持未知。正常完整最终回报才升级匹配历史的播放确认，仍无法精确同步已听字词；旁支不覆盖原任务，显式恢复后的再次打断更新最新播放段。生成前校验任务快照与归属，text_position 保持 unknown。启用文字记录后可恢复原任务与近期文字；新连接重新分配任务和播放身份，旧采样仅作历史证据，当前播放位置仍未知。恢复不会自动执行旧任务。
- 网关不实现声学回声消除；播放中持续收音需要固件支持及实际 AEC 验证。首版支持按钮/唤醒词 abort；任意语音插话是否可用由设备测试确定。
- 默认单设备并发，连续两分钟没有有效对话活动后断开、返回待机；静音音频、自动 listen.start 和播放遥测不延长会话。VAD 语音区间内的收音、手动录音、模型回复和实际下行播放会更新活动时间，播完后重新留出两分钟。单连接最长 30 分钟。
- 初期 HTTP/WS 入口只用于联调，正式使用前配置 HTTPS/WSS。服务器 API Key 始终保存在服务器，不下发给设备。
- 服务日志不包含音频、对话转录、鉴权值或上游原始错误。

## 文字记录与清空

`ROBOT_RECORD_DIR` 为空时明确关闭持久化，仅保留当前连接的临时背景；非空时启动会独占该目录，打开或校验失败则拒绝启动。`ROBOT_RECORD_RETENTION` 默认 `168h`，必须为正时长。按鉴权头中规范化后的设备 MAC 隔离记录，同一设备同时最多一个连接。

本网关仅保存最多128条有时间、会话/响应身份的文字及暂停任务元数据，每条最多512字；给模型的近期背景最多12条。正文和任务分别按原始时间裁剪，唤醒输入不写入文字记录。生成稿、下发结束、设备完整交付和中断分别标记；未验证的中断稿不会作为已播正文发给模型。新连接的任务使用新身份，保留原请求与终点，旧采样显示为 `historical_playback`，不能用于推断当前块进度。用户指定起点标记为 `user_selected`，不当作测量结果。

记录文件按快照 `UpdatedAt` 加保留期限过期；启动、读取、写入时清理，并由同一个存储 worker 在空闲时定期物理删除。周期为保留期的四分之一，最短 25ms、最长 1 分钟（默认七天保留期每分钟扫描）；正文和任务的逐条裁剪发生在加载及生成/保存快照时。磁盘阻塞会延后清理，删除或目录同步失败会记录无正文、无路径的错误，不能保证存储故障时按时删除。

模型快照中的 `recording_capabilities` 描述临时背景、本网关文字存储是否开启、待写/成功/失败、保留期限、恢复错误和 `audio_retained=false`。异步入队不代表保存成功；只有磁盘回执成功才标记已保存。恢复失败时不会用空快照覆盖旧文件，可在明确清空后重新启用。关闭服务先结束连接并提交最终状态，再在有界关闭期限内排空已接受的保存；超时会记录失败事实，进程强制终止时仍可能丢失尚未落盘的内容。

手动清空请在新一轮对话直接说 **“请清空本设备对话记录”**（也支持“清空对话记录”）。模型使用本地 `conversation_clear_records`，网关只接受本轮真实转写的直接删除命令；否定、引用、假设、历史文字和唤醒内容不能授权。临时背景、暂停任务和恢复数据立即清除，同时关闭原生模型连接；后续生成使用新连接，不再携带清空前的原生会话项、父响应正文或已提交录音。新连接只保留当前清空命令、实际结果及后续输入，磁盘删除回执成功后才返回清空成功；失败时明确报告失败，旧活动会话也不会重新启用。

重连握手异步进行，期间新设备输入有界缓存，abort 可立即停播；新的用户输入会取消过时的清空确认。确认播报使用实际结果快照并临时清空上游工具集，下一轮普通生成先恢复工具并等待确认，防止重复执行清空；确认音频本身不会成为可恢复的暂停任务。

不要在服务运行时直接删除文件，否则内存旧内容可能重新写回。这隔离了本网关使用的活动模型上下文，不代表删除云厂商留存数据。近期背景的 12 条/512 字限制指网关注入的历史快照，不声称限制原生连接在清空前自行累计的全部会话项。

本网关不持久化音频，实时转发和插话使用的有界 PCM 仅在内存中。云端音频和文字的留存由云厂商政策决定，本记录能力不承诺其行为。准确自动接续仍要求文字覆盖/边界验证和设备播放坐标标定；生产未增加辅助 ASR。

## 服务器部署

systemd 模板为 `deploy/family-robot.service`。现有服务器使用 `family-robot` 账户；配置 `/etc/family-robot/server.env` 由 root 管理，权限 `0640 root:family-robot`。发布目录 `/opt/family-robot/releases/<version>/`，`current` 链接指向启用版本。Linux 发布工具链使用官方 SHA256 校验后的 Go 版本，发布前在 Linux 运行完整测试和 vet。

启用模板中的文字记录目录前创建专用目录（修改目录时同时调整 `ROBOT_RECORD_DIR` 与服务的 `ReadWritePaths`）：

```sh
sudo install -d -o family-robot -g family-robot -m 0700 /var/lib/family-robot/records
sudo systemctl daemon-reload
sudo systemctl restart family-robot
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
