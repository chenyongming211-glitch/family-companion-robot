# Speaker evidence implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Follow cloud-only TDD and task spec/quality reviews; user approved direct main development.

**Goal:** Deliver enrollment consistency diagnostics, bounded query-vector accumulation and a four-arm offline replay study, without formal-service changes.

**Architecture:** Reuse the isolated Linux `tools/speaker-lab` module, existing strict backend manifest and worker extraction. Keep diagnostic fitting/stateful accumulation independent of study orchestration; expose a new explicit CLI command. Four-arm results preserve diagnostic-only score equivalence and label old data as seen regression.

**Tech Stack:** Go1.25+, existing sherpa-onnx Linux2.0.1, existing pinned ERes2Net-base. No new dependencies.

## Global Constraints

- Develop, commit and push directly on `main`; no branches/worktrees. Root coordinates pushes after review.
- Runtime tests/dependencies/builds/audio processing only cloud. Mac code/Git only. Existing SSH master may be reused; no credentials in code, reports or Git.
- No formal model/session thresholds/services/firmware/VAD/AEC/microphone changes. No vendor registration/outreach.
- WAV/model/runtime JSON/biometric vectors stay out of Git; vectors RAM-only and cleared. Reports contain no paths/names/text/vectors.
- Enrollment diagnostic never filters or changes the B template; baseline and diagnostic-only identity results are identical.
- Accumulator sees no speaker labels; all-pairs continuity guard, no future inputs, fixed 8-second audio/4-fragment/1.5-second gap/12-second span bounds.
- Old data is `seen_development_regression`, `constructed_sequence`, `new_blind_test=false`; overlap detection and household/production acceptance remain unavailable/false.
- Exact algorithms, ordering, thresholds and error/ownership rules are in the approved [design](../specs/2026-11-05-speaker-evidence-design.md).

## Task 1: Pure diagnostic and accumulation algorithms

**Files:** create `tools/speaker-lab/evidence_math.go`, `evidence_accumulator.go`, corresponding `_test.go`. Do not change existing algorithms.

**Interfaces:** produces these package-private contracts for Task2 (JSON names can mirror field names in snake_case; vector fields must be `json:"-"`):

```go
type evidenceLabeledVector struct { Speaker, ID string; Vector []float32 }
type evidencePolicy struct {
    DiagnosticStatus, ContinuityStatus string
    DiagnosticFloor, ContinuityFloor float64
    EnrollmentPeople, QueryPeople, EnrollmentSamePairs, QuerySamePairs, QueryDifferentPairs, QuerySamePassing int
}
func fitEvidencePolicy(enrollment, queries []evidenceLabeledVector, dimension int) (evidencePolicy, error)
type evidenceDiagnostic struct {
    Speaker, Status, Recommendation string
    Cases int
    SuspectCaseIDs []string
    SegmentMedianSimilarities []float64
}
func diagnoseEvidenceEnrollment(speaker string, clips []evidenceLabeledVector, dimension int, policy evidencePolicy) (evidenceDiagnostic, error)
type evidenceChunk struct {
    Stream string
    ID uint64
    Start, End int64 // 16kHz sample coordinates
    Qualified, Overlap, Discontinuity bool
    Vector []float32
}
type evidenceAccumulated struct {
    Vector []float32 `json:"-"`
    Samples, Chunks int
    Reason string
}
type evidenceAccumulator struct { /* private, owned state */ }
func newEvidenceAccumulator(dimension int, continuityFloor float64) (*evidenceAccumulator, error)
func (a *evidenceAccumulator) Push(chunk evidenceChunk) (evidenceAccumulated, error)
func (a *evidenceAccumulator) Reset()
```

- [ ] Write cloud-failing tests before implementation for: trained floors/nearest-rank quantile; insufficient/invalid training; per-segment median/suspect IDs/no input mutation; same-speaker weighted evidence; non-collinear drift blocked by all-pairs guard; exact/over limits; stream/gap/span/overlap/discontinuity/quality resets; duplicate/retrograde IDs/times, NaN/Inf/cancellation; independent input/state/output ownership and zeroing.
- [ ] Execute focused RED on cloud; save expected feature-missing evidence under this run's `evidence/`. Implement only the specified algorithms and tests. Concrete fusion/check rules:

```text
diagnostic_floor = sorted_same_enrollment_cosines[max(0,ceil(.05*n)-1)]
continuity_floor = Nextafter(max_different_query_cosine,+Inf)
append_allowed = every retained cosine(new,old) >= continuity_floor
fused = normalize(sum(samples_i * normalize(vector_i)) / sum(samples_i))
over audio128000 / count4 / gap24000 / span192000 => restart from current
bad IDs/times/vector => clear state and errProtocol
overlap or unqualified => clear state, no vector; no identity fallback
```

