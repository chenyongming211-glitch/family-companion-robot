# 原生会话已播对齐与中断状态优化记录

状态：用户已批准实施，分段证据、原生中断修复及旁路审计已实现并完成云端检查/审查；真实协议门控通过，补充缺项读取及提供方内部抹除仍unknown。v4无新输入后已恢复；v5完成一段实机，7个provider completed回答保持同一原生连接、2次同连接中断修正，用户反馈改善，但仍有播放断续和回答中途转聆听，整体未验收。现累计5/8协议、26/48主回答intent；v5也已恢复原入口、停止实例/采集。正式Voice/管理与固件保持；用户另一次正式旧服务会话自然更新了原记忆，隔离v5仍memory=false，不把它写成原记忆全程不变。

依据：[设计](../superpowers/specs/2026-11-14-native-playback-context-repair-design.md)、[执行计划](../superpowers/plans/2026-11-14-native-playback-context-repair.md)。只落实已播分段证据与中断恢复，不恢复耳机直连/枚举矩阵，不改固件或放宽600ms信用/播放超时。

## 起点与派生环境

- 起点main214810b；正式VoicePID732503、管理PID635398，原Caddy SHA`adb6222394b10c444c5d563b241afc87be39aa17fb00d5bce2c4f2d22653f115`，ready/memory_ready、active0/2、诊断false，隔离实例inactive。
- 私有`/home/ubuntu/waveshare-native-integration-20261114/source-repair-v1`从v8原209项来源/支持输入派生，初始化清单SHA`30caeed927a716955d949b1f81423db96c849341028e9dde96ba6bf5a396f61e`。旧source、v8、失败和共享账本不覆盖。
- 初始账本3/8协议intent、13/48主回答intent。真实检查继续同一flock/fsync账本，未知不重试或退回额度。
- 状态包完整差分检查首次因缺测试oracle.py失败，保留日志；从实际runtime-candidate补同GitHEAD字节的合成oracle，SHA`cffb0a2ddb591b0cb9430c7159789dfb85f22f305b392073ad858ece60cf8356`，6个Python依赖和config已在原支持清单；只加派生测试支持，不更改旧契约。

## 分段证据

新增纯函数`realtimestate.ConfirmedPlayback`，旧history/alignment/state语义保持。部分播放按已下发解码音频时间、120ms工程余量、完整短句及有序唯一锚点确认；局部不符、重复/乱序、零播放/零时长、数字后缀或缺识别均不猜。ASR真实独立punctuation字段只产生边界，不填造数字内容；中文句号始终分句。返回硬件确认样本、可信片段及未知标志。

云端RED→GREEN保留，包括两轮规格审查发现的零播放/零时长、全角数字后缀、中文句界回归和标点元数据构造数字问题。当前完整状态包普通/race各34顶层、含子测试79项，零失败/跳过；规格复审及独立质量审查PASS。证据`ops/repair-v1/segments-*.log`。

用前次两段实际会话的已有事件/输出ASR作离线回放，没有新推理/真人采集：原3处unmatched分别保留7、10、4个可独立确认短句，其他尾部仍unknown。这是旧记录回放，不能当新实机成绩。回放v1因分析脚本未处理无id事件失败保留；v2修分析脚本，v3用最终对齐源码复核同结果。私有`field-segment-replay-v*.json/log`不入Git。

## 原生连接修正

隔离runner v9实现串行写、单读者、至多一次取消及有界终态检查；已完成但仍在设备播放的回答，也可在Cut后按真实response/item身份处理。新问题只在旧生产者确实取消时有界等待，真实并发仍拒绝。逐项删除当前回答所有assistant item，核对ACK；在原user位置补可信已播片段与独立system状态，包含回答编号、硬件已播样本/毫秒、未知标志和新请求优先级。普通轮次和其他原生历史保持。

输出ASR在原1200ms等待内未结束或失败时，冻结当时已验证片段并明确unknown/reason；同连接修正与重建使用同一不可变快照，迟到识别只作诊断。取消/身份/ACK/顺序不确定则关闭旧连接，带同样状态可信重建，不能仅留下旧user请求或重试原输入。

