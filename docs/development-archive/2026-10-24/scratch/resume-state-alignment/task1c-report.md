# Task 1c implementation report

Status: implemented and locally verified. Commit: `44357b0229a869d90950b0df4274b1d1742c62cc` (`fix: isolate paused task classification from voice sessions`). Directly on `main`; no push, branches/worktrees, remote/native/device/API actions. Commit contains only ten Go source/test files. The pre-existing README and protocol-document edits were left unstaged and untouched.

## Source decisions

- `internal/qwen/classifier.go` opens a fresh WebSocket with only URL/auth copied from configuration. It sends the proven four-field text session configuration (text modality, null turn detection, fixed classifier role, empty tools), followed by the exact proven classification prefix and the three JSON data fields in `response.create.response.instructions`. No audio, ASR, history, voice configuration, device tools or main-client connection is reused.
- A single completed response must carry one strict trimmed enum. Parsing validates response/item identities, index zero, a single optional text output envelope, delta/done consistency, duplicate/unexpected output, and rejects every audio/function/unknown event. Native `response.output_item.done` may omit its optional text field, as shown by root's captured native envelope; any present completed-item text must match `response.text.done`. Limits: 256 bytes of output text, 16 KiB per event, 64 KiB total, 128 events, eight seconds per connection/attempt. WebSocket read/write deadlines and context cancellation close the connection. Errors never expose provider bodies, user text, auth or URL.
- `internal/gateway/task_routing.go` runs asynchronous classification with copied data and returns results over a channel. The session loop alone mutates state. Results must match a monotonically increasing token, connection generation, full input origin, task ID and captured revision. New input, abort, upstream reconnect and device disconnect cancel outstanding work; obsolete results cannot bind a task. The event loop also enforces worker deadlines, so an expired/cancelled result that cannot be delivered cannot leave generation waiting.
- Fresh routing waits at most eight seconds for existing matching native STT (and any missing original-request STT). Anonymous identities, wrong generations, empty/whitespace text, truncated text and unavailable original data fail conservatively. Malformed/provider failures, worker deadlines and stale snapshot revisions share one three-attempt budget. No keyword rules or resume fallback exist.
- Main voice configuration remains its normal role/tools/modalities. Removed the same-conversation route tool, routing prompts, function-output insertion and `UpdateGeneration` helper. Only the normal instructions/snapshot refresh is sent to main after Go applies ownership. All main response events during classification or its pending snapshot ACK fail closed. Classifier requests use `task_route`, never `voice_event.response_requested`.
- Accepted resume/replace bindings preserve the latest input origin and survive device-tool continuations and repeated interruptions. Side replies remain unbound; discard/replace use the existing explicit revision-checked task mutation. A normal current-snapshot ACK precedes each gateway-requested voice response. The existing `task_resume_bound` trace remains emitted by the gateway action.

## TDD evidence and regressions

All logs are under `.superpowers/resume-state-alignment/logs/` and are ignored/private.

1. Genuine pre-source RED: `go test ./internal/gateway -run TestIsolatedRouteWaitsForMatchingTranscriptWithoutMainMutation -count=1`, `task1c-red.log`. Failed because the old implementation emitted a main `session.update` containing routing role and required route tool while STT was missing. An initial test edit temporarily removed a shared legacy fixture helper and produced a compilation error; that helper was restored before recording the genuine behavioral RED and before source changes.
2. Native-shape RED: `go test ./internal/qwen -run TestIsolatedClassifierNativeEnvelopeWithOmittedItemText -count=1`, `task1c-native-envelope-red.log`. Reproduced rejection of the actual omitted-text output-item envelope, then parser correction passed.
3. Duplicate/unexpected-output RED: `go test ./internal/qwen -run 'TestIsolatedClassifierRejectsDuplicateConversationOutput|TestIsolatedClassifierRejectsUnexpectedInitialOutput' -count=1`, `task1c-output-red.log`. Both failed before stricter validation was added.
4. Worker-deadline RED: `go test ./internal/gateway -run TestIsolatedRouteWorkerDeadlineCannotLeaveGenerationWaiting -count=1`, `task1c-worker-deadline-red.log`. Failed because an outstanding worker retained its context when the gateway deadline passed; event-loop deadline handling fixed it.
5. `task1c-focused-final.log`: focused isolated-route suite passed (`ok familyrobot/internal/gateway 2.485s`). Later mixed-failure and all-main-output ACK cases are included in final full/race checks.

New coverage includes matching transcript readiness, anonymous/truncated/unavailable input, exact enum parsing, provider failures, native envelope shape, response/item/delta consistency, duplicate output, byte/event limits, secret-safe errors, handshake/read cancellation, stale late-final reclassification, every captured identity, obsolete tokens, mixed three-attempt bounds, abort/manual abort/disconnect/new-input WebSocket closure, premature main text/audio/tool/completion events before ACK, side/discard/replace, device-tool continuation and three successive resume interruptions retaining the original goal and latest origin.

