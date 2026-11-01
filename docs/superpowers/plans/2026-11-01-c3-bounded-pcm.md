# C3 Bounded PCM Implementation Plan

> Use subagent-driven-development task by task with spec review followed by quality review. All workers use main; no branches/worktrees. User has authorized this design direction and physical validation after the build/resource gates.

**Goal:** Replace C3 Opus media with bounded PCM and streamed cues, enable full residual echo processing, and validate the actual device without losing its working fallback.

**Architecture:** Common Go Session negotiates PCM only at the Realtime entry. C3 owns fixed input/output slots and a separately verified Flash cue trailer; its original DMA/reference path remains authoritative. No VAD/drain-policy change.

**Tech stack:** Go1.27.3/libopus for cloud tests and cue conversion; locked IDF6.1/ESP-SR2.6.1; host C++ O2/ASan/UBSan; GitHub exact-target builds; USB on Mac only after verification.

## Task 1: Promote native PCM server transport

Files: internal/protocol/protocol.go; internal/realtimelive/{xiaozhi_entry.go,xiaozhi_wire.go,session.go,pcm_test.go}; tools/c3-pcm-assessment remains a frozen historical tool.

- [ ] Copy/adapt the approved trial assertions to production tests, including old parser rejection, authenticated PCM admission, strict1920/2880B frames, raw PCM preservation, wake isolation,7200-sample window, ASR/drain and output tail clocks.
- [ ] Run cloud baseline test to observe missing-format RED; implement the minimal trial adaptation with production names/comments. Public `ParseHello` stays Opus-only; new `ParseRealtimeHello` explicitly enables PCM. Do not change other policy or credentials.
- [ ] Run cloud `go test ./...` and focused `-race`; spec review then quality review. Commit only owned Go files on main; root pushes at a controlled checkpoint.

## Task 2: Fixed PCM storage and cue pack primitives

Files: firmware/overlays/main/audio/{c3_pcm_buffer.h,c3_pcm_cues.h}; firmware/tools/generate_c3_pcm_cues.py; firmware/tests/{c3_pcm_buffer_host.cc,test_c3_pcm_buffer.py,c3_pcm_cues_host.cc,test_c3_pcm_cues.py}.

- [ ] Add RED host contracts for exact slots, 512→960 assembly/tails, pool exhaustion, stale epochs, in-flight Reset retention, duplicate/old release, fixed metadata limits and no heap allocation in the pure pool.
- [ ] Implement checked APIs for PushInput/PopInput, QueueOutput/TakeOutput/ReleaseOutput and Clear with fixed arrays; caller owns locking. Preserve a lease serial and fail closed at exhaustion rather than wrap/relabel.
- [ ] Add pack parser/converter RED for all21 source cues and packet-clock duration, malformed Ogg pages/lacing/truncated packets, stereo/rate/frame mismatch, invalid header/table/ranges/duplicates/hash, unaligned reads and bounded streaming.
- [ ] Implement deterministic source-SHA lookup,64-byte header and fixed entries with absolute bounds. PCM trailer starts at assets-relative0x50000; output pack and generated manifest header remain build artifacts. Decode Opus packets directly at24k; record actual decoder version.
- [ ] Cloud O2/ASan/UBSan and converter tests pass; spec review then quality review before integration.

## Task 3: C3 media, cue scheduling, FD and statistics integration

Files: firmware/patches/0015-c3-bounded-pcm.patch and new overlays c3_pcm_audio.{h,inc}, c3_echo_stats.h; c3_lite_uplink.inc/c3_lite_aec.h; FamilyC3Codec playback output; firmware host integration tests.

- [ ] Prepare the exact locked upstream plus existing patches in a fresh cloud directory. Add guarded CONFIG_XMINI_C3_PCM to C3 only.
- [ ] Skip C3 PCM encoder/decoder creation and Opus worker. Feed AEC spans directly to2 fixed input slots; Pop creates at most one network1920B packet and retains ButtonUplink token. Overflow reports reference/media fault and closes the session.
- [ ] Copy validated downlink into5 fixed1440-sample slots; use the codec pointer/span entry against the same DMA implementation. Keep in-flight slots until output returns, preserve truthful admit/release/seal accounting and generation cancellation.
- [ ] Stream cue descriptors through a fixed2880B aligned scratch; validate trailer before OTA-valid, cancel entire cues on reset, preserve activation/digit order, prevent cues from entering active server output. Include descriptors/producer/in-flight work in idle/drained checks.
- [ ] Gate unsupported C3 remoteOgg/audio-testing paths before state entry; preserve Wi-Fi configuration and main button behavior. Protect PCM trailer from generic assets downloads before any erase.
- [ ] Enable FD mode5 only under new flag; accumulate bounded numeric echo/processing/pool statistics and report from main. Preserve8KiB input/4KiB output stacks, remove unneeded Opus worker, and guard TCB lifetimes.
- [ ] RED→GREEN actual production-method tests and all legacy/enabled combinations; full firmware/upstream tests, spec review and quality review.

## Task 4: Build and inspect exact candidate

Files: .github/workflows/firmware-experiment.yml, firmware-c3-diagnostic.yml; build preparation/CMake/converter hooks and verify_experiment.py; tracked provenance tests.

- [ ] Build C3 cue trailer/header before app compilation using a recorded libopus version; retain trailer, manifest, source/SDK/native lock, app/ELF/map and hashes in artifacts. Verify original generated_assets.bin stays within0x50000 and unchanged.
- [ ] Commit/push main once source is reviewed. Require exact C3 symbols, five variants and stock success; inspect downloaded actual app/trailer/source/ELF and partition compatibility.
- [ ] Update private installer/rollback guards for the additional exact trailer range; cloud failure-injection tests cover partial writes, boot/mapping/hash failures, resource-gate failure and fault-tail recovery.

## Task 5: Controlled server rollout and physical verification

- [ ] With all previous gates passed, build/deploy the verified Go media adapter on cloud, preserve protected config/credentials/identity/memory and backup binary; require active_sessions0, readiness, unchanged policies and old Opus clients. No runtime service on Mac.
- [ ] Inventory exact C3 USB identity and readbacks; back up authorized changed ranges, install app0/trailer/selector0 only, verify all complements/stable fallback/security. Observe two post-ready boot gates with complete logs.
- [ ] Measure actual loaded CPU/heap/largest/stack/pool/reference statistics; fail/rollback below budget or on fault. Do not infer success from startup alone.
- [ ] Only then request the shortest physical acoustic acceptance: no speech during reply, then intentional speak-over-playback, then full new answer/remembered context and button end. Preserve failed results honestly.
- [ ] Update handoff/verification and commit/push main; leave devices/services in the verified state, close serial when finished. Natural voice interruption remains incomplete until actual acoustic acceptance.
