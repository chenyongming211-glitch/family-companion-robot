# Broad integration review at 2aaf3de

Reviewed candidate `2aaf3de2e1b10c56fb4e23b593fd142512c2f0e0`, Task 1d brief/report, supplied whole/incremental packages, relevant current source/tests, and the new clarify-choice probe/checker. The prior d210811 broad review remains evidence for unchanged code; this review concentrates on the new policy and cross-task integration. No source/index/HEAD mutation, branch/worktree, provider/server/device/microphone/playback action, or broad test rerun. Only this report and one named-risk scratch Go overlay probe were written.

## Verdict

**Needs two Important P2 fixes before promotion. No Critical finding.** The unknown-position policy is structurally sound, but its required trace evidence is silently discarded and replacement input can orphan a verified clarification's live playback block. Complete finding list follows; no additional material issue was found in the inspected integration or new tools.

## P2: Required clarification trace is dropped by the event allowlist

`internal/gateway/clarification.go:58,181` calls `traceVoice("clarification_rejected")` and `traceVoice("clarification_verified")`, but neither enum is accepted by `internal/gateway/voice_trace.go:12-17`. Both calls return without logging. The new actual-evidence checker at `tools/voice-audit/check_clarify_choice.py:46-48` requires exactly one `clarification_verified`, so a correctly generated/validated question cannot pass that checker using real gateway logs. Its synthetic test fixture supplies the event manually and therefore does not cover this producer/consumer mismatch. The implementation report's claim that the trace is emitted before TTS start is not true at this candidate.

Add the fixed enums to the existing privacy-safe whitelist and exercise the actual guard/trace path, verifying one verification event precedes TTS start, rejection is recorded, and no request/transcript/credentials/audio leak. No broad logging expansion is needed. Root independently confirmed this finding during review and is holding native final testing for consolidated fixes.

## P2: Replacing a verified playing clarification leaves its device TTS block open

`internal/gateway/playback_progress.go:210-213` calls `clearClarification` for a replacement committed input. That cleanup at `internal/gateway/clarification.go:42-55` clears pending audio, response ID, response completion and transcript, but does not send stop, mark/retire interrupted history, seal the playback record, or reset `started`/`activePlayback`. This is valid for a question still held before TTS start, but the guard deliberately stays active throughout verified playback. Once its first packet has played, the same cleanup strands an already-open block. `play()` then returns on empty response ID, so its normal stop/sealing path cannot execute. While replacement transcription/classification is pending, the device remains in the old speaking block; a later response can start a second block without a matching old stop. A late old final is also rejected as premature because its record remains unsealed. This violates the new-input cancellation and latest-playback/history integration boundary.

Reproduction used a scratch-only Go overlay against the candidate, with the actual local WebSocket playback/classifier fixtures: validate a three-frame fixed question, release its first frame, deliver `input_audio_buffer.committed` for a replacement item, then tick playback. Expected old stop/closed playback; actual events `[]`, `started=true`, `responseID=""`, active record `sealed=false`, `interrupted=false`, with one sent frame. Test `TestReviewCommittedInputStopsAlreadyPlayingQuestion` fails in 0.640 s. No provider or hardware is involved. The normal VAD path usually interrupts first, but direct replacement commits are explicitly supported by this source and by the prior input/ACK fix; they must preserve playback invariants too.

Evidence:

- `.superpowers/resume-state-alignment/final-review-2aaf-committed-question_test.go`
- `.superpowers/resume-state-alignment/final-review-2aaf-committed-overlay.json`
- `.superpowers/resume-state-alignment/final-review-2aaf-committed-question.log`

Command: `go test -overlay .superpowers/resume-state-alignment/final-review-2aaf-committed-overlay.json ./internal/gateway -run '^TestReviewCommittedInputStopsAlreadyPlayingQuestion$' -count=1 -v`.

Fix needs distinct held-only cleanup and already-admitted playback cancellation. Stop/seal/history bookkeeping must precede clearing the verified question's state, without fabricating a TTS block for held-only output. Preserve the new committed item, original task/awaiting-choice fact, accepted future route, and monotonic ACK FIFO; blindly reconnecting at this point would discard the input whose commit is being processed. Regressions should cover held-only replacement, verified-but-not-started replacement, and replacement after one frame with pending frames, plus the next response's balanced start/stop and old final handling.

## Source integration assessed as correct outside those findings

- Ordinary unknown-position resume selects a fixed one-response clarification policy after accepted isolated classification and current snapshot ACK. Explicit restart/user-start map to the same task binding with distinct response policy, preserving original endpoint and distinguishing user choice from measured playback (`generation_sync.go:110-123`, `task_routing.go:145-175`). No timing/text-length/number regex oracle was added.
- Awaiting-choice is gateway-owned, included in the task snapshot and copied to the classifier. Changes revise/revalidate state; side answers preserve it, explicit restart/start clears it, discard/replace retire the task. The six-enum classifier retains separate text-only session, strict completed output, cancellation and bounds. Current route tokens, identity/revision gates and the d210 ACK FIFO correction remain in place.
- The guard holds native PCM before any playback pop; requires positive even complete PCM, exact fixed transcript modulo only whitespace/Unicode punctuation, complete audio and successful matching response. Its 15-second decoded-byte and 8-second generation bounds are explicit. Optional item/part/final envelopes reject hidden tools/text/wrong identities/extra outputs. The completed guard stays active through playback, preventing late output injection. Explicit abort/VAD tests exercise stop before reconnect and retain interrupted question history. These are transcript-policy checks, not acoustic verification.
- The released question still uses the existing 60 ms scheduler. Classifier output cannot enter the voice context; fixed clarification instructions are a separate user-facing voice policy. No auxiliary ASR, framework, synthesis service, aligner runtime or firmware change is introduced. The offline aligner and its independent acoustic attestation/calibrated mapping requirement are unchanged.
- New `clarify-choice` probe schedules ordinary continue followed by a synthetic user start, retains loopback-only isolated endpoint restriction, records synthetic receipt/user-choice provenance and rejects unexpected actuation. The checker distinguishes fixed-question validation/task ownership from final semantic review and physical words heard. Reuse of snapshot-order checks is consistent; the missing actual trace producer is the only material new tool integration finding. Final generated start/endpoint must still be reviewed independently as the checker explicitly requires.

## Verification and acceptance boundaries

Inspected actual retained Task 1d full Mac Go log (all packages pass, gateway 39.862 s), race log (all pass, gateway 43.205 s; known C/Opus LC_DYSYMTAB warnings, no Go race report), and zero-byte vet log. Root reports Python 25 PASS and exact-2aaf Linux race/vet/build PASS with gateway hash prefix `0407e39f`; these are controller-owned evidence. They do not cover the newly reproduced replacement schedule or missing trace allowlist.

Root has not deployed this candidate and is holding final native tests for the consolidated fixes. Earlier d210 native state success with plain continue restarting 1–50 is correctly retained as a policy failure, not accurate resume or permission to restart. Final corrected candidate identity, native clarification/choice and repeated ownership tests, normal/manual regressions, idle-only deploy/rollback/health/hash evidence remain root-owned. Exact word position and physical acoustic acceptance remain open.
