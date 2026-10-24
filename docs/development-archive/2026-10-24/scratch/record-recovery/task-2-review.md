# Task 2 re-review — 5772dfb

Reviewed fix: 8fc16a3..5772dfb, after original gateway implementation ce51efa. Current inspected HEAD is 5772dfb. Scope: the original Task 2 integration/configuration/documentation requirements, I1/I2 from the first review, and the explicitly approved idle-expiry completion in task-2-fix-brief.md. Read the fix package, production paths, protocol regressions, and report appendices. No tracked source modification, broad crawl or suite rerun was performed.

## Verdicts

- **Spec compliance: PASS for the approved Task 2 code scope.** Original Important findings I1 and I2 are resolved. The approved idle-retention completion is implemented.
- **Code quality: PASS. No remaining Important or Critical findings identified.** Linux, real native gateway behavior, hardware timing and production readiness remain separate root-owned validation gates; this is not deployment approval or a field PASS.

The earlier ce51efa review and isolated failing I2 reproduction are preserved in `task-2-review-ce51efa.md`.

## Requirement authority and context bound

The root clarified that the user approved `docs/verification/2026-10-23-resume-design-review.md`, not a new total-native-history cap. I inspected that source: line 40 explicitly distinguishes the gateway's latest 12 messages / 512 characters from native items accumulated in the same Qwen conversation; line 52 requires recovered relevant text/tasks to form bounded context. The original implementation brief's shorthand “最近模型上下文” must be interpreted against that approved design. The corrected plan/README makes this existing distinction explicit rather than introducing a newly relaxed native cap. I therefore do not treat ordinary native accumulated history as an outstanding scope defect. Effective clearing of the currently used native conversation remains independently required and is addressed below.

## Original findings closed

### I1 — active native context survives successful clear: FIXED

`internal/gateway/clear_native.go:34-85` stops owned parent playback with its identity, clears queued parent audio, closes the old socket, increments generation, resets response retirement/snapshot state and opens a fresh native conversation asynchronously. It does not copy old native items or local history. `clear_native.go:148-160` can inject only the authorized clear command plus subsequent input; the actual deletion result is separately introduced by `session_tools.go:208-213`. Old-generation events remain excluded by the event-loop generation gate.

`session_tools.go:194-224` waits for the real receipt before confirmation generation. `generation_sync.go:78-95` sends instructions and explicit empty tools together through the ACK FIFO; `qwen/client.go:144-150` serializes tools=[] rather than omitting it. A subsequent business input clears the receipt state, restores configured tools and waits for the new ACK. `playback_progress.go:49-51` retires receipt playback from resumable tasks, preventing a completed deletion operation from becoming a paused task on interruption.

Protocol evidence inspected: `clear_native_test.go:35-94` covers enabled/disabled persistence, fresh initial context, actual successful result, no response before ACK, and tool restoration before later generation; lines 98-117 cover failure; lines 119-148 cover a new committed input superseding a pending disk receipt; lines 309-338 cover obsolete snapshot ACKs unable to release replacement input. These tests inspect actual mock-WebSocket traffic rather than only state flags. Root's baseline native privacy reproduction establishes the original defect; integrated native validation of this fix remains root-owned and is not invented by this review.

### I2 — persistence disabled rearchives cleared parent transcript: FIXED

`internal/gateway/records.go:394-401` installs response/generation suppression before the records==nil return. The session-level predicate at lines 451-452 applies to normal completion (`playback.go:85`), interruption (`session.go:439`), draft snapshotting (`records.go:273`) and finalization (`records.go:439`). Generation scoping permits a valid new native response to reuse an old response ID without being suppressed.

`records_test.go:743-764` incorporates the exact formerly failing completion regression. Lines 766-788 cover fresh-generation ID reuse; lines 790-812 cover the actual interruption/history path without persistence. `clear_native_test.go:340-363` also checks the new native generation does not inherit the old retired-response ledger.

## Cross-cutting review

- **Handshake/new input/abort:** the asynchronous clear path buffers at most 256 events / 1 MiB (`clear_native.go:99-123`), handles abort immediately, and replays later input after connection setup. Explicit new capture cancels stale confirmation before replay (`clear_native.go:138-146`); committed replacement input also cancels it (`playback_progress.go:236-244`). The receipt's later arrival cannot populate a canceled tool call. Tests at `clear_native_test.go:150-229` exercise held native handshake, immediate abort, and byte-for-byte forwarding of new audio; lines 247-287 cover interruption of receipt playback with preservation of the new speech prefix; lines 290-307 assert the immediate interrupted stop carries the old playback identity.
- **Clear teardown/reconnect:** existing pending-clear completion and same-device lease/final-save ordering remain intact. The native-open result is delivered through the session loop; session cancellation closes a pending client rather than adopting it after teardown (`clear_native.go:84-93`). Disk deletion failure still yields failure and does not reopen the old native conversation.
- **Recovery/source truth:** original/latest source, selected-start labels, historical-only samples and new task namespace remain unchanged. Restored state itself does not execute a task. Discard/replacement/clear still remove old executable ownership. No new accurate-position claim is introduced.
- **Durability honesty:** pending Save/Clear states and actual receipts remain distinct. Snapshot content changes require a fresh ACK before response.create. Clearing the active upstream conversation is correctly distinguished from any provider-side durable retention promise.
- **Idle physical expiry:** `internal/recordstore/store.go:431-454` schedules cleanup on the existing single worker at clamp(retention/4,25ms,1min). It does not add a concurrent disk writer, spin for tiny positive durations, or release the store lock before accepted jobs finish. Errors log a payload-free fixed message. `store_test.go:539-559` writes once and then polls only os.Stat, so physical expiry is genuinely timer-driven. Snapshot UpdatedAt expiry, per-entry gateway pruning, and disk-stall limits are documented separately.

## Verification boundary

The fixer's focused test results, final full Darwin race pass and vet pass were inspected in the reports. I inspected their protocol assertions and relevant implementation paths rather than rerunning broad tests already completed. No concrete additional uncovered doubt required another focused test in this re-review. The root separately owns exact-Linux tests/build and real Qwen same-device-WS privacy/recovery trials. Production deployment and measured hardware stop latency are outside this report.
