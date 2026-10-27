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

18040/18041已支持两路独立会话，见[服务端验证](2026-10-27-realtime-multi-device.md)。**当前Waveshare已运行2978a1a，用户确认“小满”唤醒并回答正常，后台5轮播放回报一致。此前把AFE短轮询的空缓冲警告直接判为不能对话，证据不足，现撤回该判断；详见本文末节。用户已确认3次连接结束均为主动按键/触屏操作，按主动结束验收；长稳/完整插话和双设备声学验收仍待完成。原M5保持A/e61f079。** 下方保留构建、安装、诊断过程与误判记录，以最新真人验收为准。

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

字体修复`50f21a27552e510a874c26e9f239af92563c7a19`已推送，[第二轮40376949650](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/40376949650)三个M5目标全部成功；Wave job121762488626通过字体编译后，实际在链接时报专用codec构造/析构未定义。日志证明该`.cc`已编译，但其头在读`sdkconfig.h`前就检查配置宏，导致独立编译单元的实现为空。已在条件前补SDK头，并将现有host runner的Wave宏改由生成的SDK配置提供；新增直接编译真实生产`.cc`再链接调用者的RED→GREEN、宏关闭对象无专用符号检查。

打包预核另独立调用锁定上游命名helper，确认Wave ZIP名会加`waveshare-`厂商前缀；原裸variant模式匹配0且会接受错误裸名。校验器改用显式四variant canonical映射，保持唯一文件、错误前缀/裸名/多版本拒绝，wire身份不变。此项在实际打包前确认，不能记为第二轮已进入打包或已生成Wave产物。

合并5路径修正的manifest SHA256 `68b7207af865a1e3648eea0c8001bcc0d430649494d2e90b534c8c935cd3af76`。fresh prepare及源码比对只有SDK头这一行和provenance变化；20项codec/AFE/M5+Wave打包相关回归3.638秒、exit0。增量规格复审PASS，独立20项3.679秒；质量复审PASS，独立4项1.618秒，均实际exit0。下一次实际四目标构建及最终镜像/分区/大小核验尚待完成。

## 最终四目标与产物核验

最终固件提交`411c26db7dfa0d04f780f6cbd2d9493ddd6fdcef`已推送并核对远端main。[Actions40380042158](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/40380042158)四个variant全部成功，另一个未修改上游的[stock workflow40380042542](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/40380042542)也成功。每个目标均实际通过107项host、110项上游测试，再完成ESP32-S3编译、链接、严格配置/来源校验及打包。此前一轮software-aec任务的两个布局测试曾在慢运行器超过20秒，失败日志保留；最终四目标相同覆盖全部通过，没有放宽超时、关闭sanitizer或删除断言。

| variant | host / 上游秒数 | 应用字节 | artifact ID |
| --- | --- | ---: | ---: |
| m5stack-stopwatch | 31.962 / 4.305 | 2900976 | 12679121811 |
| m5stack-stopwatch-software-aec | 47.967 / 6.968 | 2908112 | 12678982307 |
| m5stack-stopwatch-xiaoman | 50.183 / 7.198 | 2919248 | 12679590042 |
| esp32-s3-touch-amoled-2.16-xiaoman | 46.128 / 5.941 | 2903024 | 12678597234 |

全部四份下载ZIP的SHA256与GitHub digest一致，ZIP CRC通过；每份52文件校验和与清单完整性通过，共208文件。云端独立核对包内补丁/overlay、锁定上游/SDK/依赖、source SHA和有效配置，重新解析小满索引及mn7_cn/fst模型。最终32源码输入清单SHA256为`eaffeaedebc6e79ae2268fdd70a8ffffc7db54d7823b228c1f963c7822364c29`。

四套应用和bootloader共8份镜像均经云端esptool5.3.1离线检查，chip ID9/ESP32-S3，checksum和validation hash各自valid，无invalid/error。应用均为IDFv6.1、secure version0。canonical release ZIP中的merged镜像与独立merged文件逐字节相同，merged内bootloader、分区、应用、资源四段与独立产物一致。M5三个分区表与原锁定16MB布局一致；Wave分区二进制MD5、专用CSV、边界及写入地址均独立核验。

