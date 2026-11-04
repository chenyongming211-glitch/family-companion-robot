# Speaker Backend Study Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. User requests main only, no worktrees.

**Goal:** Execute B / AS-Norm / low-dimensional QMF with new Mandarin small-cohort testing and honest old50 development comparison.
**Architecture:** Add isolated backend-study, reuse guarded extraction/audit/fusion. Manifest, math and analysis units own data. Controller freezes actual inputs before two model runs.
**Tech Stack:** Go1.27.3, sherpa-onnx Linux v2.0.1, existing base512/Silero; no new dependencies.

## Global Constraints
- 依据[设计](../specs/2026-11-05-speaker-backend-design.md)执行R0/R1/R2；用户选择普通话小规模独立测试，旧50仅开发／回归。
- main直接提交推送，无branch/worktree。音频、向量、权重、凭据、原始JSON不进Git或Mac。
- Go／原生／运行测试和素材处理仅在云端，Mac仅源码／Git；正式模型、会话阈值、登记库、VAD／AEC、服务、固件、麦克风不改。
- 身份评分前冻结来源／清单／规则；旧CLI边界和JSON不变，质量失败及not_run真实分母保留。
- train10／validation50已知5陌生／cohort30／test最多5已知20陌生人物互斥；top-k10，新分数独立标定。

### Task 1: Implement and review backend-study
**Files:** create tools/speaker-lab/backend_manifest.go, backend_math.go, backend_study.go and corresponding _test.go; modify main.go, README.md only. Reuse loadBoundAudit, boundedFile, strictHeader, extractEvaluation, fuseEnrollment and timing/quality types.
**Interfaces:**
~~~go
type backendCase struct { ID, Speaker, Role, Path string; Samples int; SHA, RecordingSHA, SourceGroupSHA, TextSHA string }
type backendAuditRef struct { Path, SHA string }
type backendManifest struct { SourceKind, SourceID, SourceURL, LicenseURL, SingleSpeakerBasis, Condition string; SampleRate int; Audits []backendAuditRef; Cases []backendCase }
func loadBackendManifest(path string) (backendManifest, map[string]auditCase, string, error)
func runBackendStudy(ctx context.Context, model, sha, manifest string) (backendReport, error)
~~~
- [ ] Cloud RED strict-schema/bounds/audit/config/SHA/samples/role/sourcegroup/text/20s-first-crossing tests; independentCLI/old limits unchanged.
- [ ] Cloud RED hand-computed AS-Norm/top-k/populationstd floor, fusion, tied and singlecandidate, wrappinggroups/count-once; full/qualified/not_run counts; threshold/margin/budget/zero-wrong tieobjective; train-only standardization/stable loss/fitting failure; R2 rankinvariance/privacy.
- [ ] GREEN exact design. Stable softplus and deterministic regularized fit:
~~~go
func backendSoftplus(x float64) float64 {
    if x > 0 { return x + math.Log1p(math.Exp(-x)) }
    return math.Log1p(math.Exp(x))
}
// top10/populationstd floor1e-6; features AS-Norm/log1p(VADseconds), train-standardized.
// Loss class-balanced logistic + .01/2*(wScore*wScore+wDuration*wDuration), interceptfree.
// max2000, objective delta1e-8, step1/backtracking50; explicitnot_run on fitfailure.
~~~
- [ ] Cloud GOMAXPROCS2 ordinary/race go test -p1 -json -count1, vet/build; focusedRED/GREEN logs and source/binarySHA, no unnecessary repeatednative/oldfullruns.
- [ ] Main commit limitedfiles, private report commands/counts/hashlineage; independent spec+quality review, one consolidatedfixwave and coveringtests ifneeded.

### Task 2: Source freeze and independent review
- [ ] Full officialstream MD5/bytes, actualWAV and signal-only audits; source-order first<=5newknown/20newunknown/30background; oldS10train/50+5validation transferred.
- [ ] Freeze two backendmanifests and anonymous actual role/count/duration summary with SHA; independent source/WAV/sample/quality/text/paireddevice/70-old exclusion/role-disjoint review.
- [ ] Record actualtestN before firstidentityscore. Old50developed-on/backgroundcontentunverified/validationunknown5 disclosed; sourcefailure notsilentlydowngraded.

### Task 3: Run, compare and publish
- [ ] Pin reviewedbinary/source/model/manifests/audits; two sequential bounded1200s commands onceeach, preserveexit/stderr/failures, no outcome-basedretry.
- [ ] Check totals/rates/groups/fittedbudgets/ranks/R0legacy50/resource scopes and protectedservices; reclaimprocesses.
- [ ] Report smallindependenttest separatelyfromold50development and previousB onlymatchedinputs, actualdenominators/uncertainty; no unseen50 or productionclaims.
- [ ] Whole-change independentreview, closefindings, handoff/spec/planupdate, mainpush/exactremote/cleancheckout, temporarySSHclose.
