# StopWatch 软件 AEC 现场验收与回退清单

状态：软件交付与现场验收分开记录。本文最初为待执行清单；2026-10-22 已进行[首次现场试验并恢复原程序](2026-10-22-software-aec-field-results.md)，实验 AFE 输入检查未通过。下面未勾选的声学/插话门槛仍未通过，不以绿色 CI 代替。

已完成的离线构建/下载检查见 [交付记录](2026-10-22-software-aec-build.md)。以下现场门槛仍逐项执行。

依据：[已批准自然插话设计](../superpowers/specs/2026-10-21-natural-barge-in-design.md)、[软件 AEC 整合计划](../superpowers/plans/2026-10-22-software-aec-integration.md)。保留物理单麦、原引脚、24kHz I2S、待机唤醒、音量/亮度工具和两分钟退出；本地不新增 VAD abort。

## 1. 下载产物和恢复资料，先离线核对

刷实验固件前，先按第 5 节在现有固件记录电池与 Mac/原 C-to-C 线的噪声基线，记下固件身份和发生阶段。刷后再以相同条件重复；声学测试与 USB 噪声调查分别记录。

- [x] 记录待测 main 完整提交 SHA、两个 matrix job 链接和 artifact digest。下载同一 SHA 的 `stopwatch-patched-m5stack-stopwatch-software-aec-<sha>` 与 `stopwatch-patched-m5stack-stopwatch-<sha>`，七天内离线保留。后者检查补丁树关闭实验时的核心路径；[独立未修改原版基线](2026-10-22-firmware-baseline.md)仍是另外一份证据。
- [x] 校验下载 ZIP 的 GitHub digest、ZIP CRC，解包后在证据目录执行 `sha256sum --check SHA256SUMS`（macOS 可用 `shasum -a 256 --check SHA256SUMS`）。检查所有清单文件恰好存在，来源 SHA 对应该次运行，而不是另一次同名构建。
- [x] 核对 `build-evidence.json`、`.family-robot-source.json` 和 `source/patches/`、`source/overlays/`：上游 `49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`，SDK `fff9895c82d744c7237be8847347bdd1b07c6643`，原生 `dependencies.lock` 与仓库验证过的锁字节相同，ESP-SR 2.4.7、esp_codec_dev 1.6.2、IDF 6.1.0。依赖漂移必须先解释、复核并更新受控锁，不能把自动解析的新锁当成原来的锁。
- [x] 检查实际 `sdkconfig`：实验 `STOPWATCH_SOFTWARE_AEC=y`、`USE_DEVICE_AEC=y`、`USE_AUDIO_PROCESSOR=y`、USB Serial/JTAG PRIMARY、UART DEFAULT 关闭、secondary USB 关闭；两变体 `I2S_ISR_IRAM_SAFE=n`、`USE_AUDIO_DEBUGGER=n`。原变体 software AEC/device AEC 关闭、UART DEFAULT 开启、USB PRIMARY 关闭。Kconfig 对依赖不成立的原版 `USE_DEVICE_AEC` 可省略该符号，含义为关闭；实验 primary USB 开启时 secondary USB 也可省略，见[锁定 SDK 的控制台 Kconfig](https://github.com/espressif/esp-idf/blob/fff9895c82d744c7237be8847347bdd1b07c6643/components/esp_stdio/Kconfig#L32-L52)。记录实际 `STOPWATCH_AEC_OFFSET_SAMPLES`，单位为 24kHz 物理样本，默认 0。
- [x] 用相同 SDK 的镜像/分区工具离线检查 app 和 bootloader 的 ESP32-S3 类型、image checksum/hash、入口/段边界、Flash 模式/频率/容量、secure boot/flash encryption 策略；校验分区表 MD5。检查 `flasher_args.json` 的地址和文件与实际产物一致；不要执行其中的整套写入参数。
- [x] app 必须不超过 `0x3f0000`，assets 不超过 `0x800000`；分区不重叠，Flash 终点不超过 `0x1000000`。实际 app、bootloader、表和资源须一起核查，而非仅核对 merged 文件大小。

现有私有完整恢复源：`artifacts/device-backup-20261021-191653/full-flash-before.bin`，16,777,216 字节，SHA256 `e87c4a8541d5a324ad670e648d3700f88845cd22d895ad5033969d6c05d60d8d`。备份目录保持 0700、文件 0600；不得上传完整 Flash、NVS、凭据或原始音频。`artifacts/firmware-preflight-20261022/` 的完整性、app 身份、OTA 元数据 JSON 仅是已有离线备份分析，不能证明当前设备状态。

| 区域 | 起点 | 长度 | 默认处理 |
| --- | --- | --- | --- |
| bootloader 区域 | `0x0000` | 到 `0x8000` 前 | 比对兼容性，保留现场已有版本 |
| partition table 扇区 | `0x8000` | `0x1000` | 校验 MD5、类型、subtype、地址、大小和 flags，保持原表 |
| nvs | `0x9000` | `0x4000` | 保留当前设备配置，不回写旧备份 |
| otadata | `0xd000` | `0x2000` | 保留；不能凭旧备份盲写或清空 |
| phy_init | `0xf000` | `0x1000` | 保留 |
| ota_0 | `0x20000` | `0x3f0000` | 现场确认选择与恢复路线后才确定写入 |
| ota_1 | `0x410000` | `0x3f0000` | 同上，优先考虑保留可启动的已知良好槽 |
| assets（两 app 共用） | `0x800000` | `0x800000` | 先验证与两 app 兼容，默认保留 |

- [x] 已离线复核完整备份长度/SHA、表 MD5、app/bootloader 的 ESP32-S3 镜像 checksum 与附加 SHA；按上述范围提取 ota_0（4,128,768 字节）和完整 assets（8,388,608 字节），并逐字节核对切片。私有 `artifacts/firmware-rollback-20261022/` 保存两份完整分区、`rollback-manifest.json` 与 `SHA256SUMS`；目录 0700、文件 0600。未提取或纳入 NVS、otadata、phy、bootloader、表的默认恢复写入清单，未连接设备。清单中的地址仅代表旧备份布局，不能代替现场槽选择。
- [ ] 现场确认恢复工具/下载通道可用、供电和线材稳定、存在手动进入下载模式的恢复手段；完成当前设备身份、分区及兼容性核查后才决定准确恢复范围。
- [ ] OTA 选择必须解析两个 4KiB 扇区中的原生 `esp_ota_select_entry_t`，按此 SDK 的 sequence CRC、state 有效性和序号回绕选择规则处理，不能只取最大数字。对有效条目，用 `(ota_seq - 1) % OTA分区数量` 得到候选槽，再检查相应镜像。已有备份第一条 `ota_seq=1`、`ESP_OTA_IMG_VALID`、sequence CRC 有效；第二条擦除，故**备份**选择 ota_0。SDK 的 `bootloader_common_loader.c`、`esp_flash_partitions.h`、`bootloader_utility.c` 是 CRC/枚举/选择的核对依据；不能自行简化为对整条记录做普通 CRC。
- [ ] 当前设备的选择仍未核实。用户在场且提前知悉后，先只读保存当前表、两个 otadata 扇区与当前 app 身份，私有留档并按同一 SDK 解码；不静默打开可能触发复位的串口。若当前表、槽、加密策略或固件身份不符，停止并重做写入/恢复清单。

## 2. 按分区决定写入范围，现场通知后才执行

优先保留可启动的已知良好 app 槽。只有具备已验证、另行确认的正常槽切换路径，才考虑把实验 app 放到非活动槽；仅把 app 写进 ota_1 并不意味着 bootloader 会启动它。现有备份只有 ota_0 被确认有效，不能承诺自动回退到 ota_1。没有安全切换路径时，保留 otadata 的最小方案是仅替换现场实际选中的 app 槽，并准备同槽的分区恢复；这会失去该槽原 app 的在线副本，须在现场明确记录取舍，不能称为双槽自动恢复。

- [ ] 核对 candidate app 与现有 bootloader 的镜像/安全配置兼容、分区表几何完全一致、资源格式和实际模型可用。相同版本号或同样的分区大小不足以证明兼容。
- [ ] assets 是共享分区。首先比较 candidate 资源与备份资源的实际字节/格式、模型索引及两 app 的读取约定；严格字节一致时保留现场资源，不重写。仅比较 candidate 前缀不能证明整个分区相同；比较填充/尾部时说明方法。若不一致，先证明双向兼容；做不到则必须准备旧 assets 的完整分区恢复，不能承诺仅切 app 即可自动回退。
- [ ] 默认只允许经过确认的 app 分区写入。bootloader 和表兼容则保留；不兼容时暂停，先制定并复核独立的迁移及恢复方案，不能顺带照抄 flasher args。NVS、otadata、phy_init 不在默认写入清单；无已论证迁移不改它们。
- [ ] 如确须换共享 assets，在下载模式中完成 app/资源组合写入和逐分区读回校验后才启动；不能让旧 app 中途用不兼容的新资源运行。记录修改前后每个分区的 SHA 与实际范围。
- [ ] **准确串口设备名、波特率、写入地址/文件命令留到用户在场、提前告知影响并完成上述核查后生成。本文不提供可误执行的串口写命令。禁止 erase-flash、盲刷 merged-binary.bin 或旧完整备份。** merged/ZIP 仅作离线检查材料，不能绕过 NVS/otadata 保护。

刷入后先验证启动、联网、原 auto 对话与原工具，记录实际版本、窗口、槽和资源身份。只有通过此项才开始声学观察；期间不部署/重启线上网关。

## 3. 先观察 AEC，原始音频仅限显式窗口

待机和 USB 连接本身不会启动诊断采集；realtime 启动默认为 off。以下是现场测试窗口的命令模板，本次未执行。先用主机自己的 RFC1918 IPv4 替换占位符，选择空闲 UDP 端口；接收目录须全新且在 Git 忽略的 `artifacts/` 或仓库外。

```sh
python3 firmware/tools/capture_aec.py \
  --bind <主机本地IPv4> --port 9000 --duration 30 \
  --source <手表IPv4> --max-bytes 33554432 \
  --output artifacts/aec-field-<全新窗口名>
```

主机运行后，在实验固件 USB PRIMARY 控制台分别输入（start 严格单空格，不含尖括号）：

```text
aec_realtime off
aec_stats
aec_diag_start <主机RFC1918-IPv4> 9000 30
```

停止测试时输入 `aec_diag_stop`，主机可 Ctrl-C；两端时限独立，设备发送最长 120 秒，主机到时、显式停止或 32MiB 总文件预算先到即停止。主机预算包括原始 packet、副本 WAV、索引及 summary；预算用尽不代表设备自动停发，应同时停止设备窗口。主机必须显式启动；没有默认监听/后台服务。可在 USB 发起窗口后拔线，以相同测试场景测电池供电。

- [ ] 开始前确认环境允许必要的短时私有采集，不保留无关对话、Wi-Fi/token/API Key；正常对话流与额外诊断流是两条路径，关诊断不会停正常云端对话。保留任何控制台记录前检查 SDK/组件日志，不能只依赖应用日志抑制。
- [ ] `aec_stats` 应为 physical=24000/mono16、logical=MR；先有有效配对及当代成功 AFE 输出才有 realtime eligibility。记录 generation、fault、RX 队列、overrun、phase、offset 和诊断 drops；故障、持续缺帧/积压或有效参考缺失立即停止该轮，不带故障进入 realtime。
- [ ] `packets.bin` 是依接收顺序的 `uint32 LE datagram长度 + 原始SWAC datagram`；`index.jsonl` 逐包保留 header、源、packet offset、segment、分段原因与 `reference_valid`。`summary.json` 报告每流 frame/各声道样本总数、缺包和乱序计数、首/末 generation 与变化次数；完整 generation 序列在索引中。文件 0600、目录 0700；summary 的字节数是全部实际文件长度之和，不是音频时长。
- [ ] stream1 WAV channel0=麦克风、channel1=已完成 TX 参考，stream2=AFE mono，均为 16kHz PCM16 LE，原字节不改动。reset/gap/乱序/重复/位置不连续/时间回退/window/generation/参考有效性改变均切段，绝不补零。无效参考段必须剔除声学评分；有效标志也只是必要条件。
- [ ] raw 与 AFE 有独立 sample position 原点，不能按相同 position 或 WAV 起点直接对齐；producer timestamp 也不是实际播放时间。只在无缺口、同一窗口/代次且参考有效的完整段中，以真实声学特征估计 AFE 延迟；不能把网络到达时间、Write 时间、模型输出或网关发送时间当播放基准。

### 校准与 realtime 门槛

先在安静房间、桌面约 0.5 米、音量 80，用真实扬声器播放涵盖停顿和起落的可辨识短句，记录音量、亮度、距离、供电、摆放、电量及测试前设置。先做仅播放、仅近端说话，再做双讲；禁止通过播放期间静音麦克风或丢用户开头制造“无回声”。

MR 两声道来自同一次联合重采样，可以在完整有效段上比较 mic/ref，观察削波、漏样和回声时延；记录使用的本地分析工具、片段区间、波形/相关峰与不确定度。声学峰应稳定且可解释，不能拿双讲、静音段或跨缺口数据做校准。若在当前 offset 下，`mic[n]` 对 `ref[n-d]` 的可信直接回声延迟为正 d 个 16k 样本，候选增量为约 `round(d × 24000 / 16000)` 个物理样本；正 offset 表示向 TX 历史回看。须排除混响峰、确认符号并复测，不能盲信单个最大相关峰。timestamp 的 1500µs 完整性阈值不是声学准确度。

本工具不自动估计 offset 或给出 DSP/声学通过分数。若测量表明默认 offset 不合适，先停止窗口，将测得值及误差留档；仅在实验变体的受控补丁配置中设置 `CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES`（范围 -2400..2400），重新提交、构建、下载复核，并在另一个通知过的现场窗口测试。它不是控制台运行时可调项；不顺带改增益/功放/充电设置。若在允许范围内参考稳定仍无法消回声或保留双讲，应回退并报告该方案尚不可用。

只有配对/计数健康、延迟校准有实测依据、仅播放回声受抑且双讲保留人声后，操作者才可显式输入 `aec_realtime on`。接收命令时的 eligible 检查只证明软件门槛，不代表声学通过；主任务还会复核当前代次。遇到 fault 或新的无效参考，立即 `aec_realtime off`、`aec_diag_stop`，检查并回到 auto/已知良好固件；不强开 realtime 绕过保护。

## 4. 真实千问链路与至少十轮现场验收

先确认所需 `tts.stop reason:"interrupted"` 已在所测网关版本中可用；记录部署身份。源代码已有这个标记不等于线上已部署；若缺失，应暂停这部分，在用户知悉且在场的测试窗口完成计划内网关更新；不在用户休息期间静默部署。之后才在真实云链路测试：

- [ ] 机器人单独播放累计两分钟，无自身回声造成的用户转写或误打断。单次原始诊断窗口不超过 120 秒，采集不是正常运行的常开功能。
- [ ] 回复开头、中段、末尾直接说完整的新句，至少十轮，至少九轮识别新意图并转入新回答。至少九轮从开口到旧回复**实际扬声器停止**不超过一秒；记录中位数、最差值和每个失败原因。这是验收目标，当前没有通过证据。
- [ ] 用“停一下，先告诉我……”等带明确开头的短句核对用户开头没有被重连、清队列或门控丢弃；新回答不夹带上一轮迟到音频，模型也不误认为旧回复全部播完。
- [ ] 在同一段现场录音/录像中标注开口起点、旧回复终点和新回复开始；需要可听见实际扬声器的录制设备。设备/网关时间戳仅用于定位。没有该证据则实际停播时延仍为待测。
- [ ] 连续十轮后无重启、任务卡死、RX/诊断队列持续增长。普通 tts.stop 仍自然排空，interrupted 才清待播数据；已提交 DMA/DAC 的尾音有参考覆盖，不能要求物理瞬间零延迟。
- [ ] 待机正常唤醒；音量/亮度工具有效；静默约两分钟返回待机且再次唤醒可继续。结束时恢复测试前音量/亮度并 `aec_realtime off`、`aec_diag_stop`。首轮通过后再安排音量 100 和其他距离，不预先承诺远场性能。

每轮私有记录：窗口/代次、供电/距离/音量、插话位置、开头保留、新意图结果、实际停播时延、误转写/迟到音频、fault/drop/队列、证据文件。最终分开填写：软件回归、两变体精确 SHA 构建、下载产物检查、AEC 声学、真实模型链路、十轮插话、原工具/待机、回退检查。

## 5. 独立 USB 刺耳声对照和恢复

依照[独立 USB 噪声调查](2026-10-22-usb-awake-noise.md)，在相同摆放、音量 80、亮度 50、相同下行刺激下，对比电池、电脑 USB、独立 USB 电源。逐一观察待机、唤醒但尚未播报、实际播报、播报后聆听，记录声音位置、电量和充电状态。没有播放不等于 DAC/TX 时钟或外部功放关闭，音量 0 也不等于关闭功放。先定位来源和阶段；没有根因证据不改增益、充电或功放，不把噪声消失当作 AEC 通过。

出现无法启动、音频卡死、持续参考故障、声学不合格或回声误触发时，停止诊断/realtime；在已通知的窗口按预先复核的恢复清单处理：

1. 保持或进入下载模式，核对芯片/安全状态、当前分区表和槽；不整片擦除，不盲写原 16MB 备份。
2. 若改过共享 assets，先恢复备份中 `0x800000..0xffffff` 的完整旧资源并读回校验，维持下载模式不启动；再把备份中已验证的旧 app 恢复到已确认的目标槽并逐分区校验。未改的资源不重写。目标若不是备份 ota_0，必须确认 app 可重定位到该 OTA 槽且选择路径已验证，不能直接猜测切换。
3. 保留当前 NVS、otadata、phy_init。若试验只替换活动 app，恢复同槽旧 app 即可维持原选择；若另有正式迁移，恢复动作必须按其专门批准的清单执行。bootloader/表默认没有改变；若确改过，只能按已核查的兼容迁移恢复方案操作，不能临时拼接镜像。
4. 读回确认允许范围外没有变化，再启动，核对版本/槽、原唤醒、联网/对话、音量/亮度及两分钟待机。共享资源复原前不宣称 app 自动回退有效。记录恢复成功与剩余问题。

本次交付只准备上述流程。任何物理 AEC、自然插话、USB 噪声修复或硬件恢复结果均未在本文执行或判定 PASS。
