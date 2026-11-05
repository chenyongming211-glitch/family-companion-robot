# 现有声纹 SDK 固定对照（2026-11-05）

本轮只评估官方 SDK／前端接入现有 B 登记和标定政策后的结果。所有人物均是已见开发回归；没有新盲测、机器人麦克风新录音或家庭／生产验收。没有候选形成可替换基线的优势：同设备旧test的SB／WeSpeaker均14/14正确，但分别误接纳20/100及2/100陌生，base为13/14且0/100；跨设备仍明显拒识。Vosk两条件失败而无完整识别率。保留现有开源base B作对照，不部署替换；base跨设备2/14也未达到实用验收。

## 实测与分母

保留原两个条件各971条、全部角色和约20秒多句登记：64登记人物／461登记片段；50位开发已知253查询及5位陌生25查询；旧test为4已知14查询（每人2／2／2／8）及20陌生100查询，另10训练人物、30cohort人物。Device03是同设备条件，Device06是跨设备查询条件；两者登记字节一致。人物已经看过，不称独立新测试。全部原1942 case引用和911非cohort配对键已由根任务核对。

公开输入仍需≥2秒、RMS≥.005；登记每段原VAD语音≥1秒，原20秒前缀／合计20–28秒合同保持。查询VAD仅诊断。SDK成功但无有效声纹属于sdk_no_vector；查询仍计拒识，任何必需登记片段无向量使该条件not_run。超时／协议／推理失败不是合法无声纹或零准确率，不发布部分身份结果。

主规则unknown_target_0.01只在开发25条陌生上允许floor(25×.01)=0次误接纳，保留固定margin网格[0,.01,.030229,.05,.1]；并非总体FAR≤1%的统计保证。V4按匿名编号构造13个候选组（每组最多4人，末组2人，并非13个真实家庭），V50为完整50人池、T4为旧4人池；V4与V50复用相同查询，不可相加。每行已知总数包含所有质量拒识；正确／误归／拒识互斥且完整，陌生误接纳／拒识同理。

| 运行／池 | 状态 | 已知总／合格 | 正确／误归／拒识 | 全分母正确接纳 | 陌生总／合格 | 陌生误接纳／拒识 |
| --- | --- | --- | --- | --- | --- | --- |
| base03/V4 | evaluated | 253／224 | 224／0／29 | 88.54% | 25／19 | 0／25 |
| base03/V50 | evaluated | 253／224 | 222／0／31 | 87.75% | 25／19 | 0／25 |
| base03/T4 | evaluated | 14／14 | 13／0／1 | 92.86% | 100／100 | 0／100 |
| base06/V4 | evaluated | 253／231 | 188／0／65 | 74.31% | 25／25 | 0／25 |
| base06/V50 | evaluated | 253／231 | 170／0／83 | 67.19% | 25／25 | 0／25 |
| base06/T4 | evaluated | 14／13 | 2／0／12 | 14.29% | 100／69 | 0／100 |
| speechbrain_ecapa03/V4 | evaluated | 253／224 | 221／0／32 | 87.35% | 25／19 | 0／25 |
| speechbrain_ecapa03/V50 | evaluated | 253／224 | 206／0／47 | 81.42% | 25／19 | 0／25 |
| speechbrain_ecapa03/T4 | evaluated | 14／14 | 14／0／0 | 100.00% | 100／100 | 20／80 |
| speechbrain_ecapa06/V4 | evaluated | 253／231 | 58／0／195 | 22.92% | 25／25 | 0／25 |
| speechbrain_ecapa06/V50 | evaluated | 253／231 | 42／0／211 | 16.60% | 25／25 | 0／25 |
| speechbrain_ecapa06/T4 | evaluated | 14／13 | 2／0／12 | 14.29% | 100／69 | 0／100 |
| wespeaker_cn34_ref03/V4 | evaluated | 253／224 | 223／0／30 | 88.14% | 25／19 | 0／25 |
| wespeaker_cn34_ref03/V50 | evaluated | 253／224 | 220／0／33 | 86.96% | 25／19 | 0／25 |
| wespeaker_cn34_ref03/T4 | evaluated | 14／14 | 14／0／0 | 100.00% | 100／100 | 2／98 |
| wespeaker_cn34_ref06/V4 | evaluated | 253／231 | 3／0／250 | 1.19% | 25／25 | 0／25 |
| wespeaker_cn34_ref06/V50 | evaluated | 253／231 | 3／0／250 | 1.19% | 25／25 | 0／25 |
| wespeaker_cn34_ref06/T4 | evaluated | 14／13 | 0／0／14 | 0.00% | 100／69 | 0／100 |
| vosk03/V4 | not_run | N/A（计划253） | N/A | N/A | N/A（计划25） | N/A |
| vosk03/V50 | not_run | N/A（计划253） | N/A | N/A | N/A（计划25） | N/A |
| vosk03/T4 | not_run | N/A（计划14） | N/A | N/A | N/A（计划100） | N/A |
| vosk06/V4 | not_run | N/A（计划253） | N/A | N/A | N/A（计划25） | N/A |
| vosk06/V50 | not_run | N/A（计划253） | N/A | N/A | N/A（计划25） | N/A |
| vosk06/T4 | not_run | N/A（计划14） | N/A | N/A | N/A（计划100） | N/A |

