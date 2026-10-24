# Task 2 spec and quality review

Reviewed implementation: 7cb958e..ce51efa. Current HEAD 8fc16a3 differs only by the root-owned alignment report; reviewed gateway sources are unchanged. Scope: Task 2 integration/configuration/documentation and its binding Global Constraints; Task 1 store implementation was already reviewed. No tracked source modifications, broad repository crawl, or full-suite rerun.

## Verdicts

- **Spec compliance: FAIL — two Important findings remain.** The explicit clear/context contract is incomplete even though gateway snapshots and durable deletion receipts are handled carefully.
- **Code quality: NEEDS CHANGES — two Important findings, no Critical findings.** Release should wait for fixes and focused regressions. Linux/native/device acceptance is a separate root-owned gate; no deployment conclusion is made here.

## Important findings

### I1. Successful clear leaves the old native conversation available to subsequent generation

Primary changed location: `internal/gateway/records.go:385-401`.

`startClearRecords` retires playback and clears `hist`/`paused`, but neither closes the current upstream conversation nor removes its previous native conversation items. After the durable receipt, `internal/gateway/session_tools.go:196-210` sends the clear tool result to that same model connection and requests continuation. The subsequent ACK gate only changes instructions (`internal/gateway/generation_sync.go:78-83`, `internal/qwen/client.go:137-143`), then sends an ordinary `response.create` (`internal/gateway/generation_sync.go:111-126`). Therefore the cleared local JSON is not the full model-visible conversation: previous native user/assistant items remain available. `temporary_context=false` can describe an empty gateway cache while the actual continuation still has old conversational material, and a later response may repeat and rearchive that material.

This violates the requested effective clearing of temporary context/recovery data and the binding model-context boundary. The report's caveat about cloud retention does not resolve it: a provider's durable retention policy is distinct from the active conversation the gateway chooses to continue. The same distinction matters when claiming the 12-message/512-character snapshot is the entire model context; the local array bounds alone do not impose those bounds on retained native items.

Required direction: after the clear barrier, make subsequent generation use a native conversation without pre-clear items, preserving only explicitly allowed current input/result and later input. Coordinate reconnect/ACK and late-event retirement without blocking stop/new input on disk. Do not claim vendor-side erasure. Add a protocol regression establishing that pre-clear native items cannot remain in the generation path; root is separately running the real same-connection secret -> clear -> recall probe. This review establishes the lifecycle gap from source, and does not invent a native trial result.

### I2. With persistence disabled, the clear-tool parent transcript is restored after clear succeeds

Primary changed location: `internal/gateway/records.go:396-401`; consuming guard: `internal/gateway/playback.go:85-87` (also the interrupt path at `internal/gateway/session.go:436-440`).

When `ROBOT_RECORD_DIR` is empty, `records == nil` is the supported default. `startClearRecords` empties history and returns a successful receipt at lines 396-399 before installing `suppressResponse`, which exists only inside `conversationRecords`. When the current tool-parent response finishes, `play()` explicitly permits `records == nil`, appends its pre-clear transcript to the newly emptied history, and stamps it as a new turn. Thus a parent reply that repeated an old private value before calling the clear tool survives the successful clear in temporary context. The enabled-storage path suppresses this same response, so the two supported configurations have inconsistent clear semantics.

Required direction: track the cleared/retired parent response at session level independently of disk persistence and apply it consistently to completion, interruption and teardown. Add a regression for clear with persistence disabled and a nonempty parent transcript; verify later snapshots omit it.

Focused reproduction (isolated Go overlay, no tracked source edits):

```text
go test -overlay .superpowers/record-recovery/review-overlay/overlay.json ./internal/gateway -run '^TestReviewClearDisabledDoesNotRestoreParentText$' -count=1
--- FAIL: TestReviewClearDisabledDoesNotRestoreParentText (0.00s)
    records_test.go:756: successful clear restored the old parent response into temporary context
FAIL
FAIL familyrobot/internal/gateway 0.649s
```

The overlay constructs an ordinary session with persistence disabled, a current explicit clear request and a nonempty tool-parent transcript, waits for the successful clear receipt, completes playback, and asserts the former transcript does not return in `hist.context()`.

## Reviewed areas without additional Important findings

- Authenticated normalized MAC isolation and same-device lease extend through final save admission. Pending clear completion precedes lease release; subsequent store operations are ordered through the already-reviewed FIFO worker.
- Clear/save work is asynchronous on the event loop; save admission and receipt states differ. Pending/failed capability facts are reevaluated before generation, and changed instruction content requires a fresh ACK.
- Recovery assigns a new session-scoped task identity, retains original and latest request sources and explicit user-start provenance, exposes historical samples separately, and leaves current playback position unknown. Restoration itself does not request execution.
- Discard/replace/clear retire old task ownership and expose disposition; interrupted draft text is stripped from injected history. Original request text is already bounded and marked truncated in `transcribeOrigin` (`playback_progress.go:247-248`), so the initially considered save-time truncation concern is not a finding.
- The gateway archive and injected history have explicit 128/12 bounds and per-item retention. These local bounds do not cure I1's native-conversation lifecycle gap.
- Configuration fails startup when storage cannot open, defaults retention to 168h, and uses a narrow systemd writable directory plus documented ownership/mode setup. Documentation distinguishes generation, transport, playback evidence and unknown text position.

The implementation report's complete Darwin race/vet passes were read but not independently rerun. No raw audio, credentials, runtime snapshot bodies or generated firmware were added to Git by this review.
