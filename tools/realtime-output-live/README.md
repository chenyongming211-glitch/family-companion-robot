# 真人Realtime已播位置实验

> **当前目录包含两类代码（2026-10-25）。** Swift客户端、构建和默认启动器仍在使用；server/upstream/audio/state/history/identity.py已标记DEPRECATED_PRODUCT_PATH，仅保留临时Python回退与差异测试参考。新功能只进入Go。Python18032仍在线，不表示它仍是默认入口。见[代码生命周期](../../docs/CODE_LIFECYCLE.md)。

运行`./tools/realtime-output-live/open.command`，默认打开蓝色“小满 · Go 测试版（云端 18040）”。点一次开始并允许麦克风，之后自由说话。结束按钮停止收音，窗口保留；关窗退出并清理本启动器的SSH转发。不要同时开启其他语音窗口的对话。

当前先听排空策略：让它讲一个故事，播放中插问“2加3等于几”；听旧尾音多久停、新回答多久开始。之后说“继续刚才的故事”，核对语义衔接。也可以用数数核对候选断点，实现没有计数专用规则。准确接续位置仍未验收。

当前保留两个独立入口：Go测试版云端18040、Python对照版云端18032。标识修正后，用户已确认两端体验接近，后台复验无运行故障；默认启动器已切到Go 18040，Python仅作对照/回退。一次性票据、单会话、拒绝浏览器Origin保持，Mac只构建/运行客户端，Key仅留服务器。

两个窗口使用不同标题和常驻标识：**蓝色Go测试版（18040）**在左，**橙色Python对照版（18032）**在右。先结束一个窗口的对话，再开始另一个；窗口由用户自行关闭。标签中的云端端口来自启动器的远端映射，不能只看本地转发端口猜后端。Go标签由health.backend确认；保留的Python基线通过受控18032映射与其health签名识别，无法确认时显示“未核对入口”。

```sh
# 默认Go入口
./tools/realtime-output-live/open.command
# Python对照（本地18042仍映射云端18032）
OUTPUT_LIVE_LOCAL_PORT=18042 OUTPUT_LIVE_REMOTE_PORT=18032 ./tools/realtime-output-live/open.command
```

SSH ControlMaster仍通过`REALTIME_NATIVE_SSH_CONTROL`指定。Go服务不调用Python；本说明中的Python函数/命令是保留基线的参考实现，Go对应`internal/realtimelive`、`internal/realtimestate`和`internal/realtimeaudio`。验证和时延边界见[迁移验证](../../docs/verification/2026-10-25-realtime-go-migration.md)。

## 数据路径与差别

Mac Voice Processing持续收音，16k/20ms Opus上传；云端原生千问连接只做VAD与输入转写。按原生VAD音频时间戳切出实际话段，原音频只在内存保留最长35秒；每段最多25秒。每轮新千问原生音频连接接该话段＋最近16轮文字背景，边生成边输出。这里多了原生VAD监听连接和逐轮建连，不能当成原18030无改动性能。没有接入输入Fun-ASR、文本LLM或TTS，也没有共享三段式档案。

输出按24k/60ms Opus放行，常规完成回报窗口约300ms。编码和解码保持同一时间轴，**只有实际下发包**解码后进入输出Fun-ASR。没有预备整段源音频给ASR。包携带响应序号防止迟到串音。插话取消旧生成，未下发旧队列随任务取消释放；播放端保留已收到音频并排空，不能立即清空。120ms保护量仍是实验值，不保证声学逐字准确。

服务器封口发 `drain {id, sent}`。播放端核对 `accepted == sent`，等全部 CoreAudio `dataPlayedBack` 后回 `receipt`，其中 `terminal=drained`、`completed == accepted == sent`。封口与包发送用同一锁排序。旧回复完成确认前，输入仍持续收音，但下一轮背景快照与新输出不能越过这个栅栏；3秒无完成确认则提示并断开，重新开始可重连。正常 end 与 drain 竞争也会确认同一边界。主动结束/关窗仍立刻关闭音频设备。

下一轮带用户转写和结构化已播证据，不把旧未播生成稿放进模型背景。有界等待旧ASR最多1.2秒，通常与用户新一句话的收音并行。每轮重建会丢失此前部分声音上下文；仅保留近16轮，不是长期记忆。排空对齐的是下发和播放的样本边界，不会自动截断千问内部历史，也不保证输出ASR精确对齐每个字。本实验靠新连接和恢复背景隔离旧未播稿。

## 已播上下文与自然反问

`playback_evidence`保留两种来源：中断回复的最终输出ASR安全时间前缀，以及整段正常生成且全量完成播放的原生转写。常见中阿数字和宽度格式可保守对齐原稿；有歧义的数字串不强行等价。对齐失败仍保留ASR观察文本，不能整段null。缺句或非法后续时间停止扩展，保留此前有效部分。原始严格probe候选仍单独记录用于比较。