Waveshare应用`2903024`字节，SHA256 `0db48239f931646d278ff9fb0782504e2de369ac07340c6985173cbfa8e959fb`；3MiB槽仍余242704字节。ELF SHA256 `ba385c37bc70c38a8c04b186b3daa04e73855a37d15fcba04543499f629dadc8`。资源5376385字节，SHA256 `e5010f4259498b29ccd6147bd9b2c3800faea3766b1ee8c591cf90365c9dbee4`，小于6MiB，且与原M5小满资源一致。分区表SHA256 `c065c05db78105e292cfd08e55be5a2fe1d2036a4e488d5f9d487d66ab40668c`。

分区定义保留Wave原NVS `0x9000+0x6000`、phy `0x11000+0x1000`和storage `0xc20000+0x300000`；ota_0为`0x20000+0x300000`、ota_1为`0x320000+0x300000`、assets为`0x620000+0x600000`。**没有写入设备。未来安装需逐区迁移并保留NVS/storage，不能直接写整个merged镜像。**

云端完整证据：`/tmp/xiaoman-waveshare-20261027/artifacts-411c26d/`，含下载digest、inspection-summary和final-integrity-proof。本机待验包：`.superpowers/waveshare-20261027/artifacts-411c26d/esp32-s3-touch-amoled-2.16-xiaoman/`，复制后52文件再次逐项哈希通过；所有生成物均已被Git忽略。静默阶段未使用USB、唤醒、重启、刷写或播放测试音，实机AEC/收音、真正I2S回报、两机隔离/并发、画面呈现/帧率和功耗仍未验收。

## 备份与静默边界

第二台原16MB完整Flash备份16777216字节，SHA256 `ff002860300f98b805e2c2b2046a8a73b16fc433d45dfb54dc06abf8986e3aa0`；备份后原固件已恢复启动。原NVS0x9000+0x6000、phy0x11000+0x1000、storage0xc20000+0x300000需保留。备份、设备凭据和产物均不进Git。

静默阶段仅重读已有备份并复核全文件SHA：其中otadata第一扇区头序号为1、状态为`0xffffffff`，第二扇区头全擦除。该元数据来自备份，不是新的实体读取，也未单独证明其CRC/当前启动选择。新旧ota_0地址不同，未来安装仍须重新核对选择、分区表、目标应用及资源，不能据此直接app-only刷写。

候选布局和测试步骤见[板级计划](../superpowers/plans/2026-10-27-waveshare-xiaoman-firmware.md)。只有新目标实际构建、应用/资源/分区核验后才能确定可安装包；不能把候选布局写成已安装。

用户2026-10-27要求睡眠期间静默：本轮禁止任何USB连接、播放、唤醒、刷机或设备重启，只进行代码、云端测试、编译、审查和Git。当前原M5仍e61f079、Waveshare仍原固件，实机收音/AEC/播放/插话/重连、双设备并发、实际帧率与功耗全部保持未验收。

## 用户返回后的安装与现场问题

用户明确“可以刷机了”，仅操作第二台Waveshare USB 14201，原M5 USB 14101未打开、复位或写入。现场再次确认S3 rev0.2、16MB Flash、8MB PSRAM、安全启动与Flash加密均关闭。重新读取完整16MB，SHA256仍为`ff002860300f98b805e2c2b2046a8a73b16fc433d45dfb54dc06abf8986e3aa0`。第一次读取在part11串口NoData后停止，未写入；512KiB分块补齐后全部实际exit0，与先前完整备份逐字节一致。

现场otadata第一扇区seq1/state0xffffffff及CRC均有效，第二扇区擦除；保留它后新布局选择ota_0 0x20000。事先保存写入intent，仅写bootloader 0、分区表0x8000、app 0x20000和assets 0x620000，不整刷merged、不写NVS/otadata/phy/storage。esptool写入实际exit0后，在未启动应用状态以32个512KiB分块重新读取完整16MB，每块exit0。四个payload逐字节相同、扇区尾均为FF，四个擦除范围之外所有字节与刷前相同；四个保留区hash也完全不变。刷后整片SHA256为`46ba34b314203006f0093b8f5035256f585dc82862ab37428e3bb08b5a72129a`。只有这些检查通过后才首次启动。

第一次esptool复位后直接os.open被动读取55秒为0字节，不能据此判断启动失败。随后显式115200串口配置捕获到了正常SPI启动、ELF `ba385c37b...`和正确专用SKU，屏幕/触摸/Box codec均完成初始化；没有据此把零日志改写成固件崩溃。一次只读诊断命令误用了esptool5不支持的read-reg，CLI解析即exit2，未操作设备。

