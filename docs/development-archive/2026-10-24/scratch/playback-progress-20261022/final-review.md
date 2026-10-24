# Whole-stage independent review

Reviewed immutable base `5bae4eabfae8357eb2a1090fe33334708ea8b81e` → head `0b5eab01526f38485f4d66b527364dda67af231e`. The working tree was clean and HEAD matched the supplied review target. Scope includes the full stage diff, Global Constraints/Task 3, final protocol, firmware production overlays/patch and native tests, Go session/ledger/tool/history paths and behavior tests, mapping oracle, task reports/reviews, final verification document and candidate metadata. Existing task reviews were consulted after independently reading the relevant implementation; they were not substituted for this review.

## Verdict

- **Specification compliance: CHANGES REQUIRED.** One new P2 crosses the otherwise separately reviewed firmware and gateway tasks: transport seal retires the gateway's interruptible task before device playback has necessarily drained.
- **Code quality / source candidate delivery gate: CHANGES REQUIRED for the same P2.** No additional important actionable issue was identified in the reviewed stage. This is a bounded repair to playback lifecycle and original-task association, not a request for ASR, text alignment, or a different architecture.
- **Existing host/target build evidence: verified as recorded evidence, not invalidated by this review finding.** Successful compilation does not close the behavioral finding. A repaired source revision needs corresponding validation and candidate identity updates.
- **Physical/production acceptance: NOT VERIFIED.** No device, serial, production, raw audio or recording access was performed during this review. The current code cannot claim exact text resumption or acoustic success.

## P2 — Preserve interruptible ownership after normal transport stop until device drain is resolved

Primary location: `internal/gateway/playback.go:82-91`. Related paths: `internal/gateway/playback_progress.go:51-59,93-99`; `internal/gateway/session.go:373-387`.

The firmware's normal stop deliberately only seals an identity (`firmware/overlays/main/audio/playback_websocket.inc:62-70`). Decoder reservations, queued PCM and DMA labels can still exist; `PlaybackProgress::Finished` waits for all of them. A subsequent local reset can therefore legally interrupt a sealed, not-yet-drained block (`playback_progress.h:96-101`) and report a valid partial final prefix after the remaining DMA labels complete.

The gateway currently clears `activePlayback` immediately after its normal stop and clears `started` in the same `play()` completion path. Consequently:

1. A later explicit device abort calls `pausePlayback`, which immediately returns because the active record is nil. Its interrupted stop also omits `playback_id`, because that field is populated only from an active started block. Negotiated firmware rejects the identifier-less stop. A local device abort may already have reset its own queue, but the gateway still loses the paused original request.
2. A later upstream `speech_started` invokes the non-force `interrupt` path. With response ID/started/awaiting/tools all cleared, the guard returns without sending any matched interrupted stop. Device software/DMA draining is not represented by that guard.
3. A legitimate subsequent `finished, interrupted=true` is accepted into the retained ledger record, but `receivePlayback` only updates an already-existing paused snapshot. It never creates the missing original-task snapshot. The local query keeps returning `available=false` even though the interrupted playback's origin and final partial count are available.

This is not a claim that a normal firmware run always has a large backlog, nor an acoustic inference. Network/decode/scheduling delay is allowed by the design, and a server waiting one frame duration after its last send does not prove the firmware has completed its queue. The problem is precisely the cross-layer distinction this stage introduced: normal transport stop and device completion are separate events.

### Independent focused reproductions

Only the new concern was exercised; no previously passed unchanged suite was repeated. Both artifacts are in the ignored `final-review-repro/` directory; no product file, Git index or HEAD changed.

1. **Actual gateway methods and WebSocket transport:** the overlay test `TestFinalReviewAbortDuringSealedDeviceDrainPreservesTask` sends a committed/transcribed original request, a three-frame response, receives normal stop, sends explicit abort, reconnects the model, then sends a correctly identified valid interrupted final of 1440 out of 4320 samples. A device-originated mode update provides a cross-socket barrier before querying the local tool.

   Command:
   `go test -overlay .superpowers/playback-progress-20261022/final-review-repro/overlay.json ./internal/gateway -run '^TestFinalReviewAbortDuringSealedDeviceDrainPreservesTask$' -count=1`

   Result: **Exit 1**, deterministic assertion:
   `normal transport seal discarded still-playing original task: map[available:false ok:true text_position:unknown]`.
   Runtime trace confirms normal `tts_stop`, then `device_abort` with `response_open=false speaking=false`, followed by the query on upstream generation 2.

2. **Actual firmware production core:** `final-review-repro/sealed-drain.cc` includes the real `playback_progress.h`, admits three frames, leaves decoder/software work outstanding, normally seals, then interrupts and completes the already-labelled DMA tail. Built with C++17, `-Wall -Wextra -Werror -pedantic -fsanitize=address,undefined`.

   Result: **Exit 0**, saved `sealed-drain.log`:
   `sealed-before-interrupt: valid=1 finished=1 interrupted=1 played=720 received=4320`.
   This establishes that a valid partial interrupted final after normal seal is generated by the production state machine, not merely an invented parser input.

