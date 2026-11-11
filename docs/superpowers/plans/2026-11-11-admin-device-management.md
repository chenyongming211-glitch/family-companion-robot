# 内部后台真实设备控制与记忆管理实施计划

> 使用subagent-driven-development逐任务实施和规格/质量复核，完成后整体审查。用户已批准范围，直接连续实施；main提交推送，不建分支或worktree。

**Goal:** 内部后台真实在线状态、音量/亮度与记忆清除均以服务端/设备回执确认，与小程序共用能力。
**Architecture:** 现有Go管理服务增加admin作用域适配，复用runtime与Voice唯一Store；Vue集中API及共享组件承载后台/家庭网页界面，mock保留明确演示。
**Tech Stack:** 现有Go、Vue3、TypeScript、CSS、Vite与node:test；不新增依赖/服务。

## Global Constraints

- main直接开发提交推送，不建分支/worktree。运行测试/依赖/构建/部署仅云端；Mac代码/Git与浏览器客户端。
- 凭据、真实正文/音频/备份/固件不进Git或复制Mac。不用真实设备做设置/清除自动测试，不刷固件/开USB/碰退货M5。
- 保留账户/配置/归属/偏好/档案/.memory、统一S3 600ms、小程序与家庭接口。admin API不新增公网路由；Caddy原字节保持。
- admin要求独立admin会话+当前InternalAdmin+明确设备授权；写还要CanAddDevices，清除还要正文授权。角色不隐含设备/正文/控制权限；来源scope在下发/回执和Voice新授权时保持。
- 复用GET/PUT device-runtime和POST memory-clear，DTO及family兼容；private claim可选scope默认family，仅family/admin。无新控制/记忆服务或凭据。
- 心跳20秒、命令45秒、前端观察50秒；0..100每10一档/11圆点、松手一次提交；仅完整匹配confirmed/actual才成功，未知不自动重试。
- 清除一次确认/防重，服务端confirmed后再提示并重读，聊天保留。busy提示稍后；不推断列表永久为空。
- 隐藏/卸载/切设备/路由/退出/401/账号变化停止观察清正文，隔离所有迟到modal/HTTP；Cookie/正文不落storage。

### Task 1：服务端admin适配与新授权

**Files:** internal/management/{server.go,runtime.go,memory_clear.go}及相关新测试；internal/realtimelive/memory_control.go及测试。只拥有这些后端文件。
**Interfaces:** GET/PUT `/api/admin/device-runtime/{id}`沿用RuntimeDTO与`{field,value,boot}`；POST `/api/admin/memory-clear/{id}`仅`{confirm:true}`，回`{deviceId,status:"confirmed"}`；MemoryClearClaim可选scope默认family。复用原处理器/权限核心，audit scope真实。

- [ ] 写云端RED：有效admin原路由缺失；family Cookie/admin角色无grant/只读/撤销角色或管理/正文授权均拒绝，不产生命令/RPC。
- [ ] 实现按parts[0]作用域校验与共享处理器；runtimeCommand保存私有scope，取命令/ack重新校验；私有claim兼容旧6键并严格校验可选scope，Voice新guard按scope授权。
- [ ] 云端GREEN：合成心跳→admin PUT→取命令→匹配实际ack confirmed；角色撤销阻断待执行命令；合成admin清除及档案保留/权限锁竞态。审计scope、形状、配置失败错误码及family兼容。
- [ ] 完整相关race、vet和两命令build，仅测试子进程按既有方式去DAC特权以测试权限负例。检查来源SHA、提交main与独立规格/质量审查。

### Task 2：Vue集中API、真实状态与控制/记忆页面

**Files:** web/management/src/{remote-api.ts,api.ts,types.ts,mock-api.ts}及必要共享状态/控制组件，pages/{CorePages.vue,DataPages.vue}、App.vue、对应CSS及tests；不改小程序或后端。
**Interfaces:** runtimeDTO同Task1；集中API扩展getDeviceRuntime(id,scope)、controlDevice(id,{field,value,boot},scope)和现有clearMemories(id,scope)。clearMemories在API核验匹配回执，保持mock契约。记忆可选chatId/turnId来自现有GET。

- [ ] 云端RED覆盖实际控制逻辑：离线/未知/权限、匹配与畸形回执、拖动和松手防重、观察截止/迟到响应、隐藏/401/账号和设备变更、清除取消/重复/迟到确认/错误设备/未知。
- [ ] 实现集中API与最小共享控制组件/状态单元，内部设备列表/详情显示runtime，详情和配置页控制；记忆列表/详情授权清除与回执/重读/来源。live不借用family会话，不回退演示。
- [ ] 复用同一组件支持家庭网页，保留mock演示标识和既有31页；修补本次敏感数据跨页退出/401失效，避免改变无关功能。
- [ ] 云端完整npm test/typecheck/build、mock/live双构建；报告来源/产物SHA、提交main与独立规格/质量复核。浏览器复核由root在合成fixture服务完成，不请求真实写操作。

### Task 3：实际来源候选、发布和交接

**Files:** 验证记录、README/两端说明/DEVELOPMENT_HANDOFF及本清单；运维工具与私有证明仅忽略目录/云端。
- [ ] 冻结实际两服务176项manifest/二进制，reverse patch证明仅移入Task1增量；云端候选相关race/vet/build，保留旧语音差异。检查既有配置/static/数据摘要与PID。
- [ ] 浏览器作为测试客户端，用合成授权/心跳/回执fixture走设备→控制→记忆清除，包含375/1440布局、无权限/未知/隐藏/切设备。网页真实只读入口只验证元数据/未登录拒绝，不清正文。
- [ ] 复用已修fchmod的门控切换：先Voice后管理，私有GET405+未过deadline的陌生身份403；保护实际旧参数/环境、配置/档案/.memory、原Caddy与其他服务，静态live按产物manifest替换。失败只回退已知代码/静态不回写旧配置/用户数据。
- [ ] 独立上线后核对binary/source/hash、ready/memory_ready、admin未公开、旧family兼容、保护文件和门控规则恢复；更新验证/README/交接/清单，main提交推送核对远端与干净工作区。
