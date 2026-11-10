# 松手调节交付与语音意图跟进

本批始于2026-11-10，跟进至11日。用户明确要求音量/亮度松手提交、整10小圆刻度和具体提示，并报告语音无法获取当前状态。规格/计划见[设计](../superpowers/specs/2026-11-10-slider-release-query-design.md)、[计划](../superpowers/plans/2026-11-10-slider-release-query.md)。

## 已交付小程序

UI提交cb2cdb1。原生slider范围0..100/step10，changing只预览，change松手提交一次；两条各11个圆点，0/50/100辅助标注，刻度不拦触摸；去掉两个设置按钮。提交明确提示“正在设置音量为70，等待设备回执”，等待/成功/失败含项目名称；queued/sent仍不算成功。原权限/在线/能力门槛、单命令、50秒独立截止、隐藏/退出/401隔离保持，同值不重发。

云端独立工作目录`/home/ubuntu/slider-release-query-20261110/ui`，复用锁一致的既有依赖，无Mac运行/安装。先19项中7个新增RED，再GREEN；补明确目标提示的RED后，最终runtime20/20、完整148/148（0fail/cancel/skip）、typecheck/build/native验证通过。59跟踪输入摘要d30c9ec9a57c2891d84707d1eba7f7affac49867401be235c30ff43f634704da；独立Spec/Quality及整体复核通过。

Mac原客户端dist已更新，23份实际产物与云端逐项SHA一致；微信官方CLI实际编译预览成功168075字节。手机真实拖动、松手、刻度对齐和手感仍待验，未把静态/行为测试当作手机体验通过。

## 已发布语音查询修正

现有三个固定模型工具为device_get_status、device_set_volume、device_set_brightness，映射固件MCP状态/设置接口。设置本来就有设备状态读回；查询能力并非硬件不支持。

实际旧运行二进制df5386/PID3256763与136份冻结源码对齐，相关4文件与Git相同。云端仅查看本次设备的脱敏诊断：工具发现3/available；实际识别的音量/亮度查询含“多大”，两轮均not_candidate、无查询调用；其后设置verified。用户回述的“多少”原本已支持，不能说它本身原来失败。正文、凭据及原始音频未复制Mac/Git。

373fbb9仅补“多大”查询识别，7个生产planner夹具包含原话联合查询、只读不得写、两字段缺一不成功。原代码3项RED为control_scope_required；修正定向GREEN0.567秒。完整相关race：realtimelive162顶层/521含子测22.433秒，devicetools16/51含子测1.077秒，0fail；2项已有长超时可选测试skip，入口包无测试；vet/build通过。首次完整检查因公开product-identity测试配置缺失失败，补齐同摘要公开夹具后完整通过，未用失败轮替代最终结果。云端原始两句另在真实识别函数只读回放通过，未输出正文。

发布严格复用原运行release136份源，独立核验集合与仅control_scope.go/test两文件差异，等Git373fbb96a76382042da0741ad31f2a822278d523；没有整包发布main研究功能。新release`device-query-20261110-r1`，binary SHAfa7baf5eb7b54f72b1c49bc2cebddc0ab9e14c1a526d967d6d891ce1049278ce、PID277452。14项发布/回滚fake测试及helper独立复核通过，空闲门槛后切换；原argv/env、600ms公共S3策略、11保护文件/旧记录在切换时保持，ready/memory_ready、active0。管理200267/Caddy2415423未改未重启；未刷任何设备或开启USB。

发布后用户现场新会话实际多次device_get_status→verified、mutation_dispatched=false、route completed；也有用户主动设置并verified。后续新对话和记忆更新正常发生，不宣称所有旧记忆文件永久不变。查询工具实机读回已有证据，用户是否听到两个正确数值仍未明确回执；当前对话保留，没有为跟进再重启。

云端源码/测试/构建/脱敏记录在`/home/ubuntu/slider-release-query-20261110`；发布保护快照及含环境的回滚资料只在root0700/0600的`/var/backups/family-robot/device-query-20261110`。没有回写旧config或历史记录。

## 相对亮度准备版：未发布

用户随后报告精确数值能调，但“把屏幕亮度调大一点”不识别。c2523cc为明确亮度字段增加调大/调小/大小一点，排除屏幕尺寸/字体，保留先读后写、查询只读及字段隔离。9个新增真实planner用例RED→GREEN，向上/向下为读30后设40/20并匹配回执；完整相关178顶层/587含子测race PASS、0fail、2项原有可选skip，vet/build及独立Spec/Quality通过。候选c370ee81ba738f806af690231ae9c44b4efacee85edd930daf3555a906645c4f只在云端`/home/ubuntu/slider-release-query-20261111-relative`。

用户进一步讨论由模型理解上下文、含蓄抱怨和反话。该补词版暂未部署，不把它或当前fa7发布版称为上下文语义方案完成。建议方向见[讨论方案](../superpowers/specs/2026-11-11-contextual-device-control-design.md)；用户已确认“上下文明确时直接小幅调整，有歧义才问一句”，模型语义机制进入实施；未完成前不计发布。当前实际服务仍fa7/PID277452，不使用c252准备版或候选hash冒充已安装。
