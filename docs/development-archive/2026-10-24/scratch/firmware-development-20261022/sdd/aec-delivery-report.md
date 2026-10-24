# Task 2 delivery report

Status: DONE (software scope). Target CI, downloaded-artifact inspection, broad review and all hardware/acoustic work remain controller/field gates, not completed by this implementer.

Base: cb85517. Commit: `acec6d0192e13a8ea14f12536d9a506d0f56f804` — `feat(firmware): deliver bounded AEC collector and experiment CI`.

Main only; no branch/worktree/push. Final worktree is clean, main is ahead of origin/main by 6 including prior controller commits. No Task 1 overlay or patch file changed. No serial/device/reset/flash/live capture, gateway changes or deployment occurred. No generated firmware, audio, backup or credential files entered Git.

## Implemented

- `firmware/tools/capture_aec.py`: explicit stdlib-only bounded IPv4 UDP collector for authoritative `aec_diagnostic_wire.h` SWAC v1 `<4sBBBBIIQQIHHII` (48 bytes), datagrams <=1200. Validates magic/version/stream/channel/rate/flags/nonzero frames/exact frame payload/datagram lengths before writing. Optional source IPv4 and pinning to first valid sender endpoint prevent cross-device mixing within a session. No listener on import, default/background capture or device command sender.
- Duration >0..120s; maximum total saved file bytes 32MiB (smaller explicit budgets allowed down to 8192). Counts original packets plus uint32 framing, index, unchanged WAV PCM/headers, final summary; reserves 8192 for bounded summary. Two stream states/at most two WAV handles, no growing in-memory history/list. `os.scandir` lazily accounts final actual file sizes. Budget is logical file lengths, not filesystem allocation-block accounting.
- Newly created 0700 directory only; exclusive-created files explicitly 0600 even under a permissive umask. `packets.bin` stores original accepted datagrams prefixed by uint32 LE datagram length. `index.jsonl` stores every header field, source, packet offset, segment filename/reasons and reference validity. Per-stream WAV files preserve PCM exactly. No padding, rate conversion, equal-origin alignment, correlation score or acoustic-pass claim.
- Splits on reset, window/generation/validity changes, gaps/duplicate/out-of-order packets, position discontinuities and producer timestamp regression. A sequence high-watermark keeps an entire replayed older run isolated; the first following in-order packet also starts a new segment. Summary includes packet/frame/channel-sample counts, loss/disorder, first/last generation/change count and invalid-reference frames; full generation chronology remains in the bounded on-disk index.
- Timeout, explicit stop event or terminal Ctrl-C finalize WAV/index/packet/summary files. CLI SIGINT handler requests stop between packets rather than interrupting a write. Receiver exits do not control device sender expiry, explicitly documented.
- `.github/workflows/firmware-experiment.yml`: official `espressif/idf:v6.1`, exact pinned checkout/upload actions from baseline, 60min isolated matrix jobs for original m5stack-stopwatch and unique m5stack-stopwatch-software-aec from identical patched input files. Each runs all firmware host tests, fresh source prep, exact SDK/source SHA assertion, upstream host tests, canonical `python3 scripts/build.py m5stack/stopwatch --name "$VARIANT" --language zh-CN --zip`.
- Narrow helper `firmware/tools/verify_experiment.py` keeps CI readable and independently testable. Rejects native dependency lock byte drift against checked-in SHA pin, asserts ESP-SR2.4.7/esp_codec_dev1.6.2/IDF6.1.0, matches prep provenance against all current patch/overlay input hashes, checks effective config for both variants. Requires software/device AEC + USB PRIMARY for experiment, software/device AEC OFF + stock UART for original; both ISR_IRAM_SAFE and unconditional audio debugger OFF. Asserts rollback enable/board/target/partition selection, valid bounded configured offset. Exact byte-pinned YAML section scalars are checked with small stdlib regexes; it is deliberately not a new generic YAML parser, IDF validates native schema.
- Helper bundles only required build/config/source identity files and tracked source patch/overlay inputs, plus canonical release ZIP, app/merged/assets/bootloader/partition/flasher args and SHA256 for EVERY evidence file. Artifact `stopwatch-patched-<variant>-<exact github.sha>`, retention7d. No environment dump, OTA/flash/deploy jobs. Added narrowly scoped helper tests as justified extension to allowed CI helper.
- `docs/verification/2026-10-22-software-aec-field-test.md`: concrete, explicitly unexecuted Chinese artifact/CRC/hash/config/geometry checks; actual existing private backup identity and usable partition geometry; offline OTA sequence/state/CRC selection and clear backup/current-device distinction; protect NVS/otadata/phy; app-only vs inactive-slot tradeoff, bootloader/table compatibility holds, shared-assets equality/compatibility or paired restoration. No ready-to-execute serial writes; those wait for notified user-present window. Includes observation first, calibration sign/units/bounds + uncertainty, explicit realtime gate, approved two-minute self-echo and ten-round >=9 successful intent/<=1s actual-stop criteria, real same-recording speaker evidence, tools/wake/two-minute idle, recovery and separate USB-noise/power-state comparison. No gain/charge/PA changes.
- `firmware/README.md`: collector formats/limits/privacy, two-variant CI/evidence semantics, field/rollback gate link.

