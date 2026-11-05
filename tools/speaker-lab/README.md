# Speaker lab（隔离 Linux 实验）

本嵌套 Go module 只在云端 Linux 运行，使用 sherpa-onnx Linux v2.0.1。
根仓库的普通 Go 构建不加载 ONNX 运行库。包含有界特征提取、私有 IPC 和隔离的离线研究工具，
没有接正式 Realtime、产品登记库、人物数据库、会话队列或跨会话识别。

## 云端构建与探测

Go 1.25+、C 工具链；模型和 WAV 均使用显式绝对路径。权重、原始音频、二进制和运行日志不进 Git。

```sh
cd /path/to/source/tools/speaker-lab
go mod download github.com/k2-fsa/sherpa-onnx-go-linux/v2
go test -v -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build -o /private/evidence/speaker-lab .
/private/evidence/speaker-lab probe \
  --model /private/models/model.onnx \
  --sha256 <64位小写SHA256> \
  --wav /private/synthetic/fixture.wav
```

不带子命令时退出；不会默认监听。`probe` 用当前可执行文件启动子进程 `worker`，
冷启动（含权重摘要校验和模型加载）最多 10 秒，每次请求含 IPC 和推理最多 2 秒。
超时、取消或协议错误会终止并回收实际子进程。每个 worker 固定一个模型，CPU、1 线程、debug=0，串行处理。
父进程给 worker 只传最小 `PATH/LANG/LC_ALL` 环境，不传服务密钥；子进程输出丢弃，原生库 stdout/stderr 另行关闭到 `/dev/null`。

成功 stdout JSON 仅包含：`model_sha256`、`dimension`、`samples`、`inference_ms`、
`cold_start_ms`、`overlap_assessment: "unavailable"`、`quality_evidence: "fixture-only"`。
失败只有固定错误码；没有向量、PCM、文件路径、姓名或原文。单人 fixture 标签不是重叠检测证据。
不据此判断片段是否语音、单人或可登记；原始音频须另行具备获得授权的来源和质量事实。

WAV 由有界 Go 解析器读取：RIFF/WAVE、单一 16 字节 PCM `fmt `、16kHz、mono、16 bit、
单一 `data`，1～128000 samples。RIFF 长度必须精确，拒绝截断/尾随字节、重复 fmt/data、格式不符和超限；
元数据连同 PCM 总计最多 321536 字节。输入必须是无符号链接的 regular file，拒绝 FIFO/设备等输入。
不会调用原生库的无限制 `ReadWave`。

## 私有 worker 与协议 v1

```sh
# 调用者先建立属于当前用户、权限恰为 0700 的目录。
/private/evidence/speaker-lab worker \
  --socket /private/0700-dir/worker.sock \
  --model /private/models/model.onnx \
  --sha256 <64位小写SHA256>
```

只允许显式 Unix socket，没有 TCP。路径须是规范绝对路径且每级没有符号链接；
父目录须属于当前 UID 且权限为 0700，socket 为 0600。
拒绝已有任何同名路径，不自动 unlink；正常结束仅清理自己创建的同一 socket inode；父进程也保留连接前后核验的原 inode，
路径被替换时保留替换项，启动失败且未确认归属时仅尝试删除空临时目录。
模型必须为无符号链接的 regular file、大小 1～100 MiB；摘要完全一致后通过已验证文件描述符加载，
不重新打开可被替换的模型路径。冷启动成功后连接收到 `ready` 帧，包含固定摘要和模型实际维度（1～1024）。

每帧：4 字节 big-endian JSON header 长度（1～4096），然后 UTF-8 JSON，再按 header 长度读 binary payload。
所有 JSON 字段必需；拒绝未知字段、重复字段、null、类型不符和尾随 JSON。

请求 header：

```json
{"version":1,"id":1,"session":"session1","scope":"scope1","generation":1,"start":0,"end":16000,"model_sha256":"<sha256>","sample_rate":16000,"pcm_bytes":32000}
```

`id` 是 worker 生命周期内严格递增的正 uint64；不同会话可按顺序共享 worker，worker 不保存会话模板。
`session/scope` 仅为 1～64 个 ASCII 字母、数字、下划线或连字符，`generation` 为正 uint64。
区间为绝对半开 samples `[start,end)`；`end>start`、最多 128000 samples；PCM 为 s16le mono binary，
`pcm_bytes=(end-start)*2`，最多 256000 字节。模型路径不来自请求；请求摘要必须等于启动锁定摘要。
重复或迟到的 ID、格式错误均关闭连接；各会话区间顺序由调用方 Tracker 校验。

结果 header 回显 `id/session/scope/generation/start/end/model_sha256`，另带 `version:1`、
`kind:"result"`、`dimension`、`vector_bytes`、非负 `inference_us` 和 `code:"ok"`；
payload 为最多 1024 个 little-endian float32。特征不可用时为固定 `code:"unavailable"` 且没有 payload。
父进程把每项归属、摘要、维度和长度与当前请求/ready 精确匹配，拒绝 NaN、Inf、全零和截断向量。
向量只允许存在于此受保护 IPC 和父/子进程内存；不得打印、落盘或写报告。
每次读请求/写响应的绝对期限是 2 秒；等待新请求的连接空闲 2 秒后关闭，worker 可接收新连接。

同包后续评估可以复用 `readWAV(io.Reader)`、`startWorkerBinary(ctx, executable, model, sha)`、
`workerProcess.request(ctx, requestHeader, pcm)` 和幂等 `Close()`；这些 API 由单个串行调用者拥有，
不提供无限队列或并发 goroutine 推理。处理失败后应重新创建 worker，不能复用被退休的连接。

