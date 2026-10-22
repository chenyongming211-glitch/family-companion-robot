# Qwen-Live-Harness 真实模型隔离对照

本轮实际运行官方 realtime 核心、官方 `LiveSession` 编排和官方输出队列，连接真实 `qwen3.8-omni-flash-realtime`，复现了“打断后错误认为完成”及“继续数到五十一至一百”。官方清输出/取消机制有效，但没有因此获得用户实际听到的位置；不能以迁移框架替代暂停任务和可信播放边界。

## 环境与证据范围

- 官方源码固定 `b2d17b8706b226ff7792683ecd8f050f18f9752e`；完整 archive 下载至 `.superpowers/overnight-20261023/qwen/`。此前选取的11份 TypeScript 文件与该 archive 逐字节一致，记录 `source-match.json`。上游源码未修改。
- 隔离 Linux `/tmp/family-robot-overnight-qwen/`，私有 Node `22.22.0`，不使用全局 npm、不读取默认 `~/.qwen`、不运行 Electron；没有占用端口、访问麦克风、播放音频、刷机、重启或修改正式服务。
- 实际模型16kHz单声道s16le上行、24kHz下行、`semantic_vad`；官方 core 自己发送 `create_response=false`、`interrupt_response=true`，随后由自己的会话仲裁创建响应。保留其内置 `qwen3-asr-flash-realtime` 输入转写字段，没有另接辅助 ASR。
- root 仅在子进程内存读取 `/etc/family-robot/server.env`，不输出/保存内容。输出音频只计数，不保存、播放；只使用既有机器合成 WAV。
- 真实调用的依赖为 `ws8.22.0/ansi-regex6.4.0/@agentclientprotocol/sdk2.0.1/tsx4.23.15/jieba2.0.2/prompts2.4.2/proper-lockfile4.1.2`。前三项不同于官方 lock 中的 `8.21.3/6.3.0/0.14.1`；SDK 后端未启用。之后修正隔离依赖并通过无计费 `LiveSession` 导入检查（`locked-dependencies-import-PASS`）；**不能声称真实调用由官方完整 npm ci 锁定环境执行**。

`artifacts/overnight-20261023/qwen/*.jsonl` 保存时间、原生事件、生成稿和模拟 sink 的帧完成计数。可重复驱动在 `.superpowers/overnight-20261023/qwen/probe.ts`（核心对照）与 `probe-native.ts`（原生编排）；这些忽略目录不是公开交付或生产程序。

## 实际运行层级

核心对照直接调用原版 `openQwenRealtimeSession`。完整编排对照额外实例化原版 `LiveSession` 与空的原版 `BackendRegistry`，通过官方 `openRealtime` 注入点打开原版核心；实际执行其 speech_started、取消、输出抑制及上下文流程。没有加载任务执行后端、Memory、Proactive、屏幕或相机。

设备边界使用定制静音 `LiveHostControl`，输出通过原版 `OutputPlaybackTracker/scheduleOutputFrame`、实时时钟和定时器完成帧；将 started/completed 回调回送 `LiveSession`。因此这验证的是原生实时编排加**模拟输出 host**，不是完整桌面 daemon、Electron WebAudio 或手表扬声器。真实调用中没有重写模型回复、裁剪历史、插入已听文本或推断中文逐字位置。

每20ms持续送640字节，语音之间持续送零值静音，不使用手动 commit。数数输入4.569秒，插话2.966秒（转写“听一下，先告诉我一加一等于几”）；有效继续输入使用 Go 对照的长句“请继续刚才没有完成的数数任务，一直数到原来要求的终点”。

## 结果

5条独立会话顺序执行，总计12条实际响应；每条会话内保留同一连接。没有 quota/fatal 错误。

