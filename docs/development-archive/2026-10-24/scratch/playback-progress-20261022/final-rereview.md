# Whole-stage independent re-review — sealed-drain P2 closed

Reviewed the complete repair increment from initial reviewed HEAD `0b5eab01526f38485f4d66b527364dda67af231e` to immutable current HEAD `3a1c3737ef4712aab63ccf1b6c309bc41551255c`, against `final-fix-review.diff`, the actual source/test changes, original `final-review.md`, repair report, updated verification document and saved validation evidence. The production repair commit is `0d114768012d5e53008dccd6907abf95c6f41c50`; the subsequent change is only the verification document. HEAD and clean working-tree state were checked. The original review report remains preserved.

## Verdict

- **Specification compliance: PASS at the source/candidate gate.** The whole-stage review's sole P2 is closed, including actual interruption and matched stop identity, rather than only adding a late snapshot.
- **Code quality: PASS at the source/candidate gate.** The additional pointer is a bounded alias of the existing latest ledger record, with explicit creation, retirement, consumption and teardown. No new important actionable regression was identified in the repair or its interaction with the previously reviewed stage.
- **Source and built candidate delivery: APPROVED.** The repaired Go source/candidate can be delivered alongside the unchanged previously verified firmware candidates. This verdict does not authorize deployment/flashing or certify field behavior.
- **Remaining important finding list: none.** No P0/P1/P2 remains from this review. There is no additional fix wave requested.

## Closure evidence

### Correct control and original-task identity survive normal transport seal

`internal/gateway/playback_progress.go:31-44` assigns `devicePlayback` only when a real negotiated playback block is created. It points to the same `playbackRecord` just appended to the bounded ledger; no new record or audio storage is introduced. Normal transport completion still seals and clears the response's active pointer, while this separate device pointer survives.

`playback_progress.go:57-79` selects the actual started active block first, preserving legacy behavior, and otherwise selects the negotiated device block while unsettled or already reported interrupted. `pausePlayback` marks and snapshots that exact selected record only when no original paused task already exists. It does not substitute the current input or a generated draft.

`internal/gateway/session.go:378-390` obtains the same target before the guard. Therefore `speech_started` no longer returns early solely because server generation/transport have finished; explicit abort and speech both emit `reason=interrupted` with the selected playback ID. The session loop serializes this selection and snapshot, so the repeated helper call cannot change target between those operations. Stop is sent before upstream reconnection. After successful stop, `session.go:400-405` clears both active and device ownership, preventing repeated selection of the old block. If the send fails, the existing error path ends the session and its deferred cleanup clears the pointer.

This closes all three original effects: absent paused original task, identifier-less forced stop, and the skipped non-force speech interruption.

### Completion, old/new records and task mutation remain conservative

`playback_progress.go:112-123` retires only the matching pointer on a settled non-interrupted final. Thus a normally completed block cannot be revived by later abort/speech, an older final cannot retire the newer device block, and invalid progress alone does not falsely declare hardware drain. Interrupted final may precede its control event: the existing identity remains selectable until the actual interruption consumes it. Receipt processing still does not independently manufacture a paused task.

After interruption consumes device ownership, late final only updates the existing ledger and an identity-matching paused snapshot; it never assigns `devicePlayback` again. This also protects discard/replace while the original final is still outstanding. For a completed interrupted side block awaiting its control event, `playback_progress.go:224-227` retires that finished owner after a successful explicit task mutation. Task-ID checks and actual-current-STT validation execute before this retirement, so invalid mutation attempts do not change ownership.

A newer actual block replaces the pointer with the newer ledger record. Existing paused original work still wins over a side answer's new interruption. Normal complete history promotion, permanently invalid positions, one paused task, and unknown text position remain unchanged.

### Automatic listening and bounds

The `onDevice listen.start` trigger condition is unchanged; it does not treat `devicePlayback` alone as an interruption reason. The firmware's normal-stop automatic listen transition therefore does not cancel its own remaining device drain or create a paused task. Actual upstream speech still uses the repaired interrupt path.

