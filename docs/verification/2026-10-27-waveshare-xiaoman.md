# Waveshare 2.16 小满接入记录

## 固件共用边界

同一锁定源码工程、不同板级目标，不能互刷应用或merged binary。

| 项目 | 原M5 StopWatch | Waveshare 2.16 |
| --- | --- | --- |
| 屏幕 | 466×466 | 480×480，CO5300 |
| 音频参考 | 已实现的软件参考 | ES7210硬件参考输入，BoxAudioCodec |
| 输入/输出 | 原板级适配保留 | 24kHz输入/输出，ADC逻辑双声道 |
| 界面 | 保留已安装A/e61f079 | 共用A绘图/视图，单独接屏幕和电源钩子 |
| 分区 | 原M5布局不变 | 原NVS为24KiB，assets仅3MiB，需专用资源布局 |

18040/18041已支持两路独立会话，见[服务端验证](2026-10-27-realtime-multi-device.md)。Waveshare专用源码v3已完成实现、云端回归与独立规格/质量复审，均PASS；目标构建尚未完成，不能把服务端可鉴权或host通过当作实体接入通过。

## 已核实的驱动契约

源码锁定xiaozhi v2.5.2、IDF6.1、esp_codec_dev1.6.2及ESP-SR2.4.7。官方组件源码commit `af7b72fb2b73d4f513d3cede01c13518ab216735`的I2S适配在output channel=1时转为MONO LEFT；RX工作时TX为data16/slot32，单独TX可能slot16。不能按Box构造初始stereo推断最终DMA格式。

`esp_codec_dev_open/close`不能证明底层每次重配都成功：组件部分set_fmt/enable返回值未向上传递。IDF6.1公开`i2s_channel_get_info`可取dir/mode/is_enabled、DMA总字节、缓存时钟及mode_cfg；需在同一生命周期锁内读取，以免其内部配置指针并发变化。这属于驱动配置/计算时钟校验，**没有物理BCLK或I2S信号测量**。

