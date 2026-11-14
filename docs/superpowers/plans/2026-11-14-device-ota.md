# 微雪管理页面手动OTA实施计划

> **For agentic workers:** 使用superpowers:subagent-driven-development逐项实施，独立规格审查后再质量审查。用户已确认手动OTA方案并此前授权更新微雪，不再询问相同开发许可。直接main，不创建分支/worktree；所有运行测试、构建、服务操作及私有资料在云端。

**Goal:** 从真实设备管理页面确认一次匹配固件的Wi-Fi升级，以新启动健康证据确认结果，保留失败/未知和回退机制。

**Architecture:** 既有管理服务中的私有固件/任务文件，现有控制worker顺带轮询升级端点，复用ESP OTA与双槽。独立升级协议避免扩展普通音量/亮度数值命令导致旧端不兼容；不加独立服务或依赖。

**Tech Stack:** Go标准库、现有Vue/TypeScript、ESP-IDF6.1/C++、既有HTTPS/mbedTLS/cJSON。

**Spec:** [方案](../specs/2026-11-14-device-ota-design.md)。基线e8e8271，app3522272B/SHA7770a7fb，两槽各3604480B。新增目标≤16384B，剩余≥65536B。

## 冻结协议

任务/发布ID分别为`ota_`＋32小写hex、`fw_`＋64位app SHA。摘要均64小写hex，boot32小写hex。字符串、数值、请求体和响应体均严格有界。

`GET /api/{family|admin}/device-firmware/{deviceId}`：

```json
{"deviceId":"dev_…","canUpgrade":true,"connection":"online","boot":"32hex","mode":"online","current":{"version":"2.5.3","elfSha256":"64hex"},"releases":[{"id":"fw_64hex","version":"2.5.4","sourceCommit":"40hex","size":3523000,"sha256":"64hex","elfSha256":"64hex"}],"job":null}
```

`current`可为null；`mode`为online/restart。没有新OTA能力的旧设备为restart，不伪造已知版本。`canUpgrade`仍要求当前设备管理授权及在线、无冲突条件；旧端允许提交“等待重启核对”的任务，实际发放镜像必须再验证旧开机元数据。

`POST`同路径，输入严格为`{"releaseId":"fw_64hex","boot":"32hex"}`；返回上述视图。再次提交相同进行中任务不创建新任务；不同任务409。job公开字段：`id,releaseId,version,status,progress,error,requestedAt,expiresAt`，progress可为null。status仅queued/sent/downloading/restarting/confirmed/failed/unknown；没有明确设备证据不新增“已回退”状态。

复用既有控制worker、同一boot，增加`POST /api/device/firmware`，不改变旧`/api/device/state`严格格式：

```json
{"capability":1,"boot":"32hex","seq":1,"version":"2.5.3","elfSha256":"64hex","layout":"waveshare-s3-16m-v1","slotBytes":3604480,"phase":"ready","jobId":"","progress":0,"error":""}
```

phase：checking/ready/busy/downloading/restarting/failed。ready仅本机启动确认和音频健康通过后报告；有语音、音乐、初始化或其他控制占用时busy。jobId为空或当前OTA任务；error为空或固定短代码，不携带URL/凭据/正文。seq为正安全整数且单boot单调。

响应严格为`{"command":null}`或：

```json
{"command":{"id":"ota_32hex","releaseId":"fw_64hex","version":"2.5.3","url":"https://robot.songguoxue.com/api/device/firmware/ota_32hex","size":3523000,"sha256":"64hex","elfSha256":"64hex","ttlMs":15000}}
```

设备下载GET同一任务URL，附现有Bearer设备凭据及Device-Id；拒绝重定向。旧固件首次bootstrap兼容下载使用仅该任务固定镜像的短时票据，不把设备凭据放入URL；票据不对网页公开。不把发放命令/HTTP传输结束记为已安装。

## Task 1：云端发布、受权任务与兼容bootstrap

**Files:** 新`internal/management/firmware*.go`及测试；改`server.go/onboarding.go`最小分派、`cmd/management`命令与参数；必要audit分类。

- [x] 先写HTTP RED：上述新路由当前404；有权限账号可查询并创建，其他家庭/只读拒绝，重启加载任务保持，重复/过期/旧boot/撤权/损坏存储不误确认。
- [x] 实现私有FirmwareRoot及同步/原子持久文件：`releases/fw_SHA/{manifest.json,app.bin}`、`devices/dev_ID.json`。有限发布数量与总量，不删历史事务，不跟随符号链接；无root时返回能力不可用，旧端点保持。
- [x] 发布CLI只接云端固定app/分区/元数据。验证ESP32-S3镜像、xiaozhi项目、实际版本/ELF/APP SHA、精确布局及两槽容量；版本发布后不可变，历史来源白名单用于首次引导。运行期只下发已封存验证的镜像。
- [x] 家庭/内部接口复用runtimeScopePermission，admin不绕过授权；创建、派发、下载、确认前刷新当前配置/绑定，审计请求和结果。持久任务有期限，不在超时后隐式重试。
- [x] 设备轮询/下载认证，boot/seq去旧；有新boot且目标ELF匹配的ready才confirmed。旧bootstrap仅对明确任务和匹配已知基线ELF/完整分区元数据发放一次；无任务继续不返回firmware。使用实际镜像版本正常比较，不永久force。
- [x] 云端Go完整普通451项、相关race189项通过（按UID拆分权限测试）；独立规格/质量审查PASS，提交73db540。见本轮验证，非服务已部署。

