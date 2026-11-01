# C3 稳定按键模式收口

用户决定：停止继续实现 C3 播放中的语音插话。C3 保留 Opus 稳定按键模式，按键可开启、结束及重新开启多轮对话；播放期间关闭麦克风上行，真实排空后恢复。微雪与 M5 保持已有 Opus 与语音插话能力。此决定取代 C3 PCM/完整回声处理的继续开发路线，不把未完成试验当成已验收产品。

## 实机核对

本次只使用单一串口句柄，Flash/eFuse 写入均为 0，没有重新刷固件、恢复旧 NVS 或要求用户重复试说。

- USB/MAC、C3 rev0.4、16 MiB Flash、安全身份摘要与之前保护基线一致。
- selector0 为 seq3/ABORTED4，selector1 为 seq2/VALID2，当前选择稳定 app1。
- app1 实际设备 MD5 `322a8501b128e1ef79ef6b022b4105c3` 与本机已验收的 `6b79e4c014c36ee30817f0f186d50e2d502f736a` 完整镜像一致；该镜像 SHA256 为 `435bd4810dfbb60c5c5eaf1fdfcdc117ad8068ae261a79a90e67d9f8ee0f4bbd`。
- 实际启动 ELF `bf00948a142424d320e89c955606d8214cd6e1994cbdfdffc8f5b536c2709dc3`、SKU、版本、ota_1、联网激活及 idle 均符合稳定版，ready 后继续观察 15.166 秒，无故障。
- 串口已关闭。工具零写不表示正常启动期间设备绝无自身 NVS 更新；本次未写回未授权区域。

私有证据在 `.superpowers/c3-button-rollback-20261101/usb-stable-check/`；启动日志 SHA256 `e7b4129a5ad15c3b8950eed58a776fc6e449c95f6d07d92db3c06bb82f6aa9b1`。原按键、多轮、记忆、音量和待机的真人验收沿用[原稳定版记录](2026-10-31-xmini-c3-button-entry.md)，本次未伪记新的真人听感结果。

## 研发现场与服务

原实现者已停止，云端没有该轮预检在运行。未完成的 5 文件 IRAM/审核器改动已保存为 Git 外的 `unfinished-iram-capture.patch`、源文件快照和逐项 SHA 清单，再恢复到当时 HEAD；没有丢弃证据，也没有把未验证改动提交为产品。

Go 仍为 `58c14cd`，`ready/memory_ready=true`、active0，gateway health 为 ok/active0。本次未部署、重启或修改正式配置，共享 VAD/取消/drain、鉴权、记忆和工具未变。

## 默认构建与验证

源码 `6fb9504f5426614467b7a0e767a3fe3d202ccfed` 已把正常 C3 产品构建改为 `VOICE_BARGE=n`、`PCM=n`、`I2S_ISR_IRAM_SAFE=n`，保留按键门控、C3 独立锁和分区。正常打包不得自动引入 PCM trailer 或 IRAM 审核器，意外开启插话/PCM应被拒绝。实验专用验证函数可保留，但不能作为生产入口自动启用能力。

代码已通过原规格与独立质量复核，无阻断项。云端冻结源的64个不同预期用例已逐一通过：63项/43.016s，另1项/61.642s，无skip；前一命令包含一个类名拼错的loader error，原日志保留，不混称一次完整64项绿色调用。覆盖Opus稳定打包、三实验flag误开拒绝、原始网络输入、半双工上行门控/按键重开、实际DMA完成和微雪证据。204个输入测试后无漂移；1806个prepared文件仅C3 config及provenance变化，微雪/M5来源与配置逐字节保持。

证据在云端 `/opt/family-robot-validation/c3-button-rollback-20261101/` 的 `final-targeted.log`、`final-media-config.log`、`source-closure.json` 与 `source-closure-after-tests.json`。新原生CI结果待确认；当前实机继续使用已验收6b镜像，未安装本轮新构建。
