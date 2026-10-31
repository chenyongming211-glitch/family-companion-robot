# Xmini C3 按键接入验证

当前状态：**已安装并接入；用户确认两轮回答完整、按键可结束，后台确认两轮真实I2S DMA完整播放及C3独立归属。** 云端121项固件主机测试、上游110项脚本测试、五目标编译及下载核验、USB安装/回读、指定Wi-Fi、激活、实际OTA有效及重启确认均通过。用户选择按键开始连续问答、暂不要求播放中语音插话，并选择使用微雪当前的同一Wi-Fi。网络资料和设备凭据不写入此文档。

## 硬件与回退基线

- 只操作当前唯一匹配的C3：ROM确认ESP32-C3 revision0.4、16MB Flash；原小智2.2.6启动确认SKU `xmini-c3`、SSD1306和ES8311。照片含OLED、外接喇叭及电池，早先按纯主控裸板理解的说法已纠正。
- 完整16MB只读备份在本机私有忽略目录，长度16777216字节，SHA256 `368ad810be59003c2e6a29d739525b1d8b9e46b3145797af6c6e82427c036129`，读取由stub MD5校验。原数据未上传云端或Git。
- 分区表MD5通过：NVS `0x9000+0x4000`，otadata `0xd000+0x2000`，PHY `0xf000+0x1000`，ota_0 `0x20000+0x2f0000`，ota_1 `0x310000+0x2f0000`，assets `0x600000+0x200000`。OTA有效项seq1/state2/CRC匹配，当前ota_0；ota_1头部为空。
- 后续128KiB前缀回读SHA256 `72403469e95d11e41c40b5508c5bfa22983015c4667a010bc5e9d762659781e9`。Flash Encryption/Secure Boot关闭，VDD_SPI_AS_GPIO既有位只读确认为true。本轮没有eFuse写入；产品variant屏蔽上游的自动烧写。
- 产品使用独立CSV保留上述布局及上部8MiB内容；app/assets超过原容量即拒绝。是否可安装由最终目标镜像决定，不先迁移原分区。

## 软件实现与依赖

`xmini-c3-family`复用锁定上游Xmini C3板型与Go18041产品核心。新增独占TX DMA的ES8311播放适配，完成回调才累计实播；输入不运行S3的AFE/软件参考、MultiNet或唤醒词。播放期间的录音不发送，排空后恢复聆听。OLED保留原有状态页，不迁移彩色光球。新设备采用独立鉴权、记录和近期记忆。

C3 native依赖来自[独立配置检查41166592527](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41166592527)，精确源码提交 `5da9b0739f11cf62048c1b6e1860b1e3d25e1ebc`，IDF提交与仓库锁一致。该检查没有编译或批准可安装固件。C3锁SHA256 `56d45cdedf90210afd8f32090b078384a12b63a09f4ccb401b09b94451444141`，62组件，S3为74；59个共有组件版本和哈希全部相同，包括ESP-SR2.4.7、esp_codec_dev1.6.2和IDF6.1.0。目标规则新增servo-controller/mdns/servo依赖并移除15个不适用于C3的依赖，没有新增产品舵机功能。S3锁未改。

## 已执行的云端验证

运行在服务器独立目录，不在Mac执行固件主机测试或启动语音服务。最终主机测试记录为 `host-suite-v6.log`：

```text
Ran 121 tests in 75.754s
OK
optimized: C3 production adapter: 57 scenarios passed; ordinary writes=0
sanitized: C3 production adapter: 57 scenarios passed; ordinary writes=0
```

C3驱动真实生产方法的O2、ASan/UBSan及独立翻译单元开/关宏编译链接通过。覆盖实际完成与排队区分、DMA地址/格式/节奏、队列反压、封口、迟到代次、连接复用、关闭重开、旧DMA清理、内存/回调/驱动故障及RX边界。固定SDK源码确认C3为I2S HW2、mono LEFT16/24k、6×480B DMA、BCLK768000/MCLK6144000。

主机测试还覆盖C3专用依赖字节/target、错误板型/AEC/唤醒配置、分区/MD5/地址/容量和image chip5拒绝；保留原M5/Waveshare的生产方法回归。主机fixture的镜像头为合成数据，不当作真实目标镜像验证。

原NVS含旧MQTT资料，产品C3现在固定选WebSocket，宏关闭时保留其他板型原逻辑。实际生产选择片段穷举四种OTA配置组合：旧逻辑在C3预期下先失败，修改后C3/原板两种宏均通过。产品默认OTA URL为空，首次启动前私有配置产品引导地址；验收同时拒绝自动开启Secure Boot、Flash Encryption或anti-rollback烧写的配置。

## 审查发现与修复

