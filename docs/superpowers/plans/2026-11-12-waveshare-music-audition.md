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

- [x] Define bounded start/data/end/stop frame format, sequence, payload checksum and acknowledgment. Test malformed lengths, invalid rates, replay/out-of-order, timeout, exact counts, truncation and cancellation before implementation; run cloud RED.
- [x] Prepare Carefree on cloud from its official download; preserve source/CC attribution and hashes. One source segment is mixed and normalized once, then resampled separately. Measure final LUFS and true peak, record gain and reject clipping; do not claim physical loudness equality.
- [x] Implement test client that waits for device confirmation before streaming the next block, aborts on mismatch/timeout and reports only actual device events. Run cloud protocol/client tests GREEN.

### Task 2: Exclusive device music and hardware profile

Files: common application/audio interfaces and scoped patch; `xiaoman_box_codec.*`, lifecycle/playback adapter; `playback_progress.h`; new music implementation and existing host harness.

- [x] Add failing tests for24000/48000 DMA timing, voice timing unchanged, transition exclusion, rate-switch failure, stop and24kHz restoration.
- [x] Add optional music API on AudioCodec, implemented only by supported hardware adapter; stop admission, revoke generations, drain/clear old output, switch/reopen both shared-clock devices and read back. Keep music outside voice identity/receipt accounting.
- [x] Add default-off USB audition task and application start/cancel/finish gates. Main owns UI/state; worker owns transport and PCM; I2S owns completed output. Timeout and physical stop request cancellation; restore before idle. Suppress power/input lifecycle races during the exclusive mode.
- [x] Cloud GREEN for changed code and supported regression suite; prepare locked source and full Waveshare target build. Inspect diff for state ownership, callback locking, fixed memory, timeouts and exact voice restore.

### Task 3: Device audition and delivery

Files: `docs/verification/2026-11-12-waveshare-music-audition.md`, current handoff; ignored task evidence only.

- [x] Check other chat snapshot and current firmware provenance. Prepare concrete candidate and installation operator before any device change; preserve authoritative binding, factory and volume/brightness.
- [x] Install only after cloud checks and existing backup/readback protections pass. Test A, stop, B, full drain, stopped-data timeout and restored24kHz. Record I2S actual-completion evidence separately from hearing feedback.
- [ ] Obtain user feedback for subjective A/B and ordinary conversation after restoration. Physical unplug/replug and physical-button stop are not counted as field PASS.
- [x] Update implementation/build/install/field status honestly, retain unresolved failure evidence, commit/push owned paths only and verify remote commit. User listening is pending until actually reported.

## 进度记录

2026-11-12：同源30秒素材已准备，24/48k数字响度差0.02LU。客户端12项、协议及实际worker25场景通过。首次完整165项有测试来源/工具环境错误；改用未重复插桩的锁定源码夹具、项目既有NVS0.3.0环境后165/165零跳过。已完成整包构建和97项目单元/232项目依赖/59SDK文件核对；最终审查追加音量/硬件profile互斥及逻辑语音采样率固定，两处修复已加入实际Box方法回归，最终166/166零跳过（92.063秒）及整包复验已通过。随后已冻结并核对294来源，最终pins后操作器75项及被动采集12项通过。0920343已推main并安装：新16MiB快照、仅活动槽写入、整槽及槽外/整片校验、启动到idle通过。A/B各30秒实际I2S完成720000/1440000样本；主动停止、停发约5.23秒超时及新24kHz播放/停止通过。Mac直接开物理串口会重启，采用既有持续USB relay；客户端RFC write_timeout兼容问题补RED/GREEN后13项通过。听感和恢复对话已向用户询问，等待真实反馈。