原输入各角色质量分母如下（由冻结旧base及原审计读取）；所有完成评测的SDK角色质量计数逐项与其相同。VAD对查询只作诊断，不能用下表VAD筛选后替代主表全分母。not_run不由本表推定其实际SDK覆盖。

| 条件 | 角色 | 总数 | energy合格 | VAD合格 | 两者均合格 |
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

## 人物／家庭宏平均与排名诊断

人物宏平均先对各已知人物计算比率再等权平均；合格宏平均只包括至少1条合格查询的人。家庭宏平均先对有已知查询的家庭计算全分母正确接纳率再等权平均；下文家庭宏平均指这些构造候选组的等权平均，不是实际家庭采集结果。Top1是合格音频的排名诊断，未通过质量的查询在全分母Top1中仍保留；不能替代接纳率。EER／minDCF由模板配对ROC计算，配对共享人物／片段且不独立，不是实际1:N陌生误接纳或官方测试集成绩。

| 运行／池 | 已知人数／有合格查询人数 | 人物正确接纳全／合格 | 家庭正确接纳宏平均 | Top1正确／全部 | 人物Top1全／合格 | 配对EER | minDCF(.01) | 至少1次误接纳的陌生人数 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| base03/V4 | 50／49 | 89.33%／100.00% | 89.30% | 224/253（合格224） | 89.33%／100.00% | 0.89% | 0.027 | 0/5 |
| base03/V50 | 50／49 | 88.17%／98.81% | 87.75% | 224/253（合格224） | 89.33%／100.00% | 0.19% | 0.031 | 0/5 |
| base03/T4 | 4／4 | 96.88%／96.88% | 92.86% | 14/14（合格14） | 100.00%／100.00% | 0.45% | 0.071 | 0/20 |
| base06/V4 | 50／46 | 73.49%／80.33% | 74.12% | 230/253（合格231） | 91.10%／99.57% | 4.79% | 0.527 | 0/5 |
| base06/V50 | 50／46 | 67.17%／73.10% | 67.19% | 219/253（合格231） | 86.91%／94.92% | 3.09% | 0.366 | 0/5 |
| base06/T4 | 4／4 | 25.00%／25.00% | 14.29% | 13/14（合格13） | 87.50%／100.00% | 18.41% | 0.538 | 0/20 |
| speechbrain_ecapa03/V4 | 50／49 | 88.36%／99.01% | 88.36% | 224/253（合格224） | 89.33%／100.00% | 4.02% | 0.214 | 0/5 |
| speechbrain_ecapa03/V50 | 50／49 | 82.20%／90.98% | 81.42% | 222/253（合格224） | 88.76%／99.42% | 4.02% | 0.264 | 0/5 |
| speechbrain_ecapa03/T4 | 4／4 | 100.00%／100.00% | 100.00% | 14/14（合格14） | 100.00%／100.00% | 0.23% | 0.143 | 8/20 |
| speechbrain_ecapa06/V4 | 50／46 | 22.90%／25.07% | 23.57% | 213/253（合格231） | 85.29%／93.07% | 11.26% | 0.944 | 0/5 |
| speechbrain_ecapa06/V50 | 50／46 | 15.21%／16.72% | 16.60% | 177/253（合格231） | 71.21%／77.76% | 10.39% | 0.843 | 0/5 |
| speechbrain_ecapa06/T4 | 4／4 | 25.00%／25.00% | 14.29% | 12/14（合格13） | 84.38%／96.88% | 30.77% | 0.769 | 0/20 |
| wespeaker_cn34_ref03/V4 | 50／49 | 88.93%／99.59% | 88.95% | 224/253（合格224） | 89.33%／100.00% | 1.74% | 0.223 | 0/5 |
| wespeaker_cn34_ref03/V50 | 50／49 | 87.10%／97.72% | 86.96% | 223/253（合格224） | 88.67%／99.32% | 1.34% | 0.110 | 0/5 |
| wespeaker_cn34_ref03/T4 | 4／4 | 100.00%／100.00% | 100.00% | 14/14（合格14） | 100.00%／100.00% | 0.00% | 0.000 | 1/20 |
| wespeaker_cn34_ref06/V4 | 50／46 | 0.86%／0.93% | 1.36% | 204/253（合格231） | 81.33%／88.94% | 14.72% | 0.987 | 0/5 |
| wespeaker_cn34_ref06/V50 | 50／46 | 0.86%／0.93% | 1.19% | 122/253（合格231） | 49.41%／54.07% | 14.29% | 0.967 | 0/5 |
| wespeaker_cn34_ref06/T4 | 4／4 | 0.00%／0.00% | 0.00% | 12/14（合格13） | 84.38%／96.88% | 24.44% | 0.846 | 0/20 |
| vosk03/V4 | N/A | N/A | N/A | N/A | N/A | N/A | N/A | N/A |
| vosk03/V50 | N/A | N/A | N/A | N/A | N/A | N/A | N/A | N/A |
| vosk03/T4 | N/A | N/A | N/A | N/A | N/A | N/A | N/A | N/A |
| vosk06/V4 | N/A | N/A | N/A | N/A | N/A | N/A | N/A | N/A |
| vosk06/V50 | N/A | N/A | N/A | N/A | N/A | N/A | N/A | N/A |
| vosk06/T4 | N/A | N/A | N/A | N/A | N/A | N/A | N/A | N/A |

