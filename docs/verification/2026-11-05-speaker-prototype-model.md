# 固定模型与登记双原型回归（2026-11-05）

六次固定对照已完成，本轮不建议部署P2或更换模型。base P2跨设备旧test仍只有2/14正确，同设备反而13→12；ResNet293 P2同设备多1句正确同时新增2/100陌生误接纳，跨设备两方法均0/14正确；CN34同设备退化、跨设备旧test仍2/14，完整50人池也明显退化。固定三模型／两方法均有完整结果，没有按效果增删候选、调参或重跑。

全部人物都已看过，仅为 `seen_development_regression`，`new_blind_test=false`。旧test只有4人14句、每人2/2/2/8句；不能从某1句改善推断总体更准，模型训练人物排除也未经证明。没有替换正式模型或部署阈值，没有真实机器人多条件采集、设备适配训练、新盲测、开麦或播放；家庭现场与生产仍未验收。

## 范围、输入与冻结

依据[预注册设计](../superpowers/specs/2026-11-05-speaker-prototype-model-design.md)与[实施计划](../superpowers/plans/2026-11-05-speaker-prototype-model.md)。Go＋sherpa-onnx v2.0.1，CPU1线程，固定RMS／长度门禁、原音频与角色。每模型／条件只提取一次，再共享向量比较两种登记方式。B按原清单顺序逐段归一化、等权平均、再归一化；P2按登记ID排序选择余弦最远种子、最多10轮、每簇至少2段，否则整人回退原B。没有跨查询积累、AS-Norm、QMF或测试集阈值调参。

“VAD仅诊断”限查询评分：登记始终保留既有RMS≥0.005、每段VAD speech_samples≥16000的双门禁，按原顺序取首次累计达到320000样本的前缀，总长320000～448000样本。查询仍以既有长度／RMS保留全部合格和拒识分母，VAD不会在本轮新增查询硬过滤。当前冻结971条输入已经满足原登记合同，没有变更门禁或登记样本。
严格公开清单每个case至少32000样本（2秒）；通用energy提取器的24000样本（1.5秒）不等于此清单的准入条件。登记另需上述RMS／VAD和20秒前缀合同，全部保持原样。

复用device03／device06各971条冻结输入，旧50人validation池、旧4人14查询test池、5／20陌生人，以及训练和cohort角色。每条件实际登记64人；旧backend登记摘要含30个cohort共94项，本轮按角色排除cohort后才比较64项登记。质量失败全部计入分母。Device03／Device06为设备条件，同设备原始物理距离未知，不冒称机器人近场或本项目设备录音。

云端隔离目录 `R=/opt/family-robot-validation/speaker-model-20261105-df83105`。运行源冻结在 `8b663dc2c581313cb923cdc6ea1431787b5c78b0`，算法运行时为已独立复核 `666aec1`；新增文件只是原生测试。`evidence/approved-run-config.json` SHA256 `96fe13b0703392848f6c84d4ed36418586c33bbd09063be2e95627743a9f9f3b`，包含50份实际源码、原始／适配权重、模型图审计／来源卡／前端源码快照、清单／质量审计、旧B完整参考和常量。原41源集合扩展，原冻结证据未覆写。

|对象|SHA256|
|---|---|
|运行二进制 `bin/speaker-lab-matrix`|`a68112ef4b0a183a9cfda22658a92a335460e7d027b86c40daeaddddeef17845`|
|base原始／实际模型|`1a331345f04805badbb495c775a6ddffcdd1a732567d5ec8b3d5749e3c7a5e4b`|
|293原始|`dbb1ccc7754caff552ebc46347a51aaee2669bb24efc740e665d1a1133d20e98`|
|293适配|`5a3db46ddfe741ce4adc9fb1c86914be8b3ca687a09b27458dabdffb1a58b829`|
|CN34原始|`e7584940aeac8d5512d875e58ce6c09ba4ddad65d8128e1dac0d93aadd087ebb`|
|CN34适配|`d52258ef63cb96b44066827a61853dd2e93f4f9d619397b1f36dda83ae584da1`|
|device03清单|`a4cee902c9835910c2619a043725683fa56cd75173d5b5f86461d8469f7fc04d`|
|device06清单|`29c054a30db23d0cf3ec465d270d2cb63118514f20c242e52c089f553d6c3745`|
|控制器 `evidence/run-matrix.py`|`320e7979f01211d5c16341eb4b92a90f56f915bdefb024c5bb206eef64f0736b`|

