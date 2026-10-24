# Final whole-change review — stock StopWatch build foundation

Reviewed package: `foundation-full-review.diff`, range `9cbe153..634eca1`. Scope is the stock firmware build foundation, not AEC implementation or acoustic acceptance. Requirements are `docs/superpowers/plans/2026-10-21-firmware-build-foundation.md`.

## Verdict

**Code quality: APPROVED with one non-blocking documentation finding (P3).** No blocking implementation, source-preparation security, or workflow defect was identified in the reviewed package.

**Spec compliance: APPROVED for execution and artifact verification of the exact-head Linux build.** The foundation is not yet complete: plan step 7 still requires a successful stock compile and inspection/retention of the dependency, configuration, partition and firmware artifacts. The supplied status identifies run `39632225982` at `634eca1` as pending; this review does not independently establish its result.

## Prioritized findings

### P3 — Make the documented local SDK check stop the build on failure

**Location:** `firmware/README.md:34-40`, especially line 36.

The local build block uses a standalone `test` for the exact SDK commit, then runs the build without enabling failure propagation. When a user pastes this block into an ordinary shell, a mismatching SDK returns a nonzero status from `test`, but execution continues through `idf.py`, the host tests and `scripts/build.py`. This undermines the documented requirement that the local SDK checkout match the pinned commit. Likewise, failed host tests do not stop this copied sequence. Run the sequence in a fail-fast Bash wrapper/subshell, or chain the required checks and build with `&&`. The Actions workflow is unaffected: it explicitly uses the runner's Bash shell, which supplies fail-fast behavior. This is a documentation correction and is not a reason to stop the current CI run.

## Strengths

- The source URL is tied to the supplied commit and archive SHA256, and extraction follows checksum verification. Download and archive reads are streamed and bounded; regular-file declared sizes are bounded before extraction. Root, path traversal, absolute paths, link/device types and existing destinations are checked. Extraction uses the data filter in a fresh temporary sibling tree with cleanup, and preparation never executes downloaded code.
- The late gzip decoder failure is now normalized at the API boundary. Its real-archive regression test checks both controlled failure and absence of residual output. The broader tests use actual archives, file contents, preserved sentinels, download fixtures and subprocess CLI behavior rather than implementation-string assertions.
- The workflow asserts the exact SDK commit before running upstream tests/build, pins checkout/upload actions, disables persisted checkout credentials, and limits token permission to reading contents. Board directory `m5stack/stopwatch`, variant `m5stack-stopwatch`, and target `esp32s3` are consistent across the lock, workflow, README and revised plan. Prior focused review and actual variant-selection evidence support the corrected CLI selection.
- All ten required artifact paths are explicitly checked for nonempty files before upload. The upload enables hidden files for `.cache` while enumerating only the intended evidence paths. A surviving source lock alone cannot produce a misleading successful evidence upload. Source lock, resolved dependency lock, SDK configuration, app/merged images, assets, bootloader, partition table, flasher arguments and checksums are covered.
- Complete upstream MIT attribution is present without assigning a new license to this repository's own code. The README describes ignored outputs, the future tracked patch/overlay policy, private Actions retrieval, and the distinction between stock compilation and AEC/hardware validation. The reviewed diff does not alter Go services, devices, serial ports or runtime firmware behavior. The new repository instructions correctly require direct development on `main`.

## Reproducibility and security boundaries

The reviewed implementation provides fixed source and SDK identities and records the resolved build inputs/outputs. It does not yet demonstrate a successful compiler invocation or artifact bundle. It also does not promise bit-for-bit repeatability across all future container/toolchain or transitive dependency resolutions: the container is version-tagged, its SDK Git commit is asserted, and the resolved dependency lock is retained as evidence rather than replayed from a checked-in lock. These limits are consistent with this stock-baseline subplan; do not describe them as stronger guarantees.

Archive metadata/member-count or adversarial concurrent local writers are not independently bounded by a new general-purpose sandbox in this change. Normal preparation accepts the one trusted SHA256-locked archive, and the requested checks are implemented. No concrete extraction escape, destination overwrite in the documented non-concurrent usage, credential leak or private-file artifact inclusion was found.

## Evidence and review limits

- Read the plan, implementation report and packaged full change; recovered the truncated middle of the initial tool output without generating a new diff. Read prior preparation and board-selection review reports.
- Accepted the supplied prior-Linux-head evidence of 19 preparation tests and 110 upstream tests passing, followed by the old board-argument failure before compilation. The board correction is justified by the focused selection evidence. No broad suite was rerun and no local firmware compile was attempted.
- Performed focused read-only checks for ignored output safety: `firmware/.cache/`, `artifacts/` and `.superpowers/` are ignored. Read the prepared upstream AGENTS and canonical build entry only to assess the documentation/build-output boundary; canonical packaging invokes the offline IDF `merge-bin` step.
- Local `ci-last.json` still records the historical failed run `39630448069` at `69b44f9`; it is not evidence about the newer run. This reviewer did not poll remote CI or inspect successful artifacts. The controller owns the pending exact-head CI result and artifact retention.
- The shared checkout advanced to `353f715` while this review was underway. Approval applies to the explicitly supplied package ending at `634eca1`; later changes were not silently included.
- No tracked implementation, index, branch, HEAD, device, serial, server or remote state was changed. The only written file is this ignored review report. No branches or worktrees were created.

