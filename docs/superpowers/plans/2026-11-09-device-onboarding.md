# 扫码添加设备 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 接通微信小程序页面、一次性认领/独立设备确认接口和ESP32-S3官方安全蓝牙配网，设备连接自有服务，保留真实绑定与历史边界。

**Architecture:** 复用现有Go管理服务和家庭账号。管理私有配置作为工厂登记、认领任务和绑定的单文件权威源，跨进程文件锁＋原子写入；每请求读取有效快照，可信供应CLI无需重启服务。现有语音入口保留旧白名单，新工厂设备通过受限回环管理认证接口确认已激活身份。小程序使用原生TypeScript/WXML/WXSS，官方network_provisioning Security 2加密BLE；不另起注册微服务。

**Tech Stack:** Go标准库、微信原生小程序、TypeScript、纯JavaScript标准密码库、ESP-IDF6.1/network_provisioning、现有Caddy。

## Global Constraints

- Develop, commit and push directly on `main`, as explicitly requested by the user.
- Do not create feature branches or additional worktrees unless the user changes this instruction.
- Supported product firmware is ESP32-S3 only (currently Waveshare and M5).
- Keep device backups, credentials, raw audio and generated firmware artifacts out of Git.
- Install runtime dependencies, execute runtime tests and deploy services on the cloud server. The Mac is for code/Git and the later test client; do not start a local voice service or copy server credentials onto it.
- 用户已确认小程序＋蓝牙入口，域名robot.songguoxue.com；测试号AppID只作测试配置，不包含AppSecret或上传密钥。
- 已绑定微雪的身份、凭据、起点2026-11-09 10:05:08（北京时间）及原数据保持。旧正文不导入，新设备绑定使用实际服务端时间。
- 小程序首期只做添加设备，复用已有账号；不增加微信自动注册、手机号授权、转让、摄像头或聊天小程序。
- 没有加密/持有证明、真实设备云端认证回执及原子绑定，不能显示添加成功。向设备写入Wi-Fi、取得IP、模拟确认均不算现场通过。
- 现用设备不自动刷机、开麦或配网。先完成隔离验证与候选构建，再记录真实手机/设备验收为待验证。

## Shared Contracts

所有任务先读`.superpowers/sdd/onboarding-20261109/contracts.md`。该文件固定JSON/协议接口与目录所有权；如接口有必须调整之处先向协调者反馈，再同步文件和相关任务，不默默各写一套。

### Task 1: Go认领权威源、家庭与设备接口

**Files:** 修改`internal/management/{config,server,auth}.go`与`cmd/management/main.go`；新增`internal/management/onboarding*.go`及对应测试、`cmd/management/provision*.go`及测试。保留现有读模型文件的职责。

**Interfaces:** 供应CLI产生云端私有工厂包；新家庭onboarding DTO、设备activation/bootstrap、回环device-auth见Shared Contracts。`Options.ConfigPath`启用可持久化快照，`Options.PublicOrigin`明确公网HTTPS源，CLI提供同名参数。未提供配置路径时旧只读测试保持兼容。

- [ ] 写实际HTTP/持久化测试：未授权403、待确认不授予设备、新设备独立确认后才出现绑定与设备授权；两个家庭并发只能一个成功，重放/过期/错误设备/取消/写失败均拒绝，重启后结果可查。现有绑定与内容起点逐字段保持。
- [ ] 云端Go环境先验证RED，例如新测试中的`POST /api/family/onboarding`应因尚无接口而失败，不把编译错误当行为RED。

```go
// 测试主线：登录A，创建任务，确认前ListDevices仍空；错误设备确认失败；
// 正确设备独立Bearer认证确认；重新载入配置只有一条当前起点绑定；
// A获正文权限，B读状态/设备均被拒绝。所有身份和口令仅为临时合成数据。
if got := persisted.Bindings[len(persisted.Bindings)-1].From; !got.Equal(now) {
    t.Fatalf("binding time must be actual server time: %v", got)
}
```

- [ ] 最小实现私有单文件事务：同一固定锁文件跨进程排他，锁内重新加载最新配置→检查权限/任务/设备→原子写完整新配置。读请求使用独立不可变快照，不修改并发共享Reader.Config。
- [ ] 加明确`CanAddDevices`默认false、供货命令默认不授予真实账号；工厂包O_EXCL/0600且只保存在云端。拒绝重复MAC/序列号、现有身份冲突、未知字段/重复键与过量任务。
- [ ] 实现公网代理边界、会话Cookie及登录限流，保留本机HTTP预览；新设备接口不接收家庭归属作为身份依据，回环认证不对公网开放。返回值不含工厂密钥、认领码或Wi-Fi口令。
- [ ] 云端完整`go test -race ./internal/management ./cmd/management`、`go vet`及构建一次；提交本任务文件并写报告，明确RED/GREEN、测试数与剩余现场条件。

### Task 2: 原生小程序向导和BLE传输

**Files:** 新建`web/miniprogram`的工程、页面、API、状态管理、BLE传输、构建与行为测试；Task 3独占`web/miniprogram/src/provisioning/security2.ts`和该协议测试/向量，双方先商定export。依赖和package-lock由Task 2管理，按Task 3请求添加纯JS标准密码库。

**Interfaces:** 使用Shared Contracts中的真实HTTPS接口、family Cookie和`Security2Session`。扫描scene原值只保存在内存；先创建任务取得序列号，再用scene作Security2证明，通过`family-claim`端点传递id/challenge，最后调用官方Wi-Fi配置端点。