原始293／CN34分别114336285／26530309字节，匹配官方固定HF提交的LFS摘要／大小；镜像只承担传输。固定提交分别 `6e6bffe5bf3d772a1f143dc6dbfea58a0799ea83`／`ed25c1ecbd9e8d3c7944df95430da02bd3cdcf7a`。适配只加去重元数据，图／权重保持；图摘要293 `e5de69631cd59efa60998723599f8f40bed3880bf32fff50d1db67b4f9f61145`，CN34 `ba2f867640aee72d059260d8bea5ea7ddbb6b34e037df9646b4f08d11542d75c`，适配前后相等。base512维，另两者256维。模型卡许可声明有保存，未扩大为产品许可验收或训练人物排除证明。

首次推理前的主任务复核发现一份Go忽略的macOS AppleDouble传输旁文件混入源码冻结列表。初始51项配置SHA `00d01be39e419dd5ecc7cc84f2e71b43cad0d4ae78979c8da5b0546cd431abef` 原样保留为 `approved-run-config-pre-inference-transport-attempt.json`，旁文件隔离至私有transport-quarantine，再发布上述50项实际源码的新配置；修订和首次门禁尝试保留，无运行源码／模型／二进制变化、无公开推理／重跑。后续tar传输使用COPYFILE_DISABLE=1。公开评分后不再改冻结文件。

实际sherpa前端使用Povey窗、snip_edges=false、high_freq=-400；WeSpeaker官方参考使用Hamming、snip_edges=true、high_freq=0。`feature_normalize_type=global-mean`只使实际支持路径执行CMN，不改变窗函数等默认值。本轮比较可运行的完整工程链路，不能声称严格官方复现，不能把收益／伤害单独归因于网络架构或中文训练。

fresh `evidence/frozen-input-audit.json`重新检查1942条WAV的SHA／格式／样本数／审计、角色、源组与登记总量，64人最少登记320490样本；两条件911个非cohort键相同，登记字节相同。此前人物／来源审计链保留重用，没有按本轮分数挑样。`evidence/prior-B-reference.json`保存R0三池Top1／完整ROC／完整1%规则。冻结预检可用磁盘16.62GB、内存2.50GB；模型上限128MiB，请求2秒、启动10秒不变。base此次为新P2共享提取而重提，在推理前记录理由；不是反复重跑旧B寻找更好数字。

## 原生工程证明与命令

Mac仅代码／Git；全部依赖、Go／Python测试、构建、模型／音频操作在云端。Task1 Python9通过、普通391通过／10旧opt-in跳过；Task2普通417通过、最终race418通过／10旧opt-in跳过，vet/build通过。Task2已独立规格／质量复核，新增原生测试不改运行时，未重复全量普通／race或旧opt-in。

新 `TestRealPrototypeStudyOptIn` 默认跳过，显式逐模型运行。严格原合成清单先真实提取15条，按输入顺序取两个不同波形，在内部复用为390条逻辑案例，56个逻辑身份必须有真实双原型与`[2,2]`簇，另1个回退；它们不是57个真实人物或390次原生推理。测试强制检查全回退57人的完整三池等同B、质量失败分母、原始B向量／评分、非验收／synthetic／开放集unavailable元数据、匿名输出及向量清零／worker回收／socket删除。公开loader保持public_human严格合同。

先用旧两登记片段夹具运行，exit1准确触发“must exercise actual two-prototype identity”；此前一次字段名错误造成编译失败单独保留，不当行为RED。扩展每模式两段后首次三模型均通过；自查补充完整回退池／簇成员断言后的最终base／293／CN34分别3.425／18.095／3.062秒通过，维度512／256／256，新增默认skip0.005秒、最终build exit0。所有尝试、日志、退出值和精确环境在云端`evidence/task3-native*.log/.exit`与两份`task3-native*-check.sh`，没有隐藏失败或重跑公开数据。

公开评分后的测试专用审查修订（I1，2026-11-05）：原测试只检查提取原件的有效性，不能证明实际借给分析的`cs`副本未被修改。现在分别在正向／全回退分析前对实际输入作仅RAM快照，返回后逐float32位精确比较（含nil／长度）；另对57个原生回退输出、56个`[2,2]`正向双原型显式清零后再次要求全部借用输入不变。快照通过测试cleanup清零，返回原型与比较临时向量通过defer保证断言／错误时也清零；原提取清零、worker回收及socket删除检查保留。此修订未改运行时、算法、校准、清单或公开评分，不替换上述推理前原生证明。

