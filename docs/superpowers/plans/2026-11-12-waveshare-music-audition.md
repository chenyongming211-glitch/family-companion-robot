# Waveshare Music Audition Implementation Plan

> **For agentic workers:** Use executing-plans for inline execution of the user-approved scope; main only, no worktrees. Review actual boundary changes before release.

**Goal:** 现有微雪播放同源24kHz/48kHz片段，支持停止并恢复24kHz语音。
**Architecture:** USB分块PCM试听客户端，共用应用独占状态与有界播放队列，Waveshare适配器负责共享硬件时钟。音源处理及测试在云端，无新服务。
**Tech Stack:** Existing ESP-IDF6.1/C++/Python3/FFmpeg, no new product framework.

## Global Constraints

- Direct main; only Waveshare ESP32-S3 2.16. Runtime tests and builds on cloud.
- Preserve other chat's settings work and the existing audio-capture dirty files. No credentials, raw user audio, firmware or music binaries in Git.
- Audition switch default off; installed candidate preserves b0d8f01 icons and connection diagnostics. Voice remains24kHz.
- Same source, mono mix, shared gain, unchanged device volume; actual acoustic loudness and subjective quality require field evidence.

### Task 1: Protocol and cloud audio preparation

Files: `tools/waveshare-music-audition/{README.md,prepare_audio.py,client.py,test_audition.py}`; `firmware/overlays/main/audio/music_audition_protocol.h`; dedicated host test.

- [ ] Define bounded start/data/end/stop frame format, sequence, payload checksum and acknowledgment. Test malformed lengths, invalid rates, replay/out-of-order, timeout, exact counts, truncation and cancellation before implementation; run cloud RED.
- [ ] Prepare Carefree on cloud from its official download; preserve source/CC attribution and hashes. One source segment is mixed and normalized once, then resampled separately. Measure final LUFS and true peak, record gain and reject clipping; do not claim physical loudness equality.
- [ ] Implement test client that waits for device confirmation before streaming the next block, aborts on mismatch/timeout and reports only actual device events. Run cloud protocol/client tests GREEN.

### Task 2: Exclusive device music and hardware profile

Files: common application/audio interfaces and scoped patch; `xiaoman_box_codec.*`, lifecycle/playback adapter; `playback_progress.h`; new music implementation and existing host harness.

- [ ] Add failing tests for24000/48000 DMA timing, voice timing unchanged, transition exclusion, rate-switch failure, stop and24kHz restoration.
- [ ] Add optional music API on AudioCodec, implemented only by supported hardware adapter; stop admission, revoke generations, drain/clear old output, switch/reopen both shared-clock devices and read back. Keep music outside voice identity/receipt accounting.
- [ ] Add default-off USB audition task and application start/cancel/finish gates. Main owns UI/state; worker owns transport and PCM; I2S owns completed output. Timeout and physical stop request cancellation; restore before idle. Suppress power/input lifecycle races during the exclusive mode.
- [ ] Cloud GREEN for changed code and supported regression suite; prepare locked source and full Waveshare target build. Inspect diff for state ownership, callback locking, fixed memory, timeouts and exact voice restore.

### Task 3: Device audition and delivery

Files: `docs/verification/2026-11-12-waveshare-music-audition.md`, current handoff; ignored task evidence only.

- [ ] Check other chat snapshot and current firmware provenance. Prepare concrete candidate and installation operator before any device change; preserve authoritative binding, factory and volume/brightness.
- [ ] Install only after cloud checks and existing backup/readback protections pass. Test A, stop, B, full drain, transport interruption, restored24kHz and ordinary conversation. Record I2S actual-completion evidence separately from hearing feedback.
- [ ] Update implementation/build/install/field status honestly, retain unresolved failure evidence, commit/push owned paths only and verify remote commit. User listening is pending until actually reported.
