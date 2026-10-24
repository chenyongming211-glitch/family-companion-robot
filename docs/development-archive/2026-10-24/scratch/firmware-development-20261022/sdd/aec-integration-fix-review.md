# Spec compliance: approved for task scope

**Spec compliance: ✅ The four reviewed findings are addressed. Code quality: Approved for this fix wave.** Reviewed the supplied full `7bee4db..3f433d0` package and fix report against the original brief, original review, supplementary observer-tail assessment, and the controller-approved 120 ms observer-only policy. No new actionable implementation defect was found in this task-scoped re-review.

This approval is for source/spec review of the fixes. It is not target-compilation, acoustic, field, or production-readiness approval.

## Findings resolved and strengths

### 1. Faulted mono authorization survives intentional capture restarts

**Resolved.** `firmware/overlays/main/audio/experiment_gate.h:13-28,40-52` separates integrity identity from capture/wake identity. Hardware change and first fault change integrity; Pause/Resume/Boundary change capture as appropriate without revoking the same fault's explicit fallback authorization. `firmware/patches/0001-stopwatch-software-aec.patch:341-373` wires this policy into the real queue-locked gate rather than only its tests. `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.cc:55-67` publishes the new identities before exposing the first fault flag.

The original start-before-warmup ordering is therefore valid: explicit auto authorization survives warmup and the new observer boundary, while capture/output epochs still invalidate old AEC work. A newly faulted integrity identity cannot inherit the old fault's mono permission. Tests exercise this lifecycle through production policies, including a later fault and faulted hardware transition (`firmware/tests/interrupted_playback_test.cc:44-76`).

### 2. Common clock drift is no longer treated as an epoch-long timing gap

**Resolved.** `firmware/overlays/main/boards/m5stack/stopwatch/duplex_reference.h:149-190` retains the fixed sample origins and integer PCM timeline, replacing the old absolute error test with adjacent-descriptor, bounded rate-window, and relative TX/RX checks. Only rate-measurement anchors are renewed; reference history and mapping origins are not periodically reset. The reference-storage assertion still bounds the expanded metadata plus reference/scratch storage (`:205-206`).

The submitted regressions cover five minutes at each of four shared rate offsets, every returned MR sample, missing descriptors, accumulating differential drift, and excessive shared rate error (`firmware/tests/duplex_reference_test.cc:49-87`). These directly cover the earlier +50 ppm reproduction and preserve meaningful loss detection. The README explicitly treats the numerical tolerance/rate allowance as uncertainty and integrity policy, not acoustic alignment evidence.

### 3. Capture arming has serialized source-read/apply ownership

**Resolved.** `CaptureActivation` protects both the read and apply under one task mutex and keeps warmup as a persistent hold (`firmware/overlays/main/audio/experiment_gate.h:55-74`). Real service callers now read current event bits inside that owner, and `CaptureRequested()` also honors service stop (`firmware/patches/0001-stopwatch-software-aec.patch:375-389`). Warmup enters/releases the same owner (`:464-466`); stop and idle-close refresh it (`:334,456`); the adapter's direct input-disable bypass is removed.

The two-thread regression exercises the original delayed-active-read ordering through this production owner, followed by current-state refresh, warmup/disable/release, and forty inactive DMA callbacks through the real pairing helper (`firmware/tests/capture_activation_test.cc:19-61`). An old read can temporarily finish before the serialized disabling refresh, but cannot re-arm capture after that disabling refresh completes. This addresses the persistent inactive-consumer overflow found in the first review.

### 4. Observer auto includes the approved tail quarantine and fresh input boundary

**Resolved against the approved policy.** The actual `StartListeningAudio()` call requests quarantine only when listening mode is not realtime (`firmware/patches/0001-stopwatch-software-aec.patch:217`). `ObserverTail` starts a fixed deadline after software drain, keeps repeated idle polls from extending it, restarts on renewed output, and expires once (`firmware/overlays/main/audio/experiment_gate.h:76-110`). Output activity is incremented under the playback queue mutex, so a write wholly between polls is also detected (`firmware/patches/0001-stopwatch-software-aec.patch:487,618`).

The service advances the policy at input reads while local observation remains running. At expiry it invokes `CaptureBoundary()` before opening the new send epoch (`firmware/patches/0001-stopwatch-software-aec.patch:358-373,392`). The codec flushes pending RX/partial samples under its portMUX and increments capture generation, preserving the TX history, capture-active ownership, fault identity, and frozen wake identity (`firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.cc:146-157`; `firmware/overlays/main/audio/experiment_gate.h:24`).