通过设备自身配网HTTP页面核对实际端点，保存用户已授权的无线网络；Mac随后恢复原无线。第二次临时连接热点未找到设备，HTTP探测失败后恢复原网络，未提交配置；后续重试成功。只在进程内存转交设备OTA引导地址，未复制server.env、provider key或设备token，不输出或保存引导秘密。GET回核OTA地址一致，提交/exit响应丢失但串口明确记录退出配网、获取DHCP、连接8000、Activation done及idle，故不重复提交exit。之后配置写入来自设备自身正常NVS行为，不属于刷写回读阶段。

资源checksum与mn7_cn/fst加载、注册“小满”均成功，AFE创建为MR双通道。但首次配网后持续AFE fetch failed/empty ring，正常复位亦复现（90秒观察累计3835条空缓冲/fetch警告）；两处i2s disable未启用错误也保留。根因尚未确定，不能称硬件MR、唤醒、播放或插话已通过，也不将该问题归因于模型或放宽校验。新增专用codec任务侧驱动readback/首故障来源及DMA大小/间隔诊断，ISR仅记录标量、不打印，保护和故障锁存策略保持。

诊断版在云端`/tmp/waveshare-install-diag-20261027/`完成fresh prepare、107项host（56.957秒）及110项上游（11.351秒），实际均exit0；另以ESP_PLATFORM分支编译运行生产codec harness，严格printf格式检查及ASan/UBSan通过。首次host执行在SSL下载等待时中断；改用已复核同一锁定SHA的缓存archive。第二次因scratch漏复制CI文件而1项FileNotFoundError，补齐原文件后完整107项通过；上游初次6项因系统无python命令而exit127，切回云端现有venv PATH后110项通过。未改测试、覆盖或保护阈值。诊断版实际目标构建与实机定位待完成。

本机私有证据在`.superpowers/waveshare-20261027/install-20261027/`，备份、产物、网络口令和原始设备记录均不入Git。实机界面、AEC/收音、真正DMA回报、双设备语音/并发和功耗继续未验收。

## 诊断版已构建，等待设备重新接入USB

诊断提交`20e9fcd5181d999ea074e0dc24b5d8384e01dbf3`已推送并核对远端main；[Actions40432451449](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/40432451449)四个目标全部成功。四份下载ZIP的GitHub digest和CRC均通过，208文件逐项SHA、源码输入、SDK/配置/模型/分区再次核验。8份应用/bootloader的checksum及validation hash均valid；四套canonical release ZIP中的merged与独立merged相同，四个镜像段也逐字节相同。

Wave诊断应用2903792字节，较已安装411c26d增加768字节；SHA256 `f9576e858c912b5269c7fb265000d1ea8dcb564a16b86978315cd0164cab571f`，ELF SHA256 `8d86062ee83b40035d8d397e8c541b73e2f295894db75940f66a6d0b165c9208`。assets与分区表SHA保持`e5010f42...`/`c065c05d...`，无需再次迁移布局。云端产物及核验在`/tmp/xiaoman-waveshare-20261027/artifacts-20e9fcd/`；本机专用包已复制至`.superpowers/waveshare-20261027/artifacts-20e9fcd/esp32-s3-touch-amoled-2.16-xiaoman/`，复制命令实际exit0，52文件重新计算全部一致。诊断只增加任务侧日志与ISR标量，不修改保护阈值，也不是故障修复或实机通过。

准备保存联网后的完整备份时，USB枚举已没有Waveshare，脚本在串口打开前KeyError退出；仍仅原M5存在。已向用户请求重新插线并确认开机，未收到恢复确认，未再写入或重启任何设备。原板有60秒省电/300秒关机及初始启用的计时逻辑，但本次USB消失是否由自动关机引起尚未证实，不能写成用户拔线或确定根因。

后续步骤已准备在私有目录：`diag-backup.py`按目标序列号选择端口、完整备份当前16MB并检查只有配置/storage变化；`diag-install.py`复核52文件和正确source、分区/资源与当前备份相同、otadata CRC/选择，仅写app，再完整16MB回读，确认应用擦除范围外所有字节相同后才标记可首次启动。**尚未执行后续备份或安装，当前设备应用仍411c26d。** 重新连接后须先确认目标和无串口owner；诊断启动观察器需使用新的post-verified和实际枚举端口，不能盲用旧normal-boot脚本中写死的14201或旧安装proof。

