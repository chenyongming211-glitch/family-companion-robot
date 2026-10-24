### Spec Compliance

- ✅ Spec compliant for Task 2's software delivery scope at `acec6d0192e13a8ea14f12536d9a506d0f56f804` against base `cb85517`. The review package changes only the five requested files plus the expressly permitted CI evidence helper and its focused tests. No Task 1 runtime source is changed.
- ✅ The collector consumes the settled 48-byte SWAC v1 layout, validates all length/stream/channel/rate/flag/frame bounds before output, and preserves the original datagram and PCM bytes (`firmware/tools/capture_aec.py:25-44`, `:129-140`, `:194-196`). The independently checked authoritative producer offsets agree (`firmware/overlays/main/boards/m5stack/stopwatch/aec_diagnostic_wire.h:58-77`).
- ✅ Privacy and bounds are implemented, rather than merely documented: explicit invocation, numeric IPv4 and duration/budget validation, new 0700 directory, exclusive 0600 files, source/endpoint filtering, datagrams read with an oversize-detection byte, two stream states and at most two WAV handles, per-packet cost including index/packet/WAV overhead, and an 8192-byte final-summary reserve (`firmware/tools/capture_aec.py:47-50`, `:83-109`, `:117-155`, `:157-179`, `:202-224`). The budget is total logical file lengths; filesystem allocation blocks and directory metadata are not represented as file bytes.
- ✅ Independent timelines, invalid-reference labeling, reset/generation/window/gap/disorder segmentation, and sequence high-water tracking preserve uncertainty without zero padding or invented alignment (`firmware/tools/capture_aec.py:57-80`, `:143-151`, `:180-199`). Timeout and explicit stop are checked between bounded receive operations; the CLI SIGINT handler allows the current packet to finish before normal finalization (`:117-126`, `:202-224`, `:237-248`).
- ✅ The two isolated jobs use the official specified IDF container, existing pinned actions, fresh preparation, exact SDK/checkout identity checks, upstream/firmware host tests, canonical variant builds and seven-day variant/SHA artifact names, with no hardware/deployment step (`.github/workflows/firmware-experiment.yml:11-59`). The helper rejects native lock drift and mismatched preparation provenance, checks critical component versions and effective settings for both variants, and copies allowlisted build/config evidence plus source inputs with SHA256 coverage (`firmware/tools/verify_experiment.py:31-100`, `:103-145`).
- ✅ The field package identifies the actual private backup, publishes usable geometry rather than copying backup contents, distinguishes backup-selected ota_0 from the unverified current hardware slot, protects NVS/otadata/phy, and conditions writes on app/bootloader/table/assets checks. Shared-assets restoration and same-slot app restoration are concrete; automatic dual-slot recovery is not promised (`docs/verification/2026-10-22-software-aec-field-test.md:9-45`, `:98-106`). AEC observation/calibration precedes explicit realtime enable, with ten-round acoustic/speaker evidence and USB-noise investigation remaining separate (`:47-96`).
- ⚠️ Cannot verify from this diff: actual target compilation, resolved target sdkconfig/dependency output, downloaded artifact digest/CRC/image fit/partition/assets/bootloader compatibility, current device OTA state, physical recovery, acoustic calibration or natural barge-in acceptance. These are explicitly controller/field-owned gates in the brief and remain pending; workflow/helper implementation is not evidence that those gates passed (`.github/workflows/firmware-experiment.yml:30-59`; `docs/verification/2026-10-22-software-aec-field-test.md:3`, `:9-45`, `:77-106`).

### Strengths

- Real synthetic loopback tests inspect saved packet framing and unchanged PCM, not only mock calls or text shapes; they check independent stream origins and permissions (`firmware/tests/test_capture_aec.py:41-89`). Gap/replay/reset/validity tests inspect individual saved segments; malformed input tests verify only the valid payload reaches disk (`:91-146`). Budget, timeout, source filtering, stop and actual subprocess SIGINT cases check finalized output (`:148-206`).
- Evidence tests exercise both configurations, byte-level dependency drift, SDK/provenance rejection, unsafe/missing config and offset rejection, and the copied evidence/hash inventory (`firmware/tests/test_experiment_evidence.py:67-118`). Synthetic build files are clearly identified as fixtures, not ESP images (`:1`).
- Configuration exceptions are narrowly confined to the two known dependency-disabled symbols, while all positively required AEC/console settings remain mandatory (`firmware/tools/verify_experiment.py:68-100`). The checksum pin is checked against both checked-in and resolved lock bytes (`:37-40`).
- Scope and responsibility remain understandable: a 254-line stdlib collector and a 148-line narrow build-evidence helper, each with its own behavioral tests; documentation consistently separates host, target-artifact and acoustic evidence (`firmware/tools/capture_aec.py:1-7`; `firmware/tools/verify_experiment.py:1-5`; `firmware/README.md:391-419`).

### Issues

#### Critical (Must Fix)

- None found in the reviewed Task 2 change.

#### Important (Should Fix)

- None found in the reviewed Task 2 change.

#### Minor (Nice to Have)

- None identified that warrants a task finding.

### Assessment

**Task quality:** Approved for software delivery; controller-owned target/artifact and physical gates remain open.

**Reasoning:** The implementation matches the bounded/private collector contract and provides reviewable two-variant CI and recovery instructions. The changed tests assert actual retained bytes and finalized files, while neither helper nor documentation converts synthetic/CI success into acoustic or hardware recovery acceptance.

### Review Evidence and Scope

- Read the complete supplied 65,963-byte review package in sequential bounded chunks; changed-file judgments use that package's complete context. Read the brief and delivery report independently. No test suite was rerun; the implementer's reported final 62-test result is retained as a reported run, not independently repeated evidence. No focused reproduction was necessary because no specific unanswered behavioral doubt remained after code review.
- Named outside-diff contract check — **producer/consumer wire incompatibility:** inspected the authoritative `aec_diagnostic_wire.h` and settled `aec-wire-interface.md`; header offsets, channels, rate, flags and independent stream origins match collector parsing. Producer implementation/runtime correctness itself remains Task 1/broad-review scope.
- Named outside-diff contract check — **CI preparation/build output and disabled-config mismatch:** inspected `firmware/tools/prepare_experiment.py`, `firmware/upstream.lock.json`, the pinned baseline workflow, and only relevant canonical build/config portions of the already prepared real source. Preparation default destination/provenance structure match the workflow/helper; canonical ZIP naming avoids duplicate manufacturer prefixes and matches the bundle glob (`firmware/.cache/aec-delivery-source/scripts/build.py:85-95`, `:132-154`, `:1520-1693`). The original board entry is unchanged and the experiment entry explicitly enables its required AEC/console settings (`firmware/.cache/aec-delivery-source/main/boards/m5stack/stopwatch/config.json:5-33`); AFE defaults on under S3+PSRAM and stock device AEC is dependency-disabled (`firmware/.cache/aec-delivery-source/main/Kconfig.projbuild:987-1024`). Searches for an independently saved stock sdkconfig at three plausible existing locations found no file, so this review does not claim target-generated console-config verification; actual effective output remains a CI/artifact gate.
- Named outside-diff contract check — **unsafe rollback geometry or invented slot certainty:** read only the existing private `backup-integrity.json` and `backup-ota-metadata.json`; published sizes/geometry/hash and offline ota_0 selection agree, and both evidence and field document preserve the current-hardware uncertainty. No backup bytes or private evidence contents were copied into this report.
- Read repository `AGENTS.md`. No source, index, HEAD, branch or worktree mutation; no device/serial/flash/reset/live audio/server operation. The only written file is this requested review report.
