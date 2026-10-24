# Task 1 final review

Reviewed scope: final store-only diff `5dbbf67..7cb958e`, the Task 1 brief and its binding Global Constraints, updated implementation report, and the previous review finding. Gateway integration, per-entry retention/provenance/playback/recovery rules, and the seven-day application default remain Task 2 responsibilities.

## Final verdicts

- **Spec compliance: PASS for Task 1.** The requested FIFO, bounded asynchronous storage, atomic replacement, expiry, Clear ordering, permissions and exclusive-lock contract are implemented. The previous Close/reopen defect is fixed.
- **Quality: PASS.** No remaining Important or Critical findings in the reviewed scope. The descriptor fix is small and the new regression test exercises the reported failure behavior.

## Previous Important finding: resolved

The initial review reproduced a P2 defect at commit `585372a`: after opening a store and starting `/bin/sleep`, Close returned success but a subsequent Open failed with `ErrLocked` while the child remained alive. The raw lock and snapshot descriptors were opened without close-on-exec.

Commit `7cb958e` resolves the issue:

- `internal/recordstore/store.go:301-308`: `openStoreFile` adds `O_NOFOLLOW|O_CLOEXEC` atomically in `syscall.Open`; there is no separate post-open close-on-exec race window.
- `internal/recordstore/store.go:235-240`: the directory lock now uses that helper and retains the existing nonblocking exclusive flock.
- `internal/recordstore/store.go:326`: snapshot reads use the same helper, preventing record descriptors from leaking across exec.
- `internal/recordstore/store_test.go:215-303`: the regression test launches an actual exec child, checks descriptor device/inode identities, keeps that child alive through Close and reopen, and verifies the saved snapshot can still be loaded. It holds a record descriptor through the production helper to test read-descriptor inheritance deterministically instead of racing a brief read.

The updated implementation report records regression failure before the fix (both inherited lock and record handles, plus failed reopen), focused-test success after the fix, and final package race/vet success. The source change and regression assertions directly address the reproduced defect.

## Contract review

- `store.go:21-25, 88-101, 116-144`: 32 queued operations, bounded input and serialized-envelope checks, owned data copy, immediate busy errors, and buffered persistence receipts.
- `store.go:64-85, 146-218`: one worker owns disk operations; Load/Clear share the FIFO; queued cancellation skips execution; Close stops admission, drains accepted work, and reports timeout without dropping writes or prematurely releasing the lock.
- `store.go:104-110, 221-287, 301-345`: SHA256 filenames, 0700 directory and 0600 records, exclusive lock, bounded reads, invalid/corrupt data errors and existing-file permission tightening.
- `store.go:269-298, 347-387`: UpdatedAt-based cleanup at Open and Load, cleanup before writes, eight-device capacity, ordered Clear and directory sync. Retention is an explicit positive store input, with the default owned by gateway integration as clarified by the parent.
- `store.go:388-428`: temporary write, file fsync, close, atomic rename and directory fsync; receipts report failures including a post-rename directory-sync failure.
- `store_test.go`: tests cover FIFO/reopen/isolation, Clear ordering, expiry/corruption/size/capacity, saturation/cancellation, Close drain/timeout, data ownership, permissions, symlink rejection, rename failure receipts, and subprocess descriptor inheritance.

## Verification boundary

This re-review inspected the final complete diff, actual source locations and updated report; no suites were rerun and no source files were modified. The final implementation report records:

- `go test -count=1 -run '^TestExecChildDoesNotInheritStoreFiles$' ./internal/recordstore` → PASS (`0.495s`).
- `go test -race -count=1 ./internal/recordstore` → PASS (`3.180s`).
- `go vet ./internal/recordstore` and `git diff --check` → exit 0.

Those final command outcomes are implementer evidence. The initial review independently reproduced the original lock defect using an unchanged source copy outside the checkout. Validation remains on macOS; Linux device operation and power-loss durability are not established by this store-only review.
