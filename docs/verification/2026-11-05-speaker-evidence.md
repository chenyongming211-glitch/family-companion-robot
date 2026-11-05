# 登记诊断与有界证据积累验证（2026-11-05）

隔离软件已实现，云端工程检查通过；查询积累在已看过数据的交替顺序中出现明显伤害，目前不建议部署。

本轮交付隔离的 Go 声纹研究工具。登记诊断提供一致性／重录提示，不自动删除登记段；查询在训练拟合的连续性门槛和固定窗口边界内融合已提取向量。保留 Go＋sherpa-onnx＋ERes2Net-base 和多句约20秒登记，没有更换或部署模型，没有接正式身份决策。

所有公开人物均为 `seen_development_regression`；顺序为 `constructed_sequence`，`new_blind_test=false`。`human_acceptance=false`、`production_acceptance=false`、`feature_enabled=false`，家庭现场和端到端时延评估均 `unavailable`。单人公开录音标签不代表已经实现真实重叠检测。工程通过、旧数据效果和实际设备／家庭验收分别陈述。

## 固定代码、输入与运行

设计和实施计划在推理前已批准；运行时算法为 `2af3e0a`，新增原生测试为 `4e0b99c`。Mac 只编辑／格式化／Git；依赖、测试、构建和原生运行全部在既有云端隔离目录完成，没有额外安装依赖。

云端证据根目录：`/opt/family-robot-validation/speaker-evidence-20261105-c5e730a/evidence/`。WAV、模型、运行 JSON、二进制和向量不进 Git；向量只在内存，显式清理。本文只保留匿名汇总。

| 冻结项 | SHA256 |
| --- | --- |
| ERes2Net-base ONNX | `1a331345f04805badbb495c775a6ddffcdd1a732567d5ec8b3d5749e3c7a5e4b` |
| Device03 manifest | `a4cee902c9835910c2619a043725683fa56cd75173d5b5f86461d8469f7fc04d` |
| Device06 manifest | `29c054a30db23d0cf3ec465d270d2cb63118514f20c242e52c089f553d6c3745` |
| 实际 `speaker-lab-task3` binary | `84704def0689b42a13f72e1842664b6db44a351dda2ada6e566927d078e144a7` |
| `approved-run-config.json` | `0a195cd9f4e014d4fa602a9bc4bbd927c0c5933390d2e50d890c895e426d791b` |
| 原生合成 fixture manifest | `3fd20f2d4760d46afac5b3160b4de9b9b2281a86ba3e8a8bf3b9e45889b0a824` |

配置冻结41个源码／module文件的实际摘要（含测试和根 `internal/speaker` 依赖），与本机逐项比对；运行前后核验源码、二进制、模型、清单和全部审核摘要。`frozen-input-audit.json` 已逐条核验 WAV SHA、格式、样本数、审核绑定、人物角色隔离及录音／来源组唯一；两条件各971条、911条非 cohort 配对 key 相同，登记 WAV 保持。角色数量如下。

| 角色 | 条数／条件 | 人数 |
| --- | ---: | ---: |
| train_enrollment / train_query | 72 / 58 | 10 |
| validation_enrollment / validation_query | 364 / 253 | 50 |
| validation_unknown | 25 | 5 |
| test_enrollment / test_query | 25 / 14 | 4 |
| test_unknown | 100 | 20 |
| cohort | 60 | 30 |

每查询约2～4秒，最大63979样本；登记每人至少320490样本（约20.03秒）。总录音长度不冒充有效人声长度，既有 VAD 只作诊断而不新增硬门禁。cohort保持输入合同，本方法不利用其人物反向拟合门槛。

固定16000Hz、≤128000合格样本／4段、相邻间隙≤24000样本、源坐标跨度≤192000样本；构造间隙4000样本。margin网格固定 `[0,.01,.030229,.05,.1]`，陌生经验目标1%，预算 `floor(unknown_queries*.01)`。每组／顺序／候选数独立在validation标定，然后应用旧test。没有根据本轮结果调整或选择这些参数。

## 工程验证

`task3-red.log`：原生测试先接空测试 harness，显式启用后因缺少真实链路及诚实元数据而失败，exit1；未改变运行时代码。补上严格合成 loader→真实 worker 提取→内部分析→匿名 JSON→实际子进程／socket清理后，`task3-native.log` exit0。

最终源的 `task3-ordinary.log` 和 `task3-race.log` 均387通过／9默认opt-in跳过／0失败；后者61.154秒、无race报告。`task3-vet.log`／`task3-build.log` 均exit0。新增测试独立运行于最终二进制：`task3-native-final.log` exit0、5.297秒，15条真实原生提取、512维，278条明确复用的合成分析案例，诊断／连续性均fitted，两个顺序三候选池共观察152次积累。此数量仅为合成链路覆盖，不是识别增益。

测试检查四组身份结果的诊断等价、固定合成／非验收元数据、不输出私有路径或向量、窗口音频上限、禁用状态不能积累、实际worker已回收、socket已移除、原生向量清零。合成声音会复用到不同匿名位置，不具备人物角色独立性，不冒充 `public_human`。严格公开 loader保持。原8项opt-in与新增1项均默认skip；本轮不重复执行旧长测或根module旧全量测试。