fake保护检查已观察RED→GREEN，覆盖完成后Cut、多item、生成中取消、取消迟到的新问题、错误ACK、重建、初始关闭、已知迟到输入ASR及迟到输出ASR快照。规格及独立质量审查PASS，主任务冻结214项来源/支持输入的普通与race相关包、vet及构建通过；真实替换协议门控结果见下文，硬件已播对齐及听感仍待验证。补丁/新增test-only源码与哈希保存在`tools/native-session-pilot/`，原私有v8输入不公开、不改写。

## 真实接口检查准备

独立隐藏Go包默认禁用，必须显式allow和自身源码SHA才读云端受保护配置。只用合成输入，音频计数/哈希后丢弃，无设备或真人原音。初次检查及补检两版合计最多2协议/4主回答intent；检查准确删除、缺项读取、原位置补回与后续行为，后续回答符合不单独证明提供方内部历史完全裁剪。

fake与审查已修正服务端事件ID审计、官方type-only取消、终态精确item集；多content-part转写覆盖已通过明确拒绝不支持格式修复并经独立质量复审PASS。首次父任务入口因把公共端点写为白名单而在任何连接/intent之前拒绝，账本SHA保持；精确专属端点门控修正并经规格/质量复核后，真实第一答完成90,240样本并获准确生成项deleted ACK，随后retrieve返回invalid_value，未满足额外absence检查而停止，保留unknown结果。账本累计4/8协议、14/48主回答。没有把读取未知或一次deleted ACK写成整个方案通过。所有失败/审查前候选哈希和账本不变证明保留。原v1已消费、不重跑、不重置其目录/intent。补检v2先验证生成中取消/替换，再将其已完成followup直接用于完成后替换，只需3条新回答，仍使本阶段主要回答总计不超过4。精确deleted ACK必须通过；已删除项的额外读取若仅得到可关联的客户端缺项类错误，单列lookup未知，不假称absence证明；其他错误停止。补检源码仍须重新通过规格/质量门控。

## 真实接口补检结果

父任务仅执行一次v2，退出0、`protocol_acknowledged`，私有结果为`ops/repair-v1/protocol-v2/result.json`。与v1合计新增2个协议、4个主回答intent；累计5/8协议、17/48主回答，账本SHA`b6ea198cd5517ecc9244e7bcc4f4f2b21742c3c2af3cc4d8e3dca73fed7437ad`，没有退款、重试或重设未知记录。

- 生成中已收到19,200个24k样本后取消，同response终态为cancelled；准确删除生成assistant item，在原user之后补合成可信短句及独立system状态，created位置ACK和retrieve角色/文字全部准确。
- 上述后续回答正常完成，交付96,000样本；将这条完成回答作为第二场景，准确删除其item并重复原位置补回核对。最终追问仅输出保留短句，没有输出已删合成尾句。
- 两场景额外读取已删item均返回`invalid_request_error/invalid_value`，单独记`absent_lookup_UNKNOWN`；未将通用错误当作缺项或内部抹除证明。最后的短句行为也只是一条行为观察。
- 输入和保留短句均为合成文字，音频只计数/哈希后丢弃；交付样本不代表硬件播放，未验证微雪短句对齐或实际听感。准确源码和fake保护检查保存在`tools/native-session-pilot/probe/`，已消费v1证据保持。

## 冻结上下文旁路审计

既有结果审计新增有界`playback_sync`，复制原native SID、response/user/全部assistant item身份，canonical已播样本、真正用于修正/重建的冻结片段与时间，以及unknown/complete/ASR reason；该快照不是提供方送达ACK。仍使用原deferred append，无新增关键路径I/O。新增RED→GREEN/race检查覆盖同连接、mutation失败重建、迟到ASR与后续重建，核对审计文字和实际插入文字一致；已修正或旧连接Cut不重复列入当前审计。云端证据`driver-red-08-audit.log`、`driver-green-09-audit.log`、`driver-race-04-audit.log`保留。

## v4收尾、待验与体积（后续v5见下文）