CLI 父进程和 worker 均要求 core dump 上限为 0。现有服务器 swap 不改变，因此 RAM 处理不等于已证明不会进入 swap；
合成 benchmark 只使用明确的合成 fixture；另行获准的公开真人语料仅由下述 evaluate 离线入口处理。

## 模型 opt-in 检查

默认测试不要求模型，真实模型相关用例明确报告 SKIP，不能当作模型执行通过。
显式构建二进制后，在云端以合成 WAV 运行：

```sh
SPEAKER_LAB_TEST_MODEL=/private/models/model.onnx \
SPEAKER_LAB_TEST_SHA256=<64位小写SHA256> \
SPEAKER_LAB_TEST_WAV=/private/synthetic/fixture.wav \
SPEAKER_LAB_TEST_BINARY=/private/evidence/speaker-lab \
go test -v -count=1 -run 'TestReal(ModelProbe|Worker.*|SessionBridge|RealtimeSessionBridge)OptIn' ./...
```

模型探测校验安全 JSON；真实 worker 另测环境隔离、SIGTERM 正常退出、socket 清理及替换 socket 保留。
协议、WAV、路径权限、模型摘要和子进程超时测试独立运行；超时 fake extractor 只用于确定性控制协议/回收路径，
不能代替真实 ONNX 探测或说话人质量评估。

## 合成 fixture 基准（工程证据）

```sh
/private/evidence/speaker-lab benchmark \
  --model /private/models/model.onnx \
  --sha256 <64位小写SHA256> \
  --manifest /private/synthetic/manifest.json
```

模型来源、SDK module 摘要、大小、维度和许可状态固定在 `assets.lock.json`。
实际镜像下载须与官方 Hugging Face pointer 的原文件 SHA256／大小匹配；镜像本身不构成来源信任。
上游声明 Apache-2.0，精确导出权重的许可未独立解决，当前仅隔离研究，不能作为已批准的产品依赖。

manifest 必须是规范绝对路径的无符号链接 regular file，总计≤128 KiB；必填 schema：

```json
{"source_kind":"synthetic_tts","model":"test_tts","sample_rate":16000,"human_acceptance":false,"single_speaker_only":true,"cases":[{"id":"synthetic_1_1","speaker":"fixture_1","split":"calibration","path":"/private/synthetic/one.wav","samples":32000,"sha256":"<WAV原文件SHA256>","text_sha256":"<生成原文SHA256>"}]}
```

示例仅说明字段，实际要求3～8种匿名 `fixture_1`～`fixture_8`、6～128项，每种至少2项 calibration 和1项 holdout。
拒绝未知／缺失／重复／null字段、重复ID/路径、跨split复用相同音频摘要或原文摘要。
每项音频必须通过有界 WAV 解析、原文件摘要和样本数核对；原文只保留输入摘要，不进入输出。
`single_speaker_only:true` 是合成来源的显式声明，须由生成流程保证单音色且无混音，不能靠模型推断。
低于1.5秒或归一化 PCM RMS低于0.005的片段在提取前拒绝；此门禁只能检查时长／低能量，不是语音或重叠检测器。

一个复用 worker 按严格递增请求ID串行提取所有合格片段，向量仅在父／子进程内存和受保护IPC存在。
校准只取 manifest 首次出现的前两种音色、各自前两项 calibration，第三种及后续音色从不拟合阈值。
令 gap=校准同音色最小余弦−异音色最大余弦，只有 gap>0 才使用
new=maxDifferent+gap/4、match=maxDifferent+gap/2、margin=gap/8。
非正间隙报告 `failed_nonseparable`，不使用任意回退阈值，也不从holdout调参。

匿名序列用前两种音色的第一项calibration初始化Tracker，然后交错所有holdout两遍。
独立容量2对照重复相同序列，明确计数第三种音色unknown／不确定／误归已知，以及前两种音色重获。
评分以首个成功的新标签建立fixture与guest的一一映射，拒识不预留编号；已有fixture分裂到新标签或匹配其他fixture已有标签均计错，不静默重映射。
尚无映射的fixture期望为 `new_guest`，容量2的额外fixture期望始终为 `unknown`。
Tracker在当前运行结束清除，不登记姓名、不保存模板；输出只有生成的fixture序号、guest标签和状态。
所有holdout之间的同／异音色余弦仅报告聚合数量与范围，不输出完整配对矩阵。

JSON还包含模型／manifest摘要、有效质量与CPU配置、冷加载耗时、首次推理和温态中位数／P95
（原生推理与包含IPC的wall time分别计时），以及实际worker终止回收后的总CPU毫秒和Linux峰值RSS KiB。
温态统计剔除第一项提取，P95采用nearest-rank；worker CPU包含加载／摘要校验和所有推理。
校准失败仍输出工程报告，须读取calibration.status和逐例分类结果，不以CLI退出0代表质量通过。

报告始终 `source_kind:synthetic`、`production_acceptance:false`、`human_acceptance:false`、
`feature_enabled:false`；重叠与分段均为 `unavailable`。
合成音色可分与guest标签稳定仅是工程／合成证据，不能替代真实家庭语音、重叠／换人降级或产品验收。

## 手动、仅内存 session 输入

```sh
/private/evidence/speaker-lab session \
  --model /private/models/eres2net.onnx \
  --sha256 <64位小写SHA256> \
  --declared-single-speaker
```

