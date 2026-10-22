# 设备播放回报与暂停原任务：阶段验证

已实现本阶段的设备 I2S DMA 完成回报、网关播放账本和一份暂停原任务；完成任务级独立审查、主机测试和两变体目标构建。**文字位置保持 unknown，辅助 ASR 未加入，准确接续仍未验收。** 本轮未部署网关、未刷机、未读取串口或采集现场音频。全阶段交叉审查状态见末尾。

## 行为与约定

- 双向 hello 协商后才启用回报。每个实际 TTS 块有设备会话内唯一的 playback_id；工具父回复与有音频续答共享块。旧固件保持原有语音。
- 固件按实际 DMA 缓冲区身份，在完成回调中先计旧 PCM，再重填。提交、网络下发、提示音及空档不冒充服务器语音完成。中断清软件队列，旧 DMA 尾部按旧身份结算；正常 stop 等解码、软件及硬件排空。
- ISR 无动态分配、网络或日志。新增核心 host sizeof=7320，编译上限7360字节；队列/标签/额外元数据的静态估算低于16KiB目标。目标编译验证了类型和静态断言；实际堆碎片、栈高水位和 ISR 时延仍待实机。
- 可选跟踪资源或初始化失败时，销毁旧回调通道并重建普通自动清零 writer，关闭回报、恢复单声道输入。底层 I2S 本身无法建立时仍按原驱动错误处理，不声称硬件故障下可播放。
- Go 最多16个轻量播放记录和1份暂停任务。valid=false 锁定未知；错误身份、倒退和越界不更新有效记录；最终位置结算后不可重写。上游重连不丢设备位置，设备会话结束则清理。
- 只有正常封口、有效完整最终回报才能升级对应历史为 playback_confirmed；generated_text 仍表示生成稿。只生成文字而没有成功发送音频的工具续答不能借父块的确认升级。
- 原请求按上游 generation + committed item_id 归属，匹配晚到 STT；旁支及其再次中断不覆盖原任务。请求最多512 rune，截断明确标记。本地查询、放弃和替换工具独立于设备 MCP；替换建立新任务身份并保留未知位置。
- 未新增用于识别的音频尾部、录音留存或日志正文；回报不是用户活动，不延长闲置会话。详见[线协议](../protocol/playback-progress-v1.md)。

## 文字位置实验

沿用[前置固定版本核验](2026-10-22-upstream-realtime-contracts.md)中的 LiveKit `7a7547e442aaf9cf6f6cab54dd5db6f7cfb5a964` 实现，在隔离 venv 使用其 `_SpeakingRateData`。人工提供24kHz短语标注：`一、[0,9600]`、`二、[19200,28800]`、`三。[28800,48000]`；只比较“最后完整结束的已标注前缀”，不声称字中断点已知。

| 已完成采样 | 完整标注前缀 | 结果 |
| --- | --- | --- |
| 0、4800 | 空 | 短语未完整结束；4800时 LiveKit 语速插值已为1单位，不能据此断言已说了一个字 |
| 9600、19199、24000 | 一、 | 时间空档和下一短语中间不增加完整前缀 |
| 28800、47999 | 一、二、 | 最后短语尚未结束 |
| 48000 | 一、二、三。 | 全部给定短语完成 |

8个边界与7个未知分支通过：缺全部/中间标注、重叠/乱序时间、错误会话/播放身份、无效位置。该身份守卫和覆盖完整性是受控 oracle 的输入假设，不能替代产品协议入口测试；真实路由由 Go 行为测试独立覆盖。当前接入链路尚未取得可靠的词级时间标注，生产代码不启用插值或文字映射模块，text_position 始终 unknown。没有调用辅助 ASR。

实验脚本 SHA256 `6ed0c8d2bf7548d63d93c5baeaabcb000258d39c8bccea1e0474355bae837d45`，本机证据 `artifacts/device-playback-20261022/mapping-oracle-v2.log`；人工标注不代表当前千问原生提供了这些数据。

## 源码与验证