1. **短回答丢失所有后续DMA回调时无限等待。** 原生产方法复现为240样本提交、首次填充后不再收到ISR，5秒后仍valid/busy。新增仅对待播PCM生效的200ms任务轮询心跳：失效回报、立即静音PA，再用非阻塞生命周期锁关闭codec。原复现已转为fault/无声/停止TXRX/无效终态，长闲置不误触发。驱动独立规格、质量复审均通过。
2. **TTS开始或断线期间的迟到重开。** 实际StartListeningAudio在SendStartListening期间被接收任务关闭后，旧逻辑仍无条件打开上行。现关闭总是撤销旧票据，仅当前未变的闭合revision可重开；同时核对当前协议播放阶段和物理排空。真实方法回归先失败、修复后通过。
3. **快速start/stop合并UI事件后无法恢复收音。** 原真实六方法复现显示state=listening、processor=running而gate=closed，exit17。C3聆听分支现在把closed gate作为重新启动条件，经同一协议/排空/票据路径恢复。O2和ASan/UBSan均先失败后通过，独立复核gate=open/listen_starts=1/exit0；S3路径由宏隔离。
4. **首轮目标任务被上游配置测试拦截。** [07426d4的五目标任务41169123513](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41169123513)均在目标编译前报相同两项：C3板级配置重复默认16MB选项、板级不允许指定`CONFIG_SR_WN_*`模型。移除冗余默认项，将关闭默认模型交给仅C3使用的构建参数`--wake-word disabled`；其他四目标参数不变。修正后云端新制备树的上游110项脚本测试通过。服务器没有`python`别名，首次运行的6项环境错误通过任务私有目录中的`python → python3`解决，没有修改系统或上游测试。

发送路径已按固定网络组件源码核对：SDK Send错误直接返回，断开回调由接收任务触发；未发现已证锁反向。主机测试与静态审查不能替代真实网络、单核负载、供电和声学验证。

## 精确目标产物与USB安装

[最终五目标Actions41169657971](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41169657971)在精确提交`6b79e4c014c36ee30817f0f186d50e2d502f736a`全部成功，IDF仍为仓库锁定提交。下载C3及四个旧variant的官方ZIP均核对GitHub SHA256/长度、ZIP CRC、包内完整SHA256清单、patch/overlay与当前源码逐文件相等、native lock精确字节、镜像checksum及附加SHA256、release ZIP与merged一致；C3分区表与原备份逐字节一致。

| variant | app字节 | assets字节 |
|---|---:|---:|
| xmini-c3-family | 2197184 | 269881 |
| m5stack-stopwatch | 2900976 | 2985801 |
| m5stack-stopwatch-software-aec | 2908112 | 2985801 |
| m5stack-stopwatch-xiaoman | 2919248 | 5376385 |
| esp32-s3-touch-amoled-2.16-xiaoman | 2905712 | 5376385 |

C3为2.5.2/IDF6.1、image chip5，最小修订0.3，当前硬件0.4满足；app SHA256 `435bd4810dfbb60c5c5eaf1fdfcdc117ad8068ae261a79a90e67d9f8ee0f4bbd`。2197184字节小于原`0x2f0000`槽，资源小于原2MiB分区。构建成功不代表实体声音或声学效果已通过。

Wi-Fi来源核对微雪USB变更计划、NVS摘要、回读和联网/激活证明；只复用指定网络及产品引导入口，C3使用服务器新生成的独立token。私有NVS工具限定16KiB、唯一Active页追加六个已有键，不GC、不改namespace/key inventory，其他逻辑记录逐字节核对。13项云端合成测试通过，覆盖物理page2 Active、CRC、类型、空间、重复更新及错误输入；没有上传真实Flash/NVS。

刷前再次读取当前前缀、安全状态与完整备份对应区域的stub MD5。实际安装范围（均按4KiB补齐）为：bootloader`[0,0x4000)`、NVS`[0x9000,0xd000)`、otadata第二sector`[0xe000,0xf000)`、新app1`[0x310000,0x529000)`、资源`[0x600000,0x642000)`。保留原app0、分区表、第一份有效OTA记录、PHY和上部8MiB。第二sector最后写seq2/stateNEW/CRC，选择ota1；未整刷merged。

写入前静态复核发现官方esptool4.8.1 Namespace漏`verify`参数；补`verify=False`后，云端抽取实际官方`write_flash/_update_image_flash_params`以合成stub复现缺参数失败、补齐后五段写入/MD5/不启动finish通过。USB安装后所有改区实际字节回读相等，所有未改complement范围stub MD5与刷前相等，安全状态及eFuse寄存器保持，再首次启动。**不是安装后完整16MiB再次读取**；证据为改区回读和未改区MD5。

## 正式登记与首次启动恢复

登记前Go09d22e6、ready/记忆就绪、active0/max2，8000健康且active0。候选保留两个原设备条目/秘密，生产registry loader的26项鉴权/隔离检查通过。首次新固件启动确认SKU、ELF摘要、OLED、ES8311和指定Wi-Fi联网；短时读取C3引导请求的Device-Id/User-Agent及Client-Id摘要，确认实际身份和原board.uuid不变，不保存原HTTP请求。

