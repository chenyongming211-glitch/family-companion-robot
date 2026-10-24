# Task 1 implementation report — R1–R3 repaired, pending re-review / target CI

The final R1–R3 repair-wave section at the end supersedes the initial implementation
limitations and frozen-tree reference below.

## Status and scope

Implemented only firmware Task 1 on main. No Go edits, branch/worktree, push,
physical device access, flashing, production service changes or audio capture.
Owned implementation and tests are ready for independent specification/code
review. This report does not claim ESP32 target compilation or speaker acceptance.

The frozen fresh source tree is `task1-frozen-prepared` beside this report. It was
prepared from `.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz`, because the
brief's expected firmware/.cache archive was absent. Pinned upstream archive hash,
source revision, SDK revision and dependencies.lock are unchanged.

## Design and source evidence

- Upstream 49b4380b3faa0ceadd28a3e78aaf0f311c46c57e, IDF
  fff9895c82d744c7237be8847347bdd1b07c6643, esp_codec_dev 1.6.2.
- Pinned `idf61-i2s-common.c:824–870` obtains `finish_desc->buf`, passes it to
  `on_sent`, explicitly syncs callback-refilled bytes where needed, then optionally
  clears. Callback-refill is an SDK-supported ownership path. No SDK file modified.
- Original variant keeps auto_clear_after_cb=true and base codec writer. Software
  AEC sets false and calls no i2s_channel_write / esp_codec_dev_write for playback.
- Six physical DMA addresses own six tags. Complete first consumes the *previous*
  address tag, checks the continuous source prefix, then replaces PCM+tag from a
  12-entry fixed queue. It never identifies audio by PCM similarity or global TX
  totals. Tagged all-zero speech counts; untagged silence/prompts do not.
- `software_aec_dma.inc` contains the actual production ISR method and executes
  `pair_->Tx` before refill. It clears disarmed buffers so stale data cannot loop.
- Cadence checks are conservative (8.5–11.5 ms per 240-sample callback). Repetition,
  omission/order faults and bad format invalidate exact progress. ISR stalls
  additionally invalidate through the existing diagnostic worker's timeout poll.
- `playback_websocket.inc` contains actual negotiation, ingress, reset and send
  methods. Every socket callback captures its connection epoch. Receive admission
  fixes identity/offset/cancel token before UI scheduling or decode. Scheduled TTS
  state changes carry private validated event identity, never current-at-execution
  identity. Old hello/audio/TTS/disconnect callbacks cannot mutate a new connection.
- Each ingress frame reserves unfinished work. The decoded task retains metadata.
  Normal Seal waits all reservations, PCM queue and DMA tags; temporary underflow
  is not EOF. Interrupted reset discards software work and keeps real DMA tail
  ownership; late cancelled work does not invalidate an otherwise known prefix.
- New replies after a normal sealed block preserve old pending output. The Opus
  decoder resets at the new block's first packet. Decoder replacement now keeps
  its mutex across close/open so ResetDecoder cannot race handle assignment.
- `IsPlaybackIdle` includes core PCM/DMA occupancy. Main's 20 ms poll drives the
  existing drained notification, including observer-auto and notification paths.
- Main copies bounded snapshots under the codec mux, then sends outside locks;
  max 16 per pass, progress minimum interval 200 ms, final bypasses the interval.
  The server session is checked at send. A failed/disconnected send is best effort,
  with no invented transport acknowledgement or persistent replay queue.

## Volume proof

Pinned ADF reference `esp_codec_dev-1.6.2.c:200+` installs default software volume
only if codec or codec->set_vol is absent; `:317+` applies sw_vol only if installed.
`es8311-pinned.c:737` installs `base.set_vol = es8311_set_vol`; setter around `:350`
writes ES8311_DAC_REG32. The actual constructor has no custom software-volume
handler. Hardware SetOutputVolume/device-open gain calls remain unchanged. This
is specifically an ES8311/configuration proof, not a guarantee for other codecs.
Physical reference observes the pre-DAC digital PCM that was actually sent.

## Memory, lock, fallback boundaries

See tracked firmware/PLAYBACK_PROGRESS.md for the persistent explanation.

- Actual host sizeof(core)=7,320; target compile-time bound <=7,360. Internal SRAM
  allocation explicitly uses MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT.
- New packet/task metadata is 16 bytes. Conservative 384 compressed packets plus
  12 task slots/in-flight producers adds <=6,336 bytes. This covers the existing
  two 166-entry testing/decode queues, 40 send slots and live producer packets.
