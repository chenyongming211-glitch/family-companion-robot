# Waveshare 2.16 小满专用固件实施计划

> For agentic workers: use superpowers:subagent-driven-development, TDD, then independent specification and quality review. 用户已批准连续实施，直接main；不新建分支/worktree。2026-10-27用户就寝后，只做无声代码、云端测试与构建，不连接USB、播放、唤醒、刷机或重启设备。

**Goal:** 在同一锁定小智工程中新增Waveshare专用小满variant，复用18041协议和A动效，保留原M5行为。

**Architecture:** 新板型沿用BoxAudioCodec硬件MR和原驱动；新增专用DMA完成回填适配。共享现有播放账本与光球视图，板级布局和省电钩子单独连接，避免复制语音业务。目标产物必须单独核对分区，不能互刷。

**Tech Stack:** 已锁定xiaozhi v2.5.2、IDF6.1、esp_codec_dev1.6.2、ESP-SR2.4.7、LVGL9；本机仅编辑/Git，云端host与Actions目标构建。

## 1. 固定硬件契约及DMA适配

**Files:** 新增`firmware/patches/0008-waveshare-xiaoman.patch`；新增`firmware/overlays/main/boards/waveshare/esp32-s3-touch-amoled-2.16/xiaoman_box_codec.h/.cc`和可由host编译的适配`.inc`；新增`firmware/tests/test_waveshare_codec.py`及`waveshare_codec_host.cc`。

- [ ] 云端先执行RED，直接编译生产适配方法，覆盖首次已Admit输出、相同enable不重置、真实close/reopen使旧证据失效、旧代次提交拒绝、完成前不报告已播、DMA重复地址/格式/节奏异常失效、排空和重连。
- [ ] Box构造添加默认false参数`callback_refill`，仅新variant传true，创建通道时设置`auto_clear_after_cb=!callback_refill`；原Box和M5路径保持。不得改供应商组件源码。
- [ ] 新派生类保留24k输入/输出及硬件reference=true，`SoftwareReference()`仍false。TX为6个240frame描述符，16bit数据；确认后的MONO LEFT slot16/32白名单，每次回调480bytes/10ms。IDF6.1 `i2s_channel_get_info`在同一lifecycle锁内读取is_enabled/dir/mode/DMA总字节，以及mode_cfg的sample_rate/data_bit_width/slot_bit_width/slot_mask/slot_mode和驱动缓存时钟；这是驱动配置/计算时钟校验，不是物理BCLK实测，不能按初始stereo推断。
- [ ] 新类`Start()`先读持久音量，disarmed下先开Input再开Output，最终校验、HardwareReset、arm，之后才支持协议协商。`PlaybackOp::Start`在lifecycle锁内先重新确保硬件已准备，完成后才创建新记录，之后才Admit。Connect只清连接记录，不开启硬件（断连也调用Connect）。防止首轮Admit后首次EnableOutput误invalid。
- [ ] 同值Enable直接返回；真实开关先在mux下disarm并失效旧DMA，随后调用Box的原codec/电源流程，再验证/重新发现描述符。lifecycle串行提交与开关；ISR只持短mux，无网络、LVGL、动态分配或阻塞。`Complete`先确认上次DMA标签再回填，禁止使用write返回、排队量或时间估算冒充实播。
- [ ] 复用PlaybackProgress和AudioCodec协议接口。callback-refill模式下禁止esp_codec_dev_write/i2s_channel_write；注册或分配失败必须fail closed，不能退回会循环旧DMA的普通Write。记录/连接/输出代次独立，失效时报告unknown，不能放宽18041 valid保护；不恢复已暂停的自动音频故障处理。

关键host断言契约：

```cpp
// 用生产Complete/OutputTagged方法和fake I2S完成事件驱动。
// 第一轮尚未完成任何带标签DMA时：
assert(snapshot.played_samples == 0);
// 只完成已提交的那块描述符之后，才增加对应样本数。
assert(after_complete.played_samples == 240);
// 真实物理重配后旧记录不能再次变为可信。
assert(!after_reconfigure.valid);
```

