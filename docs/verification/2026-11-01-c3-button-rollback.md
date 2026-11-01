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

证据在云端 `/opt/family-robot-validation/c3-button-rollback-20261101/` 的 `final-targeted.log`、`final-media-config.log`、`source-closure.json` 与 `source-closure-after-tests.json`。

## 原生 CI 与实际产物核验

精确源码 `977024333df985362fa261fc5cabae44d0dadefa` 的六个原生构建目标均完成并返回 success：[五目标 experiment](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41292919595) 与 [stock](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41292919618)。

| 目标 | Job | 结果 |
|---|---|---|
| C3 稳定按键版 | 124807715725 | success |
| M5 原版 | 124807715572 | success |
| M5 software-aec | 124807715410 | success |
| M5 小满 | 124807715665 | success |
| 微雪小满 | 124807715608 | success |
| stock M5 | 124807715559 | success |

C3 job 的主机测试共 280 项/202.822s，结果为 `OK (skipped=1)`；唯一跳过项需要精确旧 `ee4249d` 产物进行历史负对照，不属于稳定按键版验收。上游 110 项/7.257s 无 skip 通过，随后原生编译、链接、正常构建验证及打包通过。故障注入测试输出中的 `assets.bin exceeds 100 bytes` 不是本次实际构建失败。

实际下载 C3 artifact `13078201329`，名称为 `stopwatch-patched-xmini-c3-family-977024333df985362fa261fc5cabae44d0dadefa`；ZIP 为 5,398,575 字节，SHA256 为 `064cb55b82cc2042083dce505c53305fa98c930247b0b4d08796b1e9f06d8a89`。云端独立检查确认：

- ZIP 大小、SHA256、CRC、安全解包及逐文件一致性通过；`SHA256SUMS` 的 107 个文件全部匹配，清单无遗漏。
- 构建证据绑定上述精确源码、`xmini/c3`、IDF 6.1.0/`fff9895c82d744c7237be8847347bdd1b07c6643`；依赖锁、补丁及 overlay 摘要匹配。
- 实际 `sdkconfig` 与有效配置一致：按键入口为 y，语音插话、PCM、实验 IRAM 均为 n；Kconfig 未出现的布尔项按 n 解释。
- 编译输入使用原始 `esp-ml307` 网络源码，摘要和编译记录一致，没有切换到实验网络源。
- 五项正常 Flash 输入的偏移、大小和摘要，与合并镜像每段及填充字节逐一匹配；发布 ZIP 只包含同一合并镜像。无 PCM 提示音包或 `0x650000` trailer 写入项。

新 app 为 2,408,576 字节，SHA256 为 `f0b27da0ce20c42af270a676fcff5f0c03b48748176e97cc68aff1ed19fd41ea`。云端私有产物与独立核验记录在 `/opt/family-robot-validation/c3-pcm-product-20261101/task5-go-build/artifacts-9770243/`，包括 `xmini-c3-family-download-proof.json` 和 `xmini-c3-family-independent-proof.json`。

当前实机继续使用已真人验收的 `6b79e4c` 稳定镜像，未安装本轮 `9770243` 新构建；新产物的声学验收仍为 pending。此次完成默认构建与产品策略收口，不追加刷机、服务部署或真人试说。

## 后续用户授权实刷：当前为 9770243

用户在报告连续说话中随机爆音后要求先刷固件，随后两次明确要求不备份、不保留旧固件。本节更新此前“实机仍为 6b、未安装新构建”的状态；爆音根因和听感改善仍未确认。

最初启动的整片读取按用户要求中止，尚未进入 Flash 写入，未形成完整备份。随后使用已独立核验的同一 `9770243` 产物直接更新：先清空 app0 并写入新应用，selector0 选择 seq5/NEW；首次启动确认 seq5/VALID 后，清除旧 app1 全部 `0x310000+0x2f0000` 及 selector1。当前仅新 app0 为有效启动应用，不再把已清除的 6b/app1 当作板上回退路径。

- 新应用 SHA256 `f0b27da0ce20c42af270a676fcff5f0c03b48748176e97cc68aff1ed19fd41ea`，实际 ELF `164697e9fc73b20251f279b6dca0152f8cf79e5ace703371ea862b735fcda563`；固件为 C3 Opus 按键模式，PCM/语音插话关闭。
- 应用设备 MD5、实际 selector、旧 app1 全区擦除及 selector1 擦除核对通过。修改范围仅两应用分区及两 OTA selector；bootloader、分区表、PHY 分区、原音频资源和其余 Flash 的设备 MD5 保持。
- 安全身份及 eFuse 读回保持。NVS 未写回；设备启动仅自行更新 `phy/cal_data`，其余逻辑记录、类型和库存保持，含 Wi-Fi、身份和音量。
- 首次联网激活/idle 后观察 15.004 秒，清除旧应用后的最终启动再观察 15.205 秒，均无捕获故障；串口已关闭。未更换服务或微雪/M5 固件。

私有实刷证据：`.superpowers/c3-button-rollback-20261101/usb-direct-977-20261101T095718Z/result.json` 及两份启动日志。本地使用固定 esptool 4.8.1；最初误选 5.1.0 时止于离线镜像检查，未接触 USB。前一有备份安装库的云端 15 项安全用例通过，仅证明该库的合成测试；最终按用户要求执行的是直接更新流程，不能把这些用例混称为无备份流程的全套回归。

本次新固件未加入爆音专项修复，也未追加真人声学验收。下一步在此版本上比较音量及连续播放，区分大音量失真、供电与缓冲断续；不恢复 C3 自然插话路线。
