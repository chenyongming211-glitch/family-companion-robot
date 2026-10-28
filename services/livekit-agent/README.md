# 服务器 LiveKit 最小语音链路

> **保留的三段式备用方案（2026-10-28）。** 用户决定完整保留ASR→LLM→TTS、专用设备桥、客户端、测试、依赖与部署说明，以便未来评估不采用端到端时使用。当前产品仍为[Go Realtime](../../tools/realtime-output-live/README.md)，不调用或自动切回本链路。下文为备用链路自身的恢复/测试说明；本次没有启停或部署服务，也不要求常驻。历史`DEPRECATED_PRODUCT_PATH`标记不构成删除本链路的依据，详见[保留边界](../../docs/CODE_LIFECYCLE.md)。

独立测试服务：真实 `AgentSession`＋Fun-ASR→官方OpenAI兼容LLM插件→Qwen实时TTS。仅监听服务器 `127.0.0.1:18010`，没有替换8000上的原Go网关。运行环境、模型Key与自动化测试均在服务器；新的主测试入口为[Mac原生终端](../../tools/native-voice/README.md)，经独立Go设备桥18020接入。

## 新主测试入口：原生排空交接

`/v1/audio?mode=drain`仅供Bearer设备连接，浏览器路由不能选择此模式。普通插话保留终端已接收旧音频，停止旧下发，独立准备新答案；终端本地栅栏等旧块实际排空后放行，不等待服务器ACK往返。完整有界原稿由ContentController保存，只有匹配的实际完整单位回报推进句级前缀；“继续”播放同一原稿首个未完整单位，部分句允许重读。主稿与最近旁支分别保留。

运行`family-robot-livekit-dev`与`family-robot-livekit-device`两项独立服务；后者监听loopback18020，原生终端通过SSH和120秒一次性配对票据接入小智v1/Opus。下行24k/60ms、上行16k/20ms，Mac以CoreAudio真实回调结算，Go扣除实际Opus lookahead并检查完整编码尾部。正常窗口300ms、待播120ms、原PCM全局420ms；Go累计设备逻辑额度600ms，不能当成物理声学上限。缓存合计最多三个12秒单位。

两份独立systemd模板启用`VOICE_TEST_KEEP_OPEN=1`：仅drain测试模式取消60秒用户闲置、8轮与15分钟会话预算，ready报告`manual_close:true`、`session_seconds:null`、`max_turns:null`。持续发言超过25秒丢弃该语段，停顿后继续；不重建STT流以免尾段被误识别为新问题。SDK滚动历史最多32项、内存已播历史最多64项，活跃主旁稿不裁剪；开启文字记录后，每轮另读服务器档案，较早内容可回查，不受这两个内存尾部限制。旧manual/continuous模式预算仍保留；单会话、短期票据、传输/播放回报期限和资源额度仍生效。

验证：服务器109项Python、25项历史客户端、3项SDK及Go全库race/vet/build通过；原生CoreAudio和真实云端三句故事→插话→同一原稿恢复完成。真人麦克风/AEC、压力与网络分位数、M5实机和跨唤醒持久化尚待验收，见[本轮报告](../../docs/verification/2026-10-24-native-drain-handoff.md)。旧manual/continuous模式继续保留兼容，其清空策略不代表新的drain行为。

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

## 语音文字存档

用户明确要求保存文字后，独立服务模板通过`VOICE_TEXT_RECORD_DIR=/var/lib/family-robot-livekit-dev/conversations`启用私有JSONL存档。代码默认关闭；目录0700、文件0600，按会话和UTC时间记录，CLI显示上海时间。最终ASR转写、生成稿、固定语音稿/提示、完整播放单位与未确认完成单位分别标注。当前native/drain有完整播放语义；旧manual/continuous仅保存最终转写及SDK生成稿，不标成drain式实播。

这些正文不进入普通日志、Git或音频文件。单写者线程使用有界队列并逐条fsync；关闭时排空。写盘或队列失败会报`text_recording_failed`并结束连接，不能默默转成无记录对话；损坏档案在列表中单独标明，其他会话仍可读取。档案不自动过期。drain模式会给模型提供有界的当前通话文字、上一轮正文和历史元数据；文字记忆与跨会话播放器恢复是独立能力。首次启用前没有保存的LiveKit正文无法补回，原8000快照保持独立来源。

### 模型读取历史与自然反问

