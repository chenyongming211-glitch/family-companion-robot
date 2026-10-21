# StopWatch software-reference AEC experiment

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. The natural-barge-in design is approved. Continue confirmed software/build work quietly; do not request another scope approval.

**Goal:** 交付具备真实 DMA 播放参考、MR 输入、诊断窗口、失效回退和打断清队列的可刷实验固件，保留现场声学门槛。

**Architecture:** 基于已锁定的小智源码，以受控补丁和 StopWatch 专属 overlay 接入现有 ESP-SR FD AEC。实际完成的 TX/RX DMA 块提供采样时间轴，固定有界缓冲在任务中配对，随后走现有联合 24k→16k 重采样。默认只观察 AEC，完成现场验证后才通过本机调试接口显式启用实时上行；中断标记只清旧播放，不关闭麦克风。

**Tech Stack:** ESP-IDF v6.1、ESP-SR 2.4.7、esp_codec_dev 1.6.2、C++17、Python 行为测试、GitHub Actions。

## Global Constraints

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

## Source evidence and file boundaries

Exact upstream source: `.superpowers/firmware-baseline/upstream/`; read its AGENTS.md. Exact SDK/codec excerpts and decisions: `.superpowers/firmware-development-20261022/research/aec-integration-evidence.md`, `idf61-i2s-common.c`, `codec-data-i2s-1.6.2.c`. These are ignored research inputs; tracked patch/overlay is authoritative.

The physical codec opens channel=1. esp_codec_dev converts it to mono-left DMA despite the constructor's initial stereo slots. Never interpret initial stereo bytes as mono. SDK on_sent exposes the completed buffer before auto_clear_after_cb. RX on_recv runs before the driver's internal receive queue and can copy into a separate bounded capture queue; an unused driver's read-queue overflow is not the experiment queue's loss indicator. Full-duplex channels share clocks but their start phase is not automatically aligned.

Narrow core hooks may be patched into AudioCodec/AudioService/AFE/Application where needed; board-specific adapters and diagnostics belong to the board. Core must not include the concrete StopWatch class or its config.h. Preserve exactly one board factory and both protocol transports' shared stop semantics. No generated/vendor output is committed.

### Task 1: DMA adapter, AFE gating and interrupted playback integration

**Files:**
- Create: firmware/patches/0001-stopwatch-software-aec.patch (changes to locked upstream existing files only)
- Create: firmware/overlays/main/boards/m5stack/stopwatch/duplex_reference.h
- Create: firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.h and .cc
- Create: firmware/overlays/main/boards/m5stack/stopwatch/aec_diagnostics.h and .cc
- Create: firmware/tests/duplex_reference_test.cc and test_duplex_reference.py
- Create: firmware/tests/interrupted_playback_test.cc and test_interrupted_playback.py if policy is extracted for real host testing
- Modify: firmware/README.md

**Interfaces / allowed design decisions:**

Consume existing PlaybackReferenceBuffer unchanged unless a behavior test exposes a defect. Use a portable sample-pairing helper (duplex_reference.h) for actual time/index/generation/fault logic so host tests execute production logic, not a model of it. Board codec can derive from Es8311AudioCodec and override public InputData/OutputData/EnableInput/EnableOutput, or add a narrow protected lifecycle hook if required. Keep the hardware codec's device-open channel count one; logical MR describes software augmentation explicitly.

```cpp
// MR interleaving after successful matching of real microphone and TX windows.
for (size_t i = 0; i < frame_count; ++i) {
    output[2 * i] = microphone[i];
    output[2 * i + 1] = playback_reference[i];
}
```

The adapter must give both channels to the same existing stateful resampler. Capture TX and RX with monotonic sample counters plus esp_timer completion stamps; fixed callback-to-capture phase and acoustic offset remain measurable quantities. A small bounded holdback can await a matching completed TX block. Make the applied fixed offset visible in diagnostics and configurable within bounded limits for calibration. Phase/cadence/format gaps, overflow, unavailable/expired reference, channel reconfiguration and allocation/callback setup failure must have explicit states. No guessed reference under integrity failure.

Implementer owns exact class signatures and bounded queue dimensions based on the locked driver. Report the selected time mapping, its uncertainty, locking order, memory sizes and fail-closed transition. Escalate controller questions if source facts invalidate the approach; do not silently substitute write-FIFO timing.

- [ ] **Step 1: Add host behavioral RED tests for the chosen pairing/policy interfaces.**

Use actual ramp PCM and independent microphone samples. Tests must assert a complete MR sequence, split/wrap order, timestamp phase offset, future-reference wait vs timeout, expired reference, missing/reordered blocks, known silent DMA frames, interruption tail preservation, hardware restart generation isolation, bounded RX overflow and fault-latched fallback. Also test a long synthetic 24k timeline (at least 10 seconds) with finite counters and no sample drift introduced by packet boundaries. Test offset extremes and invalid lengths without dereferencing invalid data. A reference fault must prevent realtime eligibility until an explicit valid lifecycle recovery; data loss cannot be reported as success.