固定工作点如下；每个候选池独立使用原标定助手，没有根据test调阈值。

| 运行／池 | 阈值 | margin | ROC正／负配对 | ROC点数 |
| --- | --- | --- | --- | --- |
| base03/V4 | 0.460094974 | 0.100000000 | 224/748 | 973 |
| base03/V50 | 0.517547533 | 0.100000000 | 224/11926 | 12151 |
| base03/T4 | 0.460094974 | 0.100000000 | 14/442 | 457 |
| base06/V4 | 0.380059194 | 0.030229000 | 231/793 | 1025 |
| base06/V50 | 0.343875626 | 0.100000000 | 231/12569 | 12801 |
| base06/T4 | 0.380059194 | 0.030229000 | 13/315 | 329 |
| speechbrain_ecapa03/V4 | 0.354190630 | 0.100000000 | 224/748 | 973 |
| speechbrain_ecapa03/V50 | 0.354190630 | 0.100000000 | 224/11926 | 12151 |
| speechbrain_ecapa03/T4 | 0.354190630 | 0.100000000 | 14/442 | 457 |
| speechbrain_ecapa06/V4 | 0.553501215 | 0.100000000 | 231/793 | 1025 |
| speechbrain_ecapa06/V50 | 0.564547423 | 0.100000000 | 231/12569 | 12801 |
| speechbrain_ecapa06/T4 | 0.553501215 | 0.100000000 | 13/315 | 329 |
| wespeaker_cn34_ref03/V4 | 0.464816213 | 0.050000000 | 224/748 | 973 |
| wespeaker_cn34_ref03/V50 | 0.559642529 | 0.030229000 | 224/11926 | 12151 |
| wespeaker_cn34_ref03/T4 | 0.464816213 | 0.050000000 | 14/442 | 457 |
| wespeaker_cn34_ref06/V4 | 0.694828027 | 0.100000000 | 231/793 | 1025 |
| wespeaker_cn34_ref06/V50 | 0.694828027 | 0.100000000 | 231/12569 | 12801 |
| wespeaker_cn34_ref06/T4 | 0.694828027 | 0.100000000 | 13/315 | 329 |
| vosk03/V4 | N/A | N/A | N/A | N/A |
| vosk03/V50 | N/A | N/A | N/A | N/A |
| vosk03/T4 | N/A | N/A | N/A | N/A |
| vosk06/V4 | N/A | N/A | N/A | N/A |
| vosk06/V50 | N/A | N/A | N/A | N/A |
| vosk06/T4 | N/A | N/A | N/A | N/A |

