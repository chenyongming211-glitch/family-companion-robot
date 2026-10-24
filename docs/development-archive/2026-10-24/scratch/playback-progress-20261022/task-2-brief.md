### Task 2: Go 播放账本与暂停任务

**Files:** `internal/protocol/protocol.go`、新播放回报解析/测试；`internal/gateway` 新播放账本与暂停任务模块/测试，接入 `session.go`、`playback.go`、工具与历史。固件文件不由此任务修改。

**Consumes:** 任务1固定线协议与现有 transport_sent/retired response 规则。
**Produces:** 最多16播放记录、一份暂停任务；本地上下文工具；未获文字映射时明确 unknown；旧固件兼容。

- [ ] 先写 WebSocket 行为测试：协商前不信任回报；正确代号/会话的有效进度；重复/倒退/越界/漏字段/小数/旧代次；正常与中断最终回报；工具共享TTS块；完成或断开清理。
- [ ] 为实际 TTS 块分配 uint32 递增代号，记录已成功发送的24k采样数。只在双方协商后携带元数据；保持旧二进制封装。最终有效且完整的回报才可升级相应历史为播放确认；普通 transport_sent 不自动升级。
- [ ] 在回复生成开始时固定原用户请求；中断时创建暂停快照，回报可在模型重连后继续完善旧快照。旁支问答及其再次中断保留原任务，不用生成稿尾部填入已播文字。
- [ ] 提供本地 `conversation_get_paused_task` 只读工具及 `conversation_update_paused_task`（discard/replace）工具；修改必须携带匹配 task_id，replace 采用当前实际用户转写。接入既有函数调用编排，与设备工具独立可用，不增加外部服务。
- [ ] 无词级标注时返回 `text_position=unknown` 和已知/未知的采样位置，工具不得声称已能精确继续。原任务请求最多512字，音频不新增留存，日志不写内容；连接生命周期清理。
- [ ] 先观察失败再最小实现；跑针对性及完整 `go test -race ./...`、`go vet ./...`，Linux隔离再测/构建。独立评审后提交。

