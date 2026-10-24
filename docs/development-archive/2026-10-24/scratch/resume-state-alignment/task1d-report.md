# Task1d report: unknown-position clarification guard

Scope: internal Go gateway/Qwen source and tests only, directly on main. No push, provider/server/device/audio actions, firmware, docs or tools edits. Root owns native/release validation.

## Behavior

- Replaced the four-enum isolated classifier response prompt with the exact v2 six-enum instructions and same four data fields, including gateway-owned `awaiting_resume_choice`. Existing dedicated text-session configuration, event/byte bounds, strict completion parsing and eight-second deadline remain.
- Ordinary `resume` at unknown word position binds the original task and requests only the proven fixed native clarification sentence via one response's `response.instructions`. The normal main role/tools/modalities stay in place. Explicit `restart`/`resume_from` use existing resume binding with distinct trusted response policy, retaining original scope/endpoint and never describing user-selected start as measured playback.
- Awaiting choice lives on the paused task, changes its authoritative revision, is copied into classifier worker input and snapshots, and survives side questions/upstream interruptions. Explicit restart/start clears it; discard/replace retire the old task. Device disconnect disposes session state.
- The clarification's native PCM remains in the existing pending buffer until matching `response.audio_transcript.done`, `response.audio.done`, and completed `response.done` are validated. Only Unicode punctuation and whitespace normalize. Positive even complete PCM is bounded to 720000 decoded bytes (15 seconds at mono16/24k); generation deadline is eight seconds starting at response.create, including waiting for response.created. Optional native item/content/final envelopes must contain a single consistent audio message, no tools/text/extra output or mismatched identity/index. Extra output after completion but before release also rejects.
- `clarification_verified` is a fixed privacy-safe trace emitted before TTS start. The same held PCM is released through the unchanged 60ms playback scheduler, with matching transcript, as the latest block of the original task without declaring goal completion.
- New input, reconnect, disconnect and interruption clear the per-response guard/PCM/deadline. Explicit abort/VAD still stop before reconnect; verified-but-interrupted question history remains interrupted. Unvalidated output never starts TTS or mutates tools/history/task content.

## RED/GREEN evidence

- `logs/task1d-red.txt`: genuine RED before source changes: response.create lacked fixed instructions; directly admitted raw first PCM produced `[start audio]` before clarification validation.
- `logs/task1d-classifier-red.txt`: genuine RED: restart/resume_from rejected and awaiting-choice data missing.
- `logs/task1d-post-completion-red.txt`: self-review regression RED for extra text/audio/transcript/done after completion but before playback; fixed by retaining guard through release.
- `logs/task1d-history-red.txt`: self-review regression RED for interrupted validated question losing its interrupted history; cleanup moved after history/cancel handling.
- `logs/task1d-target-final.txt`: targeted gateway and classifier GREEN, including valid release/cadence, exact allowed normalization, mismatch/extra words/tool/text/status/zero/odd/overlong/missing completions, deadline, envelope identities/content, stale output, cancellation/reconnect, pending choice/side/explicit decisions, revision and fresh ACK ordering. Existing ownership/device continuation tests use explicit restart when plain-resume fallback is outside their purpose.

## Boundary and concerns

No native provider claim is made by this implementation report. Root supplied proven v2 enum results and native clarification captures; implementation inspected those files, including actual audio item/part/final schema and matching user input envelope before response.created. Root must run exact-source Linux/native scenarios before release.

Guarded malformed/failed/cancelled clarification output fails closed by clearing held output and returning an error; it cannot be reused as unheard contaminated voice context. Task goal is not mutated by rejection. Session disconnect then follows existing cleanup. Root explicitly confirmed this bad-status boundary. Ordinary native cancelled/VAD prefix replay behavior remains unchanged; an explicit VAD/device abort while the guard is holding is the immediate stop/reconnect path.

No word/time/length oracle or heuristic number matching was added. Native generated transcript is the speech-policy check, not proof of words heard by a person; word position remains unknown.

Final verification and commit recorded below after completion.

## Final verified result

