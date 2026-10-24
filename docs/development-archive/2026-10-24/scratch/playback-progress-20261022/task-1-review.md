# Task 1 independent review — changes required

Reviewed immutable implementation `28cb072..0a06bc10a428eae65c7abab877a44d8506bb40bb` using `task-1-review.diff`, the frozen prepared tree, task brief/context, wire specification, and implementation report. The cited tracked overlay/patch files still match that commit. No product files were edited, no device or deployed service was accessed, and the already-passed suites were not repeated.

- **Specification compliance: FAIL / changes required.** Ordinary-conversation fallback and existing device controls are not preserved in the cases below; the queued-start cancellation race remains open.
- **Code quality: FAIL / changes required.** The core is substantially well structured, but lifecycle and application integration need three fixes before Task 2.
- **Target build: pending, not assessed as passed.** No field or acoustic acceptance is implied by this review.

## Findings

### R1 — P1: Explicit close suppresses the current connection's required close notification

**Changed locations:** `firmware/patches/0002-playback-progress.patch:348-351` and `:425-430` (prepared `main/protocols/websocket_protocol.cc:77-81` and `:178-183`). Related new method: `firmware/overlays/main/audio/playback_websocket.inc:3-7`.

`CloseAudioChannel()` now calls `ResetPlaybackConnection()` before destroying the current socket. This increments `playback_connection_`. Destruction disconnects that socket, whose callback captured the old connection number, so the new `if (!ResetPlaybackConnection(connection)) return;` rejects this legitimate close callback. No other path in `CloseAudioChannel()` calls `on_audio_channel_closed_`.

Concrete trigger: press the toggle control while Listening. Prepared `main/application.cc:857-858` only calls `CloseAudioChannel()`. The existing close handler at `:587-593` is responsible for switching to low power and scheduling Idle. It is now skipped, leaving the application in Listening after its socket has been destroyed. This regression also affects the original/non-receipt build because the new close/reset guard is unconditional.

