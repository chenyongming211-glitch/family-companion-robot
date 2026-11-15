# 固定 TTS 微雪播放核对

日期：2026-11-15。关联[实施计划](../superpowers/plans/2026-11-15-fixed-tts-waveshare-playback.md)、[资产说明](../../tools/tts-playback-pilot/README.md)、[云端A/B结果](2026-11-15-tts-metadata-compatibility.md)。

**当前：保护检查和审查通过；首次现场窗口到时关闭并恢复原入口，未取得实机播放样本。** 本轮0个有效微雪会话、0次固定播放intent，没有发声或新增提供方调用，不能宣称实机对齐、连续A/B或中断恢复已通过。用户反馈仍待后续同步。

## 实现和验证

仅新增默认不构建的 `.go.txt` 云端测试资产；正式产品代码、固件无改动。通过既有 `Server.runDevice` / `Session.generate` 注入固定源，复用公共鉴权、Opus、流控、真人中断和I2S回执。记忆、表情、音乐、工具关闭，单设备/owner/Opus/单会话；两次固定播放额度持久化，取消/失败/未知不重播，会话120秒、服务6分钟。

固定源来自A的已完成公开合成结果，141,360个24k mono PCM16样本、5.89秒、18条字词。输入文件及元数据SHA冻结；没有新主回答或TTS调用。输入监听及OutputASR仍保留作独立检查，其API费用不能漏算。

实际Opus encoder lookahead为156个24k样本。读取硬件已播位置后先扣除lookahead并限制在源样本范围，再以120ms余量确认完整字词。全文还要求ended、非cut、final、completed==sent及完整源覆盖；缺失终态/未知尾部不升级。只读审计不改变产品上下文/记忆，硬件计数仍不等于独立声学逐字证明。

云端相关4包普通/race各 **820 PASS、3 SKIP、0 FAIL**（含subtests；新增9个保护顶层，Serve默认skip），vet和编译通过。初始公共检查因独立目录缺少既有Python对照脚本失败，补齐当前Git跟踪资源后通过；原失败日志保留。新增保护及源元数据事件均有RED→GREEN，规格/质量只读复核PASS。复用路由操作器、更新本阶段health负例后的18项保护检查通过。

- 资产源码SHA：`0d3e7095f62cd4a00dceaec0e5f6e52b814e8c10f2472100bf8e7b0599c20961`。
- 测试二进制SHA：`4738f7c65b65037c7896e1ff21a2da2ce1b90f1183d440208ee72ea8df93c3e5`。
- 固定PCM SHA：`9b5af3699c41dc2de0d1033c245b08d981ed1266d556d159dd499320996e0e1d`。
- 原始metadata SHA：`7993af01ccbe106cac296825dc1c021ffd5d735d07be8248f57bba3f2d3af657`。

## 现场边界

用户已在现场并授权继续必要测试，发声前已告知。临时入口只匹配指定微雪MAC，正常鉴权后还核对稳定owner。新基线、原入口file/admin、候选exact-route proof及新apply intent均冻结；临时实例loopback18818。正式语音732503、管理635398及受保护配置保持基线，无正式发布或刷机。

自动窗口最长300秒，退出时精确核对临时PID/exe、关闭实例并guarded restore；外部file/admin漂移则拒绝覆盖。不得重用本轮intent或直接重跑历史路由命令。

现场窗口于北京时间约11:02开始，最长300秒后自动收尾；11:07最终核对：Caddy文件恢复原SHA `adb6222394b10c444c5d563b241afc87be39aa17fb00d5bce2c4f2d22653f115`，admin配置与原配置相同，guarded restore-complete存在；临时播放/分流两个unit均MainPID=0。正式语音732503、管理635398的exe、命令行、环境、starttime、unit及受保护配置与新基线完全一致；健康ready/memory_ready=true、active_sessions=0。全局账本SHA保持 `329829041e469bb1d165ad33ea786d1325b14092707e0c1ac1260e09476c2e8d`，协议9/主回答34未增加。

下次仍待核对：第一次完整播放是否连续/截尾；第二次真人“停一下”的输入识别、停声、最终I2S回执及确认前缀。没有有效会话并不说明用户是否尝试了操作；本轮没有可用实机证据。重新开放须fresh路由准备/intent，不能重用本次已消费apply/restore。

私有证据根 `/home/ubuntu/fixed-tts-waveshare-20261115/`（dev日志、冻结proof、record事件/音频）；路由准备 `/home/ubuntu/waveshare-native-integration-20261114/ops/route-fixed-tts-v1/`。凭据、真人录音、完整会话和二进制不入Git。