| 场景 | 实际路径 | 结果及限制 |
| --- | --- | --- |
| 官方 stock no-backend 提示词，5秒后插话 | 原版 realtime +队列；初版自制 host | 生成完整1–50，数学2。0.657秒“继续”没有有效第三轮，不能判接续。初版仅依赖 onBargeIn 清队列，漏掉生成已完成后的 speech_started；该适配器缺陷已识别，**不当作官方缺陷或有效播放对照** |
| stock +简化履约补充 | 原版 realtime，修正 speech_started 清队列 | 只说“没问题，我这就从一数到五十”便结束，没有实际数；数学2。短“继续”无有效第三轮。不作为提示词改动效果统计 |
| Go 对等口头履约提示词，5秒后插话，再长句继续 | 原版 realtime +队列，清输出按原生编排规则镜像 | 2913/2913帧接受；生成32.64秒计数音频，模拟仅完成5.76秒；数学2；继续答“刚才的数数任务其实已经完整结束了，我已经从一数到了五十” |
| Go 对等提示词，0.5秒后插话，再长句继续 | **原版 LiveSession +原版 realtime +原版队列** | 2685/2685帧接受；真实 provider `response.done=cancelled`，原生 `playback.cleared(reason=speech_started)`。初稿文字已含完整1–50，实际生成音频仅5.44秒，模拟完成1.28秒。官方 dialogue 仍保存该全文并标记 interrupted=true。数学2；继续实际生成**51–100**，越过原终点50 |
| Go 对等提示词，5秒后插话 | **原版 LiveSession +原版 realtime +原版队列** | 1429/1429帧接受。count 12.787秒 generation completed；13.064秒 speech_started 原生清队列，模拟只完成5.76秒，而完整生成音频36.32秒。数学2且队列正常完成。为控制调用预算不再发第三轮 |

Go 对等提示词取 `ROBOT_INSTRUCTIONS`（缺失时用 Go 默认家庭助手文本），附 `internal/qwen/client.go` 当前完整口头履约段；没有混入官方 stock prompt。原生 `LiveSession` 的模型配置保留，使用注入点替换这段 instructions 作为提示词对照，不能声称 stock 默认配置天然具有 Go 的任务约束。

## 关键证据解释

1. 生成完成与播放完成确实相隔很远。5秒编排对照中，完成生成早于用户 speech_started 277ms，但队列仍有约30.56秒未模拟完成。官方 `onBargeIn` 不是覆盖此情形的唯一出口；必须执行编排层的 speech_started 清输出逻辑。该轮并无 onBargeIn，不能据此宣称没有清输出。
2. 早期打断确实触发了真实 provider cancelled，原生编排也清输出；不是只有“收到话音”而未取消。但 cancelled/interrupted 标志没有把全文变成已听前缀，后续模型仍从51继续到100。
3. 逐条帧定时结束只代表模拟 sink 的完成；1.28/5.76秒既不是手表 I2S 回执，也不能换算成听到“第几个数字”。本轮没有据此修改任何历史。
4. 两个接续失败路径各仅一个有效样本，不估计故障率。数数是否履约也受提示词及模型波动影响；stock 与 Go 提示词不应混作纯框架性能排名。

## 对项目的含义

可以借鉴官方输出身份、结束封口、清队列以及“生成已结束而输出仍在播”时的独立处理；单纯沿用同一连接或迁移原生编排无法保证恢复目标和终点。继续任务需要独立保存原请求/终点/旁支问答归属，并保留播放边界未知的事实。真实字词恢复仍需设备播放证据与可验证文字映射，本轮没有得到这项能力。

本报告没有修改上游或正式源码，没有部署；也不声称完成现场唤醒、AEC、停声延迟、真实手表进度或逐字接续验收。

证据收尾：5份远程 JSONL 对实际 key 做仅在 root 内存的子串扫描，结果 PASS；未输出 key。本地 `summary.json` 汇总5条会话/12条响应，`sha256.json` 固定日志摘要，`dependencies-after-lock.txt` 记录复现环境修正后的依赖。