首次原子替换登记文件漏保留属主，导致以`family-robot`用户运行的8000服务报`device_auth_registry_invalid`；自动回退内容时同样漏属主。已立即恢复原登记内容的服务账号属主和0600权限，8000健康恢复；加载工具随后保留UID/GID，并在替换前以服务账号实际读候选，再空闲加载成功。Go二进制仍09d22e6，其他五个保护配置摘要保持，原两个设备条目不变，max_sessions仍2。加载后三设备各自引导/正确token鉴权、交叉token拒绝和未知设备拒绝共14项全部通过；这些探针不创建语音会话或模型请求。

首次激活尚未完成时关闭/重开原生USB串口触发USB_UART_CHIP_RESET，OTA保护回退到原2.2.6 app0，实读第二记录为seq2/ABORTED4；没有把旧应用当新固件成功。旧应用还重排了已存Wi-Fi槽位。云端恢复后，先核对新app/资源/bootloader、原app0/上部8MiB摘要及四个产品连接键，再用同一已测工具恢复指定主网络、回读NVS和选择sector；其他记录保持。首次重选的只读检查因发现网络槽位变化而停止，没有额外写入。

重新启动后1.933秒确认指定Wi-Fi、2.5.2、新SKU/ELF、ota1、Marking firmware as valid、Activation done及idle；随后进入ROM实际读取seq2/VALID2及CRC，首份seq1有效记录保持，eFuse寄存器和安全状态保持。再次启动1.767秒仍为同一新SKU/ELF、指定Wi-Fi、ota1、激活完成和idle，无panic/abort。最终串口关闭，设备留在新固件待命。这两次启动不是真人语音验收。

## 真人两轮验收与当前边界

收尾检查Go PID4139298/NRestarts0、8000 PID4139310/NRestarts0，ready/记忆就绪、active0/max2，注册表含三块硬件。此前属主错误触发的重启循环和恢复均已留私有证据；上述PID为最终成功加载后的进程，不冒称整轮未重启。M5和微雪本轮没有刷写或操作串口。

用户按键做两轮问答后明确反馈“**两轮回答完整，按键也能结束**”。正式Go会话`20261031T050655-c9b6021a85f171937b2d79b17cb8e77f`持续约32.22秒：C3认证MAC对应的device_id与session及全部event归属一致；2个speech_started、2输入、2生成、0取消/额外回复/错误。MCP发现完成，可用工具2个。

| 回答 | sent | accepted | completed / raw played | 时长（24k） | basis / terminal |
|---|---:|---:|---:|---:|---|
| 1 | 95040 | 95040 | 95040 | 3.96秒 | i2s_dma / played |
| 2 | 252000 | 252000 | 252000 | 10.50秒 | i2s_dma / played |

首包相对服务端speech stop为1240/1308ms。第一轮实际播放完成后第二次发言正常生成，支持本次半双工恢复收音通过；不外推所有噪声条件。按键结束的用户反馈对应后台transport关闭和资源释放，Go记录`transport_lost`、WebSocket1006，**不是1000关闭帧**；结束后active0且服务仍ready。C3首次会话加载记忆0轮，最终近期记忆写入saved，归属正确；未验证下一会话回忆内容。

串口已关闭，不再重开/重启设备干扰使用。重复结束/重连、音量实际调节、跨会话回忆、断网和待机仍未做本轮实测；不从主机测试或两轮成功补记通过。不把C3接入称为Mac/微雪既有语音问题的修复。

收尾期间用户又开启同一C3会话`20261031T050756-ba13df0003899348cc4ce9d64c1788fc`，后台确认重连且加载上一会话2轮近期记忆，后于05:09:47Z结束。此前快照有7个完成回答，完整会话结束后复核为8个，均sent=accepted=completed、i2s_dma/played，共1261440样本/52.56秒；另4个回复在首包前被新speech_started取消，均sent0，不是播放中已下发音频被截断。此为追加后台证据，没有对应逐轮真人反馈，不外推记忆回忆内容、音量听感或长期稳定；首次两轮的用户验收结论保持。

## 继续验收初核

用户反馈“固件良好，继续”后，再次核对以上完整会话：设备状态查询回读音量70，调节请求目标80，设备实际回读80，144ms取得verified回执；执行与回读均路由到同一C3。会话结束时memory_finalized=saved。后端控制链路和近期记忆保存/加载已取得实际证据；音量听感及下一会话准确回忆暗号仍需真人确认，已请求一次“蓝色风车”跨会话测试及音量调至70。

当前C3板级`PowerSaveTimer(160,300)`的首参是CPU最大MHz，第二参数才是300秒待机省电阈值，shutdown参数默认-1；源码无自动关机回调。安装期间最后USB快照的wifi.sleep_mode=1。本轮仅列举USB目标，没有打开串口，不把这些源码/历史快照当成当前实测待机恢复。正式Go/8000仍为PID4139298/4139310，NRestarts均0，ready/记忆就绪、active0/max2，其他五个保护配置摘要保持。
