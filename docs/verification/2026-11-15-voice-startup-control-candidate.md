# 起播规划并行候选与音量回执诊断

日期：2026-11-15。接续[正式会话核对](2026-11-15-formal-conversation-latency.md)及[实施计划](../superpowers/plans/2026-11-15-voice-startup-control-repair.md)。代码和云端合成检查后，14:02–14:03完成微雪四轮实测，用户反馈“四轮完成，整体正常”。四轮均完整played，测试入口已恢复，正式服务和固件未变。下方准备状态保留为此前阶段，最新结果见末节。

## 当前判断

- 用户已确认故事结尾听完才进入聆听，12:18:06附近按过按钮或让设备休眠。这次回聆听及1006按正常完成、主动关闭处理，不修改正常交互。
- 起播前点歌和控制规划串行是已确认的额外等待。主模型first PCM约1.1–1.3秒，不能把原3.373–8.803秒话尾至首包全部归给主模型。
- 音量设置在setter ACK接受之前5.024秒超时，没有进入readback。实际Go与已准备2.5.4固件的数值id、text true回复格式相符；仍不能确定设备是否已改音量。

## 已实现的最小候选

`--parallel-planning`默认关闭。只提前执行现有纯控制规划，与点歌规划重叠；音乐优先、记忆路由、硬件执行、回读和主回答保持原先次序。没有最终转写、音乐未启用或清记忆指令沿原路径。

冻结控制所用历史投影、memory floor和最近已核验控制；消费前及实际MCP写入处再次校验。依据改变至多重规划一次，沿用原3秒规划和18秒操作预算。若第二次失效而本轮仍有效，只丢弃控制计划、继续主回答；真正取消或退役仍退出。取消的前置任务会收拢，不提前执行硬件或生成主回答。

复用已有native_session记录，补模型、工具名、请求数、submitted_at、连接/规划耗时、终态和数值usage。发送前先持久化未知费用意图；取消、写失败、缺失/不完整usage保留unknown。表情对应记录用usage_source区别，汇总不得重复计费。记录不含握手凭据或原音。

Bridge新增受限RPC观察：收到id、当前等待id、initialize/tools_list/setter/status/readback阶段，以及matched/ignored_id/invalid_envelope。观察取既有Expire之后的状态，不修改超时、匹配、结果、取消或去重规则；matched只说明信封与id匹配。控制派发记录增加rpc_id，方便与回包关联。未收到确认时引导回答明确“尚未收到确认”，不推断未生效、不自动重发。

## 云端验证

运行位置：`/home/ubuntu/quiet-voice-backend-20261114/source`，Go 1.27.3。Mac只编辑、Git及被动串口读取。

```sh
go test -json -count=1 ./internal/realtimelive ./internal/devicetools ./internal/realtimestate ./internal/realtimeaudio ./cmd/realtime-output-live
go test -race -json -count=1 ./internal/realtimelive ./internal/devicetools ./internal/realtimestate ./internal/realtimeaudio ./cmd/realtime-output-live
go vet ./internal/realtimelive ./internal/devicetools ./internal/realtimestate ./internal/realtimeaudio ./cmd/realtime-output-live
go build -o /home/ubuntu/voice-latency-control-20261115/parallel-planning-candidate ./cmd/realtime-output-live
go test -c -o /home/ubuntu/voice-latency-control-20261115/parallel-planning-pilot.test ./internal/realtimelive
```

普通与race各844通过、4按需skip、0失败；五包通过，vet与两项构建exit 0。云端含独立固定TTS、并行规划测试资产及私有重放，数量不等同于纯仓库测试数。此次合成检查未连接真实提供方或设备。

编译检查涉及159份仓库Go文件，均与Mac一致；另3份为云端测试资产/私有重放，go.mod/go.sum一致。14份改动/现场资产逐文件SHA匹配，最终源码未在检查期间变化。规格及最终质量复核PASS；二次失效回归先RED后GREEN，并在最终完整检查中再次覆盖。

| 云端构建 | SHA-256 |
| --- | --- |
| parallel-planning-candidate | 8299a2ad309169535538c0b8f1abb981a86f5501a68079103c1c7d31c320c2f7 |
| parallel-planning-pilot.test | 2099657d82315f15c86ad226c52f2014ac48914a07de090fdb0d206b1bdca17c |

私有证据根：`/home/ubuntu/voice-latency-control-20261115/`，包括final-tests.jsonl、final-race.jsonl、final-verification-summary.json、final-source-sha256.json、final-compilation-input-check.json及parallel-dev/rpc-dev的RED→GREEN记录。原音、pcap、二进制不入Git。

## 现场与费用限制

两次被动采集均已结束，没有新有效设备会话；两份pcap仅24字节文件头。串口空闲告警不能作为本次音量超时原因。真实音量执行/回发延迟、网络丢失、id拒绝、读回失败仍待新现场证据，不能宣称音量超时已修复。

现场资产为[parallel_planning_test.go.txt](../../tools/tts-playback-pilot/parallel_planning_test.go.txt)，默认不参与仓库构建，精确opt-in才服务。仅指定微雪/鉴权owner、Opus、单并发、每会话180秒、服务10分钟；复用公共Session和当前模型，memory=false。沿用原账本，本阶段最多4个主回答，失败/未知不退还；这不是全部辅助API或硬件动作的总上限。只做四轮约定输入，第四轮结束后收尾，不追加第五条命令。入口仍需源/二进制/进程证明和新路由意图，结束核对停止与恢复；当前仅prepare，尚未apply。

