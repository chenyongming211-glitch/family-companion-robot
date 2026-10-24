# Task 1 review — locked stock StopWatch build foundation

Reviewed range: `a3d08ad391ee63fee14ab79837f43d5aad405a85..69b44f9`.

Initial review covered `0f9456f`; focused re-review covered correction `69b44f9` using the fresh complete `.superpowers/sdd/task-1-rereview.diff` and appended implementer evidence.

## Verdicts

- **Spec compliance: APPROVED for the controller review/push/Linux-build stage.** The committed implementation covers the required build foundation and preserves the supplied source/SDK/action values. The task as a whole remains pending the exact-head Linux build and successful artifact inspection required by step 7.
- **Code quality: APPROVED.** No open Critical, Important, or Minor findings. The original Minor finding is resolved by `69b44f9`.

## Findings

### Resolved Minor — normalize decoder failures that occur after the initial tar header

**Original location:** `firmware/tools/prepare.py:79` (also the original CLI boundary at line 116).

**Resolution reviewed:** `firmware/tools/prepare.py:80` now catches `zlib.error` with the existing archive exception types and raises the existing controlled `ValueError`. `firmware/tests/test_prepare.py:183` adds a real late-deflate-corruption fixture with a matching archive checksum and asserts both the API error type and absence of destination/temporary residue. The appended implementer report records this fixture failing on the old implementation and the full 19-test suite passing after the fix. The correction is narrow, preserves cleanup and normal success behavior, and introduces no concrete new risk. No test suite was rerun during focused re-review. The description below records the original finding, now closed.

The archive-reading exception handler catches `tarfile.TarError` and `EOFError`, but a malformed deflate block encountered during later member iteration can raise `zlib.error` directly. The test for an invalid initial block is insufficient to cover this: `tarfile.open` wraps the initial-header failure, whereas later gzip reads can expose the decoder exception. A focused reproduction with a valid `source/file` tar header, 64 KiB of file data, a valid deflate sync flush, and then an invalid block produced `zlib.error: Error -3 while decompressing data: invalid block type` from `prepare_source`. No destination or `.prepare-*` directory was left behind.

When the checksum intentionally matches such an input, this escapes the intended controlled archive-error handling, and would escape `main` as well. Consider catching `zlib.error` alongside the existing archive exceptions and add a late-corruption fixture asserting `ValueError` and no output. This is **Minor**, because the normal CLI uses the fixed trusted archive digest and therefore rejects arbitrary corrupted bytes before attempting decompression; the observed case requires a caller-supplied matching digest or a changed lock. There is no extraction escape or residue demonstrated.

## Spec assessment

- The lock matches every supplied value, including source commit/hash/root, ESP-IDF v6.1 and exact commit, ESP-SR 2.4.7, StopWatch board/variant, and ESP32-S3 target.
- Preparation performs a streaming hash with a compressed-size bound before opening the tar. All represented members are checked for the configured root, absolute/traversal paths, supported file/directory types, and cumulative regular-file sizes before extraction. It strips the root, uses the Python data filter, extracts into a sibling temporary tree, checks destination absence again, and moves the prepared tree into place. Existing dangling destinations are refused. The documented non-concurrent CLI usage is coherent; no additional concurrency guarantee was required for this build prerequisite.
- Download handling uses the required timeout, bounds bytes read, validates the completed temporary download before retaining it, and removes the temporary file on failure. Preparation does not execute downloaded code. Script-relative lock selection and alternate destination/archive behavior match the brief.
- CI preserves the exact SDK HEAD assertion before the upstream canonical tests/build command. The source digest is checked during preparation. Targeted inspection of the prepared manifest confirmed `espressif/esp-sr: ==2.4.7`, so the reported ESP-SR identity is enforced by the locked upstream source rather than being merely an unused lock annotation.
- CI explicitly checks all ten required evidence files for nonzero size before upload, uploads the same enumerated files, enables hidden files for `.cache`, uses failure on no matched files, and retains artifacts for seven days. No missing-evidence or hidden-directory-upload defect was identified in this revision. Actual Linux artifact contents remain to be established by the controller.
- README documents the pinned provenance, prepare/build commands, ignored outputs, private Actions artifacts, future tracked patches/overlays, and the boundary between stock compilation, AEC, flashing, and acoustic validation. Attribution and the complete MIT license are present without relicensing this workspace's own code.
- The diff changes only the seven task files. No Go behavior, serial/device/server action, firmware flashing, OTA upload, AEC implementation, or hardware validation was introduced. A focused `git ls-files` query returned no tracked `.superpowers`, firmware cache/build/managed-components, or private `artifacts` files. Existing ignore entries cover those output locations.

## Review method and verification limits

Read the requirements brief first, implementer report second, and the entire packaged review diff once as the primary evidence. Used only closely related source/ignore checks and the single named late-deflate-corruption experiment above. Did not rerun the reported 18 owned tests or 110 upstream host tests, did not compile firmware, and did not contact a device, serial port, server, or remote service. No implementation files, commits, or Git state were changed by this review; only this ignored review report was written.

A passing review allows the controller to proceed to the first exact-head Actions build. It does not establish a successful stock build, reproducibility of every transitive dependency across future registry resolution, AEC correctness, device readiness, or acoustic acceptance.

## Focused re-review conclusion — 69b44f9

Both final verdicts are **APPROVED** for the controller's next exact-head Linux build stage. The gzip finding is closed. Unchanged parts of the prior review remain approved; no new findings arose from the exception-handler/test correction. Read the amended complete package and reported red/green evidence without rerunning suites, compiling firmware, or changing implementation/Git state. Exact-head Actions, successful stock build and artifact inspection remain outstanding; AEC and hardware acceptance remain outside this review.
