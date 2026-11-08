# 表情编排与名称配置：实施验证

## 授权与基线

用户2026-11-08明确要求按定稿方案开发。按[首版v1.0](../superpowers/specs/2026-11-08-expression-orchestration-design.md)和[完整计划](../superpowers/plans/2026-11-08-expression-orchestration.md)连续实施；代码直接main，运行测试/构建/模型在云端，Mac仅代码/Git/USB客户端。

启动时main32baa67；计划与20场景预注册在9e42680提交。正式Go `58c14cd-window600-chick-22e7d4c-r1`，健康ready/memory_ready、active0、诊断全false，8项程序/配置摘要及旧网关PID已保存为云端私有before.json。微雪仍2beda8f表情基础版本（完整app槽MD5 `4ceaabaca8a82709080a402cc01b67cb`）；资源/分区/鉴权保持原基线。

## 验证范围与状态

| 项目 | 当前进度 |
| --- | --- |
| 设备名称存储、CLI、新会话快照 | 已实现，云端四包race/vet/build、独立规格与质量审查通过；尚未发布 |
| 绑定表情的DMA时间与生命周期 | 已实现并接线，云端host/sanitizer与独立规格、质量复核通过；尚未发布 |
| 固件新能力/协议与界面接线 | 最新32项云端回归及独立规格、质量复核通过；尚未发布 |
| 无声模型情绪分支与Go回复绑定 | a0fd847已实现，全仓race/vet/build及规格、质量复核通过；尚未发布 |
| 20场景真实模型及语音开/关对照 | 首轮真实选择器20/20通过；配对语音/物理窗口仍待测 |
| 最小正式来源发布/目标构建/实机联验 | 待完成 |

本表区分代码、模型与整体验收。已完成20次独立真实表情调用；没有新的真人声纹成绩，尚未部署本版功能。独立审查与测试日志将随任务补充；不以选择器GREEN代替整条物理链路验收。

## 设备名称模块

已增加独立版本化资料存储、云端管理CLI与两种鉴权入口的新会话名称快照。默认名称读取产品配置；未改离线唤醒词，管理结果明确为`effective_wake_word=unknown`。改名保留设备ID、记忆归属和已建立会话的旧快照。自定义名称仅作为转义JSON资料引用，原默认提示与冻结差分测试保持。

除初始缺失实现RED外，额外回归抓到路径别名、名称与清记忆命令重叠、CLI默认名称及名称JSON引用问题，均保留RED后修正。规格复核发现合法名称含问号或引号时，前缀命令被正文安全检查误拒；改为先认完整裸命令，再剥离配置名称，最后检查正文。8项旧失败重现后，新增正文问句、引文、换行、复合请求和其它设备目标拒绝回归通过。

root对云端`task1-names/green`运行四包race、vet和两个命令构建通过后，独立质量审查发现合法名称“不要”“他说”会把原否定/转述误剥离为删除。新增RED确认后，带名称的删除命令统一要求名称后用中文/英文逗号分隔，不增加名称限制；裸命令不变。最终四包race为`deviceprofile`2.500秒、`realtimestate`2.031秒、`realtimelive`16.437秒、管理CLI 1.033秒，相关vet/build退出0。root复核日志并再次核对13份本机源码摘要与该云端快照一致。证据在私有`task1-names/delimiter-{red,green,vet,build}.log`及先前日志；独立规格、质量复核均已通过。

## 实际播放时间与情绪生命周期

播放观察增加最多16条首个/最后DMA完成时间及终结事实，最终回报移除记录后仍保留有界墓碑直至槽位复用；回报、队列和DMA尺寸不变。纯控制器通过轻量身份等待实际开播，按标签真实到达时间执行2000ms窗口，长回复保持主情绪，正常结束以最后实际DMA时间起最多保留2秒。隐藏/息屏持续消费结束事实；旧临时接口在绑定作用结束后仍可使用，保留原5秒上限。

初始5个缺失行为先RED；追加RED覆盖未观察开播时的晚到标签、旧接口永久失效、时间回退、旧连接、无效观察及排空终结。真实ledger测试覆盖旧尾音、新回复首次DMA、六秒连续播放、延迟seal和最终回报后的结束时间。首次整组回归有2项失败，原日志保留；修复后17项通过。

独立规格审查另发现“已进入物理Active”不能证明选中情绪已经显示：标签在两帧间到达后立即排空、以及息屏期间激活，都会把未见过的情绪带到尾音。两项反例先RED后增加`ConfirmShown`，仅合格的可见Speaking帧确认选择；排空只保留该确认过的情绪。旧情绪已显示后隐藏/恢复的正例仍通过。质量审查又抓到实际末帧已完成、UI尚未更新时接纳晚到标签，并在正常结束后首次显示的问题。追加3项RED，正常PostSpeech统一要求已显示，且标签到达时间不晚于实际末帧；提前收到但从未显示的情绪也不在结束后补显。

