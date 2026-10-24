# Task 2 gateway persistence/recovery report

DONE: implementation committed on main as ce51efa (feat: persist safe per-device conversation records). Store files from Task1 were not modified. No push/deploy was performed by this implementation agent; root owns reviewed release integration.

## Implementation boundaries

- Gateway owns the exclusive record store when ROBOT_RECORD_DIR is nonempty; empty explicitly disables persistence. Retention defaults to168h. Invalid env duration/startup storage failure aborts startup; systemd grants only /var/lib/family-robot/records writable, with documented family-robot ownership and0700 creation.
- Authenticated normalized MAC is the isolation key and same-device connection lease. Load has a2s context deadline before device hello/audio setup. Failed/unloaded snapshots are write-protected until an explicit successful clear.
- Event loop admits bounded Store.Save operations and polls receipts without disk waits. Unchanged data is deduplicated, audio delta and10ms ticker do not request snapshots. Stop/new input do not wait for persistence. Shutdown waits for session final save admission and drains the owned store within a shared5s deadline; timeout logs do not claim successful draining.
- Up to128 timestamped text records (generated draft counts toward128), each text bounded to512 runes; latest12 enter model context. Wake input/reply are excluded using existing explicit wake handoff provenance. History retains session/input/response/playback identities and delivery labels. Interrupted draft text is omitted from model context.
- Recovered task retains original request/endpoint, original source, latest segment request, awaiting-start-choice state, and user_selected/user_restart provenance. It receives a new session namespace ID. Numeric evidence is historical_playback only: current playback ID is zero, played_samples is null, position_known=false, text_position=unknown. Restoring does not create a voice response or bind execution.
- paused_task, recording_capabilities, task_disposition and archived_tasks_executable=false are data in the authoritative model snapshot. Discard survives restart; replacing/newly interrupted tasks use active disposition. Historical text alone cannot reactivate an old task. Receipt fact changes require a fresh snapshot ACK before response.create.
- Local conversation_clear_records accepts only a narrow full current real-transcript deletion command (canonical: 请清空本设备对话记录). Negation, quotation, hypothetical questions, stale generation, unknown input and wake content cannot authorize deletion. Temporary history/task/recovery state clear synchronously; tool success waits for actual deletion result. Accepted old writes precede deletion in FIFO. Connection teardown retains the device lease until pending clear finishes; new input stays responsive and is flushed after that deletion barrier. Retired old playback/drafts cannot reappear on teardown.

## Red/green evidence

Tests were added before the corresponding implementation/fixes and executed red:

1. Initial recovery/config tests failed to compile because recordBackend, RecordRetention, loadRecords/checkpointRecords/recordingCapabilities did not exist.
2. TestRecordsArchiveBoundAndIndividualExpiry initially failed with per-item expiry or context bound failed; fixed persisted provenance/archive handling.
3. TestRecordShutdownBoundsSlowStore failed with slow store blocked shutdown beyond deadline (6.00s); fixed shared5s close deadline.
4. TestRecordsNewTaskAfterDiscardHasActiveDisposition and TestRecordsExpireInMemoryBeforeNextModelSnapshot failed on old disposition/expired memory; fixed before generation snapshots.
5. TestRecordsFinalizationWaitsForClearBeforeNewLoad failed connection lease could release before clear admission; TestRecordsLatestSegmentAndChosenStartSurviveRecovery failed lost selected start provenance. Both fixed.
6. TestRecordsEmptyResponseDoesNotRewritePreviousIdentity failed empty generated turn rewrote user provenance; fixed append/stamp gating.
7. TestRecordsDraftCountsTowardBound failed retained129 text records; fixed draft accounting.
8. TestRecordsRestoreKeepsOriginalSourceAcrossMultipleSessions failed original source or choice state lost across restore; fixed oldest source and awaiting choice persistence.
9. TestRecordsRestoredTaskTextRemainsBounded failed restored text bypassed per-message bound; bounded even oversized valid loaded JSON and marked truncation.
10. Clear receipt tool regression with valid whitespace JSON failed immediate output; accept an empty parsed object while rejecting null/extra arguments.
11. TestRecordsNewInputDuringClearPersistsAfterReceipt failed input accepted during clear was never saved after receipt; fixed immediate post-clear snapshot of new state.
12. TestRecordsDisconnectMarksLatestReplyInterrupted and TestRecordsClearDoesNotRestoreRetiredPlaybackFlagsOnDisconnect failed on stale/missing interruption flags; flags now describe only live non-retired business playback.

