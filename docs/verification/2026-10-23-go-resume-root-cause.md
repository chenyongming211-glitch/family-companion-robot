# Go 接续状态数据链审计

2026-10-23，审计源码 `66c0788abc1f4ac8ad1e17d112943da839e7c236`；`d68ec44..66c0788` 的 `internal/gateway`、`internal/qwen` 无源码变化。本报告中的离线审计没有改产品代码、部署、调用外部模型API、操作手表或播放声音。诊断代码已归档tools/voice-audit，私有日志仍在忽略目录；独立真实模型消融见[单独报告](2026-10-23-native-resume-ablation.md)。

现有证据不支持把失败全归为一个 `text_position=unknown`。确认的事实是：网关能保存原请求和设备采样进度，但暂停状态没有自动进入重连模型上下文；完整生成稿会进入该上下文；迟到最终回报只修正本地状态；“继续”后的新输出没有绑定回暂停任务。这些是可直接验证的数据链行为。它们各自对真实模型错误起点、漏终点的因果贡献仍需消融，不能凭源码审计认定生成稿必然污染模型，也不能把补上某一字段当成已修复接续。

## 当前完整数据链

| 环节 | 实际行为与源码位置 | 能证明什么 |
| --- | --- | --- |
| 固定请求来源 | `playback_progress.go:164–194` 按上游代次和 input item 绑定转写；`session.go:519–525` 在回复创建时固定 `responseOrigin` | 请求归属有独立对象，不依赖生成稿尾部 |
| 创建播放记录 | `playback_progress.go:32–46` 从 `responseOrigin` 建播放块；`playback.go:45–48` 每成功下发一帧加1440采样 | 是网关已发采样，不是用户已听文字 |
| 收集生成稿 | `session.go:563–572` 累积流式稿，并在 transcript.done 时直接保存整段稿 | 稿的增长跟随云端生成，与设备消费不是同一时钟 |
| 中断 | `session.go:380–433` 先快照/stop/清待播；若仍有已开始的回复，`399` 将 draft 加入历史；随后同步重连 | 停止旧输出并保留其生成稿，但该稿没有按已播放前缀裁剪 |
| 保存暂停任务 | `playback_progress.go:70–87` 首个非唤醒播放块占暂停槽，后续中断保留原槽 | 防止旁支覆盖原任务；没有区分“旁支中断”和“同任务恢复后又中断” |
| 重连上下文 | `session.go:249–268` 只传 `s.hist.context()`；`history.go:12–15,52` 只有 `turns` 和 `previous_reply_interrupted` | 不含暂停任务值、task_id、原请求字段、采样、settled |
| 模型看见历史 | `qwen/client.go:68–84` 把历史 JSON 附在 session instructions，保留“generated_text不是已听全文、位置未知”的说明 | 原请求若仍在最近历史中可见；整段生成稿也可见，但已明确标为数据和未完整播放 |
| 最终播放回报 | `playback_progress.go:89–159` 校验并更新本地 ledger、暂停副本，正常完整播放可升级本地历史 delivery | 没有向当前模型发送新的 instructions、conversation item 或状态事件 |
| 查询暂停状态 | `session_tools.go:37–38,155–157,180–189` 提供本地工具；模型调用后把工具 JSON 送回，并 response.create | `pausedData()` 只有走这条模型主动工具调用路径才实际送进模型 |
| 暂停状态内容 | `playback_progress.go:196–221` 提供原请求、采样与 settled，文字仍 unknown | 采样位置已知不等于暂停字词已知 |

`internal/gateway/tools.go` 不存在，本次审计对应实际文件为 `session_tools.go`；本地 conversation tools 定义和处理位于 `playback_progress.go:223–269`。

## 已用离线实验确认的行为

三项 characterization 使用 Go overlay 加载忽略目录里的独立测试文件，调用未经修改的生产方法及既有测试夹具。上游与设备都为回环 `httptest` WebSocket；音频为内存中的零值 PCM，通过既有 Opus 编码/解码。没有远端 API、实机 I2S、麦克风或喇叭。这些 PASS 表示“观察到所描述的现有行为”，不是修复通过。

### 1. 重连携带全文生成稿；暂停状态缺席；迟到回报不推送模型

`TestResumeAuditReconnectSnapshotAndLateFinal` 先创建原任务，令100帧音频进入待播，仅开始发送后就给出完整 transcript 和 `response.done=completed`；通过同一模型socket上的转写事件作串行屏障，确认生成完成后再发送模拟progress和abort，捕获第二代连接实际收到的 `session.update`。测试稿和既有数数稿均小于history的512字上限；任意长稿仍会被该上限截断。