The existing generation-aware read/reset path then resets the joint resampler and AFE input/output state; AFE rejects stale raw generations and pending-reset feeds, and old fetch/encode epochs remain rejected. The complete prepared functions were checked to ensure this is real downstream behavior rather than an assumption about `CaptureBoundary()` alone. Valid realtime bypasses the new guard and interruption still preserves its live microphone/reference path.

Policy regressions cover blocked in-flight output, immediate AFE reset while the timer is still pending, pre-expiry polls, one boundary, stale epoch rejection, preserved wake identity, renewed output, a write between polls, and realtime bypass (`firmware/tests/interrupted_playback_test.cc:78-112`). The 120 ms duration remains an approved conservative control policy based on the nominal configuration and stock warmup; this review does not reinterpret it as measured DMA/acoustic drain.

## Code quality and lock assessment

- The added helpers have distinct responsibilities: integrity authorization, serialized capture activation, and observer-tail timing. They are invoked from production code and exercised by host behavior tests (`firmware/overlays/main/audio/experiment_gate.h:40-110`; patch `:341-389`).
- Queue-locked gate advancement calls the short codec portMUX boundary and atomic AFE reset request; it does not acquire the AFE input mutex or perform hardware/network I/O. Capture activation takes its own task mutex then the codec portMUX, and holds neither across the warmup delay. The inspected callbacks do not acquire these task mutexes. No new lock cycle was found in the inspected call paths (prepared `main/audio/audio_service.cc:198-245,358-365`; `main/audio/engines/afe_audio_engine.cc:252-262`; codec `:146-157`).
- The raw-generation boundary is separate from frozen wake-batch identity, avoiding a new rejection of the direct wake upload after it has completed. The capture generation still changes, so stale ordinary encode/send work cannot become fresh simply because fallback permission survived (`experiment_gate.h:23-28`; patch `:358-373`; prepared AFE `:281-285,480-495`).
- The fix does not alter the diagnostic wire/commands, add a reference ring, introduce invalid-reference silence as AEC input, add local VAD abort behavior, or change stock variant/pins. The diff stays within the four fixes plus their docs/tests; the root documentation commit adds controller requirements and is not counted as new runtime implementation.

## Checks and verification boundaries

- Read the complete supplied fix report, original brief/review notes, and full fix diff in bounded sections. No Git operation or suite rerun was performed.
- Named outside-diff risk checks: (a) bypasses of the new capture owner on stop/input close — checked the audio/application `EnableInput(false)` and `SetCaptureActive` call sites and complete stop/input loop; (b) whether the new observer boundary actually invalidates raw/resampler/AFE output — checked complete prepared `ReadAudioData`, gate functions, `StartListeningAudio`, AFE Feed/Reset/Processing paths; (c) mutex ordering around policy/codec/AFE calls — checked those complete functions and the codec boundary. These checks use `.superpowers/firmware-development-20261022/prepared-aec-fix/`; diff hunks omitted parts of these functions.
- Inspected the existing reported test logs without executing them: owned suite `aec-fix-owned-full.txt` records **47 tests, OK, 4.246 s**; patched upstream `aec-fix-upstream-full.txt:120-122` records **110 tests, OK, 6.050 s**. Focused final GREEN logs contain no compiler warnings or test failures. The upstream log also includes the existing negative asset-size fixture's printed `Error: assets.bin size ... exceeds partition limit 100 bytes` after the passing summary; this is not presented as pristine stdout or attributed to these fixes. The full suites are prior implementer execution evidence, not newly rerun reviewer evidence.
- Regression sources use real production helpers and PCM values. They do not emulate or establish FreeRTOS scheduling, ESP-SR internals, actual clock accuracy, codec/drain timing, or acoustic suppression. No new executable reproduction was needed because the previous defects are addressed by the reviewed source and new focused regressions.
- ⚠️ Exact-head SDK/ABI compilation, target scheduling/stack/heap, physical fault recovery and standby/wake behavior, both transports, and acoustic/USB/battery acceptance remain controller CI/lab gates. These are acknowledged limits, not newly discovered code defects. The pending unrelated USB-awake-noise document is outside this review.

## Assessment

**Task quality: Approved for this fix wave.** All four earlier actionable findings have concrete production fixes and relevant regression coverage. No Critical, Important, or new Minor source finding is raised. Continue the separate exact-head target build and field verification gates before claiming complete hardware delivery or acoustic acceptance.