AEC, realtime capture/reference integration, natural interruption behavior, flashing, device readiness and field/acoustic PASS are not established by this review or by a future successful stock build.

## Focused follow-up — 0734591

Reviewed `foundation-doc-fix.diff` (`353f715..0734591`) and the appended implementation report's syntax/mismatch test evidence. The only changed file is `firmware/README.md`.

**P3 resolved. Code quality: APPROVED with no open findings in the reviewed foundation and this focused correction.** The full local build sequence now runs in a subshell with `set -e`, so the standalone SDK assertion and upstream tests stop the sequence on failure when used as documented. Subshell scope also preserves the interactive caller's working directory and error-handling setting. No build flags, pins or CI mechanics changed.

The reported focused verification checked the actual README Bash fence with `/bin/bash -n` and exercised a mismatching SDK with controlled stubs. It observed exit status 1, no downstream test/build sentinel, and an unaffected caller. That evidence directly addresses the original finding. No suites were rerun during this review, no implementation/Git state was modified, and only this ignored report was appended.

The earlier whole-change approval continues to apply to the package ending at `634eca1`; this follow-up additionally approves the focused documentation delta at `0734591`. It does not imply review of intervening unrelated changes. Supplied status still has stock run `39632225982` at `634eca1` running. Successful compile, artifact inspection/retention, AEC and hardware acceptance remain unestablished by this review.

## Final combined review — foundation and optional gateway metadata through 0734591

Additional scope reviewed: complete `main-additions-final-review.diff`, range `634eca1..0734591`, with `stop-reason-brief.md`, `stop-reason-report.md` and `stop-reason-review.md`. Together with the original full package, this closes whole-change code review coverage for `9cbe153..0734591`, including the interruption-metadata plan, implementation, tests, README contract and previously approved local-build documentation correction.

**Combined code verdict: APPROVED. No open code-review findings.** The prior P3 is resolved, and no new correctness, compatibility, security or test defect was identified in the additional scope. **Combined specification verdict: APPROVED for the implemented foundation and optional gateway-metadata scope**, subject to the outstanding stock-build/artifact verification already recorded. This does not approve later unimplemented firmware work.

### Additional assessment

- `internal/protocol/protocol.go:27` adds a string with `reason,omitempty`; normal controls remain wire-compatible because their empty reason is omitted. `internal/gateway/session.go:318` explicitly sets `interrupted` only after the existing interrupt guard. The stop stays before queue clearing, cancellation and reconnection, and write failure returns at the same point.
- Focused inspection of the existing `session.go:80` JSON writer and `session.go:87` TTS helper resolves the concrete helper-bypass risk: the helper only constructs a control and calls that same writer. The new direct construction preserves session ID, empty-text omission, deadline/error behavior and serialization. No hidden helper side effects were skipped.
- Wire-level interruption tests assert the reason for existing device/model/pending/capturing and tool abort/listen/model cases. Replacement-reply and normal playback tests reject the presence of the reason key, not merely a nonempty decoded value. Existing old-audio exclusion, context rebuilding, replacement playback and late-tool-result coverage remains present. The task report's behavioral RED, focused GREEN and full race/vet/build evidence is appropriate for this narrow metadata change; no suite was rerun in final review.
- The README accurately distinguishes the additional wire field from stock-firmware queue clearing and natural barge-in. Production changes are confined to the optional field and the existing interruption-stop construction; the foundation build and gateway metadata do not introduce an interaction requiring additional implementation.

### Bookkeeping and acceptance limits

The tracked interruption plan still has steps 1–4 unchecked at the reviewed head despite the implementer report documenting their completion. This is controller plan bookkeeping to reconcile, separate from the code approval and the stock foundation's still-pending build/artifact step 7. It must not be used to mark AEC or physical acceptance complete.

All reviewed work remains unflashed and undeployed. The supplied last status still identifies stock CI `39632225982` at `634eca1` as running; final CI/artifact results were not inspected here. The Go evidence is reported local verification and includes the disclosed nonfatal macOS race-link warning; it is not deployment or field evidence. AEC/reference alignment, firmware queue handling, experimental firmware, flashing/rollback and acoustic acceptance remain unimplemented or unverified as applicable.

Only the requested ignored review report was appended. No test reruns, tracked edits, branch/worktree operations, hardware changes, server deployment or remote operations were performed by this review.
