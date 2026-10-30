# Mac播放参考与上行相关性取证 Implementation Plan

> **For agentic workers:** 使用superpowers:executing-plans按任务执行。用户已经批准测试设计并要求开始，直接在main实施，不创建分支或工作树。代码复核按requesting-code-review执行。

**Goal:** 判断首次播放时上行中是否存在与当次播放PCM相关的残留，提供有时间基准和失效标记的延迟统计。

**Architecture:** 默认关闭的Mac诊断扩展，输入取转换后实际交给上行的16k PCM，参考取解码并安排播放的24k PCM。以输入tap的host time及播放器render/sample time映射时间；只在有界后台队列计算，波形仅短暂存在内存，JSON只写统计。Apple内部AEC参考仍不可直接读取。

**Tech Stack:** 便携C11计算核心、Swift/AVFoundation接入、现有私有JSONL写入器；C测试在云端cc/clang运行，Mac仅SDK编译和真人测试客户端。

## Task 1: 计算核心与云端合成验证

文件：新增`tools/native-voice/EchoCorrelation.h`、`EchoCorrelation.c`、`Tests/EchoCorrelationSpec.c`。

- [x] 先编写已知回声延迟测试及最小可编译桩，云端运行应因“未得到相关性结果”失败，保存退出码。
- [x] 实现固定容量上下文：最多两个播放、每段前三秒参考、六秒输入环形缓冲；begin/reference/anchor/capture/end/poll/close/destroy API。
- [x] 16k/24k均值降采样到4k，标记缺口而非插值造音；每250ms窗口以0.25ms步长完整搜索-200～800ms表观时延；记录有符号归一化相关系数、次峰、参考/输入能量、投影增益、边界及覆盖状态。合成验证已发现1ms粗搜可能选中旁峰，不能据此限定细搜范围。
- [x] 参考静音、输入静音、没有时钟、缺帧、尚未收齐完整搜索范围、短片段、不连续时钟均返回无结论状态，不把零填充当低回声。跨播放不共用参考。
- [x] 测试已知正/负延迟、幅度改变和反相、加噪/无关信号、静音/DC、参考/输入缺口、时钟变化、未完成、跨轮次隔离、环绕/容量边界。云端O2及ASan/UBSan运行，输出每项与总数；记录源文件SHA及测试时间。

云端命令（源码上传到私有验证目录后）：`cc -std=c11 -Wall -Wextra -Werror -O2 EchoCorrelation.c EchoCorrelationSpec.c -lm -o echo-spec`，再执行`./echo-spec`。卫生检查使用`clang -std=c11 -Wall -Wextra -Werror -O1 -g -fsanitize=address,undefined EchoCorrelation.c EchoCorrelationSpec.c -lm -o echo-spec-sanitized`，再执行该产物。预期所有合成用例通过；不调用模型或音频设备。

## Task 2: 只读客户端接入

文件：新增`Sources/AudioCorrelationProbe.swift`；修改`Sources/AudioPipeline.swift`、`OpusBridge.h`及五个显式构建入口。

- [x] 仅在NATIVE_AUDIO_TRACE有效且NATIVE_AUDIO_CORRELATION=1时创建计算对象；默认路径不保留新增PCM，不查询新增时钟。
- [x] Swift最多8个待处理作业，输入/参考单块最多4096采样，失败/积压只丢诊断；120秒后停止收集，关闭时拒绝新作业，后台清零并释放波形。
- [x] 输入传原tap host time和转换后实际PCM，注明AVAudioConverter映射未校准；参考传播放sample frame。首块完成回调前封存播放器时钟，原外部完成回调后再投递诊断，防止编号切换和原回报被计算阻塞。
- [x] 播放结束保留有界尾部输入用于完整延迟搜索；总会话停止时未收齐部分明确标记incomplete，不延迟任何播放、发包、结束或取消。
- [x] 更新所有显式构建入口编译并链接新C对象和Swift文件；OutputLive构建、共享Swift类型检查、五脚本bash -n、签名验证、git diff --check通过。

时钟边界：reference_origin=render_host-player_sample/24000；该值不是扬声器物理发声时刻。tap host属于转换前源缓冲，SRC/播放/采集缓冲和时钟量化均进入表观时延，不据此声称纯声学延迟或Apple内部参考错误。记录多次anchor的离散程度，明显跳变时不输出有效相关性。

## Task 3: 复核、真人样本及交接

- [x] 只读独立复核有界内存/工作量、归属、时钟、低能量/缺口与输出字段；修正Important/Critical后重跑相关验证。
- [x] 保留原客户端源码回退（Git归档，可重建二进制），只在窗口停止收音后加载新构建；云端服务、原身份/记忆、音量/设备和固件保持。
- [x] 私有启动器启用两个诊断开关，确认云端18040、待命未收音；已请用户手动开始一轮三次问候，真人结果另行勾选。
- [ ] 按session/playback/generation对齐本机统计与云端VAD/取消/实际播放，分别报告测量成功、低置信或采样待完成；不把相关性等同纯回声或AEC衰减。
- [x] 更新验证/交接并提交推送main；源码/合成用例入Git，原始波形、详细对话、会话标识、生成App及私有启动器不入Git。真人采样完成后恢复不带诊断的普通客户端。