## TDD evidence (commands from repo root)

### Collector initial RED before implementation

`python3 -m unittest discover -s firmware/tests -p test_capture_aec.py -v`

Observed exit1; all8 tests failed with `AssertionError: False is not true : explicit bounded collector is not implemented`; `Ran 8 tests in 0.001s`, `FAILED (failures=8)`. Tests first asserted collector script existence before import so the missing implementation produced intentional assertion failures rather than import errors. Tests specify real loopback behavior, output bytes, segmentation, limits and private permissions.

Initial implementation run gave7 passing +1 fixture failure: macOS refused a 65507-byte synthetic UDP send with OSError40 `Message too long`. Changed the oversize fixture to1201 bytes (one byte beyond protocol limit) so it tests protocol oversize on actual loopback on both platforms, rather than the OS maximum UDP buffer. No collector acceptance bound relaxed.

### Replay-run regression RED/GREEN

Added `test_replayed_run_never_becomes_a_continuous_segment` before fix. Command:

`python3 -m unittest discover -s firmware/tests -p test_capture_aec.py -k replayed -v`

RED exit1: `AssertionError: 4 != 5`, `Ran 1 test in 0.126s`, `FAILED (failures=1)` — adjacent-only sequence comparison allowed two packets from an older replay run to join. Fixed using current-window high watermark. Then:

`python3 -m unittest discover -s firmware/tests -p test_capture_aec.py -v`

GREEN exit0: `Ran 9 tests in 0.821s`, `OK`.

### Terminal stop regression RED/GREEN

Added actual subprocess CLI + loopback fixture + SIGINT test and endpoint pinning test; changed terminal-stop assertion to require cooperative stop. Command:

`python3 -m unittest discover -s firmware/tests -p test_capture_aec.py -k cli -v`

RED exit1: `AssertionError: 'interrupt' != 'stop'`, `Ran 1 test in 0.121s`, `FAILED (failures=1)`. Added SIGINT event handler so terminal stop completes current packet, restores old handler on exit. Same command GREEN exit0: `Ran 1 test in 0.119s`, `OK`. The test checks actual WAV PCM, JSON and exact sum of saved file sizes after Ctrl-C.

### CI helper RED/GREEN

`python3 -m unittest discover -s firmware/tests -p test_experiment_evidence.py -v`

RED before helper existed: exit1, all4 `AssertionError: False is not true : experimental CI evidence verifier is missing`; `Ran 4 tests in 0.001s`, `FAILED (failures=4)`.

Implemented verifier/bundle; same command GREEN exit0: `Ran 4 tests in 0.024s`, `OK`. Real files in temporary fixture dirs verify both config variants, dependency-byte drift, wrong SDK, changed input provenance, unsafe/missing config/offset, exact copied evidence and every SHA. The synthetic build files are explicitly not ESP images and never offered as actual target evidence.

