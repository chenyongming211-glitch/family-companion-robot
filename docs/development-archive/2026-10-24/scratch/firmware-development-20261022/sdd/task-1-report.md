# Task 1 — Locked stock StopWatch build foundation

Status: implementation committed locally; controller review, push and first exact-head Linux Actions build remain pending. No remote, device, serial or server changes were performed.

Base: `a3d08ad391ee63fee14ab79837f43d5aad405a85`
Commit: `0f9456f` — `build(firmware): pin and safely prepare stock StopWatch baseline`
Branch: `codex/natural-barge-in`

## Changed files

- `firmware/upstream.lock.json`: exact supplied source, archive, SDK, ESP-SR and board values.
- `firmware/tools/prepare.py`: stdlib streaming checksum, 64 MiB download/archive/expanded-file limits, validation of every entry before extraction, root stripping, regular-file/directory-only handling, Python data extraction filter, temporary sibling cleanup, existing-destination refusal, bounded verified downloads, script-relative lock and controlled CLI errors.
- `firmware/tests/test_prepare.py`: 18 behavioral tests with real tar archives, file contents, sentinels, real file-URL downloads and an isolated subprocess CLI fixture.
- `.github/workflows/firmware-baseline.yml`: official pinned ESP-IDF container and actions, exact SDK commit assertion, canonical upstream tests/build, SHA256 output and enumerated evidence artifacts.
- `firmware/third_party/xiaozhi-LICENSE`: complete MIT license copied from verified archive.
- `firmware/README.md`: pinned provenance, exact prepare/build commands, ignored output locations, private Actions artifact retrieval, future tracked patch/overlay policy and build-versus-AEC/flashing boundary.
- `THIRD_PARTY_NOTICES.txt`: upstream firmware attribution without relicensing workspace code.

## TDD and verification evidence

1. Before production implementation, wrote 13 API behavior tests.
   Command: `python3 -m unittest discover -s firmware/tests -v`.
   Expected RED: all 13 failed at dynamic import with `FileNotFoundError` for missing `firmware/tools/prepare.py`. This is the explicit missing-module failure required by the brief. Log: `.superpowers/sdd/task-1-red.log`.
2. Implemented preparation API and helpers; the original 13 tests passed.
3. Added five characterization/integration checks for corrupt gzip, verified download, rejected download checksum, download limit, and CLI lock-path/stdout/repeated invocation. These checks exercise the existing implementation; they are supplemental coverage, not additional claimed red-green cycles. CLI test initially exposed macOS `/var` versus `/private/var` canonical path expectations; resolved the expected path in the test because production deliberately resolves its script location.
4. Final full owned Python suite: `python3 -m unittest discover -s firmware/tests -v` — **18 tests, OK**, 0.351 seconds. Log: `.superpowers/sdd/task-1-green.log`.
5. Additional unmodified upstream host suite: `PATH="$PWD/.superpowers/test-bin:$PATH" python3 -m unittest discover -s .superpowers/task-1-prepared/scripts/tests -v` — **110 tests, OK**, 8.233 seconds. Log: `.superpowers/sdd/task-1-upstream-tests-system-bash.log`.
   The first upstream suite run had six errors because installed `/opt/homebrew/bin/bash` fails to load its readline dylib (`_rl_completion_rewrite_hook` missing). For the successful rerun only, ignored `.superpowers/test-bin/bash` links to `/bin/bash`, and `.superpowers/test-bin/python` links to `/opt/homebrew/bin/python3` so upstream workflow shell subprocesses can call `python`. No global tools, configuration, or upstream source were changed. This is a local environment limitation, not a firmware failure.
6. `git diff --check` and `git diff --cached --check` passed before commit. Staging contained exactly the seven task files. No Go files changed; the controller already ran baseline Go tests.

## Real archive verification

Executed:

```sh
python3 firmware/tools/prepare.py --archive /Users/chen/code/家庭陪伴机器人/.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination .superpowers/task-1-prepared
```

Preparation accepted SHA256 `b3460397122eff7edeb8c07bd9c26e2c216b24cc2e5c2d836ec2209ba5be0c63` and configured root `xiaozhi-esp32-49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`. It printed only `.superpowers/task-1-prepared`.

