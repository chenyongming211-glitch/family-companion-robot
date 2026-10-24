# Final review P2 fix — input-scoped snapshot acknowledgement

Status: COMPLETE source/test fix. Commit `c7bd0a257bf333f315090499c6cf87620321c767` on `main`, parent `5ee8d4b312bdeb8eb1fd0a0f0a6a5c2c4fef79ca`. No push or remote/provider/server/device/audio operation. Only five owned gateway Go files committed. Root-owned documentation changes remain unstaged and untouched.

## Root cause and correction

A newly committed input canceled the accepted route but retained the old generation snapshot. Its ACK could compare equal instructions and request voice while replacement STT/classification was pending. Separately the active-route cancellation path reset the snapshot sequence; an already-enqueued old ACK could collide with a replacement token.

`commitOrigin` now invalidates the prior generation sync and voice authorization at the input transition while preserving its monotonic sequence. It leaves the model client's update FIFO intact: an obsolete ACK consumes its original slot and cannot match a replacement token. The special reset in `onModel` was removed so active and accepted routes share this transition. Each sent snapshot additionally captures upstream generation and committed item identity; ACK processing rechecks those and active routing before releasing `response.create`. Existing instruction/revision refresh, receipt settlement and token-zero mode ACK checks remain intact. Reconnect still resets synchronization along with replacing the model client and its FIFO.

## Regressions and TDD evidence

Tracked regression tests were added before production edits and failed against original 5ee source for the expected failures: old ACK emitted `response.create` while replacement transcript/classification was pending; replacement classification could not send its own snapshot before the old ACK; active-route cancellation reused snapshot token 1. The cancellation fixture was then made deterministic by waiting for the canceled classifier call to return and consuming obsolete worker results through the real token guard.

Final tracked tests cover:

- Old accepted-resume snapshot ACK before replacement STT.
- Old accepted-resume snapshot ACK during replacement classification.
- Replacement classification completed and fresh snapshot sent before old ACK, with side/side actions preserving exactly equal instructions.
- An additional replacement classifier canceled by another committed input; sequence remains monotonic and original FIFO ACK cannot authorize latest input.
- Ordinary and manual turns without paused tasks, including token-zero mode ACK preceding old/current snapshot ACKs.

All paused-route schedules assert original goal preservation, latest classifier input, accepted response/task/playback binding, no premature main wire output and successful `response.created` admission after the correct ACK (no post-old-ACK disconnect). Existing real isolated-classifier and device-tool/wake tests remain in the focused/full suites. The controlled classifier fixture exchanges only enum results; no classifier instruction or output is inserted into main context.

For reproducible final RED evidence, `final-fix-old-source-overlay.json` maps only the three changed production files to exact `git show 5ee8d4b:<path>` scratch copies while using final tracked tests. It modifies no tracked source. Command:

```sh
go test -overlay .superpowers/resume-state-alignment/final-fix-old-source-overlay.json ./internal/gateway -run '^(TestIsolatedRouteSnapshotACKCannotReleaseReplacementInput|TestResumeStateReplacementInputRequiresOwnSnapshotACKWithoutPausedTask)$' -count=1 -v
```

Exit 1, all six subcases FAIL as expected; gateway 21.523s. Raw log: `final-fix-red.log`. The log includes `response_requested` before classification and explicit `snapshot token reused across input cancellation`.

## Final verification

All commands ran on the final committed Go content; no production/test changes after these checks.

| Command | Result | Raw log |
| --- | --- | --- |
| `go test ./internal/gateway -run '^(TestIsolatedRouteSnapshotACKCannotReleaseReplacementInput|TestResumeStateReplacementInputRequiresOwnSnapshotACKWithoutPausedTask)$' -count=20` | Exit 0, PASS, gateway 3.288s | `final-fix-green.log` |
| `go test ./internal/gateway -run '(IsolatedRoute|ResumeState|Wake|Manual|DeviceTools|LongModelAudioBurstDrainsAtDeviceCadence)' -count=1` | Exit 0, PASS, gateway 22.476s | `final-fix-focused.log` |
| `go test ./...` | Exit 0, all packages PASS, gateway 38.485s | `final-fix-ordinary.log` |
| `go test -race ./...` | Exit 0, all packages PASS, gateway 41.497s | `final-fix-race.log` |
| `go vet ./...` | Exit 0, no diagnostics | `final-fix-vet.log` |
| `git diff --check` and `git diff --cached --check` before commit | Exit 0, no whitespace errors | `final-fix-diffcheck.log` (unstaged check) |

`-race` emitted macOS linker LC_DYSYMTAB warnings for audio/gateway test linking; it finished successfully with no race report. Exact warning text remains in the race log. Unchanged package results may use Go's test cache; the changed gateway suite ran freshly.

## Remaining boundaries

No known unresolved source finding from the assigned P2. No qwen classifier/client or root documentation changes. The existing qwen client comment saying only one snapshot is outstanding predates this fix; retired snapshots can now remain queued before a new current snapshot, and its FIFO implementation already supports that. Root was informed of this comment outside gateway ownership.

Native protocol regression, Linux build/race/vet, deployment, physical-device and acoustic/field acceptance remain root-owned; this report makes no claims for them. The ACK association still uses the existing protocol assumption of one ordered `session.updated` per update, including token-zero mode updates.
