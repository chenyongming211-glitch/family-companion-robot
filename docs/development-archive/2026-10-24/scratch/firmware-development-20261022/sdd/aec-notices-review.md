### Spec Compliance

- ✅ Spec compliant for the focused packaging follow-up, base `3ac93b1`, head `530a395340c276dc15d7f7c943825d417aaf807b`. The supplied complete diff changes only the evidence helper, its existing behavioral test and the README description.
- ✅ The helper byte-copies the two specified existing notices into explicit, noncolliding outer-bundle paths before computing the complete hash inventory (`firmware/tools/verify_experiment.py:123-125`, `:129-131`). The canonical upstream ZIP, workflow and runtime are unchanged by this diff.
- ✅ The test creates synthetic notice fixtures, asserts both exact saved byte sequences and their explicit inventory membership, then verifies every listed digest and total inventory coverage (`firmware/tests/test_experiment_evidence.py:25-29`, `:116-126`). The README accurately limits the claim to notice preservation in the outer evidence bundle (`firmware/README.md:413-416`).
- ⚠️ Actual produced/downloaded artifact contents still require controller inspection. This scoped change and synthetic fixture do not establish target compilation, artifact acceptance, comprehensive licensing compliance or physical/acoustic acceptance.

### Strengths

- A minimal three-line packaging addition preserves existing files without changing their content or introducing a new licensing-policy interpretation (`firmware/tools/verify_experiment.py:123-125`).
- The regression test checks real copied output bytes and checksum coverage, rather than implementation text or mocked copy calls (`firmware/tests/test_experiment_evidence.py:116-126`). Missing source notices cause copying to fail, preventing successful packaging/upload with silently omitted notices (`firmware/tools/verify_experiment.py:124-125`).

### Issues

#### Critical (Must Fix)

- None.

#### Important (Should Fix)

- None.

#### Minor (Nice to Have)

- None.

### Assessment

**Task quality:** Approved.

**Reasoning:** The implementation carries exactly the requested existing notice files into the detached evidence bundle and hashes them using the existing inventory mechanism. The focused fixture provides meaningful byte-preservation and inventory assertions without expanding runtime or build scope.

### Review Evidence and Scope

- Read the full supplied focused diff once and the appended notices follow-up in `aec-delivery-report.md`. The report records a failing real bundle assertion before the change and four passing focused helper tests afterward; these executions were not independently repeated in this review.
- No tests, Git commands, outside-diff source checks, device operations or network actions were run. Only this requested review report was written; no source/index/HEAD/branch/worktree changes were made.
