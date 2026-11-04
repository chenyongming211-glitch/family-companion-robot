# B方案评分后端：普通话独立小样本与旧50人开发对照

用户已批准[调研方案](../../research/2026-11-05-speaker-b-optimization.md)继续测试；数据核查后明确选择“保持普通话：新增小规模独立验证”。基线main为ba0ce90。本文件在本轮身份推理之前记录规则。官方目录240人；保守排除70位此前被检查的人物后，普通话有71位两设备配对人物，但约20秒登记＋2独立查询的字节代理容量仅5人，不能声称新的50人盲测。

## 目标与边界

比较R0：base多段约20秒＋候选池匹配标定；R1：相同输入＋AS-Norm；R2：R1＋低维质量感知校准。主要看固定验证误接纳预算下的正确接纳／误归／拒识，Top-1只作诊断。正式模型、会话阈值、产品登记库、VAD／AEC、服务、固件、麦克风不改。Go／原生／运行测试和素材处理仅在云端，Mac仅源码／Git。音频、向量、权重、凭据、原始JSON不进Git或Mac。

沿用Go1.27.3、sherpa-onnx Linux v2.0.1、base512维导出SHA256 1a331345f04805badbb495c775a6ddffcdd1a732567d5ec8b3d5749e3c7a5e4b。原CLI／JSON／session行为不变；新增独立backend-study入口。复用extractEvaluation、loadBoundAudit及fuseEnrollment，不复制worker提取循环，向量仅RAM并由唯一所有者清理。

## 来源、角色与盲测定义

重新流式核验官方test.tar.gz完整MD5 45972606dd10d3f7c1c31f27acdfbed7及1,199,587,204字节；不保留完整包、不下载190GB训练集。新输入仅Dialect00／Device03及06／全部距离代码、实际mono PCM16/16k/2～8秒。物理距离未核实，不能称纯设备影响或近／远场。

角色人物完全隔离：
- train：旧S的10位校准人物，沿用原登记／查询。训练数据小，只训练下述3参数校准器。
- validation：旧S的50位已知＋5位陌生人物，沿用原素材。它们已看过，只是开发验证，不能称新盲测。
- test：来源顺序前最多5位严格资格合格的保守未接触人物；另取前20位未接触、非已知人物，要求至少3条不同内容的配对查询，取前min(5,可得数)条。
- cohort：来源顺序前30位独立Device06-only普通话人物，每人前2条同时通过energy/VAD的不同源录音。缺转录，声明cohort内容变化未核实，不声称两条录音一定不同文字；它们只作背景。

新已知人物按归档顺序、同人物规范化文字去重；预留最后2条不同内容的配对查询。登记从其余Device03片段依序取同时通过energy（≥24000samples且RMS≥.005）及VAD（估计段≥16000samples）的素材，累计首次≥320000samples后停止、总长≤448000、至少2段。其余所有格式合格且配对的不同内容均作查询，低质量不删除。最多选前5位，不按分数更换。实际人数／清单／审计摘要及数据审查在首次身份评分前冻结；不足则记录实际规模，不借旧人充新人数。

登记与查询按同一人物＋规范化文字及源录音组隔离；不同人物读相同文字不当同一录音，不使用文字打分。source_group_sha256为源人物／录音键组摘要，阻止同句设备版本跨登记／查询。配对版本不当两条独立内容。新人物排除旧70完整来源资格集合，不仅65位最终使用者。

## 评分与拟合

每设备条件只提取一次，R0/R1/R2复用RAM向量。每段登记L2归一化、等权平均、再L2归一化。查询沿用energy门禁、失败计拒识；VAD仅作质量特征，不加硬门禁／裁剪／增益／降噪。背景每人2条同样融合为1模板。

R0为cosine。R1固定cohort30、top-k10，分别取登记和查询对cohort最高10项分数的均值和总体标准差：
    asnorm = 0.5 * ((cosine-enrollMean)/max(enrollStd,1e-6)
                 + (cosine-queryMean)/max(queryStd,1e-6))
严格检查有限性／维度／零向量，报告标准差被下限替换次数。背景不足30则研究资格失败，不改k。

R2是借鉴QMF的低维变体，不是完整WeSpeaker复现。每设备条件各用train查询×train10模板形成同／异人对，特征只有AS-Norm分数及log1p(querySpeechSamples/16000)，加截距共3参数。仅用训练均值／总体标准差标准化两特征，标准差下限1e-6。同／异人各占损失一半，非截距L2正则λ=.01，稳定logistic softplus。确定性全批梯度下降最多2000步、目标改善收敛阈值1e-8、初始步长1、有界回溯至多50次确保目标不增；未收敛／非有限／分数权重≤0显式failed、保留not_run分母。报告训练人物／样本／损失／迭代／参数，不调用测试数据选参数。输出不是身份概率。正分数权重下同一查询R2排名须等于R1，只改变质量相关接纳工作点。

分别在validation标定实际test候选数N（1～5）及50。validation50按连续索引分N人组，末组不足时从开头循环补足N候选，每条已知查询仅在自己的组计一次；陌生人物依编号固定一个对应组，不遍历所有组。N=5为原10×5，50为全池。最终test仅在其实际N人池计分；validation50仅开发诊断。

