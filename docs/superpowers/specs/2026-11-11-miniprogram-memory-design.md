# 小程序近期记忆管理

用户已明确批准并要求继续开发：查看最近7天/最多24轮实际续聊摘录、跳到来源聊天、管理员清除且服务端确认后才提示完成，聊天档案继续保留。沿用现有小程序扁平/少量半透明主题与共享RAM会话，不增加长期人物记忆、摘要、搜索或删除聊天。

入口为设备详情“近期记忆”，展示时间、用户转写和机器人已播摘录；无数据/失败/无权限/来源暂不可用都有直接反馈。来源跳现有聊天详情，目标每次重新鉴权。清除使用一次系统确认框，内容说明聊天记录保留，普通成员只读。清除发出后禁重复提交；后台、退出、401或页面切换清正文并使迟到结果失效。成功只来自匹配设备的confirmed回执；busy提示“机器人正在对话，请结束后再清除”，超时/未知提示刷新查看，不自动重发。

读取复用GET /api/family/memories?device=dev_<32hex>的现有数组。MemoryDTO向后兼容增加可选chatId/turnId，由Reader.chats授权投影以精确session+turn映射业务chat_ID，保留conversationId旧字段；不在客户端猜ID/遍历聊天。缺可用来源只取消链接，不伪造。canClear使用成功的正文记忆读取+现有device-settings CanEdit提示，POST独立重校验。

新POST /api/family/memory-clear/{deviceId}仅接受{confirm:true}，复用当前登录/Origin及严格JSON。管理服务从最新私有配置生成actor及绑定声明，持久化无正文clear_memory_requested审计后释放配置锁，再调用同机私有Unix HTTP POST /memory/clear。实际回读确认响应{deviceId,status:"confirmed"}；409 code=memory_busy且无变更；缺权限403；结果未知/不可用503 code=memory_clear_unconfirmed。小程序不能指定actor/绑定/socket/路径。

Voice保持唯一Store写入者。其专用Unix socket父目录root0700、socket0600，不挂公网voice mux；现有两服务都为root，无新服务、凭据或依赖。可选CLI --memory-control-socket，Voice另接--management-config来构造注入式授权guard；cmd组合management和realtimelive无依赖环，realtime不导入management。新监听生命周期跟随现有voice；拒绝symlink/非socket/仍活跃的同路径监听，严验私有路径。

清除只在没有设备lease时执行（包括握手和Run退出最终checkpoint）；Server.mu下登记同设备clear gate后释放mu，两个接入共用reserveLocked检查门禁，阻止新会话直到结束，其他设备不受影响。无需在活跃语音中取消音频或操作固件。Shutdown先阻止新操作，等已准入clear完成再关Store。

新增ConfigStore.WithSnapshot(ctx,fn)复用同config.lock，读取授权且不回写配置；ctx仅限制取锁和callback准入，callback执行中不提前解锁。Voice guard在该锁下重新检查actor、正文授权、当前family管理员CanAddDevices及精确binding ID/version/family，再准入空记忆Save。管理端不持锁等RPC，避免死锁。Save准入后等待真实durable receipt，即使HTTP客户端5秒截止也不能提前释放配置锁和device gate；确认写完成后以独立2秒FIFO Load验证空数组。未知写/回读失败不可confirmed，不重试。磁盘卡死会保留变更锁，这是故障时fail-closed，不能声称整个handler有界。旧语音清除保留原memoryFloor/base/control context语义，抽取可复用写入/核验核心，聊天档案和其他设备不改。

测试/依赖/构建/服务仅云端，Mac代码/Git和微信客户端；main直接提交推送。不清除任何真实家庭记忆作测试，合成Store/配置/会话覆盖并发、延迟、重绑定、权限撤销、receipt失败与超时，真实环境只读核对既有数据/权限和源码。发布只移入当前实际voice135项及管理冻结来源之必要增量，保留语义控制/统一600ms/家庭/档案/当前偏好，空闲门控发布，复用已验证运维保护，不刷固件或触碰退货M5。