最新`green-final-4.log`17项／24.510秒／退出0，包括生产模型/渲染、实际View/Display、播放core/adapter/WebSocket、微雪codec、取消和参考缓冲，以及新增时序子用例；host C++包装器启用ASan/UBSan。root核对日志及9份本机/云端源码摘要一致，规格复核还独立运行终结、保留、DMA和排空用例通过。质量复核独立运行全部15个表情时序子用例通过（1.270秒），两阶段复核均已批准。

Task2提交时的云端64位host精确尺寸如下；Task3的额外8字节在后节单列。这不是ESP32栈高水位或物理性能证明。

| 类型 | 旧版字节 | 本版字节 |
| --- | ---: | ---: |
| 非CHICK播放ledger | 7320 | 7320 |
| CHICK播放ledger | 7352 | 7584 |
| 输出观察 | 32 | 544 |
| 合并音频观察 | 72 | 584 |
| FaceFacts | 88 | 600 |
| FaceModel | 312 | 1136 |
| 新控制器 | — | 816 |

CHICK ledger实际增加232字节；条件静态上限为CHICK 7616／其它7360字节。PCM block240、queue12、DMA slots6和records16保持。目标编译、栈高水位、协议/View接线和实机仍属后续验收，不由本模块host结果替代。私有证据在`task2-dma/`，最新完整源码归档`task2-reviewed-candidate-4.tar.gz`；旧归档与失败日志保留。

## 固件协议和显示接线

新增独立`face_expression_v1`协商。接收端在解析/排队前捕获真实到达时间，协议锁内检查scope、连接、已开始播放ID、取消lease和15规范情绪；主任务再验身份。Display采用默认标量no-op入口，CHICK View转发到纯控制器。无确认的新绑定消息不回落到旧情绪入口，状态提示仍独立。L0文案已改为“正在启动”“正在说话”“有新提示”。

接线RED/GREEN补了四种真实时序：接收时间早于UI最近观察仍是合法排队事件；早到但开播后才执行的动画起点取到达/首个DMA时间的较大者；逻辑Speaking先于显示刷新时先观察当前事实再接纳标签；音频观察与模型消费在原Display锁下串行，并在音频/PMIC工作后取当前时间，避免真实ISR的新时间戳或并发旧快照被误认成时钟回退。原音频getter释放codec mux后才读输入队列，队列仍用try_lock，未新增音频锁。

独立规格审查抓到两个旧协议问题：排队legacy闭包无连接身份，以及只删除一个emotion字段使重复成员绕回旧入口。均有生产原生片段的行为RED；现已完整消费含emotion的LLM消息，legacy字符串携带接收连接并在主任务原子检查当前连接/协商。连接级旧网络情绪在替换/显式关闭时清除，本地系统情绪保持原语义。cJSON替身改为保留有序重复成员、首次读取及单项删除，与实际库的相关行为一致。该连接字段使host FaceModel由1136增到1144字节，其它类型尺寸不变。

质量审查再发现正常stop可能早于首次DMA：一帧1440样本已经排队且正常seal，原生Stop先进入Listening，使早到标签被误清。新增合法60ms帧／6个240块的生产scheduled-stop与实际View/ledger RED；修正后有效、同连接、未终结/取消/排空且DMA首末仍为零的记录继续保留等待资格。首个实际DMA在70ms时显示Happy；逻辑stop后到达的标签可等待，首次DMA前取消仍不显示。既有聆听接管、错误/页面、排空和真实终结保护继续通过。

最新私有`task3-protocol/quality-fix1-tests.log`32项／45.222秒／退出0，覆盖生产protocol/View/Display/模型、旧播放/唤醒、微雪生命周期/电源以及C3行为。root复核日志并独立核对15文件SHA匹配；最终规格、质量复核均已批准，质量审查独立重跑3项sanitizer/集成测试通过（11.147秒）。锁定归档新准备树共23补丁、68 overlay；0023单独对patch22基线`git apply --check --whitespace=error-all`退出0，且只改5个原生文件（protocol.h、websocket_protocol.h/.cc、application.h、display.h），与overlay交集为空。证据为`task3-quality-source-proof.json`及各版RED/准备日志，不覆盖原失败。

在既有150帧一次性profile点增加当前任务最小剩余栈与任务名，不增加timer或逐帧栈扫描。固定ESP-IDF提交`fff9895c82d744c7237be8847347bdd1b07c6643`的task.h明确单位为bytes，私有下载SHA为`34fa5db62cfcb5b0007d3a3b79eabd4217554f8f8201f7f43dac68592fe0fb04`。这只准备实机测量入口，尚无新固件实机余量或帧率结论。