- [ ] Execute focused GREEN then existing module ordinary tests once; no Mac Go execution. Format/review ownership and numeric assertions, stage only task files, `git diff --cached --check`, commit. Report RED/GREEN commands and exact results. Task reviewer checks spec and quality before Task2 starts.

## Task 2: Four-arm study and explicit CLI

**Files:** create `tools/speaker-lab/evidence_study.go`, `evidence_replay.go` and `_test.go`; modify `main.go`, add relevant CLI tests to new test file. Consumes all Task1 interfaces; reuse `loadBackendManifest`, `extractEvaluation`, `fuseEnrollment`, `backendPoolQueries`, `backendCalibrate`, `backendRuleResult`, `backendTop`, `backendElapsed`.

**Interfaces:**

```go
type evidenceReport struct { /* anonymous metadata, policy, diagnostics, arm/scenario/pool scores and resource summaries */ }
func runEvidenceStudy(ctx context.Context, model, sha, manifest string) (evidenceReport, error)
// New CLI: speaker-lab evidence-study --model ABS --sha256 SHA --manifest ABS
// Invalid/missing args => {"error":"usage"}; run errors => {"error":"evidence_study_failed"}.
```

- [ ] Write and run cloud RED for explicit CLI opt-in/fixed-error/no echo, deterministic paired/alternating ordering (known/unknown interleaved, each query once), future/truth independence, reset stream per candidate group, four named arms, diagnostic score equivalence, training-only fitting, rejection-inclusive denominator, unsupported/disabled policy fallback, cancellation, no biometric/path serialization.
- [ ] Implement extraction exactly once with unchanged RMS qualification; reuse all original enrollment vectors for templates. Fit only designated train role subsets, diagnose each enrollment person once. Sequence input uses only anonymous vector/timing/eligibility; labels stay in scene-construction/scoring structures. Pair burst size2 / alternate size1, gaps4000 samples; follow design's numeric person order with ID order within person, repeated rounds until exhausted.
- [ ] Implement scoring and calibration independently per arm/scenario/pool, using original backend pool shapes and margin grid/1% unknown target; include existing two fixed R0 rules on baseline validation50 for semantic parity with saved old results. Explicitly mark constructed time/voice evidence and unavailable true overlap/field/latency. There is no sleep or waiting for future audio in this study.
- [ ] Run GREEN and affected CLI/replay/math tests, module ordinary/race/vet/build on cloud; include meaningful adversarial label-independence and baseline-parity assertions. Commit task files only, attach code/test evidence, get task spec and quality review before Task3.

## Task 3: Native validation, regression run and handoff

**Files:** add opt-in native test to `tools/speaker-lab/evidence_study_test.go`; update `tools/speaker-lab/README.md`; create `docs/verification/2026-11-05-speaker-evidence.md` and update `docs/DEVELOPMENT_HANDOFF.md`. Code tests follow same cloud RED/GREEN rule; docs include explicit executable commands and current results.

- [ ] Add opt-in `TestRealEvidenceStudyOptIn`, controlled only by explicit synthetic fixture/model/binary inputs; test genuine native worker extraction→diagnostic/accumulation→anonymous JSON→child cleanup, four-arm equivalence and fixed metadata. No public-humanness claims for synthetic inputs. Preserve all default skips of old opt-in tests.
- [ ] Run final module ordinary/race/vet/build once for final changed runtime source, new opt-in separately. Freeze module source, binary, input/audit/model SHA and experiment constants after code review. Reuse prior public frozen Device03/Device06 manifests without selecting new cases based on results. Verify source bindings/sample counts/role disjointness/input SHA and that all previous persons are seen regression.
- [ ] Execute each public condition once, sequentially, whole process group TERM/KILL timeout1200s and reap. Save stdout/stderr/exit and resource metadata. Do not repeat a successful model run for prettier numbers. Program/error fixes require a recorded amendment and both pre/post evidence.
- [ ] Compare new baseline counts and fixed rules to saved prior R0 results (no rerun old completed study); summarize all four arms and both orders, all denominators, reset/merge coverage, unknown accepts and timing. Diagnostic-only arms must be exactly identity-equivalent. State if accumulation helps, harms, disables or cannot be concluded; no effect claim from test counts alone.
- [ ] Write a concrete participant guide: quiet space, robot-microphone enrollment / separate phone-to-robot condition, multiple sentences totaling about20 seconds, obey per-clip diagnostics, separate independent query sentences and strangers, manual stop; record unavailable actual robot capture and new holdout, do not open mic. Verify formal2PID/5SHA unchanged and no analysis processes, close temporary SSH after all cloud work. Complete scoped ledger and plan, get task review and broad final review, commit/push main and verify remote HEAD/clean checkout.