必须显式声明单人测试，不作为默认模式。启动先核验模型摘要，然后 stdout 输出一行 `{"event":"ready","evidence":"user_declared_test"}`；不打开麦克风。配套 Mac 手动入口见 [`../speaker-session/README.md`](../speaker-session/README.md)。

stdin 复用上述 v1 binary PCM 帧，scope 必须为 `manual-test`，同一连接固定 session／generation／模型，ID 严格递增，sample 区间从0连续相接，每句最多64000 samples（4秒）。最多32句／5分钟，包含空闲和不完整帧等待。EOF、取消、超时、坏帧关闭并清除临时 Tracker；session 拥有可关闭 stdin，取消直接关闭读取，不遗留阻塞 reader goroutine。

仅合格片段提取特征，每句重新启动／关闭／回收实际 worker，保留其10秒冷启动与2秒RPC上限，以免人工4秒采集超过原worker的2秒空闲期限。父进程只保留一个最多8人的首次模板 Tracker；首个合格特征锁定维度，模板不随匹配更新。临时向量在 Observe 后清零，PCM 在每句处理后清零，不写录音、特征或人物库。每句冷启动仅为隔离诊断设计，不是产品延迟指标。

stdout NDJSON 仅有 `ready`、`observation`、`closed`；observation另有正 `id`、`label`（`guest_1`～`guest_8`／`uncertain`／`unknown`）与 `duration_ms`（输入音频片段时长，按样本数换算并向下取整；不是处理耗时；即使0ms也始终提供），所有事件带 `evidence:user_declared_test`。没有向量、PCM、路径、姓名、文本。错误仅 stderr 固定 `session_failed`。真实输入结果不能重定向到文件。

低于1.5秒／RMS低于0.005直接 observation uncertain、不会生成身份。单人状态来自参与者显式声明，非算法验证；门禁不检测语音／换人／重叠。ERes2Net研究阈值 new=0.622530、match=0.682987、margin=0.030229来自既有合成基准，未经家庭真人校准，不作身份认证或登记依据。单人多句实测不能宣称多人验收。

新增 `TestRealSessionBridgeOptIn` 使用显式合成 WAV，经实际 stdin CLI→新 worker→Tracker，间隔超过 worker idle deadline 后检查同标签再获和 EOF 关闭。与既有3项原生opt-in同样通过显式模型／摘要／fixture／binary环境运行。应用内存处理不证明系统／内核所有副本或既有 swap 的状态；不改系统 swap。

## Realtime 轮流发言测试输入

```sh
/private/evidence/speaker-lab session \
  --model /private/models/eres2net.onnx \
  --sha256 <64位小写SHA256> \
  --declared-single-speaker --realtime-test
```

`--realtime-test` 仅适用于 `session`，仍必须显式声明测试片段为单人。stdin 保持严格协议 v1，scope 固定为 `realtime-test`，同一连接的 session／generation／模型固定、ID 严格递增。完整片段最多128000 samples（8秒）；首段可从非零源采样坐标开始，后段 start 必须大于等于上段 end，允许真实静音／播放期间的坐标间隙，拒绝重叠或倒序。该模式不填补间隙、不裁切长片段。

32段／5分钟上限、质量门禁、临时数据清零及子进程回收规则与上述手动模式相同；所有事件的 evidence 改为 `turn_taking_test`。Realtime 的 observation 在原5字段上增加固定 `reason`，共6字段；ready／closed仍恰为2字段。默认手动模式仍使用 `manual-test`、从0连续相接、4秒上限及 `user_declared_test`，observation保持原5字段、不增加reason。`TestRealRealtimeSessionBridgeOptIn` 在内存重复显式合成 fixture，验证两个完整8秒片段、非零起点／源坐标间隙、标签／原因及 EOF 回收，不保存新增音频。

`reason` 只区分现有判定来源，不改变模型、阈值或质量门禁：

| reason | 原有结果与判定来源 |
| --- | --- |
| `candidate` | Tracker返回临时候选，label为`guest_1`～`guest_8` |
| `match_uncertain` | 通过时长／能量门禁，人工单人及完整片段声明为真，Tracker返回uncertain |
| `capacity` | Tracker容量已满，返回unknown |
| `too_short` | 输入总片段不足24000 samples（1.5秒），不调用提取器 |
| `low_energy` | 输入总片段达到1.5秒，但未通过既有RMS门禁，不调用提取器 |

`duration_ms` 仍由完整输入的样本数换算，不等于有效人声时长。原因码不含相似度、分数、向量、转写或音频，也不能追溯此前未携带原因的uncertain结果。

该入口供受控轮流发言观测，未接正式 Realtime。单人来自人工声明，匿名候选不能用于身份认证或登记；不提供连续流分段、换人或重叠检测证据，合成测试也不等于家庭真人准确率或多人验收。

## 公开真人离线评测（闭集研究证据）

`evaluate` 与只接受 `synthetic_tts` 的 `benchmark` 分开。只在云端执行，
清单和 WAV 均留在私有目录，不将真人文件、清单或向量提交 Git：

```sh
/private/evidence/speaker-lab evaluate \
  --model /private/models/model.onnx \
  --sha256 <64位小写SHA256> \
  --manifest /private/public/manifest.json
```

公开真人 manifest 的完整字段如下（示例只展示一项；真实清单须满足下述人数与划分）：

