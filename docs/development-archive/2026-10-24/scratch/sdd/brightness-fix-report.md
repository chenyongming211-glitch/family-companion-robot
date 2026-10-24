# Brightness settling fix report

Status: DONE (package implementation and local verification only).

## Behavior

Brightness setters retain the immediate first status read after ACK. A valid
intermediate brightness now schedules another `self.get_device_status` read
100ms later through `Expire`. Only status reads repeat; the setter is sent once.
Success requires the requested value. Every observation stays within the original
5s deadline, including queue time. The consumed RPC ID is retired while waiting,
so duplicate or late replies cannot settle the call or disturb the next read.

Cancellation drops both scheduled and in-flight observation state. The existing
3s discovery timeout, four-call cap, FIFO order, unique numeric RPC IDs, argument
validation, filtered outputs and volume mismatch failure behavior are retained.
Missing/invalid brightness and malformed/error results still fail immediately.

## TDD evidence

Before changing `bridge.go`, ran:

```text
go test ./internal/devicetools -run 'TestBrightness|TestCancelBrightness' -count=1
exit 1
TestBrightnessReadbackWaitsForFirmwareFade/downward:
  intermediate firmware brightness must remain pending:
  verification_failed, ok:false, verified:false
TestBrightnessReadbackWaitsForFirmwareFade/upward:
  intermediate firmware brightness must remain pending:
  verification_failed, ok:false, verified:false
```

Multiple-intermediate, original-deadline and cancellation cases also failed for
the same premature `verification_failed` completion. After the minimal state
change, `go test ./internal/devicetools -count=1` passed (2.434s).

Final verification after adding queue preservation assertions and formatting:

```text
go test -race ./internal/devicetools
ok  familyrobot/internal/devicetools  1.550s
exit 0

go vet ./internal/devicetools
(no output)
exit 0
```

Coverage includes downward 49→0 and upward 1→50 fade, no polling at 99ms,
fresh read-only polling at 100ms, multiple intermediate values, duplicate consumed
RPC replies, queue cap/FIFO preservation, original deadline including queued
time, deadline while scheduled/in-flight, cancellation while scheduled/in-flight,
and late canceled/expired reads preserving the next generation. Volume mismatch
has an explicit `verification_failed` regression assertion.

## Remaining concern

These deterministic package tests do not establish Linux integration or actual
firmware round-trip behavior. Parent owns integration, deployment and on-site
validation. Poll delivery follows the existing owner's `Expire` ticks, so actual
read intervals may exceed 100ms; the total deadline still takes priority. No
serial, server, deployment, API or hardware access was performed.
