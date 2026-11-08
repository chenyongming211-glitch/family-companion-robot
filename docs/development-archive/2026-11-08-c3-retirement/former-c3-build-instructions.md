# Historical C3 build instructions

Retired by the user on 2026-11-08. This is evidence, not an active support or build guide.

### Xmini C3 button entry

The independent `xmini-c3-family` variant uses board directory `xmini/c3`,
ESP32-C3, ES8311 mono16/24k audio, SSD1306 OLED and the original GPIO wiring.
The current candidate enables button-start continuous conversation with playback-
period MR capture and standalone AEC. Local wake words, the full S3 AFE and the
color face remain disabled. This variant always
uses WebSocket; retained old MQTT credentials cannot select another transport.
Its compiled OTA URL is empty. USB provisioning must supply the product
bootstrap URL and this device's own WebSocket credentials before first boot.
The button ends an
active conversation, including while the assistant is speaking. Processed capture
continues through playback and reuses the common server interruption/drain flow.
Opus work
keeps a capture epoch through encoding and transport, so closed/reopened input
cannot send an old frame or mix its partial PCM with a new capture.

`FamilyC3Codec` reuses the bounded playback ledger with callback-owned DMA.
Only actual TX descriptor completion counts as played. TX copies the completed
physical PCM before refill; RX pairs actual mono samples into bounded MR history
(12,816 bytes). A new capture discards its boundary descriptor and invalidates old
readiness. Joint 24k→16k conversion feeds aligned 512-sample standalone AEC frames.
TTS keeps the capture/DSP generation; close/reopen resets it. Allocation, callback, format,
timing and read faults invalidate evidence rather than claiming completion.
The product variant does not execute the upstream board's eFuse write.

Prepare and build on a cloud host or Actions in the exact locked SDK:

```sh
python3 firmware/tools/prepare_experiment.py --target esp32c3
cd firmware/.cache/experiment
python3 scripts/build.py xmini/c3 --name xmini-c3-family --language zh-CN --wake-word disabled --zip
```

C3 uses `dependencies.esp32c3.lock` and its adjacent SHA256; default preparation
still uses the original S3 lock. The C3-only ESP-SR 2.6.1 pin is applied to a fresh
manifest with recorded provenance; S3 keeps 2.4.7. The selected SR_LOW_COST kernel
and original Audio/AUTO Opus combination consumed about 59.4% CPU in a synthetic
C3 resource probe. That is not speaker-echo or physical interruption acceptance.
The C3 lock was captured in an isolated IDF configure run, with the original
button 4.2.2 pin retained; ESP-SR adds its esp-dl dependency. Target conditions
remove unsupported S3 components and add the upstream C3 servo-controller
dependencies. Those dependencies do not add a product servo feature. The manual
dependency-inspection workflow produces inspection records only, never an
installable or approved firmware package.

The dedicated `partitions/xmini-c3-family-16m.csv` preserves the observed 2.2.6
layout: NVS `0x9000+0x4000`, otadata `0xd000+0x2000`, PHY `0xf000+0x1000`,
two `0x2f0000` application slots at `0x20000` and `0x310000`, and a 2MiB
asset partition at `0x600000`. The upper 8MiB is unallocated and retained.
Oversize app/assets fail verification; no automatic repartitioning occurs.
The verifier checks native dependency bytes, effective board/target settings,
C3 image chip IDs, source/binary layouts, image capacity and flash addresses.
These addresses describe the new build; inspect the actual device backup before
deciding which regions may be written. Do not install an S3 merged image.

Target compilation, installation/readback and physical audio acceptance are
separate gates. See the [approved scope](../docs/superpowers/specs/2026-10-31-xmini-c3-button-entry-design.md)
and [implementation plan](../docs/superpowers/plans/2026-10-31-xmini-c3-button-entry.md)
for the original button baseline. The [voice interruption validation](../docs/verification/2026-10-31-c3-voice-barge.md)
tracks the newer candidate; the installed stable image remains half-duplex until
candidate installation and physical acceptance are recorded there.
