# Go Realtime 单设备音频诊断实施计划

> 执行方式：在当前会话内按 writing-plans、executing-plans 与 test-driven-development 顺序实现。遵守仓库要求直接在 main 开发，不创建分支或 worktree。按 executing-plans 的要求，完成后仅委派一次独立只读代码复核，不委派实现任务。

**Goal:** 为一次指定设备的短复现记录音频收包、解码、排队、写入千问、控制来源和播放结束，区分无上行、近静音、积压、检测缺失和停播执行。

**Architecture:** 诊断默认关闭。CLI明确指定已鉴权device_id时，每个进程只认领一次匹配的小智会话，最长5分钟。独立0600诊断JSONL、固定大小事件队列和异步写入；写入失败、队列满、期限或1MiB预算耗尽均不改变语音行为。只记录计数、时序、音量统计和固定枚举，不保留PCM、Opus、原始控制JSON、转写、模型回答或凭据。

**Tech Stack:** Go现有标准库、现有Opus解码和WebSocket；所有运行测试/构建在云端隔离源码目录执行。

## 全局约束与接口

- 仅新增诊断，不修改VAD阈值、DSP、语音/工具路由、记忆、播放封口和取消语义；不打开串口，不刷固件，不做自动播音。
- `Config.DiagnosticsDevice string`、`Config.DiagnosticsDuration time.Duration`；空设备ID关闭，时长默认5分钟且最大5分钟。
- `Server.attachDiagnostics(*Session)`只认领通过既有入口鉴权、ownerErr为空且client_type=xiaozhi的匹配会话；不同设备、Mac、旧无归属会话均不消费认领。
- `audioDiagnostics`内部提供收到帧、PCM摘要、排队/取队、上游写入开始/结束、控制事件、已有安全事件及中断来源观察接口；接收/发送路径不进行诊断文件I/O。
- 上游写入完成不是供应商接收确认；设备上报reason不是已验证的物理来源；能量统计不是新增VAD。
- 应用层按原时序运行；诊断自身失败不调用Session.fail，也不把诊断写入共用记忆事件流。

## Task 1：有界诊断核心与RED→GREEN

**Files:** 新增`internal/realtimelive/audio_diagnostics.go`、`audio_diagnostics_test.go`。

- [x] 先写`TestAudioDiagnosticsCountersAndPrivacy`：输入已知PCM和各阶段计数，未知控制原因与任意文本不能进入JSONL，文件权限0600，末条包含累计统计。
- [x] 写`TestAudioDiagnosticsBlockedWriterDoesNotBlockVoice`：阻塞诊断writer，实际WebSocket音频仍可发送，事件队列满时计数丢弃。
- [x] 写`TestAudioDiagnosticsExpiryAndBudget`：到期停止采集、无音频时仍有周期统计、文件字节预算有界。
- [x] 写`TestAudioDiagnosticsExclusiveFile`：既有文件/符号链接不能被覆盖或跟随，诊断失败不影响语音。
- [x] 在云端运行`go test ./internal/realtimelive -run TestAudioDiagnostics -count=1`，保存功能尚不存在的RED证据。
- [x] 实现上述接口：原子计数、分窗RMS/峰值、单调时间差、固定大小非阻塞事件队列、单writer、预算/时限、无阻塞停止信号。
- [x] 云端运行同组`go test -race`并确认GREEN。

## Task 2：按归属接线及来源记录

**Files:** 修改`config.go`、`server.go`、`session.go`、`xiaozhi_entry.go`、`xiaozhi_wire.go`、`cmd/realtime-output-live/main.go`；新增对应集成测试。

- [x] 先写`TestAudioDiagnosticsOwnershipAndSingleClaim`，覆盖默认关闭、其他设备/Mac/无效归属不认领、目标仅一次和参数拒绝。
- [x] 写`TestAudioDiagnosticsDeviceControlProvenance`，覆盖listen.start模式、设备abort原因白名单、错误scope拒绝、原协议返回内容不变。
- [x] 写`TestAudioDiagnosticsInterruptSources`，覆盖VAD起点/话尾、设备abort和生成失败；无活动输出也保留请求证据，既有取消行为不变。
- [x] 写服务器与伪上游的连续音频集成用例，验证实际收包、解码、排队、上游成功写入与静音期周期日志；不调用真实模型。
- [x] 云端运行新增用例确认RED，再逐处接线并确认GREEN；不增加新网络接口。
- [x] 所有后台运行测试使用云端已有Go1.27.3、libopus1.4及现有冻结Python参考，不在Mac运行服务或后端测试。

## Task 3：回归、发布与用户复现准备

**Files:** 更新交接、验证记录与当前客户端说明；部署脚本仅保存在私有诊断工作目录。

- [x] 云端执行`go test -race -json ./... -count=1`、`go vet ./...`、`go build ./...`；完整输出写私有日志并核对通过/跳过/失败计数。
- [x] 单独复核日志未保留敏感内容、队列/期限/文件限额、所有诊断I/O均脱离音频路径、关闭日志后原行为仍通过现有测试。
- [x] 提交并推送main；冻结提交后用相同源码构建版本化二进制，核对本机/云端修改文件哈希。
- [x] 检查所有活动会话为0，备份原二进制及发布意图，记录unit/env/产品身份/凭据表/设备身份库哈希；验证失败恢复原二进制。
- [x] 使用一次性`/run/systemd/system`覆盖启动诊断目标；启动验证后移除覆盖并daemon-reload，当前进程保持一次性诊断能力，下次重启恢复默认关闭。永久unit和敏感配置不变。
- [x] 健康检查确认新build、ready、记忆开启、max_sessions=2、诊断已待命；不启动模型或实体音频。
- [x] 再通知用户开始微雪单次短测试：正常问答→故事静默→直接插话；失败时保持会话10秒，不按键/重唤醒。部署成功不等于根因已定位或实机故障已修复。

## Review focus

重点检查并发关闭与文件阻塞不会挂住会话、边界丢日志有计数、异常设备reason不会泄露文本、原归属鉴权不被诊断开关绕过，以及额外计数不会污染已播历史或记忆。

## 执行记录

- 2026-10-29：用户“准备好了，下一步干什么”后，先核实功能尚未实现，云端a669f80/ready=true/active_sessions=0，开始准备诊断实现。未打开任何串口。Go工具链路径为`/opt/family-robot/toolchains/go1.27.3/bin/go`。

- 2026-10-29 23:00：功能09d22e6已推送并空闲部署，839 pass/2既有skip/0 fail，vet/build通过；9文件哈希一致，6份保护配置不变，微雪一次性诊断待命。实机复现尚未进行。
