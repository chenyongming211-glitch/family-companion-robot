# 小程序近期记忆管理实施计划

> 使用subagent-driven-development执行，任务后规格/质量复核及最终整体审查；用户已批准三项范围并要求继续，不重复申请已有授权。

**Goal:** 小程序查看真实近期记忆、查看来源聊天、管理员经确认清除并获得真实回执。
**Architecture:** 复用管理Reader和业务聊天投影，Voice独占Store；私有Unix HTTP桥接既有两服务，空闲设备门禁+新配置授权锁覆盖清除写入，列表/清除UI复用RAM会话。
**Tech Stack:** 现有Go、原生TypeScript/WXML/WXSS、Node/Vite已有构建，无新增依赖/服务。

## Global Constraints

- main直接提交/推送，不建分支/worktree。运行测试/依赖/构建/部署仅云端；Mac代码/Git/客户端。
- 凭据/原始正文/音频/备份/固件产物不进Git、不复制Mac；不清除真实家庭记忆作测试，不刷固件/碰退货M5。
- 范围仅7天/最多24轮实际续聊摘录、授权来源聊天、管理员清除；保留聊天档案、绑定起点和当前偏好。
- GET /memories?device=id继续数组格式，MemoryDTO仅增加可选chatId/turnId；来源按授权后的session+turn精确映射，不猜ID。来源缺失不伪造、不破坏记忆查看。
- POST /api/family/memory-clear/{deviceId}只接受{confirm:true}；当前管理员CanAddDevices+正文权限+当前绑定均须服务端新校验。持久化无正文审计先于RPC；小程序不能传actor/绑定/路径。
- 成功必须{deviceId,status:"confirmed"}且实际空Save receipt和FIFO Load核验通过。busy=409/memory_busy无操作；失败/超时=503/memory_clear_unconfirmed，前端未知、不自动重试。
- Voice唯一写入，私有Unix root0700/0600，固定POST /memory/clear，不挂公网mux，不新增凭据/服务/依赖。CLI可选--memory-control-socket，Voice --management-config注入guard。
- 任何lease含握手/最终checkpoint使同设备clear返回busy；Server.mu只登记gate不持锁I/O，两种reserve路径阻止新lease。shutdown等待已准入clear完成再关闭Store。
- ConfigStore.WithSnapshot只读不回写，同config.lock新授权。Manager释放锁再RPC；Voice guard持同锁到实际Save receipt，已准入写即使HTTP截止也不能提前释放配置锁/devicegate。Load独立2秒可未知返回；无后台重试/新任务框架。
- 小程序延续扁平/少量半透明现有主题，简单列表+一次系统确认。只读成员无清除按钮；后台/退出/401清正文及隔离迟到回调；Cookie/正文不写storage。
- 发布从当前实际来源最小移植；保留统一S3 600ms、现有语义控制/其他服务/历史，使用既有门控进行空闲切换。当前基线voice325098/3d4b9、management200267/7e7e438；重查后再发布。

### Task 1: 受控记忆清除、来源映射与私有桥

**Files:** internal/realtimelive/memory.go、server.go及新memory_control.go/tests；internal/management/memories.go/types.go/server.go、onboarding_store.go及新memory_clear.go/tests；cmd/realtime-output-live/main.go、cmd/management/main.go及必要命令测试。
**Interfaces:** MemoryDTO新增ChatID/TurnID string omitempty；public GET/POST契约如上；Options新增MemoryControlSocket；ConfigStore.WithSnapshot(ctx context.Context,fn func(Config)error)error；realtime定义MemoryClearClaim及Guard函数，cmd注入management授权实现，Unix固定私有RPC。

- [ ] RED云端测试：成员/无正文/旧绑定/撤销管理员/无confirm/非法键/Origin/审计失败均不调用RPC；exactsession+turn来源映射、重连业务聊天、多家庭、无映射仍显示记忆。
- [ ] RED云端测试：已有lease/握手/收尾busy，clear门禁阻止两入口；同设备双clear忙、其他设备可用；前序迟到Save后清空；Save/Load失败和取消未知不成功；RPC延迟跨重绑定/客户端截止不能清新归属，配置锁必须持到实际receipt；shutdown等待。
- [ ] 实现核心：begin gate -> WithSnapshot fresh权限/绑定 -> Save空数组 -> 等实际receipt -> 独立2s Load空 -> release config/gate；重用Voice原clear写入核验，保留floor/base/control清除。
- [ ] 实现固定Unix listener/client，根私有目录/类型/权限/陈旧与活跃socket检查，期限只影响准入，连接无重试/重定向/代理；命令flags可选且未配置不可变更；publicVoice mux无该路由。
- [ ] 云端相关完整race、vet、两命令build，检查源摘要；提交main，报告RED/GREEN、完整结果、来源增删及边界。独立规格/质量审查。

### Task 2: 小程序近期记忆页面

**Files:** web/miniprogram/src/{api.ts,family-page.ts,app.json,app.wxss}、新pages/memories/*、device/device.wxml、build.mjs/verify-native.mjs及tests/memories.test.ts和必要现有测试。
**Consumes:** GET数组{deviceId,id,conversationId,text,date,user?,assistant?,chatId?,turnId?}；device-settings.CanEdit；POST仅{confirm:true}，confirmed匹配deviceId。GET /devices/{id}验证授权设备。

- [ ] 云端RED：登录后列表/空/来源/权限/两家庭；确认取消无POST，只有管理员+正文读取成功可清；双击/隐藏/401/切账号/迟到modal和请求不误清或泄漏；busy/未知不报完成、无自动重试，错误设备/畸形回执拒绝。
- [ ] Api仅白名单新增memories?device及memory-clear/device，复用固定源和RAM；familyPage新增memories（避免影响原设置/配网），设备详情提供入口；清除单次确认、匹配回执、真实重读，不要求回读列表永远为空。
- [ ] 来源有合法chatId时去现有聊天详情且带当前device，来源缺失提示暂不可用；不猜conversationId，不遍历聊天分页。
- [ ] 云端npm test/typecheck/build/native验证，现有148回归保持；复制仅构建产物给root，提交main，仅拥有前端文件。独立规格/质量审查。

### Task 3: 最小来源发布与原生预览

- [ ] 冻结实际voice135源及真实management来源，保留公开fixture；只移入允许改动与新依赖模块，来源/测试/产物SHA逐项核对，勿把main其他研究整包部署。
- [ ] 新Unix私有运行目录，原argv仅追加明确的socket/config flags；两服务其余argv/env/配置保持，先Voice后管理，失败按兼容关系回退。复用既有AdmissionGate保护空闲切换；Caddy仅加新家庭精确GET/POST路由，私有RPC不公开。
- [ ] 测试实际权限/GET只读元数据、未登录401/越权/非法路径，所有既有配置/档案/真实memory摘要保持，绝不对生产真实设备发清除测试；合成全链路验证两服务清除与聊天保留。
- [ ] 云端构建小程序dist，Mac官方CLI编译预览，现有项目更新并给可扫描入口；不把手机未操作写PASS。
- [ ] 最终整体审查、验证/README/交接及任务清单更新，main推送核对远端/工作区。原真实内容不因开发删除。
