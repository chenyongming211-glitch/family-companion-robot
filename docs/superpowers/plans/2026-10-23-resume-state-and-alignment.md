# Resume State and Alignment Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development to implement and review each task. User approved execution of the preceding design on 2026-10-23.

**Goal:** Make paused-task state authoritative and current before generation, preserve task ownership through repeated interruptions, and experimentally establish whether forced alignment can supply a defensible playback text boundary.

**Architecture:** The Go session event loop owns task identity, playback segments and monotonically revised snapshots. Gateway-controlled response creation synchronizes that snapshot before generation; an isolated text classifier identifies the user intent and the gateway applies resume/discard/replace using captured task identity and revision. An isolated forced-aligner experiment evaluates output PCM/text coverage and timing without changing the native speech chain or guessing a word position.

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

- [x] Add failing tests reproducing absent auto-context, late-final snapshot refresh, second-interruption ownership, side-answer protection and stale revision rejection. Include a fake real WebSocket peer that verifies `session.update` precedes `response.create`, with current snapshot after a late final.
- [x] Run these tests on the old implementation and record genuine failures.
- [x] Implement revised state, explicit resume action, original request preservation, latest-segment tracking, automatic snapshot injection and response creation gate. Use `create_response=false` for VAD with explicit gateway creation, preserving all VAD mode transitions and wake handshake. Refresh instructions without resending immutable VAD options.
- [x] Rebuild interrupted history conservatively: omit unplayed draft from model context; retain internal diagnostics as needed; expose known/unknown text-position contracts without unconditional contradictory instructions. Do not fabricate played text for a fully confirmed audio block unless its transcript correspondence is validated.
- [x] Cover resume/discard/replace lifecycle, repeat interruption, duplicate/out-of-order finals, missing final timeout, old generation/input identity, bounded history eviction, multi-tool continuation, wake prelisten and manual mode.
- [x] Run targeted tests, complete Go suite, race and vet; document scope and commit only owned files on main.

## Task 2: Forced alignment feasibility and safe boundary contract

**Files:** Add tools/voice-audit/forced_alignment_probe.py, focused Python tests and docs/verification/2026-10-23-forced-alignment.md. Isolated environment, weights, generated audio and detailed output belong under .superpowers/resume-state-alignment/ or artifacts/.

**Interfaces:** Offline only. Input is output audio plus its matching text, output is word/character start/end times and explicit validation outcome. Any downstream projection must require matching playback identity, monotonic non-overlapping spans, coverage validity and a documented audio-coordinate conversion; unknown on failure.

- [x] Read official primary source and record exact package/model revisions and installed runtime identity.
- [x] Add tests before implementation for valid completed prefix, a cut inside a word, invalid/nonmonotonic/out-of-bounds spans, wrong playback identity, and incomplete audio with full text. Missing coverage must never produce a confident endpoint.
- [x] Implement a reproducible probe using the actual official aligner in an isolated environment. Run Chinese counting and ordinary speech, including silence and intentionally incomplete audio/full-transcript cases. No sound or microphone.
- [x] Measure cold initialization, warm inference latency, audio duration, memory and backend. If hardware/dependency/model access prevents a real run, record the concrete failure and leave model feasibility unproven; do not silently replace it with ASR or a mock result.
- [x] Explain whether alignment can be online, post-interruption only, or remains unproven. Clearly separate candidate model timestamps from ground-truth validated word boundaries and from device codec/I2S coordinates.
- [x] Run Python tests, record exact results and commit only owned scripts/tests/report.

## Task 3: Integration verification, release and evidence

**Files:** Add docs/verification/2026-10-23-resume-state-and-alignment.md; update protocol/README only where the actual behavior changes.

- [x] Independently review Task 1 and Task 2 against this plan and fix material findings. Final re-review approved 07c6549 after HTTP Upgrade cancellation, input/ACK crossover, clarification trace and superseded playback fixes.
- [x] Test actual Qwen VAD configuration and generation ACK gate with an isolated candidate and machine-generated inputs; no watch connection or recording. Include interruption, original goal retention, side question, resume and second interruption. Unknown text position is an expected limitation, not an accurate-resume pass.
- [ ] Run Mac Go tests/race/vet and Linux checks/build through the established server workflow. Check exact source identity, sanitize evidence and scan tracked diff for secrets.
- [x] Deploy only a passing gateway candidate after verifying no active device session, retain rollback, verify health and live binary hash. If active hardware traffic exists, preserve it and report the validated candidate without interrupting the user.
- [ ] Commit/push final docs and report code, CI/build, native-provider results, alignment feasibility and field acceptance separately. Leave physical word-position and acoustic acceptance explicitly open until measured.

