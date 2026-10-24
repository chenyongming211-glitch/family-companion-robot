# Task 1 StopWatch completed-DMA software AEC integration

Status: DONE_WITH_CONCERNS — implementation and host preparation/behavior checks complete; target compilation and physical acceptance are pending controller CI/lab work. No SDK, serial/device access, flashing, live capture, server changes or push performed. Commit: `7bee4db` — `feat(firmware): integrate StopWatch DMA software AEC experiment` (main, not pushed).

## Scope and implementation

Authoritative source is `firmware/patches/0001-stopwatch-software-aec.patch` plus owned new overlays. The ignored `aec-work` directory is only patch-generation scratch. Existing `playback_reference_buffer.h`, source lock, dependency lock, stock variant and pins are unchanged. No changes to the controller's separate Task 2 plan edit are staged.

The new unique `m5stack-stopwatch-software-aec` build uses the existing StopWatch factory/pins/flash layout, physical mono ES8311 at 24 kHz, software logical MR, device AEC, and primary USB Serial/JTAG stdin/stdout. Stock console remains unchanged; SEND_WAKE_WORD_DATA remains stock. The upstream unconditional audio debugger is explicitly disabled in the unique variant.

- `duplex_reference.h`: production portable sample pairing, bounded RX queue, clock/cadence integrity, holdback, fault latch and hardware epochs. Existing ring consumed unchanged.
- `software_aec_codec.h/.cc`: actual completed TX DMA and RX DMA callbacks, runtime format verification, internal SRAM, lifecycle serialization, logical MR delivery, fallback and readiness/fault publication.
- `experiment_gate.h`: production encode epoch policy, separate capture/wake epochs, and wake batch validity used by workers/consumer/main send guard.
- `aec_diagnostic_wire.h`: strict private IPv4 request parsing and explicit endian-safe header serialization.
- `aec_diagnostics.h/.cc`: bounded low-priority USB command reader and explicit-window nonblocking UDP capture. No audio-path sockets or dynamic capture queue allocation.
- Patch: Kconfig/variant/board wiring; narrow codec/engine interfaces; joint resampler/AFE generation handling and uplink gate; standby wake cache/worker/direct-send generation checks; shared exact interrupted-stop decoder reset; experiment-only sensitive application log suppression.

Observer mode is the boot default. It actually runs raw capture and local AEC while speaking, but cloud behavior remains auto with playback-drain before listening. `aec_realtime on` is an explicit operator command, accepted only after a valid matched pair and successful AFE output in the same current capture generation; main task rechecks. It never asserts acoustic acceptance and does not persist across reboot. No additional local VAD abort source exists.

Exact string `tts.stop.reason == "interrupted"` is copied before cJSON lifetime ends and schedules ResetDecoder, clearing upstream decode/playback generations and queues. Ordinary stop keeps drain behavior. The main microphone/AFE epoch and reference history are not reset by interrupted stop. A chunk already inside a physical write plus submitted DMA can remain audible; nominal bound is approximately a 60 ms chunk plus six 10 ms descriptors, not a measured acoustic stop bound. The reference continues to record that physical tail and actual completed zeros.

## Timing and source evidence

Pinned source: upstream `49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`; IDF `fff9895c82d744c7237be8847347bdd1b07c6643` (6.1); esp_codec_dev 1.6.2; ESP-SR 2.4.7. Consulted locked upstream and research `aec-integration-evidence.md`, `idf61-i2s-common.h/.c`, `codec-data-i2s-1.6.2.c`; parent supplied `idf61-i2s-types.h` to cross-check non-const event pointer callback signature.

The ES8311 constructor initially starts stereo DMA; codec open reconfigures mono. Register callbacks only with both channels disabled, ignore the constructor epoch, and begin a new epoch after mono open. Discard the first boundary descriptor separately for TX/RX. Runtime checks require STD, mono left slot, 16-bit, 24000 Hz, nominal 768000 Hz BCLK and six 480-byte descriptors. `on_sent` copies the completed buffer before auto_clear_after_cb. It never copies submitted/write-FIFO data. `on_recv` copies before stock driver queue eviction; expected overflow of that unused stock RX queue is not treated as own-consumer loss.

