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

Preserve the stock SEND_WAKE_WORD_DATA setting and standby wake-audio/greeting path. Its direct send loop bypasses the ordinary send queue gate: clear raw wake cache on relevant input/AFE resets and reject stale asynchronous encode batches when generation changes. Guard the direct wake send too, preserving valid normal auto fallback wake audio. Do not disable wake-audio upload merely to avoid this ownership boundary; no need to redesign unrelated detectors/boards.

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