### Self-review integration fixes

Verified existing downloaded stock sdkconfig: original `USE_DEVICE_AEC` is absent when board Kconfig dependency is false. Added omitted-disabled fixture before fix. `python3 -m unittest discover -s firmware/tests -p test_experiment_evidence.py -k both -v` failed exit1 with `ValueError: effective configuration mismatch: CONFIG_USE_DEVICE_AEC` (`FAILED (errors=1)`). Normalize only this stock disabled symbol to n; explicit y still rejected.

Checked exact SDK console source: https://github.com/espressif/esp-idf/blob/fff9895c82d744c7237be8847347bdd1b07c6643/components/esp_stdio/Kconfig#L32-L52 . Secondary USB depends on !primary USB and can similarly be omitted in experimental sdkconfig. Added omission fixture first; same focused command failed exit1 `ValueError: effective configuration mismatch: CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG` (`FAILED (errors=1)`). Normalize only that experiment-disabled symbol; USB PRIMARY=y and secondary NONE=y remain mandatory. Field doc links authoritative source. No Task1 runtime changes necessary.

After fixes, focused helper suite GREEN4/4 in0.024s; collector GREEN11/11 in1.051s, output pristine. Replaced final Path.iterdir accounting with lazy os.scandir to avoid materializing a list with many small segments; same real file budget/CLI size assertions pass.

## Final validation

- `python3 -m unittest discover -s firmware/tests -v`: exit0, final complete owned suite `Ran 62 tests in 4.906s`, `OK`, pristine. 47 preexisting +11 collector +4 evidence tests. One earlier full suite was62/62 in6.425s before self-review identified the narrow omitted-secondary config issue; the final62/62 rerun is after those fixes. Focused runs were used during edits.
- Default `python3 firmware/tools/prepare_experiment.py --destination firmware/.cache/aec-delivery-source`: controlled exit1 `prepare-experiment: experimental preparation failed`. Direct diagnostic showed public archive download `URLError <urlopen error [Errno 61] Connection refused>` in this local environment. No tool/source policy changes made to work around it.
- Used verified existing offline archive: `python3 firmware/tools/prepare_experiment.py --archive .superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz --destination firmware/.cache/aec-delivery-source`: exit0, printed `/Users/chen/code/家庭陪伴机器人/firmware/.cache/aec-delivery-source`. Fresh real archive hash verification, patch application and overlay/lock prep succeeded. Generated cache remains ignored.
- From that fresh tree, `python3 scripts/build.py --list-boards`: exit0; `m5stack/stopwatch`, `  - m5stack-stopwatch`, `  - m5stack-stopwatch-software-aec` present. Upstream warns no active SDK and assumes6.0.2 for LISTING ONLY; this is not target build evidence. Also upstream lists unrelated lilygo ambiguous-config warning. CI exports/checks exact v6.1 commit before canonical build.
- Local direct fresh-tree assertions verified exactly2 StopWatch builds, original stock sdkconfig_append unchanged, experiment appends software/device AEC, debug/ISR-off and USB PRIMARY, and provenance dependency hash equals actual native lock. Output: `Fresh real source: 2 variants; original config unchanged; experimental settings and dependency provenance verified`.
- Ruby YAML parser loaded workflow and asserted official espressif/idf:v6.1 and exactly2 matrix variants: `Workflow YAML parsed; official v6.1 container and two matrix variants verified`. Ruby uses YAML1.1 and displays unquoted `on` key as true; GitHub uses its workflow parser. No actionlint installed locally; not claimed linted by actionlint.
- `git diff --cached --check` before commit: exit0, no output. `git diff --check` after commit: exit0, no output.
- `git diff cb85517 HEAD --name-only`: exactly the7 scoped files listed above. `git diff cb85517 HEAD -- firmware/overlays firmware/patches`: empty. `git status --short --branch`: only `## main...origin/main [ahead 6]`, clean.

## Remaining gates and limitations

