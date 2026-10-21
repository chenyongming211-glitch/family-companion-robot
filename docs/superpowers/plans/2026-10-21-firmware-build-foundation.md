# StopWatch 固件构建基线实施计划

> **For agentic workers:** Use superpowers:subagent-driven-development for this task. User approved the device-side AEC design and the private repository. Execute the prerequisite without another approval prompt. This plan covers the reproducible stock build; AEC implementation and physical validation remain explicitly uncompleted after it.

**Goal:** 从固定源码在 Linux CI 构建未修改的 StopWatch 固件，保存依赖、配置、分区和镜像证据，为后续自然插话补丁提供可回退基线。

**Architecture:** 主仓库只保存源码锁定记录、下载校验脚本、构建工作流与说明；上游固件解压至忽略的 `firmware/.cache/`。上游代码不直接混入 Go 模块。保持现有 Go 网关和运行中的设备不变。

**Tech Stack:** Python >=3.12、GitHub Actions、Espressif 官方 ESP-IDF v6.1 容器、上游小智 v2.5.2。

## Global Constraints

- 用户已批准自然插话实验，但这项构建任务不打开串口、不刷机、不重启设备、不部署服务端。
- 保留所有原有 Go 功能；不得提交密钥、设备备份、原始音频、原始抓包、`.superpowers/` 或生成的固件镜像。
- 当前使用已注册的隔离 worktree，分支 `codex/natural-barge-in`，不得切换主目录分支或覆盖主目录私有材料。
- 版本和 SHA 使用下列实查值；SDK 选择 v6.1 遵循上游 AGENTS.md 和官方 CI，满足设计的 >=6.0.1 要求。
- 构建成功只证明原版源码可编译，不证明设备端 AEC 或自然插话可用。

### Task 1: Locked stock StopWatch build

**Files:**
- Create: `firmware/upstream.lock.json`
- Create: `firmware/tools/prepare.py`
- Create: `firmware/tests/test_prepare.py`
- Create: `firmware/README.md`
- Create: `firmware/third_party/xiaozhi-LICENSE`
- Create: `.github/workflows/firmware-baseline.yml`
- Modify: `THIRD_PARTY_NOTICES.txt` (add firmware source/license attribution)

**Interfaces:**
- `prepare_source(archive: Path, destination: Path, checksum: str, expected_root: str) -> Path` verifies checksum and archive entries before extracting into a fresh destination; raises `ValueError` for checksum/root/path/link/size violations and `FileExistsError` for existing destinations.
- CLI `python3 firmware/tools/prepare.py` reads the adjacent lock file, downloads missing archive to `firmware/.cache/`, creates `firmware/.cache/stock`, and prints only the prepared path. Optional `--archive PATH --destination PATH` allow a local verified archive or separate build directory; never overwrite an existing destination.
- Workflow performs stock build through upstream canonical `scripts/build.py`, without AEC patches or local hardware secrets.

- [ ] **Step 1: Write behavioral failing tests.** Use `unittest`, `tempfile`, and real `tarfile` archives; dynamic-load `firmware/tools/prepare.py` via `importlib.util`. Test valid nested file extraction with the top-level root stripped; checksum mismatch leaves destination absent; a `../escape` member is rejected without writing outside destination; symlinks and hardlinks are rejected; unexpected top-level root is rejected; an existing destination with a sentinel remains unchanged. A real fixture helper is:

```python
def archive_at(path, name="source/main/file.txt", kind=tarfile.REGTYPE):
    with tarfile.open(path, "w:gz") as output:
        info = tarfile.TarInfo(name)
        info.type = kind
        if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
            info.linkname = "../../outside"
            output.addfile(info)
        else:
            data = b"source contents"
            info.size = len(data)
            output.addfile(info, io.BytesIO(data))
    return hashlib.sha256(path.read_bytes()).hexdigest()
```

Run `python3 -m unittest discover -s firmware/tests -v` before implementation; record the expected missing-module/API failure. Tests must verify file contents and unchanged sentinels, not source text or implementation details.

- [ ] **Step 2: Create the lock file with these exact values.**

```json
{
  "schema_version": 1,
  "repository": "https://github.com/78/xiaozhi-esp32",
  "tag": "v2.5.2",
  "commit": "49b4380b3faa0ceadd28a3e78aaf0f311c46c57e",
  "archive_url": "https://codeload.github.com/78/xiaozhi-esp32/tar.gz/49b4380b3faa0ceadd28a3e78aaf0f311c46c57e",
  "archive_sha256": "b3460397122eff7edeb8c07bd9c26e2c216b24cc2e5c2d836ec2209ba5be0c63",
  "archive_root": "xiaozhi-esp32-49b4380b3faa0ceadd28a3e78aaf0f311c46c57e",
  "idf_version": "v6.1",
  "idf_commit": "fff9895c82d744c7237be8847347bdd1b07c6643",
  "esp_sr_version": "2.4.7",
  "board": "m5stack/stopwatch",
  "variant": "m5stack-stopwatch",
  "target": "esp32s3"
}
```

