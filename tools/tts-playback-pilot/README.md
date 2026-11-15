# 固定 TTS 微雪播放核对

这是显式启用的云端验证资产，不是产品后端或自动测试中的发声入口。仅支持微雪 ESP32-S3 2.16；不改固件或正式服务。关联[实施计划](../../docs/superpowers/plans/2026-11-15-fixed-tts-waveshare-playback.md)。

`fixed_tts_test.go.txt` 默认不参与构建。仅在独立云端源码显式复制为 `internal/realtimelive/fixed_tts_pilot_test.go` 后测试；Mac不运行。普通测试使用合成fixture，Serve默认skip，只有精确环境值才可启用现场入口。服务器凭据、固定源PCM、完整结果和设备记录均不进入Git。

现场版本通过 `Server.runDevice` / `Session.generate` 播放已验证的公开合成TTS，复用公共鉴权、Opus、流控、中断和I2S回执；没有新主回答或TTS调用。保留输入监听和OutputASR，辅助API仍有费用，不将本测试当作成品省钱效果。

保护包括：仅一个指定MAC/鉴权owner、Opus、单会话，持久账本跨连接最多两次播放，先fsync intent再emit，失败/未知不重播；会话120秒、服务6分钟，退出CloseActive。固定输入及SHA仅指向云端本轮验证源；不能通过替换文件重用旧intent。

结束时记录 `tts_playback_alignment`：从公共turn冻结已播/下发样本、cut/ended/final及证据来源，减实际encoder lookahead，再用120ms余量映射TTS字词。完整硬件终态且覆盖源才确认全文，其他尾部保持unknown；这是只读审计，不修改产品上下文或记忆。内部回执不能独自证明物理逐字精度。

云端保护检查（默认无提供方/设备调用）：

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go test -count=1 ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio
/opt/family-robot/toolchains/go1.27.3/bin/go test -race -count=1 ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio
/opt/family-robot/toolchains/go1.27.3/bin/go vet ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio
```

现场操作必须先告知用户，并完成新一轮exact-route准备、源/二进制证明与自动清理。复用云端 `native_route_guard_v4.py`（SHA `cdb9438c4be936834a95238005fcc18f5e2b80e9bb33114609fe55f2ebd48f8b`）的原子apply/restore；仅更换本阶段临时unit及health契约，不重用既有apply intent。不可直接运行历史路由命令。结束后关闭临时实例、核对Caddy file/admin和正式语音/管理基线，外部配置漂移时拒绝覆盖。

用户操作：唤醒说“开始”，第一次完整听完；再说“再来一次”，第二次听到中段时说“停一下”。固定内容为“早上好！今天天气不错，适合出门走走。”，约5.89秒。记录声音是否连续、有无截尾及实际停止位置。本阶段不验收连续A/B交流或自动续讲。
