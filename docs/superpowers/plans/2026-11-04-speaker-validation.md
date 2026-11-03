# 声纹评测与现场引导 Implementation Plan

> **For agentic workers:** 使用 superpowers:subagent-driven-development 按任务实施；直接 main，不创建分支或 worktree。用户已批准先记录后开发，无需重复请求方案确认。

**Goal:** 交付可重复的公开真人声纹评测入口和隔离客户端单人三步引导。

**Architecture:** 沿用 Go Realtime／sherpa-onnx；新增评测命令复用原 worker。客户端引导只消费已验证事件，不改变音频。完整分段、候选模型和多人现场是后续独立验收。

**Tech Stack:** Go 1.25、sherpa-onnx-go-linux/v2 v2.0.1、Swift／AppKit。

## Global Constraints

- 依据 [已批准设计](../specs/2026-11-04-speaker-validation-design.md)。
- Go／ONNX／语料处理与后端测试只在云服务器执行；Mac只编辑／Git及客户端 SDK 构建、无麦克风客户端状态测试。
- 直接 main 开发、提交和推送，不创建分支或 worktree。
- 不更换当前模型、阈值、VAD、AEC或产品入口；不操作设备、不自动开麦，不保存真人录音／向量／姓名／结果日志。
- 固件、模型、运行证据、语料、凭据和设备备份不进入 Git。

### Task 1: 隔离客户端单人三步引导

**Files:**
- Create: `tools/realtime-output-live/SpeakerGuide.swift`
- Test: `tools/realtime-output-live/tests/SpeakerGuideTests.swift`
- Modify: `SpeakerObservation.swift`、`LiveClient.swift`、`main.swift`、`build.command`、`build-soak.command`（均在 `tools/realtime-output-live/`）及 `tools/realtime-speaker-test/README.md`。

**Interfaces:**
消费已校验的结构化 observation（input_id、state、label、reason、audio_ms），按会话 generation 交付。Guide 输出提示文字、当前步骤和能否准备下一步；提供新连接、ready、播放忙／空闲、准备本步、结果、断开入口。保留原 `SpeakerObservationState.apply` 兼容行为。

- [x] 先写无麦克风 SDK 测试：未 ready／关闭／播放中不能准备；第一条新 pending 绑定步骤；仅 final 可判定；重复／迟到不可推进；短／长／弱声／不完整／匹配不确定给具体建议；失败重试需新 input_id；断开与新连接清除身份；三句通过仅限一致性。以类型缺失或预期断言失败取得 RED。

```sh
xcrun swiftc -swift-version 5 tools/realtime-output-live/SpeakerObservation.swift tools/realtime-output-live/SpeakerGuide.swift tools/realtime-output-live/tests/SpeakerGuideTests.swift -o /tmp/speaker-guide-tests
/tmp/speaker-guide-tests
```

- [x] 实现状态机与最小 UI 接线；SDK测试 GREEN，原 observation 测试保持通过。保留正常窗口，三个构建入口均包含新增文件。
- [x] 构建普通／试点／soak，检查签名和脚本；不启动 App，不申请麦克风。
- [x] 独立规格／质量 review，修复阻断项，限定文件提交 main。

### Task 2: 公开真人离线评测

**Files:**
- Create: `tools/speaker-lab/evaluation.go`、`evaluation_test.go`
- Modify: `tools/speaker-lab/main.go`、`main_test.go`、`README.md`

**Interfaces:**
`evaluate --model ABS --sha256 SHA --manifest ABS` 返回固定公开真人评测报告；清单来源、独立划分、输出字段遵照设计第4节。复用 `boundedFile`、`loadFixture`、`startWorker`、`similarity` 与 `summarizeTimes`，不调用 `runBenchmark` 冒用 synthetic 标签。原`calibrate`仅使用首两音色四条样本，不能直接作为公开真人校准；新增评价器使用所有合格校准片段的同／异人对，冻结后才评分。建模板只用 enrollment，计分只用 evaluation；不更改 `session` 固定研究阈值。

- [x] 先写云端测试：严格字段／来源／HTTPS参考、至少3人、划分最小数、跨划分原录音／文字／音频泄漏、重复、无效向量、不能分离拒绝、评价集不影响校准、误归／拒识分别计数、报告不泄漏、CLI错误固定。新源码复制到独立云端目录后运行并记录 RED。

```sh
cd /private/source/tools/speaker-lab
go test -run 'TestEvaluation|TestEvaluateCLI' -count=1 ./...
```

- [x] 实现清单、独立校准／建模板／评分、受控 worker 和 CLI。相同专项 GREEN，再完整 lab 普通／race／vet／build；相关原生用例使用云端已锁定模型，不获取真人麦克风。
- [x] 用服务器现有合成 fixture 验证 CLI 的工程链路，报告标记为工程检查，不以伪造 `public_human` 来源冒充公开真人基准。实际公开真人语料运行另行真实记账。
- [x] 独立规格／质量 review，修复阻断项，限定文件提交 main。

### Task 3: 首包收尾与后续证据

- [x] 记录源码绑定、命令／结果／skip、客户端构建、正式保护检查；云端恢复／获取公开真人语料未完成时明确列为待做。
- [x] 更新开发交接及11月1日声纹设计入口，避免旧段落冒作当前结论。
- [ ] 全改动审查、文档链接／差异检查，提交推送 main 并核对远端与干净工作区。
## 后续独立阶段（不计首包完成条件）

- [ ] 扩大公开真人模型基准、候选ERes2NetV2与Diart／pyannote分段对照、两人及实体播放验收继续按设计分期，完成对应证据后再选择模型和接入产品。本轮已完成既有两模型的首组公开真人基线。
