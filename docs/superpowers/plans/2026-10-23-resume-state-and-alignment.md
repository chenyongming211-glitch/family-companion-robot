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