Controller owns task review, one strongest-model broad review, push, exact-head actual target CI dispatch, artifact download digest/CRC/all hashes, effective config/binary/partition/assets/bootloader checks against private backup, and final plan bookkeeping. This implementer has no installed local target SDK and did not simulate target success. No real artifact from this commit has been downloaded or called valid. The helper's synthetic fixtures validate host evidence handling, not ESP image checksums.

No acoustic calibration/correlation analysis implemented because no real field capture was authorized/available; collector retains exact data and uncertainty, field doc gives bounded candidate offset procedure and requires separate real acoustic delay evidence. Offset adjustment would be a future reviewed config + target build/field step, never an automatic synthetic pass.

Current hardware OTA slot, bootloader/assets compatibility and recovery remain unverified until notified field preflight. Backup-selected ota_0 is only an offline fact. Existing bootloader rollback enable cannot guarantee recovery when the sole known-good slot is overwritten or shared resources change. Automatic two-slot rollback is not promised. No physical AEC, natural barge-in, real speaker stop latency or USB-noise PASS is claimed.

## Follow-up: actual CI Docker checkout ownership failure

Controller-provided actual run [39651416799](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39651416799) at acec6d0 failed both matrix variants before upstream tests/compiler. Closed custom job119400417589 log: `.superpowers/firmware-development-20261022/sdd/ci-experiment-acec6d0-failed.log`. Firmware62 host tests and fresh preparation passed; SDK checkout rev-parse succeeded. The repository SHA assertion failed with `fatal: detected dubious ownership in repository at '/__w/family-companion-robot/family-companion-robot'`. Docker ownership/trust handling, not a firmware compile/AEC result, caused this failure. The later helper assertion would have had the same issue.

Fix commit: `026415acb0877a4f3067da4eee73b59cdb34dad5` — `fix(ci): trust only checkout path for source SHA checks`. Exactly2 lines in2 files changed:

- Workflow checkout SHA assertion now uses `git -c safe.directory="$GITHUB_WORKSPACE" -C "$GITHUB_WORKSPACE" rev-parse HEAD`.
- Helper actual checkout assertion now passes `-c safe.directory=<FIRMWARE.parent absolute repo>` to that single Git subprocess before `-C <same repo>`.

No global/wildcard Git policy, persistent config, permissions, source-preparation Git environment isolation, SDK assertion or other runtime/target logic changed. Main only, no push. Final `git status --short --branch` is clean and ahead1 of controller-pushed origin/main.

Validation, limited to affected area as requested:

1. `python3 -m unittest discover -s firmware/tests -p test_experiment_evidence.py -v`: exit0; `Ran 4 tests in 0.026s`, `OK`, pristine. No whole-suite rerun for this2-line CI fix.
2. Actual Git behavior experiment using a new temporary empty Git repository with one synthetic commit. Set only subprocess environment `GIT_TEST_ASSUME_DIFFERENT_OWNER=1`; execute actual Git, no mocks/source-shape assertions, no chown or policy writes:

```python
# repo is TemporaryDirectory()/repo, initialized + committed using per-command
# user.name=Synthetic Fixture and user.email=fixture@example.invalid.
env = dict(os.environ, GIT_TEST_ASSUME_DIFFERENT_OWNER='1')
unsafe = subprocess.run(['git', '-C', str(repo), 'rev-parse', 'HEAD'],
                        env=env, capture_output=True, text=True)
assert unsafe.returncode == 128 and 'dubious ownership' in unsafe.stderr
trusted = subprocess.run(['git', '-c', f'safe.directory={repo}', '-C', str(repo),
                          'rev-parse', 'HEAD'], env=env, capture_output=True,
                         text=True, check=True)
wrong = subprocess.run(['git', '-c', f'safe.directory={repo.parent}/other',
                        '-C', str(repo), 'rev-parse', 'HEAD'],
                       env=env, capture_output=True, text=True)
assert wrong.returncode == 128 and 'dubious ownership' in wrong.stderr
assert len(trusted.stdout.strip()) == 40
```

