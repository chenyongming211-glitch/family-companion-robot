### Task 1: Atomic experimental preparation with tracked patches and overlays

**Files:**
- Create: firmware/tools/prepare_experiment.py
- Create: firmware/tests/test_prepare_experiment.py
- Create: firmware/dependencies.lock and firmware/dependencies.lock.sha256 (exact verified stock SDK lock and checksum)
- Modify: firmware/README.md

**Interfaces:**

```python
def prepare_experiment(archive: Path, destination: Path, lock: dict,
                       patches: Path, overlays: Path, dependencies: Path) -> Path:
    """Publish a fresh experimental tree only after all controlled inputs apply."""
```

CLI: `python3 firmware/tools/prepare_experiment.py [--archive PATH] [--destination PATH]`; defaults: same upstream archive cache as prepare.py; destination firmware/.cache/experiment; patch directory firmware/patches; overlay directory firmware/overlays; dependency lock firmware/dependencies.lock. Inputs resolve adjacent to script, independent of cwd. Import existing prepare helpers rather than duplicating extraction/download validation.

- [ ] **Step 1: Write real fixture behavior tests before implementation.**

The central success fixture creates a real gzip tar containing `source/main/value.txt` = `old\n`, a real unified patch changing it to `new\n`, a new overlay `main/extra.h`, and a real YAML-format SDK dependency lock with adjacent SHA256. Verify returned path, changed bytes, overlay bytes, copied dependency lock, and provenance hashes. Run from an unrelated cwd and inside an unrelated parent Git repository; the parent tracked sentinel/index must remain unchanged. Use actual git apply, not mocks or source-text tests.

```python
result = module.prepare_experiment(archive, destination, lock, patches, overlays, dependencies)
self.assertEqual(result, destination)
self.assertEqual((destination / 'main/value.txt').read_bytes(), b'new\n')
self.assertEqual((destination / 'main/extra.h').read_bytes(), b'fixture overlay\n')
self.assertEqual((destination / 'dependencies.lock').read_bytes(), dependencies.read_bytes())
```

Also cover failed patch context, checksum failure, overlay collision with upstream, existing destination sentinel, dangling destination symlink, overlay symlink, wrong dependency checksum, and two ordered patches on the same file. Every failed preparation must leave destination absent or pre-existing contents intact and remove only its own temporary tree. An absent/empty patch directory is valid during initial setup. No network/SDK required.

Run `python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v`; record expected missing-module/function RED.

- [ ] **Step 2: Implement the smallest atomic adapter.**

Use a new temporary directory under destination.parent, safe-extract to its `source` child using prepare_source(), then apply lexically ordered regular `*.patch` files. For each patch run check and apply with explicit paths and bounded timeout:

```python
subprocess.run(['git', 'apply', '--check', '--whitespace=error-all', str(patch)],
               cwd=tree, env=git_environment, check=True, capture_output=True, timeout=30)
subprocess.run(['git', 'apply', '--whitespace=error-all', str(patch)],
               cwd=tree, env=git_environment, check=True, capture_output=True, timeout=30)
```

`git_environment` removes inherited GIT_* overrides and sets GIT_CEILING_DIRECTORIES to the resolved tree.parent so git never discovers/mutates this repository or a parent checkout. Reject symbolic patch/overlay inputs. Copy only regular overlay files, refuse an existing destination path (overlays add files; patches modify existing upstream files), preserve relative layout. Preserve the SDK YAML dependency lock byte for byte. Verify it against the adjacent dependencies.lock.sha256 before copying, with a bounded size and regular-file requirement; IDF validates its native schema at build time. Do not add a YAML parser just for preparation. Inspect the known component version fields before checking in the verified lock; no runtime config or private credentials are copied.

Write `.family-robot-source.json` with upstream commit/archive SHA256, ordered relative patch paths and SHA256, overlay relative paths and SHA256, dependency-lock SHA256. No environment values or credentials. Before renaming tree to destination, refuse existing destination again. Catch OS/JSON/patch failures into a controlled CLI error without dumping subprocess output or private paths; success prints only resolved prepared path.

- [ ] **Step 3: Pin dependencies and exercise the real locked source once.**

Copy `evidence/.cache/stock/dependencies.lock` byte for byte and write its adjacent SHA256; this is native SDK YAML, not JSON. Copy only this dependency record from artifacts/firmware-baseline-20261022-8953c44 into firmware/dependencies.lock. Verify esp_codec_dev 1.6.2 and ESP-SR 2.4.7 in this lock. Prepare real source into a fresh ignored destination with the existing verified archive; verify its new reference header bytes, upstream untouched-file bytes, dependency lock bytes and provenance. This does not execute the target SDK or use the watch.

```sh
python3 -m unittest discover -s firmware/tests -p test_prepare_experiment.py -v
python3 -m unittest discover -s firmware/tests -v
git diff --check
```

- [ ] **Step 4: Document and commit.**

Document the new command, atomic fresh destination semantics, patch order, overlay collision behavior and provenance. Explain that preparing experimental source alone does not integrate AEC. Commit only scoped source/tests/dependency lock/docs on main, no push. Report RED/GREEN, actual fixture scenarios, real-source verification, commit and concerns in assigned ignored report.
