# 声纹模型与登记双原型对照设计（2026-11-05）

用户已在上一轮建议后明确“继续出优化方案，继续测试”，授权本轮隔离开发与测试；沿用 main 直接提交推送，不再次请求通用实施许可。先登记设计与计划，再实现、审查、冻结并评分。基线 df83105。

## 已有证据与路线取舍

B跨设备旧4人池14查询在预冻结1%验证工作点正确接纳2、误归0、拒识12（合格13，已知候选Top1为13/13）；这不是总体识别率或误归12次。既有AS-Norm/QMF和相邻查询积累均未支持部署。所有现有人物已经看过。真实机器人登记／新独立保留集仍未取得，不开麦、不播放、不部署。

比较三条路线：实际机器人多条件登记＋目标条件标定最贴近日常用途，但需参与者新录音；替代编码器＋登记表示可直接用现有固定录音做回归，本轮执行；有监督设备适配需另备训练人物和目标设备语料，本轮只记录采集要求、不训练或捏造数据。

## 本轮目标与约束

- 隔离 Go＋sherpa-onnx v2.0.1，CPU1线程；保持既有长度／RMS门禁、音频、角色与候选池。VAD仍仅诊断。
- 三编码器固定：ERes2Net-base；WeSpeaker VoxCeleb ResNet293-LM（大模型）；WeSpeaker CN-Celeb ResNet34-LM（中文训练候选）。不根据本轮分数增删候选。
- 两登记方式固定：B_single_template原等权归一化融合；P2_two_prototypes按以下确定算法构造。没有查询积累、AS-Norm、QMF或测试集调参。
- Mac只代码／Git；依赖、模型处理、所有Go/Python测试／构建／音频操作只云端隔离目录。正式模型／服务／配置／固件／VAD／AEC／麦克风不改。
- 模型、原音频、运行JSON、向量、凭据和二进制不入Git；向量只RAM，所有自有向量在退出／出错清零。无注册／厂商联系，既有邮件草稿不发送。
- 所有当前评分标记 population=seen_development_regression、new_blind_test=false、query_accumulation=false；家庭／生产／端到端时延未验收。合成原生测试必须 synthetic_engineering_fixture 且 open_set_assessment=unavailable。

## 模型取得与接入

官方列表：https://github.com/wenet-e2e/wespeaker/blob/master/docs/pretrained.md ；官方ONNX参考：https://github.com/wenet-e2e/wespeaker/blob/master/wespeaker/bin/infer_onnx.py 。

|候选|官方权重指针|原始SHA256|字节|
|---|---|---|---:|
|ResNet293-LM|https://huggingface.co/Wespeaker/wespeaker-voxceleb-resnet293-LM/raw/main/voxceleb_resnet293_LM.onnx|dbb1ccc7754caff552ebc46347a51aaee2669bb24efc740e665d1a1133d20e98|114336285|
|CN-Celeb ResNet34-LM|https://huggingface.co/Wespeaker/wespeaker-cnceleb-resnet34-LM/raw/main/cnceleb_resnet34_LM.onnx|e7584940aeac8d5512d875e58ce6c09ba4ddad65d8128e1dac0d93aadd087ebb|26530309|

固定HF提交后再从镜像传输；镜像只是传输源，须与官方固定提交的LFS指针SHA／大小一致。记录模型卡声明而不扩大为产品许可验收。原始和适配文件分别保留、各自计算SHA；图／初始化权重不改，只加入去重元数据：framework=wespeaker、sample_rate=16000、output_dim=实际输出维度、normalize_samples=0、feature_normalize_type=global-mean、语言／固定来源URL／研究注释。拒绝外部数据、重复或矛盾关键元数据、输入非float32 [B,T,80]、输出非float32 [B,256]、错误摘要或大小。图序列化摘要适配前后相等。

官方参考使用Hamming窗、snip_edges默认true、high_freq默认0并做CMN；所查sherpa v2.0.1通用路径仅按metadata设置采样率／幅度并支持global-mean，其他前端默认Povey／snip_edges=false／high_freq=-400。必须核对实际文件／SDK，记录差异，不能把元数据适配冒充完全复现官方前端。本轮比较实际可运行的Go＋sherpa完整候选链路，不能单独归因于神经网络架构；不为本轮重编SDK或引入第二条评分实现。

实验工具模型上限从100MiB提高到128MiB，以容纳114336285B候选；仍拒绝超界／符号链接／错误摘要。保留2秒逐请求、10秒启动边界；候选若在预检超时，记录当前链路不适配，不按人物／分数挑样，不静默放宽或丢弃查询。所有批量运行总超时3600秒，TERM后5秒KILL整个独立进程组，wait回收。

