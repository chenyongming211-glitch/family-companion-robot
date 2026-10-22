# StopWatch 软件 AEC 实验固件构建与下载验证

日期：2026-10-22。**含设备端软件 AEC 改动的实验版已经编译并完成下载后检查；没有刷机，尚无实机声学或自然插话通过结论。** 后续文档提交不改变这里记录的构建源码。

现场更新：此处记录夜间构建交付。之后已进行[首次实机试验](2026-10-22-software-aec-field-results.md)，AFE 输入检查未通过并恢复原程序；自然插话仍未通过。后续诊断修正另行记录，不覆盖本次构建身份。

## 精确源码和 CI

构建提交：`15fbc7367efbe635479a828d3026cbbeadaeadc9`（main）。它包含完成 DMA 参考、MR/AFE、观察与显式 realtime 门控、故障回退、打断清队列和有界诊断；不是此前仅用于建立基线的原版。

- [两变体实验 CI：成功](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39655807535)。同一补丁树分别构建实验开启和关闭的目标；两个 job 各通过 64 项主机测试、110 项上游测试、目标编译、依赖/配置/来源检查和产物上传。
- [未修改原版基线 CI：成功](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39655807410)。它与上面的“补丁树关闭实验”是两份不同的回归证据。
- 上游：`49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`（v2.5.2）；ESP-IDF：`fff9895c82d744c7237be8847347bdd1b07c6643`（v6.1）；ESP-SR 2.4.7、esp_codec_dev 1.6.2。原生依赖锁逐字节保持。
- 独立任务评审和完整源码评审通过；实际目标编译发现的 ISR 原子保证与 AFE 锁返回值问题已修复并复审。ISR 状态由 SDK portMUX 统一保护，未通过删除检查来假定 Xtensa 原子操作安全。CI 还修正了容器 checkout 身份检查和 SDK 原生相同配置别名重复的处理；冲突配置仍拒绝。

| 变体 | 实际关键配置 | app 大小 / 分区上限 |
| --- | --- | --- |
| `m5stack-stopwatch-software-aec` | software/device AEC 开；USB PRIMARY；offset=0 | 2,898,144 / 4,128,768 字节 |
| `m5stack-stopwatch`（补丁树） | software/device AEC 关；保留 UART DEFAULT | 2,894,912 / 4,128,768 字节 |

两者均关闭 `I2S_ISR_IRAM_SAFE` 与旧的无条件音频调试流；assets 各 2,985,801 字节，小于 8,388,608 字节分区。实验版默认只观察，realtime 需现场声学门槛满足后显式开启。

## 已下载并检查的产物

每份外层 ZIP 含 25 个文件，保留 SDK 配置、原生依赖锁、来源清单、补丁/overlay、镜像、资源、分区表、flasher 参数、原始 release ZIP、已有许可证/声明及所有文件的 SHA256。保留七天；本机另有私有副本。

| 产物 | ZIP SHA256 |
| --- | --- |
| [实验版](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39655807535/artifacts/12371643076) | `d806d680208dc8d6aee14b81a4e8b27dcad4ae48da2f29c1fd8ff727c745f9d0` |
| [关闭实验的对照版](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39655807535/artifacts/12371817691) | `6f2fb2245d34458c549880d81d83c7acc610030b645eda942ab24a08e255bf91` |

已核对 GitHub digest、ZIP CRC、每项文件哈希、精确构建 SHA、源码与依赖来源、实际有效配置。使用与 CI 相同版本的 esptool 5.3.1 **离线文件 API** 检查 app/bootloader 的芯片、段和入口范围、内部 checksum 与附加 SHA；没有调用串口命令。merged 内各组件与独立文件逐字节一致，分区表 MD5 与旧备份几何一致。

镜像实际 Flash 参数均为 DIO / 80MHz / 16MB，与旧备份镜像字段相同。候选构建关闭 secure boot/flash encryption，app secure_version 与旧备份均为 0；这不是当前硬件 eFuse 或活动槽已核实的证明。

实验 app SHA256：`7d603541b631aa8e13a35af98ae6a2f7f2cf5517c5cb897c0daeda59c9e4212f`。

私有目录：`artifacts/firmware-experiment-20261022-15fbc73/`，目录 0700、文件 0600。内含两份 ZIP、解包证据、逐项验证 JSON、运行摘要及资源比较；未进入 Git。两份镜像都不能直接用 merged/整片擦除方式刷入。

## 资源与回退准备

实验 assets 加擦除填充后，与旧备份的**完整 8MiB assets 分区逐字节一致**。对照版因资源打包顺序不同而哈希不同；逐项核对全部 24 个资源后，22 个普通资源字节相同，索引按唯一表情名称归一后语义一致，所有按模型/文件名识别的 SR 模型长度与哈希一致。原生资源头、范围和 payload checksum 均有效；没有仅凭前缀相同宣称兼容。

当前设备资源仍未读回。现场先确认其完整分区与备份匹配，再优先保留 assets；若不同，停止并复核，不顺带重写资源。没有承诺双槽自动回退。

旧备份完整性已再次验证，app/bootloader 内部 checksum/附加 SHA 有效。私有 `artifacts/firmware-rollback-20261022/` 已生成完整旧 ota_0、assets 分区及 SHA/恢复清单；不含默认 NVS、otadata、phy、bootloader 或表写入。当前活动槽、实际安全状态和下载恢复通道留到通知过的现场窗口核查。

## 尚待现场执行

按[现场验收与回退清单](2026-10-22-software-aec-field-test.md)进行：先在现有固件记录 USB/电池噪声基线，再核对设备并刷实验 app；先验证原唤醒/对话/音量/亮度/待机，再测 AEC、校准与双讲，满足条件后开启 realtime，完成至少十轮自然插话和实际扬声器停播验收。

本轮夜间没有连接串口、刷机、重启设备、采集现场音频或部署/重启网关。网关可选 `tts.stop reason:"interrupted"` 已有代码和回归，但本轮没有部署；进入真实插话阶段前需在用户知悉的测试窗口核对并更新所测服务端。

[连接电脑且唤醒时的刺耳声](2026-10-22-usb-awake-noise.md)已独立记录并完成初步源码/官方资料排查，根因仍未确认。本轮没有更改增益、充电或功放参数，也没有把未复现当成修复。
