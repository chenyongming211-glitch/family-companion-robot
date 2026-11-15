# 起播规划并行候选与音量回执诊断

日期：2026-11-15。接续[正式会话核对](2026-11-15-formal-conversation-latency.md)及[实施计划](../superpowers/plans/2026-11-15-voice-startup-control-repair.md)。本轮完成代码和云端合成检查；尚无候选实机结果，正式服务和固件未变。

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