### Required repair and acceptance

Retain a bounded, identity-specific representation of negotiated device playback that remains interruptible after transport seal, until completion/invalidity is resolved. On actual abort or applicable upstream speech interruption, send an interrupted stop with the correct playback identity and preserve that playback's original request; keep the existing single-paused-task rule. Avoid selecting an unrelated newer block or resurrecting a normally settled completed task. A late receipt must refine the intended snapshot without overwriting an older paused original task. Do not merely create a snapshot after a late interrupted receipt while leaving the real stop-control path unable to identify the draining block.

Regression coverage should include explicit abort and `speech_started` during sealed draining; matched stop identity; final receipt before versus after abort processing; normal settled completion remaining unpaused; an existing paused original surviving a side-answer interruption; and old/new playback identities not crossing. Keep the ledger bound and legacy unknown-position behavior.

## Cross-layer checks with no additional important finding

- Negotiation remains two-sided. Gateway session IDs are short printable generated identifiers compatible with the firmware's 64-byte bound. Per-block uint32 identities, 24 kHz/1440-sample frames, padded final frames, monotonic progress, and terminal invalidity agree between C++ and Go. Binary Opus framing is unchanged. Sample offsets are captured at ingress and travel through decode/output/DMA; they are not read back from a mutable current reply.
- The pinned SDK callback source (`idf61-i2s-common.c:824-870`) passes the completed descriptor's `buf` to `on_sent` before its optional clear. Software-AEC disables both clears, owns refill exclusively, and never runs the ordinary I2S writer on those channels. The adapter observes actual old PCM for AEC before refilling. The core uses address-tag ownership, accounts only continuous prefixes, retains interrupted DMA tails, waits for admitted decode/output work on normal seal, and invalidates gaps/format/cadence/hardware faults.
- Reset/output cancellation is serialized by the existing mux with generation tokens. The ISR has fixed storage and no allocation/network/log/mutex/atomic calls; submission waits occur outside the mux. The protocol mutex is not part of the ISR lock graph. Recreated ordinary channels retire partial callbacks before publishing fallback mode; original and resource-failure paths preserve the ordinary writer. The pinned codec library's software-volume conditional and ES8311 hardware DAC register setter support the documented output-volume reasoning.
- Closed-socket epochs and application cancellation leases guard delayed callbacks. Bounded progress polling emits outside the core lock. Dropped one-shot reports do not manufacture gateway confirmation. Final history promotion requires the matching normally sealed, valid, complete settled block and per-response audio-sent evidence; tool-parent/continuation sharing does not let a zero-audio continuation borrow confirmation.
- The gateway retains at most 16 logical playback records plus one paused snapshot; request text and input identity are capped. Session teardown clears state, upstream reconnect preserves the device ledger, telemetry does not refresh idle activity, and local task tools remain usable without device MCP. `replace` uses actual current STT, changes task identity, and conservatively keeps playback/text position unknown. The reviewed concern is the missing sealed-drain ownership, not the already covered active-playback pause path.
- Text position remains `unknown` in production. The supplied mapping script is a controlled annotation oracle with explicit coverage/identity assumptions, complete-phrase endpoints and gap behavior. Its results are not presented as Qwen-native annotations or an implemented exact-resume path; no auxiliary ASR or new recognition audio retention was introduced.

## Evidence identity and outstanding physical gates

The final verification document correctly distinguishes firmware source `17bb1e001f9f25926071a6a8871426ae52235e4a`, Go source `9f02a4de15d2e67c7fdb282bd4d51d86a7cafbc5`, and documentation-only reviewed head `0b5eab0`. Git confirms the firmware tree at both the built firmware commit and reviewed head is `ed32846d9c8d47f258c184adb7f7a7bf8ab243b7`; the intervening Go-to-head diff is documentation only. The saved CI index, per-variant verification JSON, Go verification JSON/Linux log, and final evidence document agree on those identities and on undeployed/unflashed status. The saved Linux log records complete race-test success and the reported binary hash. No new live CI or remote-service assertion was made by this review.

The two firmware candidate manifests/verification records separate app, bootloader, merged-image, partition and asset evidence, and explicitly leave current hardware slot/fuse/assets state unverified. Exact source/build evidence remains useful, but after the P2 repair the Go candidate must be rebuilt/reidentified; do not label the old Go binary as the repaired version.

Target SRAM/heap fragmentation and stack high-water marks, ISR duration/cadence under load, real device receipts, stop/drain interruption timing, AEC and ordinary voice/tool regressions, and physical sound remain pending an authorized field window. `sizeof`/static bounds and target compilation do not establish those measurements. I2S completion does not prove DAC/acoustic latency or human intelligibility. Accurate word-level continuation remains outside the demonstrated result.

**Complete important finding list: one P2 above; no P0/P1 or additional P2 identified.** A single focused repair/review wave can close the source candidate gate without expanding this stage's approved scope.
