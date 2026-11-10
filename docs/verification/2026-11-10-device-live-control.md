# 待机在线状态与音量、亮度控制

2026-11-10。用户明确要求待机也可控制，并授权补控制通道、升级微雪。源实现已提交推送main：后端81ace2c、共享固件f9d568d、小程序b1f925f；首版冻结`b1f925faf4f0a7350bdc3d3d219e865de862ef1d`，实机暴露的连续心跳修正后最终固件冻结`c7b02b7f1cf0acb85d1ad1705c42a72377c790b6`。后端和小程序组成文件在两提交间相同。本文件后续文档提交不改变固件代码。设计和约束见[规格](../superpowers/specs/2026-11-10-device-live-control-design.md)、[计划](../superpowers/plans/2026-11-10-device-live-control.md)。

## 已实现

- 已activated设备复用工厂认证POST精确`/api/device/state`；20秒新鲜心跳才在线。严格字段、boot/单调seq、退休boot与有界RAM状态隔离重放；管理重启后等真实新心跳。
- 家庭GET/PUT精确`device-runtime/<deviceId>`复用当前设备授权；仅当前管理成员能提交。每设备一个45秒命令；下发时在锁内重读当前actor、绑定及工厂状态。撤权、换绑定、过期或重启不能确认成功。审计失败不排队。
- 两S3同一HTTPS worker，5秒正常轮询、5秒HTTP超时、失败退避至30秒；证书校验、禁跳转、2048字节响应上限。Application线程操作codec/背光，读到目标才发送applied；命令去重、15秒执行截止、单个待处理快照防止无界队列。
- 小程序首页/详情显示真实心跳状态，设置提供音量/亮度独立草稿和按钮。queued/sent只显示等待，confirmed且实际值匹配才成功；50秒独立截止。前台10秒刷新，隐藏/退出/401隔离晚到请求，不覆盖昵称/位置草稿，不自动重发新命令。

## 云端验证

所有安装、运行测试、构建、固件及原始设备日志均在云端；Mac仅代码/Git、USB字节中继及微信客户端。未新增产品依赖或服务。

- 后端先完成完整management race 272.057秒；之后撤权锁竞争修正有确定RED与TestRuntime race 39.238秒通过。最终b1f925f精确HEAD再跑完整`go test -race -json -count=1 ./internal/management ./cmd/management`：management 84顶层/240含子测、242.192秒；CLI 9顶层/29含子测、10.658秒，均0失败/跳过，总245.231秒。相同两包vet 0.578秒/build 1.459秒通过。35份源码测试前后均对应HEAD，manifest `154231a02aea8eb9c550dd7823c01bdd201edf506fcbf8e94414a7f4c7d724c4`。
- 首版r1固件完整157项host测试，0失败、0跳过，109.720秒通过。实际固定cJSON1.7.19夹具覆盖转义NUL别名；执行超时后已相同目标不能误报成功、100次阻塞快照只保留一个回调均有RED→GREEN。最终r2为下文158项结果。
- 小程序140/140、typecheck/build、原生WXML绑定与官方Security2向量通过；微信官方CLI实际预览成功，包164185字节。Mac既有原生测试工程已更新。官方编译不等于手机操作通过。
- Waveshare及M5原生build通过。独立比较1863份生成源码/默认配置/依赖文件；各自97/98份项目编译单元归属核对。实际配置均ESP32-S3、PSRAM普通分配优先、安全版本2、自有HTTPS引导；仅板级适配不同。

首版r1冻结产物只在`/home/ubuntu/device-live-control-20261110/evidence/`。微雪app 3474464字节，SHA256 `208c1989aa8344c7c961ffa049827d889ebe15933bf54e2f4fb7646511221f0a`，manifest `675157634a5e890efffb6d0149fdbb52b9f3e956d47dfd998d6971658625db9f`；M5 app 3487936字节，SHA256 `696e40b310ac252c915a6235820b88b80535feccc80972959dc0b8d3e4013d83`，manifest `606398d9cbcd3f528739a95549301d4d0b50fd7f997cddcb41e97820bcb3aafe`。最终r2另在evidence-r2并列于下文；M5物理设备没有连接、刷写或联调。

完整host首轮因ASan加载次序和测试夹具路径、NVS测试包版本不匹配失败，修正测试环境后全157项通过；保留所有失败日志，未修改系统`/etc/ld.so.preload`。M5缓存分区表生成文件未随CSV更新，强制重新配置/重新生成后，最终实际分区表及产物核验通过。

## 已发布服务