- 分段、runner和新增审计均完成规格/质量审查；审计增量后重新冻结214项来源/支持输入，检查前后不变。普通/race各有736项测试通过（306顶层），3项原有按需测试跳过；两个相关包、vet/build全部通过。最新二进制SHA`f60b04dbd24d0bdd32d732f43a9cccdb3470c50e6d6370d75667907d6670133d`，旧二进制及测试记录保留。
- 最新runner/helper/guard摘要绑定`tools/native-session-pilot/runner-v9.sha256`。私有`pilot-compiled-inventory-v9-audit.json`的214项是来源/支持输入数，含测试、配置与支持，不能称实际编译文件数量；隐藏probe独立记录。
- 独立v4 prepared manifest SHA`cb19a0b12975a1a213cc9a45f0f2438de8bd84cf6797a7b8ab11499a6f4c43cf`；新PID856040、root proof SHA`29eed9349b61cfc2444e33705bc6239fc633db4ac87329ba395fb46ebd90b0bb`绑定新binary/来源/实际配置及协议结果。apply已核对，仅精确GET/path/Device-Id转18818，原fallback18041保持。
- 被动USB日志无串口写入或设置更改，云端标量观察同开同关。约5分钟窗口未收到新会话，账本保持5/17，记`not_covered_no_new_device_input`；不将待机心跳或无错误当作对话通过。私有`field-no-input-v1.json`及`field-observation-v1/`保留。Mac私有串口日志292,854B/4,449行，SHA`68c82f0038c3ea9396e74c7f4c672421ddd6302d4f6b5cb07599db7a29e6c82f`，没有新对话播放起点；未取得本轮boot字段，不据此断言启动情况。
- v4 `restore verified`，原Caddy文件SHA`adb6222394b10c444c5d563b241afc87be39aa17fb00d5bce2c4f2d22653f115`及admin恢复；仅停止本轮PID856040，MainPID0，串口及云端观察均已退出。正式VoicePID732503/SHA dcd233c8、管理PID635398/SHA6afbdc98及保护配置保持，Voice ready/memory_ready、active0/2、诊断false；见`cleanup-v1.json`、`route-restore-v4.log`及stop记录。
- 用户方便时仍需验证换话题、暂停/继续，核对真实`playback_sync`与硬件回执。下次须在当前基线上创建独立新分流意图，不复用已消费v4或重试原输入；仍用全局5/17起的原账本。本轮未修改输入回声/AEC规则，不能将此项优化当作所有不顺畅问题已解决。
- 固件仍2.5.4，app3,534,368B/每槽余70,112B，本轮固件新增0B；克隆样本仍未收到，本轮不创建音色。

## 用户后续反馈：21:41旧服务播放停滞

用户随后回复“说话断断续续，然后就停了”。核对正式服务和Caddy时间：v4已在此之前恢复；21:41:07–21:41:38这条新连接实际进入正式`self-echo-20261114-fddfc11`，不是优化版v9。原v4窗口仍为零新会话，不能把此次反馈记为优化版的实机成败。

该轮识别“介绍一下自己”，没有新插话事件。模型生成正常completed：3,251ms内交付370,560个24k样本（15.44秒），首PCM1,054ms。设备回执原始最大111,360样本（4.64秒），核心向下量化至110,880（4.62秒）；已发送125,280（5.22秒），恰好耗尽600ms信用。最后递增回执后5.001秒触发`generation_failed`/interrupt，再等待3.001秒以`drain_timeout`关闭。

这里`generation_failed`是发送循环的通用错误标签，不能解释成模型生成失败。直接触发链为已播回执停滞→发送信用耗尽→既有超时关闭。回执多次延迟成批到达；仅凭这些记录还无法分开设备I2S停顿、无线/TCP延迟或收发阻塞。记忆写入为异步合并任务，不能只凭同时出现的`memory_write`就归因其阻塞。现场串口未覆盖此21:41时段，不补造硬件证据。

私有`ops/repair-v1/formal-followup-v1.json`绑定这条正式会话的事件/元数据摘要和关键时间线；这是用户主动进入正式旧服务的一轮，单列，不伪造隔离runner intent。共享隔离账本仍5/17。为下一次核对，另建独立v5分流准备/新PID proof，复用同一已验证binary并同时采集被动串口、隔离音频标量与无payload的TCP统计；没有重跑协议检查、修改固件或放宽窗口/超时。v5收尾结果另记，不复用已消费v4。

