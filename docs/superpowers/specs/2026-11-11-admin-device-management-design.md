# 内部后台真实设备控制与记忆管理

用户已批准：按原计划将真实在线状态、音量/亮度控制、近期记忆清除和操作回执接到内部后台，与家庭小程序对齐。本次沿用现有Vue页面与Go服务，不重新选择界面或框架。

## 范围与实现选择

扩展现有管理服务的admin作用域，复用同一runtime状态/命令队列及唯一Voice记忆Store。相较另建控制服务或让后台借用家庭登录Cookie，这样保留现有身份、权限与回执所有权。内部列表/设备详情显示心跳在线状态，设备详情与配置页提供音量/亮度；记忆列表/详情提供一次确认清除。家庭网页复用同一组件和API，保留mock演示行为与标识。

不增加成员邀请、留存/删除、搜索、通用任务平台、模型调用或固件改动。不把管理员角色当作设备/正文授权。原账户、配置、归属、偏好及聊天档案保持。

## 服务端契约与权限

- 新增`GET/PUT /api/admin/device-runtime/{deviceId}`与`POST /api/admin/memory-clear/{deviceId}`，复用现有family处理器。admin入口要求有效独立admin会话、当前InternalAdmin及明确设备授权；写操作还要求当前绑定家庭CanAddDevices；清除另要求正文许可。不得接受family Cookie冒充admin。
- RuntimeDTO沿用`deviceId,connection,seenAt,boot,volume,brightness,canControl,command`；20秒心跳在线判定、0..100、boot核验、45秒命令期限、一次在途及真实读值匹配confirmed均保持。命令私有保存来源scope，设备取命令/回执时重新核验该scope的当前权限。
- PUT只接受既有`{field,value,boot}`；POST只接受`{confirm:true}`。审计记录真实请求scope，失败不下发。config错误清除统一memory_clear_unconfirmed；busy409，不自动重试。
- 私有MemoryClearClaim仅新增可选`scope`，省略保持family兼容；仅接受family/admin。admin claim在Voice持同一config.lock的新权限核验中检查InternalAdmin与当前家庭/绑定/正文/管理许可，锁覆盖实际Save receipt与回读。私有路由仍不挂公网。
- 小程序family API、设备协议、语音/播放600ms及存储规则保持；admin只在管理回环/现有SSH入口提供，不新增公网Caddy admin路由。

## 前端行为

沿用扁平、简洁页面。真实模式通过集中API读取runtime、提交设置、清除记忆；页面不复制服务端权限/状态规则，不回退mock掩盖失败。无心跳显示尚未上报，过期显示离线，数值未知不显示0。在线状态在前台低频刷新，离线/无能力/无管理权限禁止提交。

音量/亮度0..100每10一档、11圆点；拖动只预览，松手一次提交，键盘操作也可提交，没有第二个设置按钮。只在命令ID、字段、目标值、boot/设备匹配且status=confirmed、actual=目标时提示生效；queued/sent等待，超时/失败/冲突或非匹配回执不得虚报成功。观察最多50秒，不自动重发。

清除仅在本次授权记忆读取成功且管理权限提示通过后显示；一次系统确认，取消零POST，重复点击只发送一次。匹配设备confirmed后重读记忆，提示清除完成且聊天保留；新对话可重新产生记忆。对话busy和未知状态明确反馈。来源优先使用业务chatId/turnId，不把来源连接ID当业务聊天ID。

隐藏/卸载/路由或设备切换/退出/401/账号变化停止轮询与观察、清正文并隔离迟到请求与确认回调；不将Cookie或正文写入storage。不同入口会话独立，其他页面的401/退出也使当前敏感状态失效。刷新不得覆盖正在编辑的滑块。

## 验证与发布

先云端RED再GREEN，覆盖两家庭、只读成员、内部角色撤销、设备/正文/管理授权撤销、跨入口Cookie、审计失败、精确请求形状；合成设备心跳/取命令/真实匹配回执及双服务清除、档案保留。前端覆盖实际控制逻辑与原生DOM绑定、迟到modal/响应、双击/隐藏/退出/401、错误设备/命令/回执、未知与超时。保留原mock/家庭端/小程序回归。

从实际recent-memory176项来源仅移入本批后端差异，分别核对源码/产物；云端构建并最小发布两服务，既有IPv4/6门控保护空闲切换。后台live静态产物单独备份替换，不覆盖mock或小程序。保护配置/历史/.memory及其他服务，Caddy保持原字节。不对真实设备下发设置或清除作自动测试，不开USB/刷固件/触碰退货M5。Mac仅代码/Git和浏览器测试客户端。