本轮最后云端18040为build6dc191a、ready=true、max_sessions2、active_sessions0；未部署或重启云端服务。原M5未访问或刷写。根因、收音/唤醒、播放、插话、显示、双设备声学验收及功耗继续未完成。

## USB恢复后的首轮实机诊断

用户确认重新连接并开机，现场再次匹配同一Wave USB序列号，只有根代理持有该串口。联网后的完整16MB新备份SHA256 `274f51fa2937e10033e08a7718d23e1b7db54e806f6154761cbe1466c1b7ec4e`；应用仍为411c26d，其余变化仅配置/storage区域。52文件、专用source、当前分区/资源、otadata CRC/选择及安全状态复核通过；预存intent后仅写20e9fcd app，实际exit0。启动前32个512KiB分片完整回读均exit0，app逐字节相同、末尾擦除填充值正确、应用擦除范围外所有字节与本次刷前相同。刷后整片SHA256 `7ed1ba1e213e4c27754d95c730ad3e845ab9553d1e866cea9d7ff2e9fd8d0049`。

首次启动观察45秒，ELF `8d86062ee...`匹配、网络与8000激活正常，仍累计1801条AFE空缓冲/fetch警告。硬件readback为IN1/OUT1，TX STD/mono/data16/slot32/left、总DMA2880B；RX TDM/stereo/data16/slot16/mask3/total4、总DMA5760B；两端24k/BCLK1536000/MCLK6144000均符合。首轮未出现capture blocked日志，但该日志原来位于InputData内部，不能排除第一次ADC读取未返回而错过随后ISR故障，不能把“未打印”当作无故障。

增加仅Wave任务侧的第一次底层输入read开始/结束（只记样本数/代次，不记PCM），并由AFE调用的ReferenceValid独立打印一次锁存故障；ISR仍不打印。两项生产codec及独立翻译单元回归2.560秒exit0，另ESP_PLATFORM真实日志分支严格格式编译/ASan/UBSan运行exit0，保护与阈值未变。新诊断目标构建与实机定位待完成。当前再次只读备份目录为本机`.superpowers/waveshare-20261027/diag2-input-20261027/`，尚未完成或写入新诊断；源设备20e9fcd，原M5/云端保持原状。

## 第二轮实机诊断与管线追踪

补充提交`ee6e15ef05c1228cee56ad0a7ab39ef756a2f4ac`经[Actions40456296242](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/40456296242)四目标通过；四下载digest/CRC、208文件、源码/配置/资源/分区、8镜像checksum/hash及merged/四镜像段一致性全部通过。Wave app2904224B，SHA256 `57f5427af72b7f79e0d3c7539e0617ebf2b64b995cc412e5cbd6698a184665e3`，ELF SHA256 `c8173e88dede09be753b8d63ce757696781bb15f20c86f12dc4b0d71bb0bb5dd`。本机ZIP与GitHub digest相同，解包52文件逐项相同。assets和分区仍与已安装版本一致。

刷前再完整备份，整片SHA与20e9刷后`7ed1ba1e...`完全相同。安全状态、otadata及intent核验后只写app，实际exit0；完整16MB回读、新app/尾部与应用擦除范围外全部字节核验通过后才启动。新整片SHA256 `cbd2191d473d76bfaced5b7c3d7687a50e321fb517614d0368cfa371a7dc0273`。45秒ELF正确、网络激活正常，1811条AFE空缓冲/fetch；首ADC读480样本返回ok1，capture2与返回时current2相同，读前TX DMA480B、间隔10039us、最近回调age602us。AFE独立读取首故障未打印，不能当作对整个读取生命周期或原始声音的证明，但足以确认首次读返回与当时回调存活。

继续追踪后续读取、重采样、AFE FeedCaptured门控及reset/feed入口，新增0009补丁仅在Wave＋ESP_PLATFORM编译有限次数任务日志；不记录PCM、不在ISR打印，不改控制/故障策略。codec首读日志扩为前4次。当前新诊断的fresh prepare通过，107项host/51.241秒及ESP_PLATFORM日志分支严格编译/ASan/UBSan运行实际exit0，目标构建与安装待完成。设备已再完整只读备份至本机`pipeline-diag-20261027/`，与ee6刷后整片完全相同，现保持下载模式，避免等待期间原板关机；原M5未操作。

