# Atomic experimental source preparation report

Status: DONE

Commit: `d54991c Add atomic experimental firmware source preparation`
Branch: `main`; no feature branch, additional worktree, push, device access, serial access, server access, capture, SDK execution or deployment.

## Implemented

- Added `firmware/tools/prepare_experiment.py` with the specified `prepare_experiment(archive, destination, lock, patches, overlays, dependencies)` interface and CLI.
- Reused `prepare_source`, `download_archive`, and destination validation from `prepare.py`; archive extraction, digest validation and download bounds remain centralized.
- Prepared source only within an owned temporary directory under the destination parent, applied regular lexical `*.patch` files with actual bounded `git apply --check` then `git apply`, copied regular noncolliding overlays, and published only the completed tree.
- Removed inherited `GIT_*` variables and set `GIT_CEILING_DIRECTORIES` to the resolved temporary parent for both patch subprocesses.
- Refused symbolic patch/overlay inputs, existing destinations including dangling links, overlay collisions, dependency symlinks, dependency records over 1 MiB, and mismatching adjacent SHA256 pins. Overlay copies also refuse paths routed outside the generated tree by a patch-created link.
- Added ordered relative input hashes and upstream commit/archive hash in `.family-robot-source.json`. CLI failures emit a fixed controlled message without subprocess output or private paths; successful CLI output is only the resolved prepared path.
- Copied only the native SDK dependency lock from the approved stock artifact byte for byte and added its adjacent SHA256. No runtime configuration, binaries or credentials were copied.
- Documented command defaults, atomic fresh destination semantics, lexical patch order, collision behavior, provenance and the absence of AEC integration.

## Requirements correction

The original controller brief incorrectly assumed the SDK dependency lock was JSON. Inspection found native YAML (`dependencies:` at the first line). The controller explicitly corrected the brief and plan at `bd9f826`: preserve native bytes and verify a bounded regular file against an adjacent SHA256, with native schema validation deferred to the later IDF build. No YAML parser dependency was introduced. Known stock version fields were inspected directly: `espressif/esp_codec_dev` is `1.6.2`; `espressif/esp-sr` is `2.4.7`.

## TDD evidence

RED, before creating the implementation:

```sh
python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v
```

Expected result: `Ran 18 tests in 0.002s`, `FAILED (errors=18)`, each caused by the missing `firmware/tools/prepare_experiment.py` module. Full captured output: `experiment-prep-red.log` in this report directory. This missing-module RED was explicitly required by the task brief.

GREEN, after implementation:

```sh
python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v
```

Result: `Ran 18 tests in 0.408s`, `OK`. Actual `git apply` was used, without subprocess mocks. The only mocking is temporary environment setup through `patch.dict` for inherited `GIT_*` fixture values.

Fixture coverage:

1. Real gzip archive, actual unified patch, overlay, exact native YAML bytes and all provenance hashes.
2. Unrelated working directory inside another Git checkout, with inherited `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE` and `GIT_CONFIG_*`; parent tracked sentinel and index bytes unchanged.
3. Failed patch context; no output published and only invocation-owned temporary files removed.
4. Bad archive checksum with atomic failure.
5. Overlay collision with upstream with atomic failure.
6. Existing destination sentinel contents preserved.
7. Dangling destination symlink preserved.
8. Overlay file symlink rejected.
9. Overlay directory symlink rejected.
10. Patch symlink rejected.
11. Wrong dependency checksum rejected atomically.
12. Dependency symlink rejected.
13. Dependency size over 1 MiB rejected.
14. Two patches modifying the same file applied in lexical order.
15. Missing and empty patch directories accepted.
16. Traversal patch rejected by real Git; external sentinel unchanged.
17. CLI default inputs resolved beside script from unrelated cwd, no network required.
18. CLI patch failure controlled; no private paths/context emitted.

For failure cleanup, fixtures retain an unrelated temporary-looking sibling and verify its sentinel remains intact.

## Full verification

```sh
python3 -m unittest discover -s firmware/tests -v
```

Result: `Ran 38 tests in 2.643s`, `OK`; output pristine. Full captured output: `experiment-prep-full-tests.log` in this report directory. This includes existing archive preparation and host C++ playback reference tests.

`git diff --check` and staged `git diff --cached --check`: passed without output. Commit included only the five scoped files. Working tree clean immediately after commit.

## Real locked source verification

Command:

```sh
python3 firmware/tools/prepare_experiment.py --archive .superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination firmware/.cache/experiment-prep-20261022
```

Result: success, output only the resolved prepared path. Generated tree remains ignored.

Independent byte/provenance assertions passed:

