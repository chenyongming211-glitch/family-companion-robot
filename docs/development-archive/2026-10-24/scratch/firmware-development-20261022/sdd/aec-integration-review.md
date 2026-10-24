# Spec compliance: issues found

**Spec compliance: ❌ Needs fixes. Code quality: Needs fixes.** Reviewed `fcf6465..7bee4db` from the supplied review package. The subsequent controller documentation commit is outside this code review. Three implementation defects prevent approval: next-wake auto fallback loses its send authorization during capture restart, valid common clock drift latches a reference fault, and concurrent capture activation updates can re-arm a deliberately paused consumer.

## Strengths

- Completed TX DMA is the actual reference source, copied before driver clearing; the adapter does not append write-FIFO PCM. `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.cc:145-174`; locked IDF excerpt `research/idf61-i2s-common.c:833-861` and `research/idf61-i2s-types.h:69-70` under `.superpowers/firmware-development-20261022/` substantiate the callback contract.
- The portable pairing implementation performs real MR assembly, integer-indexed matching, bounded holdback, overflow/expiry faulting, and a reference-storage static assertion. Tests exercise actual production headers with independent microphone/reference samples, wrap/split, invalid input, pause/resume, and a 12-second timeline. `firmware/overlays/main/boards/m5stack/stopwatch/duplex_reference.h:44-116,172-184`; `firmware/tests/duplex_reference_test.cc:15-148`.
- Software logical MR remains separate from the mono device open; both channels use the existing joint input resampler. The variant retains the stock board entry and factory/pins, explicitly selects primary USB Serial/JTAG, disables the old unconditional audio debugger, and leaves the stock SEND_WAKE_WORD_DATA setting unchanged. `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.cc:23-24`; `firmware/patches/0001-stopwatch-software-aec.patch:370-421,1094-1160`.
- Observer/realtime gating, capture-generation resets, AFE wake-path reference validity checks, tagged asynchronous wake batches, and a direct wake-send validity check are implemented rather than relying on a comment or a compile-only helper. `firmware/patches/0001-stopwatch-software-aec.patch:159-177,331-362,440-479,620-981`.
- Interrupted stop copies the exact-string boolean before scheduling, calls the existing decoder reset, and does not reset microphone/reference history. Diagnostics have explicit bounded windows, a documented endian-stable header, bounded packets, and a separate UDP worker. `firmware/patches/0001-stopwatch-software-aec.patch:70-83`; `firmware/overlays/main/boards/m5stack/stopwatch/aec_diagnostics.cc:42-131,168-229`; `aec_diagnostic_wire.h:12-76` in that same overlay directory.

## Important findings

### [P1] Grant fallback authorization for the resumed capture, not the pre-start generation

**Primary location:** `firmware/patches/0001-stopwatch-software-aec.patch:335-350` (prepared `main/audio/audio_service.cc:200-215`). Related: patch `:217-218,426-432`; prepared `main/application.cc:1127-1129`, `main/audio/audio_service.cc:790-808`; `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.cc:128-141`; `firmware/overlays/main/audio/experiment_gate.h:20-23`.

The required next-wake auto conversation cannot retain a microphone uplink after a reference fault. `StartListeningAudio()` calls `SetExperimentUplink(true)` before `EnableVoiceProcessing(true)`. The former stores the current capture generation as `permitted_fault_generation_`. The latter schedules input warmup, and input resume increments the capture generation even while the reference fault remains latched. On the first resumed read, `ReadAudioData()` observes that new generation and calls `UpdateExperimentGate()`, which rejects it because it differs from `permitted_fault_generation_`. The requested flag stays true, but nothing authorizes the new generation afterward, so AFE mono fallback can run while all encoded microphone output remains gated off.

This is a deterministic lifecycle ordering issue for a wake from idle after a fault, not a missing hardware acceptance result. The valid fallback wake preroll may still upload, which can hide that subsequent live microphone packets are absent.

**Fix:** carry the explicit auto-listening authorization across the intended capture-start handshake, or grant it after the final resumed generation is established. Keep a separate fault-transition identity so a new fault/hardware restart still revokes old work; simply accepting every future capture generation would weaken the fail-closed boundary. Add a behavior regression covering fault → idle wake snapshot → start listening → warmup/pause/resume → first new mono encode, asserting both rejection of old AEC packets and acceptance of the new fallback packets. Existing `interrupted_playback_test.cc:33-36` only verifies a static fallback wake batch and does not exercise this ordering.

### [P2] Absolute nominal-rate cadence check faults on harmless common clock drift

**Primary location:** `firmware/overlays/main/boards/m5stack/stopwatch/duplex_reference.h:148-154`.

Every completion is compared against the first timestamp plus total samples divided by exactly 24000, with the same fixed 1500 μs tolerance for the entire hardware epoch. Consequently, any sustained difference between the audio clock and the timer accumulates until a permanent cadence fault, even when TX and RX share that difference, remain perfectly paired, and have no dropped DMA blocks. Pausing capture does not re-anchor either clock. This can also accumulate while only playback or standby wake capture is active.

