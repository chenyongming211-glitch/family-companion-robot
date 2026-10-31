# C3 Voice Barge Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans task by task. Work directly on main as the user requires.

**Goal:** Enable voice interruption during C3 playback through real microphone/reference AEC and the existing server interruption path.

**Architecture:** Establish the locked C3 AEC CPU/memory floor on actual hardware before integrating it with callback-owned TX playback and paired RX. Preserve the stable ota1 image as fallback; no server/device-policy change.

**Tech Stack:** ESP32-C3 160MHz, ESP-IDF6.1, ESP-SR2.4.7, ESP codec/Opus, GitHub Actions and cloud host tests.

## Task 1: Exact AEC feasibility gate

Files: firmware/probes/c3-aec/{CMakeLists.txt,sdkconfig.defaults,main/CMakeLists.txt,main/idf_component.yml,main/aec_probe.c}; .github/workflows/firmware-c3-aec-probe.yml.

- [ ] Build a non-networked synthetic AEC probe with `aec_create(16000,2,1,AEC_MODE_SR_LOW_COST)` and 16-byte aligned buffers. Query chunksize instead of assuming it. Emit heap before/after, average/max/P95/P99 process microseconds, nonzero input/output and no raw PCM. Run far-only, near-only and overlapping synthetic deterministic signals; do not treat those as speech acceptance.
- [ ] Use the tracked native dependency lock, pinned IDF commit and exact package hash. Preserve package metadata, C3 headers, nm/link map and image SHA in an inspection artifact.
- [ ] Run cloud verification of the probe source/build evidence and obtain the Actions artifact through the authenticated connector. Never put generated binaries or credentials in Git.
- [ ] Fresh USB inventory/security/partition/readback check; only after image validation back up current changed ranges, install probe into unused ota0, select pending-verify OTA without touching NVS/bootloader/assets/app1/eFuse. Probe deliberately does not mark valid and reboots to the existing stable app1. Verify actual rollback/readback.
- [ ] If AEC itself cannot run below32ms per512samples, stop this algorithm integration and report measured resource evidence; do not enable raw playback-period uplink.

## Task 2: Product integration after a passing resource gate

- [ ] Extend C3 codec TX/RX DMA pairing with the existing bounded DuplexReference and cloud host failure tests. Keep old playback accounting and callback refill exclusive.
- [ ] Add aligned512-sample AEC buffering to Lite engine after shared MR resampling; retain partial frames and invalidate old capture epochs on close/reopen.
- [ ] Keep uplink live across playback with matching application realtime mode; server interruption/drain stays common. Preserve button close, volume, memory, actual receipts and old variants.
- [ ] Run cloud O2/ASan tests, exact-target builds and independent spec/quality reviews. Review physical echo suppression/double-talk and complete user interruption acceptance before reporting availability.
