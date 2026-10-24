# Task 2 report

Status: DONE

## Scope and changes

- Only changed `internal/gateway/playback.go`, added `internal/gateway/playback_test.go`, and wrote this report. No Git operations, commit, deployment, or session integration.
- Each successful frame advances `nextAudio` from its original deadline by 60ms. Initial playback and lateness of at least one full frame reset the anchor to `now`; an empty or incomplete pending frame clears the anchor so resumed audio gets a fresh 60ms interval.
- Added virtual-time regression tests that send and receive real WebSocket traffic and decode every Opus packet. A WebSocket barrier after each `play(now)` call allows deterministic absence and ordering assertions without real-time pacing sleeps.

## TDD evidence

The regression test was written and run before any production change:

```text
go test ./internal/gateway -run '^TestPlaybackTickJitterDoesNotAccumulate$' -count=1
--- FAIL: TestPlaybackTickJitterDoesNotAccumulate
    playback_test.go:147: frame 2 written at 130.006ms, want [120ms, 130ms]; tick jitter accumulated
FAIL
```

The completion-time assertion and additional preservation cases were then added, still before changing `playback.go`:

```text
go test ./internal/gateway -run '^TestPlayback' -count=1
--- FAIL: TestPlaybackTickJitterDoesNotAccumulate
    playback_test.go:146: last of 20 frames written at 1.320006s, want [1.14s, 1.15s]; tick jitter accumulated
FAIL
```

The test drives a 10ms virtual ticker with just 0–6us jitter. The old scheduler took 1.320006s to emit 20 frames, about 180ms beyond their intended first-to-last 1.14s timeline. The fixed scheduler satisfies each frame's original 60ms deadline within one ticker interval and emits stop within one ticker interval of the 1.2s playback end.

## Verification

```text
go test ./internal/gateway -run '^TestPlayback' -count=1
ok  familyrobot/internal/gateway  0.570s

go test -race ./internal/gateway
ok  familyrobot/internal/gateway  2.716s
```

Coverage includes:

- 20 real WebSocket Opus frames under tiny deterministic tick jitter, plus completion time.
- Re-anchor after both a one-frame-late scheduler call and a one-second stall, with no following 10ms/59ms catch-up packet.
- Empty buffer and incomplete streaming-frame underruns; resumed frames remain 60ms apart.
- Partial final frame padding to a decodable 60ms Opus packet, start/sentence/audio ordering, stop only after that frame's duration, history finalization, and no repeated stop.
- Existing gateway interruption tests pass under the race detector.

The race build printed the already-known macOS linker `LC_DYSYMTAB` warning (malformed symbol table in a temporary Go object file). It did not fail the build or tests; no new race report occurred.

## Limits

This establishes the cadence regression and recovery behavior only. Device crackling is not field-confirmed fixed. No claim is made about original-video audio quality, firmware playback, or on-device listening results. Tool-only-response stop suppression belongs to Task 3.

## Review follow-up: preserve the final frame's full duration

Review identified a separate end-of-playback issue: with absolute deadlines 0/60/120ms and actual sends at 0/65ms, finalization at 120ms only gives the last packet 55ms. A new regression was added before changing production code and failed for both a complete final frame and a padded partial final frame:

```text
go test ./internal/gateway -run '^TestPlaybackLateFinalFrameKeepsFullDuration$' -count=1
--- FAIL: TestPlaybackLateFinalFrameKeepsFullDuration (0.00s)
    --- FAIL: TestPlaybackLateFinalFrameKeepsFullDuration/full (0.00s)
        playback_test.go:177: WebSocket messages = [stop], want []
    --- FAIL: TestPlaybackLateFinalFrameKeepsFullDuration/partial (0.00s)
        playback_test.go:177: WebSocket messages = [stop], want []
FAIL
```

The minimal fix retains absolute `nextAudio` pacing and records `lastAudio` after a successful binary write. Finalization separately waits until `lastAudio + 60ms` when that timestamp is nonzero. Authorized follow-up scope includes a `lastAudio` field and resets in `session.go` at interruption and new response creation, plus reset on playback completion.

The tests assert no stop or history finalization at 120ms or just before 125ms, one stop at 125ms, and no repeated stop afterward. Another case exercises fresh response creation and playback. The multi-frame jitter test now measures its stop window against the final actual send plus 60ms while retaining its original absolute frame-deadline assertions.

Fresh verification after the fix:

```text
go test ./internal/gateway -run '^TestPlayback' -count=1
ok  familyrobot/internal/gateway  0.532s

go test -race ./internal/gateway
ok  familyrobot/internal/gateway  2.521s
```

The same pre-existing macOS `LC_DYSYMTAB` linker warning occurred during the race build; tests passed without a race report. No Git operations or deployment occurred. Field audio quality remains unverified.