```json
{"source_kind":"public_human","source_id":"public_corpus","source_url":"https://example.org/corpus","license_url":"https://example.org/license","single_speaker_basis":"source_annotations","sample_rate":16000,"cases":[{"id":"case_1","speaker":"fixture_1","split":"enrollment","path":"/private/public/one.wav","samples":48000,"sha256":"<WAV原文件SHA256>","recording_sha256":"<原录音SHA256>","text_sha256":"<语句摘要SHA256>"}]}
```

来源 ID 为1～64位字母、数字、下划线或连字符；来源及许可参考必须是无凭据 HTTPS URL，
只校验格式、不在运行时下载或证明许可。`source_annotations` 表示单人片段依据是数据集标注，
不表示程序已检测单人或完成分段。应先独立核对数据来源、许可与原录音／文字摘要的生成过程。
不接受把合成 fixture 改写为 `public_human`。

清单最多128 KiB、12～128项、3～8位 `fixture_1`～`fixture_8`；每位至少1条 enrollment、
2条 calibration、1条 evaluation。每项所有字段必需，未知、重复、null、重复 ID／路径／WAV摘要均拒绝；
路径须为规范绝对路径，无符号链接 regular file。原录音摘要或相同文字摘要不可跨划分，
同一录音切片不能分别充当登记与验收。沿用16kHz mono PCM16、有界8秒 WAV、文件摘要及样本数核验。
时长／RMS门禁仍为1.5秒及0.005，只能发现过短／低能量，不能识别语音或重叠。

每位首条合格 enrollment 仅在内存建模板，不更新。阈值只使用所有人的所有合格 calibration
片段及全部无序同人／异人对；每位须至少2条合格校准。令 gap=最小同人余弦−最大异人余弦，
仅 gap>0 时冻结 match=maxDifferent+gap/2、margin=gap/8。
任何合格片段的无效向量都是固定失败；无合格模板、校准不足或不可分离则报告
`failed_missing_enrollment`／`failed_insufficient_calibration`／`failed_nonseparable`，
`evaluation.status=not_run`，不回退默认阈值，也不从 evaluation 调参。

只有独立 evaluation 计分一次：最佳模板低于 match、与次佳差距小于 margin 或并列时拒识；
通过后分别计正确匹配与误归。质量拒绝也计入 evaluation 拒识及分母。
`accuracy=正确数/全部evaluation`，`effective_rate=(正确数+误归数)/全部evaluation`；
校准失败时这两项为0且 `status=not_run`，不能当作实际评分。
所有人均有模板，因此只评闭集；开放集明确 `open_set_assessment:unavailable`。

报告仅含来源类别、模型／清单摘要、维度、用例与质量计数、聚合校准及计分结果、独立evaluation的原生／IPC耗时。
耗时剔除首个evaluation推理后计算温态中位与P95（仍采用现有 nearest-rank）；不包含模型加载、
登记或校准推理。即使校准失败，已发生的独立evaluation提取耗时仍会报告，不表示已评分。
报告不含逐人结果、路径、原录音／文字摘要、原文或向量，错误只有固定 `evaluate_failed`／`usage`。
始终 `human_acceptance:false`、`production_acceptance:false`、`feature_enabled:false`，
分段及重叠均 `unavailable`。离线公开语料结果不证明家庭现场、连续流、多人重叠或产品实时效果，
不修改 session／试点／产品阈值。原有合成基准仍只代表工程证据。

## 独立50人离线实验 evaluate-large

```sh
./speaker-lab evaluate-large --model /private/models/eres2net.onnx \
  --sha256 <64位小写SHA256> --manifest /private/public/manifest.json
```

该显式命令复用 public_human 严格清单、受控 worker 和有界WAV／摘要校验；要求恰好50人，
编号仅 `fixture_1`～`fixture_50`，400～2048项、清单≤2MiB，每人至少1条 enrollment、
3条 calibration、4条 evaluation。普通 evaluate 的3～8人／128项／128KiB、合成入口和在线
session 的阈值及8人上限保持。数据准备另行保证2～8秒、独立原录音／原句与三划分冻结。
音频摘要不得重复；原录音和文字摘要不得跨划分。低质量 evaluation 仍计入拒识和总分母。

每种条件只提取一次特征，首条合格 enrollment 是模板，全部合格 calibration 的所有无序对
参与拟合。任一人缺少合格登记或3条合格校准，则全部规则 `not_run`，保留失败原因。
四项规则全部报告：`current_session` 固定 match=0.682987、margin=0.030229；
`strict_separation` 沿用同人最小值与异人最大值间隙的 match=异人最大值+gap/2、margin=gap/8，
不可分离只将本项记为 `not_run`；`calibration_far_001` 和 `calibration_far_01` 分别固定
经验配对FAR预算0.1%和1%，margin=0.030229。FAR拟合只读取校准异人配对，允许误接纳数
为 floor(配对数×目标)，阈值相等接纳；排除边界用 `Nextafter(score,+Inf)`，再实际计数。
同分整体排除，[-1,1]内无法满足目标则 `unreachable`，不回退也不从evaluation调参。

每项规则同时输出50候选池和固定10组×5候选池：按匿名数字1～5、6～10直到46～50分组，
后者含匿名组号1～10的聚合指标。`accuracy`=正确／全部evaluation，`effective_rate`=
（正确+误归）／全部evaluation；同分、阈值不足、间距不足和质量拒绝均计为拒识。
`evaluation_template_pairs` 分别统计同人／异人分数≥阈值的接纳及拒绝，不使用候选间距；
质量拒绝视为其全部模板配对被拒绝。`top1_diagnostic` 单独统计正确、同分、质量拒绝和总量，
不应用阈值或间距；仅是特征诊断，不能用来授予身份。四规则和两候选池复用同一批样本，
评分次数不构成独立样本数，校准配对FAR也不等于多候选误认率或家庭现场保证。

