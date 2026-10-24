# Target compile fix source review

Reviewed range: `026415a..3ac93b1`. Verdict: **APPROVED for exact-target CI retry; no Critical, Important or Minor source finding in this scoped change.** This is source approval, not a successful target build, artifact acceptance, physical AEC PASS or production approval.

## Scope and evidence

Read the complete 19,413-byte review package `aec-target-compile-fix-review.diff`, implementation report `aec-target-compile-fix-report.md`, the complete current adapter header/source, complete affected AFE processing/control/feed scopes and relevant wake-worker callers in `prepared-aec-target-fix`. Reviewed named caller contracts in audio service, diagnostics, CaptureEpoch and DuplexReference for the newly locking task getters. The earlier whole-change review and its approved requirements remain context; this review specifically reassesses the synchronization replacement and nodiscard repair on their merits.

The range changes exactly four tracked files: `firmware/README.md`, `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.cc`, its header, and `firmware/patches/0001-stopwatch-software-aec.patch`. The patch's only behavioral change in this range is the AFE try-lock condition; its other changed lines adjust hunk offsets. No collector, wire, reference ring or task-only atomic implementation is changed.

Read-only byte comparisons confirmed the two current adapter files and current patch equal commit `3ac93b1`, and both prepared adapter files equal their owned overlays. Current patch SHA256 is `295101e33da3e6863cf3e28d27b4670f0bb196046d789dc706ef8cdfdd415f47`, matching the implementation report. During review HEAD had advanced to `530a395340c276dc15d7f7c943825d417aaf807b`; the only current README difference from the reviewed commit adds license/notice evidence wording at line 414, outside this synchronization review. Review conclusions apply to the specified source range, not unreviewed controller delivery changes.

Independently inspected existing closed evidence:

- `ci-experiment-026415a-custom-failed.log:3547`: actual Xtensa static assertion failure for `std::atomic<uint32_t>::is_always_lock_free`. Lines 3555 onward also show the ignored nodiscard result.
- `ci-experiment-026415a-original-failed.log:3553`: actual ignored `std::unique_lock<std::mutex>::try_lock()` result, rejected by `-Werror=unused-result`.
- These logs identify the real `esp-15.2.0_20251204` Xtensa ESP32-S3 compiler, rather than a host inference.
- `aec-target-fix-owned-full.txt`: 62 tests, 5.150 seconds, OK.
- `aec-target-fix-upstream-full.txt:120-122`: 110 tests, 6.114 seconds, OK. Expected negative-path asset output later in this log does not override the test result.

No suite, race, stress or target compilation was rerun. No network/device/serial/flash/reset/audio/server operation was performed. No source, index, HEAD or branch mutation was made by this reviewer; only this requested ignored review report was written.

## Strengths

1. **The failed guarantee is replaced, not weakened.** `software_aec_codec.h:55-64` and `.cc:53-81` remove the adapter's atomic status and mirrored epoch publication mechanism entirely. The authoritative CaptureEpoch plus validity/fault/readiness fields are now protected by the existing SDK mux. Removing the static assertion therefore does not leave the formerly asserted ISR atomic operations behind.
2. **Readiness uses a coherent state.** Header lines 25-28 take a single value snapshot for valid/faulted/capture/readiness comparison. `.cc:64-69` rejects stale generation, invalid reference and faulted reference while holding the same lock. A lifecycle transition either precedes the readiness update and rejects it, or follows it and changes the generation/validity used by eligibility. Diagnostic validity at `.cc:251-259` likewise uses a coherent snapshot and releases the codec mux before entering the diagnostic subsystem.
3. **ISR ownership remains explicit and narrow.** `.cc:177-195` acquires the ISR critical section once, performs the existing bounded pair/counter work, and calls only the already-locked plain-state publication helper. That helper does not call a public getter or reacquire the mux. The explicit private comments in `.h:47-55` accurately match the implementation.
4. **The AFE fix preserves the intended retry semantics.** Prepared `main/audio/engines/afe_audio_engine.cc:467-477` consumes `try_lock()` in the branch. Failure re-arms the dirty flag, fetches into discard for the existing bounded interval and retries. Success keeps ownership through ApplyAfeControls and releases through RAII. Ordinary codecs short-circuit without acquiring this lock. This fixes the real compiler diagnostic without warning suppression or changing control ownership.

## Synchronization and lifetime audit

Every access to the changed plain status/readiness fields and authoritative epochs was traced. Constructor member initialization precedes callback registration. Thereafter:

