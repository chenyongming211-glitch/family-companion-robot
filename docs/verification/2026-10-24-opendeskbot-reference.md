# OpenDeskBotV2 源码参考：打断、播放证据与自动接续

2026-10-24核对。对象：[tudoom/OpenDeskBotV2](https://github.com/tudoom/OpenDeskBotV2)，main提交`1cf5050ee63b1fe4cae6b7815e565a0955f15bd6`，该提交时间2026-09-16。本次通过GitHub连接器取得固定版本文件，核对32份文件的Git blob SHA，全部一致；隔离运行15项上游纯逻辑测试。没有安装整套客户端/SDK、调用供应商、连接设备、替换现有网关或刷固件。

## 结论

值得参考的是：分段TTS、插话防误触发、旧音频隔离、设备终态回执、按交付状态筛选历史。当前核对的主语音/TTS/固件播放路径没有展示“故事中断→旁支问答→保存单位检查点→继续同一原稿”的完整实现，不能把支持打断、LiveKit默认恢复或播放回执当成已解决自动接续。

本项目支持我们探索“明确内容单位＋真实播放证据”的方向，但没有证明上一份分段接续提案已经可用，也不构成直接迁移整个网关的依据。

## 架构与我们不同

- ESP32-S3通过USB或可选局域网链路连接PC；PC运行服务和模型API请求，不是完全离线模型。我们的设备经公网连接Go网关，不能直接照搬其loopback信任边界或USB协议。
- 主要语音路径为LiveKit＋LampGo Agent SDK＋用户输入ASR→通用LLM→豆包TTS，区别于我们当前千问原生实时语音。`pyproject.toml`固定`lampgo-livekit-agent-sdk==0.3.0`，并非只有上游LiveKit一个依赖。
- README和部分架构文档存在差异：README包含可选Wi-Fi，`ARCHITECTURE.md`仍有“无Wi-Fi/USB-only”描述。因此按实际文件和调用路径判断，不能把单页架构说明当运行事实。

来源：[README](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/README.md)、[依赖定义](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/pyproject.toml)、[RTC适配器](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/rtc_livekit_plugins.py)。

## 可借鉴的机制与边界

| 机制 | 源码里的实际做法 | 对我们的参考价值 |
|---|---|---|
| 分段TTS | 常规PB路径按句末标点切分；RTC流式适配器还按长度/标点/flush触发合成 | 可以参考预取、段边界和标点处理，但“已经合成一段”不等于设备完整播完；该项目也未因此形成故事游标 |
| 插话确认 | 设备AEC为基础，服务端结合转写长度、持续时间、近端响度和近期TTS相似度，拒绝弱回声 | 参考分层判据；不能仅因用户复述了机器人用词就拒绝，也不能直接移植适合其PCB的响度阈值 |
| 旧音频隔离 | 清除主机缓冲及设备流，generation阻止旧DATA/END/CANCEL；取消后先观察非speaking状态再允许新speaking | 可用于检查迟到音频把播放重新打开的问题；状态跳转门仍不等于文字检查点 |
| 接收/执行分开 | PB区分accepted与played，request/idx归属，等待终态后才保存回执 | 继续坚持“发出了”不等于“说完了”；不能直接给我们的I2S前缀换一个同名字段就认为兼容 |
| 持久化回执 | 设备/请求联合键、幂等写入、过期清理 | 参考不可重复推进和重复回执不无限续期的原则，无需为此把现有有界快照换成SQLite |
| 历史交付状态 | `session_history_for_llm`排除明确不是played的助手消息 | 与我们排除未播生成稿方向一致；仍需为部分已播内容建立可恢复游标 |

具体来源：[分句](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/tts/text_split.py)、[RTC分段合成](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/rtc_livekit_plugins.py#L1048-L1244)、[插话判断](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/rtc_barge_in.py)、[取消旧音频](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/rtc_runtime.py#L594-L647)、[传输代次](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/infrastructure/serial/session.py#L1403-L1548)、[持久回执](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/playback_receipts.py)、[历史筛选](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/session_store.py#L225-L260)。

## 三个不能混为一谈的地方

**1. 误打断恢复与旁支结束后的任务接续。** `resume_false_interruption=True`处理误触发暂停后恢复旧语音，并没有提供“接纳一段新会话后，再恢复原故事检查点”的业务语义。此项目的普通插话门槛配置为持续1秒，显式停播词可绕过纯逻辑时长门；完整链路还有响度/回声分支，不能把这个参数称为实测停播延迟，也不应直接照搬到我们的快速插话目标。

**2. PB表演回执与RTC语音播放位置。** PB使用request/idx和终态ACK；RTC路径通过LiveKit远端音频→PCM→Opus→独立AUDIO_DOWN流，串口generation是主机并发标记，不是逐句已播索引。已读路径中没有将TTS文字单位、设备连续已播前缀和可恢复故事状态完整连接起来。

更具体地，固件PB按时间轴追赶时可以跳过晚到PCM，随后执行正常尾部处理并给出完成状态；因此played在这里不能直接证明所有文字都播出。故事“不漏内容”需要我们自己的更严格交付语义。来源：[固件播放](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/hardware/firmware/audio_player.cpp#L920-L1034)、[独立RTC流](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/infrastructure/serial/session.py#L1403-L1548)。

**3. 供应商词时间戳与估算口型。** 豆包适配器请求timestamp/subtitle并解析`TTSSentenceEnd.words`等信息；有对应起止时间的词可继续评估。口型路径也包含没有有效时间信息时的分配/估算，不能拿估算口型当真实词尾。RTC TTS适配器把PCM送给AudioEmitter，未在这段已读代码中把返回的词时间表用于设备播放断点或故事恢复。不能据此推断当前千问原生或千问TTS具备相同能力。

来源：[豆包请求与返回](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/tts/doubao.py)、[词区间转口型](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/tts/doubao_words_phoneme.py)、[无时间信息时的后备路径](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/service/src/deskbot_server/tts/doubao_phoneme_align.py#L229-L262)。

## 本轮实测及其限制

在隔离源码快照中，用Python标准库加载并直接执行上游测试文件内的零参数断言函数：`test_rtc_barge_in.py`12项、`test_tts_text_split.py`3项，合计15项通过、0失败，执行部分约0.029秒。没有运行pytest全套、供应商插件、真实模型或设备。

独立调用纯逻辑门：普通插话“你喜欢吃什么”0.5秒为false、1.2秒为true；“停一下”0.2秒为true。这是函数行为，不是端到端延迟测试。它验证了该项目选择偏保守插话的事实，不能证明我们的AEC阈值应当照抄。

私有源码/文件哈希/测试结果保存在ignored `.superpowers/opendeskbot-reference-20261024/`。上游GPL源码没有拷入我们的业务目录或Git。

## 对当前方案的具体建议

1. 保留Go网关、现有原任务/revision/设备回报/ACK；借鉴旧音频隔离、分段和回执原则，以自有实现接线。
2. 在[自动接续提案](../superpowers/specs/2026-10-23-automatic-task-continuation-design.md)的P0补充比较：候选TTS是否直接返回可用的词/句时间表；若能验证，可一段自然语音合成后再标出数字/句边界，减少“每个数字单独合成”带来的停顿和调用。没有可靠时间表时仍按已知短单位做首版，不启用估算。
3. P1仍须验证Opus/采样/DMA坐标。回执只能推进确实播完的连续单位；禁止采用跳过晚到内容的表演追赶策略。
4. 回声相似度只作辅助；把“用户复述原话”“用户声音较轻”“短中文插话”列为反例，不能为防误停牺牲正常插话。
5. 不因这份静态核对直接更换LiveKit或批准长内容TTS切换。上一份提案的音色、停顿、延迟和新增调用取舍仍需评审/实测；它的核心接续门槛没有被本项目替代。

## 许可证

仓库明确列出固件与服务为GPL-3.0、外部硬件设计为CERN-OHL-S-2.0；根README还列出部分第三方组件的单独许可。它不是MIT项目，不能把代码当MIT片段直接混用。此次只作源码参考并独立写分析；如以后实际引入源码，先单独核对具体文件和分发许可要求。[仓库许可说明](https://github.com/tudoom/OpenDeskBotV2/blob/1cf5050ee63b1fe4cae6b7815e565a0955f15bd6/LICENSE.md)。