The pinned dependency source confirms the transport path: `78/esp-ml307` v3.7.6 `WebSocket::~WebSocket` disconnects its TCP object, the TCP callback forwards to `on_disconnected_`, and `EspTcp::DoDisconnect` invokes its disconnect callback. Sources: [WebSocket v3.7.6](https://raw.githubusercontent.com/78/esp-ml307/v3.7.6/src/web_socket.cc), [EspTcp v3.7.6](https://raw.githubusercontent.com/78/esp-ml307/v3.7.6/src/esp/esp_tcp.cc). Read-only copies are saved as `review-web_socket-v3.7.6.cc` and `review-esp_tcp-v3.7.6.cc` beside this report.

**Focused evidence:** the actual `ResetPlaybackConnection` implementation returns false when the current socket callback is invoked after the explicit-close reset; the reproduction prints `explicit close callback delivered=0` (details below).

**Required fix:** preserve exactly one application close notification for explicit close, while continuing to reject genuinely stale sockets. Account for callback timing and any already scheduled close state update when reopening. Add a production-path test of `CloseAudioChannel -> socket disconnect callback -> application Idle/low-power notification`, including the original build/fallback path. Merely removing the old-socket guard would reintroduce stale-connection interference.

### R2 — P2: A queued TTS start can restore Speaking after local cancellation

**Changed location:** `firmware/overlays/main/audio/playback_websocket.inc:28-30`; caller added by `firmware/patches/0002-playback-progress.patch`, prepared `main/application.cc:641-647`. Cancellation is at `playback_websocket.inc:126-131` and prepared `main/application.cc:1235-1241`.

`PlaybackEventCurrent()` checks only negotiation plus playback identity. Local cancellation calls `ResetDecoder()` and stops receiving but leaves `playback_id_` unchanged. Therefore a `start` lambda queued before cancellation remains “current” even after that record is sealed/interrupted. Its execution clears `aborted_` and switches the application back to Speaking.

Concrete interleaving: while Listening, receive a negotiated TTS start and queue its UI event; before `MAIN_EVENT_SCHEDULE` is processed, process a wake-word event. `HandleWakeWordDetectedEvent()` cancels the reply and resumes listening, but the queued start then executes and overwrites that state. The main loop processes wake-word handling at prepared `main/application.cc:271-273` before scheduled work at `:282-288`, so this ordering is directly possible. For auto/non-realtime mode, the ensuing Speaking handler also disables the experiment uplink. The user must wait for a later server stop/another interaction despite having already interrupted the reply.

**Focused evidence:** an ASan/UBSan reproduction compiling the actual `playback_websocket.inc` prints:

```
after local cancellation: interrupted=1 finished=1 receiving=0 queued_start_still_current=1
```

**Required fix:** carry/validate an event or cancellation generation through scheduled state changes, or provide an equivalent event-specific guard that rejects a pre-cancel start. Preserve normal `stop` processing and normally sealed/draining blocks; checking `playback_receiving_` for every event would incorrectly reject legitimate stops. Add the actual application scheduling interleaving to coverage, not only identity comparison across different playback IDs.

### R3 — P2: Failure to allocate the added tracker removes ordinary playback instead of degrading safely

**Changed locations:** `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_codec.cc:27-38`, `:104-112`; `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_playback.inc:44-60`; `firmware/patches/0002-playback-progress.patch` change to `es8311_audio_codec.cc` disabling automatic clear.

If the new approximately 7.3 KiB internal-SRAM tracker allocation fails, `playback_` is null, callback registration is skipped, and `accepting_` cannot become true. Nevertheless this variant has unconditionally replaced the ordinary writer with `OutputTagged()`, whose `usable` check fails and returns for every PCM buffer. It has no safe ordinary-writer mode. Capability advertisement being false does not fix this: an old server or a server that declines receipts still follows the same silent output path. Callback/diagnostics setup failure has a similar consequence.

This is a functional regression caused by the added optional tracking resource: a device that could previously play ordinary ES8311 output can become entirely silent when just that new allocation fails. The report acknowledges fail-closed output, but the wire specification explicitly requires retaining ordinary conversation when precise tracking cannot be supported, and Task 1 requires ordinary-dialogue fallback. The documented limitation does not satisfy that requirement.

**Required fix:** choose a safe fallback ownership mode during initialization/recovery. When callback refill cannot be established, disable receipt capability and use a correctly configured ordinary writer (with appropriate clear/callback behavior), or an equivalent bounded path that retains ordinary speech. Do not mix concurrent writers with refill ownership. Exercise allocation/setup failure through the production adapter/initialization path and demonstrate untagged speech output with receipts disabled.

## Positive assessment of the reviewed core

- The DMA core owns six tags by actual buffer address; it accounts for the prior tag before replacing PCM. This is not global-counter inference or PCM-value matching. Tagged zero speech remains countable, whereas untagged silence/prompts do not increase a server record.
- The pinned SDK TX callback passes the finished descriptor buffer before optional clearing. The software-AEC branch disables after-callback clearing, and the new output wrapper does not call the ordinary I2S writer while refill mode is active.
- `SoftwareAecCodec::Complete` reads the just-sent PCM into the existing AEC reference before refill. Its playback work uses fixed storage under the existing ISR mux; no new ISR mutex, allocation, network call, atomic or log was found in that call graph.
- Task submission holds lifecycle then mux; cancellation uses the audio-queue mutex and mux and does not wait for lifecycle while resetting. The decoder replacement no longer exposes a close/open assignment gap to `ResetDecoder`. No concrete lock cycle was found in the reviewed paths.
- Ingress assigns connection/playback/offset/cancellation metadata before asynchronous decode/output. Decode failure and queue drop invalidate precision; late output after reset is rejected by its saved output generation.
- Normal seal tracks admitted work and waits PCM and DMA ownership. Interrupt clears software PCM while retaining the old physical tags. The tests exercise source gaps, tagged zero PCM, partial spans, normal drain, interruption followed by new output, address/cadence faults and bounded records. No new false-positive completed-prefix count was identified in those core transitions.
- Old-epoch hello/audio/TTS are rejected by protocol methods when they arrive after the epoch has changed; report send checks the connection/session and runs outside the ISR mux. However this does not remedy R1 or R2's application lifecycle boundaries.
- Fixed arrays and compile-time size bound support bounded tracking. The source report's SRAM accounting is plausible, but exact target ABI, allocation overhead, stack high-water and ISR execution cost remain unverified by this review.
- Original codec writer behavior is retained by its virtual default and original automatic-clear setting. R1 still changes original-variant device controls and must be fixed in both variants.

## Focused reproduction and verification limits

Only one new native reproduction was compiled, for unresolved R1/R2 concerns. `review-cancel-event.cc` reuses the existing host plumbing and directly includes the production protocol methods. It does not claim to execute a real ESP32 scheduler or transport. Its assertions establish the acceptance/guard failures; the prepared application and pinned transport source establish their consequences.

Commands (from repository root):

```sh
c++ -std=c++17 -fsanitize=address,undefined \
  -I firmware/overlays/main/audio \
  .superpowers/playback-progress-20261022/review-cancel-event.cc \
  -o .superpowers/playback-progress-20261022/review-cancel-event
.superpowers/playback-progress-20261022/review-cancel-event
```

Output:

```
after local cancellation: interrupted=1 finished=1 receiving=0 queued_start_still_current=1
explicit close callback delivered=0
```

No sanitizer diagnostics. This intentionally reproduces incorrect behavior, so a successful process exit is not a product pass. The implementation report's 68 firmware and 110 prepared-upstream test results were read, not rerun. Existing protocol tests do not execute real `CloseAudioChannel()` or the real application event loop; adapter tests instantiate a permanently available tracker rather than testing its initialization failure.

## Nonblocking limitations and follow-up gates

- Printable-ASCII session IDs of at most 64 bytes, bounded-record exhaustion, and best-effort one-shot reporting are explicit conservative limitations. They do not independently create an incorrect `valid=true` count in the reviewed code.
- After an unresolved DMA stall, invalid finalization may remain pending until reset/close; missing hardware completion is correctly not invented as software progress. Operational recovery/physical latency still need field validation.
- Actual target compilation of both variants, Xtensa SRAM/stack and timing checks, and notified physical speaker testing remain separate gates. These pending gates are not used as substitutes for the actionable source defects above.

**Disposition:** fix R1–R3 and independently re-review the fixes before opening Task 2. The core ownership design can be retained; the outstanding problems are fallback and lifecycle integration.
