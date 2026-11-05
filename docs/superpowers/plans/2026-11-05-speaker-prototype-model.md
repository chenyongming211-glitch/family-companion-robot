# Speaker model and enrollment prototypes implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement task-by-task. Steps use checkbox syntax. User explicitly authorized continuing optimization and tests; main only, no branches/worktrees.

**Goal:** Compare three fixed model pipelines and single versus two enrollment prototypes on the frozen old corpus, honestly reporting acceptance, rejection and strangers.
**Architecture:** Reuse strict backend manifest, worker extraction and backend calibration helpers. Add bounded metadata preparation and a separate prototype-study command; no production integration.
**Tech Stack:** Go existing Linux module/sherpa2.0.1, cloud-only Python ONNX for metadata prep and synthetic contract tests, CPU1thread; no new Go dependency.

## Global Constraints

- Main directly, root coordinates pushes after review. Mac code/Git only; ALL tests/builds/native/audio/model processing/dependencies cloud-only.
- No formal services/model/session thresholds/env/firmware/VAD/AEC/microphone changes, no vendor registration/outreach.
- No raw audio/model/runtimeJSON/vectors/binaries/credentials in Git; owned vectors cleared, RAM only.
- Fixed3models and2template methods; no query accumulation/AS-Norm/QMF/test tuning, no post-score candidate selection.
- Data seen_development_regression, new_blind_test=false; synthetic_engineering_fixture with unavailable open-set assessment; all acceptance/feature flagsfalse.
- Reuse all971cases/condition and all quality rejects; per-condition/method/model independent1%validation calibration, existing margin grid and three pool shapes.
- Source/data/model/binary preregistration and hash freeze before public scoring; six once-only sequential runs,3600s process-group timeout thenTERM5s/KILL/reap; preserve failures/amendments.
- Actual frontend differences are disclosed; metadata adaptation is not strict official WeSpeaker feature equivalence. No SDK rebuild/second scoring path.
- Exact algorithms and model hashes in ../specs/2026-11-05-speaker-prototype-model-design.md. Physical collection and domain adaptation training remain unperformed, guide only.

## Task 1: Verified model preparation and bounded lab loading

**Files:** create tools/speaker-lab/prepare_wespeaker.py and prepare_wespeaker_test.py; modify assets.lock.json, worker.go; add focused worker/model-bound tests in new model_assets_test.go.
**Interfaces:** preparation CLI --input ABS --output ABS --sha256 SHA --bytes N --language English|Chinese --url PINNED_URL; input/output remain cloud-only. Output JSON raw/model/graph SHA and sizes/metadata, no weights. Retain onnx graph fingerprint equality. Go maxModelBytes = 128 * 1024 * 1024; requestTimeout/startupTimeout unchanged.

- [x] Download only fixed2candidate ONNX on cloud, pin official HF commit and verify officialLFS hash/bytes before parsing; mirror transport only. Preserve model cards/config and fixedsherpa/frontend sources; do not infer productlicense.
- [x] Cloud TDD for preparation: synthetic valid ONNX, wrong hash/size, external data, input/output contract, duplicate/conflicting metadata, no graph/weight alteration, output overwrite policy and deterministic adaptation. Exact preparation keys are in design; use onnx library in project-private Python env only. A successful adaptation returns derivedSHA recorded in assetslock before anypublicscoring. No arbitrary downloads in helper.
- [x] Model size contract: newnative asset exceedsold100MiB but fits128MiB; verify oversize rejects before reading, existing no-symlink/wrongSHA rules retain. Change only boundedsize constant in worker, not timeouts/protocol. Cloud focusedRED/GREEN and moduleordinary once. Build temporarybinary and probe fixedsyntheticfixture (notpublichumans), bothmodels dimension256, finite vector and proper cleanup under2srequest/10sstart. Ifincompatible record and notifyroot; don't silentlychangebudget.
- [x] Self-review, commit onlyownedcode/tests/assets entries, attach exactcommand/output/exit and raw/derivedgraph/modelSHA/provenance; task reviewer spec+quality. No publicscoring yet.

Task1 complete33ee122 + test-only7f133e6; independent spec/quality review clean. CloudPython9pass,Goordinary391pass10opt-in skip,focusedguardpass,both256dsyntheticnativepass. Root independently read logs/rawassetSHA and parsed391/10/0 counts; no public candidate inference.

## Task 2: Deterministic prototypes and comparison CLI

**Files:** create prototype_math.go, prototype_math_test.go, prototype_study.go, prototype_study_test.go under tools/speaker-lab; modify main.go only for explicitCLI.
**Interfaces:**
```go
type prototypeSummary struct {
    Speaker, Status, Reason string
    Cases, Iterations int
    ClusterSizes []int
}
func buildEnrollmentPrototypes(clips []evidenceLabeledVector, dimension int) ([][]float32, prototypeSummary, error)
func prototypeScore(query []float32, prototypes [][]float32, dimension int) (float64,error)
type prototypeReport struct { /* anonymous metadata, two methods, pools, coverage, extraction/resources */ }
func analyzePrototypeStudy(ctx context.Context, m backendManifest, a map[string]auditCase, cases []embeddedCase, dimension int) (prototypeReport,error)
func runPrototypeStudy(ctx context.Context, model,sha,manifest string) (prototypeReport,error)
```
Consumes evidenceLabeledVector, validVector, similarity, fuseEnrollment, backend pool/calibration/scoring/ROC/metadata, strictloader/extractor contracts already in module.