Legacy playback fixtures now drive a genuine separate fake WebSocket classifier rather than a main-conversation function call. The standalone late-final/mode-ACK fixture explicitly marks its request as an existing tool continuation to keep its original snapshot-refresh scope; fresh-input classification and stale-result behavior have dedicated tests. All existing normal, wake/manual, replay, device tools, playback cadence and stale-generation tests remain in the complete suite.

## Final verification at committed source

Environment: `go version go1.25.6 darwin/arm64`.

- `go test ./...` — exit 0, `task1c-final-go-test.log`: gateway 38.763s, qwen 0.507s; all packages passed.
- `go test -race ./...` — exit 0, `task1c-final-race.log`: gateway 41.819s, qwen 1.789s; all packages passed, no race reports.
- `go vet ./...` — exit 0, `task1c-final-vet.log`, empty output.
- `go build ./...` — exit 0, `task1c-final-build.log`, empty output.
- `git diff --cached --check` before commit and `git diff --check` — clean. Reviewed staged file list and diff; Go/tests only.

## Concerns and limits

- Native final ownership tests, five-second delayed-final tests against the real provider, native normal/manual-abort retests and deployment remain root-owned. No claim of native end-to-end acceptance or deployment is made here.
- The parser intentionally follows the observed text-only event envelope and fails conservatively on new/unrecognized provider shapes. Root should use the exact final commit for native retests.
- A fresh classification connection adds provider latency and API use per paused-task input; worst-case failure processing is bounded by three eight-second attempts after transcript readiness.
- Race builds on this macOS toolchain emitted existing C/Opus linker warnings about malformed LC_DYSYMTAB in generated `000012.o`; builds/tests still exited 0, with no Go race findings.
- Device samples remain I2S delivery evidence. Exact word positions remain unknown; no physical hearing claim or alignment result is introduced.

## Review fix: HTTP Upgrade cancellation (2026-10-23)

Fixed Important P2 from `task1c-review.md`. Commit: `5ee8d4b312bdeb8eb1fd0a0f0a6a5c2c4fef79ca` (`fix: cancel classifier connections during websocket upgrade`), directly on `main`, no push. Commit contains only `internal/qwen/classifier.go` and `internal/qwen/classifier_test.go`; pre-existing README/protocol/verification-document edits remain untouched.

The standard `net.Dialer.DialContext` still handles cancellation while connecting. Immediately after TCP creation, a close watcher now owns the underlying socket throughout TLS, HTTP Upgrade, and WebSocket reads/writes. It watches the classifier context rather than Gorilla's child handshake context (which Gorilla cancels even after successful Upgrade). Deferred cleanup stops the watcher on every success/error exit; Gorilla closes failed connections and the existing deferred WebSocket close handles normal returns. The callback captures only its immutable connection; no worker/shared mutable state is added. TLS >=1.2, the eight-second deadline, constant safe errors, strict enum/native envelopes and event/byte limits are preserved.

Genuine local-only TDD evidence, with logs under `.superpowers/resume-state-alignment/logs/`:

- RED before production changes: `go test ./internal/qwen -run '^TestIsolatedClassifierCancellationClosesHTTPUpgrade$' -count=1 -v` — exit 1; `task1c-fix-upgrade-red.log`: `classifier blocked during HTTP Upgrade after cancellation` after 1.00s. The server receives the actual HTTP request, hijacks the local TCP connection, withholds HTTP 101, and the test explicitly cancels a context with no early parent deadline.
- GREEN after the fix: same focused command — exit 0; `task1c-fix-upgrade-green.log`: cancellation returned the constant connection error and the peer observed EOF in `42.417µs`, both within the test's one-second bounds and well before the eight-second handshake cap.
- `go test ./internal/qwen -count=1` — exit 0, `ok familyrobot/internal/qwen 0.501s`; `task1c-fix-qwen-full.log`.
- `go test -race ./internal/qwen -count=1` — exit 0, `ok familyrobot/internal/qwen 1.810s`; `task1c-fix-qwen-race.log`, no race diagnostics or linker warnings.
- `go vet ./internal/qwen` — exit 0, empty `task1c-fix-qwen-vet.log`.
- `git diff --check` and staged diff check before commit — clean. The final staged list contained only the two owned qwen files.

No remote/provider/API/device/audio operations, gateway/doc changes, branches/worktrees or toolchain repairs were performed. Root owns exact-head Linux/full checks, final independent review, delayed-final/native/normal/manual-abort verification and deployment. This fix does not establish native end-to-end or physical acceptance. Root's independently running native443 process uses its already compiled binary and is unaffected by these source edits.
