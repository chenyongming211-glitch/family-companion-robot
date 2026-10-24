# 服务器 LiveKit 最小语音链路

独立测试服务：真实 `AgentSession`＋Fun-ASR→官方OpenAI兼容LLM插件→Qwen实时TTS。仅监听服务器 `127.0.0.1:18010`，没有替换8000上的原Go网关。运行环境、模型Key与自动化测试均在服务器；Mac收音/播放入口见[客户端说明](../../tools/mac-voice/README.md)。

## 服务器启动

已验证的源码目录：`/opt/family-robot-livekit-dev/services/livekit-agent`。运行venv：`/opt/family-robot-livekit-runtime/venv`；单元测试另用checkout下的`.superpowers/livekit-chain/venv`，不共享正式Go服务环境。

```sh
sudo systemctl start family-robot-livekit-dev
sudo systemctl status family-robot-livekit-dev --no-pager
curl http://127.0.0.1:18010/healthz
sudo systemctl stop family-robot-livekit-dev
```

systemd模板见[`deploy/family-robot-livekit-dev.service`](../../deploy/family-robot-livekit-dev.service)。服务使用独立非root账户`family-robot-livekit`，未设置开机自启；停用它不影响原服务。

模型配置由systemd读取既有`/etc/family-robot/server.env`；测试客户端令牌独立保存在`/etc/family-robot/livekit-dev.env`，文件root管理且0600。不要把两份文件内容输出到聊天、命令参数或Git。进程显式`record=False`，SDK/provider日志只保留logger名和级别，阶段/usage记录进入`/var/lib/family-robot-livekit-dev/audit/`，不含正文、PCM或Key。

固定依赖见`requirements.txt`。默认请求模型为`fun-asr-realtime`、`qwen-plus`、`qwen3-tts-flash-realtime`、音色`Cherry`。它们是请求别名，不声称云端模型版本不可变。ASR/LLM/TTS使用三个独立URL；可通过`VOICE_ASR_URL`、`VOICE_LLM_URL`、`VOICE_TTS_URL`覆盖。默认从既有业务空间域名按官方协议分别构造，不把原生Realtime客户端复用为链式接口。

## 当前浏览器入口：连续语音

Mac只点一次“开始说话”模拟唤醒，随后持续发送PCM，说完自动回复、开口自动打断，后续问题直接说。同一按钮可结束对话；空闲60秒、连续讲话25秒或达到上限后回待机。操作见[Mac客户端](../../tools/mac-voice/README.md)，结果见[连续语音验证](../../docs/verification/2026-10-24-continuous-mac-voice.md)。

浏览器连接`/v1/browser/audio?mode=continuous`，cookie/Origin/单会话鉴权不变；输入仅`listen.audio`，输出与playback回报沿用既有PCM协议。`user.state`/`agent.state`/`turn`显示状态与预算，`sleep`结束唤醒。SDK用Silero自动端点/打断，TurnSTT仅将有限语段送ASR，语段在检测时绑定代次；新开口退休旧识别。LLM前检查8轮上限，不将迟到旧ASR计为新问题。自动模式LLM最多512 tokens，指定数数需完整逐项输出。

## 在服务器复现验收

在源码目录执行下列命令。`--out`必须尚不存在，防止误将未知结果当成可自动重试；这是真实计费模型调用，不会自动重试。默认生成三段短合成问题，不采麦克风：

```sh
sudo /opt/family-robot-livekit-runtime/venv/bin/python -m family_voice.smoke \
  --out /var/lib/family-robot-livekit-dev/smoke-new-run
```

复用已经生成的合成输入，避免再次调用TTS生成fixture：

```sh
sudo /opt/family-robot-livekit-runtime/venv/bin/python -m family_voice.smoke \
  --fixtures /var/lib/family-robot-livekit-dev/smoke-20261024-1 \
  --out /var/lib/family-robot-livekit-dev/smoke-new-run-with-fixtures
```

验收依次完成“一加一”“二加三”、数数回复开始后取消、再次“二加三”。检查实际ASR final、回答语义、PCM和模拟播放回报。结果、合成WAV和事件仅保存到私有输出目录，不进Git；不要把目录换成真实对话记录。usage记录不等于已核对账单，取消后未收到完整usage时保持unknown。

单元测试在服务器checkout根目录：

```sh
PYTHONPATH=services/livekit-agent .superpowers/livekit-chain/venv/bin/python \
  -m unittest discover -s services/livekit-agent/tests -p 'test_*.py' -v
.superpowers/livekit-chain/venv/bin/python -m pip check
```

单元测试用真实SDK和受控HTTP/WS边界，不使用真实账号。SDK/provider测试结果与真实模型联调分开记录，见[验证报告](../../docs/verification/2026-10-24-livekit-voice-chain.md)。

## 历史手动探针的PCM测试协议

`GET /v1/audio`升级WebSocket，要求`Authorization: Bearer <VOICE_CLIENT_TOKEN>`；只允许一个活动会话。所有消息都是JSON；音频是base64封装的原始PCM，不是WebM或Opus。

| 方向 | 消息 | 含义 |
|---|---|---|
| 服务→客户端 | `ready` | 输入16kHz、输出24kHz、单声道s16 |
| 客户端→服务 | `input.start` | 取消旧输出、退休旧输入，等待`input.ready` |
| 服务→客户端 | `input.ready` | 本轮服务分配的`input_id` |
| 客户端→服务 | `input.audio` | 带`input_id`与`data`，每帧最多20ms/640字节 |
| 客户端→服务 | `input.end` | 同身份结束输入，服务补700ms终止静音并等实际ASR终态后提交会话 |
| 服务→客户端 | `transcript` / `reply` | 本轮转写（interim/final分开）与助手文字；仅传输，不落正文日志 |
| 服务→客户端 | `audio` / `audio.end` | 带`epoch`、`block_id`的24k PCM与封口；封口不等于播放完成 |
| 客户端→服务 | `playback` | 同身份`played_samples`、`valid`、`finished`；不能把接收入队量冒充实播量 |
| 客户端→服务 | `cancel` | 退休当前输入并停止旧回复；继续处理播放回报，返回`cancelled`；旧`input_id`的后续音频/结束消息返回`invalid_input`，新输入须重新`input.start` |
| 服务→客户端 | `audio.cancel` | 退休旧epoch；客户端丢弃旧代次缓冲，回报已完成前缀 |

## 本阶段边界

- 当前默认页面是连续语音，旧`input.start/end/cancel`只供手动探针兼容。Go小智桥、MCP设备工具和原稿任务检查点尚未在新路径交付。
- ASR只接受真实终态；自动语段由Silero封口，检测不等待旧ASR，最多4个在途/收尾识别任务，云连接关闭等待最多0.1秒。旧段结果不得启动新LLM。历史手动探针仍补700ms终止静音再显式提交。
- 输入队列最多100帧；自动单次讲话25秒触发回待机，有限ASR最多29秒；会话最多8个生成轮次、15分钟，页面显示剩余预算。到期返回`session_expired`，单次操作超时返回`operation_timeout`；不自动重发问题。输出账本最多32段、总待播前缀最多120秒；写出超时250ms即失败。
- 中断最多等待250ms回报结算；无效/缺失回报用unknown保守解锁SDK，独立观测仍为`valid=false`。不计算未验证的字词断点。正常等待期限为本段时长加2秒余量。
- 模拟客户端按PCM时长计时回报，证明协议和生命周期；Mac/手表实际听感和停声分别记录，不用模拟回报代替现场验收。