修订使用独立云端源码`R/source-ownership-reviewfix`与独立二进制`R/bin/speaker-lab-ownership-reviewfix`。只在scratch测试中注入一次单bit输入变更，保持finite／非零，base定向测试exit1、3.499秒，准确触发`positive analysis borrowed native vector bits changed`；同一变异源码的默认检查SKIP、exit0、0.005秒。随后恢复与Mac相同的测试文件，执行同一`go test -count=1 -v -run '^TestRealPrototypeStudyOptIn$' .`：最终默认SKIP／exit0／0.006秒，base PASS／exit0／3.440秒／512维，293 PASS／exit0／18.293秒／256维，CN34 PASS／exit0／3.084秒／256维；每个显式模型仍是15次原生提取、390复用案例、56双原型／1回退，均完成所有权与清理断言。独立build exit0、输出为空。全部精确环境／模型路径及命令保留在`evidence/task3-ownership-reviewfix-check.sh`；每次日志和退出值为`evidence/task3-ownership-reviewfix-{build,red-default,red-base,final-default,final-base,final-resnet293,final-cn34}.{log,exit}`。没有执行普通／race／旧opt-in／公开矩阵。

修订测试SHA256 `f4f0342199ae430c96fe79b2baf026ab40956e84665b15c514c54832db4a1d2c`，独立二进制SHA256 `b831d2aebf1dd250ca34f884863a9255e970f511639e2157c82bace628b19c55`；50项独立源文件清单摘要`5038ed6c7831b2717e556f4a4240d7fa54aaca52e8c95a41bc1623c0ea61d583`见`evidence/task3-ownership-reviewfix-preservation-after.json`，其中仅测试文件不同。`preservation-before.json`／`preservation-after.json`（共同前缀`task3-ownership-reviewfix-`）记录87项当前冻结摘要符合配置、1080个原文件前后保持、analysis_processes为空；原测试摘要`ade462c8f1d9f5f0215b3d050387bcf321254fad8f69fbea4cd38f3d356b7825`、原二进制a68112...和配置96fe13...均保持，六份结果／运行元数据／stderr也未改。准备阶段两次只读清单脚本因遍历到历史隔离旁文件引用、误用stderr后缀而失败，关联两次tar因目标尚未创建失败；全部发生在任何构建／原生运行前，无冻结文件改写。之后按当前冻结字段及实际`-result.stderr`名称核对通过，再创建独立副本；Mac只做代码／gofmt／diff检查。

以下是本轮已执行的云端命令记录；原隔离目录的冻结二进制和公开结果不得覆写或重跑。后续复现须另用云端隔离目录、重新冻结并审查，不能把新结果混入本轮。

```sh
R=/opt/family-robot-validation/speaker-model-20261105-df83105
export PATH=/opt/family-robot/toolchains/go1.27.3/bin:/usr/bin:/bin
cd "$R/source/tools/speaker-lab"
GOTOOLCHAIN=local GOMAXPROCS=2 go test -count=1 -v -run '^TestRealPrototypeStudyOptIn$' .
GOTOOLCHAIN=local GOMAXPROCS=2 go build -o "$R/bin/speaker-lab-matrix" .
SPEAKER_LAB_TEST_PROTOTYPE_FIXTURE=/opt/family-robot-validation/speaker-p0-20261101-1be02bf/synthetic/manifest.json \
SPEAKER_LAB_TEST_MODEL="$R/models/resnet293-sherpa.onnx" \
SPEAKER_LAB_TEST_SHA256=5a3db46ddfe741ce4adc9fb1c86914be8b3ca687a09b27458dabdffb1a58b829 \
SPEAKER_LAB_TEST_BINARY="$R/bin/speaker-lab-matrix" \
GOTOOLCHAIN=local GOMAXPROCS=2 go test -count=1 -v -run '^TestRealPrototypeStudyOptIn$' .
```

原生通过后，由主任务复核冻结清单和控制器才开始公开旧回归。控制器独占创建stdout／stderr／meta，前后核对全部冻结SHA，独立进程组3600秒上限、TERM5秒后KILL并wait，检查进程组消失和leader回收；记录模型／代码／配置／结果摘要、CPU、峰值RSS和wall。不改预算、不丢失败查询、不覆写结果；失败记not_run而不是全拒识。精确CLI由配置拼出：

