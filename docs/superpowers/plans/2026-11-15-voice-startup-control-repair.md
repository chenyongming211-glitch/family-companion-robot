# 起播等待与控制回执最小排查实施计划

> **For agentic workers:** 使用 systematic-debugging、TDD、subagent-driven-development；直接main，不建分支/worktree。用户已授权继续，修复沿既有功能与权限边界推进。所有运行测试/依赖/服务仅云端；正式服务保持，现场候选先隔离核对。

**Goal:** 缩短已证实的纯规划串行等待，定位音量设置回执丢失环节，并记录完整API开销。

**Architecture:** 只在公共Go Session中提前并行两个无副作用的规划。音乐优先、记忆操作、硬件执行、回读、主回答及Opus/回执保持原顺序和校验；新增一个默认关闭的候选开关，不引入新服务/模型/框架。音量先采集真实RPC，按实际故障修复，不延长超时或重发mutation。

**Tech Stack:** 现有Go、gorilla/WebSocket、标准库测试、现有Bridge、被动USB日志与云端loopback捕获。

## 已确认范围与费用

用户确认故事结尾完整听到，且12:18:06附近按过按钮或休眠。该次回聆听和1006不按自发故障修复；固件CloseAudioChannel直接释放socket能解释1006。

首包前有点歌规划约0.9–1.1秒、设备控制规划约0.7–2.5秒；表情已经并行。候选只把两项纯规划重叠，预计最多隐藏二者较短部分，须实测，不能承诺总延迟。普通聊天调用数不变；音乐已处理轮可能产生一份取消但仍计费的控制规划，必须记录。候选不声称省钱，完整用量/效果比较后决定是否启用；不融合跨会话音乐背景与控制授权，不做关键词绕过，不投机生成主回答。

## 1. 纯规划并行候选

Files: `internal/realtimelive/{config.go,session.go,device_controls.go,semantic_control.go}`；必要的一个小型`control_prefetch.go`及对应测试；`cmd/realtime-output-live/main.go`。

- [x] 先云端RED保护检查：阻塞音乐/控制提供方，两者在放行前均已开始；音乐handled时硬件零调用；主回答只在路由及控制结算后开始；取消收拢无泄漏。测试使用现有合成WS/Session夹具，不调用真实API。
- [x] `Config.ParallelPlanning`和`--parallel-planning`默认false；只有音乐启用、controls存在且有本轮最终转写时提前规划。清记忆命令沿原顺序，不提前规划。未开启时保持原调用路径。
- [x] 仅提前执行现有`semanticPlan`，绝不提前调用`RunControlPlan`（后者会写硬件）。先冻结turn、memoryFloor和最近核验controls；执行前重新比对原计划依据（当前controlHistory投影、floor/recent），失效则沿原顺序至多重新规划一次，不能用旧锚点做相对设置。
- [x] 音乐仍先完成路由。handled分支取消并等待控制规划；普通分支仍先routeMemory，再用原executeControlPlan/结果指导/上下文更新。前置工作只用本轮context，结束时cancel/join；取消/旧代/私库撤权不得发出控制。
- [x] 复用原有控制计划验证与执行结果，不复制算术、权限或回读逻辑；原3秒规划预算保持。主回答不提前生成或发声。
- [x] 补精确的规划耗时/请求数/数字usage记录，未返回usage记unknown，不将取消当零费用。只记录受限元数据，不记录握手凭据或原音；不重复统计表情记录及native_session副本。
- [x] 云端相关普通/race/vet/build；规格后质量复核；实机候选前冻结源码/二进制，默认正式行为不变。

关键测试断言（放行信号使用channel，不靠固定sleep）：

```go
<-musicStarted
<-controlStarted
if hardwareCalls.Load() != 0 { t.Fatal("planning executed hardware") }
close(allowMusic)
// handled / canceled / changed-floor / changed-recent each retain the existing guard.
```

## 2. 音量回执真实采集与修复

- [x] 核对正式Go bridge与实际2.5.4源码：setter回复true/数值RPC id格式一致；旧轮在setter ACK接受之前超时，未进入readback。尚无证据支持改JSON或增加5秒超时。
- [x] 核对正式入口的“音量80→查询音量”，随后候选四轮同步只读UART及有限RPC诊断。Mac不写串口/切控制线；云端loopback限时/8MiB尝试失败，无可用包证据，保留失败且不为此重跑真人/API测试。原始数据留私有/忽略目录。
- [ ] 根据实际证据区分：未到设备、设备执行/回发延迟、错误id/解析拒绝、读回失败。只针对证实的原因新增RED→GREEN；setter已下发不得无条件重试，未知不得假报成功。
- [x] 若现有真实采集不足，再在Bridge/controller补有限RPC元数据（无raw payload），不虚报已修复。

早期两次被动窗口无会话；用户后续完成正式及候选测试，已取得RPC与UART证据。候选设置/查询均成功，未复现原setter超时；旧13:47异常为设置已成功、确认语音缺播放回执。原12:17 ACK超时的根因及针对修复仍待完成。诊断增量不改变协议、5秒超时、回读或去重。详见[候选及现场验证](../../verification/2026-11-15-voice-startup-control-candidate.md)。

## 3. 收尾

- [x] 停止本轮精确采集进程，保留失败/未知证据；候选单次apply/restore已核对恢复，正式服务/固件保持。第四轮完成后按约定server_shutdown，不能误判自发断线。
- [x] 更新用户已确认的“正常播完回聆听、按键关闭”及剩余未验项；main提交推送。固定TTS/连续A/B仍为独立待验范围。
