# 两个USB终端的固件兼容性核对

日期：2026-10-26。用户要求核对当前接入的两个ESP32是否都能刷小满光球固件。

## 结论

不能直接给两台刷同一个StopWatch应用或整包。两个设备虽然都是ESP32-S3、8MB PSRAM/16MB Flash级别，但实际板型、现有分区及资源布局不同。小满身份、语音协议及A柔软流体代码可以复用；第二台需要板级适配、独立目标编译与迁移验证。

| 项目 | 当前手表 | 第二个设备 |
|---|---|---|
| 当次USB端口 | `cu.usbmodem14101` | `cu.usbmodem14201` |
| 实际启动SKU | `m5stack-stopwatch-xiaoman` | `esp32-s3-touch-amoled-2.16` |
| 已运行软件 | 基于小智2.5.2的3ddbbb5 | 原小智2.2.4 |
| 硬件/容量证据 | 已核验目标配置、原始启动8MB PSRAM及设备分区 | esptool识别S3、8MB内置PSRAM、16MB Flash；启动SKU确认Waveshare板型 |
| 当前应用区域 | `ota_0`在`0x20000`，容量`0x3f0000`；另有`ota_1` | `factory`在`0x20000`、6MiB；`ota_0`在`0x620000`、3MiB |
| assets区域 | `0x800000`起、8MiB | `0x920000`起、3MiB |
| NVS/otadata | NVS`0x9000`起16KiB，otadata`0xd000` | NVS`0x9000`起24KiB，otadata`0xf000` |

当前小满模型资源约5.13MiB，超过第二台原assets的3MiB容量。直接套用第一台的分区表也会改变NVS/OTA布局，不能保留原配置的假设不成立。迁移前必须对第二台单独完整备份、确定配置保留方式、模型资源与分区方案；不能复制第一台NVS或设备身份。

第二台启动记录还显示AXP2101、CO5300、BoxAudioCodec及`Display: Power management not supported`。这些是原固件自报，不等于所有硬件细节均已实测；后续应以板级配置和实机核对屏幕、供电/触控、音频I2C/I2S引脚及省电行为。锁定上游源码中已有`main/boards/waveshare/esp32-s3-touch-amoled-2.16/config.json`目标，不能拿M5目标替代。

## 本次实际操作与边界

仅对第二个设备执行USB枚举、被动串口读取、esptool芯片/Flash识别和`0..0x20000`只读配置前缀备份，随后启动它原有固件以读取SKU。未刷写应用、分区表、NVS或模型资源；它已恢复原固件并进入配网模式。原始日志、设备标识及二进制备份仅在本机忽略目录`.superpowers/esp32-second-20261026/`，不入Git、不上传云端。

当前18040/18041测试入口另有独立限制：`internal/realtimelive/xiaozhi_entry.go`仅接受一个配置MAC，`Server.busy`在设备与Mac会话间共享单会话槽。因此，两套固件即使各自可运行，也不能据此宣称两台已获准接入或能同时通话。多设备鉴权、各自会话与记录隔离应单独实施，本轮未改后端。

本轮继续给原StopWatch实现已选择的A动效；第二台适配不与当前手表的A视觉验收混为一项。
