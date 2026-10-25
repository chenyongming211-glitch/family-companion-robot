# 小满唤醒与StopWatch接入Go Realtime

2026-10-25，用户已批准开发：USB实机核验→接18040→对齐播放/排空→按需二开，重点把真实唤醒词改为“小满”。不再为已授权步骤逐次询问；串口、复位和刷写前通知用户并先保存私有恢复备份。

## 已核实基线

USB /dev/cu.usbmodem14101、Espressif 303a:1001；串口启动确认m5stack-stopwatch-software-aec、xiaozhi2.5.2、Oct22 2026固件，AFE实际为wn9_nihaoxiaozhi_tts。不是只凭仓库目录推定板型。当前Go18040运行f65e19b、单会话、回环Mac协议；旧8000提供受鉴权OTA/WebSocket。

## 唤醒方案

保留锁定小智v2.5.2、IDF6.1、ESP-SR2.4.7和既有软件AEC/播放回报，增加独立小满variant。使用MultiNet7中文量化模型，真实识别拼音xiao man，显示/协议文字小满。原WakeNet variant保留作为回退。不能只修改DISPLAY后宣称改词成功。

软件AEC分支统一使用AFE缓存和已有带batch/generation的唤醒编码worker。MultiNet不能继续绕开这一保护；捕获代次改变时由fetch owner清部分输入和模型状态，检查模型创建/命令注册/更新结果，失败明确禁用该候选。保留原唤醒音频、软件AEC与故障降级语义。精确两字是否被模型接受以实机初始化为证；识别率、误触发与耗电以真人/噪声/长稳测试为证。

## 设备入口与归属

同一个Go进程/Session负责Mac和设备。18040继续仅回环，新增可配置的设备专用18041监听，仅公开鉴权后的/xiaozhi/v1/，不公开/devices、/pair或/live。复用云端现有设备token和允许MAC认证，凭据不复制到Mac代码或日志；经过认证后在私有voiceidentity登记稳定xiaozhi终端，所有连接新session_id，记录与intent归属保持。

既有8000只调整OTA下发的WebSocket地址到设备专用入口，保留旧WS作回退；配置备份只在云端。已有设备OTA bootstrap与NVS/Wi-Fi凭据保留。单会话准入在Mac/设备之间共享，不能两端同时占用。

## 协议和播放

适配小智hello及v1/v2/v3 Opus封装；上行16k单声道20/40/60ms分别解码320/640/960样本，进入同一云端VAD/原生Realtime链路。唤醒缓存有界隔离，listen.start后才处理业务收音。

下行仍24k/60ms；begin映射tts.start+playback_id，音频按WS顺序归属；end/drain映射保留队列的seal式tts.stop。新增双向playback_drain_v1协商，不支持时明确拒绝进入新路径。固件本地语音abort在该能力下只通知取消，不清解码队列、不提前拒收封口前尾包；显式关会话仍按关闭清理。旧能力路径保持。

Mac回报保留CoreAudio依据，设备只接受真实i2s_dma。原始I2S样本保留，推进窗口时保守向下取完整1440样本包；没有accepted字段时只以已播量作为接收下界，不能以sent冒充设备已收。正确会话/播放ID、valid、非清队列中断且finished等于同ID封口目标，才放行下一回复。新生成、已下发、已播继续区分。

## 实施与验收边界

main顺序提交推送，不建分支或worktree。固件准备/主机测试/目标编译与Go运行测试在云端；Mac只运行代码/Git及USB备份/刷写、测试客户端。备份、凭据、原音频、固件二进制放忽略的私有目录，不进Git。

刷写前读取安全状态、完整Flash及实际分区/OTA选择，校验恢复资料；默认不改bootloader、表、NVS、otadata、phy。新增MultiNet资产若改变共享assets，须核查并备份对应分区，组合校验后才重启。没有兼容证据不能仅刷app或盲刷merged。

云端覆盖鉴权、9种输入组合、真实依据/错误回报、排空竞争/超时、唤醒隔离和Mac回归；固件主机测试覆盖协商隔离/队列保留/代次清理，目标构建核查锁/配置/资产。实机验收分为启动与词注册、唤醒、连18040、普通收放音、真实I2S回报、插话/AEC和待机。软件通过不能替代声学/两字准确率验收；既有AEC实时开关仍须按现场证据启用，不绕过其保护。