示例测试入口（云端源快照内）：

```sh
go test ./internal/management ./cmd/management -run 'Firmware|Runtime|Onboarding' -count=1
go test -race ./internal/management ./cmd/management -run 'Firmware' -count=1
```

## Task 2：固件受控下载、调度与启动确认

**Files:** 新`firmware/overlays/onboarding/family_ota*`、对应主机夹具/测试、共用OTA/应用补丁或准备接缝；改`family_control.cc/prepare_onboarding.py`最小worker调用。实际版本升为2.5.3，确保ESP app描述与发布相同。

- [x] 用实际生成OTA/应用方法建立RED：碎片读取、错误URL/状态码/长度/摘要/容量/镜像身份、忙/重复/旧任务、自检失败与未确认不能成功。
- [x] 同一控制worker在激活联网后顺带轮询新端点，复用boot和现有HTTP资源；不改变旧控制命令解析。升级本身由一个有界、非重复的临时执行任务完成，主任务继续处理UI和心跳；正常态无新增常驻任务。
- [x] 应用线程再次核对idle、无音乐/捕获/控制占用后预留升级，失去资格明确失败；停止旧音频/播放代次。只允许匹配该任务的固定HTTPS URL进入工厂OTA路径，旁路任意URL不能绕过校验。
- [x] 4KiB流式下载，现有mbedTLS SHA256，验证实际备用槽/精确长度/预期hash/ESP app身份，错误中止handle、释放资源、恢复音频。通过后才设置启动槽并重启。超时有界，不无限循环或自动重试。
- [x] 工厂S3的旧MarkCurrentVersionValid接缝改为等待本地健康，复用生命周期/已有tick确认主任务和新鲜音频DMA/输入；规定期限未通过则调用既有invalid+rollback，不提前确认。网络报到失败不能被当作镜像已确认；新报告由本地有效状态决定。
- [x] 云端真实代码提取的ASan/UBSan GREEN，音频/协议/时钟/音乐/控制回归；独立规格/质量审查后提交。

```sh
python3 firmware/tools/run_host_tests.py --archive /path/to/locked/xiaozhi-v2.5.2.tar.gz
```

## Task 3：两端手动升级入口

**Files:** 新`web/management/src/components/FirmwareUpgrade.vue`和小型controller，改`types.ts/remote-api.ts/api.ts/pages/CorePages.vue`；对应隔离测试。

- [x] RED：真实API解析、手动确认后才POST、重复点击、设备切换/退出/隐藏、超时未知、只读/离线、旧端需重启提示、只有confirmed成功。
- [x] GET展示当前实际身份与候选版本/大小/来源，确认步骤说明升级期间暂不可对话；POST只发releaseId+boot，不接受URL。单一状态区域展示设备回执，失败/未知不自动重试。
- [x] 复用现有页面和请求代次；持久任务重新进入页面仍能查看，切换设备的旧请求不覆盖新状态。
- [x] 云端网页测试/类型/构建GREEN；独立规格/质量审查后提交。

## Task 4：整包、发布与微雪实机

**Files:** `deploy/onboarding/Caddyfile`最小必要白名单；部署模板FirmwareRoot路径；本轮验证/交接；云端私有执行器不入Git。

- [x] 冻结输入到新的云端目录，复用前先验证原生缓存与音频候选2010项派生一致。重新准备固定源码/工厂来源后再顺序同步，不重写运行中的脚本。
- [x] 完整Go相关普通/race与网页/固件支持目标检查，完整独立审查；Waveshare原生编译、最终322+输入/Git/派生/编译/依赖/SDK来源证明，app新增≤16384B且余≥65536B。
- [ ] 从已验证实际运行管理release来源叠本任务差异，保留未部署/运行差异；准备可审阅的管理exe、static、私有固件目录及必要Caddy白名单，不修改Voice服务/记录/配置身份。实际部署前保护摘要，原事务/失败不删除或当成新基线。
- [ ] 云端隔离真实API/浏览器两端验证确认与权限，核对生成app后封存发布。main提交推送，实际服务按最小范围发布并核对健康/产物/旧数据保护；不把Actions或模拟客户端成绩冒充实机。
- [ ] 微雪精确身份、实际空闲、当前镜像/分区兼容及恢复资料核对后，优先旧bootstrap一次性OTA引导。需要人工重启时采集就绪再请求一次；不伪造初始boot或跳过基线校验。
- [ ] 验证真实OTA下载/切槽、新ELF及健康确认、重新在线、北京时间、按键聆听。故障恢复、物理断电和听感按实际观察保留待验，未观察不得记PASS。
- [ ] 更新验证/交接和计划状态、main提交推送，关闭本轮串口/转发/隔离服务，保留私有备份及全部失败证据。