- Protocol/scalar/receipt allowance 256 bytes. Report serializer uses 706 bytes of
  existing main stack and a string shorter than 512 bytes, no JSON output tree or
  extra task stack/timer. Added state plus serialization allowance <16 KiB before
  allocator bookkeeping. Exact target ABI/heap peaks remain a target-CI check.
- Fixed metadata state is protected by existing mux_ once per operation; ISR uses
  no mutex, atomic, allocation, network or log. Task lifecycle -> mux; audio queue
  -> mux; reset audio queue -> decoder. Decoder never takes audio queue while its
  mutex is held. Backpressure waits outside mux. Whole reset does one queue clear,
  not repeated PCM compactions.
- Receipt negotiation currently accepts printable ASCII nonempty session IDs up
  to 64 bytes so the serializer is bounded; other opaque session forms downgrade
  to ordinary audio. Quote/backslash escaping is covered. This is an explicit
  safe capability limitation, not a claim that all opaque IDs are supported.
- Compact per-packet offsets are uint32. Crossing this limit permanently
  invalidates precision but continues work accounting/audio. JSON counters are
  uint64 with a 2^53-1 exact-integer ceiling. Output cancellation tokens saturate
  and fail closed; connection exhaustion disables receipts instead of reuse.
- Record exhaustion permits ordinary untracked audio. Healthy unsupported servers
  retain legacy speech behavior. Infrastructure allocation/callback registration
  failure fails the refill path closed, with no unsafe mixed original writer.
- A stopped DMA engine can leave invalid finalization pending until reset/close;
  no software-only event substitutes for the missing completion.

## TDD / regression evidence

First core run failed for the missing production header. Later behavioral red
runs caught (and were fixed): normal finalization after a submitted-prefix gap
incorrectly retaining valid=true; cancellation-token uint32 wrap; late cancelled
output unnecessarily invalidating the already-known interrupted prefix.

Native C++17 executables use -Wall -Wextra -Werror -pedantic, ASan and UBSan.
They compile the real production core and .inc methods, not mirrored transitions.
Host platform doubles supply only cJSON/platform/class plumbing. Tests cover:

- submitted vs completed count; six buffers; tagged zero and nonzero speech;
  prompts/underflow; partial valid spans and zero padding;
- normal seal with admitted work; interruption clearing and physical DMA tails;
  old final/new active identity; consecutive normally sealed blocks;
- stale connections/IDs, wrong session, fractional ID, sequence/cadence gaps,
  format/hardware reset, queue and record bounds, numeric limits;
- task dequeue -> reset -> late output, reset during queue backpressure, stale
  prompt token, real adapter lock serialization against a concurrent fake ISR;
- actual Complete method observes AEC bytes before refill and zeros disarmed DMA;
- actual server hello mutual negotiation, TTS admission, stop/reset, old callback
  connection guards, immutable scheduled-event identity, report JSON bounds.

## Exact verification results

1. `python3 -m unittest discover -s firmware/tests -v`
   `firmware-final.log`: **Ran 68 tests in 11.922s; OK**.
   Native output:
   - production adapter cancellation/ISR serialization passed
   - playback core cases passed; bytes=7320
   - production WebSocket identity/stop/reconnect/fallback integration passed
2. Fresh preparation command:
   `python3 firmware/tools/prepare_experiment.py --archive .superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination .superpowers/playback-progress-20261022/task1-frozen-prepared`
   Succeeded, with generated patch/overlay SHA256 provenance.
3. Fresh upstream `python3 -m unittest discover -s scripts/tests -v`:
   `upstream-final.log`: **Ran 110 tests in 5.623s; OK**.
   Invoked from a Python subprocess with a temporary PATH directory containing
   bash -> /bin/bash and python -> current sys.executable. First unmodified PATH
   run had 6 errors because Homebrew bash aborts before executing tests:
   `dyld: Symbol not found: _rl_completion_rewrite_hook`, incompatible readline.
   No host installations/settings changed; tests and source unchanged for rerun.
4. Unstaged source `git diff --check` was clean before staging. Staged-check
   then reported whitespace on unified-patch blank context lines (the required
   one-space diff marker) and one extra EOF blank line in the protocol include.
   Those are formatting findings, not patch application errors; prepare applies
   the patch with `--whitespace=error-all` successfully. The core/wrapper source
   and the fresh tree are current.
5. Target build: NOT RUN. idf.py unavailable; Docker CLI reports daemon unavailable.
   Parent will dispatch locked original/software-AEC matrix after independent
   review. No firmware binary/target success/field evidence claimed here.

## Review handoff and concerns

