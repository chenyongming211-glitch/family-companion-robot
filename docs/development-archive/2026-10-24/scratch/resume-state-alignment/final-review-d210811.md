# Final fix re-review at d210811

Scope: complete P2 correction `5ee8d4b..c7bd0a257bf333f315090499c6cf87620321c767`, plus comment-only `d210811`; supplied `review-final-fix-5ee8d4b-current.diff`, current relevant source, `final-fix-report.md`, and retained RED/GREEN/full-check logs. This supplements `final-review-5ee8d4b.md` and resolves its only material finding. No source/index/Git mutation, remote/provider/device/audio action or test-suite rerun was performed. This report is the only write in this re-review.

## Final verdict

**Spec compliance: approved for the reviewed source scope. Code quality: approved. No unresolved Critical or Important finding.** The input/ACK crossover and its token-reuse variant are corrected. The earlier final review's task identity, isolated classifier, cancellation, alignment evidence boundaries, cadence/wake/control and bounded-state assessment remains applicable.

## Why the P2 is resolved

- `internal/gateway/playback_progress.go:208-223` centralizes new committed-input invalidation. It cancels old classification, removes old voice authorization and sync state, and preserves `sync.sequence`. The model client's ACK FIFO remains intact, so obsolete updates consume their own slots without reusing the new input's token. The old conditional reset in `session.onModel` is removed rather than retained as a competing path.
- `internal/gateway/generation_sync.go:73-80` captures connection generation and committed item with each new snapshot. ACK handling at `:82-110` first consumes the FIFO token, accepts only the current token, then checks current input/generation and active routing before requesting voice. It preserves the existing latest-instructions and pending-settlement checks. Thus an obsolete ACK cannot authorize a newer route whether it arrives before transcription, during classification, or after the replacement snapshot has been sent.
- Reconnect still replaces the client/FIFO and resets generation sync together, making reset on a new connection distinct from input-only retirement. Mode updates retain token zero and cannot unlock generation. The comment-only change at `internal/qwen/client.go:143-145` correctly documents retired snapshot slots and the current-token rule without altering runtime behavior.

## Regression and evidence quality

`TestIsolatedRouteSnapshotACKCannotReleaseReplacementInput` covers four schedules: ACK before replacement transcript, during classification, after the new snapshot, and after another replacement cancels an in-flight classifier. The side/side variants intentionally preserve identical instruction text, so passing cannot depend on accidental snapshot-string differences. Assertions verify strictly newer tokens, quiet main wire before the correct ACK, original-goal retention, latest input, task binding and successful response/playback admission after the current ACK. The cancellation fixture waits for its worker to return, and obsolete results pass through the real result-token filter.

`TestResumeStateReplacementInputRequiresOwnSnapshotACKWithoutPausedTask` separately covers ordinary auto/manual inputs with a mode token-zero ACK and an obsolete input ACK before the current snapshot ACK. These checks protect both classifier and ordinary response paths.

Inspected actual retained outputs rather than relying solely on the report:

- `final-fix-red.log`: exact old production source via overlay fails all six subcases, including premature `response.create`, inability to send replacement snapshot, and token reuse; gateway 21.523 s.
- `final-fix-green.log`: same tracked regression set repeated 20 times passes; gateway 3.288 s.
- `final-fix-focused.log`: isolated route/resume/wake/manual/device-tool/cadence group passes; gateway 22.476 s.
- `final-fix-ordinary.log`: full Go suite passes, changed gateway suite 38.485 s; unchanged packages may be cached.
- `final-fix-race.log`: full race suite passes, changed gateway suite 41.497 s, no Go race finding. Existing macOS C/Opus LC_DYSYMTAB linker warnings remain explicitly visible.
- `final-fix-vet.log`: zero bytes, consistent with the reported successful vet run.

These logs are implementation-worker verification evidence inspected by this reviewer; no independent full rerun was warranted after the focused source re-review. The d210811 delta is documentation only.

## Remaining controller-owned gates

At assignment, root was running exact-d210811 Linux validation/build. Final native ownership/delayed-final/normal/manual-abort cases remained pending, and no deployment had occurred. This source approval does not establish those outcomes or authorize representing them as complete. Retain the exact candidate identity and distinguish deterministic build/tests, native synthetic protocol evidence, deployment health/hash and physical field acceptance. Exact text position, calibrated device alignment and acoustic hearing proof remain unestablished by this execution.
