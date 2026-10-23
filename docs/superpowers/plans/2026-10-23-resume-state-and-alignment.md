# Resume State and Alignment Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development to implement and review each task. User approved execution of the preceding design on 2026-10-23.

**Goal:** Make paused-task state authoritative and current before generation, preserve task ownership through repeated interruptions, and experimentally establish whether forced alignment can supply a defensible playback text boundary.

**Architecture:** The Go session event loop owns task identity, playback segments and monotonically revised snapshots. Gateway-controlled response creation synchronizes that snapshot before generation; model tools explicitly resume/discard/replace a matching task. An isolated forced-aligner experiment evaluates output PCM/text coverage and timing without changing the native speech chain or guessing a word position.

**Tech Stack:** Go, gorilla/websocket, libopus; isolated Python virtual environment and official Qwen3-ForcedAligner-0.6B for the experiment.

## Global Constraints

- Develop, commit and push directly on main; no new branch or worktree.
- No auxiliary ASR, framework migration, firmware flashing, microphone recording or audible playback in this execution.
- Credentials, raw audio, weights, build products and private evidence stay in ignored directories.
- Never derive heard words from generated-text length, packet arrival time, a fixed speaking rate or an oracle pretending to be measured audio.
- Device samples describe I2S delivery; exact word position remains unknown until a validated alignment in the correct playback coordinate is available.
- Immediate stop/reconnect must not wait for alignment or final playback settlement.
- Preserve wake-origin identity, device controls, 60ms audio cadence and stale-generation isolation.
- Synthetic tests, native-provider tests and physical-device acceptance must be reported separately.

## Task 1: Authoritative task state and generation synchronization

**Files:** Modify internal/gateway/playback_progress.go, session.go, session_tools.go, history.go, playback.go and internal/qwen/client.go as needed. Add focused resume-state and WebSocket synchronization tests; new small gateway files are preferred for separate responsibilities.

**Interfaces:** Gateway session remains the single writer. Expose a monotonically revised paused-task snapshot to both tools and upstream context. Keep original request separate from latest playback origin. Introduce explicit resume ownership (task ID plus expected revision), allowing each resumed block to bind to the same task while unrelated answers stay unbound. Native response creation must wait for session.updated for the current snapshot; stale ACKs cannot authorize stale generation. Use a bounded event-loop settlement deadline for outstanding interrupted finals, with explicit unsettled/unknown fallback.

- [ ] Add failing tests reproducing absent auto-context, late-final snapshot refresh, second-interruption ownership, side-answer protection and stale revision rejection. Include a fake real WebSocket peer that verifies `session.update` precedes `response.create`, with current snapshot after a late final.
- [ ] Run these tests on the old implementation and record genuine failures.
- [ ] Implement revised state, explicit resume action, original request preservation, latest-segment tracking, automatic snapshot injection and response creation gate. Use `create_response=false` for VAD with explicit gateway creation, preserving all VAD mode transitions and wake handshake. Refresh instructions without resending immutable VAD options.
- [ ] Rebuild interrupted history conservatively: omit unplayed draft from model context; retain internal diagnostics as needed; expose known/unknown text-position contracts without unconditional contradictory instructions. Do not fabricate played text for a fully confirmed audio block unless its transcript correspondence is validated.
- [ ] Cover resume/discard/replace lifecycle, repeat interruption, duplicate/out-of-order finals, missing final timeout, old generation/input identity, bounded history eviction, multi-tool continuation, wake prelisten and manual mode.
- [ ] Run targeted tests, complete Go suite, race and vet; document scope and commit only owned files on main.

## Task 2: Forced alignment feasibility and safe boundary contract

**Files:** Add tools/voice-audit/forced_alignment_probe.py, focused Python tests and docs/verification/2026-10-23-forced-alignment.md. Isolated environment, weights, generated audio and detailed output belong under .superpowers/resume-state-alignment/ or artifacts/.

**Interfaces:** Offline only. Input is output audio plus its matching text, output is word/character start/end times and explicit validation outcome. Any downstream projection must require matching playback identity, monotonic non-overlapping spans, coverage validity and a documented audio-coordinate conversion; unknown on failure.

- [ ] Read official primary source and record exact package/model revisions and installed runtime identity.
- [ ] Add tests before implementation for valid completed prefix, a cut inside a word, invalid/nonmonotonic/out-of-bounds spans, wrong playback identity, and incomplete audio with full text. Missing coverage must never produce a confident endpoint.
- [ ] Implement a reproducible probe using the actual official aligner in an isolated environment. Run Chinese counting and ordinary speech, including silence and intentionally incomplete audio/full-transcript cases. No sound or microphone.
- [ ] Measure cold initialization, warm inference latency, audio duration, memory and backend. If hardware/dependency/model access prevents a real run, record the concrete failure and leave model feasibility unproven; do not silently replace it with ASR or a mock result.
- [ ] Explain whether alignment can be online, post-interruption only, or remains unproven. Clearly separate candidate model timestamps from ground-truth validated word boundaries and from device codec/I2S coordinates.
- [ ] Run Python tests, record exact results and commit only owned scripts/tests/report.