```sh
"$R/bin/speaker-lab-matrix" prototype-study \
  --model "$R/models/resnet293-sherpa.onnx" \
  --sha256 5a3db46ddfe741ce4adc9fb1c86914be8b3ca687a09b27458dabdffb1a58b829 \
  --manifest /opt/family-robot-validation/speakerb-20261105-ba0ce90/public/backend/device03-manifest.json
# 仅用于说明，已完成的运行不要再执行：
python3 "$R/evidence/run-matrix.py" base03
# 冻结顺序：base03/base06/resnet29303/resnet29306/cn3403/cn3406，各一次。
```

## 全量结果

六次顺序运行均exit0、stderr0，控制器错误为null，leader回收、进程组消失，所有冻结SHA前后保持。没有公开推理失败或not_run，没有公开重跑；初始原生编译／夹具失败和首次冻结传输元数据修订已在上文单独保留。输出分别为 `evidence/<run>-result.json`、`.stderr`、`<run>-run.json`；后者记录UTC、代码提交、模型／输入／二进制／配置摘要和CPU/RSS，与冻结配置一起绑定确切CLI和50份源码摘要。共同配置SHA为上文96fe13...，代码8b663dc，控制器320e79...。

|run|exit / stderr字节|结果SHA256|
|---|---|---|
|base03|0 / 0|`f9b313f2deaecf3002fbbe3b4992c96dba71cf6df7773ec640aefa67a8040c6c`|
|base06|0 / 0|`a604bdca537d86c0ac5756b0bdc757a7b2be42cc61cc5959af94c771d9a5d15f`|
|resnet29303|0 / 0|`a098135a209290128d71857d49124d917c440f7ca7c6f576718841f1c6200831`|
|resnet29306|0 / 0|`513f5c24dd55265dfa6fdbdfed3e6e36302a0372f102d93d075006df891ec5cd`|
|cn3403|0 / 0|`4bbc05d7c757cc35d413c25e0081fab7716289fbf7af2aa17855e5d848d914a6`|
|cn3406|0 / 0|`6bbb337a617c11458748ccb0d7a0d6af979efb54a8c3b38f1eebb21db6136941`|

本任务独立汇总2097项检查通过；主任务另用独立实现检查2532项，0失败、无pending。base B两条件×三池的完整Top-1、ROC、主1%规则（标定、组、人物、宏平均）精确重现保存R0。旧固定规则仅作历史参照，没有为替代模型添加择优规则。模型／方法／池各自用validation标定，1%预算基于25条全部陌生validation查询，floor(25×.01)=0；这是样本经验约束，不保证总体FAR≤1%，test仍必须独立报告。

base P2仅分4人组的跨设备validation正确188→198，完整50人池170→166，旧test仍2/14；同设备完整50人222→215、test13→12。ResNet293 P2同设备test12→13但新增2条陌生误接纳，两个陌生人物各至少1次；跨设备两个方法旧test全拒识。CN34同设备test9→8，跨设备虽仍2/14，完整50人池仅6→14/253，远低于base170。这些局部改善／无效／伤害都保留，不据某池或旧14句选赢家。当前工程链路没有形成替换或部署依据。

全部36池结果；K为已知全部/合格，C/W/R为正确接纳/误归/拒识，U为陌生全部/合格，FA/R为误接纳/拒识；宏为等人物全量/合格正确接纳的0～1比例，人物K/Kq/U为已知／至少一条合格查询已知／陌生人数。

