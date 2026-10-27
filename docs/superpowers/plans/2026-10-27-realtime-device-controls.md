# Go Realtime 设备控制实施计划

> For agentic workers: 使用superpowers:subagent-driven-development与TDD完成这一项会话集成，独立规格审查通过后做质量审查。用户已批准范围与连续实施，不重复询问执行方式；直接main，不创建分支/worktree。所有运行测试在云端。

**Goal:** 让当前M5/微雪通过18040/18041控制各自音量、亮度和查询状态，保留搜索、流式语音及插话排空。

**Architecture:** 每Session独立MCP Bridge；候选转写经无声Native Function Calling解释和执行，核验结果再交原搜索语音链路回复。普通聊天无额外模型调用，Mac兼容；无全局工具/设备可变状态。

**Tech Stack:** Go、现有gorilla/websocket、devicetools、原生千问Realtime、小智MCP；不新增运行依赖。

## 1. RED与云端环境

- [x] 新增 `internal/realtimelive/device_controls_test.go` 和 `native_tools_test.go`，用真实Bridge及httptest WebSocket覆盖：DiscoveryAndVerifiedReadback、CancelBeforeDispatch、LateResultNoContinuation、TwoDevicesIndependent、MCPWrongScope、NativeToolsRelativeAdjustment、NativeToolsDuplicateAndWrongResponse、NoToolsOnSearchPath。先在新云端源码目录运行，记录因缺少行为而失败的实际退出码。
- [x] 云端源码根为 `/tmp/realtime-controls-20261027/source`，从当前Git基线归档初始化，只同步源文件。运行命令：`/opt/family-robot/toolchains/go1.27.3/bin/go test -race ./internal/realtimelive ./internal/devicetools ./internal/realtimestate`。PATH包含既有Python venv供冻结参考使用，不把凭据复制到Mac。

## 2. 生产接线

- [x] 新建 `internal/realtimelive/device_controls.go`：每会话Bridge/计时器/请求结果通道，封装发现、Call、Receive及ctx取消。JSON-RPC请求用Session.send串行写socket；取消先于队列推进，写入前再次检查轮次ctx。复用旧Bridge验证，不复制一套参数或回读实现。
- [x] 修改 `xiaozhi_entry.go`、`xiaozhi_wire.go`、`session.go`：仅MCP协商设备初始化工具，MCP控制消息校验scope并传递payload；未协商仍忽略。关会话关闭工具循环；音频/播放回报继续由原读取路径处理。
- [x] 新建 `native_tools.go`，只为工具配置添加text modality与固定tools。用conversation.item.create/input_text、response.create、response.function_call_arguments.done、匹配function_call_output完成多步调用；response.done校验，最多8调用/有界参数与总时限，重复不重放、未完成时不执行动作。
- [x] 新建候选路由文件并更新 `liveTurn` 的转写就绪通知。控制候选包括音量/声音/亮度/屏幕/小声/太吵/太暗及上下文相关相对表达；模型负责实际意图，不由词命中直接执行。普通语音不走工具模型；记录实际转写等待与路由耗时。
- [x] 真实语义探针发现其他设备目标可能被规划器错误替换为当前设备，须先RED复现，再增加目标授权。明确其他目标/当前整台禁令不得调用规划器或下发MCP；允许清楚的否定其他目标后控制当前目标、以及只禁止音量但调亮度。使用故意返回设置调用的不合作规划器验证执行边界，再复验真实语音。
- [x] 修改 `internal/realtimestate/identity.go`：设备控制能力由会话发现事实决定，当前无工具回执不得伪称成功。原搜索与Mac默认配置保持，已播上下文/旧音频排空不变。

## 3. GREEN、审查和真实模型

- [x] 重跑针对性测试达到GREEN，随后全仓 `go test -race -json ./...`、`go vet ./...`、`go build ./...`，保存失败/修复与最终结果。
- [x] 规格独立审查后质量独立审查；未解决的范围、取消/设备隔离和语音退化问题阻止发布。
- [x] 云端真实模型+模拟MCP核对“音量设为20”“小声一点”“屏幕暗一点”“关闭屏幕”“现在音量多少”；否定/引用/另一个设备不操作。确认原联网搜索仍有启用回执与实际检索证据，旧回复取消/排空回归通过。

## 4. 发布与交接

- [x] 更新当前交接及验证报告，明确用户选择结束双设备阶段，不将省略的实机测试补记通过；已知串音/300秒关机不顺带修改。
- [ ] 直接main提交/推送并核对远端；空闲备份并部署18040/18041，核对build、ready、容量与既有身份，保留回退。
- [ ] 报告实体设备控制待真人确认的范围及测试命令；不将模拟MCP或工具返回当作用户已感知音量/亮度效果，不刷固件、不改变唤醒/电源/A动效。
