# 微雪 A/B/C 三链路对比记录

日期：2026-11-15。用户明确要求将A、B、C一起对比，按[最小接入计划](../superpowers/plans/2026-11-15-abc-waveshare-comparison.md)执行。

**当前：A四答完整播放；B分句修复后的一答协议和播放回执通过，但用户确认屏幕反复显示待机、没有按键，原因未定位；C回声漏拦尚未修复；三路对比未完成。** C-v3诊断窗口没有新设备输入，17:06已恢复；用户17:32的正常单答实际进入正式原生服务，不能算隔离C诊断通过。B-v4仅fresh prepare、尚未启服务或apply，等用户就位再开窗。旧C的“我叫小”已由用户明确确认为小满自身声音回录。

## C-v3空窗口与17:32正式入口后续

C-v3实际16:57:28–17:06:28开放，pilot1186855未收到任何设备连接或主回答，9分钟保护到时后停止并restore；正式进程/保护配置/Caddy file/admin均保持，账本仍protocol9/primary50。UART白名单采集16:55:50–17:10:50只覆盖一次starting→activating→idle，没有C对话状态，已自行结束。此前“入口已就绪”仅对该时段成立；不得将过期后的用户听感算入这个窗口。

用户后续“C单轮完成，正常”对应17:32:59–17:33:19正式会话`afa2d1220ddc2a425441ef15971e5bde`，build self-echo-20261114-fddfc11，仍为原生Omni直接语音，但记忆/设备规划/表情等条件与隔离ABC不同。输入“介绍一下自己。”，3.023秒首包、sent==completed==198720（8.28秒音频），首包到I2S played共8.459秒，41份回执最大间隔0.230秒。中途一次0.76秒/3字的疑似开头回声被既有`suspected_opening_echo`拒绝，没有打断或额外主回答；这是一条旧防护生效样本，不解释此前漏拦，也不是本次新修复。播完4.166秒后另有未终结输入与1006/transport_lost，闭合的设备原因未知；用户正常听感不等于已证明连接一直稳定。

B的补充反馈为“屏幕显示待机，期间我没有按过什么按键”，不能归因于用户按钮。已有B同期证据仍不足以区分设备状态机、网络、供电或显示；不扩大播放窗口或猜测修复。下一步B-v4只测一条自我介绍，同一reviewed b3c37c6b二进制，新的operator仅v3→v4路径编号替换，54项保护及4项人工收尾/marker检查通过；baseline与正式一致，尚未start/apply。先等用户物理就位再启UART和入口，并告知实际有效时间，避免再次错配路由。私有汇总`field-C-v3-expired-and-formal-followup.json`，正式样本API与辅助费用单列，不伪造ABC intent、不外推月费排名。

## v3补测：B协议已过一答，卡顿与待机仍待定位

B-v3使用已推送main `7380d9291b446319df732be1d0d64d7958066865`对应冻结新pilot b3c37c6b。16:52:25–16:53:04仅一条设备会话、一条“介绍一下自己。”；同一含`～`的原稿全部TTS成功（1 ASR+1 LLM+6 TTS），生成324480样本，Opus补齐后sent==completed==325440、I2S played，TTS只读全文确认。原`～`元数据匹配错误本轮未再出现，不能据此宣称B四轮或稳定性通过。

本轮话尾到首包7.123秒；13.56秒下发音频从首包到完成用了19.587秒。73份回执中有5段超过1秒的到达空档，最大2.150秒；之前A四答最大0.377秒，旧C各答最大0.370秒。TTS源PCM本身也有约0.78–1.08秒的句间低能量段，但不能解释全部额外播放耗时。来源、传输、设备调度和显示状态尚未区分，不据此修改信用窗口、超时或固件状态机。

用户反馈“B仍有异常：一会儿说话，一会儿待机，重复了好几次”，后续明确屏幕显示待机、没有按键。服务端本轮没有误打断或generation_failed；完整播完8.894秒后1006/transport_lost，早于主代理收到反馈后手动关闭测试窗口，故这次断开不能归因于控制器四轮收尾。按无按键的异常待机记录，具体设备机制仍待同期状态日志核对。B-v3已手动stop并guarded restore，正式两进程/配置/admin核对保持、pilot PID0。

私有补测摘要`field-B-v3-before-diagnostic.json`；B补测后账本protocol9/primary50，失败和未知仍保留。该单答按公开价已知主链路约0.0094976元，OutputASR按13.56秒样本估约0.0044748元，公共监听未知，不是完整账单或月费排名。没有新增运行时代码，沿用947普通/947竞态检查通过的冻结来源。

已暂停四轮计分，改为C-v3一次自我介绍，使用同一冻结binary、原生Omni/Tina，设备UART只读白名单状态元数据与服务端短预览诊断共同核对；入口apply/health已验证、开启时active0，pilot1186855。串口接入后另观察到starting→activating→idle，这一较晚记录不能冒充B期间的重启证据。C回声防护没有修改，结果与收尾待核对，不能提前算作修复。

## 首轮设备结果与修复

