# 微雪 A/B/C 三链路最小接入与对比计划

> **For agentic workers:** 使用 superpowers:subagent-driven-development，TDD后先规格、再质量复核。用户已明确把ABC一起对比，沿用原A/B设计；直接main，不建分支/worktree。运行、依赖、构建、提供方调用仅云端；现场分流由主代理操作、先告知。

**Goal:** 在同一个微雪公共会话上显式选择A/B/C，完成能辨认实际链路的基础对话、播放、中断及用量对比。

**Architecture:** 仅新增默认不构建的云端现场测试资产，复用已验证voicecompare.Client和公共Server/Session。每次连接固定一条路线，断开后切换；不创建生产多后端框架、不改固件。A/B先复用现有完整回答/TTS后播放的批处理实现，C保留直接语音流式；必须报告此实现差别，初轮不将首包差异当架构最终排名。

**Tech Stack:** 现有Go、标准库、gorilla/WebSocket、voicecompare、realtimeaudio、realtimestate及原路由操作器；无新依赖。

**16:24现场修订：** A四答完整played、用户听感正常；B三次因TTS把`～`作为句界而失败；C第一答被自身声音“我叫小”误触发中断，用户确认从未插话，额外回复用完测试器的4次计数，提前server_shutdown。此前“每路线4次模型调用后自动停止”的安排作废：仍以各4个人工问题为目标，由用户文字确认完成后主代理停止，保留9分钟窗口/10分钟实例保护；单会话改为8分钟，避免排查/阅读中3分钟提前结束。持久账本不退款，历史protocol9/primary49（含A4/B3/C4）全保留；安全上限改为每路线12、阶段36、累计74（38原历史+36），不是新的费用目标或自动要跑满36答。旧48工具默认不改，也不再因助手自设的小额开发门槛询问预算。

最小修复只把`～/~`加入现有TTS分句符，严格原文/任务/最终时间戳验证保持；C先补短预览的私有拦截条件快照，不猜测原因、不扩大回声阈值、不关闭真人打断。旧A-v2/B-v2/C-v2均已恢复，补测须新v3来源/binary/proof/intent，不能复用旧窗口。下方12答/4次自动收尾/180秒/50上限为首次设计与已执行历史，由本段及Task3覆盖。

## 已确认的比较设计

用户原定A/B及当前C试验均保留，现明确将C加入正式对照对象：

| 标签 | 实际主链路 | 初轮输出方式 |
| --- | --- | --- |
| A | Omni3.8原始语音理解→回答文字→CosyVoice v3 Flash/longanyang | 已验证完整TTS及最终字词后开始播放 |
| B | Fun-ASR→Qwen Plus非思考→同一CosyVoice/音色 | 与A同策略 |
| C | Omni3.8→直接语音，使用原配置音色 | 原公共逐轮直接语音流式 |

固定同一支持中的微雪、Opus、音量、初始背景、中文短答规则；各路线独立会话/记录，不互相回灌。A/B同TTS型号/音色/默认语速，C音色不同单列评价。三边首轮统一关闭工具、点歌、表情、搜索、跨会话记忆和声纹，只比较主对话；公共输入监听和OutputASR保留，费用计入。原C带辅助功能的四轮记录是独立证据，不与新条件混算。

本轮每条4个主回答，共12个；顺序A→B→C，定位为初筛，不能证明长期质量、稳定P95或最终月费。建议：身份介绍；提供一个偏好并要求据此推荐；追问推荐原因；简短故事（可在本轮中途说停，停止不补发第5答）。完整“打断后续讲”须另轮验证，首轮没覆盖就明确未测。

既有账本协议9/主回答38全部保留。本次三链路新阶段最多追加12主回答，累计止于50，单路线4、阶段12同时限制；旧工具默认48不改，不清历史、退款未知或复用attempt。这是主回答边界，不能当成所有ASR/TTS调用的总上限。必要开发测试不再以助手自行设置的小额金额门槛反复请求确认。

成品费用仍按30分钟/天、约900分钟/月比较：分开记录模型文本/音频token、ASR时长、TTS计费字符、历史、辅助API、取消/失败/未知。先给本轮实测用量，缺账单或模态细项保持未知；批处理/短答不能直接外推流式产品月费。音色克隆、3.5音色及生产迁移不包含在本轮。

