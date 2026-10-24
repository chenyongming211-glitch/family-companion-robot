# Task 1 independent re-review — R1–R3 closed

Reviewed immutable HEAD `101cb4ed662138dbea22991ae3f5d50f29fdf77f` against base `28cb072`, using `task-1-rereview.diff`, repair increment `task-1-fixes.diff`, the updated report, the original task requirements, and fresh prepared source `task1-rereview-final`. Current tracked firmware matches that HEAD. The prior review remains preserved as `task-1-review.md`.

- **Specification compliance: PASS at the source/host-review gate.** R1–R3 are closed. The implementation now meets the reviewed lifecycle/fallback requirements without changing the completed-prefix ownership algorithm.
- **Code quality: PASS at the source/host-review gate.** No unresolved actionable finding was identified in the repaired paths or their interaction with the previously reviewed core.
- **Target compilation and physical acceptance: PENDING.** This verdict does not certify ESP-IDF compilation, Xtensa memory/timing, deployed behavior, or speaker/acoustic acceptance.

## R1 — CLOSED: explicit close and delayed application state

Evidence:

- `firmware/overlays/main/audio/playback_websocket.inc:193-205` now implements both the real `NotifyPlaybackClosed` and `CloseAudioChannel`. Explicit close claims the physical socket's connection through `NotifyPlaybackClosed(socket_connection_)` before destruction. A successful claim increments the epoch, invalidates playback, and emits exactly one close notification.
- `ResetPlaybackConnection` at `:3-18` serializes that claim under `playback_mutex_`. A synchronous, delayed, duplicate, or remote-first disconnect for the retired socket fails the expected-epoch check, without suppressing the notification already claimed for the current close.
- Prepared `main/protocols/websocket_protocol.cc:78-113` checks reset success before opening and captures `socket_connection_` for the new physical socket. The original/non-negotiated variant follows the same close lifecycle.
- `firmware/overlays/main/audio/playback_application.inc:2-9` moves both low-power and Idle work into the scheduled callback and checks the supplied post-close epoch when executing it. Reopening changes that epoch and invalidates an old queued close transition. The previous immediate low-power mutation is gone.
- Prepared `main/application.cc:587` passes the epoch into that method. The shared protocol callback type and MQTT caller were updated consistently; MQTT supplies zero to retain its existing unscoped behavior.
- Epoch saturation refuses a new open and reserves the final epoch for closing the last socket; it does not silently reuse a socket identity.

`firmware/tests/playback_websocket_test.cc:185-210` exercises the actual close/protocol/application methods with negotiated and non-negotiated states, synchronous and deferred destructor callbacks, repeated close, remote-first close, and reopen before queued UI execution. This directly covers the original finding and its application-state consequence. The socket and application infrastructure remain host doubles, but the relevant control methods are production includes.

No remaining R1 defect found.

## R2 — CLOSED: cancellation lease on scheduled start/stop

Evidence:

- `PlaybackEvent` now contains immutable playback identity plus a cancellation lease (`firmware/overlays/main/audio/playback_progress.h:22-26`).
- `firmware/overlays/main/audio/playback_websocket.inc:33-36` validates both identity and lease. Validated starts increment the lease (`:52-60`), server interruptions revoke it (`:62-70`), and local abort revokes it (`:137-145`). Peer-supplied private metadata is stripped before the locally generated event metadata is attached.
- `firmware/overlays/main/audio/playback_application.inc:11-25` validates that event in the actual scheduled start/stop methods. A pre-cancel start can no longer clear `aborted_` or re-enter Speaking after local interruption.
- Normal sealing does not change the lease and the guard does not require `playback_receiving_`; a queued normal start/stop sequence remains valid while admitted audio drains. A new block's identity/lease invalidates prior scheduled work.
- Saturation rejects receipt UI events rather than reusing a lease. It is an explicit extreme counter boundary, not a routine path.

`firmware/tests/playback_websocket_test.cc:171-184` compiles and executes the real scheduled methods for pending start -> local reset/abort -> queued callback execution, and for normal stop/sealed start-stop. Its assertions retain Listening and aborted=true for cancellation and preserve the legitimate normal-stop transition.

No remaining R2 defect found.

## R3 — CLOSED: ordinary-writer fallback with exclusive channel ownership

Evidence:

