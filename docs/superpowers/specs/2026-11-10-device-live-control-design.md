# 待机在线状态与设备回执控制

用户确认待机也需要在小程序调音量/亮度，允许补控制通道并升级微雪。采用共享固件的轻量HTTPS轮询，复用现有独立管理服务及工厂设备认证；不引入新服务、MQTT、数据库或依赖。相比复用语音MCP连接，这条通道不需要唤醒或创建模型会话；相比常驻第二WebSocket，复用ESP HTTP客户端并限制响应/任务资源，减少常驻协议状态。

## 固定协议

设备每5秒POST `https://robot.songguoxue.com/api/device/state`，TLS证书校验、禁跳转，原 `Authorization: Bearer <factory-key>` 与 `Device-Id: <canonical mac>`，仅已activated设备。JSON精确字段：`{boot:string32hex,seq:positive integer,volume:int|null,brightness:int|null,ack?:{id:cmd_32hex,status:"applied"|"failed",actual:int|null}}`。值域0..100，序号最多9007199254740991；每次请求seq递增，重复/倒退拒绝。boot每次上电随机生成；服务记录最近4个已退休boot，拒绝旧启动回流。响应 `{command:null|{id,field:"volume"|"brightness",value:int}}`。响应最多2048字节，HTTP单次5秒超时，失败退避至30秒，不记录凭据。

控制和音频/配网共用两S3的同一固件代码；仅已绑定、联网、时钟同步、音频初始化后启动单个后台任务。复用一个HTTP客户端；主应用线程通过Schedule执行设置及读取实际音量/亮度，网络线程不操作codec/显示。以实际读值匹配再发applied回执，亮度渐变要等待实际读到目标。命令ID去重，同ID不重复写NVS；上电换boot，旧命令不跨boot重放。未执行/失败/超时不能发applied。

## 服务端和家庭接口

GET `/api/family/device-runtime/<dev_32hex>` 返回 `{deviceId,connection:"online"|"offline"|"unknown",seenAt:string,boot:string,volume:int|null,brightness:int|null,canControl:boolean,command:null|{id,field,value,status:"queued"|"sent"|"confirmed"|"failed"|"unknown",actual:int|null,expiresAt:string}}`。未有新固件上报为unknown；最近一次有效鉴权上报20秒内为online，过期为offline且当前数值清空，保留seenAt。不把历史语音连接当在线。

PUT同路径精确接受 `{field,value,boot}`，要求当前设备授权及当前绑定家庭canAddDevices管理权限；必须online且boot等于最新设备boot。每设备最多1个未完成命令，服务端随机cmd_32hex，45秒期限。首次返回queued或sent绝不等同confirmed；GET轮询查询。设备获取命令前重查原actor当前授权和绑定，撤权/换家庭不下发；同一boot内可重传同ID以恢复丢失响应。applied仅在相同boot、匹配ID、未过期、ack.actual及本次对应状态读值均等于目标时confirmed；失败/不匹配/过期明确failed或unknown，不自动重试新命令。新boot使未完成旧命令unknown。状态和单个最近命令仅RAM，每设备固定有界，管理重启后unknown，不伪造持久成功。

JSON拒缺失/null必填、额外键、大小写别名、重复键及非整数。审计不含正文/密钥，写入审计失败不创建命令。设备认证失败不更新心跳。当前昵称/位置设置、账号、绑定起点、语音参数和历史保持。

## 小程序与交付

首页卡片及设备详情显示在线/离线/尚未上报和检查时间；详情/设置进入后读取，前台10秒刷新，隐藏停止定时器并清数据，晚到回调/退出/401仍隔离。音量/亮度分别0..100滑块+显式设置按钮，权限不足/离线/未知/无字段能力禁用；提交后显示等待设备，最多观察50秒，confirmed才显示生效和实际值，超时提示结果未确认并刷新。不乐观改成目标值，不把排队/送达当成功。

所有依赖、测试、构建和服务均云端，Mac仅Git、USB字节转发和微信客户端。固件共享逻辑修改验证两S3原生构建；仅给微雪升级活动应用槽前做新私有备份、身份/分区核对，保留槽外NVS/OTA/原家庭/历史并完整回读。退货M5不刷不联调。先部署向后兼容管理端及窄Caddy路由，再升级微雪和小程序；无需重启语音服务。真实心跳、命令执行/回执与语音兼容单独记录，合成测试不替代现场声学验证。
