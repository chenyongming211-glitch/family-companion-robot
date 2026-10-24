# Task 2 implementation brief — collector, target CI and field package

Work at /Users/chen/code/家庭陪伴机器人. Read AGENTS.md. main only; no branch/worktree. User rests: no user-facing chatter/questions, device/serial/reset/flash/live audio, or gateway deploy/restart. No push; controller handles CI and downloading actual artifact. Implement exactly Task2, self-review, scoped commit(s) and detailed report in this directory at aec-delivery-report.md. Do not modify Task1 source unless a concrete integration issue is escalated first.

## Global constraints (verbatim)

- main 直接开发、提交、推送；不建分支或 worktree。
- 用户休息期间不发进度或提问，不打开串口、不刷机/重启设备、不部署/重启线上网关、不采集现场音频。
- 现有物理单麦克风、原引脚、24kHz I2S 和普通 m5stack-stopwatch 构建保持；新增唯一实验名称 m5stack-stopwatch-software-aec。
- 参考 PCM 固定 16000 个 int16 样本（32000 字节），对象不超过 32768 字节；调用者统一 ISR 安全锁。RX 队列另设固定容量，不允许无界分配/积压。
- 不能用 Write 调用时间、模型输出或网关时间替代实际播放时间。不能将播放中的参考缺失补零伪装成功；真正 DMA 零值可以作为静音参考。
- 停播不重置参考，覆盖 DMA/DAC 已提交尾音。硬件时钟/格式重启才换 generation，旧块不能混入。
- 先测 AEC，再启用播放时实时上行；不增加本地 VAD abort 决策。待机唤醒、音量/亮度工具、两分钟退出保留。
- 只有明确测试窗口可以采集原始 MR 与 AEC 对照，默认无音频记录；禁止日志泄漏 Wi-Fi/token/API Key。
- 构建/主机测试不代表声学通过；现场至少十轮插话、回声与实际扬声器停播验收沿用已批准设计。
- USB 连接且唤醒时刺耳声另行调查，不在没有根因证据时捎带修改增益、充电或功放行为。


## Task 2 (verbatim)
 Collector, experimental target CI and rollback package

**Files:**
- Create: firmware/tools/capture_aec.py and firmware/tests/test_capture_aec.py
- Create: .github/workflows/firmware-experiment.yml
- Create: docs/verification/2026-10-22-software-aec-field-test.md
- Modify: firmware/README.md

**Interfaces:** Consume the versioned packet layout and debug commands documented by Task 1. The collector starts only when explicitly run, receives for a bounded duration, binds a chosen local address/port, stores raw data only under a newly created private directory (0700, files 0600), validates payload bounds and complete interleaved frames, reports per-stream sequence gaps/generations/sample totals. No default background collection. Host fixtures are synthetic packets with real saved-byte assertions; reject malformed/oversized/truncated packets and ensure stop/timeout works. Add actual offset/correlation analysis only if required to calibrate the selected time mapping, without claiming DSP/acoustic pass from synthetic values.

Task 1 settled wire: SWAC v1, 48-byte little-endian header (`<4sBBBBIIQQIHHII`), stream1 MR stereo, stream2 AFE mono, rate16000. Consume the authoritative owned `aec_diagnostic_wire.h` and documented offsets. The header carries flags, sequence, capture generation, independent stream position, producer timestamp, frame/payload count, drop counter and window id. Positions of raw and AFE have independent origins: do not align them by equal position or fill gaps with zeros. Keep received PCM unchanged, split saved segments at resets/gaps/window or generation changes, and mark invalid-reference segments explicitly. A binary packet/index record plus per-segment WAV is acceptable if both preserve original data and uncertainty. Add a maximum total capture budget of 32 MiB as well as duration<=120 seconds, so an unexpected sender cannot create unbounded output. Optional explicit source IPv4 filter can keep other devices out of one test window. Use synthetic loopback traffic only in automated tests.

