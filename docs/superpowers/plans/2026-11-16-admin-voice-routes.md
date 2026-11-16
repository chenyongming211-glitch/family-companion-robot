# 长期 A/B/C 链路选择实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. 用户已确认立即结束旧对话，以及三链路为长期产品能力，沿用已批准方案持续执行。

**Goal:** 管理端每设备提供长期 A/B/C 选择，保存后取消旧会话，新连接使用所选链路，显示保存结果及真实当前链路。

**Architecture:** 管理 ConfigStore 持有选择和权限，语音持有实际连接状态与生成适配，既有Unix控制通道进行带权限/版本守卫的查询和取消。三个生成方式复用公共业务前置、设备鉴权、记忆、Opus、回执及取消，命令层组合回调；不增加生产服务或固件改动。

**Tech Stack:** 现有Go、gorilla/WebSocket、Vue3/TypeScript、标准库和现有voicecompare，无新依赖。

## Global Constraints

- 直接main开发、提交推送，不建分支或worktree；仅支持微雪ESP32-S3 2.16。
- 运行时测试/依赖/构建/服务仅云端，Mac仅代码/Git和浏览器客户端；不复制凭据、原音、设备备份和生成二进制到Git。
- 产品真实API，不加入mock应用、演示开关或虚构产品数据；隔离自动化夹具可用。
- 三链路为长期产品选择，切换立即结束当前对话，下次唤醒/按键连接使用新链路；不自动降级、不调用模型探活、不改固件、不操作USB。
- 既有鉴权、业务前置、设备控制、音乐、近期记忆、表情、已播上下文及取消保持共用；输入25秒/输出60秒等级的有界资源，历史比较工具的默认限制不改变。
- 已保存与当前实际链路分开；未确认或未知不能显示成功；PUT后GET核对；隐藏/退出/切设备废弃迟到结果。

## 共享契约

```go
// internal/realtimelive，管理包已有对该包的依赖；语音包不得反向依赖管理。
type VoiceRouteClaim struct {
 DeviceID string `json:"deviceId"`
 ActorID string `json:"actorId"`
 FamilyID string `json:"familyId"`
 BindingID string `json:"bindingId"`
 BindingVersion int `json:"bindingVersion"`
 Deadline time.Time `json:"deadline"`
 Route string `json:"route"`
 Version string `json:"version"`
}
type VoiceRouteState struct {
 DeviceID string `json:"deviceId"`
 ActiveRoute *string `json:"activeRoute"`
 Connection string `json:"connection"` // idle/connecting/active
}
type VoiceRouteGuard func(context.Context, VoiceRouteClaim, bool, func() error) error
// bool为写入/终止请求，false为状态读取；守卫核对账户、当前绑定和选择版本。
func (s *Server) VoiceRouteControlHandler(VoiceRouteGuard) http.Handler
// Config新增 VoiceRouteForDevice func(string)(string,error)。
// management新增：NewVoiceRouteResolver(path string) func(string)(string,error)
// management新增：NewVoiceRouteGuard(path string) realtimelive.VoiceRouteGuard
```

网页API `GET/PUT /api/admin/device-voice-route/:deviceId`，PUT精确JSON键`route,version`。响应 `deviceId,selectedRoute,activeRoute,connection,version,canSwitch`，PUT额外`disconnectConfirmed`；GET私有服务不可用返回`activeRoute:null,connection:unknown`并保留真实已选配置。私有POST `/voice-route/status`、`/voice-route/disconnect`接受共享claim。route严格A/B/C；缺省C；version为公开选择/绑定事实SHA256，不包含密钥。

### Task 1: 长期语音生成与私有连接控制

**Files:** `internal/voicecompare/{client,text,tts}.go`及其测试；新增`internal/realtimelive/voice_routes.go`,`voice_route_control.go`及测试；必要修改config/server/xiaozhi入口/session生成入口，不改命令层或管理包。

**Interfaces:** 产出上面的共享语音契约，和`voicecompare.Client.RunConversation(ctx, backend, instructions, pcm)`；原Run接口/限制保持。

- [ ] 先写云端RED：合法A/B/C解析、读取失败不得回C、每连接固定选择、当前route查询、带guard取消只终止目标连接、准备阶段租约/快速重连/迟到结束/ctx取消、守卫拒绝无副作用。
- [ ] 产品A/B生成复用协议，支持25秒输入/60秒音频、正常回复和上下文，不使用120字/4问/试验账本；合成提供方协议验证超120字、输入超过10秒、取消、失败不降级及最终元数据校验。
- [ ] 在newSession取得鉴权owner后解析route，A/B只替换生成函数，沿用公共reply业务前置/OutputASR/播放证据。C不改变原native行为。每API写现有私有intent/数值usage，不额外落原音。
- [ ] 实现Unix私有handler，guard下核对claim，再用Server.mu封锁目标准入、关闭该旧socket并等待租约结束。网络等待不持Server.mu；锁顺序不得反向；其他设备不终止，旧取消不能移除新租约。
- [ ] 云端专项普通/race通过；提交本任务拥有路径；产出来源、RED/GREEN命令结果、限制及架构自审报告，交task reviewer。

