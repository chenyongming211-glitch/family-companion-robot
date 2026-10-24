## Task 2: 音频调度漂移回归与修复

Files: `internal/gateway/playback.go`、新增 `playback_test.go`。

- [ ] 先写真实 WebSocket 写出回归，在存在微小 tick 抖动时验证多帧完成时间与既定 60ms 时间线相符；旧代码应因累计推迟而失败。
- [ ] 下一帧时间基于原定时间推进，而非每次 `now + 60ms`；明显停顿/缺音后重置锚点，避免突发补发。
- [ ] 补停顿恢复及末帧/停止验证，运行 `go test -race ./internal/gateway`。



## Context and ownership

Only edit internal/gateway/playback.go and new playback_test.go; report .superpowers/sdd/task-2-report.md. No Git repository or Git operations. Follow TDD; capture behavioral red using old scheduler, then implement minimal fix. Real packet captures show mean 64–65ms despite 60ms PCM due to now+60 each frame and 10ms ticker jitter. Add a deterministic virtual-time test driving play(now) while receiving real WebSocket frames; use tiny tick jitter so old code accumulates drift. Advance from original nextAudio deadline, re-anchor after material stall/empty buffer to avoid rapid catch-up; partial final frame and TTS stop need correct time/ordering. Existing server interruption tests must stay green. Do not alter session.go (Task 3 owns it).

Return DONE/DONE_WITH_CONCERNS/BLOCKED with one-line tests; full report records red and green. Run go test -race ./internal/gateway. Known Mac linker LC_DYSYMTAB warning predates this change; report it separately.
