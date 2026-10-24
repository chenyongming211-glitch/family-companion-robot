# Final review timing fix report

Status: DONE

## Change and scope

The consolidated P2 finding is fixed. A queued ticker stamp no longer enters playback: `run` discards it and `play` samples its clock when processing. After a successful real WebSocket audio write, playback samples the clock again for `lastAudio`. A full-frame stall measured at write completion re-anchors the next deadline to that completion; smaller jitter retains the absolute cadence. Final response completion, TTS stop, and tool continuation cannot occur until the final successfully written frame has had 60 ms.

Production uses `time.Now` (including Go's monotonic clock component) and the existing deadline-bounded WebSocket writer. Two private session function fields make processing time and audio-write latency controllable in tests. The test writer always delegates to the real WebSocket writer, then advances the controlled completion clock before returning success. No sleep or blocking pacing was added to the audio loop.

Changed only `internal/gateway/playback.go`, `internal/gateway/session.go`, and `internal/gateway/playback_test.go`, plus this report. No bridge, Qwen API, queue, interruption, TTS block, or Opus continuity changes. `session_tools_test.go` did not need adaptation. No Git operations, deployment, external model calls, or hardware validation were performed by this task.

## TDD evidence

Before changing scheduler behavior, added the private clock/writer seams and four behavioral regressions. At the red stage playback still used the supplied ticker stamp and stored it after writing, exactly as before. Every observation decoded actual WebSocket messages and used a following barrier message for deterministic absence checks; no assertions depended on real elapsed time or only inspected scheduler fields.

- Stale tick stamped 60 ms but processed at 160 ms: full and partial final frames must not stop at 170 ms and must stop at 220 ms.
- Same stale tick with more buffered audio: no next frame before 220 ms.
- Successful final audio write consumes 100 ms: full and partial final frames must retain their entire 60 ms after completion.
- Successful first or subsequent audio write consumes 100 ms: no tight next-frame catch-up after the write returns.

Exact red command, exit 1:

```text
$ go test ./internal/gateway -run 'TestPlayback(StaleTick|SlowWrite)' -count=1 -v
=== RUN   TestPlaybackStaleTickKeepsFinalFrameFullDuration
=== RUN   TestPlaybackStaleTickKeepsFinalFrameFullDuration/2880
    playback_test.go:223: WebSocket messages = [stop], want []
=== RUN   TestPlaybackStaleTickKeepsFinalFrameFullDuration/1440
    playback_test.go:223: WebSocket messages = [stop], want []
--- FAIL: TestPlaybackStaleTickKeepsFinalFrameFullDuration (0.01s)
    --- FAIL: TestPlaybackStaleTickKeepsFinalFrameFullDuration/2880 (0.00s)
    --- FAIL: TestPlaybackStaleTickKeepsFinalFrameFullDuration/1440 (0.00s)
=== RUN   TestPlaybackStaleTickReanchorsToProcessingTime
    playback_test.go:237: WebSocket messages = [audio], want []
--- FAIL: TestPlaybackStaleTickReanchorsToProcessingTime (0.00s)
=== RUN   TestPlaybackSlowWriteKeepsFinalFrameFullDuration
=== RUN   TestPlaybackSlowWriteKeepsFinalFrameFullDuration/2880
    playback_test.go:253: WebSocket messages = [stop], want []
=== RUN   TestPlaybackSlowWriteKeepsFinalFrameFullDuration/1440
    playback_test.go:253: WebSocket messages = [stop], want []
--- FAIL: TestPlaybackSlowWriteKeepsFinalFrameFullDuration (0.00s)
    --- FAIL: TestPlaybackSlowWriteKeepsFinalFrameFullDuration/2880 (0.00s)
    --- FAIL: TestPlaybackSlowWriteKeepsFinalFrameFullDuration/1440 (0.00s)
=== RUN   TestPlaybackSlowWriteReanchorsToCompletionTime
=== RUN   TestPlaybackSlowWriteReanchorsToCompletionTime/0
    playback_test.go:276: WebSocket messages = [audio], want []
=== RUN   TestPlaybackSlowWriteReanchorsToCompletionTime/1
    playback_test.go:276: WebSocket messages = [audio], want []
--- FAIL: TestPlaybackSlowWriteReanchorsToCompletionTime (0.00s)
    --- FAIL: TestPlaybackSlowWriteReanchorsToCompletionTime/0 (0.00s)
    --- FAIL: TestPlaybackSlowWriteReanchorsToCompletionTime/1 (0.00s)
FAIL
FAIL	familyrobot/internal/gateway	0.836s
FAIL
```

After the minimal production fix, the harness mirrors `run` by discarding the tick stamp, retains the separate processing clock, and still drives the same real wire observations. Exact green command, exit 0:

```text
$ go test ./internal/gateway -run 'TestPlayback(StaleTick|SlowWrite)' -count=1 -v
=== RUN   TestPlaybackStaleTickKeepsFinalFrameFullDuration
=== RUN   TestPlaybackStaleTickKeepsFinalFrameFullDuration/2880
=== RUN   TestPlaybackStaleTickKeepsFinalFrameFullDuration/1440
--- PASS: TestPlaybackStaleTickKeepsFinalFrameFullDuration (0.01s)
    --- PASS: TestPlaybackStaleTickKeepsFinalFrameFullDuration/2880 (0.00s)
    --- PASS: TestPlaybackStaleTickKeepsFinalFrameFullDuration/1440 (0.00s)
=== RUN   TestPlaybackStaleTickReanchorsToProcessingTime
--- PASS: TestPlaybackStaleTickReanchorsToProcessingTime (0.00s)
=== RUN   TestPlaybackSlowWriteKeepsFinalFrameFullDuration
=== RUN   TestPlaybackSlowWriteKeepsFinalFrameFullDuration/2880
=== RUN   TestPlaybackSlowWriteKeepsFinalFrameFullDuration/1440
--- PASS: TestPlaybackSlowWriteKeepsFinalFrameFullDuration (0.00s)
    --- PASS: TestPlaybackSlowWriteKeepsFinalFrameFullDuration/2880 (0.00s)
    --- PASS: TestPlaybackSlowWriteKeepsFinalFrameFullDuration/1440 (0.00s)
=== RUN   TestPlaybackSlowWriteReanchorsToCompletionTime
=== RUN   TestPlaybackSlowWriteReanchorsToCompletionTime/0
=== RUN   TestPlaybackSlowWriteReanchorsToCompletionTime/1
--- PASS: TestPlaybackSlowWriteReanchorsToCompletionTime (0.00s)
    --- PASS: TestPlaybackSlowWriteReanchorsToCompletionTime/0 (0.00s)
    --- PASS: TestPlaybackSlowWriteReanchorsToCompletionTime/1 (0.00s)
PASS
ok  	familyrobot/internal/gateway	0.579s
```

## Final validation

Existing absolute jitter cadence, full/partial final frames, underrun, tool-parent audio drain, interruption, and firmware automatic-listen continuity all passed in the focused verbose playback/device-tools run. An initial filtered bridge run selected no bridge tests because their names do not begin `TestBridge`; the subsequent unfiltered gateway/devicetools run below covers the entire bridge package.

Exact final focused package command/output, exit 0:

```text
$ go test ./internal/gateway ./internal/devicetools -count=1
ok  	familyrobot/internal/gateway	11.526s
ok  	familyrobot/internal/devicetools	0.241s
```

Exact race command/output, exit 0:

```text
$ go test -race ./...
# familyrobot/internal/audio.test
ld: warning: '/private/var/folders/0h/_1wjtc8n6h1gn9mjqwx_n1w40000gn/T/go-link-3273917720/000012.o' has malformed LC_DYSYMTAB, expected 98 undefined symbols to start at index 1626, found 95 undefined symbols starting at index 1626
?   	familyrobot/cmd/gateway	[no test files]
?   	familyrobot/cmd/smoke	[no test files]
ok  	familyrobot/internal/audio	(cached)
ok  	familyrobot/internal/devicetools	(cached)
# familyrobot/internal/gateway.test
ld: warning: '/private/var/folders/0h/_1wjtc8n6h1gn9mjqwx_n1w40000gn/T/go-link-743773351/000012.o' has malformed LC_DYSYMTAB, expected 98 undefined symbols to start at index 1626, found 95 undefined symbols starting at index 1626
ok  	familyrobot/internal/gateway	12.815s
ok  	familyrobot/internal/protocol	(cached)
ok  	familyrobot/internal/qwen	(cached)
```

Additional exact commands, all exit 0 with no output:

```text
go vet ./...
go build -o /tmp/familyrobot-final-fix-gateway ./cmd/gateway
go build -o /tmp/familyrobot-final-fix-smoke ./cmd/smoke
```

The macOS `LC_DYSYMTAB` linker warnings above were already documented in `task-3-report.md` and `task-3-race.log`. No unresolved implementation blocker remains. This validation establishes local software behavior, not hardware playback acknowledgement or deployment status; final candidate real-Qwen/MCP revalidation remains with the parent agent.
