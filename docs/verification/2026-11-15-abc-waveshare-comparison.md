# 微雪 A/B/C 三链路对比记录

日期：2026-11-15。用户明确要求将A、B、C一起对比，按[最小接入计划](../superpowers/plans/2026-11-15-abc-waveshare-comparison.md)执行。

**当前：修复后的B故事/追问、C自我介绍均有完整现场播放正例；所有窗口已恢复，三路统一对比尚未完成。** B引号归一化实际走通整任务边界，故事起播仍约7秒；C自我介绍0.954秒起播，本轮未复现回声漏拦。C追加的故事因124字触发临时120字测试上限，单列失败，不把自我介绍正常写成C所有用例通过。旧无按键待机及C漏拦原因仍未定位。

## C-v6自我介绍通过，追加故事触发测试字数保护

C-v6实际23:03:53进入隔离会话`4e05135b2dfe7227a321450ca3882cd2`，build v3、Omni3.8直接语音/Tina，入口/模型与proof相符。自我介绍0.954秒首包，57字生成正常completed，sent==completed==283680样本（11.82秒）、I2S played，首包到完成11.923秒、58份回执最大间隔0.263秒。用户“C自我介绍完整，正常”。期间没有新的speech_started/preview、误打断或额外回答；也没有需要拦截的有效回声输入，因此只记本轮未复现，不称防护修复或拦截成功。

随后用户追加“讲个小故事。”，0.968秒起播；流式文字达到124字触发测试资产的`abc_native_text_limit`（本轮保护120字），主响应取消、用量未知。已下发53280样本（2.22秒）按原路径406ms drained，没有排空超时；不能说设备突然停播或把generation_failed标签解释为模型/网络故障。此句没有按统一问句“不超过80字”测试，亦未达提供方完成终态；本次不得计为C故事通过。保持已声明的测试上限，不为这条加测临时放宽限额或修改正式产品。

用户完成反馈后主代理手动收尾，C-v6 restore核对正式进程/保护配置/Caddy file/admin保持、pilot PID0、正式active0，未改Go源码/固件，也没有重开UART。私有`field-C-v6-intro-success-story-limit.json`保留。

累计protocol9/primary58（本窗口+2），包括取消未知，全部保留。自我介绍已知主链路公开价约0.003192元，另1次取消主调用用量未知；两次OutputASR按14.04秒样本估约0.0046332元，公共监听仍未知，不是完整账单或月费排名。后续继续按同一固定问句对比质量、等待、播放、中断与每成功问题的总API费用；不能把A被分段的偏好问题、B/C各阶段正常单答拼成一次已完成的统一四轮测试。

## B-v6故事现场通过，继续C-v6诊断

B-v6窗口22:55:17 fresh apply，pilot1278058、build v3/候选0bc258e1与冻结来源一致。22:55:33进入会话`c5f9f342d727e7db5093e891b29130a0`，两条真实输入为短故事和追加“讲完了吗？”。

| 回答 | 话尾到首包 | I2S sent==completed | 首包到完成 | 最大回执间隔 |
| --- | --- | --- | --- | --- |
| 短故事 | 7.024秒 | 288000样本/12秒 | 12.123秒 | 0.374秒 |
| 追问 | 3.331秒 | 139680样本/5.82秒 | 5.920秒 | 0.396秒 |

两答provider/client completed、I2S played、无turn_error/打断。故事包含引号，真实第4个TTS任务使用`task_boundary`，精确Words只保留首缺口前缀，最终审计以任务边界确认全文；没有把粗段标为word。数字的归一化目前仍是三份真实元数据离线重放证据，本轮故事没有数字，不能写成数字实机用例已覆盖。播放时一次空转写被拒绝，未打断故事。用户明确整体正常；这是两条回答，不是B四个统一问题已完成。

追问播完3.506秒后1006/transport_lost，发生在用户反馈后主代理手动stop之前；这次没有同期UART，设备闭合原因未知，不用正常听感证明播后连接稳定。B-v6已stop/restore并核对正式进程/保护配置/admin保持、pilot PID0。私有`field-B-v6-story-success.json`保留。

累计protocol9/primary56（本窗口+2）全部保留；本轮2 ASR+2 LLM+8 TTS已知公开价约0.0122804元，OutputASR样本估0.0058806元，公共监听仍未知，不是账单或月费排名。修复没有引入辅助API或重试。

用户仍在现场，B恢复且active0后已开启fresh C-v6，pilot1279321，Omni3.8直接语音/Tina，同一build v3冻结来源，22:58:49 apply/health验证、保护至23:07:49，只请一次自我介绍且不插话不按键。C诊断结果待核对，未修改回声防护、未重开串口或复位设备。

## B-v5结果与任务边界适配修复

