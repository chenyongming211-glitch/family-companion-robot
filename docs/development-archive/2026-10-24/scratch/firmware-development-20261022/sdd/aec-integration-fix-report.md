# AEC integration review fixes — one wave

Status: DONE_WITH_CONCERNS (source/host verification complete; exact-head target compilation and physical/acoustic acceptance remain pending). Commit: `3f433d0` — `fix(firmware): preserve fallback and isolate observer capture boundaries` (main, not pushed). Base was controller documentation HEAD `ccb3edd`, following implementation `7bee4db`. No push, SDK build, device/serial/reset/flash/live audio or server action performed.

Read full `aec-integration-review.md` and supplementary `aec-observer-tail-assessment.md`; all three original findings plus the supplementary observer tail finding addressed. Controller approved the conservative stock-equivalent 120 ms observer guard after reviewing the alternative completed-DMA fence and its fault-fallback limitation.

## 1. Fault fallback authorization survives the actual start handshake

Added an integrity identity to production `CaptureEpoch`, incremented on hardware change and first fault, independent of intentional Pause/Resume/Boundary capture generations. Codec publishes it through the narrow `ReferenceIntegrity()` interface. `UplinkAuthorization` stores the explicit listening request against this identity under AudioService's queue mutex. A latched-fault mono request therefore survives input warmup, pause/resume and observer raw boundary; a new fault or faulted hardware transition cannot inherit it. Capture generation and output epoch still change, so queued/in-flight old AEC audio is rejected. Fault publication revokes validity and publishes the new identities before publishing faulted=true.

Actual production wiring: StartListeningAudio -> SetExperimentUplink -> UplinkAuthorization.Request; every UpdateExperimentGateLocked uses Allows(faulted, integrity), and the same ExperimentGate tags encode/send work. No reference padding is promoted to AEC-valid or realtime eligibility. The prior `permitted_fault_generation_` and separate requested atomics are removed.

Regression through production policies: live AEC -> first fault -> idle -> fallback wake capture/frozen snapshot -> StartListening request -> warmup Pause/Resume -> fresh mono token accepted while old AEC rejected -> second fault revokes mono -> faulted hardware transition revokes previous grant. Wake-batch checks remain intact.

## 2. Common clock drift is distinct from missing/relative drift

The production pairing helper no longer compares every callback against a permanently exact 24000 Hz first timestamp. It retains its original integer pairing origins/history and adds:

- Adjacent descriptor timestamp delta check: nominal 10000 us, 1500 us observation uncertainty plus 10 us allowance for the configured 1000 ppm rate envelope. A missing 10 ms callback remains a fault.
- A bounded 10-second rate-check window for each direction: nominal duration, permitted 1000 ppm difference plus 1500 us endpoint timestamp uncertainty. Re-anchor only this measurement window, never PCM history, integer sample counters or pairing phase.
- Relative TX/RX completion phase check at equal integer sample index, comparing against the initial relative phase. Budget is 1500 us plus the 1000 ppm correction allowance for unequal latest callback sample indices. Common drift cancels; accumulating differential drift faults.

Host regressions run five minutes EACH at -1000, -50, +50, +1000 ppm common error (7,200,000 physical samples per case), asserting every MR value and final counters. True missing-descriptor jump, independent +50/-50 ppm relative drift, and excessive common +5000 ppm rate fault. Existing exact-nominal wrap/split/expiry/lifecycle tests remain.

Uncertainty is explicit, not eliminated: fixed phase conversion uses nominal rate once, so the 1000 ppm rate component over maximum 150 ms initial phase contributes up to 150 us fixed mapping uncertainty; independent timestamp jitter/latency and acoustic delay remain unmeasured. A 1500 us relative integrity threshold is not sample-accurate acoustic alignment or acoustic PASS. Pair storage grew from host40544 to40592 bytes; reference + scratch +160-byte metadata allowance remains below32768 bytes. Immutable reference ring remains byte-identical.

## 3. Capture activation has one read/apply owner

Production `CaptureActivation` owns a task mutex around the event-state read AND codec apply. Both main/AFE lifecycle and input callers invoke Refresh with a fresh source-of-truth read inside this critical section. Warmup uses the same owner with a persistent hold; another Refresh cannot undo that hold. On release, it reads current event bits rather than reusing pre-delay state. CaptureRequested also respects service_stopped_ even though Stop wakes the input task by setting its active event bits. Stop and idle input-close refresh this owner; the adapter no longer directly bypasses it on EnableInput(false).

No platform I/O or sleep occurs while holding the activation mutex: only event-bit/atomic read and the short codec portMUX update. Existing input warmup delay remains existing behavior; the new observer policy below adds no sleep. Lock order is activation task mutex -> codec portMUX; ISR never acquires the task mutex. Boundary flush uses only codec portMUX and does not change capture-active ownership.

New `capture_activation_test.cc` executes this production policy with an actual two-thread controlled interleaving: pause an old active read inside owner; publish inactive bits and attempt disabling Refresh; verify it cannot complete ahead of old apply; release old read; disable completes last and leaves capture unarmed. Then delayed Refresh and warmup/disable/resume ordering remain unarmed. Forty subsequent TX/RX callbacks through production DuplexReference produce no queued RX and no overflow. This is a host ownership regression, not a FreeRTOS scheduling measurement.

## 4. Observer auto tail quarantine

Added portable `ObserverTail`, owned by AudioService queue mutex. Non-realtime StartListeningAudio requests this quarantine. Uplink stays closed while software decode/playback queues or in-flight write/decode work remain. The first idle observation starts a120ms deadline. Repeated idle polls cannot extend it. New output activity restarts the boundary, including a short write that starts and finishes between polls: AudioOutputTask increments an output-activity token under the same mutex before starting each physical write.