Observed output: `Actual Git behavior: simulated different owner rejected128; exact per-command repo trust accepted0; unrelated trust still rejected128`. Also executed exact trusted Git argument vector on this actual workspace with simulated different owner and compared expected source SHA: `acec6d0192e13a8ea14f12536d9a506d0f56f804` matched. This proves narrow trust works while unrelated trust remains rejected.

3. Additional exact workflow shell assertion executed after commit:

```python
repo = str(Path.cwd())
sha = subprocess.check_output(['git', '-c', f'safe.directory={repo}', '-C', repo,
                               'rev-parse', 'HEAD'], text=True).strip()
env = dict(os.environ, GIT_TEST_ASSUME_DIFFERENT_OWNER='1',
           GITHUB_WORKSPACE=repo, GITHUB_SHA=sha)
command = 'test "$(git -c safe.directory="$GITHUB_WORKSPACE" -C "$GITHUB_WORKSPACE" rev-parse HEAD)" = "$GITHUB_SHA"'
subprocess.run(['/bin/bash', '-c', command], env=env, check=True)
```

Observed: `Exact workflow shell assertion via /bin/bash under simulated different owner: exit0, SHA=026415acb0877a4f3067da4eee73b59cdb34dad5`. A preceding optional shell probe using bare `bash` was not valid evidence: local Homebrew bash failed loading `_rl_completion_rewrite_hook`, and putting GIT_TEST env assignment before an untrusted expected-SHA command substitution also produced a local dubious-owner rejection. Corrected by obtaining SHA with trusted per-command Git first and using the working system `/bin/bash`; neither issue required changing production files or environment.

4. `git diff --check` and `git diff --cached --check`: exit0, empty. Self-review confirms exact explicit absolute checkout path is trusted only for each source-SHA Git command; no policy bypass elsewhere. No generated test files committed. Actual target CI retry/artifact checks remain controller-owned and pending; no hardware/production actions or acoustic claims.

## Follow-up: preserve existing notices in detached evidence artifact

Controller identified that the canonical upstream `scripts/build.py` ZIP includes only `merged-binary.bin`; the existing outer evidence bundle carried binaries and patches but not the already-preserved source license/notices. Scope here is simply carrying those accepted existing files in downloadable evidence, with no additional license-policy claim.

Commit `530a395340c276dc15d7f7c943825d417aaf807b` — `fix(firmware): retain existing notices in evidence bundle` — touches ONLY `firmware/tools/verify_experiment.py`, `firmware/tests/test_experiment_evidence.py`, `firmware/README.md` (17 insertions,1 deletion). `bundle()` byte-copies existing `firmware/third_party/xiaozhi-LICENSE` to outer `third_party/xiaozhi-LICENSE`, and existing root `THIRD_PARTY_NOTICES.txt` to outer `THIRD_PARTY_NOTICES.txt`, before generating the inventory. Both therefore appear in `SHA256SUMS`. Runtime, prepared source, canonical upstream ZIP, workflow and firmware logic unchanged. README adds one sentence describing included notices.

Meaningful existing bundle test was extended first with clearly synthetic fixture notice files, exact output-byte assertions, explicit inclusion of both notice paths in hash inventory and the existing every-file digest verification. RED command:

`python3 -m unittest discover -s firmware/tests -p test_experiment_evidence.py -k bundle -v`

Observed exit1: `AssertionError: False is not true` for `(output/'third_party/xiaozhi-LICENSE').is_file()`; `Ran 1 test in 0.013s`, `FAILED (failures=1)`. No source-text/mirror assertions.

After the3-line production packaging addition, GREEN command:

`python3 -m unittest discover -s firmware/tests -p test_experiment_evidence.py -v`

Observed exit0: all4 tests pass, `Ran 4 tests in 0.025s`, `OK`, pristine. No whole-suite rerun for this packaging-only follow-up.