```cpp
// Example required assertion shape, with the actual production API chosen here.
const int16_t mic[] = {101, 102, 103};
const int16_t ref[] = {11, 12, 13};
const int16_t expected_mr[] = {101, 11, 102, 12, 103, 13};
// Submit through production pairing methods, consume a real frame, compare all values.
```

The comment describes fixture wiring to the selected API; it is not a completed test. Add Python unittest compile/execute wrappers following test_playback_reference.py. Capture expected missing-feature RED before implementation.

- [ ] **Step 2: Implement bounded DMA/reference and hardware lifecycle ownership.**

ISR callbacks perform only bounded copies, counters/stamps and task notifications under one portMUX; no allocation/logging/network/Application calls. Allocate ISR-shared data in internal SRAM and either ensure callback call graph is IRAM-safe or explicitly keep the experimental ISR IRAM-safe option disabled with documented guard. Register only while channels disabled. Begin mono epoch only after codec open/reconfiguration, discard prior-format descriptors explicitly, and detect clock loss/restart. Serialize codec lifecycle/output calls without introducing main-loop/ISR blocking.

Task InputData consumes bounded RX blocks and matching completed TX windows. Actual ADC/DAC clocks, descriptor sizes and cadence must be checked against the locked configuration. Initial warmup or unavailable pairs do not feed AFE. Fail closed: stop realtime uplink/close the active audio channel via Application::Schedule; return to valid auto conversation on the next wake. AEC must be disabled for invalid software reference even on the wake detector path. If auto fallback uses original mono input augmented for a fixed logical format, document why the ignored second slot is not claimed as valid AEC reference; ensure it cannot reach enabled AEC or realtime before recovery. No repeated scheduling of the same fault.

Reset the joint resampler and AFE input/output partial frames when a hardware generation changes; mixed pre/post-restart samples are invalid even if a reference ring reset itself passed. The existing resampler exposes esp_ae_rate_cvt_reset. Ensure a generation fault cannot leak through an in-flight encode/send after closing its gate. At physical 24kHz the fixed 16000-sample history is about 667ms, not one second; document this actual retention and keep total reference storage within the approved cap.

Distinguish a genuine expected-consumer overrun from startup/output-only or intentionally inactive capture. Boot sounds can open the duplex codec before a microphone consumer exists, and Wi-Fi configuration or AFE pause can temporarily stop reads. Do not permanently fault the experiment simply because RX callbacks continue in those intentional states; arm bounded capture on the actual input lifecycle and explicitly invalidate old partial frames on resumption. Add a host lifecycle regression for this case.

- [ ] **Step 3: Integrate explicit observation and realtime eligibility.**

Add Kconfig scoped to StopWatch with software-reference flag and unique config.json build; extend USE_DEVICE_AEC dependency only for that real adapter. Build starts in observation mode: local capture and AEC continue during speaking for measurement, while ordinary cloud dialogue uses auto behavior until a local explicit realtime-enable command is accepted after ready/valid state. Prevent self-echo leaking through encoded audio already queued when switching states; keep the normal auto playback-drain behavior for non-experimental builds.

Provide local USB console lab commands through a bounded low-priority reader (not on the audio/main task): `aec_stats`, `aec_diag_start <RFC1918 IPv4> <port> <seconds>`, `aec_diag_stop`, `aec_realtime on`, `aec_realtime off`. Capture duration 1..120 seconds; default off. Reject invalid destinations/ports/durations and stale not-ready/faulted realtime enable. The on command is the operator's explicit post-measurement choice, never an automatic acoustic-pass assertion. Realtime choice is volatile, defaults to off after reboot, and changes apply on main task. Do not enable another automatic local VAD abort source.

The stock sdkconfig uses UART as primary stdin and USB Serial/JTAG only as secondary output. The unique experimental variant must explicitly select the USB Serial/JTAG primary console (or explicitly install/read that peripheral without conflicting with the existing secondary stdout path); merely reading stock stdin does not make these USB commands usable. Keep the stock variant's console unchanged. The window can be started over USB then continued on battery after unplugging for the separate USB-noise comparison; no audio capture begins on connect/boot alone.

Diagnostics expose aggregate format/generation/counters/overruns/timing/offset/eligibility without normal audio logs. Window captures raw 16k MR after the joint resampler plus AEC output before cloud encoding. Use preallocated bounded packets/queue and separate nonblocking UDP worker; packet drops increment counters and are visible. Define a documented versioned binary header with stream id, sequence, generation, sample position, rate/channels/count, monotonic timestamp and payload length. Keep datagrams <=1200 bytes; split at complete frames. No destinations hard-coded to a specific user network, no capture before explicit command or after expiry/stop, no network writes in ISR/audio/main loop. Ensure a collector can distinguish gaps, streams, resets and DSP delay; do not pretend raw/AEC have zero processing delay.