官方IDF依据：[i2s_common.h](https://github.com/espressif/esp-idf/blob/fff9895c82d744c7237be8847347bdd1b07c6643/components/esp_driver_i2s/include/driver/i2s_common.h#L94-L115)。组件及SDK源码只下载到云端独立研究目录，没有安装Mac运行环境。

适配需要关闭callback后的自动清零、禁止与普通write混用、先确认旧DMA完成再回填。首次硬件准备必须早于Start/Admit，新播放记录建立后才初始化会误伤首轮；Connect在断连也调用，不能借Connect无条件开启硬件。真实重配必须撤销旧证据，不能将未知尾音作为新一代已播。Box原Read失败仍返回samples，新opt-in路径还须拒绝失败缓冲，避免当作真实收音。

ESP-SR2.4.7组件页面指向commit `2f8c4b0459db5bbb39abd77adae27962d6d94bcb`。根代理另行核对其[AFE公开接口](https://github.com/espressif/esp-sr/blob/2f8c4b0459db5bbb39abd77adae27962d6d94bcb/include/esp32s3/esp_afe_sr_iface.h#L161-L175)：enable/disable返回`-1=失败、0=关闭、1=开启`，不能按通用`ESP_OK=0`解释为AEC已开启。驱动参考有效、AFE开启成功和当前处理帧就绪需要分别判断。

## 实现与云端软件验证

新`esp32-s3-touch-amoled-2.16-xiaoman`目标采用硬件MR专用Box派生类，保留软件参考false。现有Box新增默认关闭的callback所有权和checked-read选项；只新variant启用。ISR回填及播放账本复用，内存/注册/格式/读取故障不退回普通write。Start先准备Input→Output并校验驱动，之后才能建播放记录和Admit；Connect只处理连接/采集代次，不开启硬件。正常暂时关闭可协商既有callback能力，Start负责恢复硬件，故障则拒绝。

独立`ManagedCapture()`职责用于采集代次、上行、AFE、已冻结唤醒批次与就绪通知，默认仍等于原SoftwareReference；Waveshare不伪装软件参考，也不实例化M5诊断。A动效与视图仍是同一源码，只增加480布局和新板电源钩子；原466常量值不变。Waveshare沿用内部降帧策略，未接M5专属USB诊断命令。

云端独立目录`/tmp/xiaoman-waveshare-20261027/impl`，最终32文件manifest SHA256 `b3ce0aeaa9ebd23ced2131b7aca8d406fc646395d283c2f5f6a35d2bc3a0d2bb`。根代理和规格审查分别核对本机/云端字节一致。

- 严格prepare从校验过的锁定archive应用全部补丁/overlay，退出码0；独立重新prepare的1771个源码文件与待审tree一致。
- 最终host 103项，48.864秒；上游脚本110项，7.402秒，实际退出码均0。保持ASan/UBSan，旧runner仅增加既有按binary设置系统ASan预加载的约定，没有关闭或放宽检查。
- 新增生产codec生命周期、驱动readback、DMA完成前0/完成后才计样本、格式/节奏/地址故障、旧代次/重连、临时关闭后协商、硬件MR就绪和480布局回归。
- v1的102项通过后，独立规格探针发现两处真实遗漏：AEC关闭仍假ready、公开唤醒批次检查绕过硬件MR。失败证据保留，v2将reference有效与AEC启用结果分开、补ManagedCapture的最终批次检查，并处理显式Off→On不能依赖尚未出现的ready。原探针重新提取生产方法后两项预期均变为0，退出码0。
- 新AFE运行用例直接编译生产控制/ready方法及真实header，覆盖enable1/0/-1、disable0/-1、旧capture、Wake/Off/On与M5原策略。独立规格复审另跑8项，4.663秒、exit0；最终SPEC PASS。

主机使用明确的I2S/Codec替身。上述证据不证明物理MR连线、真实DMA调度、AEC听感、帧率或功耗。完整RED/早期环境错误与最终GREEN、独立复现均在云端私有目录保留，未把失败重写为通过。

## 质量修复v3

独立质量审查确认正常双关闭后，AudioService重新采集只调用`EnableInput(true)`；原新适配未开输出，导致参考和实时ready不能成立，首次实时等待2秒后降级Auto。v3只修改专用codec及两个host用例：真实输入false→true时在同一生命周期内依次准备IN、需要时OUT，再Arm；同值enable不重置，Connect不开硬件，已锁存故障不自动恢复。

该判断以锁定IDF的ESP32-S3实际`SOC_I2S_HW_VERSION_2=1`和官方1.6.2 data_if为依据。审查早期使用HW1分支得到的“关闭输入误判pending RX”的候选不适用于该SDK，已撤回，原RX启用状态校验未放宽。

v3 manifest SHA256 `f97cb9904f5ecb9370c1d944cd458cfd82375b43518c7725b980e04d582a75c4`，32文件。根代理核对本机/云端全部哈希，较v2仅上述3路径变化。fresh prepare、最终103项host/51.997秒及110项上游/10.606秒，实际exit0。正常电源重开生产用例RED→GREEN，质量复现后置条件现为IN1/OUT1/reference1/appliedAEC1/realtime1，且尚未调用PlaybackStart；官方HW2及原规格探针也重编、运行通过。

最终规格增量复审PASS，独立重跑9项/5.735秒、exit0，1771份重新准备源码一致；质量复审PASS，独立按真实HW2关闭的两种顺序各循环3次，18项故障/代次回归24.227秒、ASan/UBSan与实际exit0。Connect、same-enable、旧代次拒绝、当前AEC处理帧资格及fault锁存保持。审查报告和94份既有日志的hash核对均保存在云端。以上为源码阶段结论，随后进入实际目标编译，未刷写。

## 实际目标编译

固件提交`e2cab214479b9ae2757e948013f7a1b22501938b`已推送并核对远端main。[首次四目标构建40374961290](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/40374961290)中M5标准版成功，Waveshare job121755389629实际报错：共用display include调用`LvglFont::font()`，Waveshare编译单元只见类型前置声明，缺少完整字体头。主机替身测试未覆盖这一真实头依赖，不能把此前host通过当作目标编译通过。

最小修复只在Waveshare小满条件块显式包含`display/lvgl_display/lvgl_font.h`；其余为补丁hunk行号调整。严格fresh prepare及独立规格重新准备1771文件一致，与v3相比仅该include和生成provenance变化，静态依赖检查RED1→GREEN0。此低影响编译修复不新增镜像实现的单元测试，以新的实际四目标重建验收；不关闭编译警告或放宽类型检查。修复后产物和尺寸仍待新构建结果。

## 备份与静默边界

第二台原16MB完整Flash备份16777216字节，SHA256 `ff002860300f98b805e2c2b2046a8a73b16fc433d45dfb54dc06abf8986e3aa0`；备份后原固件已恢复启动。原NVS0x9000+0x6000、phy0x11000+0x1000、storage0xc20000+0x300000需保留。备份、设备凭据和产物均不进Git。

静默阶段仅重读已有备份并复核全文件SHA：其中otadata第一扇区头序号为1、状态为`0xffffffff`，第二扇区头全擦除。该元数据来自备份，不是新的实体读取，也未单独证明其CRC/当前启动选择。新旧ota_0地址不同，未来安装仍须重新核对选择、分区表、目标应用及资源，不能据此直接app-only刷写。

候选布局和测试步骤见[板级计划](../superpowers/plans/2026-10-27-waveshare-xiaoman-firmware.md)。只有新目标实际构建、应用/资源/分区核验后才能确定可安装包；不能把候选布局写成已安装。

用户2026-10-27要求睡眠期间静默：本轮禁止任何USB连接、播放、唤醒、刷机或设备重启，只进行代码、云端测试、编译、审查和Git。当前原M5仍e61f079、Waveshare仍原固件，实机收音/AEC/播放/插话/重连、双设备并发、实际帧率与功耗全部保持未验收。
