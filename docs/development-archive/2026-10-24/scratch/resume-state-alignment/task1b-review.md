# Independent Task 1b review

Reviewed committed source `4e785d6aef5c16abf5fe51014921833e778843aa`, the Task 1b plan and Global Constraints, native findings document, implementation diff/report and routing tests. Review-only work; no source changes, tests rerun, model/provider calls, remote execution, devices or audio actions.

## Verdict

**Changes required: one material P2 finding.** The gateway now actually enforces a routing decision and preserves its binding, instead of trusting the provider's `required` echo. The remaining issue is upstream context contamination by unheard route-phase output. No other material source-level finding was identified in this review. Native exact-candidate acceptance remains independent and pending.

## P2 — Dropping route audio locally leaves unheard content in the upstream conversation

Locations: `internal/gateway/session.go:575-599` and `internal/gateway/task_routing.go:71-120` (local buffer clearing at 93-99, accepted/retry continuation at 106-120).

The new handlers discard routing audio and audio-transcript events locally, but the provider is still generating on the same upstream conversation. `finishRouting` clears only gateway buffers/transcript fields and then requests another response on that connection. It neither establishes that the routing attempt contained only a function call nor removes any assistant content the provider generated. Text-output events are not explicitly considered either.

Concrete supported failure paths:

1. A provider ignores the routing-only instruction and emits answer audio/text with no function. The gateway discards this output, then retries routing on the same conversation. A later valid routing function succeeds and releases voice, although the earlier unheard answer remains upstream.
2. A provider emits a valid route function **and** answer audio/text in the same response. The current implementation accepts the decision immediately at response completion and releases voice on the contaminated conversation, without even requiring a retry.

The official/native response conversation is not cleared by discarding gateway events. Existing local investigation already found same-connection generated context can cause the model to believe a task has been completed; this is precisely why unplayed drafts need conservative treatment. The actual final voice can omit or refer back to words never sent to the device. This is a context-safety defect even when playback remains correctly blocked during the routing phase.

Minimal conservative correction: flag any nonempty route-phase audio, transcript or text content as contamination, and fail closed before applying a task mutation or requesting retry/voice on that upstream context. Include the provider's text delta/done forms as well as audio transcript delta/done and PCM delta, even with stable audio/text modalities. A clean route containing only a valid function can proceed normally. Avoid adding an unproven delete/truncate/replay strategy merely to recover this path.

Required regression cases: content-only route (currently retryable); valid function plus audio; valid function plus transcript/text (including done-only content). Assert no route state mutation and no subsequent retry or voice `response.create` on the contaminated conversation. Empty content events can remain harmless. Existing missing/invalid/stale **content-free** route retry tests should continue to pass.

## Other review conclusions

- **Actual decision enforcement:** a paused new input selects only the routing tool, uses `required` as a provider request, and Go independently validates function name, argument shape, enum, singleton invocation and completed response. Missing/invalid/stale paths do not silently release voice. This addresses the native finding that the provider can ignore `required`.
- **Identity and revisions:** input commit resets route and continuation state. At route response creation request, Go captures task ID, current revision and input generation/item. Finish revalidates those values before mutation. The model does not copy identity/revision. Accepted resume/replace binds the voice response; side stays unbound. Subsequent new playback and device-tool continuations preserve the binding. Three sequential interruption/resume tests cover ownership, although exact native repetition remains necessary.
- **ACK ordering:** routing configuration and normal-tools restoration each wait for the shared FIFO ACK gate; zero-token mode updates cannot unlock generation. Changed instructions/snapshot refresh before creation. Premature routing or post-routing voice `response.created` fails closed. Normal mutable tool/instruction controls are restored without touching immutable VAD/audio/modalities.
- **Failure bounds:** three completed missing/rejected/stale attempts; eight-second route-response deadline; existing snapshot-ACK timeout. Route timeout is checked even when no generation request is pending. Cancellation follows the existing immediate stop/reconnect path, clears route state and isolates old upstream generations. A different input overtaking unresolved routing/voice is rejected rather than reusing prior classification.
- **Tools:** route functions do not enter the device bridge; normal device tools return after restoration. Advertised normal tools omit task mutation, and a legacy mutation after a route decision returns an error. Local internal mutation remains available for Go application. Shared-block ownership behavior is retained.
- **History and lifecycle:** the previously reviewed explicit-resolution task-retention and retired shared-block sanitation fixes remain intact. No sample-to-word estimate or semantic completion inference was introduced. The contamination finding above is a separate same-connection context path.
- **Wake/manual/ordinary path:** wake greeting is exempt from task routing, VAD remains gateway-created, and manual capture remains unchanged. Full-suite coverage includes these paths. Native modalities are intentionally stable based on the recorded probe result; no inference is made that all providers reject modality switching.
- **Privacy and timing:** new route traces contain fixed action/outcome enums and identity counters rather than raw arguments/content. `route_elapsed_ms` excludes initial snapshot and final restore-ACK overhead; full added response latency must use broader event timestamps and remains distinct from physical stop latency.

## Evidence boundary

The implementer records genuine pre-change failures, focused PASS and final full Go (gateway 37.813 s), race (40.859 s), vet and diff-check PASS. I inspected the report and relevant tests, without repeating those runs. Existing synthetic fixtures deliberately inject response events; their PASS does not establish native function reliability. Linux/native checks on the final fixed commit are parent-owned. No hardware, audibility, word-position, stable semantic fulfillment or production-readiness claim follows from this review.

## Re-review of `1bdc325` — approved for Task 1b source scope

**The sole P2 is resolved. No additional material source-level finding in this correction.** Reviewed the four-file committed diff `4e785d6..1bdc325`, actual event/finish guards, updated report, failure-count evidence and focused PASS log. This supersedes the changes-required verdict above for the implementation; exact-source Linux/native acceptance remains parent-owned and pending.

- All five relevant nonempty content forms now fail immediately while routing is active: PCM/audio delta, transcript delta, transcript done, text delta and text done (`session.go:575-615`). The Qwen event type now decodes `text`, so a done-only text payload cannot disappear during unmarshalling. Empty events remain harmless.
- `rejectRoutingContent` sets a sticky contamination flag and returns an error; the existing session event loop returns on that error and closes the upstream connection. `finishRouting` independently checks the flag before classification application, function output, retry or voice continuation (`task_routing.go:73-87`). A caller ignoring the initial error cannot cause the normal route-completion path to mutate ownership or reuse that context. No speculative provider delete/truncate/replay mechanism was introduced.
- Regression coverage exercises five content types, three orderings (content-only, content before function, function before content), and all four route actions: 60 cases. Assertions cover immediate rejection, defensive completion rejection, unchanged task/revision/binding, absent playback and absent outgoing continuation. The recorded pre-fix log contains these 60 failing subtests plus their parent failure. The corrected focused test log reports PASS. Empty-content success and existing content-free missing/rejected/stale retries remain covered.
- Updated report records final full Go PASS (gateway 38.556 s), race PASS (41.383 s), vet PASS and diff-check PASS. Reviewer did not duplicate these runs or invoke native/provider/device operations.
- The conservative availability tradeoff is intentional: a provider that emits route content terminates that attempt/session instead of receiving a best-effort continuation. Whether clean function-only routing is reliable enough in the complete native flow must be measured separately. Parent-reported acknowledgement-only responses before routing, and any stronger counting-input fixture, must remain separately labelled evidence; neither changes this source-safety verdict or proves physical word alignment.
