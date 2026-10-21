# StopWatch 原版固件构建基线验证

日期：2026-10-22。固件构建对应仓库提交 `8953c44007057102c00487796ed0457cc617e63d`（`main`）。后续验收文档更新不改变此次编译的固件源码。

## 构建内容与结果

- 本记录对应的编译是固定上游小智 v2.5.2 的原版 StopWatch 固件，未包含自然插话的设备端改动；后续实验交付另行记录。
- 源码：`49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`；ESP-IDF v6.1：`fff9895c82d744c7237be8847347bdd1b07c6643`。
- 目标：`esp32s3`；板目录：`m5stack/stopwatch`；构建变体：`m5stack-stopwatch`。
- 19 项源码准备测试、110 项上游主机测试、Linux 编译和 10 个必需文件检查通过。
- 依赖锁确认 ESP-SR 2.4.7、esp_codec_dev 1.6.2。
- [成功构建及产物](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39636558961)。

## 下载后校验

- ZIP SHA256 与 GitHub 返回的 digest 一致，ZIP CRC 正常；包含恰好 10 个预期文件。
- app 和 merged 两个二进制的 SHA256 与构建生成的 SHA256SUMS 一致。
- app 2889376 字节，小于 OTA 分区 4128768 字节。
- assets 2985801 字节，小于资源分区 8388608 字节。
- 分区表 MD5 正常，几何布局与现有离线设备备份一致。

| 文件 | SHA256 |
| --- | --- |
| `build/xiaozhi.bin` | `5ec93f730515363e827f130185e3f04aba5d3378d003ebd34361b19b478fb5cc` |
| `build/merged-binary.bin` | `a8d7142957d7434e90993419d6f4d576559a8a54e40014d4a2d119792b026902` |

产物和详细校验记录保存在 Git 忽略的 `artifacts/firmware-baseline-20261022-8953c44/`。GitHub 上的 Actions 产物保留七天。仓库已由用户改为公开；新的公开状态上传测试及完整固件产物上传都通过。未删除其他项目的历史产物。

## 验收边界与后续工作

此次只确认原版构建基线和产物完整性。没有打开串口、刷机、重启设备或部署服务端。整合镜像用于离线检查；后续保留 NVS 的刷写步骤需按分区制定。

音量、亮度等现有修复属于 Go 服务端。新增可选 `tts.stop reason:"interrupted"` 已提交并通过网关回归，尚未部署；这轮 CI 没有编译 Go 网关。

后续设备端软件参考、AFE 与打断集成的构建/下载检查见 [软件 AEC 实验交付记录](2026-10-22-software-aec-build.md)；声学与自然插话仍待现场验收。原版构建通过不代表自然插话通过。

用户已说明先休息：继续已确认的代码、构建和自动测试；刷机、重启及现场语音测试留待用户次日在场。