每方法／池固定报告验证陌生误接纳目标1%及5%，budget=floor(unknownTotal*target)，低质量留全分母。R0 margin候选0,.01,.030229,.05,.1；R1/R2候选0,.1,.25,.5,1。threshold候选为所有有限验证best分数及Nextafter向上边界、两端全接纳／全拒识边界；最高分同分一律拒识。仅在陌生误接纳不超预算且验证已知误归为0的工作点中，依次最大化正确接纳、最小化陌生误接纳、选较高threshold、较高margin。全拒识是有效保守工作点。N=1不用不存在的second，margin视为0。旧.682987／.4499944095418535及margin.030229仅R0 validation50兼容诊断，不套给新分数。

验证陌生5人25条，1%预算0、5%预算1，不保证总体1%／5%。测试违反目标须照实报告，不调阈值或重跑择优。DET／ROC、pair EER／minDCF仅诊断，测试最佳工作点不当已部署性能。全局单调变换本身不会改善排序／ROC。

## 命令、验证与输出

backend-study --model ABS --sha256 SHA --manifest ABS。清单≤4MiB／4096条／240人物，最多4个审计，每审计沿用旧≤2MiB／2048条。header必填source_kind=public_human、source_id、source_url／license_url为HTTPS、single_speaker_basis=source_annotations、sample_rate=16000、condition=device03或device06、audits数组(path/sha256)、cases。审计固定Silero配置、SHA、WAV SHA及samples绑定；重复ID／SHA／路径、缺失／多余字段／null、无效角色／编号、跨角色源组、symlink或坏WAV拒绝。

case必填id、speaker、role、path、samples、sha256、recording_sha256、source_group_sha256、text_sha256。人物前缀／角色：cohort_N(cohort)、train_N(train_enrollment/train_query)、validation_N(validation_enrollment/validation_query)、validation_unknown_N(validation_unknown)、test_N(test_enrollment/test_query)、test_unknown_N(test_unknown)。各前缀从1连续；text_sha256仅cohort可空。train10、validation50/5、cohort30、test已知1～5／陌生20。cohort每人2条、登记首次跨20秒且20～28秒、各登记人物至少2查询。必需资格不足显式失败，不自动降格或扩大旧入口限制。

新增测试已知人物查询数为2／2／2／8时，不能把14句当14人；输出按匿名人物的质量／正确／误归／拒识／Top-1总量及等人物权重宏平均。陌生人物单列每人误接纳／总数和至少一次被误接纳的人数。分组采用人物编号数字顺序，不用字符串字典序。

匿名输出模型／清单／审计SHA、每角色总数／质量分母、R0/R1/R2及池／政策正确／误归／拒识、陌生误接纳／总数／组计数、合格及全分母Top-1、模板配对。not_run保留真实分母而决策0。不得输出路径／正文／源人物／向量。human_acceptance、production_acceptance、feature_enabled均false，open_set_assessment=limited_public_unknown_cohort；连续／重叠／重放／家庭现场仍unavailable。

报告原生／IPC温态P50/P95、冷启动、输入数／音频时长、workerCPU/RSS、parentRSS；背景统计／拟合／标定／评分耗时分列。native延时覆盖所有合格角色，与旧仅evaluation计时不可直接作纯速度比。前后5项保护摘要／2PID/NRestarts/active核对；外层进程组超时TERM/KILL回收。两设备各执行一次，旧50 R0兼容诊断复用本轮共享提取，不另重复模型。最终代码／数据／整体独立审查、main推送、关闭临时SSH，不部署产品。

## 身份评分前的实际输入冻结

波形／信号资格审核后，最终test为4位新已知、20位新陌生，背景30人；旧10人训练／旧50已知5陌生开发验证。首次40位背景候选只有26位合格，已在无身份分数时将读取候选上限扩至全部70位，仍取原顺序前30位、同一质量资格；不是按结果更换人物。三批新审计覆盖1,831份唯一WAV，官方完整归档两次流式MD5及字节数一致。

两条件各971条、119人物；其中登记／背景共享521条，450条查询各有Device03／06版本，合计1,421份不同设备录音。test新登记25段，平均20.5813秒原录音／15.936秒VAD估计段；已知14查询／条件（2/2/2/8），陌生100查询／条件。energy通过：Device03已知14／14、陌生100／100，Device06已知13／14、陌生69／100；失败仍留全分母。旧开发集与新集的采集条件也不同，不能跨集直接计算优化增益。

冻结清单SHA256：Device03 a4cee902c9835910c2619a043725683fa56cd75173d5b5f86461d8469f7fc04d；Device06 29c054a30db23d0cf3ec465d270d2cb63118514f20c242e52c089f553d6c3745。匿名冻结摘要SHA256 8e56de66b5470eb6e0cfdb8abfbd520f0703390896cfc2e3c5697d703540fee5。输入独立审查及代码审查通过后才执行身份评分；本节尚无身份结果。


## 执行后记录（不更改预注册规则）

两条件三组评分已完成，详见[最终报告](../../verification/2026-11-05-speaker-backend.md)。新增独立测试的冻结工作点显示R1/R2虽减少拒识，但陌生误接纳明显退化，没有达到本轮目标；保持B，不采用当前AS-Norm／QMF配置，不部署新研究阈值。旧50人仅开发验证、新4人已知样本小、背景／校准／采集条件覆盖有限等限制保留；后续本轮测试已见数据不再作盲测。算法／选择／门禁／阈值／margin均未依据测试成绩调整。
