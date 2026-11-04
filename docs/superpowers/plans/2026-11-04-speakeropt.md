# 声纹优化实验 Implementation Plan

用户已批准执行建议并发布与上次对比。使用subagent-driven-development，直接main，无worktree；基础0484f44。

## Global Constraints

- 依据[优化设计](../specs/2026-11-04-speakeropt-design.md)，L保留原50人413评测；S新划分必须短／长／模型共用同一独立评测，不把换数据当优化增益。
- Go／ONNX／运行依赖／数据处理／运行测试全在云端；Mac仅源码／Git。原始录音、向量、凭据、模型、二进制、原始运行记录不进Git。
- 旧evaluate／evaluate-large／benchmark／session边界、JSON与行为不变，不改正式服务／模型／阈值／env／unit／VAD／AEC／固件，不开麦。
- 不根据evaluation或陌生人成绩挑人／句／模型／规则／阈值；质量失败留分母，全部预注册规则／失败与资源保留。

### Task 1: 离线质量审计与登记研究工具

**Files:** create tools/speaker-lab/quality_audit.go、quality_audit_test.go、study_manifest.go、study_manifest_test.go、enrollment_study.go、enrollment_study_test.go；modify main.go、evaluation.go（小型私有提取门禁接口）、README.md、assets.lock.json；如需当前nativeExtractor明确拥有的samples清理可局部修改worker.go。不复制整段worker提取循环。

- [x] 云端RED：独立命令、严格schema与大边界默认隔离；audit与WAV SHA／samples／固定VAD配置绑定；错误／取消／缓冲归属；VAD人声片段裁切／去重／重置／清理；20秒登记总长／数量／首条／归一化融合／零和拒绝；人物角色分离／文字录音跨角色泄漏；校准只用10校准人物，FAR同分／预算边界；短长／energy-vad／50与10×5及陌生5指定组计数；所有失败分母、Top-1含／不含质量失败、匿名报告和旗标；旧CLI兼容。
- [x] 实现GREEN，质量audit二进制就绪时可先交root作仅波形资格审查（无身份评分）；最终清单仍在审查通过后冻结。
- [x] 全lab普通／race／vet／build，native旧5项＋VAD opt-in、V2兼容及source摘要；原5／原50base除时间资源外JSON语义保持。只在云端执行。
- [x] 限定文件commit main，不push；独立规格／质量审查，关闭问题。

### Task 2: 冻结语料并执行L／S对照

- [x] 官方素材完整流MD5／全来源目录／模型Git对象链核验；quality-audit输入冻结、配置固定；S选择前50长登记合格／前10独立校准／前5陌生人物，记录排除与旧50重合情况、双方配对。
- [x] 独立审查audit绑定、模板与评测／校准／未知来源隔离、两设备相同分母；登记20秒录音总长与VAD人声估计分别报告，不假称连续朗读或20秒净语音。
- [x] L固定原清单base语义复验与V2；S每模型×两设备各提取一次，全部短／长／energy-vad／四规则／50及10×5／陌生指定组计分；有界执行，不重复择优。
- [x] 分开上次精确对照与新划分A/B/C／门禁／校准收益，检查每一总量／比例及资源；前后保护5摘要／2PID、实验进程回收。

### Task 3: 报告、整体审查与发布

- [ ] 更新验证报告、handoff及方案／计划状态，完整保留失败／误认／拒识与有限性。
- [ ] 整体独立审查，文档链接／diff／Git产物检查；main提交推送、核对远端与干净checkout，关闭本轮临时SSH。


## 已执行事实（最终整体复核前）

Task1已完成：08e6723实现、4da5b88关闭not_run陌生分母和有限open-set标签问题，规格／质量复审PASS。最终云端274普通／race通过、7 opt-in默认skip、覆盖75.4%、vet/build；旧原生／VAD及V2兼容探针、原5／50人语义回归均保留对应构建与摘要。

Task2已完成：官方来源和模型核验、1,706条VAD审计最终逐字节一致；50已知＋10校准＋5陌生、两条件各772条的新划分通过独立数据审查。6个新增命令逐一执行一次、无身份成绩调参；2个L/base复用已完成回归。共288个汇总核对，正式2PID／5摘要保持，实验进程回收。完整结果及限制见[验证报告](../../verification/2026-11-04-speakeropt.md)。

结论为保留Go＋sherpa＋base，优先多句约20秒登记；S跨设备合格Top-1在同一energy视图80.95%→94.81%，V2长登记84.42%，VAD硬过滤损失覆盖。Top-1不是正确接纳率；陌生5人／25条不保证总体FAR。本轮没有上线产品登记／模板库或正式阈值。

Task3剩余：整体独立复核、main推送／远端核对与临时SSH关闭。无需因文档收尾重跑已完成模型实验。