## Go无声选择器与回复绑定

Task4提交a0fd847采用独立Native文字/工具连接，关闭该分支搜索，只接受一次完成的规范`set_expression`调用；总预算2000ms含连接，不重试。主语音配置不变，选择器不进入首包等待路径。名称和当前转写以转义JSON传入，只投影有实际播放证据的有界背景。独立开关默认关闭，旧客户端或仅face_state_v1客户端不新增请求/消息。

回复绑定覆盖早到等待、start→emotion→audio顺序、正常生成封口后合法晚到、取消/新轮/断连/最终回报失效。质量回归又补18种意外音频/文本/转写/拒绝输出：即使后续有合法工具也须中性回退；保留RED后修正。最终同步记录写入之后再核对总期限，跨预算结果不得成为选中标签。

最终定向race6.304秒，全仓race58.391秒（realtimelive21.662秒），vet1.271秒、build5.400秒，均退出0；规格与质量复核通过。root独立核对183份Go/module源码摘要匹配云端，manifest SHA256 `e8e4034d2f45daf2ca4e5f02460da401eee69e8faeba16a5ca497be519206a48`。私有证据为`task4-go/logs/green4-*`与保留的各版RED。尚无真实提供方语义/耗时或物理播放成绩。

## 真实场景验证工具

新增`tools/face-expression-check`，默认仅校验冻结场景，零配置读取/推理/输出写入；显式`--run`才从受保护云端配置调用生产`SelectExpression`。20例串行、每例一次入口、无重试；仅completed、规范且在预注册集合内、固定2000ms内、实际请求数1才通过。超时/错误的neutral不算成功。E19已播文本逐字映射生产`assistant_spoken_prefix`。

真实调用前保存原始fixture、来源/版本与初始计划；每次调用前后保存不可覆盖的启动凭证、逐例和前缀汇总。失败、取消、部分usage及写入异常均保留，最终非零退出；不伪称测到物理DMA/播放窗口。最终云端工具race10项顶层测试／1.767秒、表达分支race6.301秒、vet/build退出0。独立规格/质量复核通过，质量复核再次运行fake-only race1.844秒及vet通过。root核对188份来源摘要与云端一致，manifest SHA256 `c9f84c474d5f56995ee6efe71bde862f8cd3cf5bfa0386f032fa1e1b21297c7f`。各版RED及一次漏打包公共identity fixture的无效检查均保留；没有用其替代最终GREEN。

固定语音对照E01/E07/E15/E17已在云端通过edge-tts7.3.1生成、ffmpeg转为16k单声道s16le WAV；首次对照前登记每份WAV/原MP3/文案摘要、选择器场景摘要、来源manifest及off,on,on,off顺序。私有证据`voice-inputs-v1/manifest.json`和`voice-preregistration-v1.json`；没有播放现场、真人录音或声纹成绩。工具阶段尚未发生真实表情调用，配对语音仿真器正在实现。

## 首轮真实选择器结果

工具提交5d3719b后，root逐份将188个来源摘要与该提交的Git内容对照，登记提交、manifest、fixture、工具二进制及版本，再运行`selector-real-v1`。原始清单摘要仍为`0529f0e2b608b31955731fe13a441001d1744457938567fad7b60ff6ff8e9ed5`，集合和2000ms上限未修改。20例全部completed、规范且在预注册集合内，恰好20次请求，无重试、超时、默认或缺失usage。root独立读取20份逐例文件重算，与汇总一致。

| 选择器指标 | 首轮结果 |
| --- | ---: |
| 合法、语义集合和期限同时通过 | 20/20 |
| 总耗时中位数／P95／最大 | 577.5／769／1122ms |
| 连接耗时中位数／P95／最大 | 126／145／159ms |
| 输入／输出／合计token | 12821／534／13355 |

E01/02/17为happy，E03loving、E04embarrassed、E05confident、E06relaxed、E07confused、E08surprised、E09delicious、E10silly、E11sad、E12sleepy、E13angry、E14crying，E15/16/19/20neutral、E18confident。证据为私有`task5-tool/selector-real-v1/`、注册与独立重算凭证；summary SHA256 `dde59e1e820e406a4dae87c1d42cdd64f52f885171bbd0f678cf518d37539338`。这只证明合成场景的选择器结果，未测物理DMA生效率或设备情绪与实际语音联动。

## 目标构建与候选来源

