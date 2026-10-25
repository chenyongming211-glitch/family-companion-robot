# 终端与会话归属实施计划

> 在main顺序实施，云端执行Go运行测试/编译/部署，Mac只编辑Git及运行测试客户端。用户已经批准方案并要求开始，无需再次询问执行方式。

**目标：**稳定终端身份、每连接独立会话、可信记录归属，不改变语音核心或接入手表。

**设计：**[已确认范围](../specs/2026-10-25-device-session-ownership-design.md)。

## 1. 云端红灯测试

- 新建`internal/voiceidentity/store_test.go`：登记只接受当前两类Mac客户端，服务器生成ID；重开存储仍能认证；错误凭据/未知ID拒绝；磁盘损坏/写失败不能清空原登记。
- 新建`internal/realtimelive/ownership_test.go`：真实HTTP/WS受控入口，登记两终端，验证票据绑定、重复连接session变化、伪造拒绝、旧客户端未归属，以及事件/intent不可覆盖归属。
- 将源码快照送入云端私有验证目录，运行`go test ./internal/voiceidentity ./internal/realtimelive`，保留预期失败证据。

## 2. 实现最小服务器基础

- `internal/voiceidentity/store.go`：`Open(path)`、`Enroll(clientType)`、`Authenticate(deviceID, secret)`，私有原子文件与散列凭据。
- `internal/realtimelive/ownership.go`：不可变连接归属、session.json生命周期、读取验证及查询。
- `server.go`、`audit.go`、`session.go`：登记/认证、票据绑定、新会话编号；所有事件和intent附归属；ready和控制带session_id，receipt/stop校验；结束时间和原因。
- `cmd/realtime-output-live/main.go`：启动时打开登记存储，失败拒绝启动。
- `cmd/realtime-records/main.go`：服务器侧按终端/会话查询，旧记录明确未归属。
- 云端重复上述针对性测试到通过，再跑`tools/realtime-output-live/go-tests.command`。

## 3. Mac终端适配

- 新建`tools/realtime-output-live/DeviceIdentity.swift`：受限profile，独占创建私有身份文件；不将凭据输出或送入模型。
- `LiveClient.swift`：Go先登记/读取身份，以终端凭据取得票据，验证session归属并随receipt/stop发送；Python保持旧协议。
- `main.swift`、`build.command`、`build-soak.command`：加入身份源码与profile参数；测试原生与fixture使用独立身份。
- 编译客户端，验证凭据文件权限、重读稳定、错误文件不自动覆盖。不得启动本地语音后端。

## 4. 部署与实际验证

- 核查云端工作树与18040空闲；备份旧二进制，上传已验证源码，在云端编译并切换18040。
- 两逻辑Mac测试终端依次进行真实模型/CoreAudio对话；同终端重连仍同device_id，新session_id；至少一次插话排空和新回答。
- 服务器CLI核对转写、生成、已播、intent全链归属，无跨终端历史，既有身份/搜索/流式和错误回归通过。不得把合成输入称为真人验收。
- 写验证报告，更新DEVELOPMENT_HANDOFF和客户端README；main提交推送并核对远端。保留现有测试窗口。