Other passing coverage includes process reopen through a real store; original request, endpoint and draft exclusion; different-device isolation; persisted discard/model disposition; Save queue rejection and failed fsync receipt; failed-load no overwrite; pending-vs-durable capabilities; ACK refresh when receipt settles; clear success/failure only after receipt; normalized MAC duplicates; actual WS disconnect-during-clear/reconnect lease; accepted save drain and directory reopen; no unchanged snapshot enqueue; clear request grammar.

## Actual verification

Environment: go version go1.25.6 darwin/arm64. Final verified source matches commit ce51efa.25 new record regressions; complete existing suite retained.

`go test -race -count=1 ./...` exit0, actual stdout/stderr:

```text
?   	familyrobot/cmd/gateway	[no test files]
?   	familyrobot/cmd/smoke	[no test files]
# familyrobot/internal/audio.test
ld: warning: '/private/var/folders/0h/_1wjtc8n6h1gn9mjqwx_n1w40000gn/T/go-link-2582379885/000012.o' has malformed LC_DYSYMTAB, expected 98 undefined symbols to start at index 1626, found 95 undefined symbols starting at index 1626
# familyrobot/internal/gateway.test
ld: warning: '/private/var/folders/0h/_1wjtc8n6h1gn9mjqwx_n1w40000gn/T/go-link-2721465582/000012.o' has malformed LC_DYSYMTAB, expected 98 undefined symbols to start at index 1626, found 95 undefined symbols starting at index 1626
ok  	familyrobot/internal/audio	2.045s
ok  	familyrobot/internal/devicetools	1.544s
ok  	familyrobot/internal/gateway	48.879s
ok  	familyrobot/internal/protocol	3.278s
ok  	familyrobot/internal/qwen	2.050s
ok  	familyrobot/internal/recordstore	3.983s
?   	familyrobot/tools/voice-audit	[no test files]
```

`go vet ./...` exit0; actual stdout/stderr empty (task-2-vet.log is empty). `git diff --check` and staged diff check exit0. The Darwin linker LC_DYSYMTAB warnings above are present; race test binaries still linked and every package passed. Linux verification remains root's separate gate.

## Residual limits / handoff

- These are gateway/Go tests on macOS, not Linux release, real Qwen validation, hardware playback/stop latency, or production acceptance. Root owns isolated real Qwen restart/clear trials, Linux release build and deploy.
- Failure injection uses a minimal recordBackend test double for load errors, queue rejection, receipt errors, delayed clear, and slow close. Store package real-filesystem tests run in full race suite. Actual disk stalls may outlive the shutdown deadline; the store keeps its lock and accepted worker operations until drained, but process kill can lose outstanding work.
- Same-device exclusion deliberately rejects concurrent/reconnecting connections until old final save admission/pending clear completion. Normal load deadline2s; clear deadline5s. No accurate automatic continuation is introduced; no auxiliary ASR is added.
- Retention is checked per item on load/generation/snapshot and by store startup/access for file expiry. There is no background exact-second physical deletion timer. Failed delete reports failure; existing disk contents can remain if storage itself is unavailable.
- Clear removes the gateway temporary/persisted text and task state. It does not send remote historical-item deletion to the live Qwen session; that connection stays open for tool-result continuation with a cleared authoritative gateway snapshot. This does not promise cloud-side erasure or vendor retention behavior.
- Text was bounded intentionally; truncated original requests fail existing routing safely rather than claiming a complete original range. User-selected start is explicit provenance, not measured playback.
- Protocol/tool tests adapted expected tool count for the added local clear tool and expected timestamp/identity metadata; existing control, barge-in, task routing, fixed clarification and ACK-order scenarios remain included in full tests.
- No raw audio, credentials, runtime snapshots, build artifacts or this ignored report/log directory are committed. Root-owned alignment report remains unstaged.

## Handoff state