私有汇总为云端`field-before-repair-v2.json`、`cost-before-repair-v2.json`；原始事件、API usage和会话记录保留在私有根的`v1/A|B|C/records/`。以下均为旧build v1、binary29f22417的实际结果，不能作为修复后结果。

| 路线 | 现场结果 | 首个音频包等待 | 限制 |
| --- | --- | --- | --- |
| A | 用户“四轮完成，整体正常”；4答均I2S played，sent==completed；4份TTS只读对齐均全文确认 | 5.963 / 4.929 / 7.170 / 7.758秒 | 偏好问题被分段，实际最终输入仅“一个周末活动。”；不能声称完整偏好理解通过 |
| B | 3次自我介绍均ASR/LLM成功，但TTS失败、没有下发声音 | 无 | 第3个TTS任务请求含`～`后的后半句，首个sentence-begin只返回`～`前半句，严格匹配拒绝；不能据此声称提供方最终会截断 |
| C | 自我介绍被自身声音误打断并产生额外回答，随后另2个真人问题播完 | 0.927 / 0.874 / 0.948 / 1.111秒 | 4次模型调用不等于4个人工问题；原因追问和故事未测，初始回答drained |

C首轮生成“我叫小满，是家庭陪伴助手……”，input2最终识别“我叫小。”、音频0.8秒；输入开始前8毫秒仍有播放回执，短预览触发中断。用户两次确认未插话，并再次指出这句来自机器人自身。旧记录缺少该瞬间生成参考/匹配条件快照，漏拦的具体条件仍待新诊断确认。不要修改text+stash拼接、扩大阈值或关闭真人打断来猜测修复。

C最后自动待机由测试控制器累计4次模型调用后主动停止、记录`server_shutdown`导致；不是用户按键，也不能列成原因不明的设备自发休眠。新控制器不再按模型次数结束，目标仍为各4个人工问题，用户文字确认完成后主代理手动收尾；另保留9分钟窗口、10分钟实例保护，单会话改8分钟。

最小代码修复仅把`～/~`加入既有TTS分句符，原稿/符号拼回完全相同，任务、原文及最终时间戳验证保持严格。C仅在即将接纳的短预览里增加私有`opening_echo_check`，冻结输入/生成开头各最多9字符及身份、输出、播放条件，不改变准入或历史。历史primary49（原38+A4+B3+C4）、protocol9全部保留；安全上限改每路线12、阶段36、累计74，避免错误回复占满四个人工问题的额度，旧48默认工具不变，不自动重试或跑满额度。

修复后云端Go1.27.3五包普通/race各947 PASS、5 SKIP、0 FAIL；vet（含CLI）、test-c与正式CLI构建均exit0，规格/质量复核PASS。164份编译Go输入中161份仓库/资产逐项相符，另3份既有云端私有资产单列、无不匹配。新pilot SHA`b3c37c6b12d9b80cf4ed75b335a0a9604678603df7ef1799760c652923321169`；正式CLI候选SHA`80afd3569d6906c80641b759b63ddf3b4e9c1930537695b4a800036d705c6415`，未部署。完整检查留在`repair-v3/`，与旧结果分开；v3人工收尾/超时及stop marker隔离各2项通过。B实际提供方及设备复测、C漏拦诊断仍待进行。

按公开原价的已知主链路小计：A约0.0418995元（4 Omni+23 TTS）；B三次失败约0.0063284元另有3次TTS用量未知（3 ASR+3 LLM+9 TTS）；C约0.0090705元另有1次取消用量未知（4原生调用）。OutputASR按样本时长估算A约0.0184734元、C约0.0104148元；公共输入监听费用仍未知，B的主ASR须额外计入。以上不是账单、不是全部产品费用；回复长度、有效样本与失败率不同，暂不排名或外推每天30分钟月费。

## 实际路线与共同条件

| 路线 | 语音理解与主回答 | 输出声音 | 当前实现 |
| --- | --- | --- | --- |
| A | qwen3.8-omni-flash-realtime，输入原音、只输出文字 | cosyvoice-v3-flash / longanyang | 回答和TTS最终元数据收齐后播放 |
| B | fun-asr-realtime→qwen-plus非思考 | 同A的TTS/音色 | 与A同策略 |
| C | qwen3.8-omni-flash-realtime直接语音 | 受保护原配置音色 | 公共逐轮原生语音流式 |

每次会话固定一路，开测前向用户说明标签，记录实际主模型、ASR、TTS、音色和模式；错误不会回退到其他路线。三边共用支持中的微雪、鉴权、Opus、输入监听、OutputASR、播放信用/I2S回执和中断排空，首轮统一关闭点歌/设备工具/表情/搜索/声纹及跨会话记忆。无新生产多后端框架或固件改动。

同一简洁短答约束、各会话独立的有界已播历史，未播生成全文不进入上下文。A/B先复用已验证批处理客户端，C为流式；首包数据反映本次具体实现，不能据此下最终流式架构排名。C音色与A/B不同，声音单列，三条各4答属于初筛，不能证明长期稳定率或P95。