| 项目 | 来源及结果 |
| --- | --- |
| 固件源码 | `17bb1e001f9f25926071a6a8871426ae52235e4a`；初审3项生命周期/降级问题均修复并独立复审通过 |
| Go源码 | `9f02a4de15d2e67c7fdb282bd4d51d86a7cafbc5`；零音频续答误标问题修复并独立复审通过 |
| 固件主机与准备树 | 两个 CI job 各68项 firmware 测试、110项上游测试通过；实际核心、调度、初始化故障路径使用 ASan/UBSan |
| macOS Go1.25.6 | 最终完整 `go test -race ./...` 与 `go vet ./...` 通过；保留既有 Darwin cgo LC_DYSYMTAB 链接告警 |
| Linux/amd64 Go1.27.3、libopus1.4 | 最终源码 Git 归档完整 race/vet 通过；`go build -trimpath -o voice-gateway ./cmd/gateway` 成功，未部署 |
| 固件目标编译 | [实验矩阵39791587535](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39791587535) 两变体成功；[原版流程39791587538](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/39791587538) 成功 |

Go 归档 SHA256 `d1e426a665a5c75ee06cb48d9a795d2c727f930b0e1af4b82b0bdf1b67333629`；候选二进制 SHA256 `b12bd01f3e1095fd8f76ce279500d32de060b0962d2eb8335765682d66d10945`。本机目录 `artifacts/device-playback-20261022/go-9f02a4de15d2/`。

固件 CI 实际构建提交为17bb1e0，随后 Go 提交没有改固件；两者 firmware tree 均为 `ed32846d9c8d47f258c184adb7f7a7bf8ab243b7`，实验 workflow blob 也一致。分别记录源码身份，不称固件 CI 构建了之后的 Go 提交。

第一轮101cb4e CI 在 host fixture 的 GCC misleading-indentation 错误处失败，未进入目标编译；17bb1e0只修正测试分行/花括号，保持 -Werror 和 sanitizers，窄范围独立审查通过后重跑成功。Linux 额外原生验证遇到系统 libonion 预加载使 ASan 加载顺序错误；只在测试二进制进程显式先加载 libasan，未改全局配置或关闭 sanitizer，3项均通过。目标构建保留5项固定 SDK 内部 esp_wifi/wpa_supplicant 私有 include 依赖的 CMake Warning；没有将成功构建称为无告警。

## 下载产物与候选

两份 GitHub 产物各33个文件，下载 ZIP SHA256 与 API digest 相符；检查 ZIP CRC、全清单 SHA256、补丁/overlay清单及源码逐字节匹配、许可证、固定 SDK/依赖/effective config。离线 esptool5.3.1 检查 app/bootloader checksum和附加摘要、芯片/segment/入口、merged组件字节一致、分区 MD5/大小及旧备份几何。没有连接设备。

| 变体 | app字节 | app SHA256 |
| --- | --- | --- |
| 原版对照 | 2899088 | `c8236fdf41616c961654f5ea97b7fdf6f4261d63f0553f8ce745a1ae21d024a0` |
| 软件AEC与播放回报 | 2906256 | `ed30c3e02eaa21f64d0d9a7669b7e42f826212bcc3bec71fbeb053c0a9803e66` |

产物 ID 原版 `12432096173`、软件AEC `12431961684`。ZIP SHA256 分别 `ab114c4001b04143703a25c8ec2ab5c333143ca4910d3804b75ff685a095175f`、`d465f55094b582fdbea160906df2c76d159bc659f35f0f1fc7f95fc3b80f5146`。本机私有目录 `artifacts/device-playback-20261022/firmware-17bb1e0/` 保存 ZIP、提取候选和离线 verification.json；`ci-summary.json` 保存身份索引。软件AEC assets 加擦除填充与旧离线备份分区完全相等，原版对照不相等；不能因此声称当前硬件 assets/活动槽/fuse 已核验。原有备份和回退信息保留，刷机前仍需新的现场预检。

## 发布与现场边界

收尾只读核验：正式服务 active、healthz ok且0会话，仍运行 `/opt/family-robot/releases/voice-gateway-20261022T114318Z-c5a8f36/gateway`，SHA256 `cea06fab2f37d4b153a616facf732970812a45c49a16f2238a8da2fbd6a94726`。本轮源码和构建没有部署到它。

仍待提前告知后的现场窗口：刷入候选、确认真实设备回报、正常排空/中断尾部、原任务与旁支归属、音量/屏幕及软件 AEC 回归，测量 SRAM/栈和回调耗时。数字 DMA 完成不证明声学效果或人听清。准确按字词继续仍待可靠映射与现场验收；本阶段不把“有回报/测试通过”写成该问题已修复。

全阶段交叉审查：待最终审查结论；任务级审查已通过。