## v5实机：同连接修正有正例，播放与误输入仍待定位

独立prepared v5使用同一已审operator，manifest SHA`c14447e48d56afa899a17daff04d8a0f689b1fbe3c52400ecb279acf6f763a54`；新PID860725、proof SHA`2ded2a12bdc46bc222f71fa3aa5a089ab4e722ebde3f5da55015a6ccc7f2591d`，binary及214项来源未改。实机21:51:31–21:53:41产生9个primary intent，累计5/26，账本SHA`9ce0ad5bb1f262509fc790d739ced1684534975b417e6494c6ef6c460215c70e`。首轮“嗯”刚提交即被下一句打断，尚无response身份/音频，保守关闭旧连接后重建；这条`read_unknown`不退款，也不单独归为提供方故障。末轮在零下发时transport_lost取消，闭合原因未取得用户确认，不冒充完整回答。其余7条provider completed保持同一native SID；7次硬件终态含5次完整played、2次被打断drained，无turn_error或drain_timeout。完整played不等于全程流畅。

本轮两次`same_session_context_repaired=true`。故事生成24.72秒，最终硬件已播13.02秒，冻结5个能独立确认的短句并明确unknown；下一问自我介绍直接回答新问题，沿用同一native SID。地理回答中断后，也在同连接修正后续聊。`playback_sync`绑定旧SID/response/user/item与冻结文字/时间，不把所有13.02秒都说成已逐字对齐。用户反馈“比之前好多了”，但明确指出开头断续及尚未说完就进入聆听；暂停后恢复原故事没有单独覆盖，不能据此完成整体验收。

播放断续取得同期证据：设备公网TCP连接累计重传21,587B（发送258,191B），多个约0.5–1.5秒回执间隔与重传邻近；另有4.089秒回执间隔。最长间隔内，音频接收帧停在1389，3.896秒后突增至1461；当时输入队列无积压、append无错误，Caddy公网TCP与本地转发的bytes_received也同时不变，随后一起增长。支持停顿在微雪收发/外部网络入口一侧，尚不能分开设备WebSocket任务、Wi-Fi和ISP，不能仅凭下行重传就指定路由器为唯一根因。Go未在此窗口积压输入；也没有证据将其归为模型生成慢。私有`field-playback-network-v1.json`、`field-long-gap-network-v1.json`及`audio-diagnostics.jsonl`分别保留。

输入侧有一次空转写已被拒绝，未打断故事；地理回答尚未播完时，预览/最终识别“没有啊”触发打断。用户确认其感受是回答中途转聆听，但对是否说过“没有啊”回复“记不清了”，因此该次打断的真人/误输入来源仍unknown；环境声、人声误识别或设备自收音不能只靠转写区分。本轮未调整VAD/回声/AEC规则，不能宣布自动聆听问题已解决。输入轮次仍是原Omni监听与server_vad，并非本轮验证了smart_turn_v2。

同期被动串口643,191B、9,767行，SHA`2115aa393b064140a30270d7f4a777ef3ec6b6b8dedcf22ebc2ccebef46ee65f`；未写设备或修改串口设置。AFE empty/fetch告警在早前待机及正常工作采集也大量存在，不能单独当本次卡顿根因。原音未持久保存。

v5 `restore verified`，Caddy文件/admin回原值；只停止本轮PID860725，MainPID0，两个采集均已退出。正式Voice732503、管理635398/保护配置保持，ready/memory_ready、active0/2、诊断false；固件2.5.4/app3,534,368B不变、新增0B。私有`field-repair-summary-v1.json` SHA`640fb57ef3c25f5224bc493628b881c22d863a127760c4850dd5c6107876a5c9`、`cleanup-v2.json` SHA`90f9b8258ca1a6f69dfea1d508ea9067c687f5acec2bbe40ff5c321f04fafc1b`绑定本轮。后续继续同一5/26账本，不重跑旧协议或复用已消费分流意图。