输出只含聚合指标、匿名组号和模型／清单摘要；不输出路径、原文、姓名、逐句特征或原生错误。
原生／IPC计时只统计evaluation推理；worker冷启动、总CPU和峰值RSS含登记／校准与加载，
主进程峰值RSS独立列出，不能将两个峰值相加当作同一时刻并发内存。
`human_acceptance`／`production_acceptance`／`feature_enabled` 始终false，
segmentation／overlap／open_set均unavailable。这里只是离线旁路实验，在线人数、设备声学和
完整Realtime时延需要各自验证。

## 独立离线优化研究（不改变普通命令）

`quality-audit --model ABS --sha256 SHA --manifest ABS` 只审计波形，不计算身份分数。模型必须是资产锁中的 Silero SHA；清单沿用 `evaluate` 的完整 public_human header 和 case 字段，但所有 `split` 必须为 `evaluation`，匿名人物只允许 `fixture_1` 至 `fixture_80`，1–80 人、1–2048 条、文件最多 2 MiB。旧 evaluate/evaluate-large/synthetic/session 的边界与默认策略保持。

输出为 `source_kind, model_sha256, manifest_sha256, config, cases, human_acceptance, production_acceptance, feature_enabled`。三个旗标始终 false；每行只有 `id, sha256, samples, speech_samples, rms, clipped_samples`。config 固定为 `provider:"cpu", threads:1, sample_rate:16000, threshold:0.5, window_size:512, min_silence_seconds:0.1, min_speech_seconds:0.25, max_speech_seconds:8, buffer_seconds:10`。每条独立 Reset/Clear，Flush 后把 SDK 人声片段裁切到原音频区间并计算并集。`speech_samples >= 16000` 只是估计的人声长度门禁，不是人工标注或重叠检测；RMS 与旧 qualityReason 使用相同 PCM16/32768 计算，clipped_samples 统计两个满幅端点。

`enrollment-study --model ABS --sha256 SHA --manifest ABS --audit ABS` 使用另一个严格清单：沿用 public_human header，增加 `quality_audit_sha256`（audit 文件原始字节的 SHA）；case 沿用 `id,speaker,path,samples,sha256,recording_sha256,text_sha256`，把 split 替换为 role。role 只能为 `known_enrollment, known_evaluation, calibration_enrollment, calibration_query, unknown_evaluation`；匿名人物恰好为连续 `fixture_1..50, calibration_1..10, unknown_1..5`。一个清单代表一个设备条件；跨条件配对完整性由冻结前数据审查验证。两种输入和 audit 均最多 2 MiB／2048 条，禁止未知/重复/缺失/null 字段、非绝对或含 symlink 的路径。audit 可为更大的资格池，但每条研究 case 必须匹配其 id、WAV SHA 与 samples；audit 原始摘要、固定模型 SHA、全部 config 及三个 false 旗标也核对。

登记每条 2–8 秒，至少两条，按输入顺序累计首次达到 20 秒后停止，总原始长度 20–28 秒，同时满足 RMS≥0.005 与 VAD≥1秒；至少保留两条不同句 query。校准每人至少两条 query 同时通过两个门禁；已知人物的低质量 query 全部保留在失败分母，不能因质量全部失败而换人。陌生人物每人 2–5 条。音频与录音不可重复，文字不可跨角色；同一人物不能重复同句。资格选择及设备配对是数据冻结责任，不从 embedding 结果选择人物或句子。

短模板取首条登记；长模板先逐条 L2 归一化、等权平均、再归一化，非法或零和向量拒绝。本命令每模型/设备条件仅用 energy OR vad 并集提取一次，以同一原始 PCM 复用 short/multi × energy/vad 四个视图，绝不根据 VAD 裁剪或拼接声纹输入。新的融合模板与提取向量分别由唯一所有者清理；普通 worker 的自有 float PCM 和失败向量也会清理。SDK/OS 内部临时副本不宣称已取证擦除。

每个视图只用独立 10 位校准人物的 query×10 登记模板计算同人/异人对，冻结 condition_far001/condition_far01 门槛：预算 floor(N×target)，Nextafter 整组排除同分边界，重新计数，超过 [-1,1] 则 not_run。还报告 current_session=0.682987 与 legacy_base_far001=0.4499944095418535；全部 margin=0.030229，旧 base 门槛对 V2 仅为参考。报告实际对数和预算；小样本经验 FAR 不是总体保证。

每条政策报告已知 50 人池及连续 10×5 人组、包含质量拒绝的总量/正确/误归/拒识和模板配对；Top-1 同时报告合格与全分母。陌生 5 人各只进入一个预定 5 人组（unknown_1→组1，…，unknown_5→组5），并报告 50 人池，包含全部/合格分母 false_accept；不计算未知人物 Top-1。研究报告包含全部音频时长、逐人原始登记与 VAD 估计长度、native/IPC 时间、cold start、worker CPU/RSS 和主进程 RSS。向量只留 RAM，不输出来源路径、原文或向量。