| 模型 | 条件 | 方法 | 池 | K全部/合格 | C/W/R | U全部/合格 | FA/R | 宏all/q | 有FA陌生人数 | 人物K/Kq/U |
|---|---|---|---|---|---|---|---|---|---|---|
| base | device03 | B_single_template | validation4 | 253/224 | 224/0/29 | 25/19 | 0/25 | 0.8933/1.0000 | 0 | 50/49/5 |
| base | device03 | B_single_template | validation50 | 253/224 | 222/0/31 | 25/19 | 0/25 | 0.8817/0.9881 | 0 | 50/49/5 |
| base | device03 | B_single_template | test4 | 14/14 | 13/0/1 | 100/100 | 0/100 | 0.9688/0.9688 | 0 | 4/4/20 |
| base | device03 | P2_two_prototypes | validation4 | 253/224 | 224/0/29 | 25/19 | 0/25 | 0.8933/1.0000 | 0 | 50/49/5 |
| base | device03 | P2_two_prototypes | validation50 | 253/224 | 215/0/38 | 25/19 | 0/25 | 0.8552/0.9436 | 0 | 50/49/5 |
| base | device03 | P2_two_prototypes | test4 | 14/14 | 12/0/2 | 100/100 | 0/100 | 0.9375/0.9375 | 0 | 4/4/20 |
| base | device06 | B_single_template | validation4 | 253/231 | 188/0/65 | 25/25 | 0/25 | 0.7349/0.8033 | 0 | 50/46/5 |
| base | device06 | B_single_template | validation50 | 253/231 | 170/0/83 | 25/25 | 0/25 | 0.6717/0.7310 | 0 | 50/46/5 |
| base | device06 | B_single_template | test4 | 14/13 | 2/0/12 | 100/69 | 0/100 | 0.2500/0.2500 | 0 | 4/4/20 |
| base | device06 | P2_two_prototypes | validation4 | 253/231 | 198/0/55 | 25/25 | 0/25 | 0.7788/0.8511 | 0 | 50/46/5 |
| base | device06 | P2_two_prototypes | validation50 | 253/231 | 166/0/87 | 25/25 | 0/25 | 0.6479/0.7051 | 0 | 50/46/5 |
| base | device06 | P2_two_prototypes | test4 | 14/13 | 2/0/12 | 100/69 | 0/100 | 0.2500/0.2500 | 0 | 4/4/20 |
| resnet293 | device03 | B_single_template | validation4 | 253/224 | 218/0/35 | 25/19 | 0/25 | 0.8691/0.9753 | 0 | 50/49/5 |
| resnet293 | device03 | B_single_template | validation50 | 253/224 | 215/0/38 | 25/19 | 0/25 | 0.8537/0.9421 | 0 | 50/49/5 |
| resnet293 | device03 | B_single_template | test4 | 14/14 | 12/0/2 | 100/100 | 0/100 | 0.9375/0.9375 | 0 | 4/4/20 |
| resnet293 | device03 | P2_two_prototypes | validation4 | 253/224 | 218/0/35 | 25/19 | 0/25 | 0.8691/0.9753 | 0 | 50/49/5 |
| resnet293 | device03 | P2_two_prototypes | validation50 | 253/224 | 217/0/36 | 25/19 | 0/25 | 0.8591/0.9651 | 0 | 50/49/5 |
| resnet293 | device03 | P2_two_prototypes | test4 | 14/14 | 13/0/1 | 100/100 | 2/98 | 0.9688/0.9688 | 2 | 4/4/20 |
| resnet293 | device06 | B_single_template | validation4 | 253/231 | 20/0/233 | 25/25 | 0/25 | 0.0850/0.0923 | 0 | 50/46/5 |
| resnet293 | device06 | B_single_template | validation50 | 253/231 | 20/0/233 | 25/25 | 0/25 | 0.0850/0.0923 | 0 | 50/46/5 |
| resnet293 | device06 | B_single_template | test4 | 14/13 | 0/0/14 | 100/69 | 0/100 | 0.0000/0.0000 | 0 | 4/4/20 |
| resnet293 | device06 | P2_two_prototypes | validation4 | 253/231 | 27/0/226 | 25/25 | 0/25 | 0.1120/0.1218 | 0 | 50/46/5 |
| resnet293 | device06 | P2_two_prototypes | validation50 | 253/231 | 27/0/226 | 25/25 | 0/25 | 0.1120/0.1218 | 0 | 50/46/5 |
| resnet293 | device06 | P2_two_prototypes | test4 | 14/13 | 0/0/14 | 100/69 | 0/100 | 0.0000/0.0000 | 0 | 4/4/20 |
| cn34 | device03 | B_single_template | validation4 | 253/224 | 224/0/29 | 25/19 | 0/25 | 0.8933/1.0000 | 0 | 50/49/5 |
| cn34 | device03 | B_single_template | validation50 | 253/224 | 220/0/33 | 25/19 | 0/25 | 0.8752/0.9815 | 0 | 50/49/5 |
| cn34 | device03 | B_single_template | test4 | 14/14 | 9/0/5 | 100/100 | 0/100 | 0.8438/0.8438 | 0 | 4/4/20 |
| cn34 | device03 | P2_two_prototypes | validation4 | 253/224 | 224/0/29 | 25/19 | 0/25 | 0.8933/1.0000 | 0 | 50/49/5 |
| cn34 | device03 | P2_two_prototypes | validation50 | 253/224 | 215/0/38 | 25/19 | 0/25 | 0.8540/0.9598 | 0 | 50/49/5 |
| cn34 | device03 | P2_two_prototypes | test4 | 14/14 | 8/0/6 | 100/100 | 0/100 | 0.8125/0.8125 | 0 | 4/4/20 |
| cn34 | device06 | B_single_template | validation4 | 253/231 | 12/0/241 | 25/25 | 0/25 | 0.0402/0.0437 | 0 | 50/46/5 |
| cn34 | device06 | B_single_template | validation50 | 253/231 | 6/0/247 | 25/25 | 0/25 | 0.0188/0.0204 | 0 | 50/46/5 |
| cn34 | device06 | B_single_template | test4 | 14/13 | 2/0/12 | 100/69 | 0/100 | 0.2500/0.2500 | 0 | 4/4/20 |
| cn34 | device06 | P2_two_prototypes | validation4 | 253/231 | 19/0/234 | 25/25 | 0/25 | 0.0611/0.0665 | 0 | 50/46/5 |
| cn34 | device06 | P2_two_prototypes | validation50 | 253/231 | 14/0/239 | 25/25 | 0/25 | 0.0464/0.0504 | 0 | 50/46/5 |
| cn34 | device06 | P2_two_prototypes | test4 | 14/13 | 2/0/12 | 100/69 | 0/100 | 0.2500/0.2500 | 0 | 4/4/20 |