Each direction's origin = first retained completion stamp minus 10000 us. For RX physical sample r:

`TX index = r + round((rx_origin_us - tx_origin_us) * 24000 / 1000000) - fixed_offset_samples`

The phase is rounded once per timeline, with integer counters afterwards. Positive offset looks back; CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES is [-2400,2400], default 0. Initial negative references are warmup, discarded. A future TX window waits at most 150 ms. Absolute cadence departure beyond 1500 us, sequence/format failure, expired history, missing clocks, RX overflow or setup failure latches fault. Counters and timestamp arithmetic reject impossible bounds before dereference/overflow.

Uncertainty: half-sample phase quantization at most, plus unknown ISR observation latency/jitter, actual clock error, speaker/acoustic delay and DSP delay. The 1500 us threshold is an integrity guard, not a timing accuracy guarantee. Calibration and USB/battery acoustic comparison remain hardware work. At 24 kHz, the 16000-sample ring retains 666.7 ms, not one second.

## Memory, ownership and failure behavior

- Ring PCM 32000 bytes; host ring object 32024 bytes. Ring + 240-sample reference scratch + bounded reference metadata is statically asserted below 32768 bytes. RX is separately 16 x 240 samples (7680 bytes PCM / 160 ms). Portable pairing object host size 40544 bytes, ABI-dependent.
- Pairing object explicitly allocated internal SRAM. Board static codec/ISR fields and diagnostic queue control are internal; ISR atomics use lock-free 32-bit words with static_assert. One portMUX protects all ring/pair/epoch state, including reads and diagnostics. ISR does bounded copies/counters/stamps; no allocation, logs, network or Application calls.
- Callback call graph is not IRAM-safe. Kconfig and compile guard require I2S_ISR_IRAM_SAFE=n. Flash/cache stalls may cause cadence faults; target measurement required.
- Task codec lifecycle/output/fallback reads acquire lifecycle mutex then the existing device mutex. ISR never acquires task locks. Pair lock is released before platform queries/network/main callbacks. Input consumes matched MR in whole physical-frame order for both 10 ms and 60 ms callers; partial prefix is never concatenated with fallback data.
- Only actual input consumption arms RX. Boot sounds/output-only, warmup, intentional pause and Wi-Fi setup do not fill/fault RX. Pause discards partials, resume increments capture/wake epoch; fault remains latched. Physical reopen may recover non-setup fault; setup failure remains sticky until reboot/reinitialization.
- Joint resampler processes both MR channels with one state. Capture generation/fault changes reset it and AFE input/output partials. AFE has 20 ms bounded fetch polling for this adapter, with discard-only draining if old feed backpressure holds the input mutex, so reset does not need a new mixed-generation frame. Stock boards retain their fetch behavior.
- First fault revokes reference validity/readiness immediately and changes generation. Queue/encode/send checks reject older epochs; worker schedules one mode-off/audio-channel-close. Bytes already handed to transport cannot be retracted. Next explicit auto listening allows actual mono fallback with ignored zero padding in the logical R slot. AFE disables AEC before fallback feed including WakeNet; padded zeros never confer AEC validity or realtime eligibility. DSP/resource exhaustion may prevent audio entirely and is not reported as healthy fallback.
- Standby wake audio remains enabled. Pause after detection freezes a valid wake snapshot; resume, hardware change and fault invalidate it. Raw cache is cleared under existing cache mutex plus wake batch mutex. Persistent experiment-only encoder worker avoids reusing a static task stack before previous deletion; generation/batch checks at read, enqueue, consume and main direct-send reject stale data. Valid mono fallback preroll is allowed. Existing 24 KiB PSRAM encode stack is reused by the persistent worker; batch is bounded by existing two-second cache. Unrelated MultiNet/original paths remain retained.
- Diagnostic queue is 12 preallocated <=1200-byte packets, about 14.5 KiB PSRAM, plus internal control and two 4096-byte low-priority task stacks. UDP worker creates socket only for explicit-window data and an up default netif. Stop/start serialized against nonblocking send; window id rejects old queued/racing producers. Queue/send losses counted. Console command pending flag bounds main-scheduler requests.

