# Task 3 implementation report

Status: DONE

## Implemented

- Qwen native session configuration accepts the bridge's fixed nested `type:function/function:{name,description,parameters}` tools; complete function events parse `response_id`, `call_id`, `name`, and string `arguments`. Native audio settings remain unchanged.
- MCP-capable device hello triggers bounded discovery before Qwen open. No-MCP hello has no discovery delay. Discovery retains/replays device events with 128-event and 1 MiB bounds; malformed/overflow input closes safely. Normal discovery error/3-second timeout retains voice with an explicit controls-unavailable instruction.
- Session configuration is copied before adding fixed tools and control policy. Relative changes must read status first; screen off is brightness 0; only verified successful results permit a success claim.
- The existing session owner serializes bridge calls, expiration, MCP socket writes, model outputs, and continuation. Only complete argument events start calls; stale response IDs and deltas are ignored. Dedupe precedes pending accounting.
- Tool outputs are sent only after every accepted call completed, the parent response completed, and all parent audio plus its final actual-frame 60 ms deadline drained. Outputs precede exactly one response.create; awaitingResponse guards the continuation.
- A tool-only parent emits no phantom TTS stop. An audio-bearing tool parent and its continuation share one TTS block and Opus encoder, with one final stop. This prevents v2.5.2 firmware's automatic listen.start after stop from interrupting continuation. Normal playback cadence and final-frame deadline remain unchanged.
- Abort, listen.start, and model speech_started include outstanding tools in interruption detection. Reconnect cancels old bridge generation and clears pending results. Internal bridge/dedupe keys include generation; model outputs retain raw call_id, allowing new upstream conversations to reuse IDs safely. Late canceled RPC replies cannot start queued old mutations or resume an old turn.
- Eight unique tool calls are accepted per user transcription; a ninth closes safely to prevent an infinite model continuation loop. A new transcription renews the turn budget. A separate 4096-call lifetime cap bounds both dedupe maps. Call ID/name/arguments limits are 256/128/4096 bytes; response IDs are bounded at 256 bytes.

## Behavioral TDD evidence

Initial production files were unchanged when the first real-WebSocket test failed:

- `.superpowers/sdd/task-3-red.log`: discovery→native-tools→setting→readback→function-output→continuation test timed out waiting for MCP initialize (7 seconds).
- `.superpowers/sdd/task-3-overlap-red.log`: a parent drained while a tool remained pending incorrectly accepted an unsolicited new response. Added rejection and regression.
- `.superpowers/sdd/task-3-generation-red.log`: abort followed by a fresh upstream reusing the raw call ID timed out waiting for the new MCP call. Generation-scoped bridge keys fixed it.
- `.superpowers/sdd/task-3-firmware-red.log`: firmware-like stop→listen.start behavior observed an intermediate stop at starts=1, frames=1 before continuation. Preserving the single TTS block and encoder fixed it.

Wire-level integration cases cover initialize/list, fixed schemas, setter ACK + filtered readback, duplicate arguments.done, invalid arguments, unknown tool, stale response/delta events, multiple pending results, parent completion gate, audio drain gate, no phantom stop, device failure, verification mismatch, actual five-second tool timeout, three-second discovery timeout/error/no-MCP voice fallback, buffered listen replay, buffer count/byte limits, bounded model fields, eight-call termination/new-transcription renewal, cancellation of active+queued work, late RPC replies, reused call IDs after device abort/listen/model interruption, and firmware automatic listening after final TTS stop. Qwen client regression independently checks native audio plus nested tools and function event parsing.

## Final verification

After the generation and TTS-block fixes:

- `go test ./internal/gateway -count=1`: PASS (11.457s).
- `go test -race ./...`: PASS; final gateway result 12.533s. Full output: `.superpowers/sdd/task-3-race.log`.
- `go vet ./...`: PASS, no output. Log: `.superpowers/sdd/task-3-vet.log`.
- `go build -o bin/gateway ./cmd/gateway`: PASS.
- `go build -o bin/smoke ./cmd/smoke`: PASS.

Self-review checked generation isolation, dedupe before pending accounting, queue cancellation, native API shape, parent/completion/drain ordering, encoder continuity, bounded state, config isolation, and unchanged Task 2 scheduler/final-frame behavior. Existing no-MCP interruption and playback regressions pass.

## Scope and remaining concerns

Changed only the authorized Qwen/session/tool test files and the playback boundary needed to synchronize firmware tools and voice. No devicetools edits, Git operations, deployment, real API calls, or hardware validation were performed by this task. Parent agent owns external model/field validation.

Known pre-existing macOS linker `LC_DYSYMTAB` warnings appeared in race-test linking for audio/gateway; all test commands succeeded. No unresolved implementation blocker identified.
