## Task 3: 千问工具与会话桥接

Files: `internal/qwen/client.go`、`client_test.go`、`internal/gateway/session.go`、新增 `session_tools.go` 与 `session_tools_test.go`。

- [ ] 写失败集成测试：支持 MCP 的模拟设备完成发现；Qwen session.update 含固定工具；模型调用 → MCP 设置 → 状态核实 → function_call_output → response.create → 语音回复。
- [ ] Qwen Config 增加工具定义；Event 接收函数名、call_id、arguments。使用官方嵌套 `function` 工具结构。
- [ ] 设备声明 MCP 时在建立上游前做有界工具发现；等待期间保存最多 128 个设备事件，发现失败继续纯语音并明确控制不可用。
- [ ] 完整函数参数事件驱动桥接；父回复完成且音频排空、所有本轮工具结果齐全后，回传工具输出并请求续答。
- [ ] 每次用户转写最多 8 次工具调用；取消上游轮次时取消旧代号的排队工具和续答。
- [ ] 测试失败回执、超时、重复调用、打断后迟到结果；运行 `go test -race ./...`、`go vet ./...` 和两个命令构建。


## Integration context and exact decisions

Only edit internal/qwen/client.go, client_test.go, internal/gateway/session.go, new session_tools.go and session_tools_test.go; extend gateway tests only if needed. Do not edit playback.go or devicetools. No Git or deployment. Current qwen client is audio-native; preserve that.

Bridge API is implemented in internal/devicetools/bridge.go. Read it as the interface source. Requests omit transport wrapper; wrap as {"type":"mcp","session_id":s.id,"payload":request}. All bridge calls and socket writes stay under the existing serialized session owner; no background writes. Use Receive for inbound payload, Expire on ticker. Feature hello.Features["mcp"] selects bounded discovery before qwen.Open. Preserve device events while discovering (max 128, additionally max 1MiB total), replay after model opens. Discovery failure must leave voice usable, with unavailable-controls policy. When MCP unsupported do not add discovery delay.

Real upstream preflight already verified exact shape:
- Config.Tools []map[string]any; each tool {type:"function",function:{name,description,parameters}} (bridge provides this).
- Event fields CallID, Name, Arguments strings with JSON call_id/name/arguments.
- A `response.function_call_arguments.done` event contains response_id, call_id, name, arguments JSON string. Only complete-arguments event starts a call; ignore deltas. Require current response ID and bounded fields; dedupe call ID before pending-count changes. Unknown tools/invalid arguments produce fixed safe outputs via bridge.
- Wait for parent response.done AND playback fully drained (s.responseID == "") AND all accepted pending calls complete; send `conversation.item.create` with item {type:"function_call_output",call_id:...,output:JSONstring} for each result, then one `response.create`. Mark awaitingResponse until created. This prevents active_response errors and overlapping audio. A tool parent may produce no audio.
- Bridge success output flat {ok:true,verified:true,volume:60,brightness:80}; failure {ok:false,verified:false,error:"tag"}.
- Append fixed instructions: use tools for control, relative changes read status first, never claim verified success on false/unverified result; screen off means brightness 0. With no tools explicitly explain controls unavailable. Do not mutate shared cfg.Qwen while building per-session config.

Pending tool calls/results are generation-correlated. On interrupt/reconnect cancel old bridge generation, clear pending results/IDs, ignore late completions; don't send canceled-generation outputs to new upstream. Include outstanding tool work in interruption guard/listen.start checks. Hardware actions already sent cannot be undone. Preserve current audio-context rebuild and replay behavior. Max 8 tool calls per user transcription; fail safely if exceeded rather than unbounded model loops. Bound IDs/names/arguments strings (e.g. <=256/128/4096 bytes) and lifetime deduplication state appropriately with max session duration.

Required meaningful integration cases (use real httptest websocket peers and JSON wire messages): happy initialize/list -> session.tools -> model tool event -> setter ACK -> readback -> function output -> parent finished/drained -> response.create -> audio; failure/mismatch and timeout don't claim success; duplicate complete event does not execute twice or stall; abort before RPC reply drops stale generation and delayed replies do not resume; discovery timeout/failure still voice usable; parent response with audio must drain before continuation. Existing no-MCP interruption tests must stay green.

TDD: write behavioral regression first, capture actual red output, implement, self-review, run go test -race ./..., go vet ./..., go build -o bin/gateway ./cmd/gateway and go build -o bin/smoke ./cmd/smoke. Report any platform linker warnings separately (known Mac LC_DYSYMTAB pre-existing); use meaningful assertions, no speculative features.

Report all evidence to .superpowers/sdd/task-3-report.md; return only DONE/DONE_WITH_CONCERNS/BLOCKED, one-line test summary and concerns.

Additional concrete protocol risk to handle: playback.go currently sends tts.stop even for a completed response with no audio/TTS start. A tool-only parent must complete internally without sending a phantom stop that makes firmware resume/listen or makes smoke clients conclude response ended. Task 2 will be reviewed before you start. You may make the minimal playback.go change needed for this no-audio-parent behavior, with a real WS integration assertion that no TTS stop occurs before actual continuation playback. Keep existing scheduler fix intact. This is justified by the approved requirement to keep tools and voice synchronized.

Task 2 review required separate last-actual-frame end deadline so stop is never before last actual send +60ms even when anchored cadence has tick lateness. Read final playback.go/session.go fields and preserve that fix. When resetting a response, clear that actual-audio timestamp as appropriate; a no-audio tool parent must not wait on a previous reply's timestamp or emit a phantom stop.

Named integration risks identified during implementation:
- Function call IDs belong to an upstream conversation, not the whole physical-device lifetime. A fresh generation after abort may reuse an old upstream CallID; qualify dedupe/bridge identity by generation (mapping back on function_call_output), while keeping a global bounded count. Add abort -> fresh generation same CallID regression.
- Official v2.5.2 application.cc: tts.stop moves Speaking to Listening; StartListeningAudio() sends listen.start automatically after playback drains. A tool parent with audio must not cause this normal device resumption to cancel tool continuation. Test firmware-like automatic listen.start following every actual stop. If retaining a TTS stream across parent+continuation, retain Opus encoder/decoder continuity and send only the final stop; reset codec exactly when firmware resets at a new TTS start. Preserve ordinary explicit abort/listen interruption behavior.
