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
endpoint. Software reference, AEC, realtime capture, interruption behavior and
physical acoustic acceptance remain separate, unimplemented or unvalidated work.

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

This command prepares experimental source only. Copying the playback reference
header into that tree does not connect it to playback, DMA, capture or AEC, and
does not validate natural barge-in.

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

This is a bounded-history prerequisite only. It has no DMA positions, clocks,
calibration, DSP, AEC or firmware integration yet. The host C++ behavior harness
runs through the Python test discovery command above; it does not establish
device timing, acoustic performance or natural barge-in acceptance.
