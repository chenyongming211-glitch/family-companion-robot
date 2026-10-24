# Task 1 report: device tool state machine

Status: DONE

## Scope

Created only `internal/devicetools/bridge.go`, `internal/devicetools/bridge_test.go`, and this report. No deployment, Git operation, or source changes outside the assigned package.

The bridge exposes the exact API from `task-1-brief.md`. Discovery sends MCP `initialize` then paginated `tools/list`, with a total 3 second deadline, at most 8 pages, repeated cursor rejection, and a 1024 byte cursor bound. `Ready()` becomes true after success, failure, or timeout. Discovery failures expose no tools.

Only the three fixed public names can be registered. Setters require both the corresponding device function and `self.get_device_status`. Tool descriptions and argument schemas are fixed locally; device metadata is never passed to the model.

Calls have a total 5 second deadline, including queue time. There are at most 4 unfinished calls. The bridge serializes a setter ACK and its status readback before starting the next FIFO call. A setter ACK alone cannot complete successfully; mismatches return `verification_failed`. Numeric RPC IDs increase monotonically, so late responses cannot complete a new call. Call IDs are deduplicated for the lifetime of the bridge. Cancellation removes active/queued calls of a generation and starts remaining eligible work, without trying to undo previously sent mutations.

## TDD evidence

1. Wrote deterministic state-machine tests before implementation.
2. `go test ./internal/devicetools` initially exited 1 because the new API did not exist (`undefined: Update`, `undefined: Bridge`, `undefined: New`).
3. Added only API stubs, then ran `go test ./internal/devicetools` again. It exited 1 with behavioral failures, including:

   ```text
   --- FAIL: TestDiscoveryPaginationAndStaticWhitelist
       bridge_test.go:81: want one initialize request, got {Requests:[] Results:[]}
   FAIL familyrobot/internal/devicetools
   ```

4. Implemented the state transitions and argument validation; the same tests passed.

Final fresh verification:

```text
$ go test -race -count=1 ./internal/devicetools
ok   familyrobot/internal/devicetools 1.298s

$ go vet ./internal/devicetools
# exit 0; no output

$ go test -cover ./internal/devicetools
ok   familyrobot/internal/devicetools 0.506s coverage: 96.1% of statements
```

There are 11 top-level tests plus table-driven cases covering paginated whitelist discovery, dependency on status readback, discovery errors and total timeout, page/cursor bounds, status privacy, setter verification and FIFO ordering, integer/range/object validation, duplicate fields and trailing JSON, safe device errors, queue limits, call deduplication, total call deadlines, late replies, cancellation, and unrelated/malformed responses.

## Integration and self-review

- `Update.Requests` contains MCP payloads only; transport wrapper remains the session owner's responsibility.
- Success output is a JSON string such as `{"ok":true,"verified":true,"volume":70,"brightness":80}`. Only available valid volume/brightness values are included. Other device fields, SSIDs, arbitrary descriptions, and raw errors are never included.
- Failure output has `ok:false`, `verified:false`, and one fixed `error` tag: `invalid_arguments`, `not_ready`, `unavailable`, `queue_full`, `device_error`, `invalid_response`, `verification_failed`, `timeout`, or `canceled`.
- The owner must serialize method calls and provide `Expire` ticks. The bridge has no background goroutines or I/O. It does not reconnect or retry failed discovery; a new device session creates a new bridge.
- Cancellation returns completion records tagged with the canceled generation; session integration decides whether that generation still accepts model output.
- Retaining seen call IDs for the bridge lifetime is deliberate to satisfy deduplication, including completed/rejected/canceled calls. The bounded queue concerns unfinished work.
- Review found no unresolved package-level correctness concern. This verification does not claim hardware behavior or Qwen/session integration; those are outside this task.