`recent_turns`按时间顺序保留最近16轮用户发言和每轮已播内容，供模型理解整段上下文；不使用关键词给问题分类，也不单独提取末尾数字决定回答权限。`assistant_spoken_prefix`保留可用已播文本，不能与生成稿对齐的ASR观察也以候选来源保留，不再因对齐失败让主文字字段为空。

所有问题直接流式消费原生模型音频，不再完整缓存回查回答、检查数字/限定词、拒绝播放或要求重写。提示词让模型依据上下文自然回答或续讲；指代不清或缺少影响回答的信息时，由模型围绕当前问题自然反问，不套固定问句，不把记录缺失当作事情没发生。系统继续隔离未播尾稿；播放回报、排空协议与提供方异常处理保持。

取消硬拦截消除了该层造成的静默与完整生成/重写等待，不表示模型一定理解正确。原始ASR、120ms边界和16轮范围仍有限制；语义续讲不是逐字原稿恢复，精确停点和真人自然度仍须单独验证。没有增加固定兜底语音、文本LLM或独立TTS。

### 讲述完整性提示

Go18040增加独立讲述规则：默认约120字的完整短故事，用户指定篇幅优先；长故事每次推进有阶段性结果的一段。明确续讲或催促未完成故事时直接交付后续正文，沿用实际已播记录的人物和事件。播放证据里的complete只表示该段音频播完，不表示故事或任务已经完成。

规则位于背景JSON之后，没有固定示例故事、额外运行时模型调用、关键词裁判或自动续播。普通问答、停止/换题、流式音频和插话排空保持。提示只能改善生成倾向：仍发现猜测网络原因、无上下文时只答应继续、续讲在悬念处收尾等反例，不能当作可靠的任务完成判定。详见[本轮验证](../../docs/verification/2026-10-25-storytelling-prompt.md)。已有客户端下次开始对话即可使用新提示，无需重编或关闭窗口。

## 小满身份与当前能力

本入口使用仓库`config/product-identity.json`中的“小满”产品身份。文件只读、在进程启动时加载；开发者和产品归属未提供，保留为空。`identity.py`仅取产品资料、实际配置中的模型ID/可确认供应商、本入口能力和近期历史范围，不向模型传配置URL、票据或Key。修改身份档案后需按部署流程重启18032。

`instructions_for`在每次原生回复连接前注入相同产品身份及新的能力快照，包含当前音频输入通道、连接状态、当前会话最多16轮已播上下文。没有新增身份分类器、固定身份播音或额外模型请求。正常问答按原路径流式输出。

语音聊天、一般问答、故事和中英文交流可用；近期回忆与语义续讲仍在完善。内置联网搜索按下面的开关配置。当前没有全量档案检索、跨会话记忆、图片输入/分析、提醒/闹钟、设备控制或实体动作接口。服务器留存文字记录与模型能否读取以前会话分开说明，不把不可读取说成记录已删除；服务器不保存真人原音频。能力说明由模型根据问题自然组织，不保证任意问法下都不会误述，需看真实模型及终端验证。

Go18040与保留的Python18032都加载该身份文件；其他入口、三段式、会话历史和工具没有因此合并或改名。见[身份方案](../../docs/superpowers/specs/2026-10-25-xiaoman-identity-design.md)。

## 内置联网搜索

Go18040和Python18032默认开启`enable_search`，并请求`search_options.enable_source=true`。模型自行决定需要搜索的问题；不增加外部文本LLM或强制每轮查询。仅生成连接联网，VAD/转写监听连接明确关闭。每轮核对服务端配置确认，使用实际搜索计量和来源记录验证查询，不把模型自称查过当作证据。

搜索次数、策略与返回来源保存在服务器私有intent及`events.jsonl`的`native_search`事件，普通日志不打印链接/正文。当前窗口不新增来源列表UI，模型可自然回答来源问题；元数据处理不阻断正常音频。没有返回计量不擅自记为已确认的0。

服务环境可设置`OUTPUT_LIVE_ENABLE_SEARCH=0`关闭，默认值为1，重启对应入口后生效；无需修改模型Key配置。该开关不改变其他入口或旧探针默认行为。官方要求内置搜索与`tools`互斥；当前Go设备控制使用独立search=false/text-only工具会话，普通语音仍沿用原搜索配置。只有声明MCP的实际设备获得自己的工具，Mac不假装能够控制音量或亮度，见[设备控制方案](../../docs/superpowers/specs/2026-10-27-realtime-device-controls-design.md)及[验证](../../docs/verification/2026-10-27-realtime-device-controls.md)。

搜索会增加等待，真实查询结果也可能混合预报/实况，不能仅因有来源就认为天气数据已核实为实时观测。其他选项及版本限制见[配置清单](../../docs/verification/2026-10-25-realtime-config-switches.md)，实测见[搜索验证](../../docs/verification/2026-10-25-realtime-search.md)。