- Main commit: `2aaf3de` (`Guard unknown-position resume with validated native clarification`). Only the eleven owned internal Go source/test files committed; no push.
- `go test ./... -count=1`: exit 0, all packages pass (`logs/task1d-full.txt`, gateway 39.862s).
- `go test -race ./... -count=1`: exit 0, all packages pass, no race reports (`logs/task1d-race.txt`, gateway 43.205s). macOS linker emitted `malformed LC_DYSYMTAB` warnings for cgo/Opus-linked audio/gateway test objects; retained verbatim in the log, no test failure.
- `go vet ./...`: exit 0 (`logs/task1d-vet.txt`, empty).
- `git diff --check -- internal` and staged diff check: exit 0 (`logs/task1d-diffcheck.txt`, empty).
- Exact classifier instructions compared byte-for-byte to the v2 probe response instructions: match. No native endpoints were contacted by this agent.

## Final-review P2 fixes at 07c6549

Read the final Task1d fix brief, final-review-2aaf3de.md, and the reviewer committed-question overlay reproduction. Both review findings are fixed in `07c6549` (`Close replaced clarification playback and emit guard traces`), directly on main. Only five owned `internal/gateway` Go files were committed; no push, branches/worktrees, provider/server/device/native actions, or root docs/tools changes.

- Added the two fixed `clarification_verified` / `clarification_rejected` values to the existing voice trace allowlist. Regression invokes the actual native guard handlers, demands exactly one verification before TTS start, observes rejection with no TTS/audio, rejects unknown event strings, and limits JSON trace keys to existing privacy-safe metadata. Existing broader credential/upstream-ID/text/audio privacy checks remain.
- Replacement commits now close an admitted question with its original session/playback identity and `reason=interrupted`, seal/pause the block, preserve interrupted draft history as sanitized context, reset playback scheduler ownership, then clear held output. Stop write failure is returned to the session loop before accepting replacement input. The model connection/generation remains intact, with no cancellation wait or reconnect that could discard the newly committed input.
- The same cleanup applies when an accepted direct transcript replaces the question's input or changes its transcript. Matching duplicate input and unrelated STT with an outstanding committed identity do not supersede the active question. Held and verified-but-unstarted questions clear silently without fabricating playback/history/stop.
- Regression exercises committed/direct/corrected transcript entry points across held/verified/playing phases, identity-bearing stop, late old final settling an interrupted block, stale old native envelopes, original task/awaiting-choice retention, explicit subsequent user choice, monotonically increasing snapshot tokens, a preexisting mode ACK in the FIFO which cannot release replacement voice, and balanced next TTS start/stop. Existing targeted route/choice/side/revision and ordinary VAD/device-abort regressions pass unchanged.

Fresh TDD evidence and final checks (raw logs in `.superpowers/resume-state-alignment/logs/`):

- `go test ./internal/gateway -run 'TestClarification(GuardTraces|Replacement)' -count=1 -v`: genuine RED before production changes; missing verified/rejected traces, absent playing-question stop, direct/corrected transcript retaining old output, and stop failure swallowed / replacement accepted after failed device write. `task1d-finalfix-red.txt`. The held/verified unstarted commit controls already passed.
- `go test ./internal/gateway -run 'Test(Clarification|UnknownResume|IsolatedRouteSnapshotACK|ResumeStateReplacementInput|RequestOrigin)' -count=1 -v`: GREEN, gateway 1.224s (`task1d-finalfix-target.txt`). Intermediate GREEN log retained as `task1d-finalfix-green.txt`; it caught a missing `basis` in the new synthetic receipt fixture, corrected before final targeted/full checks.
- `go test ./... -count=1`: exit 0, all packages pass, gateway 41.036s (`task1d-finalfix-full.txt`).
- `go test -race ./... -count=1`: exit 0, all packages pass, gateway 44.732s, no Go race reports (`task1d-finalfix-race.txt`). Same existing macOS cgo/Opus `malformed LC_DYSYMTAB` linker warnings retained verbatim for audio/gateway test binaries.
- `go vet ./...`: exit 0 (`task1d-finalfix-vet.txt`, empty).
- `git diff --check -- internal/gateway` and staged diff check: exit 0 (`task1d-finalfix-diffcheck.txt`, empty).

Native exact-source Linux/provider validation, evidence checker, deployment, and acoustic/word-position boundaries remain root-owned. The 15s PCM / 8s deadline, one native ASR/TTS chain, session modalities/tools, 60ms cadence and event-loop writer remain unchanged. No new field/native acceptance claim is made.