## 2. 真实状态、小满唤醒和A界面

**Files:** `0008`补丁内Waveshare板文件、`main/Kconfig.projbuild`、必要的`main/CMakeLists.txt`及AFE/AudioService接点；现有`orb_audio_service.inc`和`xiaoman_orb_display.inc`做最小公共复用；相关host测试。

- [ ] 新`WAVESHARE_XIAOMAN_REALTIME`只允许2.16板及device AEC，原`STOPWATCH_DEFAULT_REALTIME`及M5门控保持。Waveshare不得伪装SoftwareReference以绕过原保护。
- [ ] 小满使用同一`xiao man`/`小满`/mn7_cn/threshold20，唤醒音频缓存、MultiNet捕获代次以及wake上传均明确支持新硬件路径；故障/断连/暂停后的旧捕获不能触发新唤醒或上行。不得把软件reference专用诊断接到硬件MR。
- [ ] 现有hardware AFE负责MR/AEC，连续收音在当前处理器实际输出后才显示Listening；关闭/重连立即收回资格。输入等级仅取本轮处理音频，Speaking仅取真实DMA完成，不能依赖文字或网络包。
- [ ] 两板编译同一A绘图及视图源；可通过板无关include/类型别名共享现有文件，避免为目录清理改写M5算法及测试。Waveshare480×480动态布局，沿用128×128/32KiB缓冲、既有小表、静态/5fps/目标15fps降级；不将其当实机fps或功耗结果。
- [ ] Waveshare CustomLcdDisplay接Setup/Refresh，保留原panel、touch、PMU、充电参数；backlight0及省电暂停动画，待机停帧。默认无聊天字幕，保留系统错误/配网页优先级和完整状态文字。

## 3. 专用variant、分区和证据

**Files:** `0008`补丁内Waveshare`config.json`；新增分区overlay；`firmware/tools/verify_experiment.py`、相应tests及`.github/workflows/firmware-experiment.yml`。

- [ ] 新variant命名`esp32-s3-touch-amoled-2.16-xiaoman`。保留现有三个M5 variant，CI显式映射board目录，Waveshare新target使用USB console且软件AEC=false、deviceAEC=true。
- [ ] 候选专用16MB分区如下。构建必须验证实际app不超过3MiB、assets不超过6MiB；不满足则停止发布并调整布局，禁止截断或套M5布局。

```csv
# Name,Type,SubType,Offset,Size
nvs,data,nvs,0x9000,0x6000
otadata,data,ota,0xf000,0x2000
phy_init,data,phy,0x11000,0x1000
ota_0,app,ota_0,0x20000,0x300000
ota_1,app,ota_1,0x320000,0x300000
assets,data,spiffs,0x620000,0x600000
storage,data,spiffs,0xc20000,0x300000
```

- [ ] 验证器按variant分别核对board/AEC/默认实时/console/小满/分区文件与模型资源；共享SDK、依赖字节、provenance、包内产物校验继续严格，不为新variant放宽旧项。
- [ ] 实际解析partition-table.bin及应用、assets大小/地址、不重叠；生产构建证据必须绑定精确Git SHA。完整备份与原分区哈希已存在私有目录，不能提交Git。

## 4. 静默验证与提交

- [ ] 从锁定archive重新prepare新tree，云端运行以下命令（无需provider或音频设备）：

```sh
python3 -m unittest discover -s firmware/tests -v
python3 -m unittest discover -s scripts/tests -v
```

- [ ] 保存新增RED→GREEN与全套实际exit code；独立规格PASS后做独立质量审查，发现问题修复再复查。
- [ ] 提交推送main，核对远端；Actions构建原3个M5及新Waveshare，下载/核对来源、SDK、分区、资源及应用大小。Actions及host不操作实体设备。
- [ ] 更新交接、版本/产物/限制与精确未完成项。**本轮静默停在已核验产物，不刷机。** 用户回来后重新核验备份、写入范围、otadata目标与回读方案，保留NVS/phy/storage，再做单台及双台实测。