- instructions 确有 `delivery=interrupted` 与完整 `generated_text`，原请求以最近user历史文本保留。
- instructions 没有 `"original_request":`、`"played_samples":`、`"settled":`、`"task_id":` 这些暂停状态值。工具名称/说明里出现相同词，不等于状态已送达。
- session 配置没有 `tool_choice` 强制查询；当前提示词会引导查询，但产品代码没有在“继续”之前验证模型实际查询过暂停任务。
- 新连接建立后再交付旧播放块的模拟final；随后从同一设备socket发送manual模式切换，作为“此前final已处理”的次序屏障。模型socket只收到含 `turn_detection=null` 的模式更新，没有暂停状态推送。
- 此时显式发起 `conversation_get_paused_task`，才收到 `settled=true`、`played_samples=1440`、原请求已知。这证明迟到final没有丢失，而是只保存在本地，等待可选工具读取。

完整的合成 `session.update` 捕获、模式屏障字段和最终工具结果保存在 `.superpowers/resume-root-cause-20261023/audit/session-update-capture.json`。该捕获只涉及测试文本，不含设备凭据或真实音频。

这不能简单称为“模型拿到了旧采样”：默认重连上下文本来就没有采样字段。准确说法是本地状态变新，而已建立模型上下文未收到对应更新；若模型先前调用过工具，旧工具输出也不会因随后final而自动失效。继续时再次查询可以读到新值，但当前实现没有强制这一步。

### 2. 同任务恢复后的第二段播放不会推进旧暂停记录

`TestResumeAuditSecondInterruptedPlaybackKeepsFirstProgress` 使用真实播放方法创建playback1，保存暂停任务并交付1440采样final；再用实际表示“继续刚才的数数”的新origin启动playback2，发送2880采样，再中断并交付playback2的final。

结果仍是同一task_id、`playback_id=1`、`played_samples=1440`；新播放块的2880采样没有合并到暂停任务。这项实验直接设置请求origin以隔离模型理解，只证明账本/状态机行为，不证明真实千问是否识别了恢复意图。

代码原因是首槽保护只判断 `s.paused==nil`，回报只有播放代号匹配时才更新暂停副本（`playback_progress.go:81–87,131–133`）。目前没有resume动作、恢复输出与task_id关联或多段任务进度；工具只有discard/replace（`223–269`）。正常回复完成也不清空暂停槽（`playback.go:76–97`）。因此即使未来获得每段音频的文字对齐，也不能据现有对象把二次恢复的进度自然归到原任务。

保护原任务对“回答数学后又被打断”是正确行为；相同规则用于“原任务恢复后再次被打断”则缺少必要区分。不能直接删除该保护，也不能未经验证自动把所有后续回复当作原任务进度。完成任务仍保留暂停槽是另一个生命周期边界，尚无离线语义判定器能证明哪次回复真正完成了原目标。

### 3. 有界历史淘汰后，已保存原请求只剩在可选工具里

`TestResumeAuditHistoryEvictionHidesSavedOriginal` 创建原任务和暂停记录，再追加12条旁支user条目。`hist.context()` 已没有原任务，`pausedData()` 仍有原请求。

这是 `history.go:24–28,46–50` 的12条/每条512字有界设计与暂停状态不自动注入共同形成的边界，不是暂停对象丢失。若模型继续请求时没有查询工具，仅靠重连JSON已无法恢复该目标；增加history窗口只推迟这个边界。该实验不是当前两次短流程失败的直接解释，因为当时原请求仍在历史里，且额外查询也得到正确原文。

## “生成完成”与“播放未完成”的实际次序

已有真实千问隔离证据中，两个样本都是先完成生成，约0.7秒后才打断仍在按节奏发送的音频：

| 私有证据 | 模型response_completed | paused_created / interrupt | 模拟final |
| --- | --- | --- | --- |
| native-v2-wake-overlap | 09:24:24.258603 | 09:24:24.992120 / .992189 | 09:24:25.093950 |
| native-exact-wake-overlap | 09:30:12.671284 | 09:30:13.352956 / .353070 | 09:30:13.453452 |

两次快照均为已发141120、最近progress129600采样；约101.8/100.4ms后final将本地played更新为141120。它们来自模拟收包回报，不能转换成真实I2S或已听数字。中断逻辑不要求 `responseDone=false` 才保存draft；只要 `started && responseID!=""`，完整已生成稿仍会入历史（`session.go:398–403`）。因此云端done不等于任务已完成，也不等于网关已播放完。