## 双原型算法（只使用同一人物的登记向量）

接口 buildEnrollmentPrototypes(clips []evidenceLabeledVector, dimension int) ([][]float32, prototypeSummary, error)，输入必须同一人物且ID唯一，原输入不改；返回原型归调用者，摘要不含向量。至少4个合格登记片段，否则返回原B均值单模板，status=fallback_single、reason=insufficient_segments。

1. 复制并L2归一化每段，按片段ID字典序排序。选择无序余弦最小的一对为种子；相同值按有序ID对首先出现者，初始簇0是较小ID。完全重合或无法形成两个非空簇则回退。
2. 最多10轮。每片段归到余弦更大的原型，平分归簇0；任一簇少于2段时立即回退整个原B模板，reason=empty_or_singleton_cluster，不删段。
3. 每簇用既有fuseEnrollment等权融合得到新中心；清理上一轮自有中心。分配不变则停止，reason=stable；到10轮保留最后中心／对应分配，reason=iteration_limit。融合数值失效时回退原B，若原B也失效则协议错误，不能从其余片段挑选补救。
4. 返回两个自有中心及簇大小／轮数；输入副本清零。fallback由原始全部登记向量调用原fuseEnrollment，保证与B完全相同。查询对该人物的分数为两个余弦的最大值；无标签、未来发言或识别结果参与构造。

两个原型只是登记内部变化的候选表示，不是已采集真实跨设备模板。max会增加误接纳机会，因此必须按整个身份候选池重新标定，不能沿用B门槛或只报Top1。

## 评分与资源

新显式CLI prototype-study --model ABS --sha256 SHA --manifest ABS；复用严格loadBackendManifest及extractEvaluation，只提取一次后同时评分B／P2。内部 analyzePrototypeStudy(ctx,m,a,cases,dimension) 可接受明确合成输入；公开CLI仍只接受严格public_human清单。缺失／相对／非法参数固定usage，运行错误prototype_study_failed，不回显路径／声音／向量。

复用backendPoolQueries／backendTop／backendPoolROC／backendRuleResult／backendCalibrate。每模型／条件／方法独立validation标定：未知经验目标.01，margin网格 [0,.01,.030229,.05,.1]，预算floor(unknown*.01)，保持验证已知误归约束与原tie规则。三个池为validation50按testN分组、validation50全池、testN实际池。主规则仅unknown_target_0.01；不放宽5%目标或择优选规则。记录完整计数、质量拒识、人物宏平均、组／人物分布、配对诊断、原型／回退覆盖，所有分母保持。

条件复用旧device03/device06各971条冻结输入及审计。每模型／条件各运行一次（共6次），旧B此次因P2需要新提取才重跑；不是为改善历史B数字。B_single_template必须重现已保存R0的Top1、主规则完整标定／计数／宏平均，才能解释P2差异。旧B两固定规则本轮不作为替代模型规则。基线同模型同原型变更间共享提取，所有比较固定音频和资格。

输出保留native／wall分位、CPU／RSS／冷启动及处理时间；不当网络／真实等候时延。先代码／原生审查，再冻结模块源／二进制／模型原始与适配SHA／清单／审计／算法常量。失败和修订完整保留，不能覆写成功运行或为好看重跑。若仅元数据修正不影响公开评分，沿用已成功输出并说明提交差异。

## 判定及真实设备后续

本轮只能给出回归支持或反对：P2或替代模型在旧test正确接纳提高时，还须逐项报告误归／陌生接纳、同设备退化、全部validation池及耗时，不能由一次14句宣布总体更准或商用通过。所有不利／无提升／不适配结果保留。没有达到部署证据也算完成本轮诚实实验交付。

实际采集沿用：机器人麦克风多句约20秒登记，多个时间／常用距离记录条件；手机原始录音登记→独立机器人现场查询单列，不播放手机录音冒充跨设备；登记／查询／训练／校准／新保留人物和来源隔离，陌生查询单独收集。新模板数变更后重新验证陌生误接纳。执行需要参与者手工操作，不由代理自动开麦。本轮只完善步骤，没有这些现场结果，也不训练设备适配模型。

工程验收为云端RED/GREEN、原型数学／所有权／错误、CLI固定错误／元数据／分母／基线语义、原生实际两原型路径及清理；最终普通／race／vet／build一次，已有opt-in默认skip，新opt-in明确单跑。任务规格／质量及整体复核后更新报告／handoff／计划，正式2服务／5SHA保持，进程回收、SSH关闭，main推送并核对远端HEAD／清洁工作区。