**Focused reproduction performed:** compiled this production header in a temporary directory with C++17, `-Wall -Wextra -Werror -pedantic`. Submitted contiguous 240-sample TX/RX blocks with identical completion stamps `stamp=(block+1)*10000+(block+1)/2`, consuming each MR frame. This is a synthetic common +50 ppm timebase difference, with zero relative TX/RX drift and no losses. Output:

```text
common_50ppm_fault block=3002 elapsed_us=30021501 fault=3 tx=720240 rx=720240
```

Both streams had matched continuously until the code rejected block 3002 at about 30 seconds. This does not claim the physical board has a measured 50 ppm error; it proves that ordinary bounded common clock error is structurally interpreted as data loss. The existing 12-second exact-nominal timeline cannot detect this defect.

**Fix:** distinguish adjacent callback gaps/descriptor discontinuities and relative TX/RX drift from bounded common clock-rate error. Preserve integer sample indexing and a bounded uncertainty model, with a permitted clock-rate range or appropriate measured/shared-clock cadence tracking. Do not merely widen an absolute epoch-long tolerance or reset the reference history periodically. Add multi-minute common-drift and true missing-block/relative-drift regressions through the production pairing API.

### [P2] Serialize capture-state observation and application across tasks

**Primary location:** `firmware/patches/0001-stopwatch-software-aec.patch:358-361` (prepared `main/audio/audio_service.cc:223-226`). Related call sites: patch `:324,432,484,501,509`; prepared `main/audio/audio_service.cc:97-101,310-313,339-345,376-386,767-772,809-814`.

`UpdateCaptureActive()` is called from the input task and from main/AFE lifecycle paths. The event-group read and `SetCaptureActive()` call are not one serialized operation. A valid interleaving is:

1. Input reads active bits and is suspended before applying them.
2. A disabling path clears the last active bit, reads inactive bits, and calls `SetCaptureActive(false)`.
3. Input resumes and applies its stale active snapshot, calling `SetCaptureActive(true)` after disable.
4. It finishes at most the current read, then blocks in `xEventGroupWaitBits()` because all consumers are inactive.

The ISR now continues filling the armed 16-block RX queue without an expected consumer and latches Overflow after about 160 ms. The codec's portMUX protects each individual update but does not order the earlier event-group snapshot; it cannot prevent this race. This violates the explicit intentional-pause/Wi-Fi/inactive-consumer requirement and can turn a routine pause into persistent experiment fallback.

**Fix:** serialize the active-state read/apply operation across all callers, or give one task ownership of capture activation with an acknowledged disable/start protocol. Include warmup's direct `SetCaptureActive(false)` transition in the ownership design. Add a deterministic interleaving regression that delays an old activation snapshot until after disable and proves it cannot re-arm capture or overflow while idle.

## Scope checks and remaining verification

- Read the brief, implementation report, and full supplied diff first. Used the requested task-reviewer method. The first combined read was output-truncated; the diff was then read in bounded sections to cover all changes.
- Named focused source checks outside full diff context: codec `Start()`/open ordering and the mono reconfiguration contract; full `StartListeningAudio()` and enable/warmup bodies to evaluate fallback-generation ordering; full input wait loop and all added capture-state call sites to evaluate the concurrent pause race; complete AFE feed/reset/worker and encode queue bodies to assess lock/generation boundaries; exact IDF DMA callback/get-info and codec mono mapping excerpts to verify reference source and format assumptions. Several diff hunks ended mid-function, so the corresponding prepared functions were read for their missing context. No broad repository crawl or new Git operation was performed.
- Locked codec source maps device `channel=1` to active left mono, consistent with the selected format. IDF's `bclk_hz` is a stored integer configuration value returned by `i2s_channel_get_info`, not an independent measurement of physical cadence. No separate strict-BCLK defect is asserted from the available excerpt.
- No evidenced unchanged suite was rerun. The report's 46 owned tests and 110 prepared upstream tests remain prior implementer evidence. The only new executable check was the focused production-header common-drift reproduction above; compilation output was clean. The initially reported upstream test failure is tied to the host Homebrew Bash/readline loader and was followed by a clean `/bin/bash`-compatible run; it is not attributed to this patch.
- ⚠️ Exact-head target compilation, target scheduling/heap/stack, acoustic offset, tail measurements, battery/USB noise, and physical conversation acceptance remain controller CI/lab gates. They are not counted as implementation findings here. No SDK build, device/serial/reset/flash access, live audio capture, network/server access, source edit, commit, or push was performed.

## Assessment

**Task quality: Needs fixes.** The implementation follows the intended architecture and has meaningful portable tests and explicit ownership boundaries, but its lifecycle composition breaks the required fault fallback and leaves an avoidable capture race. The clock guard also rejects a continuous valid shared timeline. Resolve these three findings and add focused lifecycle/drift regressions before marking Task 1 complete.