管理release `standby-control-20261110-r1`，PID200267，实际二进制SHA256 `7e7e43819cbec5564eb019fe41730c96d1d1d1fbb14ab0669f644599b50cdce7`。既有网页静态全集保持；Caddy只增加设备state POST及家庭runtime GET/PUT精确路由，PID2415423保持。公网设备POST未认证401、GET404、额外路径404；家庭runtime GET/PUT未登录401，admin和无效路径仍404。

隔离候选复用正式授权只读核对微雪聊天，再仅在私有克隆配置中测试heartbeat→queued→sent→confirmed/actual45及退出401；未给正式服务制造假设备心跳。正式config/profiles发布前后原摘要，家庭身份/角色/绑定/历史保持。证据在云端私有`/var/backups/family-robot/device-live-control-20261110/`。

语音服务未改未重启，PID3256763、二进制SHA256 `df5386e590e9c0d1de790bd77519da001e2bfa8afa8f2f6b720a5b9e5ba17c65`保持。此次控制通道不创建模型语音会话，也不改变统一600ms播放窗口。

## 微雪安装与真实回执

同MAC `44:bd:8d:79:1f:e8`、S3rev0.2、16MiB、安全启动/Flash加密均关闭。先读取当前完整16MiB，硬件MD5与实际字节SHA256核对；备份`2f3a1f233d6e23f1a1eb578b61b5e0137fe0f073aa888abbf6685d1aa61ee1d6`只在云端root0700/0600。当前activated1及工厂八项、NVS/OTA/实际活动槽校验通过；原family-home/binding-home-1/version1及2026-11-09 10:05:08起点保持。

第一次写前预检拒绝SDK 3072字节分区表与Flash 4096字节扇区直接比较，尚未创建write-intent、没有写入。修正只接受3072/4096字节、3072补FF后完整比较，非FF尾部及非法长度继续拒绝；新增失败夹具后升级/启动工具31项通过，独立复核通过。原拒绝日志保留。被动采集工具另6项fake测试通过。

首次实际写入仅活动ota_0（0x20000/0x370000）应用槽，3604480字节完整回读逐字节通过；槽外硬件MD5及整片硬件MD5通过，启动前预期整片SHA256 `59085bf302d2f5fea99daae0813a625aabb0be84b12a33cad776ac6d9c0d9ed8`。NVS、OTA选择、PHY、assets和storage在写入阶段保持。

重新核对整片硬件摘要后，只执行一次官方S3 watchdog_reset；USB按预期断开，工具仅在复位阶段接受SerialException，没有重复写入或复位。重新识别同MAC USB后以DTR/RTS=False重开字节中继，启动单次300秒云端被动UART采集。

首版实机验证失败：首次真实鉴权心跳在14:54:20Z到达，音量70；随后没有有效新心跳，测试命令65未获得应用回执、45秒后unknown，无confirmed，不记控制通过。首次PUT前已持久保存原值70/boot/目标/恢复待办，失败证据保留，不自动重发或把偏移值当新基线。

已定位冻结ESP-IDF中`esp_http_client_set_post_field(nullptr,0)`会删除Content-Type；下一轮缺失头的getter返回NOT_FOUND，setter提前返回，worker忽略返回值继续请求，后端严格JSON解码会拒绝缺失类型头。该代码路径确定，与首次成功后不再更新吻合；无现场HTTP状态码日志，不冒称已捕获400。初版回归fake getter把缺失头返回OK，错误模拟了fallback form，独立复核发现后纠正为实际NOT_FOUND语义；该初版测试已作废。

c7b02b7仅移动JSON头到每轮请求体设置前，保留清指针。最终测试抽取SHA锁定SDK的真实getter、setter及产品Worker：原代码第2轮缺头RED，修正后三轮JSON/清指针GREEN（3项1.506秒）；完整158项host零失败/跳过67.628秒通过，独立Spec/Quality复核通过。SDK主文件SHA `b28179259684e4cadbcaff00127ac50ea5c4477536cca1b4df86590694acdf24`、getter文件SHA `23056c41505aa38885cf210e22117e1fa57d964080ff537a36a5c9d99d76701e`。

第二轮从Git archive冻结261份固件源输入，manifest SHA `e3f993aba930c7affec2c25abdeffaa8f5bf78b161c370259dc13c17e0c4ac83`；全新prepared-final-r2与两实际构建树1863项独立比较，97/98编译单元及实际配置/分区/镜像通过。只变3个实际构建输入（worker和两份来源元数据），两板原生构建通过，分区表/sdkconfig/assets均与首版逐字节同摘要。微雪app `73f624ffcfa0c124b6d525c9fb48c7d1e8b60f8ee81e9d0104d03beff9ca3f81`、3474464字节，manifest `18c8c22d16589d6448b66118d48df71679e4ac03cb9dbcf4610f5f74b90396bf`；M5 app `f6acc0ec5c8c72ba8e7e22fcbbd9780cfb2287c5bc4adc056adb634f7da67661`、3487936字节，manifest `6827197e2f94919ee14d16f403404d7df6b7fb0d205cfefc53eb6c7fe7b56070`。

