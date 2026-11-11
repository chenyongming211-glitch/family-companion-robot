# 内部后台真实操作审计实施计划

> 使用subagent-driven-development逐任务实施、规格/质量复核及最终审查。用户已批准连续开发，不重复申请范围授权；直接main提交推送，无分支/worktree。

**Goal:** 后台可追溯授权设备的真实操作与核实结果，并按设备/时间/类型筛选。
**Architecture:** 扩展现有私有审计事件与投影，只在管理服务关联runtime/clear真实结果；Vue复用集中API和敏感会话失效。只发布管理，不更新Voice/固件。
**Tech Stack:** 现有Go、Vue3、TypeScript、CSS、Vite、node:test；无新依赖/服务/数据库。

## Global Constraints

- main直接开发提交推送，不建分支/worktree。依赖/运行测试/构建/部署仅云端；Mac代码/Git和浏览器客户端。
- 凭据、真实正文/音频/备份/固件不进Git或复制Mac。不用真实设备设置/清除造测试日志，不刷固件/开USB/碰退货M5。
- 复用既有audit/runtime/clear，保留HTTP与设备/私有claim协议、家庭API、小程序、统一S3 600ms/语义控制；不发布Voice，不修改Caddy/防火墙/账户权限。
- admin独立会话+当前InternalAdmin+明确设备/事件时刻家庭绑定授权；角色不隐含全局日志访问。结果聚合严格同operation/actor/scope/device/family/binding/version/目标，旧缺失不推断成功，未归属事件不输出。
- 请求审计先持久写入再下发；真实匹配ACK/RPC后才有confirmed。终态审计失败不篡改已知设备结果、不撤销/重发操作，不自动重试。日志不含正文/凭据/任意错误文本。
- GET /api/admin/audit参数device/from/to/action/offset/limit单值，RFC3339[from,to)，action四分类，offset0..10000、limit1..100默认50；DTO严格沿用设计字段，授权后分页，无未授权总数。
- 原日志append-only不改写；root普通0600、32MiB/100000行/4096字节每行上限；未完成尾行pending，坏整行/超限503，无file-path入参。
- 页面隐藏/卸载/路由/账号变化/退出/401清记录并隔离迟到响应，筛选重置分页，无storage正文/Cookie，无mock回退，无自动全量/导出。
- 实际178项基线最小移植；保留旧审计前缀和旧static，管理只换exe/static，Voice/PID/Caddy/防火墙/配置/记录/.memory保持。

### Task 1：结果事件与授权审计查询

**Files:** internal/management/audit.go及新audit_read.go/相关tests；runtime.go、memory_clear.go、server.go及必要相关测试。只拥有后端文件。
**Interfaces:** 设计中的AuditPage/AuditItem/AuditFilter及GET /api/admin/audit；现有runtime/clear HTTP与私有claim保持，auditEntry仅omitempty扩展。

- [x] 云端RED：旧请求无结果为未知、访问只表示已记录；新控制request→真实匹配ack、清除RPC三类结果、终态失败不重发/伪报、重复终态不追加。
- [x] 实现元数据请求/终态追加与关联，不改变原设备/RPC成功契约；记录绑定锚点，保留旧六字段调用兼容及锁顺序。
- [x] 云端RED→GREEN：两家庭/无设备/转让历史/撤销角色/当前会话、迟到结果、冲突/孤立事件，未知过滤参数、分页/文件私有和大小/尾行边界。实现有界快照、严格授权投影及仅admin路由。
- [x] 最终完整相关race、vet、管理build，source摘要；权限负例使用既有去DAC特权测试子进程，不弱化测试。提交main，报告失败/修正/边界，独立规格/质量审查。

### Task 2：后台审计真实页面

**Files:** web/management/src/{App.vue,remote-api.ts,types.ts}，新pages/AuditPage.vue及必要小控制单元/测试；mock现有OperationsPages审计保持。只拥有web/management。
**Interfaces:** 集中API listAuditPage(AuditFilter)→AuditPage，字段按设计；live独立admin，API不接受scope改写或任意文件路径；mock原listAudit(query)契约不改。

- [x] 云端RED：筛选参数/日期转换/分页、未知与缺失数值、不授权设备跳转、错误/空/partial、隐藏/401/退出/账号/路由变化及迟到请求清理，实际组件事件绑定。
- [x] 实现live审计入口和简洁表单/列表/结果，按id操作独立展开可选；优先复用敏感会话/请求代际，无新状态框架，不扩展无关mock功能。
- [x] 完整npm test/typecheck/mock+live构建，source/dist摘要，提交main与独立规格/质量复核。root使用合成API浏览器走查，无真实操作。

### Task 3：最小管理发布与交接

**Files:** 验证/README/API与交接/计划，运维脚本仅忽略目录/云端。
- [x] 冻结实际管理178项及原binary/static、Voice/Caddy/配置/数据/旧audit前缀；最小Go补丁反向核对，云端实际候选相关race/vet/build与静态manifest核验。
- [x] 合成浏览器验证admin筛选/分页/设备链接/未知、权限拒绝/迟到清理与375/1440布局，结束测试空间/服务/新增SSH转发。
- [ ] 管理仅换已核验binary/static；原Voice/Caddy/防火墙不动，未知process/unit拒绝切换，失败仅回退已知代码/static不恢复旧数据或日志。只读实际接口计数/401/403/公网404检查，无真实设置/清除。
- [ ] 最终整体审查及上线独立SHA/参数/环境/保护文件/审计前缀核对，更新验证/README/交接/任务清单，main推送核对远端和本批文件无未提交变更。

## 本轮交接状态

Task1/2已实现、测试、审查并推送main；Task3已最小来源切换，实际功能、数据/配置/旧日志/Voice/Caddy等核验通过，但不勾选发布全流程及全保护完成。首次事务中断且自动回退未完成，保留健康已知新管理；一次独立全保护证明后，后续观察的防火墙摘要与起点不同，严格验收仍未收口。没有改写规则、重新设定基线、伪造deployment.json或还原旧数据。原异常未保存，后续明确firewall guard失败不等于首次原因已查明；私有异常记录修复57项合成及复核通过。详见[实际验证与限制](../../verification/2026-11-11-admin-audit.md)。

主线/文档按main提交推送；工作区另有原有robot-audio-capture三文件及M01验证文档，不属于本批，保持未暂存。用户暂缓预览/现场操作，不新增手机或真实设备成绩。下次先核对防火墙差异来源及原事务状态，再决定恢复/回退；不能绕过现有保护条件。
