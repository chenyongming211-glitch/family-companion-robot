# StopWatch stock firmware baseline

This directory pins the unmodified Xiaozhi ESP32 v2.5.2 source at
`49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`, ESP-IDF v6.1 at
`fff9895c82d744c7237be8847347bdd1b07c6643`, and ESP-SR 2.4.7 for the
`m5stack/stopwatch` board directory and `m5stack-stopwatch` variant (`esp32s3`). The source URL, archive SHA256,
archive root and SDK identity are recorded in `upstream.lock.json`.

From the repository root, with Python 3.12 or later:

```sh
python3 -m unittest discover -s firmware/tests -v
python3 firmware/tools/prepare.py
```

Preparation verifies the archive checksum and every member before extraction,
strips its top-level root and prints only the prepared path. It never executes
upstream code and refuses an existing destination. To use a local archive and a
fresh separate destination:

```sh
python3 firmware/tools/prepare.py --archive /path/to/xiaozhi-v2.5.2.tar.gz --destination /path/to/fresh-stock
```

The default lock is always resolved from the script location, including when the
command is invoked from another working directory. Local archives undergo the
same checksum, path, entry-type and 64 MiB compressed/expanded size checks.
Downloads use a 30-second timeout and are retained only after verification.

In the ESP-IDF v6.1 environment, run the same stock build as CI from the repository
root (the SDK checkout must match the exact pinned commit):

```bash
(
  set -e
  source "$IDF_PATH/export.sh"
  EXPECTED_IDF=$(python3 -c 'import json;print(json.load(open("firmware/upstream.lock.json"))["idf_commit"])')
  test "$(git -C "$IDF_PATH" rev-parse HEAD)" = "$EXPECTED_IDF"
  idf.py --version
  cd firmware/.cache/stock
  python3 -m unittest discover -s scripts/tests -v
  python3 scripts/build.py m5stack/stopwatch --name m5stack-stopwatch --language zh-CN --zip
  sha256sum build/xiaozhi.bin build/merged-binary.bin > build/SHA256SUMS
)
```

Downloaded archives and extracted stock sources, dependencies, SDK configuration,
build products and release ZIPs live under ignored `firmware/.cache/`. Never
commit generated binaries, local credentials or vendor output. Future source
changes will be maintained as tracked patches/overlays against the lock, rather
than edits to untracked vendor output.

The repository's **Actions → StopWatch stock firmware** workflow runs on
`main` firmware changes and supports **Run workflow**. Open a
successful run matching the exact Git commit and download the
`stopwatch-stock-<github.sha>` artifact within its seven-day retention period.
It contains the source lock, resolved dependency lock, SDK configuration, app and
merged binaries, generated assets, bootloader, partition table, flasher arguments
and binary checksums. These records are needed to inspect the flash layout and
preserve existing NVS during any separately authorized future experiment.

A successful stock build is neither AEC validation nor a flashing instruction.
This workflow does not invoke esptool, change a device, or upload to an OTA
endpoint. The separate experimental source below implements software reference, observation
and opt-in realtime gating. Its target build and physical acoustic acceptance
remain separate from stock CI.

The complete upstream MIT license is in `third_party/xiaozhi-LICENSE`; see the
repository's `THIRD_PARTY_NOTICES.txt` for attribution. Dependencies retain their
own licenses.

## Experimental source preparation

Prepare a fresh experimental tree from the same locked upstream archive:

```sh
python3 firmware/tools/prepare_experiment.py
# Or use an already verified local archive and a fresh destination:
python3 firmware/tools/prepare_experiment.py --archive /path/to/xiaozhi-v2.5.2.tar.gz --destination /path/to/fresh-experiment
```

The default output is `firmware/.cache/experiment`. Default inputs resolve beside
the script, independently of the working directory: `upstream.lock.json`,
`patches/`, `overlays/`, and `dependencies.lock` with its adjacent
`dependencies.lock.sha256`. The dependency lock is the exact native YAML record
from the verified stock build, including esp_codec_dev 1.6.2 and ESP-SR 2.4.7.
Preparation checks its SHA256, regular file type and 1 MiB size bound; IDF validates
the native dependency schema during a subsequent build.

Preparation applies regular `patches/*.patch` files in lexical filename order,
checking each with real `git apply --check --whitespace=error-all` before applying
it. An absent or empty patch directory is valid. Patch application strips
inherited `GIT_*` overrides and prevents parent Git repository discovery. Each
check/application has a 30-second timeout. Overlays preserve their relative
layout and add regular files only: any collision with upstream or patched paths
fails. Root `dependencies.lock` and `.family-robot-source.json` are reserved for
generated output and cannot be overlays. Symbolic patch and overlay inputs are
rejected. Overlay paths also cannot traverse symlinks introduced by patches.

