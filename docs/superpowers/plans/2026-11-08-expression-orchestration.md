# 表情编排与设备名称 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成用户已批准的首版表情闭环及按设备配置名称，保留已有脸部资源、L0/L2与实际DMA音频事实。

**Architecture:** 名称存储按鉴权device_id解析为新会话不可变Product快照；独立无声模型分支每轮最多选择一次情绪，Go持有并在合法播放开始后下发，固件按同一连接/播放ID的DMA时间应用和结束。旧客户端能力不足时不发送新指令、不新增模型请求。

**Tech Stack:** Go1.27.3、C++/ESP-IDF6.1/LVGL9、云端运行测试/构建/模型；Mac仅代码/Git与USB客户端。用户要求直接main；不创建分支或worktree。

## 执行纪律与基线

2026-11-08用户已明确“按照现在的表情方案开发”；后续询问还需做什么不取消该授权。采用分任务实现→规格审查→质量审查，root统一提交；实现子任务不自行推送、刷机或重启服务。全部runtime测试通过SSH在云端私有目录执行，先RED再GREEN；不用本机装服务或复制云端凭据。

当前main32baa67，设备2beda8f（完整app槽MD5 4ceaabaca8a82709080a402cc01b67cb），Go58c14cd-window600-chick-22e7d4c-r1。云端正式来源为`/opt/family-robot-validation/chick-face-20261108/go-release`，正式二进制来自58c14cd加窗口和视觉最小增量；发布新功能需将本次限定diff移植到该来源再验证，不整包替换为main。原设备600ms/其它300ms、鉴权、资源、分区和未验收唤醒实验默认关闭保持。

云端新工作根：`/opt/family-robot-validation/expression-v1-20261108/`。Go工具`/opt/family-robot/toolchains/go1.27.3/bin/go`。固件host的可选ASan加载覆盖：`FIRMWARE_HOST_ASAN_PRELOAD=/usr/lib/gcc/x86_64-linux-gnu/13/libasan.so`。私有传输使用Python tarfile与git ls-files -z，避免AppleDouble和中文路径转义漏传；缓存锁定归档，不重复从GitHub下载。

## Task 1：设备名称与会话快照

**Files:** 新建`internal/deviceprofile/store.go`、`store_test.go`、`cmd/device-profile/main.go`及测试；修改`internal/realtimestate/identity.go`及测试、`internal/realtimelive/config.go`、`server.go`、`xiaozhi_entry.go`、`memory.go`及相关测试、`cmd/realtime-output-live/main.go`。

固定资料合同：

```json
{"schema_version":1,"profiles":{"dev_0123456789abcdef0123456789abcdef":{"name":"豆豆","revision":1}}}
```

提供`Open(path) (*Store,error)`、`Resolve(deviceID,defaultName string) (Profile,error)`、`SetName(deviceID,name string) (Profile,error)`；Profile包含Name、Revision。Store锁保护同进程缓存；管理写入在文件锁内读最新版本后原子替换，保留其它设备资料并增加该设备revision。读取异常保留上次有效缓存；没有有效资料回默认并返回可记录的原因。设备ID采用现有dev_32hex合同。名称trim后1–12Unicode字符、有效UTF8、无控制符/换行；错误不得修改文件。非正规文件/重定向路径失败，不更改鉴权注册表。

Product增加只读`Name()`和返回新实例的`WithName(name)`；原Product不被修改，raw身份JSON中name也同步，历史名称作为历史对待。Server从可选`Config.DeviceProfilesPath`初始化资料Store，给鉴权会话解析设备名称；Mac有鉴权device_id时同样隔离，无身份则用默认。`--device-profiles`配置路径默认空，保持旧行为；空/缺省名称保持配置中的默认name。新会话采用新资料，活动会话不变。CLI提供get/set，输出name/revision和effective_wake_word=unknown，不伪造设备回读；JSON/身份只留云端私有文件。

清记忆解析增加当前名称参数，调用点来自session.product.Name()；保留旧问题/引文/复合请求拒绝及明确命令合同，不把常规聊天变成删除操作。先认完整裸命令；带配置名称时要求名称后有中文/英文逗号再剥离，避免“不要”“他说”等合法名称把原否定/转述变成删除。

- [x] 写行为测试，包括原Product不可变、两个设备不同名称、改名后旧Session不变/新Session更新、坏文件保留旧缓存、并发写不丢资料、错误名不写、名称前缀清记忆及旧拒绝用例。最小意图断言：

```go
base := loadTestProduct(t)
renamed, err := base.WithName("豆豆")
if err != nil || renamed.Name() != "豆豆" || base.Name() == "豆豆" { t.Fatal("identity snapshot mutation") }
```