Top-1与配对ROC仅诊断，不是身份正确接纳率；正负配对不独立。

| 模型 | 条件 | 方法 | 池 | Top1正确/合格 | 正/负对 | EER | normalized minDCF |
|---|---|---|---|---|---|---|---|
| base | device03 | B_single_template | validation4 | 224/224 | 224/748 | 0.008929 | 0.026786 |
| base | device03 | B_single_template | validation50 | 224/224 | 224/11926 | 0.001929 | 0.031250 |
| base | device03 | B_single_template | test4 | 14/14 | 14/442 | 0.004525 | 0.071429 |
| base | device03 | P2_two_prototypes | validation4 | 224/224 | 224/748 | 0.008929 | 0.035714 |
| base | device03 | P2_two_prototypes | validation50 | 224/224 | 224/11926 | 0.002264 | 0.040179 |
| base | device03 | P2_two_prototypes | test4 | 14/14 | 14/442 | 0.006787 | 0.071429 |
| base | device06 | B_single_template | validation4 | 230/231 | 231/793 | 0.047919 | 0.527440 |
| base | device06 | B_single_template | validation50 | 219/231 | 231/12569 | 0.030949 | 0.365683 |
| base | device06 | B_single_template | test4 | 13/13 | 13/315 | 0.184127 | 0.538462 |
| base | device06 | P2_two_prototypes | validation4 | 229/231 | 231/793 | 0.049180 | 0.502165 |
| base | device06 | P2_two_prototypes | validation50 | 216/231 | 231/12569 | 0.030303 | 0.347163 |
| base | device06 | P2_two_prototypes | test4 | 13/13 | 13/315 | 0.230769 | 0.846154 |
| resnet293 | device03 | B_single_template | validation4 | 224/224 | 224/748 | 0.026786 | 0.200893 |
| resnet293 | device03 | B_single_template | validation50 | 221/224 | 224/11926 | 0.026786 | 0.209967 |
| resnet293 | device03 | B_single_template | test4 | 14/14 | 14/442 | 0.058824 | 0.142857 |
| resnet293 | device03 | P2_two_prototypes | validation4 | 224/224 | 224/748 | 0.026786 | 0.200893 |
| resnet293 | device03 | P2_two_prototypes | validation50 | 222/224 | 224/11926 | 0.026786 | 0.232843 |
| resnet293 | device03 | P2_two_prototypes | test4 | 14/14 | 14/442 | 0.006787 | 0.071429 |
| resnet293 | device06 | B_single_template | validation4 | 208/231 | 231/793 | 0.129887 | 0.913420 |
| resnet293 | device06 | B_single_template | validation50 | 173/231 | 231/12569 | 0.116883 | 0.815118 |
| resnet293 | device06 | B_single_template | test4 | 13/13 | 13/315 | 0.193651 | 0.923077 |
| resnet293 | device06 | P2_two_prototypes | validation4 | 212/231 | 231/793 | 0.129870 | 0.887446 |
| resnet293 | device06 | P2_two_prototypes | validation50 | 175/231 | 231/12569 | 0.114568 | 0.787940 |
| resnet293 | device06 | P2_two_prototypes | test4 | 13/13 | 13/315 | 0.212698 | 0.846154 |
| cn34 | device03 | B_single_template | validation4 | 224/224 | 224/748 | 0.018717 | 0.252889 |
| cn34 | device03 | B_single_template | validation50 | 223/224 | 224/11926 | 0.009727 | 0.128837 |
| cn34 | device03 | B_single_template | test4 | 14/14 | 14/442 | 0.004525 | 0.214286 |
| cn34 | device03 | P2_two_prototypes | validation4 | 224/224 | 224/748 | 0.021390 | 0.299107 |
| cn34 | device03 | P2_two_prototypes | validation50 | 223/224 | 224/11926 | 0.013393 | 0.156950 |
| cn34 | device03 | P2_two_prototypes | test4 | 14/14 | 14/442 | 0.006787 | 0.428571 |
| cn34 | device06 | B_single_template | validation4 | 198/231 | 231/793 | 0.129887 | 0.948052 |
| cn34 | device06 | B_single_template | validation50 | 128/231 | 231/12569 | 0.108225 | 0.934345 |
| cn34 | device06 | B_single_template | test4 | 13/13 | 13/315 | 0.228571 | 0.846154 |
| cn34 | device06 | P2_two_prototypes | validation4 | 191/231 | 231/793 | 0.128625 | 0.917749 |
| cn34 | device06 | P2_two_prototypes | validation50 | 128/231 | 231/12569 | 0.106055 | 0.938612 |
| cn34 | device06 | P2_two_prototypes | test4 | 12/13 | 13/315 | 0.253968 | 0.846154 |