Commit: ce51efa. Remaining untracked public file is root-owned docs/verification/2026-10-23-record-recovery-alignment.md, deliberately not staged. The plan-path correction was included as authorized. This ignored report and the race/vet/targeted logs remain under .superpowers/record-recovery/.


## Important I1/I2 fix wave and idle retention completion (2026-10-23)

This section supersedes the earlier residual statements that the live native connection remains open after clear and that idle physical expiry has no periodic worker. Work is on main; no deployment or push by the fixer. Root's offline alignment report and firmware were untouched. Root's retention cadence plan edit is included, expanded with the actual interval/failure boundary.

### Implementation

- Accepted clear installs generation-scoped response suppression at session level, regardless of whether persistence is enabled. Completion, interruption, draft snapshotting and finalization use the same guard. Fresh native generations can reuse response IDs without suppressing valid output.
- The clear tool immediately retires the old native socket and its reader generation, clears pending parent PCM/transcripts, and sends an interrupted stop with the existing playback ID when device audio is still owned. Opening the replacement native conversation is asynchronous. Its initial instructions have only cleared gateway state; no old native items/history/committed audio are copied. The response-retirement ledger and snapshot ACK state reset for the new generation.
- At most 256 device events/1 MiB are buffered during the replacement handshake. The existing at-most-two-second uncommitted input tail may cross the barrier, because it follows the clear command's commit; pre-clear committed audio cannot. Abort returns an interrupted stop while the handshake is held, cancels the pending confirmation and drops only the explicitly aborted buffered input. Overflow/handshake errors fail the connection explicitly. New listen capture or a new committed item supersedes pending clear confirmation, while the disk operation/lease barrier still completes.
- Fresh native conversation receives only the current authorized clear command as input_text. The actual disk receipt becomes authoritative clear_records_result snapshot data; successful confirmation waits for deletion, failed receipt remains failed. It does not send the old function_call_output on a new socket. Root's real native capability probes found that manually recreated function_call items do not populate the upstream pending-call registry. They also found both session and response tool_choice=none ineffective, while explicit tools=[] yields speech for both success and failure.
- Accordingly, the existing ACK FIFO now supports one snapshot update containing instructions plus an explicit tools=[] for receipt speech. Normal tools are restored with another ACK before the next business generation. A same-operation repeated clear call reuses its actual receipt rather than clearing disk twice. Receipt playback cannot create a resumable paused deletion task.
- Store physical expiry runs inside its existing single FIFO worker, never a second disk writer. Sweep cadence is clamp(retention/4, 25ms, 1min). Default168h therefore schedules once a minute; very short positive TTL cannot spin. Worker exit stops the ticker, accepted jobs still drain and the exclusive directory lock stays held until worker completion. Maintenance errors log only record_retention_cleanup_failed, without body/path/secrets. The existing filesystem I/O and fsync failure boundaries remain.
- README/plan distinguish gateway-injected recent background (12 messages/512 characters) from native items accumulated during an ordinary connection. They document active-session isolation rather than provider retention/deletion claims, and clarify file expiry by snapshot UpdatedAt separately from per-entry pruning.

### Tests-first failures and regression coverage

All failures below were observed before their corresponding production fixes; logs are in this ignored coordination directory.

1. fix-red.log: TestClearUsesFreshNativeConversation failed for both disabled and enabled persistence with upstream did not open; TestRecordsClearDisabledDoesNotRestoreParentText failed with successful clear restored the old parent response into temporary context; TestIdleExpiryPhysicallyRemovesSnapshot failed with expired snapshot remains physically present with idle worker. These now pass.
2. fix-races-red.log: TestClearNewCommittedInputSupersedesPendingReceipt timed out because a new committed input was gated on the old clear tool's pending disk result. Fixed by canceling that obsolete confirmation when a new input identity is committed. New input can generate while deletion remains pending; its later old receipt cannot emit an extra response.
3. fix-repeat-red.log: TestClearConfirmationCannotRepeatDeletion failed because a repeated confirmation call started deletion again. It now returns the completed local receipt.
4. fix-generation-red.log: TestRecordsClearSuppressionDoesNotCrossNativeGeneration failed because a fresh response reusing the prior ID was dropped. Suppression now includes generation.
5. fix-confirmation-interrupt.log: TestClearConfirmationInterruptionKeepsOnlyNewInput timed out because interrupted clear receipt audio became a paused task with the deletion command as original request. Receipt-only playback is now retired from task recovery; new speech prefix survives and the next response proceeds normally.
6. fix-stop-red.log: TestClearStopsParentPlaybackBeforeHandshake failed because the clear stop omitted reason=interrupted. It now stops the existing device block with the correct reason and playback identity before any replacement handshake.
7. fix-empty-tools-red.log: native protocol confirmation failed the explicit tools=[] assertion. The ACK-tracked tool update now passes; restoration before later normal response.create is also checked.
8. fix-retired-red.log: TestClearNewNativeGenerationDoesNotInheritRetiredResponseIDs timed out waiting for a fresh response that reused a completed response ID from the old socket. The new native generation now has an empty retirement ledger, as ordinary reconnect already did.

