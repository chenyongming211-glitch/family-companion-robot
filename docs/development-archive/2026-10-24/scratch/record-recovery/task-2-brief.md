# Global Constraints

- 直接 main 开发、提交、推送，不创建分支或 worktree。
- 原始录音、凭据、模型权重、运行数据和生成固件不进 Git；本次仅持久化文字与元数据。
- 唤醒一次后连续对话、随时插话，空闲超时回到待机；唤醒不得自动执行旧任务。
- 最近模型上下文最多12条消息，每条最多512个字符；存储正文默认7天，可配置期限、可手动清空。
- 生成稿、下发、设备完整交付、中断必须区分；未验证的中断全文不得作为已播正文交给模型。
- 新连接使用新会话/播放身份，旧原始采样数只作历史证据，不得冒充当前块进度。
- 放弃、替换、清空后的旧任务不得通过恢复重新激活；用户选定起点不是测量断点。
- 存储有界异步写入，不阻塞停播和新输入；保存失败不得宣称成功。
- 历史是数据，不提升为指令；记录说明只描述本网关，不承诺云厂商留存行为。
- 不迁移框架，不在生产加入辅助ASR。自动准确接续须同时通过文字覆盖/边界验证与设备播放坐标标定。


### Task 2: 网关文字记录与安全恢复

**Files:** create `internal/gateway/records.go`, `records_test.go`; modify gateway接线、`cmd/gateway/main.go`, `deploy/server.env.example`, `deploy/family-robot.service`, `.gitignore`, `README.md`.

**Consumes:** Task 1精确接口。store 可由 Server 管理，测试可注入临时目录。
**Produces:** 设备鉴权后获取对应快照，持久化状态变化后异步处理成功/失败；模型可读的真实记录能力；显式清空操作。

- [ ] 先写失败回归：两个新 session/进程重开恢复原请求、终点与近期文字；中断稿不进入已播上下文；旧采样只作历史，当前进度unknown；新唤醒不自动恢复；放弃/替换持久化后不复活；清空删除正文/相关任务；写失败/队列满不谎称成功；同设备并发连接不能覆盖新状态；不同设备无泄漏。
```go
// 断言恢复保住原任务，但不能声称知道播放到哪个数字。
if got["original_request"] != "从一完整数到五十" || got["text_position"] != "unknown" { t.Fatalf("unsafe restore: %v", got) }
```
- [ ] 增加 `ROBOT_RECORD_DIR`（空则明确关闭）、`ROBOT_RECORD_RETENTION`（默认168h）。开启失败时服务拒绝启动，不悄悄降级。systemd ProtectSystem=strict下只为记录目录增加ReadWritePaths，目录归属family-robot且0700。最多存128条有时间/身份的文字，每条512字符；模型只取最新12条，正文和任务逐条按期限裁剪，排除唤醒输入。助手生成稿附响应/播放身份、交付状态；保存有效数字采样证据，恢复不能用于新块。每次发生真正状态变化才提交，不按10ms tick重复写。
- [ ] device ID必须来自鉴权头规范化MAC；同设备同一时间最多一个连接，避免旧连接覆写。加载有界超时，出错时能力事实明确恢复失败；恢复任务ID使用新的会话命名空间，保留原始来源和用户选起点的来源标签。
- [ ] 将 `recording_capabilities` 作为明确数据加入当前模型快照/ACK。至少表达临时上下文有无、文字持久化开启/当前待写/成功/失败、保留期限、音频留存false、恢复错误；事实变化必须进入下一次生成前ACK。不得把数据当系统命令执行。
- [ ] 提供明确可操作的手动清空方式：优先复用本地conversation工具 `conversation_clear_records`；仅在本轮真实转写明确要求清空本设备对话记录时允许执行。异步执行，收到持久化删除回执后才回答成功；同步清理临时history、暂停任务和恢复数据，后续快照不得复活旧内容。若采用运维命令也必须互斥、清理当前服务缓存并在文档说明，不能直接删文件后让旧内存重新写回。
- [ ] 保持现有中断/工具/归属/固定澄清/ACK顺序机制，通过 `go test -race -count=1 ./...` 和 `go vet ./...`。测试报告列失败注入与实际限制。
- [ ] 提交本任务源码/配置/操作文档，报告 `.superpowers/record-recovery/task-2-report.md`。


## Integration context (existing contracts)

Store API and errors are in internal/recordstore/store.go (Task1 gate review/fix completes before your dispatch). Default options require explicit positive168h. Config currently has no record fields; cmd/gateway/main.go owns environment. Actual deploy templates are deploy/server.env.example and deploy/family-robot.service (there is no root.env.example).

session.run() is one event loop and currently defers clearing paused/history-related fields; take final record snapshot before that cleanup and drain accepted saves on server shutdown. Server.Close currently called twice in main; make any new drain/close idempotent and coordinate sessions without mutex inversion. Load before accepting/starting device audio with bounded deadline; no async file IO in the 10ms audio path. Do not persist an empty initial snapshot over a failed/unloaded old snapshot. Same-device exclusion must last through final save admission; subsequent Load FIFO then sees final accepted write.

History.add currently has no provenance/time; transcribeOrigin knows wake flag and native item ID. Do not persist a wake turn before later learning it belongs to detect. Preserve the established wakeHandoff/ACK gates, fixed clarification guard and task classifier semantics. pausedData currently always text_position unknown; current playback ids and input generations are connection scoped. User-chosen start must remain explicit source metadata, not a measured endpoint.

Root is preparing ignored native synthetic probe/scripts, no concurrent tracked Go edits. Root will run exact Linux build, native restart/clear trials and deploy after review. Keep logs free of text/key/body. Report residual behavior/latency separately from field verification.