- Locked archive SHA256: `b3460397122eff7edeb8c07bd9c26e2c216b24cc2e5c2d836ec2209ba5be0c63`.
- `main/boards/m5stack/stopwatch/playback_reference_buffer.h` exactly matches tracked overlay bytes; SHA256 `21d965c4688f556aa135298eb3b934b4450e25dccb2c1ebf31849099d6057a2d`.
- Untouched `main/CMakeLists.txt` exactly matches its original archive bytes; SHA256 `1980e2da8ae4ffabbdd018dd5d6086ff076e5bc78988e06f88b089ff0a4209dc`.
- Generated dependency bytes exactly match both the approved stock artifact and tracked lock; SHA256 `f0568ddc1585ad4bf0f272cc16b75f38d7befab1cebaad2f38aa33d47d8e665b`.
- Full provenance mapping equals expected upstream commit/archive digest, empty initial patch list, exact new overlay path/hash and dependency digest.

Captured assertions: `experiment-prep-real-source.log` in this report directory.

## Files changed

- `firmware/tools/prepare_experiment.py`
- `firmware/tests/test_prepare_experiment.py`
- `firmware/dependencies.lock`
- `firmware/dependencies.lock.sha256`
- `firmware/README.md`

## Self-review and concerns

Self-review found no outstanding implementation or scope issues. Preparation is a source prerequisite only; no target SDK build was attempted because none is installed locally. Native SDK schema acceptance, firmware integration, timing, acoustic behavior and natural barge-in remain unvalidated. No AEC source integration was added. No push was performed.


## Review amendment: generated output overlay reservation

Review finding: a root `overlays/dependencies.lock` was accepted and hashed as an overlay before the pinned dependency record overwrote its bytes. This made that overlay entry disagree with published provenance. The real fixture confirmed the defect before implementation.

Fix: explicitly reject root `dependencies.lock` and `.family-robot-source.json` overlays with `FileExistsError` before copying either file. The prior final provenance collision guard remains for upstream/patch output. Added two real behavior cases and documented both reserved filenames. Each regression uses the existing atomic-failure assertion, checking destination absence, owned temporary tree cleanup and unchanged unrelated sibling sentinel bytes.

RED command:

```sh
python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v
```

Exit 1. `Ran 20 tests in 0.446s`, `FAILED (failures=1)`. Expected failing case: `test_dependency_output_overlay_is_rejected_atomically`, `AssertionError: FileExistsError not raised`. The existing provenance-path rejection case passed before the fix. Captured output: `experiment-prep-reserved-red.log`.

GREEN command, after the explicit root reservations:

```sh
python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v
```

Exit 0. `Ran 20 tests in 0.479s`, `OK`; output pristine. Captured output: `experiment-prep-reserved-green.log`. No unrelated/full suite was rerun, as the controller requested focused verification for this amendment. `git diff --check` and `git diff --cached --check` passed without output.

Commit: `04f0b94 Reject overlays claiming generated firmware metadata` on `main`. Only `firmware/tools/prepare_experiment.py`, `firmware/tests/test_prepare_experiment.py` and `firmware/README.md` changed. Working tree clean immediately after commit. No push, SDK/device/serial/server/capture operations or user notifications. Self-review: the accepted-overlay provenance defect is resolved; no outstanding concern from this review. Earlier source-only validation limits remain unchanged.


## Re-review amendment: reject patch-created symlink parents

The re-review demonstrated that the prior lexical reservation was incomplete: a real Git patch creating `alias -> .` lets `overlays/alias/dependencies.lock` name the reserved root output indirectly. The earlier amendment's conclusion is superseded for this alias case.

Smallest complete rule selected for this finding: reject every overlay path with any symlink parent inside the prepared tree. The safe archive extractor admits no symlinks, so these links can only have been introduced by tracked patches. The existing reserved lexical names and resolved-tree containment guard remain. README now states that overlay traversal through patch-created links is refused.

Added a real mode-120000 Git patch fixture (no subprocess mocks) for `alias -> .`. Separate behavior tests cover aliased `dependencies.lock`, aliased `.family-robot-source.json`, and an ordinary `ordinary.h` through that same alias. Every case uses the shared atomic assertion to verify no destination is published, the invocation-owned temporary tree is removed and the unrelated sibling sentinel remains byte-identical.

RED command before the implementation change:

```sh
python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v
```

Exit 1: `Ran 23 tests in 0.607s`, `FAILED (failures=2)`. Expected failures: dependency alias and ordinary-file alias incorrectly succeeded (`ValueError` / `FileExistsError` not raised). Aliased provenance output was already rejected at the existing later guard. Captured output: `experiment-prep-alias-red.log`.

GREEN command after the parent-symlink rule:

```sh
python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v
```

Exit 0: `Ran 23 tests in 0.610s`, `OK`; output pristine. Captured output: `experiment-prep-alias-green.log`. Only the focused suite was rerun as requested; unrelated/full suites and SDK operations were not run. `git diff --check` and `git diff --cached --check` passed.

Commit: `ad5d513 Reject overlay paths through patch-created symlinks` on `main`. Scoped files: `firmware/tools/prepare_experiment.py`, `firmware/tests/test_prepare_experiment.py`, `firmware/README.md`. Working tree clean immediately after commit; no push/device/serial/server/capture access or user notification. Self-review checked each lexical parent component, so symlinks to inside or outside the generated tree are both rejected for overlay traversal. No outstanding concern from the named alias finding; source-only validation boundaries remain.