## Commands and collector contract

USB primary console, <=95 chars/line:

```
aec_stats
aec_diag_start <RFC1918 dotted IPv4> <port 1..65535> <seconds 1..120>
aec_diag_stop
aec_realtime on
aec_realtime off
```

Start syntax uses single spaces and rejects extra/signed/overflow values and public IPv4. Diagnostic off at boot/connect. A started window may continue after USB unplug. All raw PCM stays outside Git.

Exact 48-byte LE header and complete offset table are in `firmware/README.md` and ignored `sdd/aec-wire-interface.md` (already sent to controller). Magic SWAC, version 1; stream 1 MR after joint 24k->16k resampler, stream 2 mono AFE before cloud encoder; flags bit0 valid reference / bit1 reset; per-stream sequence, capture generation, independent stream position, producer timestamp, rate 16000, frame/payload counts, aggregate drops and capture window id. Raw max 288 frames, mono max 576 frames per datagram. PCM16 LE interleaved. Sequence and position advance even on dropped enqueue. Reset positions are stream-local; collector must segment gaps/resets/invalidity and estimate acoustic/DSP alignment, not equate raw and AFE positions. Collector itself is Task 2, not implemented here.

Privacy scope: experiment-only guards suppress explicit normal app TTS/STT/wake/custom/alert transcript logs, protocol message/error payloads and session ids, explicit OTA activation/body/URL, MCP content, Wi-Fi SSID and BLUFI password log sites. UI captions and ordinary conversation transport are preserved. No managed component/SDK-wide privacy claim: external logging and inherited error-string content still need target audit before keeping/sharing console logs. Diagnostic switch controls extra raw MR/AFE UDP only, not ordinary cloud conversation.

## Test-first evidence

Before respective headers existed, ran:

```
python3 -m unittest discover -s firmware/tests -p 'test_duplex_reference.py' -v
python3 -m unittest discover -s firmware/tests -p 'test_interrupted_playback.py' -v
python3 -m unittest discover -s firmware/tests -p 'test_aec_diagnostic_wire.py' -v
```

RED: each C++ compile/execute wrapper failed its compile return-code assertion because the production header was missing (`duplex_reference.h`, `experiment_gate.h`, `aec_diagnostic_wire.h`, respectively; compiler fatal file-not-found). These were expected missing-feature failures observed before implementing each production interface. First duplex implementation also exposed signedness warning under -Werror; fixed before GREEN. This report records relevant output from the session, not invented saved RED log files.

GREEN: focused wrappers compiled real production headers under C++17 -Wall -Wextra -Werror -pedantic and ran assertions successfully. Duplex harness prints `pair_storage=40544 reference_storage=32024` and `duplex behavior PASS`. Tests cover all MR samples (independent mic/ramp reference), split/wrap, 12-second / 288000-sample timeline without packet drift, 125 us phase, offset extremes, known zero DMA, future wait/timeout, expiry/reorder/cadence/overflow, latch/restart, output-only boot and intentional pause/resume, invalid pointers/lengths/arithmetic. Gate harness covers queued/in-flight epochs, interrupted-playback mic preservation, frozen wake snapshot, stale batch rejection and valid fallback wake. Wire harness asserts exact bytes and strict address/numeric bounds.

Final owned suite command: `python3 -m unittest discover -s firmware/tests -v` -> **46 tests, OK, 3.603 s**. Full output `sdd/aec-owned-final.txt`. Existing tests plus three new behavior wrappers; no physical/DSP model claim.