- `firmware/overlays/main/boards/m5stack/stopwatch/software_aec_initialization.inc:2-32` covers missing reference/tracker/diagnostics resources and callback/diagnostics startup failure. These failures enter `UseOrdinaryOutput` before producer/AFE initialization.
- `UseOrdinaryOutput` at `:34-48` disarms acceptance under the ISR mux, records the setup fault, rebuilds the physical output ownership, then publishes ordinary mode and mono/no-reference input shape. The mode is set only after the old channels and callbacks have been retired.
- `firmware/overlays/main/audio/es8311_output_ownership.inc:15-35` asserts that `dev_` is still null; disables both channels; deletes the old codec data interface; deletes both old I2S channels, including a possible partially registered callback; sets ordinary ownership; creates/enables fresh channels; and creates a new data interface with their new TX/RX handles. No stale data-interface handle is carried into a subsequent `esp_codec_dev_open`.
- Actual prepared `main/audio/codecs/es8311_audio_codec.cc:120-166` still performs standard-mode initialization before enabling the recreated channels. `CreateI2sChannels` (`es8311_output_ownership.inc:1-10`) enables automatic clearing for ordinary ownership. The constructor's callback-refill argument defaults false, so existing original-variant callers keep ordinary ownership.
- The startup ordering was checked in prepared `main/audio/audio_service.cc:49-54` and `main/audio/audio_codec.cc:30-39`: codec Start occurs before resampler/engine setup and only loads codec settings before the new infrastructure setup. The physical codec device has not yet been opened on this fallback path. The later existing UpdateDeviceState open uses the replaced `data_if_` and retains hardware gain/volume setup.
- `software_aec_playback.inc:44-55` sends ordinary output through the ES8311 writer only in ordinary mode. Its reset token advances even without a tracker (`:2-5`), so already canceled queued output is rejected. Receipt polling/busy/capability are disabled in ordinary mode. Healthy callback-refill mode retains the previous exclusive writer path.
- `software_aec_codec.cc:157-161` routes fallback input through mono ES8311 read. `SoftwareReference()` returns false; input shape/reference flags are set before AFE configuration. This preserves the existing software-variant AEC-off/auto conversation startup.
- The pinned SDK/data-interface source supports the teardown order: `_i2s_data_close` removes its keeper registration and format state; `i2s_del_channel` requires disabled channels and frees their callback/DMA resources. These source checks supplement the host doubles.

`firmware/tests/playback_adapter_test.cc:182-218` injects reference/tracker/diagnostics allocation failure, TX registration failure, RX registration after TX success, and diagnostics startup failure into the actual production initialization/recreation/output methods. Assertions cover retired old channels, callback-free automatically cleared replacements, ordinary writes, disabled receipts, mono input, and cancellation tokens. The healthy case retains callbacks and performs no ordinary write.

No remaining R3 defect found. Failure of basic I2S/data-interface recreation itself remains the base driver's checked failure policy; it is not evidence that optional tracking failure still silently suppresses otherwise viable speech. Successful fixed objects may remain allocated for codec lifetime, but do not produce unbounded retention.

## Verification and unchanged guarantees

No passed test suite was rerun and no new targeted reproduction was needed: source inspection and the expanded production-method tests resolved the original three concerns without a new concrete uncertainty requiring execution. The existing saved logs were inspected directly:

- `firmware-review-fixed.log`: **68 tests, OK**, including production adapter initialization/fallback, core size 7,320 bytes, and protocol/application integration.
- `upstream-review-fixed.log:120-122`: **110 tests, OK** in the fresh prepared tree. Expected negative-test output elsewhere in that log does not override the final successful test result.

The six-slot actual-DMA ownership, AEC-before-refill order, fixed core storage, interrupted physical-tail treatment, normal admitted-work drain, and bounded outside-ISR receipts are unchanged by the repairs. No additional ISR allocation, network work, atomic or nested lock was introduced by the fixes. The prior core assessment continues to apply.

Remaining gates are target compilation of both original and software-AEC variants, exact target SRAM/stack and ISR cost, and separately authorized physical validation. Host fault injection does not prove driver timing on the ESP32. This review made no product edits, device/production access, commit, push, or deployment; the review report is stored in the ignored review directory.

**Disposition:** R1, R2 and R3 are closed. Task 1 passes independent source/host specification and code-quality review. It may proceed past this review gate to the parent's planned target-CI and subsequent work, while preserving the separate target/field verification boundaries.
