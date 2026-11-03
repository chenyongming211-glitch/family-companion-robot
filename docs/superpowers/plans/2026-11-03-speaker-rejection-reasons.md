# 声纹拒识原因诊断

> 按systematic-debugging/TDD执行。直接main；Go/ONNX测试及部署仅云端，Mac只编辑/Git与SDK客户端。原产品、模型、阈值、VAD、AEC、保存策略均不改。

**触发事实：** 单人对照中逐句AX读取为A、不确定、不确定。已结束收音。当前根Session、lab质量门禁和Tracker匹配均可能返回uncertain，旧结果不含细分原因，不能事后猜测这两句的真实来源。

**目标：** 为下一次针对性核验提供仅内存的固定原因码和片段总长度，不采集或保存新录音、向量、相似度、转写或原文，不宣称修复识别准确率。

## 协议及语义

- lab仅`--realtime-test` observation增加`reason`，ready/closed及原manual-test输出不变。原因：`candidate`（临时候选）、`too_short`（总片段不足1.5秒）、`low_energy`（既有RMS门禁）、`match_uncertain`（Tracker拒绝可靠匹配）、`capacity`（未知音色超出临时容量）。不输出分数或模板。
- Go speakerlive解码兼容旧5字段和新6字段，新增字段严格校验枚举及label一致性，其他未知/重复/null/错误类型仍拒绝。Result增加Reason与AudioMS；AudioMS复用已校验的duration_ms。
- 根Session对`incomplete_span`、`too_short`、`too_long`、`not_queued`、`observer_unavailable`给固定原因。片段长度按所持PCM字节计算，不等于有效人声时长；未知时0。失效终态优先，迟到结果不覆盖当前发言。
- WS仅增加可选reason/audio_ms，不进入recorder或磁盘。客户端兼容旧事件，新事件在不确定后显示可理解的原因及片段秒数；不再用通用“请轮流说话”冒充重叠检测。无原因时明确“证据不足”。

## 步骤

- [ ] lab：先云端RED覆盖短片段、低能量、匹配灰区、容量、候选及manual兼容；实现原因字段，普通/race/vet及真实模型opt-in。
- [ ] Go桥/Session：先云端RED覆盖旧/新schema、非法原因、状态兼容、原因和时长透传、各拒识来源、永久失败优先与隐私；最小实现。
- [ ] 客户端：先SDK RED断言原因显示、旧事件兼容、未知枚举/类型拒绝；实现并构建普通/试点/soak，不开麦。
- [ ] 独立审查；最终源摘要/受影响race与构建。以新路径成对部署试点server+helper，旧文件保留可回退；只在试点active0时切换，不动产品。
- [ ] 云端合成链路验证reason/audio_ms及无正文/音频落盘，核对产品保护摘要。
- [ ] 关闭旧测试App和自己的转发，打开新诊断窗口，不自动收音；以一次针对性发言获取真实原因，不再盲目重复三句。等待真人结果，不将诊断完善写成准确率修复。
