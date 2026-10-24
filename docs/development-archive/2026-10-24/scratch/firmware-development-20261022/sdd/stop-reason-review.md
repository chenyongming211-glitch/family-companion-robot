# Optional interrupted TTS stop reason — review

Reviewed range: `fe99184..353f715` (`353f715cfe7fe6ec0e7abfc65b862ae2473a44ba`).

## Verdicts

- **Spec compliance: APPROVED.** The six-file delta implements only the requested optional interruption metadata, wire assertions and documentation.
- **Code quality: APPROVED.** No Critical, Important or Minor findings identified.

## Assessment

`internal/protocol/protocol.go:27` adds `Reason string` with `json:"reason,omitempty"`. `internal/gateway/session.go:318` sends the immediate interruption stop using `s.json(protocol.Control{...})`, with `Reason: "interrupted"` and the existing session ID. The initial no-op guard, interrupt signature, send-error return and all subsequent pending-audio clearing, cancellation and lifecycle code are untouched by the delta.

The concrete risk from bypassing the normal helper was whether it skipped existing side effects. Focused inspection of `session.go:80` and `session.go:87` confirms `tts` only constructs a control and delegates to the same JSON writer. The previous empty `Text` value and new default empty `Text` both omit the text field. Therefore this substitution preserves the existing serialization/write path and stop-before-cancellation ordering, while adding only the requested reason. Normal stops continue through the unchanged helper, with an empty Reason omitted.

The existing wire-level tests now assert the interruption reason for the device/model/pending/capturing table (`server_test.go:219`) and tool abort/listen/model triggers (`session_tools_test.go:372`). They retain the old-audio, replacement-reply/context and late-tool-result coverage. The replacement reply's stop (`server_test.go:249`) and normal playback harness (`playback_test.go:125`) decode actual wire JSON into maps and reject even the presence of a reason key; these checks cover omission rather than merely an empty decoded string. No production replay, context, tool, idle or pacing behavior is changed in the reviewed delta.

README gives the exact interruption JSON example, normal omission contract and explicit custom-firmware/field-acceptance dependency. It does not claim stock-firmware queue handling or natural-barge-in acceptance.

## Evidence and limits

Read root `AGENTS.md`, task brief, implementation report, and the supplied review diff once. The diff was the primary scope; outside it, inspected only the JSON/TTS helpers to resolve the concrete helper-bypass risk, the existing interrupt test table to confirm the stated cases, and line locations for this report. No concrete unresolved risk justified additional test execution.

The implementer reports behavioral missing-reason RED failures, focused GREEN, full `go test -race ./...`, `go vet ./...`, `go build ./cmd/gateway`, and diff checks. Those suites were not rerun during this review. The disclosed macOS `malformed LC_DYSYMTAB` race-link warning is not hidden and did not fail the reported tests; no inference of Linux/device field acceptance is made from it.

No tracked files, index, HEAD, branches, remotes, hardware, server or API state were changed by the review. Only this requested ignored report was written. The approved change is optional gateway metadata only; AEC/reference alignment, firmware queue handling, flashing/rollback and acoustic acceptance remain separate outstanding dependencies.