- [ ] **Step 3: Implement minimal safe source preparation.** Compute SHA256 using streaming reads; reject an archive over 64MiB. Open `r:gz`, validate every entry before extraction: exactly the configured top-level root, no absolute path or `..` component, only regular files/directories, no links or device nodes, cumulative declared file sizes <=64MiB. Strip the root, extract with Python's `data` filter into a temporary sibling directory and move the prepared tree to a destination that does not exist. Clean temporary data on failure and refuse an existing destination before touching it. Download with `urllib.request` using a 30-second timeout and 64MiB limit into a temporary file; only keep the download after checksum validation. Do not execute downloaded scripts during preparation.

Implementation uses stdlib only. Keep extraction logic in `prepare_source`, CLI/download in small helpers and `main()`. Bind the CLI lock path from the script location so invocation from another cwd cannot select another lock file. Clearly report controlled errors without dumping environmental credentials.

- [ ] **Step 4: Add the CI workflow.** Use this complete workflow as the starting point; resolve any real toolchain/build incompatibility rather than hiding it. Changes to workflow mechanics must preserve source/SDK checks and artifacts.

```yaml
name: StopWatch stock firmware
on:
  push:
    branches: [main, 'codex/**']
    paths: ['firmware/**', '.github/workflows/firmware-baseline.yml']
  workflow_dispatch:
permissions:
  contents: read
jobs:
  stock:
    runs-on: ubuntu-latest
    timeout-minutes: 60
    container:
      image: espressif/idf:v6.1
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7
        with:
          persist-credentials: false
      - name: Test source preparation
        run: python3 -m unittest discover -s firmware/tests -v
      - name: Prepare verified stock source
        run: python3 firmware/tools/prepare.py
      - name: Build stock StopWatch
        shell: bash
        run: |
          source "$IDF_PATH/export.sh"
          EXPECTED_IDF=$(python3 -c 'import json;print(json.load(open("firmware/upstream.lock.json"))["idf_commit"])')
          test "$(git -C "$IDF_PATH" rev-parse HEAD)" = "$EXPECTED_IDF"
          idf.py --version
          cd firmware/.cache/stock
          python3 -m unittest discover -s scripts/tests -v
          python3 scripts/build.py m5stack/stopwatch --name m5stack-stopwatch --language zh-CN --zip
          sha256sum build/xiaozhi.bin build/merged-binary.bin > build/SHA256SUMS
      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7
        with:
          name: stopwatch-stock-${{ github.sha }}
          retention-days: 7
          if-no-files-found: error
          path: |
            firmware/upstream.lock.json
            firmware/.cache/stock/dependencies.lock
            firmware/.cache/stock/sdkconfig
            firmware/.cache/stock/build/xiaozhi.bin
            firmware/.cache/stock/build/merged-binary.bin
            firmware/.cache/stock/build/generated_assets.bin
            firmware/.cache/stock/build/bootloader/bootloader.bin
            firmware/.cache/stock/build/partition_table/partition-table.bin
            firmware/.cache/stock/build/flasher_args.json
            firmware/.cache/stock/build/SHA256SUMS
```

Record dependency lock, SDK config and flasher/partition artifacts because the existing device must retain NVS when experiment flashing is eventually performed. This task never invokes esptool or uploads firmware to an OTA endpoint.

- [ ] **Step 5: License and docs.** Copy the complete upstream MIT license from the verified archive to `firmware/third_party/xiaozhi-LICENSE`. Add attribution to THIRD_PARTY_NOTICES without assigning a new license to our own code. `firmware/README.md` must show pinned source/SDK, the exact local prepare and stock build commands above, where ignored outputs live, how to find the private Actions artifact, and an explicit statement that stock build is neither AEC validation nor a flashing instruction. Explain that future source changes will be maintained as tracked patches/overlays against the lock, not edits to untracked vendor output.

- [ ] **Step 6: Verify and commit.** Re-run Python tests; run preparation against the real already-downloaded archive `/Users/chen/code/家庭陪伴机器人/.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz` into a fresh worktree-local `.superpowers/` destination. Compare prepared `main/application.cc` and the license to the verified archive. `git diff --check`; stage only task files; commit. Do not push (controller handles remote/CI).

- [ ] **Step 7: Controller review and Linux build.** Generate task review package; require both spec compliance and code quality approval. Push the feature branch without proxy environment overrides being written into Git config or scripts. Observe the first exact-head Actions run, inspect failures, resolve actual build issues under the same task and re-review changes. Save dependency lock and build/partition evidence from a successful artifact locally under ignored `artifacts/`; report stock build separately from unimplemented AEC and unperformed hardware tests.

## Self-review

This prerequisite is limited to stock build reproducibility. It does not implement the remaining design's software reference, AEC, realtime capture, optional interruption reason, firmware flashing or acoustic acceptance. Those remain active required work and must not be marked complete merely because CI is green. The source archive was independently fetched by commit and hashed; SDK and action commits were resolved from official GitHub repositories. No new user decision is needed to execute this prerequisite.
