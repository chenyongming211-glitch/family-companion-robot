# 18040终端、会话与记录归属验证

日期：2026-10-25。实现提交`c9050e0`，云端18040运行该构建；本报告及交接属于后续文档提交。范围是Mac验证归属基础，没有接小智或刷机，没有新增判题、提醒、长期记忆和声纹。

## 结果

- 服务：`family-robot-realtime-output-go.service`，PID`1014445`，`build=c9050e0`，`device_identity=1`，最终ready=true、busy=false、NRestarts=0。
- 同一Mac上两个独立测试profile取得不同的服务器终端编号；profile A重连仍沿用原编号，新的会话编号与旧编号不同。它们是两个逻辑测试终端，不是两台物理设备。
- 新客户端用终端凭据换一次性票据，票据绑定服务端登记的终端。未知/错误凭据不能降级成未归属模式，客户端自填Device-Id不能改绑。
- 记录、生成/输出ASR调用intent均绑定同一终端和会话；事件/intent更新不能覆盖归属。CLI按B终端查询A会话返回0条，按A会话显式读取47条事件且归属全部一致。
- 旧模式继续可用，但标为`legacy_unattributed`、无device_id；原有历史不回填归属。所有实测会话第一轮recent_turns长度都是0，未自动接入跨会话记忆。

## 自动测试与审查

运行环境为云端`/opt/family-robot/toolchains/go1.27.3/bin/go`，验证快照在`/tmp/family-ownership-dev`。运行`tools/realtime-output-live/go-tests.command`，全仓race、vet、build通过；最终合计595项通过、0失败、2项原有opt-in长边界测试未运行。末次生命周期修改后，重新完成全仓检查，并以最后一次realtimelive JSON结果替换此前同包结果核对计数。

覆盖登记/认证持久化、文件权限、失败写入不破坏原身份、损坏登记拒绝启动、单次票据/过期/Origin/单会话准入、重连、终端隔离、真实WS上的错会话/缺会话播放回报拒绝、元数据与正文查询隔离、旧记录和旧协议。生命周期覆盖主动stop、正常WS关闭、服务关闭、上游失败与启动事件写入失败。

红灯证据：原实现`/devices`返回404，损坏登记仍允许pair；随后发现intent.update可覆写归属；受控上游断开被误记为正常transport close；启动events不可写时manifest缺结束信息。均先在云端复现，再修复并回归。

独立requesting-code-review审查发现Mac首次不存在文件实际错误码260、异步stop与进程退出竞争、内部失败原因丢失、空客户端版本可覆写及启动失败缺生命周期等问题。已分别修复；保持原客户端关闭方式，没有为发送stop延长停播。Mac运行`test-device-identity.command`，7项纯客户端文件检查通过；正常客户端构建签名通过。Mac没有安装或运行语音后端。

## 真实模型与Mac CoreAudio

使用既有合成输入WAV，按照20ms实时节奏上传，真实千问/输出ASR和真实CoreAudio播放；不启用麦克风，不替代真人收音、回声或听感验收。

| 测试 | 会话ID | 内容与结果 |
|---|---|---|
| A首次 | d93dcf9acb485f7625c167369609d243 | 4轮：小满身份、故事播放中插入数学、续讲；3次完整播放＋1次排空 |
| B首次 | 5141765c977d8adcc730c709ec36d210 | 数学完整播放，独立终端编号 |
| A重连 | 9492c149cee4fcc65125d13917021815 | 终端编号不变；天气完整播放，实际搜索count=1且有来源 |
| 旧协议兼容 | 25c0d98936936c9bacc4c84da152d1ad | 用客户端的无身份协议接Go18040，数学完整播放；不是调用Python后端 |

总计4会话/7轮：6次完整播放、1次排空；84条事件、25份intent归属全匹配。全部终态completed=accepted=sent，没有turn_error、session_error、input_failed或drain_timeout。插话封口时余300ms，实际CoreAudio排空289ms；该次不是所有终端的延迟承诺。普通回复服务器话尾至首包931—1302ms，天气4057ms；没有做新旧性能对照，不宣称普遍提速。

合成客户端在成功后直接退出进程，未等WS正常关闭握手，4次结束原因真实记录为`transport_lost`，不能称为正常client_stop，也不据此认定模型失败。正常关闭/stop/服务退出原因由受控WS测试另行验证。没有为美化记录修改原退出交互。

服务登记文件和会话JSON/JSONL为0600，会话目录0700。客户端安装凭据只留Mac Application Support，服务器仅存摘要；本轮未复制模型Key、保存用户原音频或提交凭据。私有详细日志在Mac`.superpowers/ownership-integration/`及云端会话目录，不进Git。

## 部署与回退

部署前核对18040空闲，云端源码工作树干净；Git bundle快进到实现提交后，在云端编译。旧二进制保留为`/opt/family-robot-livekit-runtime/bin/realtime-output-live.pre-ownership-b6f6ebd`，新CLI为同目录`realtime-records`。

只重启18040；原8000/18010/18020/18030/18032服务PID分别保持2261506/4188798/4188800/42018/874598。原测试窗口未关闭。Mac为本轮检查使用本地18044到云端18040的SSH转发；默认启动器仍是18040。当前Mac的默认control socket私有别名指向已存在的cloud-20261025连接，不创建新服务器凭据。

需要回退时先结束18040会话，再恢复上述备份二进制并重启同一systemd单元。新登记和会话记录保留；旧二进制会忽略新增归属数据。旧客户端/无身份模式仍可作为协议回退；新版客户端要求身份能力，不能将认证失败静默降级。

后续真人测试须从新版启动器打开客户端；已经运行的旧窗口不会自动升级，仍能使用但记录为未归属。精确故事停点、所有自然语言问法和ESP32适配未由本轮验证。