普通聊天基础API调用数不变；点歌被处理可能增加已提交后取消的控制规划，依据失效也可能多一次重规划。候选不承诺省费。成品按每天30分钟、每月约900分钟比较主模型、ASR、TTS、表情、点歌、控制、历史及取消/重试的完整用量，按各自计费单位核算；缺usage保持未知，真实账单待核对，不用单轮问候外推月费排名。

截至本轮检查，正式Voice PID732503、管理PID635398，Caddy SHA adb6222394b10c444c5d563b241afc87be39aa17fb00d5bce2c4f2d22653f115；ready/memory_ready、0/2、diagnostics=false。没有正式部署、路由apply、固件改动或新付费推理。实际起播改善、音量修复、固定TTS逐字对齐/中断恢复及连续A/B均未验收。

## 14:02–14:03微雪四轮实测

用户确认在设备旁后执行。本轮开启前正式旧会话已结束，先核对原配置、正式进程、14份冻结来源及现场binary SHA2099657d，再启动仅loopback候选、生成绑定baseline/manifest/进程的proof，并单次apply指定MAC路由。未改正式Voice/管理unit或固件。

| 输入 | 话尾至首包 | 完整I2S已播 | 控制核对 |
| --- | ---: | ---: | --- |
| 自我介绍 | 1.946秒 | 8.22秒 | 无动作 |
| 音量调到80 | 2.510秒 | 2.10秒 | setter id3匹配，85ms后进入readback id4，147ms确认80 |
| 查询音量 | 2.792秒 | 1.98秒 | status id5匹配，273ms确认80 |
| 简短故事 | 2.464秒 | 19.38秒 | 无动作 |

四轮completed samples均等于sent samples，全部played；无turn_error、drain_timeout或session_error。用户反馈四轮完成、整体正常。14:03:36.917收到第四轮完整回执后，控制器停止临时实例；14:03:38的server_shutdown为约定测试收尾，14:03:39记录restore verified，不归为自发断线。

两规划提交时差分别约6.6/7.7/23.0/10.5ms，实际响应等待重叠0.578/0.636/0.819/0.550秒，证明实机路径确实并行。首包是服务端下发边界，不等于扬声器首音。本轮memory=false，正式旧服务memory=true，输入、历史和网络条件也不同；不能用四条样本计算稳定提速比例或长期可靠率。音量超时本轮未复现，旧故障未宣称根治。

### 同期旧入口的“第一次有问题”

用户随后反馈此前第二次调整正确、第一次有问题。正式13:47轮已在2.938秒回读80，随后只下发600ms音频、没有I2S回执；5秒信用等待后生成失败，再3秒drain超时关闭。该轮设置已核验成功，异常在确认语音播放链路，不能记作音量设置失败。13:48后一次设置/回读80为95ms，查询32ms；随后设置90为84ms、查询117ms，均verified。13:59旧入口设置85也在55ms核验。以上不消除12:17原轮setter ACK超时的未知原因，不把设备调度或网络假设当结论。

私有证据：`field-predecessor-v1/`两份冻结正式events摘要，以及`field-result-v1.json`；候选会话`parallel-pilot-v1/records/20261115T060243-8cb5f6cd154940806d44fa7917ceda93`，events SHA77570537c4102c88f4cc9b4e2cb6a9e92e058e1ea06e4f0fe1916710c601c947。UART只读采集确认AudioCodec的Set output volume to 80和各轮speaking→listening；采集已停止，原串口留忽略目录，不入Git。网络采集v3权限失败，v4 tcpdump以-11退出、文件0字节，无可用pcap证据；没有据此声称已定位网络问题，也未重跑真人/API测试。

### 本轮用量与恢复

| 模型用途 | 请求数 | input tokens | output tokens |
| --- | ---: | ---: | ---: |
| 主回答 | 4 | 15,553 | 495 |
| 点歌规划 | 4 | 5,713 | 188 |
| 控制规划 | 4 | 7,413 | 225 |
| 表情选择 | 4 | 2,701 | 105 |

16份数值usage均已取得；表情只计expression_selection，排除其native_session副本。另有4次OutputASR、合计31.68秒音频，以及输入监听/转写，不能把16次当成全部API数量。现有主回答记录只留顶层token，未留分模态计费细项；输入监听也缺独立账单usage。因此上述为用量证据，不能直接算精确金额，辅助文本token与主回答混合token不能按统一单价比较。普通聊天基础调用数未减少，本轮没有点歌handled或失效重规划，不能由此证明这两类额外费用为零。成品仍按30分钟/天、约900分钟/月比较完整费用。

原账本从协议9/主回答34增至9/38，本阶段4次主回答全部保留。pilot/window两个unit均MainPID0，采集进程结束；正式Voice732503/管理635398身份、7份受保护配置摘要、Caddy file/admin均恢复/保持与baseline一致，ready/memory_ready、0/2、diagnostics=false。候选开关仍默认关闭，未正式启用。固定TTS逐字对齐、中断恢复和连续A/B是独立待验项。