`devicePlayback` can only be nil or the newest existing ledger entry, which is replaced on the next real block. It cannot retain an evicted seventeenth record. The strengthened ledger test checks 20 created blocks, 16 retained records and pointer equality with the last retained entry. Device session teardown clears the added pointer; upstream reconnection retains it only as appropriate to the existing device lifecycle. There is no added unbounded container, audio retention, worker, network service, logging payload or ASR.

## Regression evidence assessed independently

Read every added case in `internal/gateway/playback_drain_test.go` and the updated bound assertion. The tests exercise real WebSocket transport, the session event loop, original committed/transcribed input, normal TTS seal, actual abort/speech entry, emitted stop identity, model reconnect and local task tools. They do not merely mutate ledger fields and inspect a helper.

Coverage includes both interruption entry points across no final, interrupted final before/after, invalid progress, and invalid interrupted final; normal settled completion; old final versus a new block; side interruption preserving the original paused task; and discard/replace both before and after final settlement. The assertions explicitly check matched interrupted stop ID, original request, sent and played samples or unknown position, settlement, and `text_position=unknown`. Device receipt → mode-change → model `session.update`, or receipt → abort → emitted stop, supplies causal ordering rather than a timing sleep. The normal-listen barrier also guards against canceling a sealed block merely because listening resumes.

The original independent overlay remains unchanged. Its saved repaired execution `/tmp/playback-final-fix-overlay-green.log` is `ok familyrobot/internal/gateway 2.309s`, matching the repair report's red-to-green account. The added formal tests check the broader actual-stop acceptance that the original minimal overlay did not cover.

No passed unchanged suite was rerun in this re-review, and no new focused reproduction was required: inspection of the actual repair and test code resolved the original finding without a concrete new unresolved hypothesis. Saved final macOS race output and vet log were read directly; race passed with the same disclosed Darwin cgo linker warnings, and vet produced no diagnostics. These are execution evidence from the implementation run, not tests newly executed by this reviewer.

## Candidate identity and retained verification boundary

The new Linux evidence was inspected at `artifacts/device-playback-20261022/go-0d114768012d/verification.json` and `linux.log`. It identifies source `0d114768012d5e53008dccd6907abf95c6f41c50`, return code 0, undeployed status, Go 1.27.3/libopus 1.4, successful full package race results, and the candidate binary hash. The controller's complete race/vet/build evidence remains associated with this repaired revision, not the older Go candidate.

- Source archive SHA256: `9641fc3b19fac0bf31158751b49aac2697302e7fe69758cd0d7ebf74bcab407e`.
- Repaired Linux binary SHA256: `0a12865dd4b4298467d0ba9a695d1fc6566086774e4c3254be8969bcd680dca2`.
- Git confirms firmware tree `ed32846d9c8d47f258c184adb7f7a7bf8ab243b7` is identical at current HEAD and built firmware source `17bb1e001f9f25926071a6a8871426ae52235e4a`. This Go-only repair does not require a different firmware candidate.
- The final verification document now names the repaired Go source, archive and binary, and describes the sealed-drain correction without asserting deployment or physical success. Its pending-review sentence and plan review checkbox can now be finalized using this report.

The initial whole-stage review's unchanged assessments of wire agreement, DMA ownership and AEC-before-refill order, fixed storage/ISR constraints, fallback, mapping oracle and documentation scope continue to apply. No firmware source changed in this repair.

Physical device receipt timing, actual stop/drain behavior, AEC/voice/tools on the device, target heap and stack high-water marks, ISR duration/cadence under load, acoustic latency and intelligibility still require the separately authorized field window. Compile success and host simulations do not make those PASS. Exact word-level continuation remains unverified and production text position remains unknown. This re-review performed no device/serial/production access, deployment, flashing, recording, product edits, Git index changes or commits; only this ignored review report was written.
