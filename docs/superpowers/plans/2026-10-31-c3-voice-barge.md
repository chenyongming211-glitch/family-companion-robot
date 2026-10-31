# C3 Voice Barge Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans task by task. Work directly on main as the user requires.

**Goal:** Enable voice interruption during C3 playback through real microphone/reference AEC and the existing server interruption path.

**Architecture:** Establish the locked C3 AEC CPU/memory floor on actual hardware before integrating it with callback-owned TX playback and paired RX. Preserve the stable ota1 image as fallback; no server/device-policy change.

**Tech Stack:** ESP32-C3 160MHz, ESP-IDF6.1, C3-only ESP-SR2.6.1 SR_LOW_COST (selected after rejecting the original2.4.7 resource result), ESP codec/Opus, GitHub Actions and cloud host tests.

## Task 1: Exact AEC feasibility gate

Files: firmware/probes/c3-aec/{CMakeLists.txt,sdkconfig.defaults,main/CMakeLists.txt,main/idf_component.yml,main/aec_probe.c}; .github/workflows/firmware-c3-aec-probe.yml.

- [x] Build a non-networked synthetic AEC probe with `aec_create(16000,2,1,AEC_MODE_SR_LOW_COST)` and 16-byte aligned buffers. Query chunksize instead of assuming it. Emit heap before/after, average/max/P95/P99 process microseconds, nonzero input/output and no raw PCM. Run far-only, near-only and overlapping synthetic deterministic signals; do not treat those as speech acceptance.
- [x] Use the tracked native dependency lock, pinned IDF commit and exact package hash. Preserve package metadata, C3 headers, nm/link map and image SHA in an inspection artifact.
- [x] Run cloud verification of the probe source/build evidence and obtain the Actions artifact through the authenticated connector. Never put generated binaries or credentials in Git.
- [x] Fresh USB inventory/security/partition/readback check; only after image validation back up current changed ranges, install probe into unused ota0, select pending-verify OTA without touching NVS/bootloader/assets/app1/eFuse. Probe deliberately does not mark valid and reboots to the existing stable app1. Verify actual rollback/readback.
- [ ] If AEC itself cannot run below32ms per512samples, stop this algorithm integration and report measured resource evidence; do not enable raw playback-period uplink.
- [x] With the standalone 21–24ms floor established, measure AEC plus exact product Opus16k encode/24k decode60ms, complexity0, AUDIO, DTX/VBR. Use240 AEC blocks/128codec frames/7.68s audio, no terminal partial codec frame. Probe main stack must match product codec task24KiB; log highwater, retain network/resampling exclusion as a lower-bound limitation. Result:8.598s CPU/7.68s audio (~112%); old kernel rejected.
- [x] Repeat the isolated probe with officialESP-SR2.6.1, record exact resolved graph, and log AEC-only stack highwater before Opus. FD combinations still use96–104% CPU; select updatedSR_LOW_COST mode0 + originalAudio/AUTO Opus:4.564s/7.68s (~59.4%). Real acoustic suitability remains a required gate.

## Task 2: Product integration after a passing resource gate

- [x] Extend C3 codec TX/RX DMA pairing with the existing bounded DuplexReference and cloud host failure tests. Keep old playback accounting and callback refill exclusive.
- [x] Add aligned512-sample AEC buffering to Lite engine after shared MR resampling; retain partial frames and invalidate old capture epochs on close/reopen.
- [x] Keep uplink live across playback with matching application realtime mode; server interruption/drain stays common. Preserve button close, volume, memory, actual receipts and old variants in code; physical behavior remains to verify.
- [x] Run cloud O2/ASan tests, exact-target builds and independent spec/quality reviews. Final enabled source4c5132f:147 firmware host checks,110 upstream script checks, all five variant builds and stock build passed; downloaded C3 image independently verified.
- [ ] Install the verified candidate into inactive app0, verify activation/VALID and preservation, then review physical echo suppression/double-talk and complete user interruption acceptance before reporting availability.

### Product integration details

Driver files: `firmware/overlays/main/boards/xmini/c3/family_c3_{codec.h,codec.cc,dma.inc,lifecycle.inc,playback.inc,capture.inc}`. Keep the new path under `CONFIG_XMINI_C3_VOICE_BARGE`; legacy undefined/0 remains covered. Instantiate `BoundedDuplexReference<4096,8,75000>` (12,816B) with offset0. Default M5 aliases remain16000/16/150ms. RX/TX callbacks retain actual24k mono samples; generations/AEC readiness invalidate at capture boundaries; task reads and playback polls reject a75ms callback stall. Tests execute production `.inc` files with mocked SDK plumbing in cloud O2 and ASan/UBSan.

Frame assembly files: `firmware/overlays/main/audio/c3_echo_stream.h` and `firmware/tests/{c3_echo_stream_test.cc,test_c3_echo_stream.py}`. `Begin(epoch)` drops only the preceding capture's incomplete512-sample frame. `Feed(MR,frames,epoch,process,emit)` deinterleaves into16-byte aligned fixed512 arrays, retains every incoming sample across10ms frames, rejects stale epochs/invalid ranges, emits only successful processed mono. Cloud RED→GREEN must cover first480samples retention, arbitrary partial blocks, restart, alignment and DSP failure without raw fallback.

Integration patch: add `firmware/patches/0011-c3-voice-barge.patch` after the existing button-entry patch. Wire an internal AEC owner into LiteAudioEngine; create/query the exact512-sample API, reset its state at new capture generations and retain it continuously through TTS. Joint MR resampling uses the existing24k→16k converter with checked return/output size. Realtime application mode and the TTS receive callback keep the existing ButtonUplink epoch open during playback; close/error/reconnect still invalidate old work. A clock-task fault closes the channel instead of silently keeping a dead conversation. Do not change Go/Qwen/VAD.

Only after loaded resource success: enable the new flag in the C3 product build, update config/provenance verifier assertions and keep the oldvariant regression matrix. Build/inspect candidate, install into inactive app0 preserving stable app1 and identity/NVS; ask for a physical speak-over-playback test only after the reviewable candidate is ready. Do not claim success from compilation or synthetic echo energy.

Selected candidate uses2.6.1 mode0 with unchanged codec profile;0012 explicitly enables the family variant and budgets8KiB input/28KiB Opus stacks. The larger worker stacks account for MR/SRC processing and measured codec stack pressure. Verify real boot heap and task margins before accepting the candidate.

### Conditional C3 dependency pin

The upstream manifest pins ESP-SR==2.4.7 exactly. If the modern kernel/profile is accepted, keep the S3 native lock byte-for-byte unchanged: add optional `esp_sr_version_esp32c3` metadata, and apply that single manifest override only when preparing a fresh `--target esp32c3` tree. Record the override in prepared provenance and verify both the record and actual manifest pin. The optional recipe has no effect while the metadata is absent; do not enable/change a real target pin before the resource gate. Reject a missing/duplicate old pin or invalid version before publishing a prepared tree.
