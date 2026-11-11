# 内部后台真实操作审计

用户已批准按原计划继续P3：查看操作记录、区分真实执行结果、按设备/时间/操作类型筛选并返回设备。复用既有Go管理服务、私有append-only审计日志与Vue后台，不增加服务、数据库、依赖、模型或固件功能。此次只部署管理服务，Voice协议和运行版本保持。

## 记录与真实结果

既有日志保留，不回填或改写。旧访问事件仅表示访问被授权并记录，不证明客户端实际读到；旧设置/控制/清除请求没有完成回执时显示未知。无设备归属信息的旧全局事件不向家庭设备审计页面暴露。

auditEntry增加omitempty元数据：operationID、familyID、bindingID、bindingVersion、outcome、field、value、actual、reason；不记录正文、密码、Cookie、工厂密钥或任意错误文本。现有六字段日志和写入调用兼容。新控制请求以现有cmd_ID关联，清除生成op_ID；请求先持久写入，失败不下发。记录请求时固定当前家庭/绑定及目标字段/值。

控制终态仅在现有状态机得到真实匹配回执后记录：confirmed须匹配命令、boot、目标与实际；设备明确failed为failed，回执不一致/超时/设备重启/权限或绑定改变为unknown。不改原runtime状态或重发硬件操作。私有命令标记防止重复终态写入；终态日志失败不篡改已有真实设备结果，不自动重试或重发；缺失记录在审计投影中保守未知。

清除的结果记录关联同一op_ID；仅原RPC已完成Save/readback确认才记confirmed，busy/明确拒绝记failed，网络/期限/不确定错误记unknown。结果日志失败不能撤销已完成的清除或导致再次清除。HTTP仍遵循既有真实回执契约，Voice仍独占Store，私有claim不增加字段。

## 查询与权限

仅`GET /api/admin/audit`，有效独立admin会话+当前InternalAdmin+明确设备/历史绑定授权。无family审计入口，不新增公网Caddy路由。查询在输出前新核验会话/角色/授权；当前内部角色不自动获得所有家庭日志。

参数仅`device,from,to,action,offset,limit`，各单值；device为dev_32hex；from/to为带时区RFC3339、区间[from,to)，from<to；action为access/device_control/memory_clear/settings；offset0..10000，limit1..100默认50。省略设备仅返回全部获授权记录，不泄漏全局总数或未授权对象。按请求时间降序稳定排序，筛选授权后分页。

返回`{items,hasMore,pending}`；item为`{id,time,actor,deviceId,target,action,description,status,result,scope,canOpenDevice,field?,value?,actual?}`。action为上述分类，status为recorded/requested/confirmed/failed/unknown，time为请求/访问RFC3339时间；result与description使用受控说明，不含正文或任意日志字段。actor/target仅在当前相同家庭授权可安全解析时映射名字，否则使用历史账号/历史设备；不泄漏新家庭改名。只有当前仍有设备访问权限才canOpenDevice。

新事件按operationID聚合请求和结果，必须匹配actor/scope/device/family/binding/version与目标；以请求时刻的绑定为归属锚点，迟到结果不错误归入新家庭。重复相同终态可合并，冲突降为unknown，孤立结果不假造请求。旧日志通过事件时刻历史绑定授权，控制等请求结果未知；不将当前设备数值当历史回执。未归属事件不输出。响应不暴露原始recordID、原始行或可推算未授权行数的偏移。

读取仅既有AuditPath，root私有普通0600文件、拒绝symlink/FIFO/目录；最多32MiB/100000行、每行4096字节JSON。缺失日志为空；只跳过未完成尾行并pending=true，已换行坏记录/超限返回503不静默截断。短暂持auditMu取得有界一致字节快照，释放后解析与新权限核验；不建立runtimeMu/auditMu/会话锁反向依赖。

## 页面与验收

复用后台“审计”入口，简单设备/日期起止/类型表单、查询/刷新/分页列表，显示操作者、时间、设备、操作、目标/实际与结果；缺失数字显示未记录而非0。设备跳转只在canOpenDevice时开放。mock原审计保留明确演示，live不回退mock。隐藏/卸载/路由或账号变化/401/退出清记录并使迟到请求失效，筛选变化重置分页，不自动全量拉取或导出。

云端TDD覆盖真实请求/匹配ACK/清除结果、日志失败、未知/重启/重复/冲突、旧日志、两家庭/历史转让/撤销角色权限、参数/文件与边界；Vue覆盖实际组件事件/迟到请求、筛选/分页和退出；浏览器仅合成日志。实际环境只读核对授权计数/新接口拒绝边界，绝不对真实设备下发设置/清除来造日志。Mac仅代码/Git/浏览器客户端。

从实际管理178项来源仅移入本批Go改动，核验源码/产物，原静态保留、管理换exe/static；Voice/PID、Caddy字节/元数据、防火墙、配置/偏好/记录/.memory不变，旧审计字节前缀保持。既有账号无需改权限，管理重启后登录RAM失效需重登。无需用户现在预览或现场操作。
