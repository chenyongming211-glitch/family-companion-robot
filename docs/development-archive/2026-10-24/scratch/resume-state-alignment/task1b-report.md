# Task 1b — enforced paused-task routing

## Finding and scope

The original native 071a23f `final-resume-twice-delayed` run produced the final count but only one `task_resume_bound`; final playback 7 did not replace paused playback 4. Old metadata did not expose the optional tool arguments/outcome, so the precise optional-tool omission could not be inferred from the original trace alone. Root's metadata-only diagnostic reproduced a stale model-copied revision failure on the first resume, recovery via query/retry, and final resume audio with no mutation after a query. Root owns those native evidence files and their verification.

Go changes only. No remote execution, provider calls, device actions, recording, audio playback, branches or worktrees were performed by this worker. Existing native ASR transcription is retained; no extra recognizer or keyword intent parser was introduced.

## Implementation

- A new committed user turn with a saved task enters a dedicated routing phase. The only advertised function is `conversation_route_paused_task`, taking exactly one action enum: side/resume/discard/replace. Gateway captures input generation/item, task ID and current revision after the current snapshot ACK.
- Routing configuration sets `tool_choice=required`; this is a provider request, not a trusted enforcement boundary. Go suppresses all route audio/transcript events, never opens route playback and requires a valid function decision and completed routing response before release.
- Missing, invalid, duplicate or stale decisions cannot bind output. Up to three attempts are allowed, with an 8-second response timeout per attempt and the existing 8-second snapshot ACK timeout. A parsed late playback final that changes revision causes retry against the fresh snapshot. No failed route silently falls through to voice.
- The gateway applies captured task identity/revision through the existing mutation logic; the model never supplies those values. Side leaves the original task unbound. Resume retains original request and endpoint; replace uses actual current native user transcription. Successful resume emits the existing `task_resume_bound` event.
- Accepted routing restores normal tools and `tool_choice=auto`, synchronizes the resulting snapshot and waits for ACK before requesting voice. Premature voice `response.created` is rejected even after routing acceptance. Binding survives the following voice response, new playback and device-tool continuations.
- The normal voice tools include paused-state reads but no longer advertise the task mutation tool. Attempts to invoke that legacy mutation after routing are rejected. Its internal implementation remains for gateway application and focused lifecycle tests.
- Abort/reconnect clears route state; stale generations remain filtered by the existing event loop. A different input overtaking unresolved routing/voice generation fails closed. New committed turns reset continuation state.
- Metadata-only `task_route` records action/outcome, attempt, captured/current revisions, session/generation, binding and route elapsed milliseconds. No tool arguments, user text, generated drafts or raw audio are logged.

## Provider-specific decision

Root's native tests found `tool_choice=required` can be acknowledged yet ignored. They also observed a text-only route followed by restoring audio modalities could complete without voice audio, whereas keeping `[text,audio]` stable preserved voice continuation. Therefore this patch never changes modalities; Go drops any route-phase audio regardless of provider behavior. Native retest of the complete committed candidate remains root-owned.

## TDD evidence

`task1b-red.log` records genuine pre-implementation failures: routing audio entered the pending playback buffer, missing route decisions were accepted, and a stale decision did not trigger restricted required routing. The stale regression was subsequently strengthened to use a parsed real-format late final receipt, rather than only incrementing a revision.

Focused tests cover restricted native wire controls and ACK ordering; no route draft/audio; missing/rejected retries and cap; stale final; side/discard/replace; three resumed interruptions retaining original task; device-tool continuation binding; abort; input overtaking routing; and premature voice before restore ACK. Legacy drain fixtures now perform explicit routing where they commit input with a saved task; normal/wake/manual/device/cadence tests remain in the complete suite.

## Verification

Final command outcomes and commit identity appended below. Native-provider, Linux and physical-device acceptance are separate; this worker claims only deterministic/macOS checks. No physical word-position or acoustic acceptance is established.

Final checks: `go test ./...` PASS (gateway 37.813s), `go test -race ./...` PASS (gateway 40.859s), `go vet ./...` PASS, `git diff --check` PASS. macOS race linking emitted the existing-style `LC_DYSYMTAB` warning; test process exited 0 with no race report. Focused route/legacy migration check passed (1.266s) before full validation. Two old fixture failures during implementation were corrected to obey the mandatory route; they were not suppressed or skipped.

Committed only owned Go source/tests on main: `4e785d6` — `fix: require paused task routing before voice generation`. No push by this worker. Root-owned `docs/protocol/playback-progress-v1.md` remains unstaged.

`route_elapsed_ms` measures from the first route response request to each routing outcome; it excludes the initial snapshot update and the final normal-tools/snapshot ACK round trip. Root should use broader event timestamps when reporting full added user-response latency. Physical interruption/stop timing is independent.


## P2 correction — fail closed on upstream routing content

The independent review identified one material P2: dropping route output locally left unheard assistant content upstream, so retrying or releasing voice on that context was unsafe. This correction supersedes the earlier drops-only description. Any nonempty route-phase PCM delta, audio-transcript delta/done or text delta/done immediately marks the routing context contaminated and returns an error. `response.text.done` now decodes its `text` field, including done-only payloads. `finishRouting` independently rejects the sticky contamination flag before task mutation, function output, cleanup/retry or voice generation, including direct calls after an ignored event error. No provider context delete/truncate/replay recovery was introduced. Stable modalities are unchanged.

Genuine pre-fix RED is saved in `task1b-contamination-red.log`: all 60 content/type/order/action combinations failed on old source (five content forms; content-only, content-before-function and function-before-content; side/resume/discard/replace). The passing regressions verify immediate rejection, defensive completion rejection, unchanged task/revision/binding, no new playback/content and no outgoing retry or voice control. Empty events remain harmless and allow a clean route to proceed. Existing content-free missing/invalid/stale bounded retry cases remain covered. The abort fixture now aborts an unresolved content-free route; a provider sending route audio instead fails closed immediately.

Fresh local checks after the minimal fix: `go test ./internal/gateway -run 'TestTaskRouting' -count=1` PASS (1.190s); `go test ./...` PASS (gateway 38.556s); `go test -race ./...` PASS (gateway 41.383s); `go vet ./...` PASS; `git diff --check` PASS. Logs use the `task1b-contamination-{focused,full,race,vet}.log` prefix. Race linking again emitted macOS `LC_DYSYMTAB` warnings; exit was 0 with no race report. These are deterministic/macOS results only. Fresh exact-source Linux/native checks remain parent-owned and pending for the corrected commit. No remote/API/provider, hardware, audio or push action was performed by this worker.

Committed the four owned Go files only on main: `1bdc325` — `fix: fail closed on routing context contamination`. Root-owned protocol documentation remains unstaged. No push by this worker.
