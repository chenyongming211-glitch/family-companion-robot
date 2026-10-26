# Go Realtime 多设备准入实施计划

> For agentic workers: use superpowers:subagent-driven-development with TDD and spec-then-quality review. User authorizes direct main, cloud execution and the service→firmware→dual-device sequence; do not create branches/worktrees or ask again for execution mode.

**Goal:** 当前Go核心接入两台独立鉴权终端，分别管理会话/播放/记录，并保留原设备和Mac行为。

**Architecture:** 共用受保护凭据表仅供当前设备鉴权与既有OTA引导读取；voiceidentity继续负责稳定ID。Server采用设备范围租约及有界总容量，Session状态继续独占。

**Tech Stack:** Go1.25、现有WebSocket/Opus/voiceidentity、云端保护配置与systemd。新固件在下一板级计划实施，不与服务端文件交叉编辑。

## 1. 共用凭据和OTA契约

**Files:** 新增`internal/deviceauth/registry.go`及测试；修改`internal/realtimelive/xiaozhi_entry.go`、`internal/gateway/server.go`、`cmd/gateway/main.go`、`deploy/server.env.example`与对应测试。

- [x] 先写云端RED：两个受保护凭据按MAC各取自己的token；交叉token/未知MAC拒绝；重复MAC、权限过宽、非法JSON/token、过大配置拒绝；原env单设备保持兼容。
- [x] 实现只读schema1加载，最多64设备、文件最多1MiB；条目`mac`/`token`，canonical MAC，token长度32..512。错误只输出固定原因，配置快照不可被外部修改。
- [x] `ROBOT_DEVICE_REGISTRY`为空保持现有单设备逻辑；配置时校验legacy主设备原凭据存在且一致。当前Realtime入口按查得凭据BindXiaozhi，既有8000只扩展OTA凭据下发，旧语音路由不扩大范围。
- [x] 验证OTA保留bootstrap路径/请求大小与版本检查，第二MAC只得到自己的token及当前WS URL；未注册者403，不输出跨板更新URL。

## 2. 设备范围会话租约与票据

**Files:** `internal/realtimelive/server.go`、新`admission.go`（如有利于单一职责）、`config.go`、`cmd/realtime-output-live/main.go`、现有server/ownership/xiaozhi/review测试及新并发测试；`deploy/family-robot-realtime-output-go.service`。

- [x] 先在旧代码写并执行RED：两已登记设备能同时接入且票据互不覆盖；A断开/取消不关闭B；同设备重复和第三个超限拒绝，已有会话继续；失败升级回收租约。
- [x] 实现每设备单活、总容量兼容默认1/部署2/最大8。租约使用已鉴权device_id，匿名旧入口单独兼容key；释放匹配本租约身份，禁止旧清理删新会话。
- [x] 按设备替换自己的待用票据而非清空全表；120秒/一次性/Origin与消息限制保留。清理过期项并限制待用票据64，跨设备票据不能互用。
- [x] 健康信息增加active_sessions/max_sessions/capacity_full，busy仍表示active>0。CloseActive关闭所有活跃连接并有界等待，不持锁进行网络阻塞操作。
- [x] 继续复用Session/Ownership/voiceidentity；实际WebSocket路由测试不同上下文/事件/文件标记，重连稳定device_id和新session_id；旧回报不落入新会话。
- [x] 检查Config/Tools/Search/Product与原生模型连接的共享可变状态；只修为并发所必需的隔离，不改身份、搜索、声音、提示或排空语义。

## 3. 云端验证与两阶段复核

- [x] 在独立`/tmp/family-multidevice-20261027/server-impl`运行新增RED→GREEN，再执行`go test -race ./...`、`go vet ./...`、`go build ./...`，保存所有实际退出码和日志。需要的Python参考仅为冻结差异测试，不启动Python产品服务。
- [x] 先规格、后代码质量独立复核；修复确定问题，重复受影响验证。核对Mac与M5旧协议测试、记录/取消与已播上下文不退化。
- [x] 根代理在云端隔离入口用两个独立合成客户端连接真实模型，验证两个不同device/session及取消/结束一台不影响另一台；保存结构化结果，合成回报不当作手表声学通过。

## 4. 部署与原手表验证

- [x] main提交推送，核对远端。服务器生成第二台独立随机token，凭据仅保存在服务器私有注册表；原主设备token保持。
- [x] 备份当前二进制/unit/env/身份存储到服务器私有回退目录；确认18040及8000没有活跃会话，再部署有界并发2和注册表配置；需要等待用户正在进行的通话自然结束时先继续独立工作。
- [x] 确认18040/18041与OTA8000启动身份、健康计数、两MAC准入与交叉凭据拒绝；不下载provider key到Mac，不输出token或bootstrap URL。
- [ ] 原M5 e61f079实机基本问答、播放及插话验证；可用Mac＋原手表验证独立连接，现场声学干扰与业务会话隔离分别记录。
- [ ] 写回设计/计划/交接及本轮验证，提交推送。然后进入Waveshare板级计划，完整备份和专用构建后安装，继续用户已授权的后续阶段。

2026-10-27补充：服务端6dc191a已部署并完成静默健康/鉴权/OTA核验，见[验证报告](../../verification/2026-10-27-realtime-multi-device.md)。用户就寝要求无声，不连接/重启/刷写实体设备；原M5回归及双实机验收保持待办，先进行不依赖实机的Waveshare代码与构建。
