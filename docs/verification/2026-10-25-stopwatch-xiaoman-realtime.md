# StopWatch小满唤醒与Go Realtime接入验证

2026-10-25。3f530fa已完成目标构建、最小分区刷写和实机接入；真人已确认“小满”可唤醒，临时开启实时上行后可打断并回答新问题。54ec169已固化开机实时意图和唤醒后同代AEC就绪启动，并完成刷写、自动启动与真人插话复验。**识别率/误唤醒/长稳等仍未全面验收。**

## 实机与恢复基线

USB串口确认设备为ESP32-S3 rev0.2、m5stack-stopwatch-software-aec、xiaozhi2.5.2、IDF6.1；旧实际唤醒模型为wn9_nihaoxiaozhi_tts。Secure Boot及Flash Encryption均未启用。

已完成只读16MiB整片Flash备份，SHA256为fb874ba1f7e96f8536739b5cb812937e8b40e2bff80f3b89a8d6842239c4ddb7。备份位于忽略的私有目录，未提交设备NVS、凭据、原音频或固件二进制。分区表MD5及当前app校验通过。

实际分区：nvs=0x9000/0x4000，otadata=0xd000/0x2000，phy=0xf000/0x1000，ota_0=0x20000/0x3f0000，ota_1=0x410000/0x3f0000，assets=0x800000/0x800000。有效otadata为seq1/stateVALID，当前选择ota_0；另一OTA槽未确认有效。默认只写兼容的新app和配套assets，不改bootloader、表、NVS、otadata或phy；恢复时app与共享assets必须成套恢复。

## 已完成的软件检查

- Go实现a5a4e54：同一Session核心新增鉴权小智入口；云端全仓race为620运行、618通过、0失败、2个原有opt-in长测跳过，vet/build通过。主代理对13个变更文件与测试源码逐文件SHA256核对一致。
- 固件b5c58e0，构建配置修正3f530fa：独立m5stack-stopwatch-xiaoman variant、MultiNet7中文量化、真实拼音xiao man/显示小满；原两个variant保留。
- 最终整合0001至0005后，云端78项固件主机测试通过，含18个MultiNet生产方法场景；上游110项脚本测试通过。云宿主缺少python别名，使用仅测试PATH中的python3链接，不改全局Python。ASan仅为临时测试ELF调整preload顺序，未关闭sanitizer或修改系统loader。
- 独立审查发现的P2已闭环：AFE fetch的原始capture代次显式传给MultiNet，追加partial、detect前后校验；Store hook覆盖只改变capture、同时改变capture/wake两种RED/GREEN。复核未发现新增P1/P2。
- 第一次CI在目标编译前被上游规则拒绝：板型配置不能直接加入CONFIG_SR_WN_*；3f530fa移除多余旧WakeNet开关，并增加回归断言。资源选择继续由USE_CUSTOM_WAKE_WORD控制，不能通过跳过上游测试掩盖配置冲突。

以上不证明ESP32目标ABI、模型实机创建、两字识别率、误唤醒、耗电或AEC声学效果。

## 云端部署与Mac回归

Go a5a4e54已部署到family-robot-realtime-output-go.service，PID1128374，ready=true、busy=false、NRestarts=0。18040仍只回环；同一进程设备专用18041已监听，只注册/xiaozhi/v1/，复用受保护的现有设备token及允许MAC。公网验证：未鉴权接口401，/devices及/pair均404；云端端口放行已由用户完成。不存在把Mac配对/登记接口直接公开的变更。

服务端原二进制、unit及server.env备份只留云端/opt/family-robot-livekit-runtime/backups/watch-20261025-a5a4e54。8000的OTA下发地址已改为ws://82.156.48.111:18041/xiaozhi/v1/，在无活动会话时重启，PID1145530；受鉴权OTA探测确认token保持且没有强制固件升级。18041只对协商playback_progress_v1及playback_drain_v1的新固件放行。

Mac原生合成输入＋真实模型/CoreAudio回归完成4轮，1次排空＋3次全播，客户端fixture_success、无错误事件；服务端同一会话4段输入、4次首包/播放、1次取消，无生成/收音/排空错误。终端和会话归属一致。该fixture退出仍按transport_closed记录，不能伪称人工client_stop。既有用户窗口保留。

## 3f530fa目标与实机验证

GitHub Actions运行40179629072的原版、软件AEC和小满三个variant全部成功。小满产物35个文件SHA256全部通过，输入补丁/overlay与本地源码一致，SDK精确commit及ESP-SR2.4.7符合锁。真实资产只含mn7_cn/fst，命令xiao man、文字小满、阈值0.2；不是只修改显示名。

分区表与完整备份逐字节一致。app为2,907,744字节，小于0x3f0000，SHA256为3e0ff5fffffcb98168eb31b02e3e45850a6c8eb28e366af0c243bf96a79f8c54；assets为5,376,385字节，小于8MiB，SHA256为e5010f4259498b29ccd6147bd9b2c3800faea3766b1ee8c591cf90365c9dbee4。app的校验和与附加hash有效，IDF6.1、ESP32-S3、secure version0符合基线。

只写ota_0和assets，芯片校验通过，并实际读回两个文件逐字节比对通过。启动前读回0至0x20000，与旧备份逐字节一致，确认bootloader、表、NVS、otadata和phy未被刷写改变。未使用merged镜像盲刷。