另一条边界是“网关已发完，但设备final尚未到”：此时 `playback.go:78–95` 已清除responseID/started并加入transport_sent历史；`interruptiblePlayback()` 仍能找到设备所有权（`playback_progress.go:58–68`），创建暂停任务，但 `session.go:398–400` 不再追加interrupted稿。原历史仍为transport_sent，并非按字词裁剪后的interrupted文本。这是不同次序，不应混为同一个取消事件。

## 生成稿污染：路径成立，模型因果尚未证明

存在的路径是：快速生成整段transcript → `draftTranscript` → interrupt时 `hist.addInterrupted` → 最近历史JSON → 新会话instructions。完整稿可以先于实际播放数十秒到达；[原生能力核对](2026-10-23-playback-text-capability.md)已给出该时钟差异。

不过当前history没有把稿存为普通assistant.text，而是generated_text并标interrupted，`qwen/client.go:70` 也明确禁止当作已听全文。两个已有同条件真实样本的完整稿均为1–50，后续却分别从46和1开始。能确认模型可能接触全文且准确位置无依据；尚不能确认“看到全文”必然使其从46/50开始。即使移除全文后从头读1–50，也只可能保持目标，仍非准确接续。

已保存原请求查询本身也是额外一轮对话：模型先调用工具获得原目标，再口头复述原请求，其答复继续留在当前上游会话中。这个步骤可能强化目标，也可能改变恢复选择。当前两次真实样本含该轮查询，不能直接当作用户只说“继续”的纯场景。后续比较须让有/无查询两组分别固定，而非把查询结果正确当作接续状态完整可用。

## 可证伪假设与最小后续实验

| 假设 | 最小实验 | 能支持或否定的结论 |
| --- | --- | --- |
| 全文生成稿使模型误把生成终点当已播终点 | 相同原目标、旁支问题、继续输入和参数，比较现有history与只移除未播generated_text；重复并记录起点/终点/是否履约 | 差异可支持稿件干扰；单次变化不足认定因果；无差异说明还存在别的因素 |
| 目标可用性不足于恢复时被稳定使用 | 现有history对比自动附暂停原目标、task_id与unknown位置；先固定无额外查询，再单独比较有查询 | 改善终点只能支持目标注入有帮助，不能证明准确起点；unknown条件不可宣称精确接续 |
| 模型即使有正确进度仍无法执行续答 | 单独给合成oracle“已播完1–8”，明确是测试输入事实，而非根据音频推算；验证是否9–50且无遗漏/越界 | 可分开测试信息是否足够和模型履约能力；不能证明生产映射有效或设备实际停在8 |
| 迟到final造成状态消费陈旧 | 回环固定同一块，分别在模型查询前/后发送final，用显式屏障确认顺序，核对查询值及模型接收事件 | 已证无自动push；继续前强制重查可证是否读新值，仍不产生文字位置 |
| 同任务第二次中断无法更新进度 | 固定原任务→旁支→明确继续→再次中断，用预设任务身份和两段可信文字区间测试关联；同时保留旁支中断保护对照 | 现有账本已证保留旧块；需新的任务关联契约后才能检验不会误合旁支 |
| 原请求在转写迟到时无法进入暂停对象 | 在旧上游response已开始、原STT未到时中断重连，再交付旧代次STT | `session.go:201–203` 丢弃旧代次事件，`connect:262–265` 清输入身份；这是保守隔离边界，当前已有原请求正确的失败样本不适用 |

真实模型消融需另行记录模型、音色、提示词、工具、样本次数、原始生成文本和执行身份；只改变被检验变量。本报告中的离线审计未执行外部API；后续已执行的合成历史消融单独记录，不能与回环characterization混为同一实验，也未用任何合成oracle倒推真实数字。

## 复现与证据边界

诊断文件位于忽略目录 `.superpowers/resume-root-cause-20261023/audit/`：`resume_audit_test.go`、`overlay.json`、`offline-test.log`、`offline-race-test.log`、`session-update-capture.json`。运行命令：

```sh
RESUME_AUDIT_CAPTURE="$PWD/.superpowers/resume-root-cause-20261023/audit/session-update-capture.json" \
GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local \
go test -race \
  -overlay .superpowers/resume-root-cause-20261023/audit/overlay.json \
  -count=1 -run '^TestResumeAudit' -v ./internal/gateway
```

macOS/arm64 Go1.25.6，普通和race各三项通过；race保留已有 `LC_DYSYMTAB` 链接警告，没有失败或竞态报告。overlay不修改正式包文件，测试只模拟回报。没有运行产品完整测试、Linux构建、API/声学验收或修复发布，因此本文不构成接续功能通过。