旧test逐人物的完整分母如下，避免8句人物主导总体结果；全部V4/V50/T4逐人物和逐家庭计数、宏平均重算留在云端task3-aggregate-audit.json。

| 运行 | 匿名人物 | 总／合格 | 正确／误归／拒识 | Top1正确／全部 |
| --- | --- | --- | --- | --- |
| base03 | test_1 | 2／2 | 2／0／0 | 2/2 |
| base03 | test_2 | 2／2 | 2／0／0 | 2/2 |
| base03 | test_3 | 2／2 | 2／0／0 | 2/2 |
| base03 | test_4 | 8／8 | 7／0／1 | 8/8 |
| base06 | test_1 | 2／2 | 2／0／0 | 2/2 |
| base06 | test_2 | 2／2 | 0／0／2 | 2/2 |
| base06 | test_3 | 2／1 | 0／0／2 | 1/2 |
| base06 | test_4 | 8／8 | 0／0／8 | 8/8 |
| speechbrain_ecapa03 | test_1 | 2／2 | 2／0／0 | 2/2 |
| speechbrain_ecapa03 | test_2 | 2／2 | 2／0／0 | 2/2 |
| speechbrain_ecapa03 | test_3 | 2／2 | 2／0／0 | 2/2 |
| speechbrain_ecapa03 | test_4 | 8／8 | 8／0／0 | 8/8 |
| speechbrain_ecapa06 | test_1 | 2／2 | 2／0／0 | 2/2 |
| speechbrain_ecapa06 | test_2 | 2／2 | 0／0／2 | 2/2 |
| speechbrain_ecapa06 | test_3 | 2／1 | 0／0／2 | 1/2 |
| speechbrain_ecapa06 | test_4 | 8／8 | 0／0／8 | 7/8 |
| wespeaker_cn34_ref03 | test_1 | 2／2 | 2／0／0 | 2/2 |
| wespeaker_cn34_ref03 | test_2 | 2／2 | 2／0／0 | 2/2 |
| wespeaker_cn34_ref03 | test_3 | 2／2 | 2／0／0 | 2/2 |
| wespeaker_cn34_ref03 | test_4 | 8／8 | 8／0／0 | 8/8 |
| wespeaker_cn34_ref06 | test_1 | 2／2 | 0／0／2 | 2/2 |
| wespeaker_cn34_ref06 | test_2 | 2／2 | 0／0／2 | 2/2 |
| wespeaker_cn34_ref06 | test_3 | 2／1 | 0／0／2 | 1/2 |
| wespeaker_cn34_ref06 | test_4 | 8／8 | 0／0／8 | 7/8 |

## SDK覆盖、延时与资源

下面SDK计数是实际逐case提取，非复制的合成逻辑行。attempts包含失败请求；vectors只计成功向量，native no-vector只计SDK成功返回无声纹。not_run中的计数仅为失败前已处理工作，不能外推971条覆盖或准确率。SDK未完成启动时cold_start=0表示未测得，不解释成零延迟。