Nine new clear protocol tests use real mock WebSockets. They cover enabled/disabled storage, fresh initial context and subsequent response path, successful/failed/pending receipts, no early response before snapshot ACK, explicit empty tools and restoration, retired ACKs unable to unlock replacement input, new committed input during deletion, bounded handshake input forwarding byte-for-byte, abort before handshake completion, confirmation interruption, playback identity, and generation-local response retirement. Three history/suppression tests additionally cover disabled storage completion, interruption, and generation reuse. The idle expiry test writes a real snapshot once, then uses only os.Stat (no Load/Save) until physical deletion. Existing lock, FIFO, Close drain/timeout, exec FD, retention and filesystem failure tests remain in the full suite.

### Validation boundary

The fixer ran Go tests on go1.25.6 darwin/arm64. Root separately owns the Linux build, real same-device-WS secret/clear/recall test, full native persistence workflow, and any device/deployment acceptance. Root's native capability probes inform the protocol choice but are not presented here as an integrated gateway field PASS. No raw audio, credentials, firmware artifacts, runtime snapshot bodies or ignored coordination files are staged.

### Final verification on the committed source

`go test ./internal/gateway ./internal/recordstore ./internal/qwen -run 'Record|Clear|Generation|Tool|Playback|IdleExpiry' -count=1` exit0:

```text
ok  	familyrobot/internal/gateway	21.108s
ok  	familyrobot/internal/recordstore	0.912s
ok  	familyrobot/internal/qwen	0.927s
```

`go test -race -count=1 ./...` exit0 (rerun after final retired-ID fix):

```text
?   	familyrobot/cmd/gateway	[no test files]
?   	familyrobot/cmd/smoke	[no test files]
# familyrobot/internal/audio.test
ld: warning: '/private/var/folders/0h/_1wjtc8n6h1gn9mjqwx_n1w40000gn/T/go-link-786414889/000012.o' has malformed LC_DYSYMTAB, expected 98 undefined symbols to start at index 1626, found 95 undefined symbols starting at index 1626
# familyrobot/internal/gateway.test
ld: warning: '/private/var/folders/0h/_1wjtc8n6h1gn9mjqwx_n1w40000gn/T/go-link-1234005437/000012.o' has malformed LC_DYSYMTAB, expected 98 undefined symbols to start at index 1626, found 95 undefined symbols starting at index 1626
ok  	familyrobot/internal/audio	1.373s
ok  	familyrobot/internal/devicetools	1.612s
ok  	familyrobot/internal/gateway	48.393s
ok  	familyrobot/internal/protocol	1.735s
ok  	familyrobot/internal/qwen	2.312s
ok  	familyrobot/internal/recordstore	4.246s
?   	familyrobot/tools/voice-audit	[no test files]
```

`go vet ./...` exit0 with no output. `git diff --cached --check` exit0. Existing Darwin LC_DYSYMTAB linker warnings remain; every race-tested package passed. Final tracked changes are15 files; source/config/docs scope was checked before staging. No ignored reports/logs or production artifacts are staged.

Fix commit: `5772dfb` (`fix: isolate cleared native context and expire idle records`), main. Tracked working tree clean after commit. No push/deploy by fixer. Root Linux/native/integration acceptance remains pending.