- [x] Cloud RED tests before implementation for inputpermutation/ties, two separatedmodes with >=2each, <=3segments/singleton/identical fallback exactB,10iterationcap, noquerytruth, negative/NaN/Inf/dimension/mixedperson/duplicateID, independent ownership and reset/error clearing. Test handcomputed directions: [1,0],[1,.01],[0,1],[.01,1] yields2prototypes; query[1,0] maxscore must exceed all-mean cosine. Fallback must equal originalfuseEnrollment, not normalized-again alteredmean. Use finite assertions.
- [x] Implement design's sortedID farthestpair + <=10 deterministiccosine-kmeans, min2members each, originalallclips fallback. Allreturnedvectorsowned; borrowedinputsunchanged; cleartemporarycenters/copies onallreturns. Score maxcosine, validate all vectors.
- [x] Integration RED for explicitCLI/fixederrors/noecho; extractiononce; two methodnames B_single_template/P2_two_prototypes; strictmetadata/public vs synthetic; all3pools inclusivequality/unknown denominator; originalB R0 parity; fit/calibrate usingvalidation only and prototypeconstruction enrollmentonly; diagnosticscoverage no path/vector serialization; cancellation/cleanup. Use existing numericfixture helper ifappropriate, without public-human claims.
- [x] Implement two-method analysis, wrapper and CLI. Build methodtemplates perperson only, compute candidate scores from matchingrolepopulation, then backendPoolQueries/calibration. No new calibration optimizer; unknown target .01 and margins[0,.01,.030229,.05,.1] literal fixed. CLI invalid/missing/relativeargsusage, runtimeprototype_study_failed. No sourcekind override for synthetic internal.
- [x] FocusedGREEN, cloudmoduleordinary/race/vet/build once on final runtime. Commitownedfiles; exactTDDreport; taskreviewer gate before public data.

Task2 complete666aec1; independent spec/quality gate clean, nofindings. Cloudordinary417pass10skip,marker-amendedrace418pass10skip0fail/noRaceReport,vet/build0. Failedinitialrace and state-based fixturefix preserved; runtimeunchanged duringtesthardening. Rootreadlogs andcompared5cloud/Macsourcehashes,allmatch. Native/public effect checks Task3.

## Task 3: Native proof, frozen experiments and honest report

**Files:** create prototype_study_native_test.go, update tools/speaker-lab/README.md; create docs/verification/2026-11-05-speaker-prototype-model.md; update docs/DEVELOPMENT_HANDOFF.md and planstatus.
- [ ] Add explicit TestRealPrototypeStudyOptIn using model/sha/binary/syntheticfixture inputs. Genuineworker extraction into internallytagged syntheticstudy. Require actual >=1two-prototype identity (notlogonly), twomethod layout, synthetic/unavailable/nonacceptanceflags, denominator/parity/fallback/ownership/privacy and actualprocess/socket cleanup. Defaultskip, no publicloaderrelaxation. Runnewoptin explicitly for all availablefixedmodels, do notrerunoldoptins.
- [ ] Freeze reviewedruntime source/binary/modelsraw+derived/manifests+audits/role/sourcebinding and algorithmconstants. Copyoldfrozeninput audit/reference aftercheckingnewhashes; source41 set expands withnewfiles. CheckallWAV sha/samples/format and no datasetselectionfromscores. Resourcepreflightspace/memorybeforesequentialruns.
- [ ] Execute eachmodel/condition exactlyonce sequentially using prototype-study. For currentbase the newP2 requires fresh sharedextraction; thisreason recordedbeforeinference. Capturestdout/stderr/exit/config/code/input/resultSHA, processgroup3600s timeoutTERM5KILLreap and childCPU/RSS. Nooverwrite or hiddenreruns. Unavailablemodel yields recordednot_run ratherthanfilteringpersons orwrongclaim.
- [ ] Independently compare baseB shared3poolsTop1 and complete1%rule objects withsavedR0; allmethods inclusivecounts/macros/group/people, templatecoverage/fallbackcounts, scoreaccounting/inputquality. Report3×2×2×3allpoolrows, notjustwinner. Unknownbudgetempirical notpopulationguarantee. Fullknowncorrect/wrong/reject &unknownFA/reject, perpersonmacro, native/wallP50/P95/CPU/RSS/coldstart. State helps/harms/noeffect/noncompatible with seenregression limits.
- [ ] Document exact runnablecommands, fixedassets/frontenddifference, initial/failed/amendedevidence and fullresults plusactualdeviceenrollment/crossdeviceguide. No falsephysicalcapture/domaintraining claims. No deployment onoldregression. Gettaskreview thenmostcapablewholechange review; closefindingswithfocusedtests only, keeporiginalfrozenprovenance.
- [ ] Root final2service/5SHA/processcleanup proof, cloudSSHclose, scopedledger/planhandoffclosure, mainpush/remoteHEAD/cleanverification. Final response linksreport and candidcomparison; no production/fieldPASS.