22:28:11设备进入正确B-v5会话`528e7ca59bb60b91fd463b1a5f790afc`（connection c1-af28d78f14435da5-00000001）。自我介绍实际转写“小马，介绍一下自己。”，与预定单句并不完全相同；6.860秒首包，335520个下发/已播样本（13.98秒），从首包到I2S played 14.195秒，回执最大间隔0.230秒。UART按同一连接ID核对只有listening→speaking→listening，没有在本次播放中反复进入idle。准备阶段串口reset15不并入播放故障；主机时钟存在偏差，不能把UART主机时间与云端毫秒强行精确相减。这个单次未复现不否定先前无按键待机反馈。

随后收到三次讲故事输入，均ASR/LLM成功、TTS已返回PCM/最终元数据，但服务端严格字词映射失败，整轮未下发。两例`100`按“一百”返回，索引已按口语文本推进，且`——`对应两个`、`；另一例`……`文字有2个rune、索引跨度却仅1，后续文字索引也整体偏移。`normalized_text`仍与原稿相同，不能据此把word索引当原稿坐标。私有`field-B-v5-protocol-normalization-failure.json`及三份实际末事件保留。用户“B出现其他异常”对应本轮，不能写成B通过。用户反馈后手动收尾，22:29:44 socket_closed/listening→idle与本轮server_shutdown相符，正式配置/admin/进程保持；不解释此前B-v3在收尾前的1006。

最小修复保留原严格`validateWords`及任务、原稿、句身份、PCM、终态、usage检查。仅当全部字段/索引范围/时间有效、非数字非标点词面逐字同序时，坐标不匹配可降为显式`TaskBoundary{Text,EndSamples,Precision=task_boundary}`；数字发音为不透明整体，不计算数值等价、不猜数字内部位置。没有伪造Words；精确Words在首个坐标缺口停止，后段不跨缺口追加。ABC审计按完整任务PCM边界、encoder lookahead及120ms确认连续前缀，并明确精度；它只证明对应任务PCM边界，不是逐字内容或数值核验。公共OutputASR/历史/中断保持，未增加辅助API调用或自动重试。A/B仍完整收齐后播，本次没有优化约7秒起播等待。

公共三例合成协议先RED后GREEN，7类坏字段/索引/词义/时间/原稿/终态仍拒绝；三份真实失败元数据静默重放通过。规格复核发现Full短粗末段可能误标word，已新增RED→GREEN并改为独立计算保守精度；规格和质量最终PASS。云端Go1.27.3六包普通/race各976 PASS、5 SKIP、0 FAIL，vet/三构建exit0；169份编译Go来源165仓库/资产一致、4私有重放/既有资产单列、无不匹配。证据`repair-task-boundary-v1/`，新pilot SHA`0bc258e133b23268f47bcfc9a179d937be509f2ecac6261923443b2121bcec95`、build abc-comparison-20261115-v3；正式CLI候选仍80afd356、未部署。B-v6只准备新来源/路由，不自动开窗发声，实际完整故事仍待设备验证。

B-v5账本新增4主回答后protocol9/primary54，失败和原预留全部保留。实际4 ASR+4 LLM+13 TTS的已知公开价小计约0.033634元，各调用数值usage均在；OutputASR按13.98秒样本估约0.0046134元，公共监听未知，不是账单或全部产品费用。旧失败返回的整体`usage_unknown`不能覆盖各API已知计数，统计继续逐调用核对；正常化映射降粗也不把费用记零。两段TCP观察只部分覆盖会话，不能借其推断完整播放期的网络原因。

## 22:26 B单轮诊断开窗

B-v4的全局编号替换误将health中的cosyvoice-v3-flash写成不存在的v4标签；启动核对拒绝，已精确停止本轮实例、没有proof/apply意图或模型调用。错误脚本/冻结清单及拒绝证据保留，不覆盖。B-v5只替换文件/unit/artifact编号，额外逐项断言三路health与已审v3完全一致；54项保护、4项人工收尾/marker检查通过，仍用同一b3c37c6b二进制、原947普通/947竞态的Go来源，没有产品代码变化。B-v5新baseline/proof/source匹配后22:26:42单次apply，pilot1266238，实际Fun-ASR-Realtime→Qwen Plus→cosyvoice-v3-flash/longanyang，开启active0；只请用户一条自我介绍、不插话不按键，截止时间已明确告知，结果待核对。

本机UART白名单采集`uart-v4-metadata.jsonl`于22:25:38开启，包含主机UTC、设备uptime、状态及连接ID。串口打开后约255毫秒记录reset_code_hex=15，随后starting→activating→idle（22:25:55）；已告知用户，这次准备阶段的复位单列，不能写成B播放期间的异常，也不再把串口打开概括为绝无设备副作用。另有75秒只读TCP元数据观察`ops/B-v5-tcp-metadata.jsonl`，公开443多连接时不得不经关联就归因设备。原C/B的失败、费用及未知全部保留；B-v5开窗本身不构成已通过或新增已结算样本。

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