只在云端独立实验进程运行。调用方必须为**整个 CLI 进程树**设置有界外层超时并在超时后终止、回收，不能只依赖 Go context 中断同步 native 调用，例如 `timeout --kill-after=5s 600s speaker-lab ...`；更大清单由实验控制器按已冻结总预算设置。CLI 在片段/窗口之间检查取消并释放 native 对象与拥有的缓冲。原 SDK、服务/session 配置、正式阈值、VAD/AEC、固件和麦克风均不改变。连续流、重叠、重放、家庭现场和生产验收保持 unavailable/false；有限公开语料陌生人观测不能外推访客保证。

可选原生 VAD 回归使用 `SPEAKER_LAB_TEST_VAD_MODEL` 加既有 `SPEAKER_LAB_TEST_WAV`（合成 fixture）和 `SPEAKER_LAB_TEST_BINARY` 显式开启 `TestRealVADOptIn`；普通测试默认跳过。所有 Go/ONNX 测试、构建和运行均在云端。

## 独立评分后端研究 backend-study

```sh
# 仅云端；外层控制器须对整个进程组设置 TERM/KILL 超时并回收。
timeout --kill-after=5s 900s ./speaker-lab backend-study \
  --model /private/models/eres2net.onnx --sha256 <64位小写SHA256> \
  --manifest /private/backend/device03-manifest.json
```

该入口比较 R0 余弦、R1 AS-Norm、R2 低维质量校准；普通 evaluate、evaluate-large、quality-audit、enrollment-study 和 session 的清单限制、JSON 和行为保持。不得把离线研究门槛用于产品。一次调用对应一个冻结条件，共享一次 energy 合格特征提取；查询不以 VAD 硬过滤、不裁剪音频。所有向量仅 RAM，由提取结果的唯一所有者清理，融合模板另行清理。

新清单最多 4 MiB／4096 条／240 人。全部 header 必需：`source_kind:"public_human", source_id, source_url, license_url, single_speaker_basis:"source_annotations", sample_rate:16000, condition, audits, cases`；URL 为 HTTPS，`condition` 只允许 `device03` 或 `device06`。这是实验条件标识，具体来源设备组合与距离须另查冻结资料，不能由标签推出物理近远场或纯设备效应。`audits` 为 1～4 个 `{path,sha256}`；每个审计仍限 2 MiB／2048 条，固定 Silero SHA／config／三个 false 旗标。审计 ID 只在各自文件内唯一，跨文件以 WAV SHA 绑定，重复引用／WAV SHA 拒绝；未入选审计记录可以保留。

每个 case 必需 `id,speaker,role,path,samples,sha256,recording_sha256,source_group_sha256,text_sha256`。逐层拒绝未知／重复／缺失／null 字段、重复 ID／路径／WAV／录音摘要、无效编号或角色；路径须为无 symlink 的规范绝对路径，实际 PCM16 mono 16k WAV、SHA 与 samples 当场核验。人物／角色如下，编号均从 1 连续：

| 人物 | 人数 | role |
| --- | --- | --- |
| train_N | 10 | train_enrollment／train_query |
| validation_N | 50 | validation_enrollment／validation_query |
| validation_unknown_N | 5 | validation_unknown |
| cohort_N | 30 | cohort |
| test_N | 实际 1～5 | test_enrollment／test_query |
| test_unknown_N | 20 | test_unknown |

各登记人物至少两条查询；陌生人物各 2～5 条。cohort 恰好两条，同时通过 energy（RMS≥0.005，samples≥24000）及 VAD（speech_samples≥16000）；所有登记按清单顺序累计首次达到 320000 samples 后停止，总长 320000～448000、至少两段且每段通过两个门禁。每段逐一 L2、等权平均、再 L2 形成模板。查询保留所有格式合格输入，energy 失败计入总分母并拒识；VAD 长度仅作 R2 特征。

同一人物不得重复文字；登记／查询不得共享文字或源录音组，源录音组与录音摘要不得重复。不同人物允许相同文字，cohort 的文字摘要可为空，但源录音组必须不同。匿名清单不能自行证明源人物纯度、真正独立内容或未接触资格，冻结前数据审查负责这些证据。

R1 固定 30 个背景模板、top-k=10；分别对登记和查询取背景余弦最高十项的均值与**总体**标准差，标准差下限 1e-6；输出两侧 z-score 的均值。报告实际标准差下限替换次数。R2 特征仅为 `[AS-Norm, log1p(querySpeechSamples/16000)]`，均值／总体标准差仅来自 train 查询×10 模板配对，下限 1e-6；加未正则化截距。同／异人损失权重各半，非截距 L2 正则 λ=.01，稳定 softplus；全批梯度下降初始步长 1，最多 50 次折半回溯、2000 步，目标改善≤1e-8 收敛。非有限、未收敛或分数权重≤0 显式失败。输出训练人物／查询／同异人对数、均值／标准差、参数／初终目标和迭代；分数不是身份概率。成功时逐查询核对 R2 与 R1 排名完全一致。

`methods[].pools[]` 固定包含 validation 的实际 test 候选数 N、validation50，以及 testN。validation 按**数字编号**连续分 N 人组，末组从开头补齐 N 个候选；每条已知查询只在所属组计一次。陌生人物 j 只进入 `(j-1)%groupCount` 组；test 使用全部实际 N 个模板。N=1 无次佳间距，margin 固定 0；多候选最高分并列拒识。