2026-11-15复核北京区公开原价：[Omni3.8](https://help.aliyun.com/zh/model-studio/qwen3-8-omni-flash-realtime)文本输入/音频输入/文本输出/音频输出分别1.5/6/4.5/12元每百万Token，C的音频与对应文字分别收费；[CosyVoice v3 Flash](https://help.aliyun.com/zh/model-studio/cosyvoice-v3-flash)为1元每万计费字符；[价格表](https://help.aliyun.com/zh/model-studio/model-pricing)中Qwen Plus非思考≤128K为输入0.8/输出2元每百万Token，fun-asr-realtime和公共qwen3-asr-flash-realtime均0.00033元/秒。以代码实际payload及usage为准，B与OutputASR实际使用fun-asr-realtime；不把简称Fun-ASR误套为录音模型fun-asr的0.00022元/秒。免费额度/活动价/缓存折扣及账号账单另核对，不当作实际扣费结论。

## Task 1：显式三链路现场资产与必要计量

**Files:** 新建`tools/tts-playback-pilot/abc_comparison_test.go.txt`；若C缺分模态计量，只最小修改`internal/realtimelive/native.go`及对应测试。

- [x] 在资产内先写合成保护测试，显式复制到云端`internal/realtimelive/abc_comparison_pilot_test.go`，运行RED。场景：精确opt-in前无文件/网络；A/B/C枚举拒绝其他值；loopback/唯一MAC/鉴权owner/Opus；4/12/50持久上限、重复/破损/未知不重跑；取消不发声音；失败不降级到C；A/B同TTS；来源完成前不emit音频；各路线记录明确；字词对齐不改公共历史。
- [x] 实现一个三分支适配，复用`Session.generate`，不增加产品配置或自动路由。Serve精确gate为`FAMILY_ABC_PILOT_ALLOW=waveshare-abc-20261115`，路线由`FAMILY_ABC_PILOT_ROUTE=A|B|C`固定；loopback18818、单并发、会话180秒、实例10分钟，SIGTERM/结束CloseActive。

```go
switch route {
case "A", "B":
    backend := "native"
    if route == "B" { backend = "cascade" }
    result, err := client.Run(ctx, backend, prompt, pcm, "")
    // 完整验证成功后，先保存结果/字词元数据，再emit text/audio。
case "C":
    // 使用同样短答约束与已播历史，复用原generateNative。
}
```

- [x] 三边使用同一简洁主提示和有界已播历史投影；只用Turn.Prefix/PlaybackEvidence，不能把未播生成全文当历史，不用串行工具/音乐改变背景。保留真实产品名，B模型身份为Plus。输入最长10秒、回答120字/30秒音频，超限记录失败，不截断冒充成功、不自动重试。
- [x] 全局primary intent先fsync，再调用提供方；A/B每次BeforeCall创建独立私有intent并落盘，AfterCall记录usage/失败/取消，unknown不能当零。JSON事件/原音/结果仅云端0700/0600，不打印正文或凭据；Call.Stage明示native/asr/llm/tts，TTS分句可能多次调用。
- [x] A/B保存本轮源文本、PCM样本数和最终Words，结束时复用固定TTS的lookahead/120ms规则做只读对齐审计。公共中断/排空/OutputASR上下文保持；本阶段不把尚未验收的TTS映射写进产品历史。
- [x] C若现有usage只保留顶层，补数值白名单内的input/output_tokens_details（text/audio/cache等必要计费字段），拒绝字符串/任意对象，不记录额外提供方payload。先RED再GREEN，旧计数保留。
- [x] 云端普通/race/vet/build；规格再质量复核，冻结来源与二进制。默认Serve全部skip，合成检查不得触发真实API或设备。

## Task 2：新现场入口与真实对比

**Files:** 云端私有`/home/ubuntu/abc-voice-compare-20261115/`；更新资产README和公开验证记录，不把服务器操作脚本、凭据、原音或二进制入Git。

- [ ] 重新核对正式服务/7配置/Caddy file/admin和原账本，按每条路线fresh prepare、health/source/binary/PID proof，单设备apply。沿用已验证operator，不复用已消费的parallel/fixed-TTS意图。
- [ ] 开窗前明确告诉用户正在测试A/B/C哪一条、实际模型和声音；用户就绪才开放，不在准备期间消耗窗口。每路线4主回答后等待硬件终态，再停止对应临时实例和guarded restore；不自动把下一次连接切到别的模型。
- [ ] 先A，再B，再C，分别记录用户听感和真实首包/播放/中断/上下文/usage。任一路线因真实协议失败，保留错误并修实际原因，不伪称已测完三条，也不改用C顶替。
- [ ] 核对全部恢复、停止本轮采集，更新handoff/设计/计划和验证。main提交推送；正式部署另作决定。

## 云端检查命令

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go test -count=1 ./internal/voicecompare ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio ./internal/devicetools
/opt/family-robot/toolchains/go1.27.3/bin/go test -race -count=1 ./internal/voicecompare ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio ./internal/devicetools
/opt/family-robot/toolchains/go1.27.3/bin/go vet ./internal/voicecompare ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio ./internal/devicetools
/opt/family-robot/toolchains/go1.27.3/bin/go test -c -o /home/ubuntu/abc-voice-compare-20261115/abc-pilot.test ./internal/realtimelive
```

测试日志必须包含RED原因及GREEN结果；纯文档不重跑运行时测试。核对仓库实际编译输入与云端一致，私有重放及其他现场资产单列。完成标准是三条真实链路可分别接入并有可核验的现场结果；只完成代码时不能标成三链路已测完。

## Task 3：按真实失败最小修复并补测

**Files:** `internal/voicecompare/tts.go`、`client_test.go`；`internal/realtimelive/session_input.go`、`session_echo_test.go`；既有ABC资产及云端私有v3控制器。

- [x] 以三份B实际请求/返回分句边界及ASCII形式复现RED，只增`～~`分句，拼回原稿完全相等；未分句的任意prefix返回仍拒绝，既有6份真实私有重放保持。
- [x] 测试资产改build v2、12/36/74和480秒，先RED再GREEN；每API intent及失败/未知保留，不自动重试或fallback C。
- [x] 在既有`input_preview`即将中断的短预览中锁内冻结`opening_echo_check`：输入/生成开头各≤9字符、是否有参考、同一输出、活跃状态、身份资格、已播样本/回执年龄。仅私有诊断，不进prompt/历史，不改回声判定、拼接或任何阈值；边界/隐私回归RED→GREEN。
- [x] 私有v3控制器删除“达到4次即结束”，仅人工stop marker/时限/实例退出后收尾；合成用例证明已有4个模型结果也等待人工完成，超时仍精确停自身实例/restore。
- [x] 独立规格再质量复核PASS，最终普通/race各947通过/5skip，vet/双构建通过；编译164份Go输入161仓库/资产一致、3私有单列，冻结新pilot b3c37c6b。已更新全部已消费窗口、当前账本与交接，随本次main提交推送。
- [ ] 明确告知用户后补B四个人工问题；人工完成再收尾。C用新诊断核对回声防护为何漏拦，未定位不宣称已修复，未完成的问答/真人中断/续讲不算通过。
