# 故事早停最小修复 Implementation Plan

> 执行方式：使用 subagent-driven-development，单个代码任务加独立审查；用户已要求直接修复，按现有 main 授权连续执行。

**Goal:** 收紧既有完整短故事要求、补全 C 原生终态诊断，并验证后只发布必要 Voice 改动。

**Architecture:** 复用 StorytellingGuidance 和 Native.Stream 的既有 intent，不新增服务、业务判定器或自动续讲。

**Tech Stack:** 当前 Go、WebSocket、既有私有 intent 和云端工具链，无新依赖。

## Global Constraints

- main 直接开发提交推送，不建分支/worktree。
- 运行时测试、模型调用、构建、部署仅云端；Mac 只做代码/Git。
- 仅支持微雪 ESP32-S3 2.16，本次不刷固件、不操作 USB。
- 凭据、原音、设备备份、生成二进制不入 Git，不复制到 Mac。
- 只改现有故事提示与 C 原生受控终态日志，不增加关键词意图层、判定模型、自动续讲/重试、token/音色/播放窗口调整。
- completed 仅是上游本轮生成结束，不能宣称用户任务完成；缺少详细原因须保留未知。

### Task 1: 提示与终态日志的最小改动

**Files:** internal/realtimestate/identity.go 及相关测试；internal/realtimelive/native.go、native_usage_test.go 或新的针对性测试。

**Interfaces:** 不改变公开接口。既有 live_native_turn intent 增加受控 terminal 字段；不改变原 state/usage 的含义或 streaming/错误/取消行为。

- [ ] TDD RED：通过真实隔离 WebSocket 服务验证匹配 response.done 的 completed/非 completed、缺失原因和恶意/超长终态字段；日志应保留安全原因及字数，不复制任意 provider payload。直接检查原 intent 文件，不只测试新 helper。
- [ ] 最小实现：identity 的现有短故事要求明确“直接正文、同一回复推进到行动和结果、不能只停在开场/背景”；保持长故事分段和普通问答语义。Native 在终态身份匹配后先保存受控终态，再沿用原有效/无效响应处理。原因只接受有界协议标识，正文/错误 message 不保存；没有原因用 null 或显式未知。
- [ ] 云端 GREEN：go test -count=1 ./internal/realtimelive ./internal/realtimestate；新终态专项 -race；相关 vet 与 Voice build。使用实际权限范围，不以 optional skip 冒充验证。
- [ ] 自审职责/依赖与最终 diff，提交该任务文件，交独立规格/质量审查。报告 RED/GREEN 命令和结果、源 SHA、限制。

### Task 2: 有界云端对照、发布与交接

**Files:** 本计划/设计、验证记录、DEVELOPMENT_HANDOFF；合成输入/输出、私有 helper 和产物仅云端。

- [ ] 以当前部署来源最小叠加 Task 1，保持原 source 差异；冻结编译输入与候选 SHA，不整包覆盖 main。
- [ ] 复用云端合成输入或既有 TTS 合成“讲个小故事吧”，相同模型/音色/上下文对照旧/新提示。最多旧 1 轮、新 2 轮，保存数值 usage 与真实终态，不连接设备、不播放。人工检查人物行动/变化/结果，不用字数直接宣称故事完成。
- [ ] 若行为证据支持，独立审查后空闲时仅更新 Voice exe；管理/前端/配置/绑定/记忆内容/其他服务与静态防火墙保持，云盾动态 SSH 名单按已核实规则单独核对。保留失败证据，不恢复旧数据、不复用已消费发布 marker。
- [ ] 核对实际 PID/exe/SHA/build/ready、管理私有只读 C 状态及原 IPC 兼容；现场效果单列待验。更新交接、提交推送 main、核对远端与干净 Git 工作区。