C3退休后，df13ea5117c2852ac7684601b23d3f3d68cc3d51的四S3目标[42450694685](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/42450694685)及独立原版[42450694646](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/42450694646)均success。当前main固件树与df13ea5相同（`115ae2e930e6969f9cd1a43b2be189a4ea4ff37f`）。微雪ZIP摘要`4ab4e49b2c40f57d47909f435914220e91e4b682a44acfe31577482fe44d95dd`及全部SHA256SUMS核对通过；CHICK开启、0021关闭、AEC/阈值/分区/资源保持。离线安装预检通过，未打开USB。候选完整3MiB槽SHA256 `5fa2287737cb4e6faa85c63d7e02d0a33d32da10c0bdce9a74901a07b0234085`、MD5 `fe957c389750c58c20b949bb90755e43`、ELF `e433c7e5a0f95afe85a3457c38bc5c234f22ba01e321e5dc50ce03ebcfddba29`；尚未安装。

仅Task1+4移植到正式来源的`limited-source-expression-v1`已完成全仓race（realtimelive22.057秒）、vet/build，130个Go/module摘要固定；`config.go`仅适配新增字段，未带入MemoryOnly/speaker实验。完整功能规格/质量终审均批准，并独立复核限定来源。预构建候选`58c14cd-window600-chick-expression-a0fd847-r1`及名称CLI已准备，二进制SHA256分别为`1fbbb91f103a00473cef67c3c7bb4cdc86eef364cdbf72f7eb2840863721a56d`、`a0987ac65661761f88a6d94cb52607cc1d190f8ff13bf8be8aaa1f5841b59e24`。此时仅准备，配对语音/资源与实机仍是发布验收门槛，正式服务未更新。

## ESP32-C3退出支持

用户2026-11-08明确停止支持C3，当前开发/发布门槛只覆盖S3（微雪、M5）。活动构建矩阵移除C3、保留四个S3变体与独立原版基线；三个C3专用工作流及旧说明原文移至[退休资料](../development-archive/2026-11-08-c3-retirement/README.md)。旧来源和证据保留。

统一host入口排除C3专用模块/方法及网络/提示音fixture准备，通用flash防伪测试改用真实S3芯片/分区fixture。规格审查抓到原共享测试仍使用C3布局，先RED确认旧OTA0xd000与S3所需0xf000不同，再修正并保留原文件。最终云端`c3-retirement/supported-tests2.log`为123项通过／53.615秒，root与质量复核均核对活动runner、flash测试及CI文件摘要。规格、质量复核通过；四S3目标构建仍须等待本轮精确源码SHA，不用旧五目标结果替代。

## 预注册与产物边界

固定清单：[20场景](fixtures/2026-11-08-expression-scenes.json)。清单在首次表情模型评估前提交，每例至多一次选择请求，正常场景的超时/默认不得算成成功显示目标情绪。失败与无效尝试保留，改集合或时限须记录新版本及原因。仿真回报仅验证服务端协议，不当作物理DMA或声学通过。

云端私有工作根`/opt/family-robot-validation/expression-v1-20261108/`，本机私有控制与元数据`.superpowers/expression-v1-20261108/`。缓存的锁定上游归档和网络fixture复用已校验输入。凭据、运行JSON、原音、设备备份、生成固件和日志不入Git。首次子任务RED/GREEN、人工发现并补充的行为回归均保留原日志，不覆盖失败记录。

## 发布来源预检查

Task1功能差分已对实际正式来源`chick-face-20261108/go-release`执行只读`git apply --check`。`config.go`和`server.go`上下文不直接匹配：正式来源没有main的`MemoryOnly`等后续实验配置。其余文件未报冲突。此检查不是构建或发布通过；后续必须复制正式来源到新私有目录，仅适配本次资料/表情增量并重新验证，不能用整包main覆盖。正式来源未被修改，服务未更新。

已将该来源复制到云端私有`limited-source-names`，仅移植Task1差分并在上述两处适配资料字段/初始化/会话构造，不带入main的实验配置。四包race为2.538／1.910／16.332／1.043秒，相关vet与命令构建退出0（`limited-names-{race,vet,build}.log`）；原来源两文件摘要保持，准备记录明确`deployed=false`。这是名称增量的来源适配，不是尚未完成的情绪选择器或正式发布验收。

新协议补丁的云端原生基线已按锁定归档和现有0022准备到私有`protocol-baseline-patch22`；生成0023时以provenance中的overlay清单排除覆盖文件，仅保留原生/vendor差分。

Task2提交`2912f1c5544ff9177618381ff9f929602866894a`已推main，五目标CI [42445453846](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/42445453846)全部success（微雪、M5产品/软件AEC/原变体、C3）。后续协议变更仍需最终精确SHA的目标构建，不将本轮host或中间CI当作完整交付，未下载中间产物刷机。