All work happens in an owned temporary directory under the destination's parent.
Only after extraction, patches, overlays and dependency verification succeed is
the completed source tree renamed to the destination. An existing destination,
including a dangling symlink, is refused; failures remove only this invocation's
temporary tree. Successful commands print only the resolved prepared path.

The generated `.family-robot-source.json` records the upstream commit and archive
SHA256, ordered relative patch paths and hashes, overlay paths and hashes, and
dependency-lock SHA256. It records no environment values. Keep the prepared tree
and subsequent build products under ignored `.cache/`.

This command prepares experimental source only. The tracked patch and overlays
now connect the software reference to completed DMA and AFE. Preparation and
portable host tests do not validate target timing or natural barge-in.

## Playback reference history prerequisite

`overlays/main/boards/m5stack/stopwatch/playback_reference_buffer.h` provides a
portable, sample-indexed history for one playback generation. Its fixed ring
retains the newest 16000 signed 16-bit PCM samples: exactly 32000 bytes of PCM
storage, with the whole object bounded to 32768 bytes by a compile-time assertion.
It performs no dynamic allocation and has no internal lock. Callers must protect
every operation, including reset, reads and diagnostic accessors, with the same
ISR/task-safe critical section.

Appends must be contiguous and match the current generation; rejected appends
leave the history unchanged. Reads report expired or unavailable ranges and leave
the entire output unchanged unless ready. Missing samples never become invented
silence; zero samples must be explicitly submitted. Reset only for a hardware
sample-timeline restart, not playback stop or interruption: an already submitted
tail and subsequent explicit zeros must remain readable. Reset changes logical
bounds and generation without clearing the PCM storage.

The immutable ring is consumed by `duplex_reference.h` and the adapter below.
The host C++ behavior harness runs through Python test discovery; it does not
establish device timing, acoustic performance or natural barge-in acceptance.


## Completed-DMA AEC experiment

`0001-stopwatch-software-aec.patch` changes locked upstream files; overlays add
owned files. The unique `m5stack-stopwatch-software-aec` release variant retains
the StopWatch pins, physical mono ES8311 open, flash/partition selection and one
board factory. The original `m5stack-stopwatch` variant/config remains available.
In an exact locked ESP-IDF environment, the canonical experimental build is:

```sh
cd firmware/.cache/experiment
python3 scripts/build.py --list-boards
python3 scripts/build.py m5stack/stopwatch --name m5stack-stopwatch-software-aec --language zh-CN --zip
```

No SDK build or physical-device test is implied by the host checks. This source
is a laboratory experiment, with acoustic acceptance still pending. No automatic
local VAD interruption is added. Both transports share the same `tts.stop`
handling: only a string `reason` equal to `interrupted` clears pending decode and
playback. Ordinary stop continues to drain normally. The boolean is copied while
the cJSON object is alive, before scheduling on the main task.

### Timing, storage and lifecycle

The locked esp_codec_dev 1.6.2 reconfigures the ES8311 path to mono16 at24000Hz.
The adapter verifies STD/mono/left/16-bit, nominal24000Hz,768000Hz BCLK, six DMA
buffers and480 bytes per completed descriptor. Callbacks register with both
channels disabled. The initial stereo constructor epoch is ignored; each mono
open discards the first boundary descriptor independently on TX/RX before
anchoring counters. `on_sent` copies the buffer that actually completed TX,
before the driver's `auto_clear_after_cb`; submitted/write-FIFO samples never
become reference. Completed zero DMA is real silence. RX copies from `on_recv`
before the driver's internal queue eviction; that unused stock RX queue's
expected overflow is deliberately not the experiment's overflow signal.

For each direction, origin microseconds = first retained completion stamp minus
10000. For an RX sample index `r`, reference index is:

```
r + round((rx_origin_us - tx_origin_us) * 24000 / 1000000) - fixed_offset_samples
```