每个池的 `rules` 前两项为陌生目标 1%／5%，只使用相应 validation 池标定，budget=floor(全部陌生查询×target)。阈值候选包含有限 best 分数、Nextafter 向上边界和两端边界；R0 margin 候选 0/.01/.030229/.05/.1，R1/R2 为 0/.1/.25/.5/1。只接受 validation 已知误归为零且陌生误接纳不超预算的组合，依次最大化正确接纳、最小化陌生误接纳、取较高 threshold、较高 margin。全拒识允许。R0 validation50 另外输出 current_session=.682987／legacy_base_far001=.4499944095418535，margin=.030229，作为旧 B 的兼容诊断。test 不参与选方法或调阈值。

每项规则含全量／energy 合格分母、已知正确／误归／拒识、陌生误接纳／拒识、匿名组及逐人计数。`macro` 的全量正确接纳与 Top-1 对各已知人物等权；合格宏平均只包含至少一条 energy 合格查询的人，另报此人数。陌生人物报告逐人计数及 `unknown_people_with_any_false_accept`。Top-1 只作无门槛诊断；质量失败保留全分母。`not_run` 保留真实人数／样本／合格分母，所有决策为零，不能读成全拒识。

`template_pair_diagnostic` 只使用 energy 合格查询的 own-template 正对和已知其他模板／陌生模板负对，提供成组同分 ROC/DET、线性插值 EER、原始最小 `.01*FRR+.99*FAR` 成本与除以 `.01` 的标准 normalized minDCF。配对**不是独立观测**，点列是诊断，不能从 test 最佳点选部署策略。

角色质量统计分列全部／energy／VAD／两者；报告匿名登记长度、模型／清单／审计 SHA。native/IPC 时间覆盖所有 energy 合格角色，剔除首个请求后取温态 P50/P95；与旧只计 evaluation 的结果不可直接作纯速度比。冷启动、worker 总 CPU／峰值 RSS、parent 峰值 RSS 分列；背景统计只计算一次并记入 R1，R2 标记复用，拟合、门槛标定、评分和方法总耗时另列。两个 RSS 峰值不能相加当瞬时并发内存。

输出不包含路径、源人物、正文、录音／文字／源组摘要或向量。`human_acceptance`、`production_acceptance`、`feature_enabled` 均 false；`open_set_assessment:limited_public_unknown_cohort`，连续分段、重叠、重放和家庭现场 unavailable。错误仅 `usage`／`backend_study_failed`。首次真人身份评分须在代码与冻结数据审查通过后进行。

只验证冻结输入合同、不加载模型的可选检查：`SPEAKER_LAB_BACKEND_MANIFEST=/private/backend/device03-manifest.json go test -run '^TestBackendFrozenManifestOptIn$'`。默认跳过；不构成身份准确率或模型原生验证。所有 Go 测试／构建／运行均在云端。

## 登记诊断与有界证据积累（隔离 `evidence-study`）

`evidence-study` 保持已有 B 等权登记模板、RMS／长度资格、严格 `public_human` 清单与冻结输入；
登记诊断只提示重录，不自动删段。查询候选将合格片段向量逐段归一化，按样本数加权融合后再归一化，
不是拼 PCM 重新推理。连续性只用 `train_query` 拟合；全窗口两两守门，无人物标签、身份结果或未来输入。
训练不足／门槛无效时明确禁用并回退单片段。固定上限为8秒合格音频、4段、1.5秒相邻间隙、12秒源坐标跨度。

只在云端 Linux 使用已校验绝对路径；不在 Mac 安装运行依赖、构建或运行语音／原生测试：

```sh
cd /private/source/tools/speaker-lab
GOTOOLCHAIN=local GOMAXPROCS=2 go test -json -count=1 ./... > /private/evidence/ordinary.jsonl
GOTOOLCHAIN=local GOMAXPROCS=2 go test -race -json -count=1 ./... > /private/evidence/race.jsonl
GOTOOLCHAIN=local GOMAXPROCS=2 go vet ./...
GOTOOLCHAIN=local GOMAXPROCS=2 go build -o /private/bin/speaker-lab .
SPEAKER_LAB_TEST_EVIDENCE_FIXTURE=/private/synthetic/manifest.json \
SPEAKER_LAB_TEST_MODEL=/private/models/eres2net.onnx \
SPEAKER_LAB_TEST_SHA256=<64位小写SHA256> \
SPEAKER_LAB_TEST_BINARY=/private/bin/speaker-lab \
GOTOOLCHAIN=local GOMAXPROCS=2 go test -v -count=1 -run '^TestRealEvidenceStudyOptIn$' .
```

新增测试只由以上四个显式输入启用；默认新增1项和原8项 opt-in 均跳过。合成清单必须通过原有
`loadManifest` 的 `synthetic_tts`／非真人验收／单人声明合同。测试先真实原生提取，再在内存复用合成声音填充候选池，
执行内部分析、四组诊断等价、匿名 JSON 与子进程／socket 清理；这种声音复用不代表人物独立、公开真人或识别准确率。
它没有放宽公开数据 loader。实际拟合状态、观察到的融合数与工程测试结论见[验证报告](../../docs/verification/2026-11-05-speaker-evidence.md)。

公开回归 CLI 为：

```sh
/private/bin/speaker-lab evidence-study \
  --model /private/models/eres2net.onnx \
  --sha256 <64位小写SHA256> \
  --manifest /private/public/backend/device03-manifest.json
```

