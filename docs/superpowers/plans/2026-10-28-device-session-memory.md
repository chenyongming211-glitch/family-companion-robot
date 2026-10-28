# 同设备跨会话记忆实施计划

> For agentic workers: 使用superpowers:executing-plans在当前会话逐项执行，TDD；用户已批准连续开发与main工作流，不建分支/worktree，不重复询问执行方式。运行验证全部在云端。

**Goal:** 重新唤醒同一设备时恢复近期已播对话，并可清除而不复活。

**Architecture:** recordstore复用；Server拥有一个存储worker，Session拥有恢复内容、保存串行锁和清除边界。有界原话及摘录加入现有JSON上下文，原语音模型自然回答，不增加摘要模型。

**Tech Stack:** Go、现有recordstore/realtimestate/voiceidentity；无新依赖。

## 1. 云端RED

- [x] 新增internal/realtimelive/memory_test.go：用当前服务创建真实session.json、已播Turn和共享recordstore，验证A重连恢复、B隔离、未播稿排除、部分播放证据、条目时间限制与恢复资料上限。
- [x] 增加清除测试：完整命令白名单、否定/引文/其他目标不删除，清除后当前上下文和重连均无旧记录；迟到播放回报和已准备的下一轮背景不复活；保存失败不得伪报成功。
- [x] 归档当前main到/tmp/realtime-memory-20261028/source，先同步测试，执行 `/opt/family-robot/toolchains/go1.27.3/bin/go test ./internal/realtimelive -run Memory -count=1` 并保留失败输出。

## 2. 实现与接线

- [x] 新增memory.go：定义带来源的memoryTurn/memoryData；实现截取、过期、覆写合并、原话摘录和JSON合并。`attachMemory(*Session)`只使用服务端归属；`loadMemory()`、`checkpointMemory()`及`routeMemory(*liveTurn,string)`实现读取、保存和明确清除。
- [x] 修改server.go/xiaozhi_entry.go：启用配置下初始化recordstore，Mac与小智Session均接入，关闭先等待会话结束再关闭存储。
- [x] 修改session.go：Run读取/后台保存与最终保存，record成功后非阻塞通知；settle在清除边界过滤并加入此前会话资料；reply在设备控制前处理清除，成功后重建无旧历史的prompt。
- [x] 修改config.go/main.go：加入MemoryEnabled与默认启用的--memory开关；保持原unit参数可用，关闭开关可回到原行为。
- [x] 修改identity.go：仅启用设备说明可使用近期跨会话记录，不宣称可查询所有档案；清除按本轮回执回答，缺失记录自然追问。禁用时保持Python差异基线。

## 3. GREEN及完整验证

- [x] 针对性测试GREEN，再执行 `go test -race -json ./... -count=1`、`go vet ./...`、`go build ./...`；保存退出码/测试计数。最终822 pass/2既有skip/0 fail，vet/build退出0；保留旧gateway一次失败和独立30次/后续全仓通过，不修改该模块。
- [x] 自查规格覆盖、锁顺序、取消、清除并发、期限不续期、无凭据/声音/生成产物入Git；云端真实模型＋模拟终端验证跨会话事实回忆、隔离与清除，再回归当前设备控制、联网搜索、故事插话排空。

## 4. 发布

- [x] 更新验证/交接/能力说明，main提交并推送核对；云端build=a669f80，确认active_sessions=0后备份并部署，ready/device_memory/memory_ready均true，二进制SHA与产物相同，配置/身份文件哈希不变。
- [x] 报告自动验证和真人验收边界；18040/18041已开启，用户自行唤醒测试。本轮不操控实体设备或发声，实体听感与真人语音记忆验收仍待用户确认。