| 运行 | 状态／原因 | 处理／计划 | SDK请求 | 向量 | 原生无声纹 | 登记无声纹 | 分析合格／短／静音（未完成为部分） |
| --- | --- | --- | --- | --- | --- | --- | --- |
| speechbrain_ecapa03 | evaluated | 971/971 | 935 | 935 | 0 | 0 | 935／0／36 |
| speechbrain_ecapa06 | evaluated | 971/971 | 914 | 914 | 0 | 0 | 914／0／57 |
| wespeaker_cn34_ref03 | evaluated | 971/971 | 935 | 935 | 0 | 0 | 935／0／36 |
| wespeaker_cn34_ref06 | evaluated | 971/971 | 914 | 914 | 0 | 0 | 914／0／57 |
| vosk03 | not_run:worker_failed | 13/971 | 14 | 13 | 0 | N/A（未分析） | N/A（未分析） |
| vosk06 | not_run:worker_failed | 13/971 | 14 | 8 | 5 | N/A（未分析） | N/A（未分析） |

Vosk03／06均在第14次请求失败，实际只处理13条，分别取得13／8个真实向量，后者另有5次SDK成功返回无声纹。两者最终原因都是worker_failed，分析和登记判定尚未完成；输出中的enrollment_sdk_no_vector=0及analysis_total_ms=0是未计算默认值，本表登记／分析合格栏用N/A。IPC暖P95约2013／2014ms与2秒请求预算边界一致，存储结果未进一步区分超时与其它worker错误，故不写成已证明的唯一根因。没有放宽预算或重跑；不把错误当合法拒识／无声纹，也不从部分覆盖计算准确率。

native与IPC时延分别只计实际成功向量及所有请求；原生无声纹不进入native向量时延。原始2–8秒不同长度登记／查询片段混合，不是话尾至身份结果的端到端时延。whole wall为监督器实验进程墙钟，不含外部冻结文件复核；CPU／RSS依赖当时云端条件，旧base是保存的先前运行，未复跑；其全分析还包含旧快照其它方法，只有B方法耗时属于本表政策范围。Go与worker分别RSS不可直接相加称峰值；外层child_max_rss为Linux子进程高水位，非同时总内存。

| 运行 | cold ms | native首／暖中位／P95 ms | IPC首／暖中位／P95 ms | worker CPU秒 | worker／Go RSS KiB | whole wall秒 | B方法／全分析ms |
| --- | --- | --- | --- | --- | --- | --- | --- |
| base03 | 429.982 | 137.346/143.651/193.533 | 137.733/144.051/194.062 | 136.958 | 223204/24928 | 旧保存／N/A | 217.196/458.887 |
| base06 | 428.509 | 138.742/144.230/193.233 | 139.130/144.593/193.783 | 134.053 | 223908/28176 | 旧保存／N/A | 232.897/492.269 |
| speechbrain_ecapa03 | 4172.730 | 338.131/213.126/282.496 | 339.005/213.733/283.399 | 204.786 | 843560/30492 | 208.365 | 199.270/201.280 |
| speechbrain_ecapa06 | 3510.628 | 210.173/213.784/281.691 | 210.830/214.541/282.601 | 200.029 | 840076/30360 | 202.946 | 216.312/218.398 |
| wespeaker_cn34_ref03 | 4644.145 | 227.913/151.650/199.481 | 228.573/152.327/200.144 | 146.319 | 788632/34524 | 150.163 | 199.787/201.937 |
| wespeaker_cn34_ref06 | 3821.568 | 147.673/151.983/197.822 | 148.467/152.777/198.458 | 142.618 | 790152/30364 | 145.410 | 208.923/211.009 |
| vosk03 | 1012.628 | 1172.645/1468.685/1887.381 | 1173.225/1479.337/2013.285 | 21.408 | 233912/30492 | 22.226 | N/A/N/A |
| vosk06 | 946.337 | 1189.312/1452.753/1672.013 | 1190.545/1325.169/2014.071 | 16.654 | 233308/30492 | 17.423 | N/A/N/A |

外层每次的exit、stderr、CPU、RSS和回收如下。2秒请求／10秒启动和外层3600秒整组预算未放宽；TERM5秒后KILL／wait，六次顺序固定、各一次，失败仍保留。