实机加载mn7_cn，MultiNet打印Command1:xiao man，AFE实际detector为MultiNet。AFE_CONFIG仍有“wakenet model not found”通用提示，随后正确创建未启用WakeNet的AFE并成功识别，不能据此误判MultiNet初始化失败。合成“小满”一次触发概率0.632；真人确认能唤醒，不把一次合成触发当作长期准确率。

手表鉴权接入同一个a5a4e54核心，归属为xiaozhi，重连device_id保持、session_id改变。真实i2s_dma的finished与同ID sent目标相等，多轮全播完成。

## 首次真人插话失败与定位

用户反馈“播放时插问，不能打断或没反应”。旧软件AEC实验在构造中默认kAecOff，监听模式为auto，Speaking只保留本地AEC观察并关闭上行；服务端因此未收到播放期间新问题。这是端侧默认模式缺口，不能归因模型不支持取消。

主代理打开串口排查时触发USB_UART_CHIP_RESET并结束一次现场连接，已告知用户；随后保持一个串口控制连接，不反复打开。使用原控制台临时开启aec_realtime on前，实机eligible1/fault0/rx_overruns0，phase190us；32秒有界私有诊断参考均有效，记录56个诊断队列drop、MR流42个缺包，不能把该采样声称为完整无丢包。500ms生产者时间分箱观察有声段AEC输出明显降低，但两流未做声学时序对齐，**不是严格ERLE或全面声学验收**。

临时实时模式开启后，真人明确反馈“可以打断并回答新问题”。该轮9段输入的快照中5次打断均收到真实DMA排空终态，等待设备回报144/165/181/239/259ms；这不是话尾至新回答的完整时延。未见生成、收音或排空错误。此现场结果用于确认硬件路线；下面的54ec169另行验证开机默认行为，原两个variant默认Off不变。

## 54ec169默认实时固化与最终复验

0006只给小满variant设置CONFIG_STOPWATCH_DEFAULT_REALTIME=y；构造保存实时意图，唤醒缓存发送完后先恢复processor、保持上行关闭，再以实际AFE输出携带的capture代次通知主任务。主任务复查Listening、channel epoch、同capture与RealtimeEligible；Off、故障、断连或取消退休等待。2秒deadline由1Hz clock检查，实际超时约2–3秒后安全Auto，发送前再次检查故障。没有沿用暂停前的ready或放宽原有参考/故障保护。

云端87项固件host及110项上游脚本全通过。审查发现并修复旧flag0显式On后被新selector降级的P2，新资格守卫完整隔离到小满flag；最终审查patch SHA256为c176816320658a6ee055a752d0b0efb6592e7b01757fffeee9b31c69487f41d7，未发现剩余可确认P1/P2。主代理核对58个固件文件与云端测试源码一致。GitHub Actions运行40203674306的三个目标variant全部成功。

54ec169产物36文件SHA256及输入/SDK/配置校验通过。app为2,908,736字节，SHA256为1f9b161b52f9255a52486ea63591367e9e74d629028c58689ce5c4ae857d03d8，镜像校验和/附加hash有效。assets与已刷入并读回的3f530fa资源逐字节相同，仍为e5010f4259498b29ccd6147bd9b2c3800faea3766b1ee8c591cf90365c9dbee4，因此本次只写ota_0。刷写前另备份当前启动/配置区域；app实际读回相同，启动前0至0x20000与本轮刷前快照逐字节相同，包含更新后的Wi-Fi NVS。

用户在本轮更换无线网络。通过设备官方配网热点的/submit和/exit完成，未重写NVS分区，保留设备身份及OTA地址；口令通过内存输入，不放源码/Git。Mac临时连接设备热点后恢复原网络，手表重启获取新网段DHCP地址并完成云端激活。

最终实机启动后未发aec_realtime on。合成“小满”成功触发，Listening后约220ms日志为realtime_start ready capture=4 channel=1，确认新一代AEC受控启动并自动实时上行。stats为eligible1/fault0/RX溢出0、phase188us。新会话仍归同一xiaozhi终端，session_id更新。

最终现场同时有合成与真人输入，不能把混合记录当成单一受控语料。一次合成故事插问触发实际I2S排空261ms，但该问题及随后一段转写为空、回复“嗯，我在听”；较高播放增益的数学复测识别完整并回答五。未据此调整DSP或声称空转写问题已根治。随后停止自动播音，用户单独测试并明确反馈“正常，能打断并回复新问题”。一份17段输入快照中16次真实播放终态、9次interrupt，无显式生成/收音/排空错误；空转写是体验限制，不能用无错误事件代替识别准确率。

## 后续体验验收与回退

两字唤醒的噪声/误触发、弱音/空转写、双讲、待机/续航与长稳仍需扩大测试；已通过的现场插话不能替代这些维度。故事重复、具体文字断点等模型语义问题沿用18040既有边界，不夹带新提示或硬拦截。

本轮默认模式固化的首选回退是已验证3f530fa app＋相同assets，仍连接18041，按需临时开实时模式。若回退到原始“你好小智”固件，先恢复云端备份server.env中的8000下发地址并空闲重启family-robot.service，再恢复原app＋配套assets；保留当前NVS与私有整片备份，不能仅换回旧app却继续强制走要求新能力的18041。

设备start后目前没有新增服务端专属空闲/最大会话时长；依赖设备stop/close。暂不接入旧设备工具、判题、提醒、长期记忆或声纹。playback依据、模型生成稿和实际用户已听内容仍分开记录。
