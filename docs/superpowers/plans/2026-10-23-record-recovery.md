# 文字记录、恢复与断点验证实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax for tracking.

**Goal:** 让网关保存有界文字与原任务，安全恢复到新会话；同步用独立真值比较断点候选，只有定位可信才自动接续。

**Architecture:** 单进程异步文件存储按已鉴权设备隔离，原子快照保存文字和任务，不保存音频。设备连接内保持现有单写者状态机；恢复快照经过裁剪和来源校验，再经现有模型 ACK 门控。断点实验独立运行，生产未知断点继续询问。

**Tech Stack:** Go 标准库、现有 WebSocket/Qwen/Opus；离线 Python 模型工具。首阶段无数据库服务或向量库。

## Global Constraints

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

## File structure

- `internal/recordstore/store.go`, `store_test.go`: 队列、原子文件、保留期限、权限、配额、锁和清空。
- `internal/gateway/records.go`, `records_test.go`: 设备范围、文字快照、恢复、能力事实、异步回执和删除。
- `internal/gateway/{server,session,history,session_tools,generation_sync,playback_progress}.go`: 最小接线。
- `cmd/gateway/main.go`, `.env.example`, `.gitignore`, `README.md`: 配置与操作说明。
- `tools/voice-audit/` 与 `docs/verification/`: 可重复离线验证和实际结果；私有实验脚本/素材放 `.superpowers/record-recovery/`。

### Task 1: 有界异步本地快照存储

**Files:** create `internal/recordstore/store.go`, `internal/recordstore/store_test.go`.

**Interfaces:**
```go
type Options struct { Directory string; Retention time.Duration }
type Snapshot struct {
    Version int `json:"version"`
    UpdatedAt time.Time `json:"updated_at"`
    Data json.RawMessage `json:"data"`
}
func Open(Options) (*Store, error)
func (*Store) Save(device string, snapshot Snapshot) (<-chan error, error)
func (*Store) Load(context.Context, string) (Snapshot, error)
func (*Store) Clear(context.Context, string) error
func (*Store) Close(context.Context) error
```
`Save` 只做有界复制/入队，即时返回；回执才代表持久化结果。`Load/Clear` 排在同一 FIFO，支持 context 取消，返回标准 `ErrNotFound`、`ErrBusy` 等。队列32，单文件不超过1MiB，最多8设备；文件名使用设备 SHA256，不接受路径作为文件名。目录0700、文件0600、原子临时文件写入+fsync+rename+目录fsync，独占目录锁，第二个实例必须失败。打开与读取时执行过期清理；损坏数据返回错误，不冒充空或已保存。过期按快照 UpdatedAt，应用层还逐条裁剪正文/任务时间。

- [ ] 写失败测试：Save→Load FIFO、Close→重开恢复；不同设备隔离；Clear排除先前排队写入；过期、损坏、超大、满队列、超8设备、权限与重复打开。
```go
receipt, err := st.Save("device-a", Snapshot{Version:1, UpdatedAt:time.Now(), Data:json.RawMessage(`{"task":"count to fifty"}`)})
if err != nil { t.Fatal(err) }
if err = <-receipt; err != nil { t.Fatal(err) }
if _, err = st.Load(context.Background(), "device-b"); !errors.Is(err, ErrNotFound) { t.Fatal(err) }
```
- [ ] 运行 `go test ./internal/recordstore` 证明缺失实现时失败。
- [ ] 实现上述接口，只有 worker 操作磁盘，拒绝非法/过大数据。Close停止接受后有界等待排空，超时如实报错。
- [ ] 运行 `go test -race -count=1 ./internal/recordstore`，记录实际命令与结果。
- [ ] 自查并仅提交本任务文件，报告路径 `.superpowers/record-recovery/task-1-report.md`。

### Task 2: 网关文字记录与安全恢复

**Files:** create `internal/gateway/records.go`, `records_test.go`; modify gateway接线、`cmd/gateway/main.go`, `.env.example`, `.gitignore`, `README.md`.

**Consumes:** Task 1精确接口。store 可由 Server 管理，测试可注入临时目录。
**Produces:** 设备鉴权后获取对应快照，持久化状态变化后异步处理成功/失败；模型可读的真实记录能力；显式清空操作。

