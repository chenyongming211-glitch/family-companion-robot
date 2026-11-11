# 真实管理平台及微雪唯一目标清理计划

> 使用subagent-driven-development逐任务实施与规格/质量复核；用户已明确删除mock并退役M5，直接main，无分支/worktree。

**Goal:** 当前产品与运行环境只保留真实管理入口，微雪为唯一当前设备与固件支持目标。
**Architecture:** 复用现有Remote API和S3共同实现；删除演示实现与分支，使用既有ConfigStore绑定结束时间退役M5，无新服务/依赖/数据模型。
**Tech Stack:** Go、Vue3/TypeScript/CSS/Vite、Python运维与既有ESP32-S3构建链。

## Global Constraints

- main直接提交推送，无分支/worktree。依赖、运行测试、构建与服务操作仅云端；Mac代码/Git和浏览器客户端。
- 删除产品mock，不归档保留；测试隔离样例不作产品fallback。不要改写Git历史或删除真实聊天/记忆/音频、凭据、设备备份、原中断事务证据。
- 不改界面布局，不增加新业务功能/依赖/框架；真实API、权限、回执、隐私清理与原微雪绑定及当前偏好保持。
- 固件仅支持微雪ESP32-S3，M5/C3不是构建/验收门槛；保留必要共用实现和历史硬件证据，不刷机/开USB/触碰退货M5。
- 只发布前端静态更新及M5绑定Until事务，不重启Voice，不改Caddy/防火墙，不恢复旧配置或重新设定旧审计发布基线。
- 清理运行产物前必须形成明确路径/PID/用途清单，未知服务或路径不删除；原18791真实入口保留。

### Task 1：仅真实前端

**Files:** web/management全目录（仅此任务所有）；创建src/api-error.ts，必要时src/api-types.ts；删除prototypes由root操作。
**Interfaces:** 默认api=createRemoteApi()；ApiError与实际接口类型不依赖mock；既有设备/聊天/记忆/runtime/clear/audit HTTP契约不变。

- [ ] 云端RED验证默认入口只能使用真实同源API、匿名登录和受保护401/隐藏/迟到隔离、无演示路由与存储；保留真实功能行为测试，删除仅测试被删除演示产品的用例。
- [ ] 删除mock-api.ts/seed.ts/OperationsPages.vue/FacePreview.vue/operations-pages.css及其专属类型/导入/演示分支；从CorePages/DataPages/App/navigation移除mock选项、假ID默认值、模拟写入/状态与虚构记录入口。不要保留恒true的liveMode或旧模式开关。
- [ ] api.ts直接构造真实Api；真实错误类放api-error.ts。控制器以实际方法的结构类型为接口，保留纯类型依赖方向；未实现写操作及其演示UI删除，不顺手实现。
- [ ] 云端完整npm test/typecheck/build；40项原输入变更按新清单逐项核验，产物不含产品seed/演示入口；提交main，不推送，报告失败/修复/测试/摘要并独立审查。

### Task 2：微雪唯一固件目标

**Files:** .github/workflows/firmware-experiment.yml、firmware-baseline.yml、firmware/tools/run_host_tests.py及必要固件验收工具/文档。不得修改web目录。
**Interfaces:** 活动产品目标仅waveshare/esp32-s3-touch-amoled-2.16与其已验证变体；共用代码保持；历史配置解析兼容不代表继续M5供应。

- [ ] 核对锁定上游的微雪原版构建名和baseline verifier，删除活动M5矩阵及M5专用门槛；若独立原版微雪需适配则最小修改验证工具，不伪造成功或以M5当微雪基线。
- [ ] 新供应/发布工具只开放微雪；保留读取历史M5配置所需兼容。仅改已确认供应入口，不改变语音/配网公共协议。
- [ ] 云端相关host准备/验证测试通过，提交main并独立复核；推送后跟踪实际活动微雪CI，不能以主机测试冒充固件构建或实机成绩。

### Task 3：运行清理、M5退役、真实发布

**Files:** prototypes/management-platform删除；README/交接/部署文档及忽略目录运维工具；仅云端私有配置事务。
**Interfaces:** ConfigStore.Transaction内仅目标M5 Binding.Until=本次UTC；当下ListDevices不再授权该设备，历史绑定/正文保留。

- [ ] 只读盘点Mac/云端mock源码副本、dist-mock/旧演示构建与18790进程/转发；区分依赖缓存、真实live产物、测试样例和私有数据，生成明确删除manifest。
- [ ] 对合成配置验证关闭绑定后微雪当前授权/历史不变、M5当前拒绝且历史仍可授权；云端事务执行实际单一M5退役，不创建新持久业务字段。
- [ ] 审查后更新当前静态资源，验证真实两端与原数据保护，删除已确认演示源码/产物/18790服务/转发和无引用旧演示资产，不留演示归档副本。
- [ ] 独立检查当前设备仅微雪、真实两端与服务健康、演示入口不可访问及源码/产物无产品mock依赖；准确记录CI/部署/实机边界和未关闭的旧防火墙差异。
- [ ] 最终整体审查、验证/README/交接/计划更新，main推送核对远端；原有audio-capture三文件及M01验证文档不属于本批，保持不动。

完成后再建议下一批：优先把网页设备昵称/位置保存接到已有真实接口，随后才安排成员权限和记录删除/留存；本批不自动扩展这些功能。
