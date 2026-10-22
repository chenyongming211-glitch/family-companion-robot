# StopWatch 软件 AEC 首次现场结果

2026-10-22，用户在场。**首次实验未通过音频输入检查，已恢复原程序；用户确认原版可正常唤醒和回复。自然插话尚未进入验收。**

## 刷机和恢复证据

- 在现有固件上先完成电池、Mac、独立充电器的[蜂鸣基线对照](2026-10-22-usb-awake-noise.md)，之后才通知并打开串口。
- 实读 ESP32-S3 revision v0.2、16MiB Flash；安全启动与 Flash 加密均关闭。保存现场完整 Flash，目录 0700、文件 0600，未进入 Git。
- 解析当前分区表 MD5、两份 OTA 元数据的 sequence CRC/state：第一条 seq=1、state=VALID，另一条擦除，实际选择 ota_0。启动程序、分区表、活动 app 与旧备份一致；完整 assets 与实验资源加 FF 填充逐字节一致。
- 刷入构建提交 `15fbc7367efbe635479a828d3026cbbeadaeadc9` 的实验 app，SHA256 `7d603541b631aa8e13a35af98ae6a2f7f2cf5517c5cb897c0daeda59c9e4212f`。仅写 `0x20000` 起的应用，实际擦除范围为 `[0x20000, 0x2e4000)`。
- 启动前再次读取完整 16MiB，确认 app 逐字节一致，以上擦除范围之外所有字节不变，包括 NVS、otadata、phy、启动程序、分区表、ota_1 和 assets。
- 实验进入 idle，未见崩溃标记，但 AFE 持续报告输入环形缓冲为空，未取得成功的输入处理证据；`aec_stats` 在不同 DTR 状态和换行方式下均未返回。未开启 realtime，未启用额外诊断音频采集，未进行声学或十轮插话验收。
- 随后通知并恢复现场保存的完整 ota_0 分区（4,128,768 字节，SHA256 `d836e04d703cc205797687a296f8ff4429a6010b9ed38cadfc3e8002210acc1d`）。恢复前保存当时前缀，恢复后通过下载工具从 Flash 计算的 MD5 验证旧 app、该前缀及整个 ota_1/共享资源后缀。当前配置得到保留，未回写旧 NVS/otadata，未改共享资源。
- 恢复启动进入 idle，启动观察中 AFE 空缓冲报错为零；用户现场确认可正常唤醒并回复。此项证明同槽手动恢复可用，不代表双槽自动回退。

私有证据在 `artifacts/device-field-20261022-125251/`：安全信息、完整读回、写入/恢复核验、受限控制台记录及摘要。原始控制台可能含组件输出，不上传。现场没有部署或重启网关；密码方式的服务器登录尚未重新建立。

## 诊断读取问题与最小修正

锁定 SDK 的 [USB VFS 读取实现](https://github.com/espressif/esp-idf/blob/fff9895c82d744c7237be8847347bdd1b07c6643/components/esp_driver_usb_serial_jtag/src/usb_serial_jtag_vfs.c) 在 `O_NONBLOCK` 清除时才先从轮询 FIFO 取一个字符；随后通过[驱动的可读字节计数](https://github.com/espressif/esp-idf/blob/fff9895c82d744c7237be8847347bdd1b07c6643/components/esp_driver_usb_serial_jtag/src/usb_serial_jtag.c)决定读取长度。未安装可选中断驱动时，后者为零。原诊断任务设置 `O_NONBLOCK`，因此跳过 FIFO 预取，读不到命令。

最小修正清除这个 VFS 标志，继续使用底层本来就立即返回的轮询 RX，不另行安装驱动或增加音频缓冲。回归测试编译实际 `Console()` 方法，在该 SDK 的硬件/VFS 契约边界模拟输入：修正前收到 0 条命令而失败，修正后收到 `aec_stats` 而通过；完整主机回归 65 项通过。

另在实验变体增加初始化、物理格式以及每个首次故障代次的聚合状态输出，供下一轮定位收音问题；不输出原始音频或对话。**此修正解决的是诊断读取的源码问题，未证明 AFE 收音根因，也未证明蜂鸣修复。** 新目标构建、下载检查和实机诊断回复仍待执行。
