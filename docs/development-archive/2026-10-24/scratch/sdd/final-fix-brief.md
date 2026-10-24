# Final review fix: actual playback timing

One consolidated Important/P2 finding. No Git or deployment; workspace /Users/chen/code/家庭陪伴机器人. User approved full repair already.

Files allowed: internal/gateway/playback.go, session.go, playback_test.go, session_tools_test.go if adapting shared test harness is necessary. Avoid unrelated feature changes. Preserve bridge, Qwen API, absolute cadence, bounded queue, TTS/Opus continuity and interruption behavior.

Problem: playback.go records ticker-supplied now as lastAudio after successful write. A buffered tick stamped 60ms may be handled at160ms (e.g. MCP I/O), followed by170ms tick; current guard thinks60ms has elapsed and stops/continues only10ms after actual final write. Encoding/WebSocket write delay causes same issue. Stall re-anchor also uses stale time, allowing tight catch-up. Current tests equate tick time with write time and miss it.

Requirements: use actual processing time for scheduler lateness/stall decisions; sample time after successful audio write to establish final-frame +60ms protection; preserve absolute timeline under tiny jitter and avoid tight catch-up after a meaningful stall, including delay in the write itself. Do not block/sleep audio loop. A small private injectable clock/test write timing seam is acceptable; production time must be real monotonic wall clock. Tests should drive real WebSocket output and controlled clock/writer latency, not merely assert internal fields.

TDD: add behavioral failures for delayed stale tick and delayed successful write first, then minimal fix. Existing jitter, full/partial final frame, underrun, tool-parent drain and firmware automatic-listen tests must remain green. Run focused playback/device-tools tests then go test -race ./..., go vet ./..., builds gateway/smoke. Record exact red/green output plus any existing linker warning in .superpowers/sdd/final-fix-report.md. Return concise DONE/DONE_WITH_CONCERNS/BLOCKED, tests and concerns. This is one fix wave for the entire broad-review findings list (no others).