- [x] 将仅新测试加当前源传云端，运行对应包并保留预期缺失实现的RED；随后实现上述API及会话挂接。
- [x] 云端`go test -race ./internal/deviceprofile ./internal/realtimestate ./internal/realtimelive ./cmd/device-profile -count=1`，相关vet/build通过；检查默认设备、名称错误与CLI输出。规格/质量分开审查后root提交。

## Task 2：固件实际播放时间与绑定情绪生命周期

**Files:** 修改`firmware/overlays/main/audio/orb_activity.h`、`playback_progress.h`、`display/chick_face_model.h`；新建`display/face_expression.h`及`firmware/tests/face_expression_host.cc`；修改`chick_face_host.cc`、`audio_activity_host.cc`和对应Python测试包装器。

在现有播放记录中发布UI需要的首个DMA完成、最后实际完成时间、终结事实与播放身份。ISR只作有界标量更新；不绘图、不增加PCM分析或新锁。C3未选CHICK时布局保持；CHICK若因时间标量增加少量固定SRAM，精确测量并以配置条件更新静态预算，禁止增加PCM队列或内部RAM画布。

新纯C++控制器以`OrbOutputActivity::Identity`作为轻量身份（与PlaybackIdentity相等比较兼容），接收规范FaceEmotion和到达事件，采用最多现有kRecords的有界槽；每身份最多一次生效。FaceModel新增`SetPlaybackEmotion(OrbOutputActivity::Identity,const char*,int64_t)`及清理入口供后续View转发，不向模型头引入包含PCM队列的ledger头。控制器由UI任务消费；表达阶段包括Pending、Active、PostSpeech、Retired。FaceModel保留无绑定SetEmotion的5秒行为；新增有身份事件入口，绑定事件按实际同ID DMA开始生效，开播2000ms后或终结后到达拒绝。长回复不受旧5秒截断，正常结束以实际最后DMA时间起最多2秒；新状态可提前接管。新回复首帧没有其标签时不得复活旧主情绪。旧尾音排空保留已显示旧情绪，新标签不得跨ID提前应用。

- [ ] 写真实时间线测试，至少覆盖以下具体输入：

```cpp
// Pending happy for {connection9,playback4}, while physical3 still drains:
// frame3 retains only its own mood; first real4 completion applies happy.
// +6000000us while4 still plays retains happy; final stamp then +2000001us clears.
// Arrival at first_dma+2000001us rejects; stale connection and duplicate reject.
```

另测hidden/息屏跨结束后不重计2秒、页面/异常/断连失效、未绑定5秒、neutral占用本轮一次选择、起止时间回退和无效观察不驱嘴型。
- [ ] 先云端RED，再实现并云端ASan/UBSan GREEN；旧播放记账、队列/取消、UI文字优先及渲染内存回归通过。
- [ ] 两阶段独立审查，root提交；记录实际类型尺寸和未做的物理结果。

## Task 3：固件协议和显示接线

**Files:** 新增`firmware/patches/0023-bound-expression.patch`；修改`audio/playback_websocket.inc`、`display/chick_face_view.h/.cc`、`chick_face_display.inc`、`firmware/tests/playback_websocket_test.cc`、`chick_face_display_host.cc`、`chick_face_view_host.cc`、`test_chick_integration.py`、`firmware/tools/verify_experiment.py`相关证据断言。

补丁只修改原生/vendor文件，overlay文件直接改。以32baa67的完整准备树作baseline，新补丁不得包含原overlay的diff；保留0022及默认关闭的0021开关。CHICK hello声明face_expression_v1；服务端确认后接收带session_id/playback_id/emotion的llm。协议锁内校验当前会话、连接代次、合法已开始ID和规范情绪，生成不可变事件；主任务应用前重验代次与播放有效性。正常tts.stop不能当实际完成，drain/取消不接受新旧标签；当前已显示旧尾音作用可至实际尾音完毕。校验入口不要返回未锁的session_id引用。

Display新增默认no-op的有身份情绪入口，CHICK实现转发纯控制器；旧客户端/非CHICK路径保持。L0三处含名文案替换为“正在启动”“正在说话”“有新提示”，同步断言和有效方案。通用情绪legacy接口保留原映射但不能污染新的绑定轮。

- [ ] 在生产方法host中先测协商、错误scope、非整数/旧/未来ID、接收→主任务之间close/reopen、已封口仍物理播放、drain拒绝新标签；观察旧代码RED。
- [ ] 实现接线，准备新的全补丁树，以`git apply --check --whitespace=error-all`验证；云端跑新协议、实际View、模型及旧播放/唤醒回归，获得GREEN。
- [ ] 分别规格/质量审查后root提交。不得为验证表情重新启用未验收唤醒窗口。

## Task 4：Go情绪选择、编排和能力协商