- [ ] **Step 4: Apply interrupted-stop semantics without stopping mic.**

Patch the transport-neutral tts handling. Copy the boolean from cJSON before scheduling; exact string only:

```cpp
auto reason = cJSON_GetObjectItem(root, "reason");
bool interrupted = cJSON_IsString(reason) &&
                   strcmp(reason->valuestring, "interrupted") == 0;
Schedule([this, interrupted]() {
    if (interrupted) audio_service_.ResetDecoder();
    // Existing Speaking -> Listening/Idle behavior follows unchanged.
});
```

Ensure queued/in-flight decode generations cannot repopulate old playback. Existing ResetDecoder already covers decode queues/generation; preserve it and cover the one physical OutputData/DMA tail explicitly. Ordinary stop drains normally. Microphone processing/upstream mode must survive a marked interruption in valid realtime mode. Host test the real selected policy/queue helper where practical; use target build and code evidence for platform-owned queues rather than tests that only assert source text.

- [ ] **Step 5: Verify, prepare exact patched source, document and commit.**

```sh
python3 -m unittest discover -s firmware/tests -v
git diff --check
```

Focused tests while editing, entire firmware suite once before commit. Prepare a fresh real experimental tree with prepare_experiment.py; assert patch applies and added board files are present, selected variant is listed by canonical build.py, normal variant unchanged, source provenance covers all owned files. Do not run a local unsupported SDK build. Document exact debug commands, memory/queue/cadence assumptions, fallback, offset uncertainty, reference-tail behavior and no acoustic acceptance yet. Commit scoped code/patch/tests/docs on main, no push; controller handles CI.

### Task 2: Collector, experimental target CI and rollback package

**Files:**
- Create: firmware/tools/capture_aec.py and firmware/tests/test_capture_aec.py
- Create: .github/workflows/firmware-experiment.yml
- Create: docs/verification/2026-10-22-software-aec-field-test.md
- Modify: firmware/README.md

**Interfaces:** Consume the versioned packet layout and debug commands documented by Task 1. The collector starts only when explicitly run, receives for a bounded duration, binds a chosen local address/port, stores raw data only under a newly created private directory (0700, files 0600), validates payload bounds and complete interleaved frames, reports per-stream sequence gaps/generations/sample totals. No default background collection. Host fixtures are synthetic packets with real saved-byte assertions; reject malformed/oversized/truncated packets and ensure stop/timeout works. Add actual offset/correlation analysis only if required to calibrate the selected time mapping, without claiming DSP/acoustic pass from synthetic values.

- [ ] **Step 1: Collector RED/GREEN.** Use a local loopback synthetic sender and short explicit test window; compare recorded PCM bytes and gap reports. Never bind to or collect live watch audio during this phase.
- [ ] **Step 2: Experimental Linux target build.** Reuse pinned checkout/upload actions and official espressif/idf:v6.1 container. Run firmware host tests, prepare fresh experimental source, assert exact SDK commit, run canonical `python3 scripts/build.py m5stack/stopwatch --name m5stack-stopwatch-software-aec --language zh-CN --zip`. Include a second job/matrix target building the original m5stack-stopwatch variant from this same patched tree to check disabled-feature core paths; the already verified unpatched stock baseline stays separate. Save source/patch provenance, SDK config, resolved dependency lock, app/bootloader/partition/assets/flasher args and SHA256. Fail if native dependency lock changed from checked-in validated bytes without an explained deliberate change; ensure critical versions and intended sdkconfig effective values. Artifact name includes variant and exact github.sha, retention seven days. No auto OTA or flash/deploy job.
- [ ] **Step 3: Inspect downloaded artifact.** Controller dispatches build and verifies exact source SHA, CRC/hashes, actual AEC/adapter config, app/assets fit and partition geometry against existing private backup. Fix build/review errors through normal source/patch changes. Successful CI alone is not field PASS.
- [ ] **Step 4: Write concrete field and rollback instructions.** Identify existing full backup as rollback source, protect NVS/otadata unless a justified migration requires otherwise, and never flash the merged image blindly. Give partition-specific app/assets/bootloader/table compatibility checks before any write, detect active OTA slot from saved otadata, and defer exact serial writes until the user is on site and notified. Include preflight restore checks, AEC observation first, calibration/real-time enable gate, original tools/idle checks, ten-round acoustic criteria, and the separate USB-noise comparison. Do not execute hardware/production steps.
- [ ] **Step 5: Review and finish software delivery.** One strongest-model broad concurrency/ISR/lifecycle/queue review after task review, fix important findings, push main and verify exact-head CI. Save artifact/test/review links and accurate remaining field work. No user notification while resting.