Prepared-tree upstream command: `python3 -m unittest discover -s scripts/tests -v` -> **110 tests, OK, 5.681 s**. Full output `sdd/aec-upstream-reviewed.txt`. Test runner subprocess environment prepended `/bin:/usr/bin` and retained the actual current Python interpreter. Initial attempt without this environment had six WorkflowDiffTests errors: Homebrew bash 5.3.9 aborted with dyld missing `_rl_completion_rewrite_hook` in readline 8.2.13. Reproduced `bash --version` exit134; `/bin/bash` 3.2.57 works. No repo code or global environment modified for that unrelated host failure. Initial failed output is `sdd/aec-upstream-tests.txt`.

Formatting: clang-format 23.1.3 with pinned upstream .clang-format; new owned C++ formatted and dry-run -Werror passes. Existing source formats changed ranges only. `git diff --check` passes. Immutable ring was excluded from formatter and remains byte-identical.

## Preparation/provenance verification

Fresh prepare with local verified archive succeeded, including real git apply --check --whitespace=error-all and atomic overlay install:

```
python3 firmware/tools/prepare_experiment.py --archive .superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination .superpowers/firmware-development-20261022/prepared-aec-commit-clean
```

Prior `prepared-aec-reviewed` tree is the source of full upstream tests; final commit tree differs only in explicit CONFIG_USE_AUDIO_DEBUGGER=n variant setting. Both retain stock build entry verbatim; no original pins/factory changes. Provenance verification compares every recorded patch/overlay SHA to owned inputs and prepared dependency lock to exact tracked native lock. One patch / eight overlays (including unchanged ring); dependency SHA256 `f0568ddc1585ad4bf0f272cc16b75f38d7befab1cebaad2f38aa33d47d8e665b`. Archive SHA256 `b3460397122eff7edeb8c07bd9c26e2c216b24cc2e5c2d836ec2209ba5be0c63`. Final patch hash is in final prepared `.family-robot-source.json` and recorded below.

Canonical `scripts/build.py --list-boards` discovers both StopWatch variants. SDK inactive warning / unrelated existing Lilygo ambiguous identity warning are not experimental compile results. Build was not attempted without SDK. Controller's earlier stock CI at fcf6465 is separate baseline evidence, not a build of this implementation.

## Self-review and remaining concerns

Self-review addressed: bounded AFE fetch/reset handshake; initialization-safe null codec gate; physical mono open versus logical MR; actual read/write error propagation for experiment; expected inactive RX; keeping wake preroll instead of disabling it; stale asynchronous wake outputs and persistent stack reuse; sequence flags on raw/AEC reset; generation retained in diagnostic producers; parser signed/overflow values; no socket at boot; USB actually primary; sensitive app logs. No change to existing ring necessary.

This is substantial integration into the existing large AudioService/AFE/Application state machine. Portable policy tests exercise real shared helper logic, but cannot prove FreeRTOS scheduling, ESP-SR resets, stack/heap headroom, codec/IDF ABI compilation, ISR callback latency or end-to-end wake timing. Exact-head Linux SDK build (including stock regression target as appropriate) is required next. Then separately authorized physical checks must cover fallback/recovery, standby wake/greeting, both transports, reconnect, output queue tail, auto/observe/realtime gates, capture loss/timestamps, USB noise/battery, far-end suppression/near-end intelligibility and false interruption behavior. No acoustic PASS or production readiness is claimed.

Final prepared patch SHA256: `8c9f3a7f41f100c401456082c29d53de5979b647d80ae5a3f9f2d63a6af62c49`. Final provenance, stock variant, pins, single factory, audio-debugger disabled and immutable ring checks: PASS.

Staged diff initially reported trailing space on unified-patch empty context lines (patch container only). Normalized those context lines to empty lines, re-prepared via strict git apply successfully, and verified every resulting main/ file byte-identical to the prior final tree. `git diff --cached --check` now passes.