Rounding occurs once on epoch phase. All later positions are integer counters;
packet splits and ring wraps introduce no new rate conversion. Positive offset
looks back; `CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES` is bounded to[-2400,2400]
(default0). A negative initial index is warmup and is discarded, never fed to AFE.
Future reference waits up to150ms; expiry, sequence/format gaps, RX overflow and
missing clocks latch a fault. Cadence checks distinguish common clock error from
lost/relatively drifting streams: adjacent callbacks must be within1500us plus
10us rate allowance of10ms; independent10-second measurement windows allow
1000ppm common rate error plus1500us timestamp uncertainty. TX/RX relative
completion phase, compared at equal integer sample index, must remain within
1500us of its initial value plus1000ppm allowance for unequal latest indices.
Thus harmless common clock error does not accumulate against the first nominal
timestamp, while gaps and growing relative drift still fault. Pairing origins and
sample history are never periodically reset; the relative budget is a bounded
integrity uncertainty, not proof of sample-accurate acoustic alignment.
The timestamp is ISR completion observation, not acoustic speaker time. Phase
quantization is at most half a physical sample; callback latency, jitter,
clock error and acoustic delay are not established by this calculation and
must be measured. The allowed1000ppm rate error over the maximum150ms initial
phase adds up to150us fixed mapping uncertainty. The1500us relative/cadence threshold is an integrity guard, not an
acoustic accuracy promise or a substitute for calibration.

The unchanged ring stores16000 physical samples:666.7ms at24k, with32000 bytes
of PCM. Reference object plus240-sample scratch and bounded reference metadata
stay below32KiB. The separate RX queue holds16×240 samples (160ms;7680 PCM bytes).
The portable pairing object measures40592 bytes on the host (ABI-dependent) and
is explicitly allocated in internal SRAM; ISR state also uses internal storage
and lock-free32-bit atomics. One portMUX protects every pairing operation; ISR
work is bounded copies, counters and stamps only. The callback graph is not
IRAM-safe, so both Kconfig and a compile guard require `I2S_ISR_IRAM_SAFE=n`.
Flash/cache stalls can therefore trip a cadence fault and must be field-tested.

Codec lifecycle/read-fallback/output writes serialize through a task mutex,
then the existing ES8311 device mutex when needed. DMA callbacks never take
those mutexes. The audio input task consumes10ms AFE or60ms test requests in
whole sample order. The same existing stateful24k→16k resampler processes both
MR channels together. There is no independent per-channel rounding.

Capture is armed only when the input task actually consumes data, after warmup.
Output-only boot sounds, Wi-Fi setup, wake-detection suspension and intentional
processing pauses discard pending RX/partial frames without a false overrun.
Capture resumption, hardware restart and the first fault change the capture
generation. AFE/input/output partial buffers and joint resampler state reset;
20ms fetch polling and discard-only draining allow reset acknowledgement even
when no new frame can safely be fed. Old in-flight AFE/encode results and queued
cloud packets carry epochs and are rejected after the gate changes. A separate
fault/hardware integrity identity lets an explicit next-wake mono authorization
survive intentional warmup/pause/resume while a new fault revokes that grant.
Capture activation serializes the event-bit read and codec apply together across
all callers; warmup holds activation off until its owner releases it. Service stop
and idle input-close updates use the same owner.

A fault immediately revokes reference validity/eligibility and invalidates send
epochs; a worker schedules one main-task mode-off/channel-close operation. The
next wake uses ordinary auto conversation with actual mono input and a zero
padding slot to retain the fixed logical MR format. That padding is not claimed
as a reference: the AFE handshake disables AEC, including its wake path, before
feeding fallback data. Successful suspend/resume cannot clear a latched fault;
a valid physical close/open may recover non-setup faults, while setup failures
require reboot/reinitialization. A missing DSP resource can prevent audio from
working at all; resource exhaustion is never reported as healthy fallback.

Observer auto additionally holds uplink closed for120ms after software playback
queues and write/decode in-flight flags first become idle. Polling does not extend
the deadline; new output invalidates that idle boundary. A write-activity token
also detects writes that begin and finish between polling calls. Local observation keeps
running. At expiry, a single atomic RX/partial flush changes capture generation,
then joint-resampler/AFE resets reject pre-boundary raw/fetch/encode work before
fresh cloud output. It preserves TX history, the hardware microphone and frozen
wake batches. Valid realtime bypasses this quarantine. This is the stock-equivalent
120ms control guard, conservatively beyond six nominal10ms DMA descriptors after
the last write finishes; it is not a measured physical/acoustic drain guarantee.
No extra sleep is introduced in the main/audio path for this guard.

The stock standby wake-audio/greeting path is retained. Its software-experiment
encoder uses tagged batches and a persistent worker. Suspension after detection
preserves the valid frozen wake snapshot; resumption, hardware transitions and
faults invalidate it. Raw cache, asynchronous encode queue and direct main-task
send all check generation. A valid mono fallback wake batch remains permitted.
Other boards retain their original wake encoder path. Bytes already handed to a
transport before a fault cannot be retracted; subsequent queued/in-flight encode
results are gated. A physical output chunk already in progress at interrupted
stop can still play, plus DMA already submitted (up to approximately120ms from a
60ms write and six10ms descriptors under nominal clocks). Its completed-DMA
reference remains intact; interruption never resets the reference or microphone.