| 运行 | exit | stderr字节 | 外层子进程user／sys秒 | child maxRSS KiB | leader回收／组为空 | 临时目录剩余 |
| --- | --- | --- | --- | --- | --- | --- |
| speechbrain_ecapa03 | 0 | 0 | 198.701/8.271 | 843560 | True/True | [] |
| speechbrain_ecapa06 | 0 | 0 | 193.657/8.479 | 840076 | True/True | [] |
| wespeaker_cn34_ref03 | 0 | 0 | 147.299/1.235 | 788632 | True/True | [] |
| wespeaker_cn34_ref06 | 0 | 0 | 143.629/1.082 | 790152 | True/True | [] |
| vosk03 | 0 | 0 | 21.580/0.344 | 233912 | True/True | [] |
| vosk06 | 0 | 0 | 16.833/0.412 | 233308 | True/True | [] |

SDK失败与原生可行性证据不能相互覆盖：此前SB／WeSpeaker各15次合成真实提取及官方同输入路径maxdiff0只证明接口；Vosk原生严格夹具4次成功后第5次在约2014ms失败，另9条合成无声纹登记not_run。独立recognizer曾出现差异0.130504、原因未证；同状态API桥maxdiff0在请求预算外，仅作诊断。没有设置种子、改变预处理或扩充预算。

## 可运行产品、许可与采用边界

| 对象 | 实际加载／范围 | 许可与未验证项 |
| --- | --- | --- |
| SpeechBrain1.1.2 ECAPA | 官方SpeakerRecognition.encode_batch(normalize=False)，192维；VoxCeleb权重提交0f99f2d0ebe89ac095bcc5903c4dd8f72b367286 | SDK／模型卡Apache-2.0；VoxCeleb训练人物排除未验证 |
| WeSpeaker CN34参考 | 官方infer_onnx.py compute_fbank＋ORT1.23.2，256维；参考提交9fecd6cb4f47475d01761d87c826298dff4ef18c | Apache-2.0；CN-Celeb训练人物排除未验证；这是前端控制，不是完整产品 |
| Vosk0.3.45 | 实际KaldiRecognizer API＋spk0.4／small-cn0.22，128维 | SDK／模型列表Apache-2.0；wheel构建提交未验证，不将参考源码提交当wheel构建提交 |
| VeriSpeak2025.2试用 | not_run；合法TrialMode／Initialize／HTTPS成功，但两种许可探针的VoiceExtractor、VoiceMatcher均不可用 | 无音频推理／上传，无注册／购买／绕授权／全局授权daemon；不证明全部官方试用途径不可行 |
| Eagle | not_run；未取得AccessKey，用户已停止注册 | 无新成绩或付费／申请推进 |
| 小智voiceprint-api | 固定提交源码核查，未执行／未参与本轮计分 | Apache-2.0代码不替代模型／数据许可；模型ID与本轮不同，不可借用本轮数字 |