## Task 3: Integration verification, release and evidence

**Files:** Add docs/verification/2026-10-23-resume-state-and-alignment.md; update protocol/README only where the actual behavior changes.

- [ ] Independently review Task 1 and Task 2 against this plan and fix material findings.
- [ ] Test actual Qwen VAD configuration and generation ACK gate with an isolated candidate and machine-generated inputs; no watch connection or recording. Include interruption, original goal retention, side question, resume and second interruption. Unknown text position is an expected limitation, not an accurate-resume pass.
- [ ] Run Mac Go tests/race/vet and Linux checks/build through the established server workflow. Check exact source identity, sanitize evidence and scan tracked diff for secrets.
- [ ] Deploy only a passing gateway candidate after verifying no active device session, retain rollback, verify health and live binary hash. If active hardware traffic exists, preserve it and report the validated candidate without interrupting the user.
- [ ] Commit/push final docs and report code, CI/build, native-provider results, alignment feasibility and field acceptance separately. Leave physical word-position and acoustic acceptance explicitly open until measured.

## Task 1b: Native-test finding — make task routing a prerequisite

Native071a23f test resumed twice and generated the expected final count, but only the first resume bound its output to the original task. Optional model mutation calls are therefore insufficient, even with current snapshots. This is a failed ownership acceptance, not an accurate-resume pass. Production remains d68ec44.

- [ ] First trace the missing binding with a metadata-only diagnostic (tool/action/outcome/revision, no arguments or user text). Preserve the failing native trace.
- [ ] Add a failing deterministic peer test where a paused-task turn emits audio without a successful ownership decision, and one where a stale decision races a late final. Neither may be silently accepted as a valid resumed task.
- [ ] Implement enforced structured routing whenever a saved task exists for a new user input. Use a dedicated side/resume/discard/replace classification tool, a restricted tools list and native `tool_choice=required`; explicit identity/revision application belongs to the gateway rather than model-copied fields. The real provider must demonstrate forced function behavior, not just acknowledge an unknown setting.
- [ ] Wait for accepted routing and latest-snapshot ACK before releasing voice generation. Suppress routing-phase audio/draft output. Restore normal tools with `tool_choice=auto` for the actual voice response and device tools. Keep ordinary no-paused-task turns and wake greeting unchanged.
- [ ] Revalidate a classification against its captured task/revision. If late final changes it, resynchronize and retry within a small fixed bound; never let stale arguments or a missing tool decision establish ownership. Preserve same-task binding through device-tool continuations and new playback, and isolate side replies.
- [ ] Cover route missing/rejected/stale, new input/abort during route, ordinary manual/wake paths, device tools and second/third interruption. Run focused and complete Go/race/vet tests, independent review, then exact-source Linux/native retests. Report added routing latency separately from physical interruption timing.


## Task 1c: Native-test finding — isolate task classification from the voice conversation

The Task 1b candidate passes deterministic checks but the real provider emitted an answer in the main conversation even under `tool_choice=required`. The guard correctly closed the candidate; it was not deployed. A fresh text-only native session with explicit response instructions classified 12 synthetic transcripts correctly using a strict enum. This supersedes the same-conversation route/tool implementation, without adding ASR.

- [ ] Add regressions before implementation for transcript readiness and identity, independent classifier output parsing, cancellation, stale snapshot retry, route failure bounds and main-context isolation.
- [ ] Replace same-conversation routing with a fresh text-only Qwen session consuming the existing matching input transcription and paused original request. Send no microphone or audio to that session, request no ASR, advertise no tools. Request exactly side/resume/discard/replace with response.instructions; accept only one exact trimmed enum from a completed response, with bounded bytes/time and no audio/function calls. Do not use keyword guesses or a fallback that silently resumes.
- [ ] Keep the gateway event loop as the only state writer. An asynchronous classifier result must match its cancellation token, generation, input identity, task ID and captured revision. Cancel on reconnect/abort/disconnect/new input. A stale revision must reclassify within at most three total attempts. Await existing transcription under an eight-second deadline and reject missing/truncated/anonymous routing input conservatively. All classifiers must terminate/close their WebSocket on cancellation.
- [ ] Keep the main native voice session's original role, tools and modalities throughout. Never send classifier output/function messages into that context. Apply accepted ownership in Go, then wait for current-snapshot session.updated before response.create. Keep side reply isolation, explicit discard/replace and same-task binding through all device-tool continuations.
- [ ] Test real native two-interruption/two-resume ownership with five-second delayed finals, plus normal and manual abort regressions. Preserve failed attempts. Build and review the exact final candidate before considering an idle-only deployment.