- [ ] 先编写状态/API/蓝牙行为测试：未确认不可success、云端过期/401/网络失败可反馈、取消阻止旧回调覆盖新任务、后台/页面销毁清理密码与BLE、分包超时拒绝；云端Node运行RED。

```ts
assert.equal(state.phase, "waiting_cloud");
state = reduce(state, { type: "cloud_status", status: "waiting" });
assert.notEqual(state.phase, "success");
state = reduce(state, { type: "cloud_status", status: "confirmed", deviceId: "dev_" + "a".repeat(32) });
assert.equal(state.phase, "success");
```

- [ ] 实现登录/扫码与家庭选择、Wi-Fi列表/密码、进度与成功反馈；一个家庭自动选，昵称可选。原生简单页面，不引入跨端UI框架，不预填真实账号/设备凭据，不将Wi-Fi密码写storage或console。
- [ ] 实现wx.request Cookie处理、固定HTTPS源、客户端请求错误与有时限轮询；scan结果校验仅接受32位小写hex或明确的scene包装，不跟随二维码任意URL。
- [ ] 实现BLE发现、GATT端点发现、20字节起分包、响应拼接/超时和断开处理；仅安全会话完成后写Wi-Fi，不能明文fallback。实际协议endpoint及protobuf字段以Task 3官方互通证据为准。
- [ ] 云端完成Node行为测试、TypeScript和打包；编译产物不入Git；README说明测试号导入/预览方式、需要微信登录的边界及未完成实机项。提交并写报告。

### Task 3: Security 2互通与S3固件适配

**Files:** 新建`web/miniprogram/src/provisioning/security2.ts`及专项协议测试/向量；按现有结构新增`firmware/patches`与`firmware/tools`中的小范围官方配网适配、对应host检查和锁定依赖。不得改Task 1/2的其他所有权文件；所需依赖报Task 2添加。

**Interfaces:** Shared Contracts固定TS Security2Session与加密transport；固件新增官方自定义`family-claim`endpoint，接收id/challenge，不接收服务器URL或长期凭据。工厂包为Task 1格式，云端工具派生SRP盐/验证器到设备NVS候选，不入Git。

- [ ] 从固定ESP-IDF6.1与network_provisioning官方源码确认SRP参数、protobuf、AES-GCM nonce/counter与端点映射，锁定组件版本。先写官方交叉向量测试，在云端证明未实现时失败。
- [ ] 最小实现官方Security2客户端协议适配；密码算法用维护的标准库，随机数来自wx安全随机接口并可注入确定性测试值。认证失败、计数器错误、篡改均fail closed，无Security0/无PoP降级。
- [ ] 用官方Python/设备端实现交叉验证完整握手、请求/响应加密解密、篡改拒绝；非仅与自己写的server对照。保留命令、版本、输入类型与结果，向量只用公开合成值。
- [ ] 适配上游S3入口：新工厂身份启用官方BLE配网和180秒窗口；默认自有HTTPS bootstrap，证书校验；独立Bearer认证activation后才完成绑定并停配网，再取自有WSS。现用无工厂身份设备保持原链路，配置失败不能落到官方云。
- [ ] 云端S3候选构建与host检查；只验证支持的微雪/M5目标，不运行C3。记录实际构建目标/产物状态，私有产物留云端，不自动刷机或触碰麦克风。
- [ ] 提交本任务文件并写报告；遇到微信或官方协议确实不兼容，提供具体失败与已完成交叉证据，不假装实机可用。

### Task 4: 统一语音身份、HTTPS路由与隔离部署

**Files:** 新增`internal/realtimelive/onboarding_auth.go`与专项测试，修改`internal/realtimelive/xiaozhi_entry.go`/`cmd/realtime-output-live/main.go`的可选回环认证参数；修改`deploy/onboarding/Caddyfile`和管理部署文档；更新本计划、设计、交接及验证。

**Interfaces:** `DeviceConfig.OnboardingAuthURL`仅允许固定回环HTTP地址的`/api/internal/device-auth`，默认空。旧白名单已有MAC仍按原凭据认证；仅未知MAC尝试受限回环factory认证，确认后的稳定ID来自管理权威源。新设备不调用BindXiaozhi再次生成ID。

- [ ] 云端先写RED：未知MAC未激活/错key拒绝，认证服务断开fail closed、超时有界，已激活返回权威ID并在新记录归属中使用；已登记旧MAC错key不走fallback。正常测试不打开模型连接或生成语音。
- [ ] 接入可选factory认证客户端，调用固定URL、严格解析 bounded响应，timeout<=2秒，拒绝重定向与非回环/非固定path；只在新设备新连接读取，不改已有会话。
- [ ] 配置Caddy精确开放小程序所需family login/logout/me/families、onboarding、设备activation/bootstrap及设备WSS；不开放admin、内部device-auth、源记录、Mac登记/配对。代理显式覆盖X-Forwarded-For/Proto，不能信客户端伪造来源。
- [ ] 隔离配置/合成设备验证跨账号权限、原子绑定、重放/过期/断连/重启、Cookie与来源、HTTPS/WS握手与路径限制；完整Go专项race/vet、前端构建、S3候选证据及源码复审通过后发布已验证部分。
- [ ] 迁移真实配置只显式授予现有owner在自有家庭的添加权限，保留其原绑定/正文权限；不生成测试工厂设备进真实配置。管理服务可独立发布；语音factory开关根据兼容/发布证据安排，记录实装版本，不整包未经验证的main替换正式语音。
- [ ] 写最终验证，准确分开已实现、已部署、自动验证与真实手机/设备待验证；提交推送main，不要求用户再确认既定入口。