Review actual frozen commit, especially callback ring/cadence assumptions and
cross-task application integration. Host doubles prove algorithmic behavior and
lock ordering used by these methods; they do not measure Xtensa ISR duration,
physical DMA latency, heap fragmentation or acoustic behavior. The target build
must validate both variants, SRAM/stack use, include paths and dependency pins.
No known unhandled code correctness issue remains from self-review. Session-format
capability restriction, infrastructure fail-closed output, best-effort sends and
stalled-DMA final pending state are explicit limitations above. Task 2 remains
behind the parent's independent review gate.

Implementation commit: `0a06bc1` (main, not pushed).

## Independent review repair wave R1–R3 (supersedes initial failure-mode notes)

All three findings were reproduced and repaired together. Initial implementation
commit remains `0a06bc1`; this section describes its follow-up repair commit.
Final fresh tree: `task1-rereview-final`. The earlier `task1-frozen-prepared` and
initial fail-closed optional-feature limitation are historical, not current.

### R1 — explicit close / queued application notification

Actual `CloseAudioChannel`, `NotifyPlaybackClosed` and `ResetPlaybackConnection`
now claim the current socket's close before destroying it. A successful claim
increments the connection epoch and invokes the close handler exactly once;
synchronous, deferred, repeated and remote-first callbacks are rejected after the
claim. The application receives the post-close epoch and schedules both low-power
and Idle together. Its actual `playback_application.inc` method checks that epoch
at execution; a reopened socket invalidates the queued transition. The same path
runs when receipts are not negotiated. MQTT passes epoch zero to retain its
existing lifecycle behavior. Epoch exhaustion reserves the last value for close
and refuses subsequent opens instead of reusing a socket identity.

Directed red evidence before repair: native production close-path assertion
`close_notifications == 1` failed. Green tests now execute socket-destructor
callbacks (synchronous/deferred), repeated explicit close, remote callback first,
reopen before queued application work, negotiated and non-negotiated cases, and
the saturation boundary. Actual `ScheduleAudioChannelClosed` is compiled into the
native harness, not reimplemented in its fake application.

### R2 — immutable application event lease

`PlaybackEvent` carries identity plus a uint32 cancellation lease. Receive adds
private lease metadata only after validating the TTS event; peer-provided private
markers are stripped. New starts, local abort and server interruption revoke old
leases. `PlaybackEventCurrent` validates both identity and lease. Normal stop does
not revoke a lease or require `playback_receiving_`, so already queued normal
start/stop events remain valid while admitted audio drains. Saturated event leases
fail closed for queued receipt UI transitions rather than accepting stale events.

Directed red evidence before repair: queued-start cancellation reproduction failed
its `!speaking` assertion. Green tests use the actual scheduled application methods
for receive start -> enqueue UI -> local cancel/reset -> execute queued UI; the
result stays Listening with aborted=true. Normal stop after cancellation and a
normally sealed start/stop sequence are also covered. No DMA owner/core counting
algorithm was changed by this repair.

### R3 — exclusive ordinary-writer fallback during initialization

The ES8311 constructor has an optional callback-refill selector, default false for
all original callers; the software-AEC constructor explicitly selects true. The
actual shared `CreateI2sChannels` method configures `auto_clear_after_cb` as the
inverse of that selector. Healthy refill channels retain existing six-descriptor
ownership and never call the ordinary writer.

Optional reference/tracker/diagnostics allocation, TX/RX callback registration,
or diagnostic startup failure invokes `UseOrdinaryOutput` during constructor or
Start, before AudioService starts producer tasks and before dev_ opens. It stops
acceptance under the ISR mux, marks tracking invalid, disables both old channels,
deletes their codec data interface and channels, then recreates channels with
automatic clearing and no callbacks. A new codec data interface uses the new
handles. The mode is published only after old callback ownership is gone.
`OutputTagged` then uses the existing ES8311 writer, preserving hardware volume;
input returns to mono with no software reference and receipts remain disabled.
The software-variant Application already defaults to AEC off/auto listening; no
new startup mode notification or control change is needed. Cancellation tokens
advance even when tracker allocation failed, rejecting stale queued output.

Pinned source support: `AudioCodec::Start` loads settings only; AudioService calls
it before resampler/AFE initialization and task Start. AFE derives input shape
and aec_init from the codec's input_reference flag. Pinned codec data-interface
`_i2s_data_close` (codec-data-i2s-1.6.2.c:746+) clears sample-format state and keeper
registration; it does not retain independently owned playback channels. Pinned
IDF `i2s_del_channel` (idf-i2s-common-pinned.c:1314+) requires stopped channels and
tears down channel resources. `RecreateDuplexForOrdinaryWrite` asserts dev_ is null
and checks disable/delete/create errors. No SDK or dependency pins were changed.