## API费用口径

记录主模型分模态token、累计历史、ASR时长、TTS计费字符及失败/取消/未知；公共输入监听、OutputASR不可漏算。TTS分句可能对应多次请求，12个主回答不是12次API请求。表情/点歌/设备控制本轮关闭，其开启后的产品费用需追加，不能将本轮主链路小计当整机全部费用。

B在本次最小接入中保留公共输入监听，同时另调用Fun-ASR-Realtime取得主回答转写；两项必须分别记录，不能把重复识别费用隐去。后续若复用一份ASR，需要验证对应新实现，不能直接把节省假设计入本次结果。输入监听若没有返回独立usage则保持未知，OutputASR音频样本量也不冒充已核对的供应商账单。

2026-11-15复核北京区公开原价：Omni文本输入1.5、音频输入6、文本输出4.5、音频输出12元/百万Token；CosyVoice v3 Flash为1元/万计费字符；Plus非思考输入≤128K为0.8/2元每百万输入/输出Token；fun-asr-realtime及qwen3-asr-flash-realtime为0.00033元/秒。免费额度、优惠和实际账单另核对。成品按每天30分钟、约900分钟/月比较，短会话结果不直接外推月费排名。

来源：[Omni3.8](https://help.aliyun.com/zh/model-studio/qwen3-8-omni-flash-realtime)、[CosyVoice v3 Flash](https://help.aliyun.com/zh/model-studio/cosyvoice-v3-flash)、[百炼价格表](https://help.aliyun.com/zh/model-studio/model-pricing)。

## 开窗前保护准备

私有证据根`/home/ubuntu/abc-voice-compare-20261115/`。开窗前正式2进程、7份受保护配置与健康已冻结，A路由当时仅prepare、没有apply。复用固定SHA的原路由操作器，更新三路线health契约后每路18项合成保护检查通过；模型标签已在冻结来源/二进制时再次核对。准备阶段没有真实模型或设备调用。

历史账本协议9/主回答38保持；本次最多新增12主回答，每路线4，累计至50。旧工具默认48未改，历史失败/未知全部保留，新阶段上限不代表人民币扣费上限。接入、云端检查、实际A、B、C现场结果及结束恢复分别以新证据记录，未测不算通过。

## 接入实现与最终云端验证

新增一个默认不构建的现场资产，A/B复用原Client，C复用generateNative；公共Session/设备播放代码未改。唯一产品代码增量为Native完成记录补充数值白名单内的文本/音频/缓存token细项，原顶层计数保留。三个冻结来源：

| 来源 | SHA-256 |
| --- | --- |
| abc_comparison_test.go.txt | 10e7deb06a2f518715184256c77143e0ca675700d2cf6f19e1b39ffaacf23463 |
| internal/realtimelive/native.go | 494dcfdfd725c3130ab579fbbf3b84c803acde82818f5d11128a4d2aa34f0de7 |
| internal/realtimelive/native_usage_test.go | f35130abaa1815c1732d423d4b4a7abaaf726149119072085a8999c4f2cee99e |

云端Go1.27.3，五包voicecompare/realtimelive/realtimestate/realtimeaudio/devicetools普通与race各934 PASS、5 SKIP、0 FAIL，vet（含正式CLI）及test-c/正式CLI build均exit0。编译相关164份Go文件中161份仓库/ABC资产与Mac逐项匹配，另3份为既有固定TTS、规划并行和私有重放，go.mod/go.sum一致。检查期间冻结源码未变，无真实提供方、设备、服务或原账本调用。

覆盖精确gate/route/target/Opus、持久4/12/50、重复/破损/未知/取消、实际Client/Native对合成HTTP/WS服务的路由、API intent先于网络、A/B同TTS、失败不播放不降级、C输出超限、私有事件8MiB及fsync、cut历史不升级Generated、只读字词审计及计费字段。独立规格复核最初发现失败原因丢失，先以B HTTP503及结果验证错误复现RED，再补私有error{reason,message,truncated}（最多2048字符）；复核及质量最终PASS。

最终现场binary `abc-pilot.test` SHA29f224176fdd02cb88c8df7410a993cc508a699feff31935fbeb195ae12e39c0；未部署的正式CLI candidate SHA1ec99f35ed2fec8cbc0c34f42c3bc7c4ffc8f408c71f24fce5ed70eef008c493。私有根下final-tests.jsonl、final-race.jsonl、final-verification-summary.json、final-compilation-input-check.json及dev/保留完整证据，二进制和原始数据不入Git。

现场顺序A→B→C，每条重新核对source/binary/PID/health proof，单次apply并在第四答硬件结算后停止/restore。第一A窗口已绑定binary29f22417、来源/控制器SHA、PID1146868及exact health；启动/首次核对active_sessions=0，用户已收到四轮说明。未取得样本不能报告A通过；B/C未开始，不报告三链路已测完或费用排名。窗口过期后续测须先核对restore，再fresh prepare/proof/intent，不复用已消费A-v1。
