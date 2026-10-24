# Task 1 target compile gate fixes

Status: DONE_WITH_CONCERNS — both source defects addressed in one scoped wave; exact-head ESP-IDF recompilation remains controller work. Commit: `3ac93b1` — `fix(firmware): guard ISR state with SDK mux and consume AFE lock result` (not pushed). Base `026415a`, main. No push, device/serial/flash/reset/live audio, server change or local target build.

## Actual target RED evidence

Read the closed exact logs provided by controller:

- `sdd/ci-experiment-026415a-custom-failed.log`: line3547 reports static assertion failure at software_aec_codec.cc:14 for `std::atomic<uint32_t>::is_always_lock_free`. Line3555 also reports the AFE try_lock error.
- `sdd/ci-experiment-026415a-original-failed.log`: line3553 reports the same ignored `[[nodiscard]]` `std::unique_lock<std::mutex>::try_lock()` result at afe_audio_engine.cc:472, treated as `-Werror=unused-result`.
- The custom log identifies `/opt/esp/tools/xtensa-esp-elf/esp-15.2.0_20251204/xtensa-esp-elf/bin/xtensa-esp32s3-elf-g++`, SDK6.1.0, and the real ESP32-S3 compiler. Thus the lock-free guarantee is actually false for the selected target/toolchain, not a guessed property from the host. This does NOT establish the exact implementation of every atomic operation; the fix removes that dependency instead of asserting those operations are safe.

These two actual target failures are the RED evidence for this wave. No synthetic source-text tests or host emulation of an Xtensa guarantee were substituted.

## ISR state ownership fix and design proof

Replaced adapter ISR-shared std::atomic status/epoch mirrors with plain fields and the already-existing single internal `portMUX_TYPE`. Removed the redundant mirrored capture/wake/integrity values; the authoritative plain `CaptureEpoch` is read while holding the mux. No std::atomic operation remains on the adapter callback path. The failing assertion is removed because its required mechanism has been replaced, not because the guarantee was weakened.

Task getters call `ReadState()`, which acquires the mux exactly once, copies valid/faulted/capture/wake/integrity/AEC-ready into a value, and releases. Realtime eligibility compares the fields in this one coherent snapshot. Diagnostic validity likewise uses one snapshot, releasing codec mux before entering the diagnostic queue/mux path. `NotifyAecReady` is task-only and updates readiness under the same mux, rejecting a stale generation or invalid/faulted reference. It does not call any public getter while locked.

Source grounding for the replacement primitive, all at pinned SDK commit `fff9895c82d744c7237be8847347bdd1b07c6643`:

