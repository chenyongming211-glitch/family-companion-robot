# Memory relevance implementation plan

> Use subagent-driven-development; user authorized direct main work and continuation. No branch/worktree or repeated approval.

Goal: current-session context stays available; only necessary selected prior-session evidence reaches A/B/C reply models.

Architecture: shared read-only selector using existing Native function-call plumbing; storage and playback ownership unchanged.

Global constraints: cloud-only runtime/tests/build/deploy; direct main; Waveshare only; no credential/audio/artifact Git; no new dependency/service; no mock product; keep existing records and device/family ownership; no B TTS/firmware/frontend changes.

## Task 1: Shared semantic memory selection and integration

Files: new internal/realtimelive/memory_selection.go and tests; memory.go/session.go integration; identity.go guidance and relevant tests. Reuse Native.completedControlCalls and controlHistory projection rather than copy protocol parsing. Existing raw mergeMemory storage tests can call that projection explicitly; new reply tests must assert final selector-filtered prompts.

- [ ] Add cloud failing tests for selection contract, exclude unselected old text, retain current context, bounded recall/continue, unknown/malformed/duplicate/out-of-range selections, cancellation/clear races and provider failure.
- [ ] Implement `MemorySelectionInput`/selection result with none/recall/continue and max3 unique integer candidate indices; bounded old candidate evidence and current request. One read-only function tool, no hardware tool authority. 3s shared limit; Native audit preserves usage.
- [ ] Remove unconditional prior-memory merge from settle and include only selected evidence through routeMemory. Revalidate after network return under memory lock, so a clear invalidates stale choices. No selector call with no memory, no candidates, explicit clear, or missing transcript. Soft failure omits old material and continues normal reply.
- [ ] Keep current session playback evidence, storage/clear and authentication unchanged. Selected old excerpts keep timestamps/source and cannot become an answer template. Reject whole invalid plan rather than accepting fabricated indices; do not allow unknown fields/duplicates/noninteger IDs.
- [ ] Cloud targeted normal/race/vet/build. Adapt existing tests that asserted unconditional inclusion to distinguish storage projection from reply behavior. Save initial failures and final command/results.
- [ ] Commit source/test changes on main and report exact files/tests/limits. No service or provider calls in Task1 except test fake servers on cloud.

## Task 2: Root verification and delivery

- [ ] Review Task1 spec and quality from exact diff/report.
- [ ] Cloud actual-source copy of sealed deployed225-file source; apply only this task's production changes and required tests, preserving undeployed differences.
- [ ] Limited real selector checks: new story→none, current-session continuation→none, historical recall→necessary records, historical continuation→necessary records; then full selected-prompt story generation using existing synthetic input. Record usage/elapsed and failures without automatic retries.
- [ ] Exact source/dependency/artifact proof and cloud related normal/race/vet/build; independent whole-change/source/release review.
- [ ] Fresh Voice-only transaction (never reuse prior consumed CLI), preserve management/config/records/permissions/audit/Caddy/firmware, check idle and readiness.
- [ ] Update handoff/results/remaining field limits; commit/push main and verify clean/ahead0/behind0.