精确命令：`go test -count=1 ./internal/voicecompare ./internal/realtimelive -run 'VoiceRoute|Conversation|ProductRoute'`，再对同目标`-race`，预期0失败。

### Task 2: 设备选择持久化、权限与即时取消API

**Files:** `internal/management/config.go`,`server.go`；新增`voice_routes.go`,`voice_routes_test.go`；`cmd/realtime-output-live/main.go`的配置和Unix handler组合；相关CLI测试。

**Interfaces:** 消费共享claim/state/guard；产出GET/PUT响应及resolver/guard函数。DeviceConfig新增可选`VoiceRoute string json:voice_route,omitempty`，ValidateConfig拒绝未知route，历史配置缺省C。版本仅用id/binding/route事实。

- [ ] 先云端RED：真实登录请求不存在API返回404；default C、A/B/C持久化及重读、错误枚举/大小写键/重复JSON、只读/家庭Cookie/跨家庭/撤权/重绑/版本冲突拒绝且不调用私有接口。
- [ ] 使用ConfigStore.Transaction及锁后重新认证/授权，审计后仅修改该设备的voice_route。GET只需admin/当前设备授权，PUT另需CanAddDevices；原始资料/密码/绑定/其他设置不修改。
- [ ] PUT提交成功后发送带版本/绑定的私有disconnect，结果未确认返回真实已保存且disconnectConfirmed=false，不伪装原子回退。GET从Unix真实取当前route，IPC不可用标unknown。IPC后重新授权，避免撤权后泄露。
- [ ] guard通过WithSnapshot持读锁调用fn；写必须验证route/version与配置匹配且仍有管理权。resolver每次连接重读配置；不存在/损坏不静默C，只有未设置选择缺省C。
- [ ] 命令层接入Config.VoiceRouteForDevice，在同一既有Unix listener上按精确私有路径分发两handler，不加入公网Handler；没有新监听口/启动器。
- [ ] 云端专项普通/race、vet/build通过；提交拥有路径；交task reviewer。

精确命令：`go test -count=1 ./internal/management -run 'VoiceRoute'`；`go test -race -count=1 ./internal/management -run 'VoiceRoute'`；`go test ./cmd/realtime-output-live`。预期新API行为GREEN，不执行真实配置写入。

### Task 3: 管理端三按钮与实际状态

**Files:** `web/management/src/{types,remote-api}.ts`；新增`voice-routes.ts`,`components/VoiceRoutes.vue`及对应tests；`pages/CorePages.vue`挂载仅admin设备配置页。

**Interfaces:** 复用共享API契约；类型`VoiceRoute = 'A'|'B'|'C'`。集中`getDeviceVoiceRoute(deviceId)`/`setDeviceVoiceRoute(deviceId,route,version)`；请求路径始终admin，家庭界面无新增入口。

- [ ] 云端RED：响应严格校验路由/设备/版本/active状态、三按钮、PUT→GET回读、持久化不等于取消确认、IPC unknown、重复点击、失败/409后要求刷新、隐藏/401/切设备迟到结果清理。
- [ ] 简单一块panel展示三个可键盘操作按钮、链路完整名称与A/B完整合成后播放说明；点击前明确当前对话将结束，点击即执行，不再弹额外确认。选中与实际状态分开显示，切换中禁重复。
- [ ] 编写小型controller沿用敏感生命周期；保存后GET确认selected route/device/version，并结合disconnectConfirmed和当前connection显示“已切换，请重新唤醒”或“选择已保存，旧对话结束尚未确认”。状态可手动刷新，定时读取不发写请求。
- [ ] 云端npm test/typecheck/build通过，记录来源与产物；提交拥有路径；交task reviewer。

精确命令：在云端网页目录`npm test`、`npm run typecheck`、`npm run build`，预期现有和新增回归均通过。

### Task 4: 集成审查、云端发布和交接

**Files:** 本计划、设计、验证记录、DEVELOPMENT_HANDOFF、必要产品README；私有发布脚本及产物仅云端。

- [ ] 各任务独立规格/质量review后修复范围内重要问题，整批diff作最终架构/权限/取消/前后端契约review。
- [ ] 云端相关完整Go功能、相关race、vet/build、前端完整测试/类型/构建，冻结公开source；核对实际部署差异，只移入本批拥有路径，保留原Voice MemoryOnly/Speaker等差异。
- [ ] 隔离真实浏览器验证三按钮及生命周期，使用隔离服务和自动化夹具；不触发正式模型发声或替用户改变链路。
- [ ] 云端已有空闲状态下保留配置/绑定/记录/记忆/固件/Caddy及其他进程，先发布兼容新字段的Voice，再管理与静态；保持默认C，不自动选择A/B，不刷机。按真实PID/二进制/健康/静态来源核对。
- [ ] 正式只读确认admin真实GET显示C及语音闲置，家庭与公网私有路径拒绝；实际切换/三路现场音效另列待验证。提交推送main、核对远端和干净工作区，更新交接如实列已实现/部署/自动化/现场边界。

## 执行记录

- 设计已按用户最新选择更新为长期产品能力和立即终止旧会话；先前next-connection提案为已撤销建议。
- 已先清理经引用核对的Go缓存约3.37GiB，当前服务保持；构建前再次检查磁盘。
