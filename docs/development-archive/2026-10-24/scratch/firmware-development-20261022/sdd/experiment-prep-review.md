# Spec Compliance

- **Approved with one Minor gap:** `33b0401..d54991c` implements the requested atomic source preparation, real patch/overlay tests, native dependency lock/checksum, and README scope. A root `dependencies.lock` overlay is silently overwritten by controlled output rather than rejected; see the finding below.
- **Cannot verify from the diff alone:** exact byte identity with the prior stock artifact and the real-source execution reported by the implementer. The checked-in native YAML visibly pins ESP-SR 2.4.7, esp_codec_dev 1.6.2 and IDF 6.1.0. Reported artifact-byte/provenance assertions and IDF schema acceptance are separate evidence; no SDK build is established by source preparation.

# Strengths

- `firmware/tools/prepare_experiment.py:29` reuses the existing safe archive/download helpers and checks the dependency record's regular type, bounded size and adjacent digest before copying the captured bytes. Native YAML is preserved without an unnecessary parser.
- `firmware/tools/prepare_experiment.py:72` contains extraction and all patch/overlay work in an invocation-owned temporary sibling. The destination is refused before work and immediately before publication at line 109. Failures leave the fresh destination unpublished and TemporaryDirectory cleanup affects only the owned tree.
- `firmware/tools/prepare_experiment.py:75` removes inherited GIT_* overrides and line 77 prevents upward repository discovery. Both real git apply passes use the isolated cwd/environment, explicit absolute patch paths, strict whitespace and a 30-second timeout. Lexical ordering allows successive patches to modify the same file.
- `firmware/tools/prepare_experiment.py:89` rejects destination collisions before overlay copies and checks the resolved parent against the prepared root, preventing patch-created links from routing copies outside it. Symbolic input entries and dependency targets are rejected.
- `firmware/tests/test_prepare_experiment.py:94` checks a real patch/overlay/dependency/provenance result. The subsequent real parent-Git fixture verifies sentinel and index bytes with inherited Git overrides. Failure tests retain an unrelated temporary-looking sibling and cover bad patches/archive hashes, collision, destination/symlink preservation, dependency pin/size/type errors, traversal, ordered patches and controlled CLI errors. These are behavioral tests, not source mirrors.
- `firmware/README.md:72` documents the separate preparation entry, script-relative defaults, patch order, collision rules, native pin and atomic publication, with an explicit absence of AEC/firmware integration.

# Issues

## Critical

None.

## Important

None.

## Minor

- **P3 — reject overlays that collide with generated dependency output.** `firmware/tools/prepare_experiment.py:105` unconditionally overwrites the root `dependencies.lock` after it may already have been accepted as an overlay and entered in provenance at line 98. For an archive without that file, a regular `overlays/dependencies.lock` currently succeeds, loses its copied bytes, and leaves an overlay digest that no longer matches the published file at its recorded path. The pinned dependency bytes remain correct; the defect is the accepted-overlay contract and provenance consistency. Reserve this root filename explicitly and fail atomically if an overlay claims it, with a real fixture asserting rejection/no destination. The existing `.family-robot-source.json` collision check already rejects its corresponding reserved output path.

# Assessment

- **Task quality: Approved with the Minor finding above.** Default inputs, atomicity, Git-parent isolation and fixed dependency-pin integrity are sound in the reviewed implementation. The reserved-filename edge case should be corrected before overlays grow to include other root metadata.
- **Focused check run for the named provenance risk:** constructed a real tar archive lacking dependencies.lock, a verified native dependency record, and an overlay of that same filename with different native bytes; invoked the real preparer in a temporary directory with no patch directory. Result: `preparation=success`, `overlay_bytes_preserved=False`, `recorded_overlay_digest_matches_output=False`, `pinned_dependency_bytes_preserved=True`. No existing suite was rerun. The fixture and generated tree were temporary and removed.
- **Review method:** read the corrected brief, report and supplied implementation diff; retrieved only tool-truncated diff portions and line locations afterward. Reviewed the five implementation files; intervening controller plan/evidence commits in the packaged range were context, not a new implementation task. Reported 18 focused and 38 full successful tests were not independently rerun, and no additional unverified concern justified broad testing.
- **State:** no tracked files, index, HEAD, branch, remote, SDK, device or server state changed. Only this requested ignored report was written. Preparing source does not establish target schema/build acceptance, synchronization timing, acoustic performance or natural barge-in completion.

# Narrow re-review — 04f0b94

- **Spec compliance: Approved with the same Minor provenance gap still reachable through an internal symlink.** The ordinary root-name cases are corrected, but the reserved dependency output can still be claimed indirectly.
- **Task quality: Approved with one open Minor finding.** No Critical or Important finding; no broader review scope was added.
- **Strength:** `firmware/tools/prepare_experiment.py:91` explicitly rejects both direct reserved root filenames before copying. The two new real fixtures check atomic failure using the existing sibling/sentinel assertions, and README documents the reservations. Reported RED followed by 20 focused GREEN cases is consistent with this change.
- **P3 — remaining alias bypass of reserved dependency output:** `firmware/tools/prepare_experiment.py:91` compares only the lexical overlay path. The existing resolved-parent check at line 96 permits links that stay inside the tree. A real patch can create `alias -> .`, then an overlay `alias/dependencies.lock` passes the new check, writes the root dependency file, is hashed as an overlay, and is subsequently overwritten by the pinned dependency output. The accepted overlay hash still disagrees with the published path. Fix by rejecting overlay traversal through any patch-created symlink, or by reserving the resolved root dependency output as well as its lexical name. Add this real patch-plus-overlay fixture so the fix covers the actual published target. Pinned dependency integrity remains intact; the defect is still accepted-overlay/provenance consistency.
- **Focused check:** executed exactly the new alias case above in an owned temporary fixture with real git apply. Output: `preparation=success`, `patch_created_alias=True`, `recorded_overlay_path=alias/dependencies.lock`, `recorded_overlay_digest_matches_output=False`. No existing suite was rerun. All fixture files were removed with the temporary directory.
- **Scope/state:** read the amended report and supplied fix diff, including only controller docs as context. No implementation/Git/device/server state changed; only this requested report appendix was written.

# Final narrow re-review — ad5d513

- **Spec compliance: Approved.** The direct reserved-output and internal-alias provenance findings are resolved; no open spec finding remains within this preparation task.
- **Task quality: Approved.** No open Critical, Important or Minor findings from this review. These final verdicts supersede the preceding provisional verdicts.
- **Resolution:** `firmware/tools/prepare_experiment.py:95` now checks every lexical parent in `relative.parents` for a symlink before any mkdir/copy, rejecting even links that resolve inside the prepared tree. This closes the demonstrated `alias -> .` bypass and deeper parent aliases. The target collision check still rejects symlink targets themselves, while direct reservations and resolved containment remain intact. Normal regular directory paths are unaffected.
- **Regression evidence:** the supplied delta adds a real mode-120000 Git symlink fixture and three atomic-failure cases for the dependency name, provenance name and an ordinary filename. The ordinary-file case guards the general rule rather than only special-casing reserved names. Shared failure assertions preserve the unrelated sibling and require an absent destination. The amended report records the expected two RED failures and 23/23 focused GREEN results without diagnostic noise.
- **Checks/limits:** read the supplied `04f0b94..ad5d513` fix diff and last report amendment; no new concrete risk required execution, so no tests were rerun. Controller plan-only changes were context. No implementation/Git/device/server state changed; only this report appendix was written. Earlier source-only and target-build/field-validation limits continue to apply.
