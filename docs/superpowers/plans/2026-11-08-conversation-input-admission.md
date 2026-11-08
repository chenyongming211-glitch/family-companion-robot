# 连续对话准入与屏幕能力 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Root统一提交，单个实现任务顺序执行，规格后质量审查。

**Goal:** 阻止空输入切掉/替换有效回答，补齐主语音模型的真实屏幕能力。

**Architecture:** VAD候选→匹配的有效流式识别预览确认自然打断→最终文字准入主回复；屏幕能力只来自既有独立协商。旧输出、显式abort、流控/身份和名称机制保持。

**Tech Stack:** Go1.27.3，云端runtime，Mac代码/Git及客户端。直接main，无分支/worktree。服务最小增量来源仍由实际当前发布manifest确定，不整包部署main；不改固件。

## Task1：输入候选/确认/准入

Files：`internal/realtimelive/session.go`；新建`session_input.go`/`session_input_test.go`；必要的既有listener fake测试fixture修正。

- [x] 写生产listen方法的行为测试，先云端RED。覆盖620ms空VAD不取消旧声、0新生成/控制/情绪；有效预览在final之前打断；最终非空一次准入；空/标点/错ID/旧ID/重复final/乱序committed；关闭/abort失效；有界候选；临时文字不执行控制或记忆命令。
- [x] 实现有界候选状态和真实item身份，保留语音主路径不等待说话人身份。旧fixture补真实ASR事件，不删除原断言。
- [x] 定向race GREEN、全仓race/vet/build，规格/质量分别复核，root提交。复核补齐stop→preview→final的text/stash回归；213项摘要前后及本机一致，尚未部署。

## Task2：主模型设备能力事实

Files：`internal/realtimelive/device_controls.go`（sessionFacts）、`internal/realtimestate/identity.go`，相关测试。

- [ ] 先RED验证协商连接的快照/提示包含15种屏幕情绪，与L0/L2区分；无协商/Mac无此能力，旧默认冻结提示不改。
- [ ] 用既有规范情绪来源和独立能力标志实现，不硬编码机器人名称，不修改语音搜索/工具配置。
- [ ] 云端相关race/vet/build与独立规格/质量复核后root提交。

## Task3：控制范围与单轮停止

Files：`internal/realtimelive/device_controls.go`、`native_tools.go`；可新增独立控制意图/范围模块及对应测试。

- [ ] 先云端RED：真实故事转写不启动设置；只请求音量不能设亮度，状态查询不能写；换call_id也不能重复设置，冲突批次先验证后执行；保留明确单/双字段、相对读取、其它目标/否定/引文拒绝。
- [ ] 服务端当前请求的范围校验约束工具集合及执行入口；同字段一次，所需核实回执齐备即停；记录拒绝/部分完成，不重试设置。
- [ ] 定向/全仓race、vet/build及独立规格/质量复核后root提交。

## Task4：实际来源验证和最小发布

- [ ] 从当前实际a0fd847-r1来源复制新候选，保留130份旧来源核对，只移植Task1/2/3，适配main未部署的实验字段；不引入MemoryOnly/speaker变化。
- [ ] 云端真实增量事件及合成语音对照，核对空候选不出新回复、正常回答完整、有效插话及时、0额外ASR模型及每轮情绪预算不变，失败留档。
- [ ] 确定回退、保护文件/名称资料/窗口/另一网关与健康检查，0会话发布；无需重新刷固件。
- [ ] 最少一次现场正常问答+插话确认，新的连续对话问题未通过前不宣称整体验收完成；关闭临时客户端，更新交接/验证并推main。
