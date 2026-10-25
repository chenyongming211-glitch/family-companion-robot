# StopWatch小满唤醒与Go Realtime接入验证

2026-10-25。当前为开发验证记录，**目标构建、刷写和实机声学验收仍在进行，不能据此宣称手表已接通或可靠唤醒**。

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

服务端原二进制、unit及server.env备份只留云端/opt/family-robot-livekit-runtime/backups/watch-20261025-a5a4e54。当前尚未修改8000的OTA下发地址，旧服务PID2261506保持。18041只对协商playback_progress_v1及playback_drain_v1的新固件放行。

Mac原生合成输入＋真实模型/CoreAudio回归完成4轮，1次排空＋3次全播，客户端fixture_success、无错误事件；服务端同一会话4段输入、4次首包/播放、1次取消，无生成/收音/排空错误。终端和会话归属一致。该fixture退出仍按transport_closed记录，不能伪称人工client_stop。既有用户窗口保留。

## 待完成

1. 三variant目标编译及产物source SHA、精确SDK、依赖、实际mn7_cn/fst资产、分区和镜像校验。
2. 在无活动会话时切OTA下发地址，保留原8000 WebSocket回退。
3. 通知用户后最小分区刷写并读回校验，检查实机模型创建、xiao man命令注册与真正唤醒。
4. 实机连接18041并复用18040核心，核对普通收放音、真实i2s_dma进度/finished、取消后尾音排空、重连归属。
5. 真人两字唤醒、噪声/误唤醒、AEC插话和待机验收。软件AEC的realtime开关不得仅凭构建或eligible状态强开。

设备start后目前没有新增服务端专属空闲/最大会话时长；依赖设备stop/close。暂不接入旧设备工具、判题、提醒、长期记忆或声纹。playback依据、模型生成稿和实际用户已听内容仍分开记录。
