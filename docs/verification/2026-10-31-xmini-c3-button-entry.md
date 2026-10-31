# Xmini C3 按键接入验证

当前状态：**代码及云端121项固件主机测试通过，规格和质量审查通过；目标编译、安装和实体语音验收尚未完成。** 用户选择按键开始连续问答、暂不要求播放中语音插话，并选择使用微雪当前的同一Wi-Fi。网络资料和设备凭据不写入此文档。

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

运行在服务器独立目录，不在Mac执行固件主机测试或启动语音服务。最终主机测试记录为 `host-suite-v5.log`：

```text
Ran 121 tests in 72.422s
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

## 正式服务与后续门槛

本阶段仅检查正式服务并保存服务器私有基线；未改正式代码或配置、未重启服务。Go仍09d22e6，检查时PID2183364/NRestarts0、ready/记忆就绪、active0/max2；8000健康且active0。新凭据及加载时点在安装阶段另验，不以这些历史检查代替部署前现状。

接下来完成精确提交的五variant目标编译、产物/来源/依赖/配置/镜像/分区核验，再登记独立身份、USB配置网络和安装回读。真人问答、重连、音量及记忆另列结果；当前不补记这些已通过，也不把C3接入称为Mac/微雪既有语音问题的修复。