ReadAudioData advances this policy without a blocking delay, while local raw/AFE observation continues. Exactly once at expiry, codec CaptureBoundary atomically flushes pending RX/partials and increments capture generation. It retains physical microphone/clock, TX reference history, latched fault identity and frozen wake snapshot. The changed generation causes joint-resampler/AFE reset and rejects old raw/fetch/encode output. Only the fresh output epoch can enter cloud send. A boundary cannot invalidate the direct wake send: StartListeningAudio is invoked after that guarded batch finishes, and Boundary deliberately does not change wake generation. Valid realtime passes no quarantine and keeps the live microphone/ref epoch through interrupted stop.

The guard is an approved control policy, not proof of exact physical drain: after software drain there is no outstanding write; six configured10ms DMA descriptors have nominal60ms capacity, and120ms preserves stock warmup conservatism. Nominal24k clock configuration and reasonable functioning hardware are assumptions; analog ringing, unexpectedly stopped/restarted clocks and measured acoustic delay remain lab concerns. It does not depend on successful AEC or completed-callback availability, preserving faulted mono fallback. No reference history reset, fake silence, new local VAD abort, or main/audio guard sleep was introduced.

Regression: output still in flight -> idle but DMA tail held -> immediate AFE reset cannot open gate -> polls before120ms stay closed -> one raw generation boundary -> new output accepted, old raw/AFE/encode epoch rejected, wake identity unchanged. Additional cases cover new output resetting the idle deadline, output entirely between polls, and realtime bypass.

## TDD and exact commands/output

Ran these focused commands before new production policy interfaces/clock behavior existed:

```
python3 -m unittest discover -s firmware/tests -p test_interrupted_playback.py -v
python3 -m unittest discover -s firmware/tests -p test_duplex_reference.py -v
python3 -m unittest discover -s firmware/tests -p test_capture_activation.py -v
```

RED logs are `sdd/aec-fix-interrupted_playback-red.txt`, `aec-fix-duplex_reference-red.txt`, `aec-fix-capture_activation-red.txt`. All exit1, `FAILED (failures=1)`:

- interrupted: compiler missing `UplinkAuthorization` / CaptureEpoch.integrity (12 errors); expected new production policy absent.
- duplex: existing header compiled but runtime aborted: `Assertion failed: (d.Tx(b * 240, stamp, tx.data(), 240)), function main, file duplex_reference_test.cc, line 57.` This reproduces harmless common drift rejection.
- capture: compiler missing `family_robot::CaptureActivation`; expected ownership policy absent.

Supplementary tail tests were also added before implementation: same interrupted focused command exit1 for missing ObserverTail/Boundary (`aec-fix-tail-red.txt`). The between-polls output-activity case first failed compilation against the two-argument Advance API (`aec-fix-tail-activity-red.txt`), then passed after real production output activity tracking was wired.

Focused GREEN (same commands, final focused logs `aec-fix-*-green-final.txt`): interrupted1/1 OK0.703s, duplex1/1 OK0.882s, capture1/1 OK0.878s. After adding the output-activity case, reran interrupted focused:1/1 OK0.633s. All use C++17 -Wall -Wextra -Werror -pedantic; capture adds -pthread. Direct duplex harness compile/run also printed:

```
pair_storage=40592 reference_storage=32024
duplex behavior PASS
```

After all source changes settled, full suites:

```
python3 -m unittest discover -s firmware/tests -v
# Ran 47 tests in 4.246s / OK
cd .superpowers/firmware-development-20261022/prepared-aec-fix
python3 -m unittest discover -s scripts/tests -v
# Ran 110 tests in 6.050s / OK
```

Logs: `sdd/aec-fix-owned-full.txt`, `sdd/aec-fix-upstream-full.txt`. Subprocess environment prepended `/bin:/usr/bin` while preserving the current Python interpreter, to avoid the previously documented unrelated Homebrew bash/readline dyld failure; no global environment changed. No SDK compiler results claimed.

## Patch, provenance, self-review and limits

Regenerated tracked patch against exact locked upstream using ignored scratch, preserving original implementation and limiting this wave to affected policy/codec/service/Application changes. Only firmware source/tests/README are staged. Controller's `docs/verification/2026-10-22-usb-awake-noise.md` remains untouched/unstaged.

```
python3 firmware/tools/prepare_experiment.py --archive .superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination .superpowers/firmware-development-20261022/prepared-aec-fix
```

Fresh atomic preparation passed real git apply --check --whitespace=error-all. Verified each manifest patch/overlay SHA against owned inputs, exact native dependency lock, unchanged reference ring, unchanged stock variant/pins and exactly one factory. Patch SHA256 `6728ff56732b00b66a4636f76952a8ff377a70212bbbf33b9a10253dbad81e49`. New C++ uses pinned upstream .clang-format; clang-format23.1.3 dry-run -Werror passes; existing changed ranges formatted only. `git diff --check` passes.

Self-review additionally caught the short-write-between-polls case and removed adapter activation bypass on input-close. Read/apply serialization, queue mutex -> codec boundary lock ordering, separate frozen wake identity, identity publication, false-reference AFE handshake and output epoch checks were rechecked. No downstream wire format or command change; collector interface remains v1. Diagnostic normal behavior/privacy limitations from initial report remain.

Remaining gates: independent review of these fixes, exact-head Linux SDK compilation, target scheduling/stack/heap and capture ownership validation, physical codec/clock behavior, acoustic alignment/ERLE/near-end quality, ordinary wake/greeting and fault recovery conversation, both transports/reconnect, and observer/realtime interruption behavior including actual analog/DMA tail and USB/battery noise. Host tests are not physical AEC PASS or production readiness.
