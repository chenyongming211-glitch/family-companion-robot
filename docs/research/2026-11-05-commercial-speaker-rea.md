# 商用声纹产品与 REA 实际分析（2026-11-05）

本轮完成产品资料核查，以及对 Picovoice Eagle 官方发布的 Linux 原生库和公开 SDK 封装的静态分析。实际使用 **REA 6.8.0（Reverse Engineer Anything）**，官网 [rea.tools](https://rea.tools/)，官方仓库 [morluto/rea](https://github.com/morluto/rea)。全部分析工具和目标文件在云端隔离目录运行、保存；Mac 只处理文档和 Git。

结论：继续保留 **Go＋sherpa-onnx＋ERes2Net-base＋多句约20秒登记（B）**。商业实现支持多段登记、有效语音积累、模板融合这条工程路线，但本轮没有证据证明 Eagle、HomePod 或小爱在我们的普通话跨设备任务上更准。新发现的具体差异是 Eagle 在所见路径保留均值向量长度，B 则再次归一化；可先将这一长度作为登记一致性诊断。后续再验证登记有效性、查询证据融合和多条件模板，不因厂商分数看起来更高就更换模型或放宽身份阈值。

## 1. 已经商用的产品

| 产品 | 官方确认的功能 | 本轮取得的证据 | 分析边界 |
|---|---|---|---|
| Apple HomePod／HomePod mini | 家庭内最多6位用户的声音识别，用于个性化音乐和个人请求 | [当前支持文档](https://support.apple.com/en-us/108397)；Apple 历年技术文章 | 本轮未取得或逆向 HomePod 固件；研究文章不能当作当前固件的精确实现 |
| Google Nest 等支持 Google Assistant 的家庭音箱／屏幕 | Voice Match，每个家庭最多6人，可开启个人结果 | [当前官方帮助](https://support.google.com/assistant/answer/9071681?hl=en) | 本轮未取得设备模型；语言和设备支持范围依产品而异 |
| 支持声纹功能的小爱音箱／小米智能音箱、电视小爱 | 登记声纹并关联账号、昵称、日程等，支持声纹管理 | [小爱语音政策的声纹管理说明](https://privacy.mi.com/ai-voiceassist/zh_CN)、[智能音箱政策](https://privacy.mi.com/SmartSpeakerPrivacy/zh_CN/) | 官方说明有服务器上传和存储；不能据此推出全部推理都在云端，也不能推出模型架构。并非每个音箱型号都必然支持 |
| Picovoice Eagle | 本地运行的商用声纹 SDK，提供登记和多模板匹配接口 | [官方 SDK](https://picovoice.ai/docs/eagle/)、[固定版本发布库](https://github.com/Picovoice/eagle/tree/7a117318154cdbd4b729460f968351e0dab32b53)及本轮 REA 输出 | SDK 产品，不是整机音箱；本轮分析其发布二进制，没有执行识别或取得准确率 |

这些产品证明声纹个性化已经商用，不能证明“同样输入、相同候选人数、相同陌生人误接纳约束”下优于本项目。家庭6人、固定唤醒词的个性化与任意短句、跨设备、陌生人、换人／重叠是不同任务。

## 2. 本轮真正用了 REA 做什么

选 Eagle 是因为厂商公开提供包含核心入口的本地原生库，能核查程序行为。仅分析小爱 App 的界面或登记请求，无法直接看见远端识别模型；本轮没有把该层面的资料核查称为模型逆向。

### 样本与工具身份

| 项目 | 固定值 |
|---|---|
| 官方源码／发布库提交 | `7a117318154cdbd4b729460f968351e0dab32b53` |
| 实际原生目标 | `lib/linux/x86_64/libpv_eagle.so`，ELF64 AMD64，1,261,648字节 |
| 目标 SHA-256 | `1518547098b0d2f7e6d04b18483b214a32a5d67c6b56a8055d4cb21aae72e3d7` |
| 原生库版本字符串 | `3.0.0`，从版本函数引用的只读数据读取，未调用目标函数 |
| Node／Web 封装 package.json | 均为 `3.0.1`；封装版本与原生库版本分别记录 |
| REA npm 包 | `rea-agents@6.8.0`，元数据仓库指向 `morluto/rea` |
| 隔离运行时 | Node `v24.22.2`，官方下载包与官方 SHASUMS256 核对一致 |
| 实际分析后端 | REA 原生导出表读取器、REA 的 GNU objdump 集成、REA JavaScript／TypeScript 静态分析器 |

未安装 Ghidra／Java，没有生成 C 反编译伪代码；原生结论来自汇编和只读数据的静态检查。工具部署没有修改本机 MCP 配置或云端全局 Node。目标的普通符号表已剥离，但仍有28条动态导出，包含登记、导出、重置和识别入口。`objdump` 对内部地址显示的“最近导出符号＋偏移”只是位置注释，不能当作内部函数真实名称或行为。

实际执行的 REA 命令形态如下；`REA` 为隔离 Node 执行官方 `scripts/rea.mjs`，`TARGET` 为上表原生库绝对路径，均在云端使用：

```text
REA --version
REA inspect-native-exports TARGET --format json
REA inspect-with-objdump TARGET file_headers --format json
REA inspect-with-objdump TARGET symbols --format json
REA inspect-with-objdump TARGET disassemble --symbol pv_eagle_profiler_enroll --format json
REA inspect-with-objdump TARGET disassemble --symbol pv_eagle_profiler_export --format json
REA inspect-with-objdump TARGET disassemble --symbol pv_eagle_process --format json
REA inspect-with-objdump TARGET disassemble --format json
REA analyze-javascript-application WEB_WRAPPER --max-heap-mb 256 --analysis-timeout-ms 60000 --format json
REA analyze-javascript-application NODE_WRAPPER --max-heap-mb 256 --analysis-timeout-ms 60000 --format json
```

另外对 `flush`、`reset`、采样率、登记帧长、识别最短输入和版本函数作了逐符号反汇编。没有加载 `.so`／`.node`、执行厂商 SDK、创建账号、申请或使用 AccessKey，也没有采集或处理真人录音。

### 实际观察到的逻辑

以下地址均为这份固定 ELF 的虚拟地址，不适用于其他版本。

| 观察 | 实际证据锚点 | 可以得出的结论／不能得出的结论 |
|---|---|---|
| 采样率16000Hz，登记输入帧512样本 | `pv_sample_rate` 的 `0x28420` 返回 `0x3e80`；`pv_eagle_profiler_frame_length` 的 `0x13fd0` 返回 `0x200` | 登记输入帧为32ms；**不能说32ms就足够登记或识别人**，识别最短输入另由对象配置计算 |
| 登记不是每调用一次就增加完成量 | `enroll` 在 `0x18aa8` 调用内部处理；`0x18ac1–0x18ac3` 检查返回的片段位置；只有对应分支在 `0x18b10–0x18b1d` 增加累计计数 | 支持按内部处理得到的片段积累；结合 API 的 `min_enrollment_chunks` 可解释完成量。不是录满一个墙钟时长即保证有效；完整片段选择规则仍未恢复 |
| 登记完成百分比与累计量有关 | `0x18ac5–0x18af4` 使用两个计数字段、除法、`fminf`；只读常量分别为1和100 | 可观察到累计量比例封顶100%的计算；字段语义结合公开 API 推断，并非恢复了原始结构体定义 |
| 不足登记量不能正常导出；导出模板做均值融合 | `export` 的 `0x13e7a–0x13e80` 检查累计／目标字段；`0x13ea1` 把累计数转浮点，`0x13ee0–0x13ee7` 及标量尾部分量除以该数 | 此路径导出累计向量的均值，支持多片段融合。不能单靠这一函数确认网络、全部归一化步骤或各类噪声处理 |
| 共享向量函数先对单段向量做 L2 归一化，再累加 | 内部入口 `0xfc70` 接收登记／识别传入的累计向量指针；`0x107e0–0x10860` 平方求和，`0x10872–0x1087e` 开方并取倒数；`0x108cb–0x108d9`、标量尾部和 `0x10a1a–0x10a28` 归一化后累加 | 结合 `export` 可确认所见路径为“单段归一化→累计→均值”，导出函数没有再对均值做 L2 归一化；不能把此局部路径扩展为所有平台、版本或全部推理分支的保证 |
| 识别也积累多个内部片段的向量 | `process` 在 `0x183d9` 调用与登记共用的内部片段处理入口；`0x183ec` 分支检查；`0x183fd` 增加片段计数，调用内部向量处理；`0x185f8–0x18602`／标量尾部按计数求均值 | 看到了识别输入内的片段级证据融合；这不证明 SDK 自动跨每轮对话继承同一个人的身份 |
| 每个模板先算向量内积，再做分段映射 | `process` 的 `0x186f0–0x18724` 及尾部乘加；`0x1877d–0x187be`、`0x188f0–0x18913` 比较、截断／分段线性映射；`0x187c0` 写对应模板分数 | 0～1输出是匹配分数；未见此路径将分数归一化为候选人物概率。不能把0.9解释为90%身份正确率，也不能把它与 ERes2Net 余弦阈值直接换算 |

公开封装和 [C API](https://github.com/Picovoice/eagle/blob/7a117318154cdbd4b729460f968351e0dab32b53/include/pv_eagle.h)提供了独立的语义参照：登记量参数与人声检测敏感度分开；多次同一人语音可用于登记；换人前清空登记状态；输出是各模板的相似分数。`voice_threshold` 是**人声检测**参数，不是**身份接纳**阈值。单人、自然声音、安静环境的要求也应与“模型自动解决多人／音乐”区分。

[Node 封装](https://github.com/Picovoice/eagle/blob/7a117318154cdbd4b729460f968351e0dab32b53/binding/nodejs/src/eagle.ts)默认 `minEnrollmentChunks=1`、`voiceThreshold=0.3`，不能把默认值当作适合我们场景的最佳值。[Web 封装](https://github.com/Picovoice/eagle/blob/7a117318154cdbd4b729460f968351e0dab32b53/binding/web/src/eagle.ts)的 `enroll`／`process` 明确转交 WASM 原生接口；本轮没有取得其 WASM 与模型权重。

REA 对 Web／Node 源码分别解析18／7个 JS／TS 文件，解析失败均为0，声明完整性矛盾均为0。两份图的覆盖状态均为 **partial**，语义关系还有未知项；不能把未恢复到的调用当作不存在，也不能凭静态图证明运行时网络行为。

一个可解释的优化线索：设每段归一化声纹为 `u_k`，均值 `m=mean(u_k)`。其长度 `||m||` 在数值误差之外不超过1，同方向向量的均值长度大，分散向量的均值长度小。因此“均值后再次归一化”会去掉这个一致性信息。所见 Eagle 路径对登记／查询均值做内积，数学上含有两边均值长度的影响；B 的余弦路径会消去长度。这是本轮静态证据结合数学得到的**推导**，不是已证明的识别率增益；长度也不能直接等同于音质、身份可信度或跨设备可靠性。

### 没有得到的东西

- 没有恢复 Eagle 的完整网络结构、训练数据、模型参数或训练配方；不能断言它使用 ERes2Net、ECAPA、AS-Norm 或某个特定模型。
- 没有取得小爱／HomePod／Nest 当前核心模型的二进制或云端代码。产品支持页、隐私政策和历史论文各有证据范围。
- 没有 Eagle 的普通话同设备／跨设备正确接纳率、误归率、陌生人误接纳率或时延实测。需要有效厂商 AccessKey 后才能作真正的 SDK 对照；本轮静态分析不需要它。
- 没有发现一种可直接照搬、已被本项目独立数据证明优于 B 的身份决策方法。

## 3. Apple 的公开工程经验说明什么

Apple [2018年 Personalized Hey Siri](https://machinelearning.apple.com/research/personalized-hey-siri)介绍过多次引导登记、由多个声纹向量形成用户档案、对相似度聚合，并利用后续接纳样本扩展档案；同时讨论了错误接纳污染档案的风险。这是历史上的唤醒词个性化方案，不能写成“2026年 HomePod 就是这样实现”。

[2023年 Voice Trigger System for Siri](https://machinelearning.apple.com/research/voice-trigger)进一步说明，多阶段触发检查、完整发言信息、更丰富训练数据及训练／推理目标匹配会影响效果。这里最适合借鉴的是引导采样和分阶段积累证据；唤醒词检查、设备主人验证与家庭多人任意短句识别仍须分别评估。

## 4. 对本项目的具体建议

当前 B 的登记融合已实现“逐条 L2 归一化→等权平均→再次归一化”，见 [fuseEnrollment](../../tools/speaker-lab/enrollment_study.go)。这与商用实现采用多片段融合的方向相符，最后一步不同。可先保留再次归一化前的均值长度作旁路诊断，再考虑是否值得在独立校准中使用；本轮没有发现足以推翻 B 的新模型。

最近的 [独立普通话试验](../verification/2026-11-05-speaker-backend.md)中，B 的PC条件仅2/14正确接纳、12/14拒识，陌生人0/100；AS-Norm／质量校准增加已知接纳时，陌生误接纳达到9～27/100（不同条件和方案）。这说明当前重点是**保持陌生人约束时增加可信证据**；0/100也不是总体误接纳率为零的保证。

| 顺序 | 待验证工作 | 本轮依据及预期目的 | 约束 |
|---|---|---|---|
| 1 | 完善多句登记引导，分别展示录音时长、有效语音量及重录原因 | Eagle 按内部片段累计；Apple 有引导登记。让用户知道安静等待、混入电视人声不等于有效登记 | 先作诊断和独立对照。已有 VAD 硬门禁损失覆盖的结果仍有效，不默认启用它或照搬厂商0.3 |
| 2 | 记录登记均值向量长度，并核查它与拒识／误接纳的关系 | 原生路径保留该长度；B 再次归一化会去掉它。这是比笼统“换个模型”更具体的差异 | 先作旁路诊断，不增加硬过滤。若试验保留长度参与打分，须独立重新校准；不得照搬厂商分数映射，也不能在已看过的测试集挑阈值后报增益 |
| 3 | 验证实际机器人麦克风登记，以及显式的多条件模板 | 直接覆盖我们真正要用的采集通道；多条件模板是针对当前跨设备问题的工程假设，**不是本轮已恢复的厂商专有方法** | 每种设备／条件留独立识别句；不可拿同一原录音、其切片或重放版本同时登记和评测。更多模板也会增加误接纳机会，须重新校准完整候选池策略 |
| 4 | 将已确认的短句证据复用设计做独立实验 | 识别输入内片段融合有静态证据；更长可信发言可能减少单句不稳定 | 跨句融合是本项目待验证设计。必须有时间／证据上限，换人、长间隔、重叠时清空；不能无限继承上一人身份。单句和融合结果分别报告，保留等待时延 |
| 5 | 对“归一化后相似度”与可显示的置信度分开标定 | Eagle 分数有映射，模型间数字不能直接比较；当前开放集问题已经实测暴露 | 冻结阈值和候选人数；在陌生人误接纳受限的工作点比较正确接纳、误归、拒识。不得从最终测试集反选阈值 |
| 6 | 将 Eagle 作为可选商用对照，保留现有 Go 主线 | 其本地 C API 可形成可比基线，但尚无本项目效果证据 | 有可用 AccessKey 后才运行；登记／查询源录音、条件和人物划分一致，各模型独立开发集校准，报告人均／短句与时延；旧数据只作开发／回归，最终增益用新保留集验证 |

目前不采用自动把“系统刚认定为本人”的音频写回长期模板。已有陌生人误接纳问题时，这会形成越学越错的循环；将来如验证更新，须有独立可信身份确认、本人同意、版本回退与独立测试。维持已确认的登记授权和隐私边界。

以上是下一阶段候选工作，没有在本轮实现或部署，没有新增准确率或验收结论；也没有继续重跑已完成的 B／AS-Norm／质量校准实验。

## 5. 证据保存与核对

云端隔离根目录：`/opt/family-robot-validation/speaker-products-rea-20261105-df9e83b/`。

| 保存项 | 内容 |
|---|---|
| `evidence/eagle-tree.json`、`eagle-source-commit.json` | 官方固定提交及 Git 树元数据 |
| `evidence/eagle-target-files.json`、`eagle-additional-source-files.json` | 共36个目标／封装／说明文件的长度、Git blob SHA-1、SHA-256；下载与收尾复核 |
| `evidence/native-*.json`、`pv_eagle_*-asm.json`、`pv_sample_rate-asm.json` | 共13份原生 REA 证据。objdump 输出均 exit0、未截断、目标摘要前后一致；普通符号表为空如实保留 |
| `evidence/web-sdk-static.json`、`node-sdk-static.json` | 两份 REA 源码静态图，明确 partial 覆盖；进度信息保留在 stderr，不能当作识别运行日志 |
| `evidence/native-constant-reads.json` | 汇编引用的少量常量／版本数据读取来源，ELF load segment 映射已核对 |
| `evidence/findings-summary.json` | REA证据ID、证据文件摘要、样本摘要、解析统计和覆盖边界 |
| `evidence/static-analysis-validation.json` | 文件复核数、证据复核数、分析进程清理状态、未执行厂商SDK／语音运行测试标记 |
| `evidence/protection-before.json`、`protection-after.json` | 正式两服务 PID／状态／NRestarts，以及二进制／unit／env共5项保护摘要；不包含凭据内容 |

本轮完成静态来源与输出核对，正式服务保护项前后一致，分析子进程已结束，临时 SSH 控制连接及本机临时空目录已关闭清除。工具、目标二进制和原始分析 JSON 只留云端，未进入 Git 或 Mac；没有权重／原始音频／凭据入库，没有正式服务、阈值、固件、VAD／AEC或麦克风变更。家庭现场和生产声纹仍未验收。