第二轮使用独立ops-r2与新root私有目录，旧manifest及PENDING pins均拒绝，升级/boot/capture fake测试42项通过；旧快照只用于来源/分区兼容预检，不当作本次安装快照。新完整16MiB已实际读取并通过硬件MD5/分区/NVS身份校验，SHA256 `59085bf302d2f5fea99daae0813a625aabb0be84b12a33cad776ac6d9c0d9ed8`，与首轮安装后预期整片一致；不是复用旧文件。

R2只写同一活动槽，3604480字节完整读回逐字节通过，槽外及整片硬件MD5通过，启动前整片SHA256 `595d803760de646bbc9cf1db7002e6be7e0f738eaff7704ecc870424ca7616be`；工厂身份/绑定及槽外保持。重新核对整片后只执行一次官方watchdog启动；未为后续诊断再次刷写或主动重启。

第二轮最早控制也未确认：15:18:28Z心跳的boot为bd3c，后续当前boot已变为4538，旧命令按启动隔离规则unknown。受控启动意图为15:18:17Z，被动捕获15:18:34Z开始；未观测窗口内的启动标识变化原因未定，可能与USB接入有关，不能断言固件自行重启。300秒捕获设备时间8473→305863ms无大幅倒退，仅一次联网/idle/AFE初始化；无重启、watchdog、panic、Brownout或NO_MEM证据。

为区分网络错误，临时按[Caddy日志规则](https://caddyserver.com/docs/caddyfile/directives/log)只记录精确state路径状态码、固定JSON布尔标记，删除整个request、响应头和用户字段，其他路径跳过；无凭据/正文日志。首次validate以root创建日志导致服务用户无写权限，reload失败后原配置自动恢复，失败记录保留；修正日志归属后诊断成功。观察55次设备200/JSON请求、约5秒间隔；唯一401是自己的无凭据探测。诊断已撤回，磁盘与实际运行Caddy配置均确认无诊断logger，原配置摘要恢复，PID不变。随后真实控制验证在原部署配置下进行。

等待同一boot连续三个不同心跳后，15:28:52—15:29:33Z实际闭环通过：

| 字段 | 验证动作 | 结果 |
| --- | --- | --- |
| 音量 | 70→65→70 | 两条独立命令均confirmed，ack实际值及最新设备读值匹配，恢复70 |
| 亮度 | 待机自动调暗20→70→75 | 修改及恢复均confirmed；新备份未存display亮度键，按原驱动默认偏好75恢复，未将自动调暗20误存为用户偏好 |

四条命令的ID、同boot校验、实际值、确认时间及恢复状态只在私有证明中；测试前先读回70，确认此前失败未改变原音量，不重放旧命令。前后语音active_sessions均0，未唤醒机器人或创建模型会话。原失败证明继续保留，不改写成PASS。

完整R2被动观察300秒/510968字节，30个内部堆样本，末次140079字节，历史最低36152字节；panic/NO_MEM/watchdog/大幅时钟倒退均0。AFE短轮询的既有ringbuffer空/-1日志不是此处的内存不足判据，未为本任务修改AEC，也不据此声称唤醒或声学通过。

USB字节中继、串口占用及SSH反向42217已关闭。15:31:29/34/39Z再次收到同一4538启动的三个真实心跳，音量70、亮度75，证明清理后仍在线。管理200267/语音3256763/Caddy2415423及两二进制摘要保持；config、profiles及旧网页静态全集均原摘要，ready/memory_ready、语音active0。原家庭/绑定起点保持，没有重新注册、删除历史或修改模型/600ms播放策略。

最终私有证明在`/var/backups/family-robot/waveshare-control-20261110-r2/`的`control-stable/real-control-proof.json`、`final-proof.json`、`stability-proof.json`；R1和R2最早失败、备份、意图/整槽回读、原始UART及诊断元数据完整保留，只在云端root0700/0600。测试Cookie只在RAM且均退出。

禁止用旧activated0 NVS、旧整片镜像或旧完整config回写，不对退货M5执行此工具。

## 待现场验证

手机新版首页/设置滑块、后台恢复、权限提示与实际扬声器听感/屏幕观感仍待用户回执；本轮不把自动化、驱动读值或无会话待机观察当作声学/插话通过。官方CLI最终再次生成164185字节预览；现有模拟器窗口的登录提示不作为本工程页面渲染证据。原家庭账号和密码保持，管理服务重启后需重新登录；不需要重新扫码绑定微雪。