- [ ] **Step 1: Collector RED/GREEN.** Use a local loopback synthetic sender and short explicit test window; compare recorded PCM bytes and gap reports. Never bind to or collect live watch audio during this phase.
- [ ] **Step 2: Experimental Linux target build.** Reuse pinned checkout/upload actions and official espressif/idf:v6.1 container. Run firmware host tests, prepare fresh experimental source, assert exact SDK commit, run canonical `python3 scripts/build.py m5stack/stopwatch --name m5stack-stopwatch-software-aec --language zh-CN --zip`. Include a second job/matrix target building the original m5stack-stopwatch variant from this same patched tree to check disabled-feature core paths; the already verified unpatched stock baseline stays separate. Save source/patch provenance, SDK config, resolved dependency lock, app/bootloader/partition/assets/flasher args and SHA256. Fail if native dependency lock changed from checked-in validated bytes without an explained deliberate change; ensure critical versions and intended sdkconfig effective values. Artifact name includes variant and exact github.sha, retention seven days. No auto OTA or flash/deploy job.
- [ ] **Step 3: Inspect downloaded artifact.** Controller dispatches build and verifies exact source SHA, CRC/hashes, actual AEC/adapter config, app/assets fit and partition geometry against existing private backup. Fix build/review errors through normal source/patch changes. Successful CI alone is not field PASS.
- [ ] **Step 4: Write concrete field and rollback instructions.** Identify existing full backup as rollback source, protect NVS/otadata unless a justified migration requires otherwise, and never flash the merged image blindly. Give partition-specific app/assets/bootloader/table compatibility checks before any write, detect active OTA slot from saved otadata, and defer exact serial writes until the user is on site and notified. Include preflight restore checks, AEC observation first, calibration/real-time enable gate, original tools/idle checks, ten-round acoustic criteria, and the separate USB-noise comparison. Do not execute hardware/production steps.

Offline controller evidence is in private artifacts/firmware-preflight-20261022/{backup-integrity,backup-app-identities,backup-ota-metadata}.json. Existing backup's first OTA entry has sequence1, state ESP_OTA_IMG_VALID and valid sequence CRC; second entry is erased, so the backup selects ota_0. Exact SDK loader CRC/enum/selection source is retained under research/idf61-{bootloader-common-loader.c,flash-partitions.h,bootloader-utility.c}; current device selection remains unverified until a notified test window. Prefer preserving a known-good app slot where practical. The assets partition is shared: changing assets can compromise automatic rollback compatibility, so verify equality/compatibility or give explicit restoration steps rather than promising automatic recovery. The verified stock config enables bootloader app rollback, but that does not itself prove current hardware/asset recovery.
- [ ] **Step 5: Review and finish software delivery.** One strongest-model broad concurrency/ISR/lifecycle/queue review after task review, fix important findings, push main and verify exact-head CI. Save artifact/test/review links and accurate remaining field work. No user notification while resting.

## Inputs and implementation contract

- Task1 wire: aec-wire-interface.md here and firmware/overlays/main/boards/m5stack/stopwatch/aec_diagnostic_wire.h. Read authoritative header; raw and AFE independent timelines, producer timestamps not acoustic timestamps. Packet data/index are private; tests synthetic only. Keep duration plus TOTAL bytes budget (including WAV/index/packet overhead) bounded, not merely raw PCM.
- Existing source-prep CLI defaults are firmware/tools/prepare_experiment.py. CI pattern/pinned actions in .github/workflows/firmware-baseline.yml. Exact SDK identity in firmware/upstream.lock.json. Native dependencies.lock SHA pin in firmware/dependencies.lock.sha256. No local target SDK is installed. Run actual collector loopback behavior and source preparation/config validation locally; target CI controller-owned.
- Prepare experiment source once per isolated matrix job. Required 2 variants from that same patch/overlay inputs: original + experiment. Prove effective original software-AEC OFF/stockUART vs experiment software-AEC+DEVICE_AEC+USBPRIMARY ON, ISR_IRAM_SAFE and unconditional audio debugger OFF. Preserve exact upstream lock/patch-overlay provenance and resolved dependency bytes. Only app artifacts/configs, never an env/secrets dump.
- Private offline evidence artifacts/firmware-preflight-20261022/*.json. Existing backup and stock output paths can be cited in field checklist, but do not copy backup/keys/NVS into Git. Backup CRC/geometry/slot verified offline; hardware current-slot unknown. No serial commands executed. Avoid publishing exact full private evidence contents; publish usable partition geometry and verification procedure only.
- Collector may have a bounded packet index and per-segment WAV. Validate data before allocation/writing. Header length48, datagram<=1200, allowed flags0..3, stream1=2ch/stream2=1ch/16000Hz only, frames nonzero payload exact complete frames. Conservatively segment duplicate/out-of-order/gap/reset/generation/window/validity changes; do not append older packets into new continuous segment. Stop on explicit duration/budget/Ctrl-C, close WAV/update private metadata on normal exits. Actual saved PCM bytes and timeout/stop tested, avoid purely text-shape implementation tests. Honor original UDP order/metadata, no padding or invented alignment.
- Scope files: capture_aec.py, test_capture_aec.py, firmware-experiment.yml, field-test doc, firmware/README.md. A narrowly needed CI evidence check helper is acceptable if it keeps workflow reviewable; report reason, no broad framework.
- TDD: RED collector tests before implementation, exact command/output then GREEN. Iterate focused tests; full owned suite once final. Self-review, git diff --check, commit scoped files main, no push. Report status DONE/DONE_WITH_CONCERNS/BLOCKED/NEEDS_CONTEXT, commits, exact commands/results and any material limitations. Never call physical natural barge-in passed.