登记覆盖仅64实际登记人物，不把30cohort算入。

| 模型 | 条件 | 方法 | 双原型 | 回退 | 按角色状态 |
|---|---|---|---|---|---|
| base | device03 | B_single_template | 0 | 0 | test:single_template=4; train:single_template=10; validation:single_template=50 |
| base | device03 | P2_two_prototypes | 55 | 9 | test:fallback_single=2,two_prototypes=2; train:fallback_single=2,two_prototypes=8; validation:fallback_single=5,two_prototypes=45 |
| base | device06 | B_single_template | 0 | 0 | test:single_template=4; train:single_template=10; validation:single_template=50 |
| base | device06 | P2_two_prototypes | 55 | 9 | test:fallback_single=2,two_prototypes=2; train:fallback_single=2,two_prototypes=8; validation:fallback_single=5,two_prototypes=45 |
| resnet293 | device03 | B_single_template | 0 | 0 | test:single_template=4; train:single_template=10; validation:single_template=50 |
| resnet293 | device03 | P2_two_prototypes | 51 | 13 | test:fallback_single=2,two_prototypes=2; train:fallback_single=1,two_prototypes=9; validation:fallback_single=10,two_prototypes=40 |
| resnet293 | device06 | B_single_template | 0 | 0 | test:single_template=4; train:single_template=10; validation:single_template=50 |
| resnet293 | device06 | P2_two_prototypes | 51 | 13 | test:fallback_single=2,two_prototypes=2; train:fallback_single=1,two_prototypes=9; validation:fallback_single=10,two_prototypes=40 |
| cn34 | device03 | B_single_template | 0 | 0 | test:single_template=4; train:single_template=10; validation:single_template=50 |
| cn34 | device03 | P2_two_prototypes | 54 | 10 | test:fallback_single=1,two_prototypes=3; train:fallback_single=1,two_prototypes=9; validation:fallback_single=8,two_prototypes=42 |
| cn34 | device06 | B_single_template | 0 | 0 | test:single_template=4; train:single_template=10; validation:single_template=50 |
| cn34 | device06 | P2_two_prototypes | 54 | 10 | test:fallback_single=1,two_prototypes=3; train:fallback_single=1,two_prototypes=9; validation:fallback_single=8,two_prototypes=42 |

时间单位ms，RSS单位KiB；控制器wall/CPU单位秒。native/wall为剔除首请求后的P50/P95，冷启动单列；worker和parent峰值不能相加为并发内存。