本机HTTPS Git曾直连超时/HTTP2 framing错误，经远端head核对仍旧提交后才切换传输；临时本地SOCKS19087经既有云端SSH连接，Git最终用本机SSH443认证成功推送，GitHub API和fetch再次核对ee6 head。没有复制GitHub凭据到服务器，也未复制服务器Key到Mac；不是本地语音服务。原始传输失败日志与全部设备证据均在忽略目录，后续仍需实际定位并完成真人/声学验收。

## 管线实测、误判纠正与真人验收

管线诊断提交`2978a1a82173a72613408953d482743bea431ce4`已推送；[Actions40458115930](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/40458115930)四目标全部成功。四ZIP digest/CRC、212文件、源码/配置/资源/分区、8镜像checksum/hash以及canonical merged/四个镜像段全部独立核验。Wave app2905712B，SHA256 `de573b60ab30e123378d2cf8712943ffab63cf15f9e4d9323e741bb806cd4cfc`，ELF SHA256 `5f23590744809e1de5ad95ce82156ca6a36d66525f140411440f7ea1ad5fff97`。本机ZIP digest及解包53文件复核通过，资源/分区与设备一致。

当前ee6再完整备份仍为整片`cbd2191d...`；仅app写入实际exit0、完整16MB回读通过，新app相同、尾部FF、应用擦除范围外所有字节不变后才首次启动。新整片SHA256 `ef83e4d483398a405c380ca61280108b0ece155e9bba32e8a7a7a906f53d9534`。私有安装/日志在本机`pipeline-diag-20261027/`，设备无NVS/分区/资源重写。

实机日志已确认：连续底层读取成功、capture2稳定，24k双通道240帧经重采样返回成功/160帧，320个交错样本进入FeedCaptured；启动重置后pending清除，AFE feed收到1024个交错样本并实际返回。20ms的fetch轮询比512/16k=32ms的帧周期短，空缓冲提示可以与正常帧处理并存。**此前仅凭这些提示就宣布收音故障/不可对话，是主代理诊断过早，现明确撤回；本轮没有证明需要放宽保护，也没有实现所谓硬件故障修复。** 45秒仍记录1823条空缓冲/fetch警告，但随后实际语音成功，不能把警告计数当作丢失帧数或不可用次数。后续判定要结合当前帧/唤醒/上行/回复/实际播放证据。

用户随后明确确认“能唤醒并回答”。设备日志可见小满唤醒、WebSocket握手、`realtime_start ready`和Listening→Speaking→Listening。云端2026-10-27 06:12:02—06:13:25 UTC同一Wave身份建立3会话，共5轮用户转写、5次输出及5份最终播放回报；165份进度/完成回报均为progress/finished，没有unknown。五次sent/completed分别43200/43200、125280/125280、217440/217440、46080/46080、280800/280800；两次drained的排空记录为125ms和69ms，其余terminal=played。没有模型错误事件。上述排空是协议事件记录，不是物理声学停声延迟，也不代表完整插话验收。

三次会话最终均有transport_closed code1006/transport_lost，设备侧TCP read失败后回idle，随后可重连再回答。用户随后明确确认“我主动按键或触屏结束”，因此这3次按本轮主动结束对话记录，不计为设备自发掉线。1006仍保留为传输层原始标记；用户确认操作原因不等于协议执行了优雅关闭，也不作为长期稳定通过的证据。本轮主动结束后重新进入对话并回答已得到设备/服务端记录支持。当前声学结论仅为用户确认单设备唤醒与回答，界面观感/功耗、完整插话与续讲、轮流/并发双设备及同房间双唤醒仍待各自验收。原M5未打开串口、重启或刷写；云端服务仍6dc191a，未部署新后端。有限启动/管线诊断代码保留在测试固件中，不记录PCM；USB被动观察已结束，未主动结束会话、关机或复位；最后实测时可正常唤醒/回答。06:19 UTC收尾枚举再次只剩M5，Wave USB消失原因未知，不据此宣称仍开机或擅自归因。继续测试须先确认开机/USB状态，不再因这些短轮询提示重刷。
