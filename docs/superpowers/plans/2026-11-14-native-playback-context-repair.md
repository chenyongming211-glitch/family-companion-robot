# 原生会话播放边界修复执行计划

> **For agentic workers:** Use superpowers:subagent-driven-development执行；用户已批准实施，不再询问执行方式。直接main开发/提交/推送，不创建分支/worktree。所有运行检查在云端。

**Goal:** 分段保留可信已播文字，打断后修正同一模型连接的当前回答及状态，失败可明确降级重建。

**Architecture:** 复用现有回执/OutputASR和隔离runner，新增纯证据提取函数，串行化原生取消与历史替换；正式200项产品来源不整包部署，派生source-repair-v1验证。

**Tech Stack:** 现有Go1.27.3、gorilla/WebSocket与标准库，无新增语音服务或固件功能。

设计：[已批准范围](../specs/2026-11-14-native-playback-context-repair-design.md)。

## Task 1：固定派生环境和检查原生成语音项

- [x] 核对正式PID/原Caddy/admin、原209来源、v8摘要及共享账本3/13；复制source到source-repair-v1并记录原清单，保留旧证据。
- [ ] 准备仅显式env允许才连接的云端协议检查源码与fake保护测试；调用前持久intent、未知不重试、两版补检合计协议最多2、主要回答总计最多4，凭据仅云端LoadConfig。
- [ ] 检查两类真实生成assistant音频项：completed后delete/create/retrieve，以及生成中cancel→同response终态→delete/create。精确ACK/身份/顺序必须核对；后续仅允许已同步上下文的诊断回答，结果私有保存。
- [ ] 失败明确降级，不以文档或合成typed项代替生成音频验证，不擅自增加探针。

## Task 2：分段已播证据（纯函数）

Files: `internal/realtimestate/playback_segments.go`、`playback_segments_test.go`；旧history/alignment保持语义，新增函数先供隔离runner调用。

- [x] 先写保护测试并在云端观察RED：中间一个字不同仍保留独立前后可信短句；重复锚点/乱序/未播/120ms余量内/缺字词时间/缺终态不猜；完整播放与原证据规则一致。
- [x] 实现`ConfirmedPlayback(*Turn) PlayedSegments`，`PlayedSegments`至少含`Segments []ConfirmedSegment`、`PlayedSamples int64`、`Complete bool`、`Unknown bool`；`ConfirmedSegment`含`Text string`及`BeginMS, EndMS float64`。使用已有ASR字词和有序唯一文本锚点，限制现有上下文长度，不增加依赖。
- [x] 同步到派生云端源码，普通/race目标包检查通过，核对旧契约未变化。
- [x] 规格审查通过后质量审查；确认没有将模糊/未播文字当事实。

## Task 3：取消终态与同会话历史修正

Files: 隔离runner现有`native_device_pilot_test.go`与保护测试；Git在`tools/native-session-pilot/`保存相对固定v8的补丁、新增test-only源码和云端使用说明，不加入默认产品服务入口。

- [x] 先RED：completed后被Cut的回答在同连接替换；生成中取消终态后同连接复用；多assistant item全部处理；新response必须晚于final播放和deleted/created核对；未知ACK/身份/重排/超时关闭旧连接并可信重建；晚到输入ASR与重连身份隔离不退化。
- [x] 保存每个回答的response/item/user身份；每个连接单读者、单串行写路径，取消只执行一次，终态有界，不在ctx取消时无条件先关闭可修复连接。错误/预算/未知不自动重试原输入。
- [x] 用Task2证据替换被打断回答，独立system项表达中断/未知/当前请求优先，不把状态文字作为实际已播原话。正常轮次和其他历史保持。
- [x] 复用原重建退路，补同样中断状态，避免只留下旧user；不扩大到提示词专项纠错、语速/音色或固件。
- [x] 云端fake普通/race、实际相关包、构建和两阶段审查通过；完整来源清单绑定派生目录与二进制。

## Task 4：隔离实机与交接

- [ ] 准备新路由operator版本/独立prepare目录和新PID/source proof，不复用已消费v2/v3意图；正式空闲时仅精确目标分流。
- [ ] 用户手动测试故事中换问题、明确暂停再继续；比对已播段、未知、中断状态、旧item删除和原生session身份，不把普通mock/协议当听感成绩。用同一账本，最多总48/8。
- [ ] 结束恢复原Caddy/admin，停止仅本轮实例，固件不变、0B增量。
- [ ] 更新验证/主设计/交接与计划，main提交推送；未验证项目及失败原样保留。