| Path | Ownership and result |
| --- | --- |
| ReadState / all task getters | Exactly one task critical entry, scalar copy, exit; no further lock or blocking work inside. |
| NotifyAecReady | Exactly one task critical entry; direct state/generation comparison and assignment; no getter recursion. |
| Fail / OnDeviceEpoch / SetCaptureActive / CaptureBoundary | Existing task critical section owns all changed status/epoch writes. Removing mirror publication leaves the authoritative epoch transition intact. |
| Sent / Received -> Complete -> PublishLocked | Existing ISR critical section owns all changed reads/writes; no task getter, standard-library atomic, lifecycle mutex, diagnostics, Application call, logging or allocation enters this callback path. |
| InputData | ReferenceFaulted snapshot is outside the explicit pairing critical section; pairing/publication stays inside; delay/fallback/lifecycle operations stay outside. |
| PollFault | Publication and fault notification decision under mux; Application scheduling after exit. |
| PrintStats | Existing pair/counter snapshot exits the mux before the public generation/eligibility getters and printing. Its separate diagnostic snapshots are not a transactional acceptance proof. |
| RecordDiagnostic | Snapshot exits the codec mux before diagnostic queue/critical-section operations. |

Named caller inspection finds no new lock cycle. AFE input/wake locks and the audio-service queue/capture/lifecycle locks can precede a short codec snapshot/update, but no codec critical section calls back into those owners or takes their locks. The diagnostics worker calls PollFault before its destination/diagnostic locking; stats calls PrintStats before its own diagnostic critical section. The AFE wake encoder's validity checks are task-context calls even when made under its wake mutex. The application, console, input, encode and send callers are also task paths, not ISR paths. No conversion to a task-only critical primitive was introduced on the callback path.

Internal placement is unchanged: the board constructs a function-local static SoftwareAecCodec, its mux/status/epoch fields remain members, and pairing storage still uses `MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT`. The callback graph retains the `I2S_ISR_IRAM_SAFE` compile guard and corresponding configuration constraint. This review does not infer IRAM safety or latency measurements from those facts.

The controller supplied the exact pinned SDK primary files during this review, and the critical-section implementation was independently read from those saved files. The source contract is interrupt masking plus the SDK core-owned spinlock, with possible spinning for the other core, not a wait-free primitive. Actual saved-file references (correcting the report's web-rendered line offsets) are:

- `research/idf61-portmacro.h:178-181,438-449,552-560`: portMUX is spinlock_t; task/compliance and ISR mappings are distinct; the default enter passes SPINLOCK_WAIT_FOREVER.
- `research/idf61-port.c:482-531`: entry masks interrupts, acquires the spinlock, and tracks nesting/prior interrupt level; exit releases and restores the prior level at the outermost exit. Lines 533-554 make task compliance calls abort from ISR, validating the importance of the inspected task/ISR caller separation.
- `research/idf61-spinlock.h:69-145,168-205`: core ownership, esp_cpu_compare_and_set acquisition/retry, recursion count and release; no allocation, semaphore or task scheduler wait on the normal path.

Local Git blob hashes independently match the controller's exact `fff9895c82d744c7237be8847347bdd1b07c6643` fetch metadata: portmacro.h `34f387ff25c766b5acf2d2ccd4b7ab76be31f56d`; port.c `558475de23195c8dec5b86251f1cda5156152118`; spinlock.h `e34f7564879e54f46d4bd5eb3ce29e4900e56bc3`. This change reuses the same SDK mux already governing every pair operation. No adapter callback critical section contains a task wait or task-lock dependency. Its bounded work and the short added task snapshots avoid a source-level lock dependency cycle; actual contention/latency still requires the planned physical measurement.

## Issues

### Critical (must fix)

None found in the reviewed change.

### Important (should fix)

None found in the reviewed change.

### Minor (nice to have)

None recorded. Additional generic helper/tests or formatting changes would not improve the evidence for the concrete target failures.

## Recommendations and remaining gates

Proceed with the controller's exact-head custom and original-variant ESP-IDF CI retry and inspect the closed results and resulting artifact identity. The existing host tests establish preparation and production-policy regression coverage; they do not execute the real SDK critical-section implementation or establish target linking success. The two known failure sites are addressed in source, but further target compile/link findings remain possible.

Retain the planned physical checks for scheduling, ISR latency/contention, memory/stack margins, cache-stall behavior, reference/acoustic alignment, wake and fallback behavior, observer/realtime operation, audible stop and USB/battery noise. The additional task snapshots are small critical sections but their timing cost has not been measured. No host-test or source-review result substitutes for those gates.

## Assessment

**Ready to merge? Yes at the scoped source-review gate; the repository uses main directly, and this verdict authorizes no physical rollout.**

**Reasoning:** Both actual compile defects have a targeted source repair. The ISR synchronization replacement consistently uses the existing internal SDK mux, avoids recursive/task-lock acquisition in callbacks, and improves coherent task-state checks; the AFE change preserves the failed/successful/ordinary-codec paths. Exact-target recompilation and later artifact/physical acceptance remain outstanding controller gates.
