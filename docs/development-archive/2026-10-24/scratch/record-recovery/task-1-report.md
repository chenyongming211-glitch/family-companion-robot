# Task 1 report

Status: DONE

Scope: `internal/recordstore/store.go` and `store_test.go` only; no dependency changes or source integration.

## Actual red/green evidence

1. Before `store.go` existed: `go test ./internal/recordstore` failed with `undefined: Snapshot`, `undefined: Store`, `undefined: Open` and `FAIL ... [build failed]`, proving missing implementation.
2. After the initial implementation: `go test ./internal/recordstore` -> `ok familyrobot/internal/recordstore 0.719s`.
3. Additional validation/permissions regression tests: `go test ./internal/recordstore` failed with `zero retention: <nil>` and `existing permissions ... 420`, exposing zero-retention acceptance and 0644 existing files.
4. After fixes and remaining queue/cancellation/bounds tests: `go test -race -count=1 ./internal/recordstore` -> `ok familyrobot/internal/recordstore 2.165s`.
5. `go vet ./internal/recordstore` -> exit 0, no output.

## Implemented contract

- Exact requested public interface. Version 1, nonzero nonfuture UpdatedAt, valid non-null JSON required. Device IDs nonblank and <=1024 bytes, hashed with SHA256; path-like device IDs never become paths.
- Per parent integration instruction, Open requires explicit positive retention; gateway owns the 7-day default. Expiry uses UpdatedAt. Already-expired Save is a successful retention purge/no-op, and subsequent Load returns ErrNotFound.
- 32 waiting operations plus one worker operation; 1 MiB maximum serialized snapshot including JSON envelope; 8 live devices. Capacity errors for devices are delivered on Save receipts. Immediate invalid-input/full-queue errors come directly from Save.
- Only the worker performs filesystem operations, including initialization; Open waits for initialization. Save copies/validates bounded input and never waits for disk or queue capacity.
- Load/Clear use the same FIFO as Save. Cancellation before execution skips work. If context cancels while disk I/O is running, caller may return before that disk operation completes; subsequent accepted operations still follow FIFO.
- Close stops admission, drains accepted operations, then releases lock. Close is idempotent. Timeout returns context error without dropping queued writes, spawning waiters, or releasing lock early.
- Directory 0700, lock and snapshots 0600, including existing snapshots at Open. Exclusive nonblocking flock. Final directory symlinks, snapshot symlinks/nonregular files, and lock symlinks rejected; path aliases still contend on lock inode.
- Temp file chmod/write/fsync/close, atomic rename, directory fsync; temp cleanup on failures. Expired removals and Clear sync the directory. Open removes abandoned snapshot temp files. Read size is bounded independently of stat size.
- Corrupt/oversized existing records return errors at Open or Load. JSON payloads never appear in validation errors; filesystem errors may contain directory/hash paths.

## Self-review and limitations

- Tests cover FIFO, reopen, cross-device isolation/path-like IDs, Clear ordering, expiry at Open/Load, configured retention, corrupt/oversized files, raw/envelope limits, device capacity reuse, full queue while worker is blocked, canceled queued Clear, canceled Load/Clear, Close timeout/drain/idempotence, input buffer ownership, permission tightening, exclusive lock, symlink rejection, and a deterministic rename failure receipt/temp cleanup.
- Deterministic queue tests enqueue a blocking worker closure within the same package; production has no test hook or injectable disk behavior.
- Validated on this macOS host with the race detector; Linux runtime, power-loss durability and pathological filesystem/kernel stalls have not been field-tested. Context cancellation cannot abort an in-flight fsync; timeout reports this rather than pretending completion.
- Directory is a trusted server-owned dedicated directory. This is not protection against another privileged process replacing files/lock inode or mutating ancestors concurrently.
- A directory-fsync failure after rename returns an error even though the replacement may already be visible. This preserves truthful receipt semantics.
- Snapshot-only store does not interpret per-entry text/task TTL, authenticity, playback completion, history trust, or recovery activation; gateway integration owns those rules.

## Important review fix: exec descriptor inheritance

Status: FIXED (2026-10-23). Read `task-1-review.md` and reproduced its P2 finding before applying the fix.

- Both the directory lock and snapshot reads now use the small shared `openStoreFile` helper. Its raw `syscall.Open` atomically applies `O_NOFOLLOW|O_CLOEXEC`, avoiding a post-open fork/exec race.
- `TestExecChildDoesNotInheritStoreFiles` starts the actual test binary as an exec child, compares descriptor device/inode identities, and keeps the child alive over Store.Close, Open and Load. To test snapshot-read opening deterministically, it holds a snapshot opened through the same production helper rather than racing a brief read. No production test hooks or new dependencies.
- Initial red command: `go test -count=1 -run '^TestExecChildDoesNotInheritStoreFiles$' ./internal/recordstore` -> FAIL, child inherited `.lock`; reopen while child alive returned `snapshot directory already locked: resource temporarily unavailable`.
- After behavior-preserving extraction of the shared opening helper and adding the held snapshot check, the same command -> FAIL, child inherited both `.lock` and the hashed `.json` snapshot; reopen failed with the same lock error.
- After adding atomic `O_CLOEXEC`, the same focused command -> `ok familyrobot/internal/recordstore 0.495s`.
- `go test -race -count=1 ./internal/recordstore` -> `ok familyrobot/internal/recordstore 3.180s` (exit 0).
- `go vet ./internal/recordstore` -> exit 0, no output.
- `git diff --check` -> exit 0, no output.

Validation is on the current macOS host. Linux device runtime and power-loss behavior remain outside this store-only fix's validation boundary. The existing root plan modification belongs to the parent task and was not edited or staged here. The report remains in the repository-ignored `.superpowers` coordination directory.


## Approved retention completion (Task2 fix wave, 2026-10-23)

Added periodic physical expiry to the existing single worker, with cadence clamp(retention/4,25ms,1min), a stopped ticker at worker exit and a payload-free maintenance failure log. The same worker still serializes Save/Load/Clear and expiry, drains accepted operations before releasing the lock, and obeys existing Close timeout/FD behavior. Snapshot UpdatedAt determines file expiry; gateway per-entry text/task pruning remains a separate layer.

Tests-first evidence: TestIdleExpiryPhysicallyRemovesSnapshot writes one real snapshot with80ms retention, verifies its file exists, then only polls os.Stat. It failed before implementation because the expired file remained after700ms, and passes after the worker timer was added. No Load/Save drives cleanup after the initial write. Full final race/vet outcomes are appended to task-2-report.md. Disk stalls can delay the scheduled sweep and deletion/fsync errors do not claim timely or durable success. No new dependency/service/API or parallel disk writer was added.

Retention completion committed with gateway fix as `5772dfb` on main. Final `go test -race -count=1 ./...` PASS, recordstore4.246s; `go vet ./...` PASS. Full output and limits in task-2-report.md.