| run | 合格提取 | native P50/P95 | IPC wall P50/P95 | cold启动 | 首native/IPC | worker CPU | worker RSS | parent RSS | 总wall s | 总child CPU s | child RSS |
|---|---|---|---|---|---|---|---|---|---|---|---|
| base03 | 935 | 143.65/193.53 | 144.05/194.06 | 429.98 | 137.35/137.73 | 136957.54 | 223204 | 24928 | 139.84 | 139.19 | 223204 |
| base06 | 914 | 144.23/193.23 | 144.59/193.78 | 428.51 | 138.74/139.13 | 134053.26 | 223908 | 28176 | 136.33 | 136.30 | 223908 |
| resnet29303 | 935 | 931.32/1227.29 | 931.67/1227.61 | 663.44 | 869.28/869.67 | 869612.38 | 383740 | 27508 | 873.93 | 871.84 | 383740 |
| resnet29306 | 914 | 928.87/1229.17 | 929.42/1229.66 | 683.42 | 837.01/837.40 | 850599.84 | 399524 | 39844 | 854.51 | 852.80 | 399524 |
| cn3403 | 935 | 148.58/195.55 | 149.07/195.95 | 164.99 | 136.71/137.14 | 140460.35 | 134644 | 48504 | 141.99 | 142.60 | 134644 |
| cn3406 | 914 | 148.93/194.66 | 149.33/195.20 | 163.85 | 136.39/136.78 | 136977.89 | 135652 | 28444 | 138.45 | 139.11 | 135652 |

独立汇总核对 2097 项通过；base B两条件×三池Top1、完整ROC和完整1%rule对象均精确等于保存R0。完整组／逐人／calibration和ROC点列留在云端原始JSON；汇总核对其人数／分母／守恒／等人物宏平均，未挑选人物或池。

## 真实设备登记与独立查询采集步骤（尚未执行）

先建立参与者知情同意与私有录音协议、保留期限和访问范围；当前公开 `public_human` loader不能装入家庭私有录音，更不能把私有声音改标公开／合成。项目尚无已完成的产品登记界面或自动采集工具，下面是后续人工采集协议，不是假称现成功能。代理不会自动开麦或播放。

1. 机器人登记→机器人查询：参与者手动开始／结束采集，用真实机器人麦克风录多句，总有效登记约20秒。沿用本研究合同，每段PCM至少2秒、RMS≥0.005与VAD有效语音≥1秒；按预先确定的顺序取首次累计达到20秒的前缀，总长20～28秒，至少两段。涵盖常用距离、朝向、时间和正常背景，逐段记录设备、样本数、条件和同意状态。登记不包含多人重叠，不自动按分数删段；质量问题提示另录并保留原因，不按识别分数挑登记。若要独立验证双原型，需要至少4个合格登记片段并保留整人回退情况。
2. 手机原始登记→机器人查询：直接取得手机原始录音作为登记，记录采样格式／设备与转换链；随后参与者在机器人前独立现场说新句子作为查询。不用手机扬声器播放登记录音给机器人当跨设备证据。机器人登记／机器人查询与手机登记／机器人查询两条件单列。
3. 登记与查询分时、分内容、分源录音组，查询覆盖平常距离、安静／实际背景和短句；标注单人／重叠、设备和来源。至少保存既有长度／能量不合格查询并计入总分母，不能只看合格Top1。
4. 训练／开发校准／最终独立保留人物及来源分开；先固定模型、模板数、门禁和整个候选池的陌生接纳预算，再封存新保留录音。陌生人单独采集，不参与已知登记；新增模板数后重新验证陌生误接纳。手机和机器人条件分别报告正确接纳、误归、拒识、陌生FA及逐人物宏平均。
5. 新录音只用于后续独立协议，不在本轮旧分数上择模型、调阈值或宣布生产通过。有监督设备适配需另外取得训练人物／目标设备语料，本轮未训练。实体设备播放、打断、端到端时延、多人／重叠及家庭现场仍须实际验证。

本轮三个任务的规格／质量复核均已通过；Task3原生所有权断言缺口已按上文单独修补并复审关闭。整体复核及最终正式保护、main推送与SSH收尾仍由主任务完成后补记。

## 下一轮优化的优先顺序（待独立预注册和执行）

1. 先核对候选模型的实际音频前端：在固定、非身份评测的音频上比对官方参考与Go／sherpa的特征计算，查清窗函数、边界、频率范围、幅度和CMN差异；严格对齐后再立新的性能对照。本轮不把未对齐的候选结果当成模型架构定论。
2. 优先验证目标设备登记：用机器人自身麦克风完成合格的多句约20秒登记，与手机原始登记共用独立现场查询作对照；先检查采集资格，再冻结人物／来源／划分及陌生接纳预算。现有公开PC配对登记的补充清点并未形成完整合格50人新方案，不能静默替换模板或冒充机器人采集结果。
3. 只有在上述采集和前端核对后仍有明显失配，才评估使用另行隔离训练人物的设备适配训练。新的盲测结果出来前，base B只是回归基线；不部署本轮P2、替代模型或研究阈值，也不承诺这些后续假设一定提高识别率。