可执行构建／测试命令见 [speaker-lab README](../../tools/speaker-lab/README.md#登记诊断与有界证据积累隔离-evidence-study)。公开回归由已冻结配置的云端 `run-experiments.py` 按Device03→Device06各一次执行；每次进程组超时1200秒，TERM后5秒仍存活则KILL，并wait回收。控制器拒绝覆盖既有成功输出，保留原控制器和新增资源／后代进程清理版本。本轮没有重跑已完成的旧 `backend-study`；原B仅读取保存的R0结果。

## 效果判断：顺序依赖，暂不采用查询积累

登记诊断两条件均在64名登记人物中提示3人／3段需复核，但未取得真实重录素材；诊断臂和对应对照的身份输出完全相同，不能据此宣称增益。

| 条件／顺序 | validation4正确/253：B→积累 | validation50正确/253：B→积累 | 旧test正确/14：B→积累 | 旧test陌生误接纳/100：B→积累 |
| --- | --- | --- | --- | --- |
| Device03 paired_bursts | 224→224 | 222→220 | 13→14 | 0→0 |
| Device03 alternating | 224→46 | 222→41 | 13→3 | 0→0 |
| Device06 paired_bursts | 188→199 | 170→178 | 2→2 | 0→0 |
| Device06 alternating | 188→188 | 170→170 | 2→2 | 0→0 |

以上是独立validation标定后的正确接纳计数，全部分母不变，不是Top-1准确率。配对顺序有局部改善，但不能选择性报告为总体增益：Device03交替顺序显著增加拒识，Device06旧4人测试仍仅2/14正确接纳。两条件全部组的旧test陌生误接纳均0/100，但100条／20名已看过陌生人不能给出总体FAR或生产安全保证。

Device03交替顺序在validation4与validation50各有2个事后混入异人的窗口；连续性门槛只在训练异人对上零通过，并未保证新片段不会误合并。保存输出显示其1%validation阈值由原0.460095（4人）／0.517548（50人）变为0.832361，margin仍0.1；旧test继承对应4人工作点。这与大量拒识同时出现；本轮没有反事实重跑或调参，不能从两处混人计数单独证明全部伤害的因果链。Device06交替validation50也出现1个混人窗口。全部重置、融合数和分母如下完整列出。

结论是候选积累的效果依赖构造顺序，当前不能推荐部署；保留B为已有对照，也不把B的跨设备2/14说成实用验收。诊断提示可供后续真人重录流程验证，尚不能证明其提升身份效果。下一步效果判断需要实际机器人麦克风采集和新的独立保留集，本轮不改变任何正式策略。

## 两次公开旧数据回归结果

下表小数展示四舍五入到6位，不能拿展示值重设阈值；精确值保存在云端成功运行JSON和冻结配置中。

两条件各一次，顺序Device03→Device06；全部406项汇总／语义核对通过。baseline在两个顺序、三个原候选池的Top-1、配对诊断、1%标定和两个固定规则均与已保存R0逐项相等，包括完整validation calibration、全部known/unknown计数、group/person计数和宏平均。诊断臂与对应对照的完整pool结果逐项相等。仅比较已保存旧输出，没有重跑旧B。

| 条件 | exit | stderr字节 | 进程墙钟秒 | 需额外组清理 | 结果SHA256 |
| --- | --- | --- | --- | --- | --- |
| device03 | 0 | 0 | 141.127602 | False | da6155bbc318ba202b34e4740a77f53e4fc2e6738e6086e591ae1dbf5e49ca96 |
| device06 | 0 | 0 | 138.115087 | False | 4b33d0a927a96e3d396b1cc98c2f0f4e6ab4859b5f347c442f23890ca081a8cf |

### 拟合与登记提示

| 条件 | 诊断状态 | 诊断线 | 登记训练人数 | 同人登记对 | 连续性状态 | 连续性线 | 查询训练人数 | 同人查询通过/总对 | 异人查询对 | 疑似人物/全部登记人物 | 疑似段 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| device03 | fitted | 0.405120 | 10 | 226 | fitted | 0.439331 | 10 | 147/156 | 1440 | 3/64 | 3 |
| device06 | fitted | 0.405120 | 10 | 226 | fitted | 0.543141 | 10 | 109/145 | 1340 | 3/64 | 3 |

门槛只用训练角色；登记提示没有删段或改变模板。训练经验零异人通过不代表总体误合并率为零；训练同人通过比例也不是身份接纳率。

| 条件 | 角色 | 全部 | RMS/长度合格 | VAD诊断合格 | 两者合格 |
| --- | --- | --- | --- | --- | --- |
| device03 | cohort | 60 | 60 | 60 | 60 |
| device03 | test_enrollment | 25 | 25 | 25 | 25 |
| device03 | test_query | 14 | 14 | 14 | 14 |
| device03 | test_unknown | 100 | 100 | 72 | 72 |
| device03 | train_enrollment | 72 | 72 | 72 | 72 |
| device03 | train_query | 58 | 57 | 55 | 54 |
| device03 | validation_enrollment | 364 | 364 | 364 | 364 |
| device03 | validation_query | 253 | 224 | 224 | 202 |
| device03 | validation_unknown | 25 | 19 | 21 | 18 |
| device06 | cohort | 60 | 60 | 60 | 60 |
| device06 | test_enrollment | 25 | 25 | 25 | 25 |
| device06 | test_query | 14 | 13 | 9 | 9 |
| device06 | test_unknown | 100 | 69 | 88 | 68 |
| device06 | train_enrollment | 72 | 72 | 72 | 72 |
| device06 | train_query | 58 | 55 | 52 | 49 |
| device06 | validation_enrollment | 364 | 364 | 364 | 364 |
| device06 | validation_query | 253 | 231 | 152 | 148 |
| device06 | validation_unknown | 25 | 25 | 21 | 21 |

### 四组身份结果（全部分母）

候选池表示“split/每组候选人数×组数”。known总/合格和unknown总/合格均保留门禁失败；正确/误归/拒识按全部known计数；未知误接纳/拒识按全部unknown计数。Top1列为正确/全部/合格，只是排名诊断。1%规则的阈值与margin在各组、顺序、候选数的validation冻结；test/4应用对应validation/4工作点。

| 条件 | arm | 顺序 | 候选池 | known总/合格 | 正确/误归/拒识 | unknown总/合格 | 未知误接纳/拒识 | Top1正确/总/合格 | threshold | margin |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| device03 | B_baseline | paired_bursts | validation/4×13 | 253/224 | 224/0/29 | 25/19 | 0/25 | 224/253/224 | 0.460095 | 0.100000 |
| device03 | B_baseline | paired_bursts | validation/50×1 | 253/224 | 222/0/31 | 25/19 | 0/25 | 224/253/224 | 0.517548 | 0.100000 |
| device03 | B_baseline | paired_bursts | test/4×1 | 14/14 | 13/0/1 | 100/100 | 0/100 | 14/14/14 | 0.460095 | 0.100000 |
| device03 | B_baseline | alternating | validation/4×13 | 253/224 | 224/0/29 | 25/19 | 0/25 | 224/253/224 | 0.460095 | 0.100000 |
| device03 | B_baseline | alternating | validation/50×1 | 253/224 | 222/0/31 | 25/19 | 0/25 | 224/253/224 | 0.517548 | 0.100000 |
| device03 | B_baseline | alternating | test/4×1 | 14/14 | 13/0/1 | 100/100 | 0/100 | 14/14/14 | 0.460095 | 0.100000 |
| device03 | B_enrollment_diagnostic | paired_bursts | validation/4×13 | 253/224 | 224/0/29 | 25/19 | 0/25 | 224/253/224 | 0.460095 | 0.100000 |
| device03 | B_enrollment_diagnostic | paired_bursts | validation/50×1 | 253/224 | 222/0/31 | 25/19 | 0/25 | 224/253/224 | 0.517548 | 0.100000 |
| device03 | B_enrollment_diagnostic | paired_bursts | test/4×1 | 14/14 | 13/0/1 | 100/100 | 0/100 | 14/14/14 | 0.460095 | 0.100000 |
| device03 | B_enrollment_diagnostic | alternating | validation/4×13 | 253/224 | 224/0/29 | 25/19 | 0/25 | 224/253/224 | 0.460095 | 0.100000 |
| device03 | B_enrollment_diagnostic | alternating | validation/50×1 | 253/224 | 222/0/31 | 25/19 | 0/25 | 224/253/224 | 0.517548 | 0.100000 |
| device03 | B_enrollment_diagnostic | alternating | test/4×1 | 14/14 | 13/0/1 | 100/100 | 0/100 | 14/14/14 | 0.460095 | 0.100000 |
| device03 | B_accumulation | paired_bursts | validation/4×13 | 253/224 | 224/0/29 | 25/19 | 0/25 | 224/253/224 | 0.460095 | 0.100000 |
| device03 | B_accumulation | paired_bursts | validation/50×1 | 253/224 | 220/0/33 | 25/19 | 0/25 | 224/253/224 | 0.521149 | 0.100000 |
| device03 | B_accumulation | paired_bursts | test/4×1 | 14/14 | 14/0/0 | 100/100 | 0/100 | 14/14/14 | 0.460095 | 0.100000 |
| device03 | B_accumulation | alternating | validation/4×13 | 253/224 | 46/0/207 | 25/19 | 0/25 | 222/253/224 | 0.832361 | 0.100000 |
| device03 | B_accumulation | alternating | validation/50×1 | 253/224 | 41/0/212 | 25/19 | 0/25 | 222/253/224 | 0.832361 | 0.100000 |
| device03 | B_accumulation | alternating | test/4×1 | 14/14 | 3/0/11 | 100/100 | 0/100 | 14/14/14 | 0.832361 | 0.100000 |
| device03 | B_combined_diagnostic | paired_bursts | validation/4×13 | 253/224 | 224/0/29 | 25/19 | 0/25 | 224/253/224 | 0.460095 | 0.100000 |
| device03 | B_combined_diagnostic | paired_bursts | validation/50×1 | 253/224 | 220/0/33 | 25/19 | 0/25 | 224/253/224 | 0.521149 | 0.100000 |
| device03 | B_combined_diagnostic | paired_bursts | test/4×1 | 14/14 | 14/0/0 | 100/100 | 0/100 | 14/14/14 | 0.460095 | 0.100000 |
| device03 | B_combined_diagnostic | alternating | validation/4×13 | 253/224 | 46/0/207 | 25/19 | 0/25 | 222/253/224 | 0.832361 | 0.100000 |
| device03 | B_combined_diagnostic | alternating | validation/50×1 | 253/224 | 41/0/212 | 25/19 | 0/25 | 222/253/224 | 0.832361 | 0.100000 |
| device03 | B_combined_diagnostic | alternating | test/4×1 | 14/14 | 3/0/11 | 100/100 | 0/100 | 14/14/14 | 0.832361 | 0.100000 |
| device06 | B_baseline | paired_bursts | validation/4×13 | 253/231 | 188/0/65 | 25/25 | 0/25 | 230/253/231 | 0.380059 | 0.030229 |
| device06 | B_baseline | paired_bursts | validation/50×1 | 253/231 | 170/0/83 | 25/25 | 0/25 | 219/253/231 | 0.343876 | 0.100000 |
| device06 | B_baseline | paired_bursts | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |
| device06 | B_baseline | alternating | validation/4×13 | 253/231 | 188/0/65 | 25/25 | 0/25 | 230/253/231 | 0.380059 | 0.030229 |
| device06 | B_baseline | alternating | validation/50×1 | 253/231 | 170/0/83 | 25/25 | 0/25 | 219/253/231 | 0.343876 | 0.100000 |
| device06 | B_baseline | alternating | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |
| device06 | B_enrollment_diagnostic | paired_bursts | validation/4×13 | 253/231 | 188/0/65 | 25/25 | 0/25 | 230/253/231 | 0.380059 | 0.030229 |
| device06 | B_enrollment_diagnostic | paired_bursts | validation/50×1 | 253/231 | 170/0/83 | 25/25 | 0/25 | 219/253/231 | 0.343876 | 0.100000 |
| device06 | B_enrollment_diagnostic | paired_bursts | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |
| device06 | B_enrollment_diagnostic | alternating | validation/4×13 | 253/231 | 188/0/65 | 25/25 | 0/25 | 230/253/231 | 0.380059 | 0.030229 |
| device06 | B_enrollment_diagnostic | alternating | validation/50×1 | 253/231 | 170/0/83 | 25/25 | 0/25 | 219/253/231 | 0.343876 | 0.100000 |
| device06 | B_enrollment_diagnostic | alternating | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |
| device06 | B_accumulation | paired_bursts | validation/4×13 | 253/231 | 199/0/54 | 25/25 | 0/25 | 231/253/231 | 0.380059 | 0.030229 |
| device06 | B_accumulation | paired_bursts | validation/50×1 | 253/231 | 178/0/75 | 25/25 | 0/25 | 221/253/231 | 0.363959 | 0.100000 |
| device06 | B_accumulation | paired_bursts | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |
| device06 | B_accumulation | alternating | validation/4×13 | 253/231 | 188/0/65 | 25/25 | 0/25 | 230/253/231 | 0.380059 | 0.030229 |
| device06 | B_accumulation | alternating | validation/50×1 | 253/231 | 170/0/83 | 25/25 | 0/25 | 219/253/231 | 0.343876 | 0.100000 |
| device06 | B_accumulation | alternating | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |
| device06 | B_combined_diagnostic | paired_bursts | validation/4×13 | 253/231 | 199/0/54 | 25/25 | 0/25 | 231/253/231 | 0.380059 | 0.030229 |
| device06 | B_combined_diagnostic | paired_bursts | validation/50×1 | 253/231 | 178/0/75 | 25/25 | 0/25 | 221/253/231 | 0.363959 | 0.100000 |
| device06 | B_combined_diagnostic | paired_bursts | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |
| device06 | B_combined_diagnostic | alternating | validation/4×13 | 253/231 | 188/0/65 | 25/25 | 0/25 | 230/253/231 | 0.380059 | 0.030229 |
| device06 | B_combined_diagnostic | alternating | validation/50×1 | 253/231 | 170/0/83 | 25/25 | 0/25 | 219/253/231 | 0.343876 | 0.100000 |
| device06 | B_combined_diagnostic | alternating | test/4×1 | 14/13 | 2/0/12 | 100/69 | 0/100 | 13/14/13 | 0.380059 | 0.030229 |

### 人物宏平均

所有小数为比例；人物列为known总人数/有合格known人数/unknown人数，按人物等权而非按录音条数。

| 条件 | arm | 顺序 | 候选池 | 人数总/合格/未知 | 正确接纳all | 正确接纳qualified | Top1all | Top1qualified | 至少一次误接纳的未知人数 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| device03 | B_baseline | paired_bursts | validation/4×13 | 50/49/5 | 0.893333 | 1.000000 | 0.893333 | 1.000000 | 0 |
| device03 | B_baseline | paired_bursts | validation/50×1 | 50/49/5 | 0.881667 | 0.988095 | 0.893333 | 1.000000 | 0 |
| device03 | B_baseline | paired_bursts | test/4×1 | 4/4/20 | 0.968750 | 0.968750 | 1.000000 | 1.000000 | 0 |
| device03 | B_baseline | alternating | validation/4×13 | 50/49/5 | 0.893333 | 1.000000 | 0.893333 | 1.000000 | 0 |
| device03 | B_baseline | alternating | validation/50×1 | 50/49/5 | 0.881667 | 0.988095 | 0.893333 | 1.000000 | 0 |
| device03 | B_baseline | alternating | test/4×1 | 4/4/20 | 0.968750 | 0.968750 | 1.000000 | 1.000000 | 0 |
| device03 | B_enrollment_diagnostic | paired_bursts | validation/4×13 | 50/49/5 | 0.893333 | 1.000000 | 0.893333 | 1.000000 | 0 |
| device03 | B_enrollment_diagnostic | paired_bursts | validation/50×1 | 50/49/5 | 0.881667 | 0.988095 | 0.893333 | 1.000000 | 0 |
| device03 | B_enrollment_diagnostic | paired_bursts | test/4×1 | 4/4/20 | 0.968750 | 0.968750 | 1.000000 | 1.000000 | 0 |
| device03 | B_enrollment_diagnostic | alternating | validation/4×13 | 50/49/5 | 0.893333 | 1.000000 | 0.893333 | 1.000000 | 0 |
| device03 | B_enrollment_diagnostic | alternating | validation/50×1 | 50/49/5 | 0.881667 | 0.988095 | 0.893333 | 1.000000 | 0 |
| device03 | B_enrollment_diagnostic | alternating | test/4×1 | 4/4/20 | 0.968750 | 0.968750 | 1.000000 | 1.000000 | 0 |
| device03 | B_accumulation | paired_bursts | validation/4×13 | 50/49/5 | 0.893333 | 1.000000 | 0.893333 | 1.000000 | 0 |
| device03 | B_accumulation | paired_bursts | validation/50×1 | 50/49/5 | 0.868810 | 0.957483 | 0.893333 | 1.000000 | 0 |
| device03 | B_accumulation | paired_bursts | test/4×1 | 4/4/20 | 1.000000 | 1.000000 | 1.000000 | 1.000000 | 0 |
| device03 | B_accumulation | alternating | validation/4×13 | 50/49/5 | 0.175000 | 0.182070 | 0.886667 | 0.993197 | 0 |
| device03 | B_accumulation | alternating | validation/50×1 | 50/49/5 | 0.156905 | 0.161273 | 0.886667 | 0.993197 | 0 |
| device03 | B_accumulation | alternating | test/4×1 | 4/4/20 | 0.375000 | 0.375000 | 1.000000 | 1.000000 | 0 |
| device03 | B_combined_diagnostic | paired_bursts | validation/4×13 | 50/49/5 | 0.893333 | 1.000000 | 0.893333 | 1.000000 | 0 |
| device03 | B_combined_diagnostic | paired_bursts | validation/50×1 | 50/49/5 | 0.868810 | 0.957483 | 0.893333 | 1.000000 | 0 |
| device03 | B_combined_diagnostic | paired_bursts | test/4×1 | 4/4/20 | 1.000000 | 1.000000 | 1.000000 | 1.000000 | 0 |
| device03 | B_combined_diagnostic | alternating | validation/4×13 | 50/49/5 | 0.175000 | 0.182070 | 0.886667 | 0.993197 | 0 |
| device03 | B_combined_diagnostic | alternating | validation/50×1 | 50/49/5 | 0.156905 | 0.161273 | 0.886667 | 0.993197 | 0 |
| device03 | B_combined_diagnostic | alternating | test/4×1 | 4/4/20 | 0.375000 | 0.375000 | 1.000000 | 1.000000 | 0 |
| device06 | B_baseline | paired_bursts | validation/4×13 | 50/46/5 | 0.734905 | 0.803339 | 0.911000 | 0.995652 | 0 |
| device06 | B_baseline | paired_bursts | validation/50×1 | 50/46/5 | 0.671690 | 0.731004 | 0.869143 | 0.949249 | 0 |
| device06 | B_baseline | paired_bursts | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |
| device06 | B_baseline | alternating | validation/4×13 | 50/46/5 | 0.734905 | 0.803339 | 0.911000 | 0.995652 | 0 |
| device06 | B_baseline | alternating | validation/50×1 | 50/46/5 | 0.671690 | 0.731004 | 0.869143 | 0.949249 | 0 |
| device06 | B_baseline | alternating | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |
| device06 | B_enrollment_diagnostic | paired_bursts | validation/4×13 | 50/46/5 | 0.734905 | 0.803339 | 0.911000 | 0.995652 | 0 |
| device06 | B_enrollment_diagnostic | paired_bursts | validation/50×1 | 50/46/5 | 0.671690 | 0.731004 | 0.869143 | 0.949249 | 0 |
| device06 | B_enrollment_diagnostic | paired_bursts | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |
| device06 | B_enrollment_diagnostic | alternating | validation/4×13 | 50/46/5 | 0.734905 | 0.803339 | 0.911000 | 0.995652 | 0 |
| device06 | B_enrollment_diagnostic | alternating | validation/50×1 | 50/46/5 | 0.671690 | 0.731004 | 0.869143 | 0.949249 | 0 |
| device06 | B_enrollment_diagnostic | alternating | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |
| device06 | B_accumulation | paired_bursts | validation/4×13 | 50/46/5 | 0.776333 | 0.848370 | 0.915000 | 1.000000 | 0 |
| device06 | B_accumulation | paired_bursts | validation/50×1 | 50/46/5 | 0.699952 | 0.761724 | 0.877143 | 0.957945 | 0 |
| device06 | B_accumulation | paired_bursts | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |
| device06 | B_accumulation | alternating | validation/4×13 | 50/46/5 | 0.734905 | 0.803339 | 0.911000 | 0.995652 | 0 |
| device06 | B_accumulation | alternating | validation/50×1 | 50/46/5 | 0.671690 | 0.731004 | 0.869143 | 0.949249 | 0 |
| device06 | B_accumulation | alternating | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |
| device06 | B_combined_diagnostic | paired_bursts | validation/4×13 | 50/46/5 | 0.776333 | 0.848370 | 0.915000 | 1.000000 | 0 |
| device06 | B_combined_diagnostic | paired_bursts | validation/50×1 | 50/46/5 | 0.699952 | 0.761724 | 0.877143 | 0.957945 | 0 |
| device06 | B_combined_diagnostic | paired_bursts | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |
| device06 | B_combined_diagnostic | alternating | validation/4×13 | 50/46/5 | 0.734905 | 0.803339 | 0.911000 | 0.995652 | 0 |
| device06 | B_combined_diagnostic | alternating | validation/50×1 | 50/46/5 | 0.671690 | 0.731004 | 0.869143 | 0.949249 | 0 |
| device06 | B_combined_diagnostic | alternating | test/4×1 | 4/4/20 | 0.250000 | 0.250000 | 0.875000 | 1.000000 | 0 |

### 积累、重置、事后混人及音频／处理量

查询全部/合格包括known与unknown；积累数指返回窗口含多段的次数。混人窗口仅在最终计分后以标签统计，不能反馈给门控或选阈值。输入合格秒是合格样本和；返回累计秒会重复计算重叠保留窗口，不是新增录音时长；最大返回秒为单窗上限。输入录音秒含被拒片段；虚拟跨度秒为各独立group的（录音样本＋4000固定间隙）跨度之和，不能当真人等待或端到端时延。CPU／processing为实测父进程算法工作量。

| 条件 | arm | 顺序 | 候选池 | query全/合格 | 积累 | 事后混人窗 | 重置/窗口原因 | 输入合格秒 | 返回累计秒 | 最大返回秒 | 输入录音秒 | 虚拟跨度合计秒 | 融合CPUms | replay处理ms |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| device03 | B_baseline | paired_bursts | validation/4×13 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 7.056000 |
| device03 | B_baseline | paired_bursts | validation/50×1 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 7.132000 |
| device03 | B_baseline | paired_bursts | test/4×1 | 114/114 | 0 | 0 | {"single_segment":114} | 341.126750 | 341.126750 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.441000 |
| device03 | B_baseline | alternating | validation/4×13 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 7.216000 |
| device03 | B_baseline | alternating | validation/50×1 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 7.196000 |
| device03 | B_baseline | alternating | test/4×1 | 114/114 | 0 | 0 | {"single_segment":114} | 341.126750 | 341.126750 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.426000 |
| device03 | B_enrollment_diagnostic | paired_bursts | validation/4×13 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 7.815000 |
| device03 | B_enrollment_diagnostic | paired_bursts | validation/50×1 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 7.223000 |
| device03 | B_enrollment_diagnostic | paired_bursts | test/4×1 | 114/114 | 0 | 0 | {"single_segment":114} | 341.126750 | 341.126750 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.415000 |
| device03 | B_enrollment_diagnostic | alternating | validation/4×13 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 7.217000 |
| device03 | B_enrollment_diagnostic | alternating | validation/50×1 | 278/243 | 0 | 0 | {"single_segment":278} | 714.927812 | 714.927812 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 7.162000 |
| device03 | B_enrollment_diagnostic | alternating | test/4×1 | 114/114 | 0 | 0 | {"single_segment":114} | 341.126750 | 341.126750 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.412000 |
| device03 | B_accumulation | paired_bursts | validation/4×13 | 278/243 | 101 | 0 | {"accumulated":101,"continuity_mismatch":44,"initial":23,"sample_limit":65,"stream_change":10,"unqualified":35} | 714.927812 | 1013.135188 | 7.890187 | 816.443125 | 882.693125 | 1.729000 | 8.896000 |
| device03 | B_accumulation | paired_bursts | validation/50×1 | 278/243 | 100 | 0 | {"accumulated":100,"continuity_mismatch":53,"initial":23,"sample_limit":67,"unqualified":35} | 714.927812 | 1008.735937 | 7.890187 | 816.443125 | 885.693125 | 2.017000 | 9.918000 |
| device03 | B_accumulation | paired_bursts | test/4×1 | 114/114 | 45 | 0 | {"accumulated":45,"continuity_mismatch":30,"initial":1,"sample_limit":38} | 341.126750 | 477.776250 | 7.935562 | 341.126750 | 369.376750 | 0.776000 | 1.157000 |
| device03 | B_accumulation | alternating | validation/4×13 | 278/243 | 11 | 2 | {"accumulated":11,"continuity_mismatch":182,"initial":35,"sample_limit":5,"stream_change":10,"unqualified":35} | 714.927812 | 750.231000 | 7.732125 | 816.443125 | 882.693125 | 1.828000 | 9.017000 |
| device03 | B_accumulation | alternating | validation/50×1 | 278/243 | 2 | 2 | {"accumulated":2,"continuity_mismatch":205,"initial":35,"sample_limit":1,"unqualified":35} | 714.927812 | 720.711125 | 5.291000 | 816.443125 | 885.693125 | 1.806000 | 8.866000 |
| device03 | B_accumulation | alternating | test/4×1 | 114/114 | 1 | 0 | {"accumulated":1,"continuity_mismatch":111,"initial":1,"sample_limit":1} | 341.126750 | 345.096125 | 6.991125 | 341.126750 | 369.376750 | 0.751000 | 1.156000 |
| device03 | B_combined_diagnostic | paired_bursts | validation/4×13 | 278/243 | 101 | 0 | {"accumulated":101,"continuity_mismatch":44,"initial":23,"sample_limit":65,"stream_change":10,"unqualified":35} | 714.927812 | 1013.135188 | 7.890187 | 816.443125 | 882.693125 | 1.717000 | 8.855000 |
| device03 | B_combined_diagnostic | paired_bursts | validation/50×1 | 278/243 | 100 | 0 | {"accumulated":100,"continuity_mismatch":53,"initial":23,"sample_limit":67,"unqualified":35} | 714.927812 | 1008.735937 | 7.890187 | 816.443125 | 885.693125 | 2.437000 | 10.747000 |
| device03 | B_combined_diagnostic | paired_bursts | test/4×1 | 114/114 | 45 | 0 | {"accumulated":45,"continuity_mismatch":30,"initial":1,"sample_limit":38} | 341.126750 | 477.776250 | 7.935562 | 341.126750 | 369.376750 | 0.763000 | 1.149000 |
| device03 | B_combined_diagnostic | alternating | validation/4×13 | 278/243 | 11 | 2 | {"accumulated":11,"continuity_mismatch":182,"initial":35,"sample_limit":5,"stream_change":10,"unqualified":35} | 714.927812 | 750.231000 | 7.732125 | 816.443125 | 882.693125 | 1.600000 | 8.725000 |
| device03 | B_combined_diagnostic | alternating | validation/50×1 | 278/243 | 2 | 2 | {"accumulated":2,"continuity_mismatch":205,"initial":35,"sample_limit":1,"unqualified":35} | 714.927812 | 720.711125 | 5.291000 | 816.443125 | 885.693125 | 2.175000 | 9.593000 |
| device03 | B_combined_diagnostic | alternating | test/4×1 | 114/114 | 1 | 0 | {"accumulated":1,"continuity_mismatch":111,"initial":1,"sample_limit":1} | 341.126750 | 345.096125 | 6.991125 | 341.126750 | 369.376750 | 0.771000 | 1.164000 |
| device06 | B_baseline | paired_bursts | validation/4×13 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 7.454000 |
| device06 | B_baseline | paired_bursts | validation/50×1 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 7.566000 |
| device06 | B_baseline | paired_bursts | test/4×1 | 114/82 | 0 | 0 | {"single_segment":114} | 244.341062 | 244.341062 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.312000 |
| device06 | B_baseline | alternating | validation/4×13 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 7.526000 |
| device06 | B_baseline | alternating | validation/50×1 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 10.114000 |
| device06 | B_baseline | alternating | test/4×1 | 114/82 | 0 | 0 | {"single_segment":114} | 244.341062 | 244.341062 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.311000 |
| device06 | B_enrollment_diagnostic | paired_bursts | validation/4×13 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 7.603000 |
| device06 | B_enrollment_diagnostic | paired_bursts | validation/50×1 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 7.544000 |
| device06 | B_enrollment_diagnostic | paired_bursts | test/4×1 | 114/82 | 0 | 0 | {"single_segment":114} | 244.341062 | 244.341062 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.390000 |
| device06 | B_enrollment_diagnostic | alternating | validation/4×13 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 882.693125 | 0.000000 | 8.383000 |
| device06 | B_enrollment_diagnostic | alternating | validation/50×1 | 278/256 | 0 | 0 | {"single_segment":278} | 751.630250 | 751.630250 | 3.998687 | 816.443125 | 885.693125 | 0.000000 | 7.613000 |
| device06 | B_enrollment_diagnostic | alternating | test/4×1 | 114/82 | 0 | 0 | {"single_segment":114} | 244.341062 | 244.341062 | 3.992812 | 341.126750 | 369.376750 | 0.000000 | 0.301000 |
| device06 | B_accumulation | paired_bursts | validation/4×13 | 278/256 | 79 | 0 | {"accumulated":79,"continuity_mismatch":97,"initial":11,"sample_limit":57,"stream_change":12,"unqualified":22} | 751.630250 | 986.373812 | 7.588875 | 816.443125 | 882.693125 | 2.202000 | 9.704000 |
| device06 | B_accumulation | paired_bursts | validation/50×1 | 278/256 | 77 | 0 | {"accumulated":77,"continuity_mismatch":110,"initial":12,"sample_limit":57,"unqualified":22} | 751.630250 | 979.691812 | 7.588875 | 816.443125 | 885.693125 | 2.508000 | 10.130000 |
| device06 | B_accumulation | paired_bursts | test/4×1 | 114/82 | 32 | 0 | {"accumulated":32,"continuity_mismatch":19,"initial":7,"sample_limit":24,"unqualified":32} | 244.341062 | 341.007437 | 7.935562 | 341.126750 | 369.376750 | 0.582000 | 0.884000 |
| device06 | B_accumulation | alternating | validation/4×13 | 278/256 | 8 | 0 | {"accumulated":8,"continuity_mismatch":212,"initial":20,"sample_limit":4,"stream_change":12,"unqualified":22} | 751.630250 | 778.155875 | 6.318062 | 816.443125 | 882.693125 | 1.753000 | 9.304000 |
| device06 | B_accumulation | alternating | validation/50×1 | 278/256 | 1 | 1 | {"accumulated":1,"continuity_mismatch":233,"initial":21,"sample_limit":1,"unqualified":22} | 751.630250 | 754.428562 | 6.754437 | 816.443125 | 885.693125 | 2.015000 | 10.326000 |
| device06 | B_accumulation | alternating | test/4×1 | 114/82 | 1 | 0 | {"accumulated":1,"continuity_mismatch":69,"initial":11,"sample_limit":1,"unqualified":32} | 244.341062 | 248.310438 | 6.991125 | 341.126750 | 369.376750 | 0.673000 | 0.983000 |
| device06 | B_combined_diagnostic | paired_bursts | validation/4×13 | 278/256 | 79 | 0 | {"accumulated":79,"continuity_mismatch":97,"initial":11,"sample_limit":57,"stream_change":12,"unqualified":22} | 751.630250 | 986.373812 | 7.588875 | 816.443125 | 882.693125 | 1.758000 | 9.273000 |
| device06 | B_combined_diagnostic | paired_bursts | validation/50×1 | 278/256 | 77 | 0 | {"accumulated":77,"continuity_mismatch":110,"initial":12,"sample_limit":57,"unqualified":22} | 751.630250 | 979.691812 | 7.588875 | 816.443125 | 885.693125 | 2.460000 | 10.031000 |
| device06 | B_combined_diagnostic | paired_bursts | test/4×1 | 114/82 | 32 | 0 | {"accumulated":32,"continuity_mismatch":19,"initial":7,"sample_limit":24,"unqualified":32} | 244.341062 | 341.007437 | 7.935562 | 341.126750 | 369.376750 | 0.577000 | 0.870000 |
| device06 | B_combined_diagnostic | alternating | validation/4×13 | 278/256 | 8 | 0 | {"accumulated":8,"continuity_mismatch":212,"initial":20,"sample_limit":4,"stream_change":12,"unqualified":22} | 751.630250 | 778.155875 | 6.318062 | 816.443125 | 882.693125 | 1.693000 | 9.271000 |
| device06 | B_combined_diagnostic | alternating | validation/50×1 | 278/256 | 1 | 1 | {"accumulated":1,"continuity_mismatch":233,"initial":21,"sample_limit":1,"unqualified":22} | 751.630250 | 754.428562 | 6.754437 | 816.443125 | 885.693125 | 1.956000 | 10.011000 |
| device06 | B_combined_diagnostic | alternating | test/4×1 | 114/82 | 1 | 0 | {"accumulated":1,"continuity_mismatch":69,"initial":11,"sample_limit":1,"unqualified":32} | 244.341062 | 248.310438 | 6.991125 | 341.126750 | 369.376750 | 1.944000 | 1.459000 |

### 原B两固定规则回归

以下为baseline；诊断臂逐项相同。每规则在两顺序均与旧R0完整语义相等；它们不是本轮拟合的新正式阈值。

| 条件 | 顺序 | 固定规则 | threshold | margin | known总/合格 | 正确/误归/拒识 | unknown总/合格 | 未知误接纳/拒识 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| device03 | paired_bursts | fixed_current_session | 0.682987 | 0.030229 | 253/224 | 190/0/63 | 25/19 | 0/25 |
| device03 | paired_bursts | fixed_legacy_base_far001 | 0.449994 | 0.030229 | 253/224 | 224/0/29 | 25/19 | 1/24 |
| device03 | alternating | fixed_current_session | 0.682987 | 0.030229 | 253/224 | 190/0/63 | 25/19 | 0/25 |
| device03 | alternating | fixed_legacy_base_far001 | 0.449994 | 0.030229 | 253/224 | 224/0/29 | 25/19 | 1/24 |
| device06 | paired_bursts | fixed_current_session | 0.682987 | 0.030229 | 253/231 | 1/0/252 | 25/25 | 0/25 |
| device06 | paired_bursts | fixed_legacy_base_far001 | 0.449994 | 0.030229 | 253/231 | 140/0/113 | 25/25 | 0/25 |
| device06 | alternating | fixed_current_session | 0.682987 | 0.030229 | 253/231 | 1/0/252 | 25/25 | 0/25 |
| device06 | alternating | fixed_legacy_base_far001 | 0.449994 | 0.030229 | 253/231 | 140/0/113 | 25/25 | 0/25 |

### 原生资源与耗时

原生推理与worker资源基于每条件一次提取，四臂复用内存向量。云端控制器进程墙钟时间见上表；以下CPU与RSS不代表设备功耗或真实交互延迟。

| 条件 | 合格数 | short | silence | 输入总秒 | 冷启动ms | workerCPUms | workerRSSKiB | parentRSSKiB | 纯analysisMS | 子userCPU秒 | 子systemCPU秒 | 子maxRSSKiB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| device03 | 935 | 0 | 36 | 2879.536938 | 438.563000 | 137854.747000 | 222756 | 55504 | 1657.261000 | 140.284428 | 1.165752 | 222756 |
| device06 | 914 | 0 | 57 | 2879.536938 | 440.701000 | 135047.815000 | 224276 | 55372 | 1805.091000 | 137.707672 | 1.006256 | 224276 |


device03 原生耗时摘要：`{"native_inference":{"first_ms":143.165,"warm_count":934,"warm_median_ms":145.464,"warm_p95_ms":194.775},"wall_ipc_inference":{"first_ms":143.58,"warm_count":934,"warm_median_ms":145.945,"warm_p95_ms":195.104}}`。

device06 原生耗时摘要：`{"native_inference":{"first_ms":139.779,"warm_count":913,"warm_median_ms":145.014,"warm_p95_ms":194.162},"wall_ipc_inference":{"first_ms":140.172,"warm_count":913,"warm_median_ms":145.342,"warm_p95_ms":194.775}}`。

### 汇总核对记录与修订

`semantic-regression-check.json` 首次406项中404通过，2项失败来自汇总脚本错误地要求新 `enrollment_summary` 也包含旧backend的30名cohort。旧94项中64名实际登记人物的逐项值完全相同；新研究不将cohort登记成身份模板，cohort仍在输入和完整 `role_quality_counts` 中。保留首次失败记录和初版脚本，`summary-audit-amendment.json` 明确这只是分析断言范围修订；按匿名人物键比较实际登记64项后，`semantic-regression-check-amended.json` 406/406通过。未修改运行源码、输入、阈值或原始成功输出，未重跑任何推理。

控制器另独立核对246项／0失败，保存为 `root-independent-result-check.json`；41份冻结源码再次与Mac当前文件逐项一致。两次公开运行均exit0／stderr0，未超时、未需额外进程组清理；不存在为改善数字而重跑的条件。


## 实际设备／手机参与者采集步骤（待执行）

本轮未操作真实机器人麦克风，没有新的独立保留录音，也没有开麦、播放声源、重启设备或刷固件。以下步骤交由参与者手工执行，不代表已经存在产品登记页面或自动采集器。

1. 先约定参与者授权、匿名编号、云端私有保存位置和删除期限；不要把姓名、原音频或凭据放入Git。准备安静房间，关闭电视和其他扬声器，确保一次仅一人发言。记录机器人、固件、麦克风位置、距离与方向及环境噪声；不要把未知距离写成近场。
2. **机器人麦克风登记条件：** 参与者面向实际机器人，在预定日常距离用正常音量分多句说话，总录音约20秒；建议5～8句，每句约2～4秒，内容和语气自然变化，句间手工停止／分段。登记全部由同一人完成，采集结束立即手工停止。这个条件必须使用机器人真实麦克风采集；手机录音不能替代它。
3. 将授权录音通过经审核的私有云端流程转为工具要求的16kHz、mono、s16le WAV，并逐条校验原始来源、转换记录、样本数和SHA。登记段遵守原长度／RMS资格；质量不合格或诊断提示可疑时，参与者核听并重录指定段。未报警不能证明可信，可疑也不能直接断定是陌生人；不要自动删段或悄悄选最优片段。重录形成独立修订，保留失败／前后证据。
4. **手机→机器人独立条件：** 如需检验电话／播放传输，让同一参与者另用手机录制多句约20秒登记素材，再由参与者手动播放到机器人实际麦克风并采集。固定并记录手机型号、播放音量、手机到机器人距离／方向、房间及机器人增益；单独标记此条件，绝不与直接真人→机器人登记混成一个结果。手机原始录音和机器人采到的重录要分别记录来源和摘要。手工结束播放及采集，不自动开启扬声器或麦克风。
5. 登记完成后另录**独立查询句子**，不复用登记原句、录音或切片；分别覆盖机器人直接采集及约定的手机→机器人条件。另招未参与登记／训练／调参的陌生人录独立查询，保存角色／来源分离证据；不将当前已看过人物写成新盲测。先冻结新保留集、排除／质量规则及阈值，再打开结果，避免按分数补录或挑样本。
6. 短句和多人交替应另安排真实连续场景；有人同时说话时手工标注并保留，不把现有构造回放当真实换人／重叠验证。参与者随时可以说停并手工停止采集、播放与上传；错误、低音量、陌生误接纳也照样记录。无授权不继续。新保留集效果和家庭现场声学验收通过前，不启用正式身份决策。

诊断只有在真实重录并另行受控对照之后，才有条件讨论其身份效果；本轮没有这些素材。商业模型是否更优仍未知。Eagle注册／申请与厂商联系已停止，Gmail草稿保持未发送。

## 正式环境与交接边界

本轮只修改隔离实验代码／测试及文档，不改正式服务、模型、环境、固件、VAD／AEC、现有身份门槛或部署。控制器已运行独立 `final-protection-check.py`：正式2项服务的PID／NRestarts／ActiveState等字段与运行前完全相同、5项SHA保持、`analysis_processes=[]`，证据为 `protection-after.json`；这项结论来自实际前后核对，不来自单元测试。任务／整体复核、计划关闭、推送核对和临时SSH关闭仍由控制器完成。真实设备登记、新独立保留集、真实连续换人／重叠和家庭／生产验收仍待执行。
