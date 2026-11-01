# P0 真人输入：仅内存的会话内说话人测试入口

> **For agentic workers:** 使用 `superpowers:subagent-driven-development`，测试先行、先规格后质量审查；直接main，不建分支/worktree。Go运行与依赖在云端，Mac仅构建/运行原生测试客户端。

**Goal:** 用户目前只有一位参与者，先让其用Mac麦克风验证同一会话多句发言的标签一致性；保留后续多人轮流输入的能力，不登记姓名或持久模板。

**Architecture:** 独立Mac采样窗口，每次手动点击才采集4秒PCM。经现有SSH授权把有界片段送给云端`speaker-lab session`；云端临时Tracker仅在该进程内存保存特征。CLI每个片段使用独立、可终止的模型子进程，避免当前worker的2秒空闲读取期限与真人停顿冲突；这项诊断允许每段冷启动，不当作正式语音延迟或常驻worker方案。

**Tech Stack:** 现有Go原型与ERes2Net研究权重；AppKit/AVFoundation/AVAudioConverter、SSH管道。无公网新接口，无本地语音后端，无模型Key下发。

**边界：** 这是P0的输入工具，不提前进入人物登记或正式Realtime试点。当前多人/重叠检查仍未验证。每个片段的单人属性是用户按界面说明轮流说话的人工测试条件，不宣称算法已验证单人。单人结果不能证明多人区分、陌生人拒识或跨设备质量通过。

## Task 1：云端session命令

**Files:** `tools/speaker-lab/session.go`、`session_test.go`，修改`main.go`和README。

- [ ] 先在云端运行RED测试：有界输入、会话/代次/ID/采样连续性、坏帧、静音/过短、A→A与A→B→A、退出清理、不能输出向量/音频/路径、EOF/取消可结束。
- [ ] 新增显式`session --model --sha256 --declared-single-speaker`；未声明测试条件不启动。参数只接受研究阈值及固定模型摘要；初始ERes2Net采用本轮合成校准值，明确不是家庭真人阈值。
- [ ] stdin复用v1 request framing：4096B以内JSONheader、binary PCM16mono16k≤8秒。固定scope=`manual-test`，同一session/generation，严格递增ID和连续采样，禁止旧片段/其他scope混入。第一段前先验证模型文件/摘要并输出不含私人数据的ready。
- [ ] 每段先检查≥1.5秒及基本能量；通过后启动现有worker，2秒调用、10秒冷启动、退出回收。只在父进程保留临时Tracker，返回片段ID、temporary/uncertain/unknown、guest_N、处理耗时及`user_declared_test`证据类型。
- [ ] 短/低能量段不冒充“已确认单人”；显示不确定，不分配人物。新会话创建全新Tracker，最多8个访客。
- [ ] 最多32片段/5分钟；EOF、取消、达到限额或协议错误结束并清除特征引用。不会向正式语音服务调用任何接口，不落音频、特征、转写、人物数据库或会话结果文件。
- [ ] 输出只经SSH回到当次窗口；不将真实测试stdout重定向到文件。原生worker输出继续被屏蔽，core dump禁用。程序不承诺清除所有系统/运行时内存副本，不改变系统swap设置。
- [ ] 云端完整module测试/race/vet/build、真实合成片段通过session管道验证；模板/PCM只在内存；提交限定源码。

## Task 2：Mac手动采样窗口

**Files:** `tools/speaker-session/main.swift`、`Capture.swift`、`Transport.swift`、`Info.plist`、`build.command`、`open.command`和README。

- [ ] 窗口说明“每次点击采集4秒，发送到云端临时分析；程序不保存录音或登记声纹”。只提供采样与结束/重开操作；没有姓名输入。
- [ ] 环境配置SSH目标、现有私钥路径、远端二进制/模型及摘要。Process用参数数组；远端参数正确shell quote，不拼接未转义输入。不开本地服务，不复制服务器API凭据。
- [ ] 第一次点击连接独立CLI，收到ready后才申请麦克风权限并采样。打开窗口本身不取麦、不发声、不自动采集；服务器失败时保持未采集状态。
- [ ] AVAudioConverter显式选输入channel0，再转16k mono Int16，避免已知多声道转静音问题。最多4秒/64000samples，音频回调与UI串行交接、取消代次隔离；停止后移除tap，释放缓冲。
- [ ] 串行等待一段结果再允许下一段，stdout逐行JSON严格限长；只显示本次访客标签或不确定。结束时关闭SSH和采样，丢弃临时结果；再次采样新建会话，不携带上一会话特征。
- [ ] SDK构建到被Git忽略的`.superpowers/speaker-session/`，ad-hoc签名；静态检查构建脚本。运行时客户端测试只操作按钮/退出和可见状态，不由代理自动开启麦克风。
- [ ] 云端Go测试与MacSDK构建后，规格/质量独立审查；打开未收音窗口，再请用户点按钮完成3～5段不同句子。用户没开始时不得自动采集。

## 验收与后续

只有一位参与者时，记录的是“现场单人多句一致性”，不提升为多人识别通过。用户可以说不同短句，无需长时间静默或重复问候，不进行语音播放。后续有其他参与者时再按原设计做换人、陌生人和重叠场景。

首轮场景足够判明一致性后停止采集；任何异常保留不含音频/向量的必要代码与测试证据，真实片段不转存。正式Go语音、设备记忆、C3策略及固件不修改。