这是通用播放端策略。当前Mac、M5和微雪分别已有各自适配，实机结果与未验收边界以[开发交接](../../docs/DEVELOPMENT_HANDOFF.md)为准。新的微信小程序、App或其他ESP32板仍需分别实现封口、排空与可信完成回报，不能只改服务器就推定所有终端通过。原实验计划见[历史实施步骤](../../docs/superpowers/plans/2026-10-25-realtime-drain-test.md)。

## 云端运行与记录

### Go18040终端与会话归属

新版Mac客户端在首次开始时通过现有SSH转发登记终端，之后以终端专用凭据换取一次性票据。相同安装/profile重启、重连仍是同一个`device_id`，每次连接生成新的`session_id`；一轮回复的完整编号是`session_id + id`。这些编号不代表说话人的身份，也不自动恢复上次聊天或音频。

客户端凭据仅在本机`~/Library/Application Support/FamilyCompanion/Realtime18040/`的0600文件内，目录0700。默认真人客户端使用`default-mac_native.json`；合成验证使用独立`mac_fixture`身份。可用`--device-profile validation-a`、`validation-b`模拟不同逻辑终端。它们不是不同物理设备或硬件证明。不要上传、打印或复制凭据内容；文件损坏或认证失败会明确连接失败，不自动覆盖/换身份。

18040新增`POST /devices`，只允许当前回环/SSH信任边界内登记，不作为公网自助绑定接口。服务器生成身份、仅落凭据摘要；`POST /pair`的身份与凭据必须匹配，票据绑定终端，`/live`无法改绑。新客户端的控制和播放回报必须匹配当前`session_id`。原有Opus封装、样本窗口、取消与排空策略不变。

每个新会话目录增加`session.json`，包含终端、会话、客户端类型/版本、服务构建和起止信息。`events.jsonl`及提供方intent都附同一归属，生成、下发、实播仍分别记录。服务器`devices.json`是私有登记表，不是聊天正文。未完成或崩溃时没有结束字段，不能推断已正常结束。

旧窗口/无身份客户端仍可连接，记录标为`legacy_unattributed`，不推测其稳定终端身份；已有旧记录不批量改写。**需要启动新版客户端，才会获得稳定终端归属。** 旧Python入口不受此次改动影响。

仅在云端通过新CLI查询，默认只输出元数据；明确选定一个会话并加`--events`才输出私人对话事件。没有公开历史HTTP接口，也没有跨会话模型记忆。

```sh
/opt/family-robot-livekit-runtime/bin/realtime-records --device dev_终端编号
/opt/family-robot-livekit-runtime/bin/realtime-records --session 会话编号
/opt/family-robot-livekit-runtime/bin/realtime-records --session 会话编号 --events
```

Mac纯凭据文件验证运行`./tools/realtime-output-live/test-device-identity.command`，不启动服务、收音或调用模型。Go所有运行测试仍只在云端执行。

当前Go服务为`family-robot-realtime-output-go.service`，回环18040；部署单元在`deploy/`，启动文件为`cmd/realtime-output-live/`。仅在云端构建/运行；Mac不启动语音服务。当前Go私有文字/Intent目录为`/var/lib/family-robot-realtime-test/verification/output-live-go/`。默认搜索开关保持`OUTPUT_LIVE_ENABLE_SEARCH=1`。

下面是仍保留的Python基线命令，在服务器checkout根目录使用既有环境：

```sh
/opt/family-robot-livekit-runtime/venv/bin/python -m unittest discover -s tools/realtime-output-live -v
/opt/family-robot-livekit-runtime/venv/bin/python tools/realtime-output-live/server.py
```

本轮通过独立临时systemd单元`family-robot-realtime-output-live.service`保持运行。只读健康检查`http://127.0.0.1:18032/healthz`。不修改8000、18010、18020、18030的配置或入口。关闭客户端释放会话，服务不自动关停，以便用户反复开始。

私有文字证据在`/var/lib/family-robot-realtime-test/verification/output-live/<session>/events.jsonl`：输入转写、带未播标记的生成稿、ASR稳定字时间戳、播放回报和实际送入下一轮的背景分别记录。音频仅内存；普通日志只记录事件/计数/错误类型，不输出正文、票据或Key。真实模型调用前落无正文intent，无未知结果自动重试。与三段式存档独立。

合成输入检查可加`--fixtures /absolute/path/plan.json`，复用原生客户端的按20ms定时输入工具；不启用麦克风。它验证实时I/O与传输，不替代用户声学核对和准确接续验收。当前已观察到严格ASR匹配返回unknown的情况，参见[验证记录](../../docs/verification/2026-10-24-realtime-output-live.md)。

云端`replay_history.py /private/path/events.jsonl`只输出历史重放的统计；`probe_grounding.py --events /private/path/events.jsonl --turn N --wav /private/synthetic.wav --run-provider`明确运行真实模型回归，默认不打印正文，`--show-reply`才显示生成回答。结果/调用intent保留服务器私有目录，合成探针不落音频，不替代真人验收。
