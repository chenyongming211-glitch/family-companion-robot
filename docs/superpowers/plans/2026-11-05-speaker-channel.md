# 五人家庭通道对齐执行计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development task-by-task. 用户已批准五人及其余验收条件并要求开始，连续执行，不重复请求执行许可。

**Goal:** 完成五人家庭采集／验收预注册与可执行准备；仅在无机器人新语料时执行一次Device06同通道B诊断，独立报告而不冒充产品通过。
**Architecture:** 固定现有模型／原B助手；来源资格和身份评分分开。优先复用现有工具，必要时加最小test-only隔离适配，不改正式运行时或CLI，不生成新算法。
**Tech Stack:** 现有Go1.27.3／sherpa-onnx、已存云端公开WAV及质量审计；Mac代码／Git。

## Global Constraints

[预注册](../specs/2026-11-05-speaker-channel-design.md)全部Global Constraints与五人／19of20／0误归／陌生20×10且0误认，以及离线唯一B合同、确定性选择、角色隔离和not_run条件均为约束。直接main，无分支／worktree；所有运行只在云端。禁止新模型、新打分、已失败方法复跑、阈值择优、部署或音频策略变更。

## Task 1: 来源资格、冻结候选和五人采集操作表

**Files:** create docs/verification/2026-11-05-speaker-channel-inputs.md; private cloud E/channel-input-inventory.json, E/channel-selection.json and manifest/provenance/audit references. No Go/CLI/runtime changes.
**Interfaces:** Source existing paired/audited public records only, exactpre-regselection; output private JSON containing five development known, five diagnostic known, five development strangers, twenty diagnostic strangers; each known enrollment/query backendCase fields+boundaudit, roles/membership/source/text isolation, `source_kind=public_human`, `condition=device06`, `dataset_usage=seen_development_regression`, `diagnostic_only=true`, `new_blind_test=false`, allacceptanceflagsfalse. Schema and SHA documented in taskreport for Task2, no vectors/scores.
- [ ] Read only stored source/quality/provenance and check target new recording manifests; no SDK/speaker inference, source identity scoring or model loading. Build deterministic eligibility ledger, verify WAVheaders/hash/sample counts/audit/source/text links cloud only. If needed new quality computation requires rootapproval first, never change VAD config.
- [ ] Select exactly prereg roles in order; if insufficient emit not_run with needed/eligible counts and reasons, no arbitrary source/person substitute. Preserve all query quality failures. Write private manifests/provenance and source selection SHA, inspect code availability to recommend smallest B-only harness interface; do not score yet.
- [ ] Create sanitized inputs report with actual availability, role counts and five-person manual capture/quality/re-record checklist, two-session20query/member and200stranger acceptance worksheet; raw IDs/audio remain cloud. Commit owned reportmain, provide full evidence/report .superpowers/sdd/speaker-channel-20261105/task-1-report.md. Spec+quality gate before Task2.

## Task 2: Minimal unchanged-B diagnostic adapter, one run and product decision

**Files:** create tools/speaker-lab/channel_study_test.go only if no existing B-only path accepts selected roles; create docs/verification/2026-11-05-speaker-channel.md; update tools/speaker-lab/README.md only if newoptin, activehandofftop and planstatus. No production source/CLI edits. Task2mustreview Task1 schema before code.
**Interfaces:** consume Task1hash-boundchannelmanifest/selection and existing unchanged native worker. Use fuseEnrollment/prototypeScore/backendPoolQueries/backendCalibrate/backendRuleResult/backendMacro helpers as applicable, never run analyzePrototypeStudy/R0R1R2/P2 fullstudies. The calibration/test candidate count is5, scoring policies exactlyB/.01/grid. Output only sanitized perperson counts, quality/enrollment durations/reasons, policy and resource/cleanup metadata; diagnosis not product acceptance.
- [ ] If Task1not_run, publish dataset gap/report without building an unused adapter or running models. Otherwise specify minimal test-only strict-loader schema and interfaces in taskbrief before implementation, rootreview; cloud focused RED/GREEN for hash/role/text leakage, Bfusion/policy parity, all-denominator/not_run semantics and native child/buffer cleanup. Reuse originalnumericimplementation rather than copyingalgorithms.
- [ ] If code needed, explicit opt-in defaultsSKIP, all-or-none configuration and exclusive0600aggregate; public-only diagnostic, no private-family relabel. Cloudbuild/testonly; no broad oldstudy/native/fullsuite reruns without specificcode concern. One bounded syntheticnative check ifnewlinkage requires, engineeringmetadatafalse.
- [ ] Root first-scoregate independently checks data/roles/source/binary/model/config/hashes, code/review/focusedtestproof and shortprivateTMPDIR (socket≤100bytes). One actualDevice06Bdiagnostic, 2srequest/10sstartup/whole3600sPGbound, TERM5/KILL/wait; preserve failures/not_run, no publicreruns/newmethod or comparisons. No vectorstorage. Calibrate before holdoutdecision per originalhelper, no testfeedback.
- [ ] Publish all5known/personwise+allunknown counts/full denominators and genuine source/coverage limits. Root independentaudits and freshspec+quality, then mostcapable wholechange review; final2services/5SHA/proc proof/SSHcleanup, allcheckboxes/repo/remote close. Actualrobot5person/20query/200stranger criteria stayawaiting_recordings unless actualnewdatasetavailableandvalidated; no deployment.