Read upstream `AGENTS.md` before upstream inspection. Using Python `tarfile.extractfile`, compared prepared `main/application.cc` and `LICENSE` byte-for-byte with their entries in the verified archive: both identical. The tracked license was copied directly from the same archive entry. No upstream source modifications were made.

## Workflow mechanics correction

At controller request, `include-hidden-files: true` is set because the explicit build outputs are inside `.cache`, while upload-artifact v7 otherwise excludes hidden files. A preceding shell step checks each of the ten explicitly enumerated artifact paths with `test -s`; an existing source lock alone cannot disguise missing firmware/partition evidence. Upload uses only those explicit paths, no broad globs or private artifacts. The source/SDK checks and required artifacts from the brief are preserved.

## Self-review and remaining work

- Verified scope is stock source/build reproducibility only. No AEC, reference signal, realtime capture, optional interruption reason, acoustic acceptance or firmware flashing is implemented by this commit.
- Safe extraction validates every archive entry before creating the temporary tree; failures remove temporary output and existing destinations (including dangling symlinks) are refused. File count, archive paths, type and file sizes are bounded by the locked trusted archive plus requested checks. Preparation never executes downloaded code.
- Archive and expanded size checks, path/root/link/device rejection, unchanged existing sentinels, CLI behavior and download cleanup have behavioral coverage.
- All lock values match the supplied brief. No source dependency substitutions or hardware configuration changes were introduced.
- No `.superpowers`, caches, generated binaries, secrets or private artifacts are tracked by this commit. Existing Go code is untouched.
- Authoritative Linux ESP-IDF build and private artifact availability have **not** been established locally. Controller must review this commit, push, observe exact-head Actions, resolve any real incompatibility, and retain successful dependency/config/partition evidence under ignored artifacts. Do not treat passing host tests as a stock firmware build result or hardware PASS.
- No other implementation concerns identified in this task's self-review. Concurrent competing writes to a destination are outside this local preparation CLI's usage; it checks absence both before validation and immediately before the final move.

## Review follow-up — controlled late gzip corruption

Fix commit: `69b44f9` — `fix(firmware): normalize late gzip decoder failures`.

The review's Minor finding was reproduced before changing production code. Added a real gzip fixture containing a valid `source/file` tar header, 64 KiB file payload, `Z_SYNC_FLUSH` and an invalid later deflate block (`0xff`). The matching checksum allows entry iteration to reach the late corruption rather than stopping at hash validation.

RED command: `python3 -m unittest discover -s firmware/tests -k late_deflate -v`.
Expected and observed: one error, escaping `zlib.error: Error -3 while decompressing data: invalid block type` during `for member in source`. Log: `.superpowers/sdd/task-1-late-corruption-red.log`.

Minimal correction: import `zlib` and catch `zlib.error` alongside `tarfile.TarError` and `EOFError`, preserving the existing `ValueError('invalid source archive')` boundary. The fixture asserts `ValueError` and verifies that the only remaining temporary-directory entry is the input archive, with no destination or extraction residue.

GREEN command: `python3 -m unittest discover -s firmware/tests -v` — **19 tests, OK**, 0.345 seconds. Log: `.superpowers/sdd/task-1-late-corruption-green.log`. `git diff --check` and staged diff check passed; only the preparation module and its test changed. Worktree clean after commit. No broad upstream/Go reruns, push, remote change, or hardware activity was performed for this narrow correction. Exact-head Linux stock firmware build remains pending controller execution.

## First exact-head Linux run — corrected board directory

CI evidence: run `39630448069`, tested head `69b44f9`; complete supplied failed-step log preserved at `.superpowers/sdd/ci-first-failed.log`. The Linux run passed the exact SDK commit assertion, 19 preparation tests and 110 upstream host tests, then stopped before CMake/build with `[ERROR] board_type m5stack-stopwatch not found in main/CMakeLists.txt`. The copied upstream board README command was stale: current canonical board arguments use directory `m5stack/stopwatch`, while the variant/name remains `m5stack-stopwatch` and target remains `esp32s3`.

Correction commit: `8437b16` — `fix(firmware): select current StopWatch board directory`.