`git diff --check` and `git diff --cached --check`: exit0, empty. Self-review confirmed both files are copied unchanged, paths do not collide with prepared outputs and inventory enumeration occurs after copying them. Explicit staging included only the3 owned files. Parent's4 unstaged docs remain untouched/uncommitted; final status shows those same4 docs and main ahead2. No push, hardware/server/network operation or acoustic claim. Actual artifact production and inspection remain controller-owned.

## Follow-up: native SDK identical alias repetition

Controller supplied closed actual CI logs at530a395: `.superpowers/firmware-development-20261022/sdd/ci-experiment-530a395-original-failed.log` (job119410977187) and `ci-experiment-530a395-custom-failed.log` (job119410976851). Both targets COMPILED SUCCESS, produced merged/canonical ZIP, then verifier failed with `ValueError: duplicate SDK config key`. Actual target compilation is now controller-observed successful for both variants at530a395; published validated artifacts still require next exact-head CI after this parser fix. This is separate from acoustic acceptance.

Cause: native IDF sdkconfig retains repeated deprecated Wi-Fi/WPA aliases with IDENTICAL values. The saved independently verified stock config contains25 repeated identical assignments; the overly strict duplicate-key guard rejected valid native SDK output.

Fix commit `15fbc7367efbe635479a828d3026cbbeadaeadc9` — `fix(firmware): accept identical native SDK config aliases`. Only `firmware/tools/verify_experiment.py` and `firmware/tests/test_experiment_evidence.py` changed (20 insertions,2 deletions). Parser accepts a repeated key only when its normalized effective value equals the existing value; conflicting repeats still raise `ValueError: conflicting SDK config key: <key>`. Dependency byte pin, provenance equality, critical component and positive configuration checks unchanged. No runtime/target source changes or generic parser framework added.

TDD:

- Added meaningful fixture behavior for native-style numeric aliases repeated identically, repeated critical AEC=y, repeated ISR disabled, and conflicting critical AEC y/n repetition.
- RED: `python3 -m unittest discover -s firmware/tests -p test_experiment_evidence.py -k repeats -v` exit1. Identical repeat fixture errored `ValueError: duplicate SDK config key`; conflicting case failed expected specific conflict exception assertion. `Ran 2 tests in 0.012s`, `FAILED (failures=1, errors=1)`.
- GREEN after narrow guard change: `python3 -m unittest discover -s firmware/tests -p test_experiment_evidence.py -v` exit0. All6 tests pass, `Ran 6 tests in 0.032s`, `OK`, pristine. No whole-suite rerun for validator-only fix.

Actual native configuration behavior probe (Python executed locally, no SDK install/network/device): read existing `artifacts/firmware-baseline-20261022-8953c44/evidence/.cache/stock/sdkconfig` bytes into a temporary fixture; append ONLY `# CONFIG_STOPWATCH_SOFTWARE_AEC is not set` because that new owned symbol did not exist in unpatched baseline. Copy its actual native `dependencies.lock`; create explicitly synthetic current-input provenance wrapper required by production verify(). Call real `verify(firmware, temporary_tree, 'm5stack-stopwatch', locked_idf_commit)` with all required checks active, assert software/device AEC off + UART primary and inspect returned effective config. Original baseline artifact bytes were not modified. Production verifier checked real native lock bytes against the unchanged checked-in SHA pin and all other actual native baseline required values matched.

Observed exit0 output:

```text
Saved native baseline repeated identical assignments: 25
Production verifier accepted actual saved native baseline config + explicit new experiment flag OFF; required original config values all matched
Native dependency bytes checked against existing pin; provenance wrapper is synthetic current-input fixture, not actual target-artifact evidence
```

This probe detects further baseline effective-config omissions without claiming the synthetic wrapper is prepared target evidence. No original critical config omission beyond known absent device-AEC was found.

`git diff --check` and `git diff --cached --check` exit0, no output. Self-review confirms first-value/last-value conflicts cannot silently win, identical aliases can pass, and non-config lines remain ignored. Explicit staging/commit includes only2 owned files. Main ahead1; parent's4 unstaged docs preserved untouched, no push. No hardware/server operations or physical AEC claim.
