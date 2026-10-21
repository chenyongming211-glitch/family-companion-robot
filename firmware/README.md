# StopWatch stock firmware baseline

This directory pins the unmodified Xiaozhi ESP32 v2.5.2 source at
`49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`, ESP-IDF v6.1 at
`fff9895c82d744c7237be8847347bdd1b07c6643`, and ESP-SR 2.4.7 for the
`m5stack-stopwatch` board/variant (`esp32s3`). The source URL, archive SHA256,
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
source "$IDF_PATH/export.sh"
EXPECTED_IDF=$(python3 -c 'import json;print(json.load(open("firmware/upstream.lock.json"))["idf_commit"])')
test "$(git -C "$IDF_PATH" rev-parse HEAD)" = "$EXPECTED_IDF"
idf.py --version
cd firmware/.cache/stock
python3 -m unittest discover -s scripts/tests -v
python3 scripts/build.py m5stack-stopwatch --name m5stack-stopwatch --language zh-CN --zip
sha256sum build/xiaozhi.bin build/merged-binary.bin > build/SHA256SUMS
```

Downloaded archives and extracted stock sources, dependencies, SDK configuration,
build products and release ZIPs live under ignored `firmware/.cache/`. Never
commit generated binaries, local credentials or vendor output. Future source
changes will be maintained as tracked patches/overlays against the lock, rather
than edits to untracked vendor output.

The private repository's **Actions → StopWatch stock firmware** workflow runs on
`main` and `codex/**` firmware changes and supports **Run workflow**. Open a
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