### Local lab controls and bounded capture

Only the unique variant selects USB Serial/JTAG as the PRIMARY console, disables
the old secondary-USB stdout choice, and reads its stdin on a low-priority task.
The original variant's UART console is unchanged. Commands are bounded to95
characters; repeated pending realtime commands are rejected. Nothing records or
sends diagnostic audio at boot or merely on USB connection.

```
aec_stats
aec_diag_start <RFC1918-IPv4> <port> <seconds>
aec_diag_stop
aec_realtime on
aec_realtime off
```

Use a receiver on your own private IPv4 network; port1..65535 and duration1..120
seconds are mandatory. Start syntax uses single spaces. Public destinations,
invalid/overflowing numeric fields and extra arguments are rejected. Starting a
new window invalidates old queued packets. Stop and expiry prevent old-window
transmission. A window started over USB can continue after unplugging, permitting
a separate battery/USB-noise comparison. Raw PCM belongs outside Git.

At boot, local raw+AEC observation continues during speaking, while cloud dialogue
uses auto behavior. `aec_realtime on` is accepted only after a valid pair and a
successful AFE output in the current generation, then rechecked on the main task.
It is the operator's explicit post-measurement choice, not an acoustic-pass
assertion. The choice is volatile and resets off on reboot. Off closes the audio
channel but retains valid local AEC observation. `aec_stats` prints aggregate
format, generation, eligibility, fault code, counters, queue depth, overruns,
callback phase, offset and diagnostic drops. For the experimental flag only,
application TTS/STT/wake/custom/alert content logs and explicit protocol payload,
OTA activation/body/URL, MCP content, Wi-Fi SSID and BLUFI password logs are
suppressed; UI captions and normal conversation transport remain unchanged.
This is a narrow app-source log audit, not a privacy guarantee for every SDK or
managed dependency: their target logs still require inspection before retaining
or sharing a console capture. The diagnostic switch controls only the extra
raw MR/AFE UDP stream, not the ordinary cloud conversation. The unique build
also explicitly disables the upstream unconditional audio-debugger stream.

The diagnostic producer uses a preallocated12-packet queue (about14.5KiB PSRAM),
with internal queue control and two4096-byte low-priority task stacks. Only the
UDP worker creates a socket or performs nonblocking network writes; no socket is
created until an explicit window has data and a network interface is up. Queue
and send failures increment a visible counter. Every datagram is at most1200
bytes, split only at complete frames. Sequence/position advances even when queue
insertion drops, so loss cannot appear as continuous capture.

### Diagnostic wire v1

Exactly48 bytes of header, followed by signed PCM16 little endian. Integer fields
are explicitly serialized little endian; compiler struct layout is never used.

| Byte offset | Size | Field |
| --- | --- | --- |
| 0 | 4 | ASCII `SWAC` |
| 4 | 1 | Version1 |
| 5 | 1 | Stream1=raw MR after joint resampler;2=AFE mono before encoding |
| 6 | 1 | Channels:2 for MR,1 for AFE |
| 7 | 1 | Flags: bit0 valid reference; bit1 first packet after stream reset/window/generation |
| 8 | 4 | Per-stream sequence within window, including failed enqueue attempts |
| 12 | 4 | Capture generation |
| 16 | 8 | Stream-local sample-frame position |
| 24 | 8 | Producer observation timestamp, monotonic microseconds |
| 32 | 4 | Rate16000Hz |
| 36 | 2 | Frame count per channel |
| 38 | 2 | Payload bytes=`frames×channels×2` |
| 40 | 4 | Aggregate diagnostic drop count at header creation |
| 44 | 4 | Capture window id |

Raw channel0 is microphone; channel1 is completed-TX reference. Flags bit0 clear
must never be scored as AEC-valid. Each stream has its own sample origin; AFE
reset may happen while the raw resampler continues. A reset sets bit1 and starts
that stream's position at0. Sequences remain monotonic within the window even
across resets. A lost reset marker is accompanied by a sequence gap and/or
position discontinuity. Reboot starts a new collection session. Raw/AEC producer
timestamps and sample positions do not imply zero DSP delay: segment at gaps,
resets and invalid flags, then estimate delay/alignment within valid acoustic
segments. The collector is a separate task; this change defines its wire only.