每轮在线程中读取最终转写和完整实播，unknown单独标注，生成稿/未播稿不冒充已听历史。短通话包含全文；长通话使用近期原文与带来源序号的提取式摘要（摘录，不保证概括每个细节）。档案上下文最多18000字符。模型可用`conversation_history`列会话、逐页读原文或搜索；列表可按`next_session_id`一直向前翻，搜索`all`仅覆盖最近20份，其`continuations`按具体会话续读，禁止全局seq游标。每页最多20条、结果最多12000字符；长单条按`next_offset`无损续读，游标属于对应会话。单档16MiB、一次搜索总扫描32MiB，超额/损坏明确返回unavailable。档案为只读低信任资料，不作为新的系统指令；当前单设备家庭测试共享此私有目录，多用户授权隔离另行实现。

正式回答与内容操作由模型明确选工具：正常答案/故事`respond`、自行措辞的反问`ask_clarification`、续讲/查询等内容工具。完整`tool_calls`终态通过后才执行，截断参数和工具外正文不播出。内容工具返回真实状态；成功恢复播放固定原稿，其他状态由同一模型继续理解和回答。没有关键词直接播报固定问句的路径。反问/状态回答用独立dialogue槽，不能覆盖主稿与旁支；确认完整播完后进入助手历史与档案，下一轮能理解用户对问题的回答。新语音起始会退休旧工具和临时播放。

每轮另有一次可选48-token短模型请求，与正式回答并行，只读本轮请求和少量近期语境，不等待全文档案；完整JSON短句通过校验后作为准备语，空text表示省略。由模型自行措辞，没有固定语音模板，也无工具执行权。准备语复用progress槽与每轮一次提示限额，最多20字符/3秒PCM、启动后2秒内才允许首帧；正式答案先就绪则撤销未下发提示，已下发部分按原drain结算。为了不排在主模型生成后，准备语在同一播放锁内直接经过DrainOutput账本，正式内容仍走SDK。新语音/失败/关闭退休旧任务，最多4个含清理中的短请求。额外调用与确认/正文耗时分别审计，见[并行准备语验证](../../docs/verification/2026-10-24-parallel-confirmation.md)。

每轮最多3个只读步骤，预留第4次模型调用给回答或自然反问。每轮附带最新main/side快照，模型直接据此回答，不再提供重复的`query_content`入口（兼容调用仍保留）；旧话题应查通话档案，重复历史游标会结束读取阶段。上下文足够时模型直接回答。明确换话题可一次调用`respond(action=replace,text=新完整正文)`，先验证正文，再替换旧稿，减少一次模型往返；默认reply仍保留暂停故事。

真实回查可在`conversation_history.acknowledgement`中由模型自行拟定一句查询提示，每轮最多一次、24字符；没有固定语音模板。独立progress槽与查档并行，不占用主旁稿。可选提示准备最多3秒、PCM最多6秒，失败不终止正式答案；正式答案或恢复动作到达时撤销尚未下发的提示，已下发部分继续按drain结算。它的完整实播也进入文字档案。提示与实质答案的延迟分别统计；当前等一个短语TTS完整生成后下发，并未实现流式TTS。

针对真人通话的67字/13.44秒回答被12秒保护拒绝的问题，drain按自然短语优先切分成约24字符单位，完整原文和冻结后的检查点保持不变；标点尾段与相邻正文归并，数字/小数/英文词/URL优先保持完整。最多192个播放单位，单个PCM仍最多12秒。main/side/dialogue各预备2个单位、progress为1个，最坏PCM为6×12+6秒（约3.744MB），输出与排空额度不变。下一单位准备失败时先结算当前实播；完整排空推进到失败单位后，一次明确继续即可重试未播部分。此修复和普通插问仍约2.5秒的限制见[真人报错验证](../../docs/verification/2026-10-24-real-reply-audio-limit-fix.md)。

前一版v1的190项云端回归、真实模型对照和原生合成插话接续通过。换题3组首帧中位减少约21%；历史查档样本未提速，普通问答无普遍提速证据。具体测量边界与上游超时见[验证报告](../../docs/verification/2026-10-24-voice-latency-v1.md)。

在服务器源码目录经已有SSH读取，不新增公开HTTP或网页入口：

```sh
cd /opt/family-robot-livekit-dev/services/livekit-agent
sudo /opt/family-robot-livekit-runtime/venv/bin/python -m family_voice.conversation_log \
  --directory /var/lib/family-robot-livekit-dev/conversations list
```

从列表取得会话ID后，用同一入口将`list`换成`show 会话ID --limit 100`；后续页使用输出的`--after`，`--json`可读完整事件，`--include-generated`明确标注未确认播放的生成稿。默认视图只展示最终转写、已确认完整播完及本句未确认播完的文字。