实际研究应先审查源码并冻结二进制／模型／清单／审核摘要和实验常量，用进程组1200秒超时、TERM／KILL及回收的控制器顺序运行，
保留 stdout／stderr／退出状态／资源记录，禁止覆盖成功输出。2026-11-05 两次既有运行已完成，不应再次执行其控制器。
旧 B 只读取已保存结果做语义回归，不重新推理。四组固定为 `B_baseline`、`B_enrollment_diagnostic`、`B_accumulation`、
`B_combined_diagnostic`；两种顺序为 `paired_bursts`、`alternating`，相邻虚拟间隙固定4000样本。
只用 validation 以1%陌生误接纳经验预算独立标定各候选池，再冻结应用于旧 test；不按结果选择片段、顺序或阈值。

输出是 `seen_development_regression`、`constructed_sequence`、`new_blind_test=false`，不是现场连续录音。
未知／不合格查询仍计分母；报告事后混入异人的窗口，但不把标签反馈给积累器。
诊断没有真实重录素材时必须保持身份结果不变；没有生产／家庭验收、真实重叠检测或端到端时延结论。
模型未替换或部署，正式服务／环境／固件／VAD／AEC均未改变。参与者采集分别比较“机器人原始登记／机器人独立现场查询”与“手机原始录音登记／机器人独立现场查询”；不播放手机录音，手机登记素材经私有转换直接生成模板。具体步骤见验证报告；工具不会自动开麦。

## 固定模型／登记双原型（隔离 `prototype-study`）

此命令只做离线研究，仍通过严格 `public_human` backend 清单读取全部角色和质量拒识。
每个模型／条件共享一次提取，同时输出 `B_single_template` 和 `P2_two_prototypes`。
登记保留原有双门禁：每段RMS≥0.005且VAD speech_samples≥16000，并按原清单顺序累计首次达到
320000样本（20秒）的前缀，总登记320000～448000样本。查询评分仍以既有长度／RMS判断资格，
查询的VAD仅诊断；本轮没有新增VAD硬门禁或绕过登记门禁。
严格公开清单中所有片段至少32000样本（2秒）；通用energy提取器的24000样本（1.5秒）是另一层下限，
不能把它当作此公开清单准入条件。
B 保留原清单顺序的等权归一化融合；P2 只看登记向量，按 ID 排序选择最远种子，最多10轮余弦聚类，
每簇至少2段；不足4段、重合或小簇时整人回退原B，不删除登记段。每人得分取两个原型余弦最大值。
没有查询积累、AS-Norm 或 QMF。最大值可能提高陌生误接纳，故各方法／池独立使用 validation
按1%经验陌生预算和既有 margin 网格标定；不能沿用B门槛或将Top-1当接纳率。

全部 Go／Python／原生运行和依赖安装限云端 Linux；Mac仅代码／Git。使用已校验绝对路径：

```sh
cd /private/source/tools/speaker-lab
GOTOOLCHAIN=local GOMAXPROCS=2 go test -count=1 -v -run '^TestRealPrototypeStudyOptIn$' .
GOTOOLCHAIN=local GOMAXPROCS=2 go build -o /private/bin/speaker-lab .
SPEAKER_LAB_TEST_PROTOTYPE_FIXTURE=/private/synthetic/manifest.json \
SPEAKER_LAB_TEST_MODEL=/private/models/fixed.onnx \
SPEAKER_LAB_TEST_SHA256=<64位小写SHA256> \
SPEAKER_LAB_TEST_BINARY=/private/bin/speaker-lab \
GOTOOLCHAIN=local GOMAXPROCS=2 go test -count=1 -v -run '^TestRealPrototypeStudyOptIn$' .
/private/bin/speaker-lab prototype-study \
  --model /private/models/fixed.onnx \
  --sha256 <64位小写SHA256> \
  --manifest /private/public/backend/device03-manifest.json
```

新增测试默认跳过，原10项 opt-in 保持原样。启用时先从原严格合成清单真实提取，按输入顺序选两个不同
波形摘要，在内存为逻辑身份复用为两种模式、每模式两段，并保留一个两段回退身份。
测试强制要求真实双原型计数和 `[2,2]` 簇成员、全回退与B的完整三池等价、质量失败分母、匿名输出、
独立原型所有权及向量清零、worker回收和socket移除。`synthetic_engineering_fixture`、开放集 unavailable、
所有验收／功能标记false；这些逻辑身份不是独立真人，不能用于准确率结论。公开loader不接受此合成输入。

本轮固定 ERes2Net-base／ResNet293-LM／CN-Celeb ResNet34-LM，128MiB模型上限、2秒请求／10秒启动边界不变。
WeSpeaker适配只加元数据、不改图或权重；实际sherpa使用Povey窗／snip_edges=false／high_freq=-400，
官方参考使用Hamming／snip_edges=true／high_freq=0，两者虽有CMN仍非严格官方前端复现。
报告比较的是完整工程链路，不能把差异单独归因于模型架构。

公开推理前必须固定源／二进制／原始及适配模型／清单／审计／算法常量，并复核原生证据。
使用独立进程组控制器按 base03、base06、resnet29303、resnet29306、cn3403、cn3406 顺序各一次；
总超时3600秒、TERM后5秒KILL并wait，输出独占创建，失败保留且记not_run，不覆写、不按分数重跑。
本轮base因新P2需要一次共享提取而重提，B必须精确重现保存R0的Top-1／ROC／完整1%规则。
所有旧人物都已看过，不构成新盲测；真实机器人／手机原始登记的独立现场查询和家庭验收仍待执行。
详细冻结、全部36池结果和采集边界见[本轮验证报告](../../docs/verification/2026-11-05-speaker-prototype-model.md)。