Directed red evidence before repair: actual OutputData with a missing tracker
failed `allocation_failure.written == pcm.size()`. The final harness no longer
merely nulls a pointer: it compiles production initialization, channel recreation,
channel configuration, output dispatch and ISR .inc methods. It injects each of
reference/tracker/diagnostics allocation failure, TX registration failure, RX
registration after TX success, and diagnostics Start failure. Each case verifies
old channels deleted, no callback on replacements, automatic clearing enabled,
ordinary speech written, receipts/pending false, mono input flags, and stale token
rejection. The healthy case confirms callbacks and no ordinary writes. An extra
callback invocation in ordinary mode cannot overwrite its DMA bytes. SDK calls
and diagnostic dependency APIs remain host doubles; target lifecycle timing is
not claimed from these tests.

Initialization fallback retains any successfully allocated fixed objects for the
codec lifetime, including diagnostics objects whose startup might have partially
created a worker. No new PCM retention is added. Core remains 7,320 host bytes
with 7,360 compile ceiling; added mode/pin/lease scalars fit the existing 256-byte
metadata allowance. Basic I2S/data-interface recreation can itself fail under
hardware or global resource exhaustion: the base assert/ESP_ERROR_CHECK policy
then applies. Ordinary-audio fallback is proven for optional feature failures
when basic codec setup succeeds, not for unavailable hardware/global heap.

### Final verification and handoff

- `python3 -m unittest discover -s firmware/tests -v`:
  `firmware-review-fixed.log`: **Ran 68 tests in 7.656s; OK**.
  Native ASan/UBSan production wrappers/core all pass; output includes
  `production adapter cancellation/ISR serialization and initialization fallback passed`
  and `playback core cases passed; bytes=7320`.
- `python3 firmware/tools/prepare_experiment.py --archive .superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination .superpowers/playback-progress-20261022/task1-rereview-final`:
  success, full patch0001 + patch0002 + current overlay application, locked archive.
- In that fresh tree, `python3 -m unittest discover -s scripts/tests -v`:
  `upstream-review-fixed.log`: **Ran 110 tests in 5.818s; OK**. Same temporary PATH
  bash/python shim documented above; no host installation or test edits.
- `git diff --check -- firmware/overlays firmware/tests firmware/PLAYBACK_PROGRESS.md`:
  clean. All .inc trailing extra EOF blank lines removed. Git's outer diff still
  flags single-space blank context markers inside the unified patch; these are
  required patch syntax and retained. Fresh prepare validates patch application
  with whitespace-error checking; added source whitespace is clean.
- Target ESP-IDF original/software-AEC builds remain pending; no physical device,
  service, live audio, deployment or push was performed. No Go files were edited.
  Root's design-spec change is excluded from staging.

Ready for independent re-review of R1–R3 and subsequent target CI. No known
unresolved source defect from this repair self-review. Hardware/ISR timing,
allocator failure during basic channel recreation, target ABI and acoustic
acceptance remain explicit verification boundaries, not host-test claims.

Repair-wave commit: `101cb4ed662138dbea22991ae3f5d50f29fdf77f` on main, not pushed.
Only the root-owned design-spec change remains modified after commit.

## CI host-fixture GCC warning repair

The `101cb4e` CI run failed before target compilation. Its sole compiler error,
recorded in `ci-aec-101cb4e-failed.log:308`, was GCC's
`-Werror=misleading-indentation` in `cJSON_GetObjectItem`: an early `if` return and
the following independent declaration shared one indented line. Replaced it with
an explicitly braced guard and separate declaration/return. Checked the added
native fixtures and expanded the analogous allocation-failure, callback-register
failure and deferred-callback one-line sequences. These are formatting-only test
changes: no production logic, assertion, compiler flag or warning policy changed.

Verification command:
`python3 -m unittest discover -s firmware/tests -p test_playback_progress.py -v`

`native-gcc-fixture-fix.log`: **Ran 3 tests in 2.690s; OK**. The actual production
core, adapter/initialization/ownership and WebSocket/Application native harnesses
all passed with ASan/UBSan and unchanged `-Wall -Wextra -Werror -pedantic`.
Local compiler is Apple clang 21.0.0; Linux GCC verification and target CI retry
remain the controller's next steps, not claimed by this local run.
`git diff --check` for the two changed fixture files is clean. No additional
production problem identified; no Go/root-owned files touched or staged.

Fixture-only commit: `17bb1e001f9f25926071a6a8871426ae52235e4a` on main; not pushed.
