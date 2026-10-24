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