Changed only configuration/documentation: workflow build command, `firmware/upstream.lock.json` board field, `firmware/README.md`, and the tracked build-foundation plan's lock/build-command examples. Updated the ignored task brief's matching examples for consistency. All source, SDK, action, checksum and variant values remain unchanged. No firmware/upstream source modifications.

Focused validation imported the actual unmodified prepared upstream `scripts/build.py` and called `_collect_variants(idf_version=(6, 1, 0))`. Read the workspace lock using an absolute path before import because upstream initializes its own process working directory. Asserted the previous board argument selects zero variants and corrected lock board + variant + target selects exactly one actual upstream variant. Result:

```json
{"board":"m5stack/stopwatch","name":"m5stack-stopwatch","target":"esp32s3","config":"CONFIG_BOARD_TYPE_M5STACK_STOPWATCH"}
```

Full selected variant evidence is saved in `.superpowers/sdd/task-1-board-selection.log`. The collector emits an unrelated existing ambiguity warning for LilyGo t-cameraplus; StopWatch selection is unique. No YAML source-text tests were added. `git diff --check` and staged diff check passed. No unrelated Python/Go/upstream suites were rerun for this configuration/docs correction. Controller owns the retry at the new exact head; no successful firmware build or artifact is claimed yet. No push, remote, device, serial, server, or global-tool changes were performed.

## Final review follow-up — fail-fast local SDK assertion

Commit: `0734591` — `docs(firmware): stop local builds on SDK mismatch` (primary checkout, `main`).

Changed only `firmware/README.md`: wrapped the complete local stock-build Bash snippet in a subshell with `set -e`. A mismatched SDK now stops the snippet before SDK version inspection, upstream tests, or firmware build. Subshell scoping keeps the caller's current directory and error-handling options intact and does not exit the caller's interactive shell. CI was unchanged.

Focused verification extracted the actual README Bash fence and checked `/bin/bash -n`: PASS. Executed the same snippet in a controlled shell with a temporary empty SDK export script, stub `git` returning `incorrect-sdk`, stub Python lock lookup returning `expected-sdk`, a wrapper delegating `test` to the Bash builtin, and sentinel-writing test/build/version/checksum stubs. Observed subshell status `1`; the caller continued, its current directory and errexit setting remained unchanged, and no test/build sentinel existed. Output: `SDK mismatch status=1; caller alive; cwd/errexit unchanged; no test/build sentinel`.

`git diff --check` and staged diff check passed. No SDK install, network, actual build, broad test rerun, remote operation, branch/worktree creation, or push occurred. No additional concerns identified for this documentation correction.

## Artifact quota diagnostic — manual public-context storage probe

Commit: `8953c44` — `ci: add manual artifact storage probe [skip ci]` on `main`.

Controller evidence: stock run `39634154317` at `32df47e` compiled successfully and passed every required artifact-file check, then artifact upload failed on storage quota again. The controller verified repository API `isPrivate=false` after the user made the repository public. Deletion of other projects' artifacts is not authorized and was not attempted.

Added `.github/workflows/artifact-storage-check.yml`, triggered only by `workflow_dispatch`, with read-only contents permission, one Ubuntu job and a five-minute timeout. It creates one tiny text file containing only GitHub SHA/run ID and uses the existing pinned upload-artifact v7 commit `cf430e030ddbb5b0abf93d22962f4752f3646cd9` to upload `storage-probe-${{ github.run_id }}`, with one-day retention and missing-file failure. It performs no checkout or firmware build and contains no credentials or custom network scripts. Included the controller's existing one-word README change from “private repository” to “repository” unchanged. The commit message includes `[skip ci]` to avoid another stock build from that documentation change.

Focused verification: parsed workflow YAML with system Ruby YAML (PASS); extracted the actual shell block and checked `/bin/bash -n` (PASS); ran that shell block with controlled SHA/run-ID values and a temporary runner directory, confirming exactly one file with exactly those two metadata lines (PASS). `git diff --check` and staged diff check passed. No YAML mirror tests or broad suites were added/run.

Runtime upload probe remains UNPERFORMED and storage-quota recovery is not established. Controller owns push and manual dispatch. No push, dispatch, artifact deletions, settings changes, branches/worktrees, server or hardware actions were performed by this correction.