**Files:** 新建`internal/realtimelive/native_expression.go`、`session_expression.go`、相应测试；修改`config.go`、`session.go`、`xiaozhi_wire.go`、`xiaozhi_entry.go`、`cmd/realtime-output-live/main.go`及协议测试。

配置`ExpressionsEnabled bool`与`--expressions`开关默认false，发布验收通过后再对具备face_expression_v1的设备启用；新能力与face_state_v1独立。Native情绪分支只定义set_expression工具，clone配置关闭搜索/只用文字工具模式；2秒总ctx包括握手/生成，只发送一次response.create，不循环、不重试、不输出音频、不调用设备Bridge。依据当前转写、设备名称和有界已播背景选择15枚举之一；若已有本轮助手前缀可带入，但不能等待它。只接受终态完成的一次合法调用；记录耗时/使用量/默认原因及一次请求计数，内容沿用现有云端私有记录边界。

Session在settle上下文和当前转写就绪后并行启动一次，不加入语音关键等待路径。测试可注入selector函数代替网络；生产默认用新的Native方法。liveTurn由Session.mu管理待选结果、已发送标记，生命周期沿用turn.ctx取消。合法begin写出后、首段音频前发送已准备情绪；晚到时只在仍当前/未Cut/未PlaybackFinal的同一轮发送一次，pending阶段只持有不发。wire验证该ID已begin、未排空，发llm+session_id+playback_id+emotion；legacy未协商设备无新增消息/调用。流控计数、300/600ms选择器、TTS事件及PCM/Opus不改变。

- [ ] 先写并云端运行RED：selector一直阻塞时首包仍可发；已准备标签排序start→emotion→audio；晚到一次；旧轮取消/新轮/连接替换拒绝；legacy零调用；多个工具/未知情绪/超时neutral；主语音cfg与搜索不被修改。
- [ ] 实现上述方法；用本地WebSocket提供方fake但实际Native序列测2秒取消/恰好一次response.create/no audio配置，云端定向race GREEN。
- [ ] 云端全仓`go test -race ./... -count=1`、`go vet ./...`、`go build ./...`；两阶段独立审查后root提交。

## Task 5：固定真实模型试次和对照

**Files:** 新建`tools/face-expression-check/main.go`、说明与测试；新建`docs/verification/2026-11-08-expression-orchestration.md`；预注册场景清单为`docs/verification/fixtures/2026-11-08-expression-scenes.json`（只含合成提示/可接受集合，不含客户录音或身份）。

工具读取云端受保护配置，不复制密钥到Mac；执行默认不推理，显式--run才调用，独立私有输出。每例选择器最多一次，保留失败/超时；变更后标版本再运行，不能覆盖原结果。20场景覆盖数学/普通问答、中性、祝贺/运动目标、感谢/夸奖、鼓劲、澄清、放松、意外、食物、游戏、sad/sleepy/angry/crying剧情、长祝贺/讲解、记忆话题和改名自称；每例在评分前列出可接受集合。强制故障另用可控fake，不算正常模型通过。

- [ ] 在任何真实模型调用前提交清单及工具测试RED/GREEN。固定耗时、合法输出、语义/窗口生效、默认原因和token统计字段。
- [ ] 云端用新源码执行20例，数值与原记录独立核对；正常试次所有必测项通过才进入发布，超时或语义不符如实保留并修复复验。
- [ ] 用固定输入进行分支开/关交替语音协议回放，记录首包分布、完整音频和打断；语音不得等待选择器。模型调用计数与并发上下文回收核对；不新增真人声纹成绩。

## Task 6：完整审查、最小发布与目标固件

**Files:** 活跃README/handoff/spec/验证记录；发布脚本和产物仅云端/本机ignored私有目录。

- [ ] 完整方案对照与质量两阶段独立审查，检查所有规格项均有实现和新鲜测试证据。
- [ ] 对实际部署来源核对manifest/hash/配置，复制到新私有release来源，仅移植本次功能/测试diff；验证名称/选择器/旧客户端/窗口race及vet/build。原二进制/配置备份、失败回退和ready/memory_ready检查具体就绪后，在0活动会话发布。
- [ ] main推送后等待精确SHA五目标CI和原版基线通过，下载微雪artifact并核验ZIP及所有文件摘要、配置、无lab、分区/原资源兼容。新表情能力只对新版协商设备生效。
- [ ] 按MAC与原2beda完整槽MD5核对，app-only安装，回读和app外/NVS保护通过；观察初始化、实际绘制开销与低亮，避免重复朗读。
- [ ] 现场最少自然问答/祝贺/插话确认，记录通过和未通过，失败修复再验；未取得的物理结果保持待验，不用host成绩替代。
- [ ] 关闭临时客户端/观察；健康、零诊断、保护配置及Git同步核对，更新交接并提交main。最终报告代码、服务/设备版本、用户视觉反馈和实际限制。
