# Device playback receipts (candidate)

The software-AEC StopWatch variant implements `docs/protocol/playback-progress-v1.md`.
The original variant keeps `esp_codec_dev_write`, automatic DMA clearing and no
playback capability advertisement. This document describes source and host-tested
behavior; target build and physical speaker acceptance are separate gates.

## Ownership and completion

The locked SDK's `i2s_dma_tx_callback` obtains `finish_desc->buf`, invokes `on_sent`,
then optionally clears that buffer. Software-AEC disables automatic clearing and
uses **only** callback refill for TX. `SoftwareAecCodec::Complete` first feeds the
old physical bytes to `DuplexReference::Tx`, then gives that same address to
`PlaybackProgress::Complete`. The core accounts for the address's previous tag,
then installs the next queued PCM and tag, or zeros with no speech identity.

The six addresses are learned in their initial descriptor cycle and checked in
that order thereafter. Repeated, omitted or reordered callbacks, malformed sizes,
and callbacks outside the 8.5–11.5 ms cadence invalidate all tracked positions.
A faulted AEC reference does not prevent the playback callback from replacing old
DMA bytes. A disarmed callback explicitly zeroes its buffer.

Each server frame reserves one unit of unfinished decoder/output work at ingress,
with connection epoch, server playback ID, 24 kHz offset and cancellation token.
Those values travel with the packet and decoded task. A normal stop seals the
record; finalization waits for reservations, PCM queue and all owned DMA tags.
An interrupted stop clears pending software work and leaves already loaded DMA
labels until their real completion. Late cancelled tasks are rejected at each
small queue submission. New replies do not relabel old tasks or old DMA tails.

The WebSocket callback captures its connection epoch. Old socket audio, TTS,
hello and disconnect callbacks cannot mutate a new connection. TTS start is
processed synchronously before subsequent binary frames, while scheduled UI
changes carry that event's immutable identity and cancellation lease. Explicit
close claims exactly one notification before socket destruction; its queued Idle
and low-power transition checks the close epoch, so reopen supersedes it. Local
cancellation revokes already queued start events, while normal seal keeps the
lease valid for start/stop events during draining. Main-state and AFE activation no
longer reset newly admitted negotiated audio. `IsPlaybackIdle` and the ordinary
playback-drained notification include the additional PCM/DMA tail.

## Bounds and synchronization

- Production core: 12 × 240-sample PCM blocks, 6 DMA tags, 16 records; **7,320 bytes**
  on the host ABI, compile-time ceiling **7,360 bytes**. Allocated with
  `MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT` before callback registration.
- Each existing compressed packet and audio task gains 16 metadata bytes. A
  conservative bound of 384 simultaneous compressed packets and 12 audio tasks
  adds 6,336 bytes. This covers both 166-entry test/decode capacities, the 40-entry
  send queue, in-flight work and local producer packets.
- Protocol/scalar state and copied receipt fit within a 256-byte allowance.
  Report serialization uses 706 stack bytes and a string shorter than 512 bytes;
  there is no report container, task stack, timer allocation or JSON output tree.
  Total added state plus this serialization allowance is below 16 KiB, excluding
  allocator bookkeeping. Target ABI/heap peak still needs target build evidence.
- To bound report encoding, receipt negotiation accepts nonempty printable ASCII
  session IDs up to 64 bytes. Other opaque session formats retain ordinary audio
  without confirmed-playback receipts. Quote and backslash are JSON-escaped.
- Packet offsets use uint32. Crossing that compact range invalidates the block
  permanently while admitted-work accounting and ordinary playback continue.
  Output cancellation epochs saturate instead of wrapping; saturation fails
  closed. Local connection epoch saturation refuses further opens rather than reusing a
  socket identity; event-lease saturation rejects queued receipt UI events.
- The only ISR lock is the existing `mux_`. ISR code uses fixed arrays and copies,
  no allocation, mutex, atomic, network, logs or nested getters. Task submissions
  hold `lifecycle_`, then briefly `mux_`; backpressure waits outside `mux_`.
  Reset takes audio-queue mutex then `mux_`; decoder mutex is not held while
  acquiring the audio-queue mutex. The protocol mutex is never used by ISR code.

Main polls bounded snapshots at a 20 ms event-wait interval, sends at most 16
reports per pass and no more than one progress per record per 200 ms. Final
reports bypass the progress interval. No synchronous network send occurs while
holding the core lock. Reports are one-shot best effort; a disconnected/failed
transport does not manufacture a receipt acknowledgement.

## Output volume and failure modes

The locked esp_codec_dev 1.6.2 `esp_codec_dev_write` applies software volume only
when a software-volume handler exists. The pinned ES8311 implementation installs
`set_vol = es8311_set_vol`, which writes DAC register 0x32. This constructor does
not install a custom software-volume handler, so the default library does not
allocate one. Existing `Es8311AudioCodec::SetOutputVolume` and device-open volume
calls remain intact. This proof is specific to this codec/configuration.

Tracking-record exhaustion degrades to untracked ordinary speech. Unsupported
servers also keep ordinary conversation. Optional reference/tracker/diagnostics allocation, callback registration or
diagnostics startup failure switches to ordinary ES8311 output before producers
start. Both old I2S channels are disabled and destroyed (including a partially
registered callback); new channels use automatic clearing and no callbacks, and
the codec data interface is rebuilt against their handles. Receipt capability is
off, input returns to mono without a software reference, and the application retains its existing software-variant default auto mode. Original-variant constructors always select this ordinary
writer configuration. No ordinary write is issued on callback-refill channels.
Failure of channel recreation itself is a driver/hardware/resource failure and
retains the base codec's assert/ESP_ERROR_CHECK policy; optional feature fallback
is not a guarantee of audio when basic I2S setup cannot succeed. A stopped DMA stream invalidates records and may
leave finalization pending until reset/reconnection because completion cannot be
proved. No speaker or physical acoustic pass is claimed.

## Verification

`python3 -m unittest discover -s firmware/tests -v` includes native ASan/UBSan
executables that compile the actual production core and `.inc` methods. They
cover six-slot completion, zero PCM, partial spans, interruption/new reply,
normal sealing, gaps and ordering faults, bounded capacity, hardware reset,
cancellation/backpressure interleavings, ISR/task lock serialization, AEC-before-
refill order, negotiation, malformed identities, old connections and JSON bounds.
Additional cases execute actual allocation/registration/startup and channel
ownership methods with failures injected at reference/tracker/diagnostics
allocation, TX/RX registration and diagnostics startup. They verify ordinary
audio, no receipts, auto-clearing new channels, retired old callbacks and stale
output-token rejection. Actual application scheduled methods cover both variants'
explicit/disconnected close ordering, reopen before queued UI, local cancel before
queued start, and normally sealed stop. Platform APIs are host doubles; these tests do not replace ESP-IDF target builds.

Fresh preparation must apply patch 0001, patch 0002 and all tracked overlays before
running the pinned upstream `scripts/tests` and both target build variants.
