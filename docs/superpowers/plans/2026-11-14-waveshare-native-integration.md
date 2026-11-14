# 微雪原生连续语音隔离测试执行计划

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans. 用户要求main直接开发/提交/推送，不创建分支或worktree。运行依赖、测试、模型连接、构建及后端均在云端；Mac仅代码/Git与用户操作。

**Goal:** 按用户“现在按照新方案开始测试”的授权，将一个具备语速与克隆能力的端到端候选接入微雪的隔离后端，验证原生连续对话；不进行耳机直连/模型基础聊天、不永久迁移现网。

**Architecture:** 优先核对Qwen-Audio3.1-Realtime Plus实际端点。以正式200项来源冻结副本复用鉴权、输入准入、Opus/I2S回执、输出ASR与回声保护；仅替换主回答为会话级原生连接，原监听暂保留并明示不代表smart_turn_v2已验。关闭隔离实例的旧记忆、情绪/业务工具，不对原产品源码作整包发布。仅目标微雪下一次新连接经精确Caddy路由进入loopback隔离端口；预备可核对的原路由恢复。

**Tech Stack:** 既有Go1.27.3、gorilla/WebSocket及设备协议；云端受保护配置读取凭据，不新增生产依赖或固件功能。测试源码先放本轮私有隔离目录，Git保存方案与去敏证据。

## Task 1：冻结现场与端点核对

- [x] 核对Voice PID732503/SHA dcd233c8、管理PID635398/SHA6afbdc98、Caddy SHA adb62223、ready/memory_ready、0/2会话及诊断false。
- [x] 以发布pins核对并复制200项Go/mod输入到`/home/ubuntu/waveshare-native-integration-20261114/source`，根目录0700，独立records/ops与持久调用账本。
- [x] 以单次显式允许的协议探针连接目标模型，flat session.update获回执确认model/voice/speech_rate1/output pcm/searchfalse；没有response.create或测试语音。
- [x] 首探针已接受，无模型/账号拒绝；另以合成文字核对typed user/assistant create、assistant exact retrieve和两次delete ACK。加上音频准入/rate-only探针共3/8协议探针、累计主回答intent13/48（首条1＋复测12），不作语义/真人成绩。

## Task 2：最小原生主会话与隔离入口

云端文件：隔离副本内`internal/realtimelive/native_device_pilot_test.go`与对应保护测试，显式允许后才启动。既有产品文件保持200项摘要；临时二进制不加入Git。

- [x] 先以云端假WebSocket观察保护用例RED，再实现最小owner并GREEN/race：未允许不连接；正常多轮只创建一次主会话；响应/item身份不串；总48主要回答/8协议探针且未知不重试；取消后旧连接不再复用不确定历史。
- [x] 复用listener/input admission，主回答发送raw PCM；初次原生会话使用简洁固定规则，不逐轮送历史JSON。
- [x] 无已验证安全裁剪时，取消后关闭主连接；下一轮等待旧播放结算后，仅用确认的用户文字和已播助手文字按官方typed items重建，恢复是明确例外，不假称一直保留原音频。
- [x] 普通连续会话正常完成后保持；限制模型原生容量，受控重建保留可信记录，不能引用未知尾部。未确认协议或边界时不进入真人打断阶段。
- [x] loopback隔离入口只接受目标Device-Id，仍经正式Bearer/工厂鉴权和归属检查；禁用注册/清记忆/私有曲库等非本轮入口。memory=false、独立records、max_sessions1；不写生产用户记忆。
- [x] 先完成本轮定向保护检查、编译和规格/质量审查，再启动loopback实例并核对健康与拒绝非目标设备。

## Task 3：目标微雪临时分流

- [x] 核对live Caddy admin配置等于当前文件适配结果、正式Voice空闲，保存原字节/摘要与候选JSON。
- [x] 仅插入GET `/xiaozhi/v1/`且Device-Id精确匹配微雪的路由，目标为隔离loopback；适配结果须位于既有fallback之前，其他路由结构不变。
- [x] 经候选SHA/路由差异审查后原子更新并reload，正式Voice/管理二进制与进程保持，固件/设备URL/NVS不改。
- [ ] 用户手动连接微雪后核对真实鉴权设备、原生session ID跨正常轮次保持、输入和I2S播放身份、模型语音结果。最多48主要回答与8协议探针，不把未知当失败重试。

## Task 4：实机反馈、恢复与交接

- [ ] 首先普通聊天，用户实际反馈与转写/事件并列，技术完成与逻辑/自然度分列。再按预算做语速、已验证安全边界内的打断与恢复。
- [ ] 克隆须由用户指定参考样本后才创建，不利用现有真人历史音频；无样本则该项明确未覆盖，同时继续其他已授权检查。
- [ ] 保留错误、未知和缺测，不扩大为smart_turn_v2、长聊或全目标PASS。若接入拒绝，提供具体阻断事实及继续所需信息。
- [x] 测试结束或出现边界失败时，核对当前Caddy为本次候选SHA后恢复精确原字节及配置，关闭仅本轮隔离进程；不恢复用户数据、不重设基线、不删除旧证据。
- [ ] 更新去敏验证记录、设计状态与交接，commit/push main；报告已完成与尚需用户实机/克隆输入的部分。


## 现场边界失败后的定位

- [x] 首条实机失败后恢复原Caddy文件/admin、停止首轮隔离实例；保留intent、provider完成与drain_timeout证据。
- [x] 完成真实Opus慢消费者/冻结回执反事实；不改产品窗口或超时，明确模拟成绩不等于设备播放通过。
- [x] 完成固件只读审计；被动串口未改控制线/波特率，尚无唯一根因。
- [x] v8复用现有目标设备标量诊断；新健康断言RED→GREEN、定向普通/race、18项路由守卫检查及独立审查通过。
- [x] 使用新v3准备与新proof执行精确目标分流，不覆盖或复用已消费的v2 apply/restore意图。
- [x] 收集用户一次有界复现，结合RX/播放/心跳/串口和TCP头定位停滞边界；结束恢复原入口、停止隔离实例。
- [ ] 根因有证据后才实施最小修复，继续用户原来的整体对话/声音测试；不以增加超时掩盖停滞。


复测已结束：用户两段5＋7条有效输入，3答/4答复用正例；原停滞未重现，确认误输入与识别差异、中断后旧任务回流，另2次生成上限单列。临时v3路由恢复、实例停止、采集结束；下一步按验证记录的四项优先级推进，整体目标不记完成。克隆仍缺用户参考附件。
