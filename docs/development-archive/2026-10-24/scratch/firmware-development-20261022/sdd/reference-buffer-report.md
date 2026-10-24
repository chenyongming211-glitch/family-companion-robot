# Playback reference history implementation report

Date: 2026-10-22. Base: `0bf3361`, primary checkout on `main`.
Scope: portable bounded playback reference history prerequisite only.

## Test-first evidence

The complete C++ behavioral harness and Python runner were written before creating
the production header. No mock ring or source-text assertions are used.

RED command:
`python3 -m unittest discover -s firmware/tests -p test_playback_reference.py -v`

Result: exit 1, one expected failed test. Compiler diagnostic:
`fatal error: 'playback_reference_buffer.h' file not found` at harness line 1.
This is the explicitly requested missing-feature RED condition.

After adding the minimum supplied implementation contract and locking comment:

GREEN command:
`python3 -m unittest discover -s firmware/tests -p test_playback_reference.py -v`

Result: exit 0; 1 test passed, 1.262 s. The runner compiles the real C++ harness
once in a temporary directory with C++17, `-Wall -Wextra -Werror -pedantic`, then
executes it. Successful compilation and execution produced no diagnostics.

Regression command (run once):
`python3 -m unittest discover -s firmware/tests -v`

Result: exit 0; 20 tests passed, 0.922 s: the new C++ test plus all 19 existing
firmware preparation tests. No unrelated Go tests were run.

Whitespace command: `git diff --check`.
Result: exit 0; no output.

## Actual behavioral scenarios

- Exact real sample append/read and a read spanning two distinct submitted blocks.
- Full 16000-sample ramp followed by three distinct samples; read the complete
  retained 16000-sample window across physical ring wrap and compare every sample.
- Expired read crossing the retained boundary leaves every output sentinel intact.
- Wholly future and partially available reads leave the complete output unchanged.
- Wrong-generation append leaves generation, bounds, size and every retained PCM
  sample unchanged; wrong-generation read leaves output and history unchanged.
- Gap and overlap append rejection each preserves the whole prior history;
  a subsequent contiguous append still succeeds.
- Unsubmitted silence is unavailable and preserves output; explicitly submitted
  zero samples are readable alongside the original submitted nonzero tail, without
  resetting generation or playback timeline.
- Negative append/read indices, nonempty null buffers, capacity-plus-one counts,
  and SIZE_MAX counts are rejected. Rejected appends preserve snapshots; rejected
  reads preserve real sentinels. SIZE_MAX append does not read beyond its small
  valid input array.
- Full-capacity Read at INT64_MAX minus one returns InvalidRange without changing
  any of the 16000 output sentinels. Append with an overflowing range is rejected
  and history preserved; no production-only index setter was introduced.
- Reset selects the new generation, empties logical history and restarts index
  zero. Old generation reads fail unchanged; old PCM is not exposed under the
  new generation. New submitted samples then read correctly.
- Zero-count null append/read at the empty timeline succeed without advancing it.

## Memory and locking

The ring is `std::array<int16_t, 16000>`: exactly 32000 PCM bytes. The actual host
`sizeof(PlaybackReferenceBuffer)` is 32024 bytes, measured by a temporary C++17
program compiled with the same warning flags. The class and harness both enforce
`sizeof(PlaybackReferenceBuffer) <= 32768` with static_assert; the harness also
asserts capacity 16000. Target ABI size is compile-time checked when compiled
there, but no target firmware build was performed in this task.

No internal locks or dynamic allocation. Reset changes only generation and bounds;
it does not clear or allocate PCM storage. README and header explicitly require
one caller-owned ISR/task-safe critical section for every operation, including
Reset, reads, and diagnostic accessors. No caller locking integration exists yet.

## Self-review and concerns

Reviewed the implementation against the supplied contract and all named cases.
No behavioral defect in the supplied starting implementation was exposed; no
algorithm change or extra production API was needed. Signed range validation
bounds count before conversion and guards addition against INT64_MAX. All
validation and availability decisions happen before any sample copy or mutation.
The 16000-sample retention bound and modulo indexing preserve newest samples.

The overflow Append test necessarily also violates contiguity because a host test
cannot reach INT64_MAX through realistic appends from Reset(0). It verifies
rejection and no mutation; the full-capacity near-INT64_MAX Read independently
exercises the signed range guard. No fake internal counter/setter was added.

This class assumes valid caller-provided memory for non-null buffers and external
synchronization; host tests do not establish real-time critical-section latency.
Hardware DMA sample positions, MR input, clocks/calibration, DSP/AEC, fail-closed
fallback, interruption queues, experimental firmware build, rollback and acoustic
field acceptance remain future work. Natural barge-in is not completed or accepted.

No hardware, serial port, raw audio capture, production server action, push or
notification was performed. No branch/worktree was created. Only the four scoped
files are staged for the local main commit; this report stays ignored.

## Local commit

`9010fc32341ecd6bfcc284e6b0b4eac20c939c98` on `main`. Exactly the four scoped
files committed; final `git status --short` was empty. No push.