- [Xtensa portmacro.h](https://raw.githubusercontent.com/espressif/esp-idf/fff9895c82d744c7237be8847347bdd1b07c6643/components/freertos/FreeRTOS-Kernel/portable/xtensa/include/freertos/portmacro.h), lines158-163 and393-404: portMUX uses the SDK spinlock type; ISR/task macros select the SDK critical-section APIs, including the task-context compliance variant when configured.
- [Xtensa port.c](https://raw.githubusercontent.com/espressif/esp-idf/fff9895c82d744c7237be8847347bdd1b07c6643/components/freertos/FreeRTOS-Kernel/portable/xtensa/port.c), lines441-486: entry masks interrupts and acquires the spinlock; exit releases it and restores the prior interrupt level. There is no task semaphore allocation or scheduler wait on this path.
- [spinlock.h](https://raw.githubusercontent.com/espressif/esp-idf/fff9895c82d744c7237be8847347bdd1b07c6643/components/esp_hw_support/include/spinlock.h), lines69-142: the SDK lock uses core ownership and `esp_cpu_compare_and_set`. The default portMUX entry may spin for the other core; it is NOT claimed to be wait-free. Safety relies on the short, non-sleeping critical sections already required by this adapter.

Inspected complete local call graph and every status/epoch access:

```
ISR Sent/Received
  -> Complete: portENTER_CRITICAL_ISR(mux)
       -> bounded DuplexReference Tx/Rx / counters
       -> PublishLocked: plain fault/validity/epochs only
     portEXIT_CRITICAL_ISR(mux)

Tasks: Fail / OnDeviceEpoch / SetCaptureActive / CaptureBoundary /
       InputData pairing / PollFault / PrintStats state read
  -> portENTER_CRITICAL(mux), direct plain helpers/data, exit

Task getters -> ReadState: one mux acquire/copy/release
Task AFE readiness -> NotifyAecReady: one mux acquire/update/release
```

`PublishLocked` is explicitly documented as requiring an already-held mux, and never acquires it, calls a task getter, or takes another lock. All task getters in PrintStats/RecordDiagnostic/InputData are outside any existing codec mux section. ISR never enters ReadState/NotifyAecReady, lifecycle std::mutex, diagnostic locks, Application, allocation, logging or network code. Task locks may precede codec mux; codec mux never precedes a task lock. This preserves the existing ordering without recursive acquisition.

Internal placement is unchanged: board codec is the existing function-local static object with no external-memory section annotation; pairing storage remains explicitly MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT. State/mux are members of that codec object. No ISR state was moved to PSRAM or added heap allocation. The separate reference ring is unchanged. The callback graph still requires I2S_ISR_IRAM_SAFE=n; this change does not claim full IRAM safety or measured ISR latency.

## AFE nodiscard fix

Changed the experimental control branch to consume the result directly:

```
if (codec_->SoftwareReference() && !lock.try_lock()) {
    afe_control_dirty_ = true;
    afe_iface_->fetch_with_delay(afe_data_, pdMS_TO_TICKS(20));
    continue;
}
```

A failed acquisition follows the existing discard/retry path. A successful acquisition remains owned by unique_lock through ApplyAfeControls and is released by RAII. Ordinary codecs short-circuit without acquiring the lock, preserving their existing control path. No warning suppression or discarded-result cast was added. Regenerated only this change in the upstream patch; remaining patch diff lines are hunk line-offset adjustments.

## Verification commands and outputs

Focused existing production-policy regressions, before the full suites:

```
python3 -m unittest discover -s firmware/tests -p test_interrupted_playback.py -v
# Ran 1 test in 1.045s / OK
python3 -m unittest discover -s firmware/tests -p test_duplex_reference.py -v
# Ran 1 test in 1.144s / OK
python3 -m unittest discover -s firmware/tests -p test_capture_activation.py -v
# Ran 1 test in 0.851s / OK
```

Logs: `sdd/aec-target-fix-interrupted_playback.txt`, `aec-target-fix-duplex_reference.txt`, `aec-target-fix-capture_activation.txt`. They exercise actual production lifecycle/fallback/tail/activation/pairing policies; they do not emulate ESP32 critical-section instructions. No new policy helper was needed, so no mirror test was added.

Fresh closed preparation:

```
python3 firmware/tools/prepare_experiment.py --archive .superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination .superpowers/firmware-development-20261022/prepared-aec-target-fix
# /Users/chen/code/家庭陪伴机器人/.superpowers/firmware-development-20261022/prepared-aec-target-fix
```

Real git apply --check --whitespace=error-all and atomic overlay installation passed. Manifest patch/overlay hashes match current owned files; prepared dependencies match the tracked native lock; existing reference ring is byte-identical to026415a. Patch SHA256: `295101e33da3e6863cf3e28d27b4670f0bb196046d789dc706ef8cdfdd415f47`.

Full owned and patched upstream suites after fixes settled:

```
python3 -m unittest discover -s firmware/tests -v
# Ran 62 tests in 5.150s / OK
cd .superpowers/firmware-development-20261022/prepared-aec-target-fix
python3 -m unittest discover -s scripts/tests -v
# Ran 110 tests in 6.114s / OK
```

Logs: `sdd/aec-target-fix-owned-full.txt`, `sdd/aec-target-fix-upstream-full.txt`. Test subprocess PATH prepended `/bin:/usr/bin` while retaining current Python to avoid the already-recorded Homebrew bash/readline loader issue. No global environment edit. Changed overlay C++ clang-format23.1.3 --dry-run -Werror with pinned upstream style passes; existing AFE changed ranges formatted only. `git diff --check` passes.

## Scope and remaining limits

Changed four tracked files only: adapter header/source, tracked upstream patch, and firmware README ownership explanation. No test-source change or collector/wire change; all controller spec/plan/verification documentation edits stay unstaged. No other ISR or task-only atomic behavior was changed.

Self-review covered the exact target errors, lock ownership of every plain field, task-only snapshot callers, absence of recursive codec locking, coherent readiness/generation checks, internal storage, ordinary-codec branch behavior, and prepared source provenance. Local tests establish regressions and preparation only. The corrected target sources still require controller's exact-head custom and original-variant ESP-IDF recompilation; further toolchain findings remain possible. Physical scheduling/heap/stack/ISR latency, acoustic alignment and AEC effectiveness, wake/fallback/observer/realtime operation and before/after USB/battery noise validation remain pending. No physical AEC PASS or production-ready claim.
