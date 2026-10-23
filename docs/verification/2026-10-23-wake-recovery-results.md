# 唤醒来源与失败恢复：实施结果

2026-10-23，用户不在现场。本轮完成网关唤醒来源修复并上线；固件发送失败恢复已提交，双配置完整MCU构建和离线镜像核验通过。没有刷机、操作串口、播放声音或开启额外现场录音。准确接续位置仍未修复，辅助ASR未加入。

## 网关：已部署，现场复验待进行

- 源码提交：`d68ec449ce69005097f837b67a19d23a4539460d`，已推送main。后续固件提交未改变Go源码。
- macOS完整`go test -race -count=1 ./...`、`go vet ./...`通过；既有LC_DYSYMTAB链接警告保留。Linux从该提交git archive独立构建，完整race/vet通过，Go1.27.3、libopus1.4。
- Linux发布文件SHA256：`7e31686eb7d1df86a221ebac538065705eab8f081a341bc53b592e6472265533`；源归档SHA256：`b296ba8f679dd8f96af5f117f5a8cb633b9ab7b8b9d18b57bb1f3004d40ac87b`。
- 北京时间09:31在正式活动会话数为0时原子切换至`/opt/family-robot/releases/voice-gateway-20261023T013114Z-d68ec44`。systemd active、healthz ok、进程/proc/exe SHA匹配。
- 回退目录保留：`/opt/family-robot/releases/voice-gateway-20261022T154320Z-c6f2787`。部署脚本核验旧目录/旧文件、新文件/源归档和空闲状态，失败会回退；没有覆盖旧二进制。

修复按输入阶段与commit身份绑定wake来源，避免“小智→小志”导致问候占任务槽；业务音频在交接中有界排队并完整转发。确定性用例覆盖乱序/迟到、近2秒交接重放、重复/无身份ACK、manual模式及超时/超限。真实千问还暴露模式切换重发不可修改VAD时长的问题，已改成type/null；恢复后使用provider默认800ms，未声称保留原600ms。详见[网关报告](2026-10-23-wake-source-fix.md)。

## 真实模型隔离复验

机器合成输入、真实Opus/千问、静音模拟设备，不执行硬件工具。保持正式模型、音色、提示词和工具配置；设备工具只模拟发现，不能拿来证明设置了真实手表。

| 场景 | 结果 |
| --- | --- |
| 候选v2正常唤醒/数到50 | 完整1–50，554帧；唤醒转写为“小志” |
| 候选v2问候中发数数指令，再直接插话 | 问候不占槽；数数成为暂停原任务；回答数学2；查询原请求正确；继续从46开始，位置失败 |
| 精确提交Linux发布文件复验同一链路 | 问候不占槽、保存数数原请求、数学2均正确；继续从1重读到50，位置仍失败 |

精确发布文件的插话入队至网关stop为801ms，包含合成输入驱动/VAD/网络；不是喇叭停声时延。所有播放回报均为明确标记的模拟回报，不能当成真实I2S确认或现场插话验收。

对应证据：`native-v2-normal.json`、`native-v2-wake-overlap.json`、`native-exact-wake-overlap.json`及匹配的gateway元数据日志，位于忽略目录`artifacts/wake-fix-20261023/`。

## 固件：候选已提交，未刷入

- 源码提交：`1fba13b320878c277aacf1ef23f1a00abad0f12d`，已推送main。
- 四配置ASAN/UBSAN与完整72项主机测试通过；独立审查发现的提前复用编码器、旧close取消重试及组合故障遗漏AEC降级均已修正。
- 完整patch链重新prepare成功；0003补丁SHA256：`d140cb2e4751456f3f8b3788586bee2afacb8d334d9a66da22165be1559c058c`。
- [双配置MCU构建](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39884369229)：两项任务均已成功；构建、有效配置、源码provenance及上传均通过。
- [原版基线构建](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39884369216)：已成功；该独立基线不包含本轮恢复补丁。

下载的两份artifact ZIP均匹配GitHub公布的SHA256；离线检查每份34个文件的checksum、完整三项patch/overlay来源、锁定依赖、IDF commit、有效AEC配置，以及ESP32S3镜像checksum/附加SHA、分区表MD5、分区尺寸、merged各组件字节均通过。没有通过串口读设备或执行任何刷写命令。

| 候选 | app字节 | app SHA256 |
| --- | ---: | --- |
| 普通版 | 2,899,344 | `a7048a85a4520293bf1405ed64a5a99c139c1f2aaa574559381d6161ee26265c` |
| 软件AEC版 | 2,906,464 | `8ed611c897976947ed28cd8ab7e34f23bfd70b177f8ddb50110264065b6d51cb` |

对应ZIP摘要为普通版`d08c06bc89f7fbfaccd88ec3766a832f6c803fe0fc7d4c17056d6218c8cd43f6`、软件AEC版`80793ebf4bcb70fc5916a806d8cb03809b827d4db11b992ec9d4522459ea0b38`。assets与已有离线备份的整分区（补FF后）逐字节一致；本轮未读取当前设备，不能据此声称当前设备分区状态已验证。私有结果为`firmware-normal-verification.json`和`firmware-software-aec-verification.json`。

恢复逻辑停止后续上传，对仍有效的异步批次消费至EOF，再清理连接；idle/唤醒恢复排在旧close回调之后。失效批次保留原AEC降级路径。没有声称修复上游原有EOF到任务退出之间的全部FreeRTOS边界。详见[主机验证与审查记录](2026-10-23-firmware-wake-recovery.md)。

本轮没有刷机，因此手表仍按此前现场记录使用源码`17bb1e0`对应固件；这项来自既有写入记录，本轮未读设备重验。不能把候选构建或主机测试当作已经更新了手表。

## 播放文字对应：已核对能力，缺陷保持打开

实际原生事件仅提供内容身份与增量，无逐字时间戳；31.36秒音频在5.261秒内到齐，140字符在更早时刻到齐。事件到达顺序不能代替播放对齐。当前`text_position=unknown`保持，未实现新对齐器。详见[能力核对](2026-10-23-playback-text-capability.md)。

后续现场只验本轮实际改动：新网关正常唤醒/完整播放、问候中接业务后暂停原请求归属；固件候选刷入后的正常唤醒和失败重试恢复单独验收。准确接续需要可信文字映射后再验，暂不让用户反复做已知会失败的“继续数”测试。

## 收尾

09:34复核正式服务active、healthz ok、活动会话0、进程SHA与发布文件一致。6份本轮远程隔离JSON/日志对实际API key、设备token、OTA secret作内存子串扫描均未命中；不输出密钥，也不构成公开原始日志的授权。源码、测试和报告保存到main；固件、备份、音频和私有证据均在忽略目录。源码审查与报告证据抽查均无未解决的重要问题。