来源固定在[products.lock.json](../../tools/speaker-lab/products.lock.json)。[SpeechBrain模型卡](https://huggingface.co/speechbrain/spkrec-ecapa-voxceleb/tree/0f99f2d0ebe89ac095bcc5903c4dd8f72b367286)报告VoxCeleb1-test(cleaned) EER0.80%，属于厂商指定数据和协议；本轮普通话1:N、未知人、登记长度、共同B政策和质量分母不同，不能把0.80%当本项目错误率或与本表配对EER直接排名。[WeSpeaker参考](https://github.com/wenet-e2e/wespeaker/blob/9fecd6cb4f47475d01761d87c826298dff4ef18c/wespeaker/bin/infer_onnx.py)与[Vosk模型列表](https://alphacephei.com/vosk/models)同样不提供本轮完整产品等价成绩。所有SDK默认相似度／阈值不是身份概率。

WeSpeaker只控制其本次上游前端；相对ERes2Net-base同时改变了模型和前端，任何差异都不能单独归因于前端或网络架构。此前GoCN34实验属于历史背景，不是本轮额外冻结候选或新成绩。

实际小智[模块核查](../research/2026-11-05-speaker-product-module-audit.md)读取固定9份源码，核心是另一个ModelScope CAM++模型，登记覆盖单向量、余弦最大值阈值，Python／MySQL及临时WAV／向量持久化。服务默认.2与消费侧文档.4属于不同配置层，不能直接迁移为base阈值；没有它的同条件准确率，不宣布它更准。

## 产品决定与下一步

本轮没有支持替换ERes2Net-base＋B的清晰优势。SpeechBrain同设备旧test多正确1句，却把20/100陌生接纳，涉及8/20位陌生；WeSpeaker同样多正确1句但误接纳2/100，涉及1/20位陌生，且开发V4／V50正确接纳均略低于base。跨设备开发V50正确接纳base170/253、SpeechBrain42/253、WeSpeaker3/253；旧test分别2/14、2/14、0/14，陌生均0/100。Vosk在原2秒请求预算下未完成，无法比较身份准确率。当前选择保留既有成熟开源base作基线，优先实设备登记／前端一致性／新保留集；这不是宣称base已足够实用。

继续沿用成熟开源识别核心，Go只承担有界SDK适配／进程监督、鉴权与会话、候选成员、登记／删除生命周期、拒识策略和播放／隐私业务；不重写网络、特征或声纹算法。若后续成熟开源模块在预注册的新设备录音对照中更好且许可／资源合适，应直接复用其核心。整套服务与薄SDK接入的数据库、临时WAV、依赖和向量保存行为需分别评估。

下一次有效验证需先冻结目标设备和SDK前端：机器人实际麦克风采集约20秒多句有效登记，每句检查RMS／语音量，覆盖停顿、音量、距离；手机原始录音登记／机器人独立查询作为单列跨设备条件。每人重录登记且与查询分会话、内容和日期留出；保留从未用于调参的已知与陌生人物，固定候选家庭／50人池、正确接纳／误归／拒识／陌生误接纳及全部质量分母。只在开发划分选工作点，新保留集一次报告；同时测话尾至身份结果及云端CPU／内存。目标设备录音、新盲测和家庭使用结果本轮均未取得，不先写增益。

## 冻结、验证与可复查证据

云端根目录：`/opt/family-robot-validation/speaker-products-20261105-39ae72e`。原始音频、模型、venv、二进制、运行JSON与向量不进Git；本地只同步源码／报告。源、包和输入均在首次公开评分前冻结，包身份涵盖私有SDK源／原生库及复用的torch2.10.0／torchaudio2.10.0／numpy2.4.3依赖；排除pycache，不声称完整OS构建溯源。52源文件与Git摘要一致；全部冻结文件（其中1421唯一WAV路径／1942 case引用）每次前后复核。

冻结前发现AppleDouble旁文件，先保存SHA再只刷新Git追踪源，二进制未重建。首次门禁后，根任务的保护检查更新了被冻的protection-after.json，控制器在首个推理／日志claim之前因SHA不符退出；原config／gate、失败stdout／stderr／exit均保留。根任务将该可变绑定改为已验证的不可变副本。随后SB03／06机械启动尝试均processed0／attempts0／dimension0／cold0：深TMPDIR使Unix socket超过listenPrivate的100字节限制，SDK尚未启动。两个原始失败及旧配置／门禁保存在`pre-inference-controller-amendment-2/`，旧控制器另存为`controller-before-path-amendment.py`。根任务只缩短控制器私有TMPDIR，以实际非模型IPC的RED／GREEN及10项辅助检查验证，再独立重冻／审批；此后才是本表六次实际SDK尝试。源码／二进制／worker／模型／输入／规则／预算保持，没有成绩后调参或重跑。相关证据另见`task3-source-sync.json`、`task3-pre-score-guard-abort.json`、`root-pre-score-amendment-1.json`及控制器修正报告。

| 固定对象 | SHA256 |
| --- | --- |
| 运行配置 | 2b42abe401ce1b64be1db05156eaa3150fe71d703b8ea1e0864c36c105a9e484 |
| 根任务门禁 | 0afd8c98b6f6d3e4d4eb1a8a726a996acbfae071dd009c9d25becee57ee659ab |
| 控制器 | 1546d9c45ce1eff002b61a8a8399beb0151f7df576eda75bd7b0e523ffceb438 |
| 评测二进制 | 519b58c92a38003c27f17b7a5a8fbb57a00672186f1ac0311810916a35d6d01f |
| SDK worker | f7f53d1606bfc332c281e466a8eda43b0831a8f788ce11df1d5c304e5d23f3df |
| products.lock.json | f91808ca4b98123f838a895ba18043dc378869d3821bcec7cae93e2be6c596a9 |
| 原manifest03 | a4cee902c9835910c2619a043725683fa56cd75173d5b5f86461d8469f7fc04d |
| 原manifest06 | 29c054a30db23d0cf3ec465d270d2cb63118514f20c242e52c089f553d6c3745 |

| 固定运行 | 结果SHA256 | stdout SHA256 | stderr SHA256 |
| --- | --- | --- | --- |
| speechbrain_ecapa03 | 9d45af4bb138a08992a6fc772d4892475003e19ea71aae5c30e770475289aae6 | 7035408df6704f58e4e2414087d3399bf91d882d246148445b186deb28338732 | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| speechbrain_ecapa06 | b7232ba1a5e96c6af82ba633adf7546c2d39e822bc4fa56515bcf1c7d2e68fbb | 6ebfe14c627a7b3e1a6136e8cfddad2979f78712042266198c7f01499516d46b | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| wespeaker_cn34_ref03 | 840422e63cef61dac3ca126ad4d0108ce7e93ab5b421a9c53a2cf7e0168ebc5b | d45e6cf3a2eb1fd8fbc031b3e6a1bdabb5fcc8d352d3f07bfcd800f702a46ebe | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| wespeaker_cn34_ref06 | 7f25c5713f88c70dac312fff58f1e7afd5d52870a48df2fa17d63a202ef09f61 | 09aaceec37af8acc1b43bc2220956a59bda9974aa53024ae568ff8a97d16be88 | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| vosk03 | fad45a3b43db13f55a9bf8b3046af912449c90dd2f1bda6e479154fc7e9edcc6 | d507d80a9c00e881ca288dc13de4c6711aebbc4309240a8c59c193c057f158bf | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| vosk06 | de2b9d7ecdfc00111ddc7905a7342f3713b15ce04fb3040c0db046c75c0b09f2 | 1c27ecef0715bccd4c93adfd9dc1ca77a09c510164abb31687548dcf29f713b0 | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |

最终冻结清单文件数：20486。

旧base只读取冻结`base03-result.json` SHA `f9b313f2deaecf3002fbbe3b4992c96dba71cf6df7773ec640aefa67a8040c6c`及`base06-result.json` SHA `a604bdca537d86c0ac5756b0bdc757a7b2be42cc61cc5959af94c771d9a5d15f`中的B_single_template三个池，不重跑旧base或P2等旧方法。

本轮只新增文档／冻结／顺序运行／数据核对；既有普通／race各436通过14跳过、vet/build及已审查原生探针不重复运行。`task3-aggregate-audit.json`以807项断言独立复核24计划池行、已知／陌生分区、逐家庭／逐人物和、人物及家庭宏平均、Top1和从已输出ROC重新计算EER／minDCF；不重新提取向量或生成分数。全部逐人物／家庭和角色质量字段保留在该私有证据，报告中的N/A不计为零准确率。

根任务另以独立脚本复核全部24计划池行、分区／宏平均／Top1、标定网格及ROC单调性／插值EER／minDCF、运行和日志／结果SHA、971条／64登记人及回收，结果通过，见`root-pool-audit.json`。其中174202个机械断言主要来自逐ROC点数学核对，不是174202个新样本、运行或测试。根任务另直接读取全部6次资源／原生与IPC计数，并独立重算完成SDK的等家庭正确接纳宏平均，见`root-group-resource-audit.json`，与表格相符。

工程实现：研究worker／测试专用Go harness已实现并加载过真实SDK；公开对照：按本表实际evaluated／not_run区分；正式部署：本轮无模型／服务／阈值／固件／VAD／AEC／麦克风变更或开麦。三个任务及截至c150d29的整体审查均通过，无剩余问题。根任务最终重核20,486项冻结文件无漂移，见`root-final-frozen-integrity.json`；2026-11-05T14:26:18Z最新正式2服务字段／5SHA保持、分析进程为空、空私有临时目录已清理，见`root-final-operational-check.json`。研究代码与报告截至c150d29已推送main并核对远端；临时SSH及本机控制目录已关闭／清理。本次仅更新交付记录，不改变实验或运行时；家庭／新设备／新盲测／生产仍未验收。