- [ ] 先写失败回归：两个新 session/进程重开恢复原请求、终点与近期文字；中断稿不进入已播上下文；旧采样只作历史，当前进度unknown；新唤醒不自动恢复；放弃/替换持久化后不复活；清空删除正文/相关任务；写失败/队列满不谎称成功；同设备并发连接不能覆盖新状态；不同设备无泄漏。
```go
// 断言恢复保住原任务，但不能声称知道播放到哪个数字。
if got["original_request"] != "从一完整数到五十" || got["text_position"] != "unknown" { t.Fatalf("unsafe restore: %v", got) }
```
- [ ] 增加 `ROBOT_RECORD_DIR`（空则明确关闭）、`ROBOT_RECORD_RETENTION`（默认168h）。开启失败时服务拒绝启动，不悄悄降级。最多存128条有时间/身份的文字，每条512字符；模型只取最新12条，正文和任务逐条按期限裁剪，排除唤醒输入。助手生成稿附响应/播放身份、交付状态；保存有效数字采样证据，恢复不能用于新块。每次发生真正状态变化才提交，不按10ms tick重复写。
- [ ] device ID必须来自鉴权头规范化MAC；同设备同一时间最多一个连接，避免旧连接覆写。加载有界超时，出错时能力事实明确恢复失败；恢复任务ID使用新的会话命名空间，保留原始来源和用户选起点的来源标签。
- [ ] 将 `recording_capabilities` 作为明确数据加入当前模型快照/ACK。至少表达临时上下文有无、文字持久化开启/当前待写/成功/失败、保留期限、音频留存false、恢复错误；事实变化必须进入下一次生成前ACK。不得把数据当系统命令执行。
- [ ] 提供明确可操作的手动清空方式：优先复用本地conversation工具 `conversation_clear_records`；仅在本轮真实转写明确要求清空本设备对话记录时允许执行。异步执行，收到持久化删除回执后才回答成功；同步清理临时history、暂停任务和恢复数据，后续快照不得复活旧内容。若采用运维命令也必须互斥、清理当前服务缓存并在文档说明，不能直接删文件后让旧内存重新写回。
- [ ] 保持现有中断/工具/归属/固定澄清/ACK顺序机制，通过 `go test -race -count=1 ./...` 和 `go vet ./...`。测试报告列失败注入与实际限制。
- [ ] 提交本任务源码/配置/操作文档，报告 `.superpowers/record-recovery/task-2-report.md`。

### Task 3: 同期离线断点方法对照

**Files:** ignored `.superpowers/record-recovery/offline/`; final tracked report `docs/verification/2026-10-23-record-recovery-alignment.md`；若新增可复用纯验证器则放 `tools/voice-audit/` 并配测试。

**Consumes:** 现有真实机器人输出素材与Qwen3-ForcedAligner候选。不得采用户麦克风。
**Produces:** A强制对齐 vs B输出ASR的真实实测与是否满足启用条件，未满足即明确保持unknown。

- [ ] 从官方来源确认识别器与时间戳机制、许可证和运行方式。ASR若仍用同一个强制对齐器生成时间戳，不可称独立边界真值。
- [ ] 先构造有独立真值的可重复样本：按可独立听辨单位生成音频片段并拼接，片段边界由构造给出，明确这是拼接条件；另跑现有连续原生输出但未经人工标注不能宣称边界准确。覆盖数数、普通句、数字/专有词、很早取消、词/句中间截断、再次恢复截断、静音/正文领先等。裁剪位置从独立素材坐标派生，不能从被评估模型的时间戳反推真值。
- [ ] 实际跑两种方法，保存版本/素材SHA/参数/资源与结果；不能用纯单测替代模型试验。识别错误、空结果、时间戳幻觉分别记录。无有效费用依据时不编造API费用。
- [ ] 测量正确最后单位、漏读/重读、准备耗时/内存。缺设备播放坐标标定时，单独注明不能上线自动接续。
- [ ] 汇报 `.superpowers/record-recovery/task-3-report.md`，公开报告只保留可公开文字和数据，素材/权重不进Git。

### Task 4: 集成、真实模型验证与发布

**Files:** verification docs, ignored release scripts/evidence; minimal source fixes if review requires.

- [ ] 每个编码任务以任务brief+报告+完整range diff进行独立规格/质量审查，重要问题用新fixer修复并复审；最终做整个8f9b312..HEAD复核。
- [ ] 同一提交跑Mac和Linux Go测试/race/vet/build，已有Python验证器跑对应测试；验证异步写不阻塞interrupt与队列界限。
- [ ] 在隔离目录/端口，使用真实千问测试：原任务→中断→新设备会话/进程重启→查询原任务；明确继续仍未知时询问；用户指定起点继续；显式放弃和清空后重开不恢复。模拟播放回报不当作实机听感。
- [ ] 如定位或坐标条件不满足，不启用自动准确续读，报告各项原因和下一项实验。
- [ ] 发布前扫描提交/源码包中的凭据，空闲连续两次确认后部署已验证binary，配置受保护记录目录，保留07c6549回滚。若用户设备仍连接，则等待空闲，不强断。
- [ ] 验证health、进程binary SHA、目录权限/恢复状态；保存公开证据、更新计划状态并push main。需要现场声音验收时给用户一轮具体步骤，不虚报现场通过。
