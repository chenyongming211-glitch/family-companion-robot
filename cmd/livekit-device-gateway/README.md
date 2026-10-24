# 独立 LiveKit drain 设备桥

只在云端运行，默认监听 `127.0.0.1:18020`；不修改现有 8000 网关。

服务配置：

- `VOICE_CLIENT_TOKEN`：读取现有服务器 `/etc/family-robot/livekit-dev.env`，仅用于向 PCM 后端鉴权，不发给终端。
- `VOICE_DEVICE_BACKEND`：默认 `ws://127.0.0.1:18010/v1/audio?mode=drain`，只接受 loopback drain 端点。
- `VOICE_DEVICE_LISTEN`：默认 `127.0.0.1:18020`，必须为 loopback 地址。

依赖 Go 1.25+、系统 libopus 和 pkg-config；构建、测试和服务启动在云端执行。

## 配对与连接

通过已授权 SSH 在服务器调用 `POST http://127.0.0.1:18020/v1/pair`，JSON 为 `{"device_id":"native-mac"}`。返回 `ticket`、`expires_in:120` 和 `websocket_path`。票据只用一次，绑定 `native-mac`，最多保留 8 个待用票据；通过标准输入交给原生客户端，不落盘或放进命令行参数。该端点的权限边界是服务器 loopback 与 SSH 授权，原生 SSH 转发不可开放给其他主机。

连接 `GET /xiaozhi/v1/`，头部为 `Authorization: Bearer <ticket>`、`Device-Id: native-mac`、`Protocol-Version: 1`。任何 Origin 均拒绝；最多一个会话、最长 15 分钟。首个消息：

```json
{"type":"hello","version":1,"transport":"websocket","audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":20},"features":{"drain":true,"staging":true,"coreaudio_playback":true}}
```

必须具备这三个能力；旧设备明确拒绝。响应 hello 包含当前 `session_id`、24k/mono/60ms Opus 下行参数和播放来源 `coreaudio_playback`，随后转发后端 ready。`listen` 的 `state:start` 可省略，`state:stop` 结束连接。`abort`/`hard.stop` 为显式硬停止；普通插话通过持续上行音频触发后端 VAD，不发送硬停止。

## 音频与采样坐标

上行二进制为协商的 16k 单声道 Opus（20/40/60ms）；解码后仅发送 `listen.audio`，每块最多 20ms PCM16。帧实际时长必须与 hello 一致。

下行先转发 `output.handoff {block_id,epoch,after,input_epoch}`，每个二进制 Opus 包紧邻前置文本元数据：

```json
{"type":"audio.packet","session_id":"...","block_id":1,"playback_id":1,"epoch":0,"samples":1440}
```

不同 block 使用独立编码器。收到后端封口时先把剩余 PCM 加编码器 lookahead 零尾，再补齐 60ms 末帧，最后发送：

```json
{"type":"audio.end","session_id":"...","block_id":1,"playback_id":1,"epoch":0,"sent_samples":1920,"valid_samples":1920,"device_sent_samples":2880,"lookahead_samples":156,"policy":"drain"}
```

数值仅为示例；lookahead 来自实际 libopus 查询。`sent_samples/valid_samples` 是后端有效 PCM；`device_sent_samples` 包含全部编码延迟与末帧补零。终端不得跳过这段时间线后虚报完成。

终端播放前发送 `playback.started`，进度/最终发送 `playback`。两者必须有当前 `session_id`、`block_id`、`epoch`、`basis:"coreaudio_playback"` 和 `sample_rate:24000`；若带 `playback_id`，必须等于 `block_id`。最终回报示例：

```json
{"type":"playback","session_id":"...","block_id":1,"epoch":0,"basis":"coreaudio_playback","sample_rate":24000,"played_samples":2880,"valid":true,"finished":true,"discarded":false}
```

Go 将真实设备位置映射为 `clamp(played_samples-lookahead_samples,0,valid_samples)`，并保留后端 block/epoch。有效 final 必须覆盖整个 `device_sent_samples`；这只证明已下发范围排空，是否整句完整仍由后端 expected 范围判定。迟到/错误会话、旧代次、倒退/越界/非整数回报拒绝；最终回报可幂等重发。后端 `playback.ack` 保留身份回传。unknown 回报转发后结束会话，不能据此授权新音频。

`output.drain`、`output.revoke` 保持后端身份。新 block 只能在 `after` 全部结算后起播；收到 revoke 后，仅未起播待播块可用 `discarded:true,played_samples:0,finished:true` 释放。已经起播的块继续排空。Mac 不接受 `i2s_dma` 来源。

桥转发的文本事件带递增 `control_seq`。终端在串行播放状态中应用该序号，并在起播回报里带上 `observed_control_seq`。如果起播回报与撤销在网络双向交叉，仅当终端起播时已看到 handoff、尚未看到该 revoke，才接受它为已经起播的排空输出；看到撤销之后再启动会被拒绝。此序号表达控制消息的因果次序，不是播放采样或时钟同步，也不增加交接往返。

## 资源上限与证据边界

读取限制 16KiB、消息队列 8 项、单次写入期限 250ms、首个 hello/backend ready 5 秒、终端输入空闲期限 30 秒；块账本最多 16 个未结算块，历史 64 项，每块有效 PCM 最长 12 秒。Go 检查未播有效 PCM 不超过 420ms，另把所有块所需编码尾帧计入最多 600ms 的设备逻辑时间线；后端违反额度时显式终止，避免缓存无限增长。这些是逻辑上限，不是声学延迟实测。

云端 Go 测试使用真实 Gorilla WebSocket、libopus 编解码和受保护的合成 PCM 后端，可验证协议与坐标；不证明 CoreAudio 扬声器实播、真人麦克风、云模型质量或 M5/I2S 行为。