## Task 1b: Rejected native experiment, superseded by Task 1c

- [x] Trace the missing second binding with metadata-only diagnostics and preserve both failing native traces.
- [x] Implement and deterministically test same-conversation restricted tool routing with bounded validation, snapshot ACKs and a contamination guard.
- [x] Test actual native behavior: required is accepted/echoed but does not force calls; candidate `1bdc325` emits routing content and correctly fails closed. This candidate was rejected and never deployed.
- [x] Preserve these failures in the routing findings report and replace the unsuitable same-conversation design with Task 1c below. These checkmarks record completed investigation, not successful native acceptance of Task 1b.

## Task 1c: Native-test finding — isolate task classification from the voice conversation

The Task 1b candidate passes deterministic checks but the real provider emitted an answer in the main conversation even under `tool_choice=required`. The guard correctly closed the candidate; it was not deployed. A fresh text-only native session with explicit response instructions classified 12 synthetic transcripts correctly using a strict enum. This supersedes the same-conversation route/tool implementation, without adding ASR.

- [x] Add regressions before implementation for transcript readiness and identity, independent classifier output parsing, cancellation, stale snapshot retry, route failure bounds and main-context isolation.
- [x] Replace same-conversation routing with a fresh text-only Qwen session consuming the existing matching input transcription and paused original request. Send no microphone or audio to that session, request no ASR, advertise no tools. Request exactly side/resume/discard/replace with response.instructions (extended to six enums in Task 1d); accept only one exact trimmed enum from a completed response, with bounded bytes/time and no audio/function calls. Do not use keyword guesses or a fallback that silently resumes.
- [x] Keep the gateway event loop as the only state writer. An asynchronous classifier result must match its cancellation token, generation, input identity, task ID and captured revision. Cancel on reconnect/abort/disconnect/new input. A stale revision must reclassify within at most three total attempts. Await existing transcription under an eight-second deadline and reject missing/truncated/anonymous routing input conservatively. All classifiers must terminate/close their WebSocket on cancellation.
- [x] Keep the main native voice session's original role, tools and modalities throughout. Never send classifier output/function messages into that context. Apply accepted ownership in Go, then wait for current-snapshot session.updated before response.create. Keep side reply isolation, explicit discard/replace and same-task binding through all device-tool continuations.
- [x] Test real native two-interruption/two-resume ownership with five-second delayed finals, plus normal and manual abort regressions. Preserve failed attempts. Build and review the exact final candidate before considering an idle-only deployment.

## Task 1d: Native finding — enforce unknown-position clarification

The final source d210811 passes task ownership, but a plain continue native test restarts1–50 without first asking. It must not be represented as accurate continuation or complete fallback. Before release, make this existing unknown-position requirement concrete without ASR or hard-coded counting.

- [x] Extend isolated text routing with strict explicit restart/user-start choices, using existing transcription and an optional gateway-owned awaiting-choice fact; root must first verify the extended enum protocol with actual native requests. Preserve current action bindings and revision/input gates.
- [x] When position is unknown and the user has not provided a start or permission to replay, request one fixed honest clarification in the native voice response. Buffer that short response until its completed transcript matches the fixed text (punctuation/space normalization only), before any audio/TTS start. Reject extra tools/text/audio over bounded size/time, no unheard-content reuse on failure. Do not introduce a separate STT/LLM/TTS chain or guess a point.
- [x] Explicit replay/user-provided start may generate actual task content within the original goal; side questions remain isolated. Bare start answers can be understood only with gateway-owned awaiting-choice state, not manufactured playback evidence.
- [x] TDD for held output, mismatch failure, deadlines, explicit modes, side/cancel/revision and repeated tasks; full checks/review/exact Linux/native regressions before release. Current unknown-point native output remains preserved as a failed policy sample.
