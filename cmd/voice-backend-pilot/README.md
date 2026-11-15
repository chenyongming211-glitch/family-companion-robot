# 云端静默语音链路验证

仅用于对比测试，**没有播放、麦克风、设备连接、监听端口或服务控制功能**。不是新的产品服务，也不是已接通微雪的双后端开关。使用现有Go和gorilla/WebSocket，无新增依赖。

**2026-11-15实际状态：** TTS及原生真实协议适配已修，77项云端检查/race/vet/build与只读复核通过；A、B真实单轮文件输入均completed，分别取得5.89秒/18字词及5.57秒/16字词。没有播放或连接设备，仍未实现产品连续双后端。用户已明确继续必要开发测试，费用要求指成品长期API成本，不再因小额开发预留反复申请确认；历史失败/未知记录保留，见[最新记录](../../docs/verification/2026-11-15-tts-metadata-compatibility.md)。

配置、模型凭据、输入、音频和结果只在云端。Mac只编辑和同步代码，运行测试与构建均在云端。私有结果不入Git；不使用历史真人录音作为合成测试输入。

## 运行边界

- 默认不允许API调用，`--allow-provider-calls`必须显式提供；只接受北京业务空间下现有Omni3.8配置。
- `tts`：公开合成短句→带字词时间戳的TTS；`native`：16k PCM语音→Omni仅文字回答→同一TTS；`cascade`：同一PCM→Fun-ASR→Qwen Plus非思考→同一TTS。
- 原生和三段式均只做单轮批处理，未实现设备连续会话/中断恢复；不将此命令作为首音时延或实机听感证据。上游音频从未送到扬声器。
- 输入为普通云端PCM16/s16le/16k/mono文件，最多10秒；TTS探针最多120字符。回答和合成总音频有上限；TTS每个完整短句独立任务，按实际PCM累计偏移，拒绝无法解释的多句/缺失/乱序时间戳。
- 默认使用`cosyvoice-v3-flash/longanyang`作系统音色预检，单价按1元/万计费字符，汉字计2字符。3.5仍为待验证候选；其音色必须显式指定已创建ID，不借用v3系统音色，不自动克隆。
- 全部API经`--budget-file`共享预留，`--budget-fen`最多100分。预留永不退款，未知不按零费用计；预留不等于真实账单，保留每个API的usage待核对。
- 每个TTS任务最多64字符，即最多128计费字符，按v3 Flash价格最高0.0128元，预留0.015元；只要失败或计费数字缺失/无效，立即停止后续付费步骤。
- 原生模型设置`max_tokens=256`并验证配置确认，关闭搜索、工具及辅助转写。官方说明该参数只裁切返回长度，不保证内部生成量；原生步骤因此按0.40元保守预留，不能拿256个返回Token当作实际账单。
- `--ledger`必须指向既有48主回答/8协议探针的JSONL，不另建来绕过总量。每个明确批准的日期阶段最多2条TTS探针、native/cascade各1条主回答；标识形如`quiet-voice-YYYYMMDD[-阶段]/名称`，日期/字符严格验证；阶段名称不代表自动授权，必须沿用全局账本；同一attempt不能重用，没有自动重试。
- 后续实机16答初筛仍待实时设备接入。用户已在现场并授权继续必要测试；沿用账本与合理界限，不重置未知费用或无条件重试。

## 云端命令示例

在已核验的独立云端源码中构建；输出路径使用本任务私有目录，所有示例均无播放命令：

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go test ./internal/voicecompare ./cmd/voice-backend-pilot
/opt/family-robot/toolchains/go1.27.3/bin/go test -race ./internal/voicecompare ./cmd/voice-backend-pilot
/opt/family-robot/toolchains/go1.27.3/bin/go vet ./internal/voicecompare ./cmd/voice-backend-pilot
/opt/family-robot/toolchains/go1.27.3/bin/go build -o /home/ubuntu/quiet-voice-backend-20261114/voice-backend-pilot ./cmd/voice-backend-pilot
```

新路径及attempt只消费一次；下面是真实计费调用，仅在已冻结源码/二进制、门控检查和预算允许后执行：

```sh
/home/ubuntu/quiet-voice-backend-20261114/voice-backend-pilot \
  --allow-provider-calls --backend tts \
  --config /etc/family-robot/server.env \
  --text '请简单介绍一下你自己。' \
  --out /home/ubuntu/quiet-voice-backend-20261114/tts-short-v1 \
  --attempt quiet-voice-20261114/tts-short-v1 \
  --ledger /home/ubuntu/waveshare-native-integration-20261114/attempts.jsonl \
  --budget-file /home/ubuntu/quiet-voice-backend-20261114/api-costs.jsonl \
  --budget-fen 100
```

主回答将`--backend`改为`native`或`cascade`，移除`--text`并显式提供`--input <云端合成PCM文件>`，为输出目录和attempt使用新的名字；不能重放失败或未知的原输入。没有合格合成输入则停在TTS验证。

`result.json`为私有文字、时间戳、模型身份和API usage；已完成且验证通过的音频保存为`output.pcm`，后续失败时也保留这些局部结果，并标记失败/未知。控制台仅输出状态、样本数及结果路径，不输出正文、原音或Key。没有硬件回执，因此结果不能证明微雪已播位置或扬声器实际精度。

原始JSON事件保存在私有`events.jsonl`，上限8MiB，逐事件持久化；不包含二进制音频或握手凭据。记录失败会终止调用，失败字词/音频仍保持不可信。原生额外错误及完整元数据仅写入云端私有结果，控制台继续隐藏正文。