固定依赖见`requirements.txt`。默认请求模型为`fun-asr-realtime`、`qwen-plus`、`qwen3-tts-flash-realtime`、音色`Cherry`。它们是请求别名，不声称云端模型版本不可变。ASR/LLM/TTS使用三个独立URL；可通过`VOICE_ASR_URL`、`VOICE_LLM_URL`、`VOICE_TTS_URL`覆盖。默认从既有业务空间域名按官方协议分别构造，不把原生Realtime客户端复用为链式接口。

## 历史浏览器入口：连续语音兼容

Mac只点一次“开始说话”模拟唤醒，随后持续发送PCM，说完自动回复、开口自动打断，后续问题直接说。同一按钮可结束对话；空闲60秒、连续讲话25秒或达到上限后回待机。操作见[Mac客户端](../../tools/mac-voice/README.md)，结果见[连续语音验证](../../docs/verification/2026-10-24-continuous-mac-voice.md)。

浏览器连接`/v1/browser/audio?mode=continuous`，cookie/Origin/单会话鉴权不变；输入仅`listen.audio`，输出与playback回报沿用既有PCM协议。`user.state`/`agent.state`/`turn`显示状态与预算，`sleep`结束唤醒。SDK用Silero自动端点/打断，TurnSTT仅将有限语段送ASR，语段在检测时绑定代次；新开口退休旧识别。生成前检查8轮上限，不将迟到旧ASR计为新问题。自动模式LLM最多512 tokens。

明确计数范围和“继续数”在LLM前进入独立CountController：保存原终点与首个未确认完整播放的数字，每个数字用固定PCM和`add_to_chat_ctx=False`播放；同块完整有效回报且SDK未中断才推进。插话暂停，普通问题仍走LLM，继续时复用内存音频并保守重读当前数字。`count.state`显示进度，completed的next=end+1仅为终态哨兵，不合成/播放它。最多预取3个数字、每个5秒，范围0..1000、一次最多100个；没有跨唤醒/进程持久化。见[计数验证](../../docs/verification/2026-10-24-counting-resume.md)。

口语前缀规范化覆盖“啊那继续数吧”等表达；其他自然恢复请求可由模型调用无起点参数的resume_counting，后台使用真实检查点，StopResponse阻止工具后生成数字稿。模型每轮获得只读任务快照；工具绑定语音起始代次，迟到旧轮次不能恢复任务。`turn.route`供页面显示有界数值诊断，不含用户正文/音频；结束后保留的页面检查点明确标注上次会话，不能跨唤醒恢复。

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
| 服务→客户端 | `transcript` / `reply` | 本轮转写（interim/final分开）与助手文字；启用文字档案后final/SDK生成文字独立存档，普通日志仍无正文 |
| 服务→客户端 | `audio` / `audio.end` | 带`epoch`、`block_id`的24k PCM与封口；封口不等于播放完成 |
| 客户端→服务 | `playback` | 同身份`played_samples`、`valid`、`finished`；不能把接收入队量冒充实播量 |
| 客户端→服务 | `cancel` | 退休当前输入并停止旧回复；继续处理播放回报，返回`cancelled`；旧`input_id`的后续音频/结束消息返回`invalid_input`，新输入须重新`input.start` |
| 服务→客户端 | `audio.cancel` | 退休旧epoch；客户端丢弃旧代次缓冲，回报已完成前缀 |

## 本阶段边界

- 新的drain主路径已接Go小智/Opus桥、原生Mac终端和会话内故事原稿检查点。下列清空/长输出额度针对旧manual/continuous兼容模式。MCP设备工具、Go持久化和新手表固件尚未在drain路径交付。
- ASR只接受真实终态；自动语段由Silero封口，检测不等待旧ASR，最多4个在途/收尾识别任务，云连接关闭等待最多0.1秒。旧段结果不得启动新LLM。历史手动探针仍补700ms终止静音再显式提交。
- 输入队列最多100帧；自动单次讲话25秒触发回待机，有限ASR最多29秒；会话最多8个生成轮次、15分钟，页面显示剩余预算。到期返回`session_expired`，单次操作超时返回`operation_timeout`；不自动重发问题。输出账本最多32段、总待播前缀最多120秒；写出超时250ms即失败。
- 中断最多等待250ms回报结算；无效/缺失回报用unknown保守解锁SDK，独立观测仍为`valid=false`。不计算未验证的字词断点。正常等待期限为本段时长加2秒余量。
- 模拟客户端按PCM时长计时回报，证明协议和生命周期；Mac/手表实际听感和停声分别记录，不用模拟回报代替现场验收。
