# TTS 真实协议修复与云端 A/B 单轮验证

日期：2026-11-15。关联[对比方案](../superpowers/specs/2026-11-14-voice-backend-cost-comparison-design.md)、[静默实施计划](../superpowers/plans/2026-11-14-quiet-voice-backend-comparison.md)、[前次记录](2026-11-14-quiet-voice-backend-comparison.md)。

**结果：A、B 两条云端单轮文件链路均 completed；微雪播放对齐、连续对话与中断恢复仍待验。** 本阶段没有播放、开麦、连接设备、分流、重启服务、部署或刷机。实际 TTS 为 `cosyvoice-v3-flash / longanyang`，24k mono PCM16；3.5 和克隆未测试。

用户明确：关注最终产品长期 API 费用，在不影响功能/效果的前提下节省费用；必要开发测试继续，不因助手设置的小额预留反复申请追加。用户已在现场，可配合后续测试。成品比较仍按每天聊30分钟，包含辅助API；不是以开发预留判断产品费用。

## 为什么元数据不一致

1. **原代码将示例当成强制约束。** 实际 `sentence-begin`、`sentence-synthesis` 可以有非空 `words`，其中字词时间会更新；原代码强制为空，误拒绝正常任务。现在忽略预估值，只信校验后的 `sentence-end` 最终字词时间。
2. **最终标点标签存在格式归一化。** 实际任务把末尾逗号标成句号，或省略末尾 `～`，或将 `～` 标成 `~`。仅兼容这些末尾格式差异，结果保留原稿；不放宽词义、内部标点、索引、顺序、覆盖或音频时长要求。
3. **原生文字输出也暴露了协议假设。** 配置ACK省略明确关闭的VAD/辅助转写/空tools；会话级助手事件不带response_id；结束项可只给text类型而不重复正文。仅适配这些真实返回，仍严格绑定回答和助手item身份、增量/完整文字、完成终态和usage。

首次2026-11-14的合并错误没有完整事件，不能逐字段还原那一次；后续实际捕获已精确复现上述失败，先RED再最小修复GREEN，六组私有实际事件/元数据重放留在云端。没有靠重试或猜测放宽词义校验。

## 实际 A/B 结果

两边使用同一公开合成输入“早上好。”，16k mono PCM16、1.58秒、50,560字节，输入SHA `89fa45a66de755d4484a388bf3c67787e2db4fc5f6ef062fffd470f5713b10c1`。无真人录音。

| 项目 | A：Omni 3.8 文字＋TTS | B：Fun-ASR＋Qwen Plus非思考＋TTS |
| --- | --- | --- |
| 状态 | completed | completed |
| 有效24k音频 | 141,360样本／5.89秒 | 133,680样本／5.57秒 |
| 字词元数据 | 18项 | 16项 |
| 主回答usage | 输入145（文字131、音频14），输出11 | ASR duration=1；LLM输入78、输出12 |
| TTS计费字符usage | 7＋13＋13＝33 | 7＋9＋13＝29 |
| 批处理总耗时 | 2693ms | 3791ms |
| 按方案公开单价估算 | 约0.00363元 | 约0.00332元 |

费用未与供应商账单核对，不是实际扣费结论。回复内容和长度不同、样本各仅一条，不能据此排名质量/长期月费；总耗时不是首音延迟，也没有上传/播放硬件证据。此命令单轮读文件、保存文件，未提供产品实时会话/连续A/B切换。

## 保护与验证

- 私有 `events.jsonl` 在解析前保存有效WS JSON，0600、每条fsync、上限8MiB，使用json.Compact；不记录二进制音频和握手凭据。失败的音频/字词不升级为可信结果，完整错误只留云端。
- 每个API先持久intent和预留，失败/未知不返还；阶段计数允许严格日期标签，不清空全局调用数或重复保护。默认协议8、主回答48不变，第9个协议采集是本轮一次追加。
- 云端 **77项通过（71项仓库检查＋6项私有实际重放）**，race、vet、build通过；末尾宽度差异等新增回归有RED→GREEN，规格/质量只读复核通过。无新依赖、服务、前端或固件代码。
- 最终验证二进制SHA `f3ccc352bb768acbe20cceee0d3296ba24b02a6e9222cdf132d5ac388710709b`。
- 完成B后，全局账本协议9、主回答34，历史失败/未知均保留。累计预留 **2.145元**，与实际费用不同；不再将助手设置的小额开发预留当作反复等待用户的门槛。
- 正式语音进程732503、管理635398的exe/命令行摘要及Caddy、server.env、admission、管理config摘要与静默前基线相同；健康ready/memory_ready=true、active_sessions=0。

## 云端证据与下一步

私有根 `/home/ubuntu/quiet-voice-backend-20261114`：

- A：`final-ab-20261115/native-v1/result.json`。
- B：`cascade-final-20261115/result-v1/result.json`。
- 最终构建、77项/race/vet日志及RED证据：`finish-cascade-20261115/`。
- 原始采集、逐次失败、父账本路径/SHA和用量分别保留在各阶段目录，不覆盖/重置；完整原音、全文、返回、配置和二进制不入Git。

下一步按[固定TTS微雪计划](../superpowers/plans/2026-11-15-fixed-tts-waveshare-playback.md)，先同一固定音频完整播放，再真人中断；复用Opus、实际encoder lookahead和I2S回执，保守映射到TTS字词。物理逐字精度、漏讲/重读与后续连续对话必须分别验证，不以本次云端通过替代。

参考：[CosyVoice服务端事件](https://help.aliyun.com/zh/model-studio/cosyvoice-server-events)、[Omni客户端配置](https://help.aliyun.com/en/model-studio/client-events)、[Omni服务端事件](https://help.aliyun.com/en/model-studio/server-events)。
