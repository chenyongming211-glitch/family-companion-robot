# C3有界PCM实施与验证

当前状态：ee4249d首次实机启动再次被拒绝，已捕获RxPair/Cadence首次故障及25,873µs软件回调间隔；当时内部堆最低34,800B，pack/SRC/AEC/两个任务均成功。已恢复三个授权域和稳定6b/app1，独立只读审计确认仅PHY校准NVS值变化，其余配置保持；两次稳定启动观察通过，串口已关闭。下一步修C3完整IRAM闭包并用同钟cache-span检验Flash屏蔽假设，尚不放宽门禁或宣称唯一原因/已修复。Go58健康且未再重启，M5/微雪保留Opus且未刷写；仍无实机PCM负载或回声验收，自然插话未修好。实施门槛见[设计](../superpowers/specs/2026-11-01-c3-bounded-pcm-design.md)和[计划](../superpowers/plans/2026-11-01-c3-bounded-pcm.md)。

## 已完成的源码和云端验证

| 范围 | 提交 | 已核对证据 |
|---|---|---|
| Go显式PCM协商与共同Session接线 | 5d90ef7 | 全仓376主测试/869含子用例通过，2可选skip；PCM及race各10主测试/30含子用例通过；独立规格和质量复核通过 |
| 固定PCM池与提示音包基础 | f0ef155 | 云端16项测试通过，包含O2、ASan/UBSan、全部21项真实源提示音及517帧读取；独立规格和质量复核通过 |
| C3受限网络构建输入 | d9b4cac | 云端7项Python测试通过，实际生成WebSocket39、EspTcp12、辅助器14项分别在O2与ASan/UBSan通过；独立规格和质量复核通过 |
| 实际PCM媒体、流式提示音、FD和资源观测接线 | d156e8d、d6c6ff3、14df705 | 最终25项源码摘要与云端一致；独立规格和质量复核均在14df705通过。d6完整206项/388.211s通过且无skip；最终PCM控制修正的20项专项/67.462s及4配置8次旧协议构建/运行通过。最终整合源码的完整回归仍属Task4/CI门槛 |
| 构建接线与实际产物绑定 | 3ff3951、cbc49de | 3ff基线完整225项/1056.633s无skip通过，上游110项/7.581s通过；cbc修正后的114项专项与准备流程回归/17.988s、上游110项/7.396s通过。最新188项源码闭合前后一致，独立规格与质量复审均PASS；仍待六目标实际CI，不把3ff完整结果当作cbc完整回归 |
| 固定SDK的公共PSA SHA适配 | 32024dc | 正确环境RED的30项专项中19个预期失败；GREEN30项/61.197s通过，O2及ASan/UBSan覆盖初始化、hash错误和错误长度。源码186项闭合、15项公共API证据摘要及锁/包保持经原规格→质量复审PASS；原生六目标构建仍待验证 |

测试在云端独立目录`/opt/family-robot-validation/c3-pcm-product-20261101`执行，Mac未运行语音服务或运行时测试。Go日志保留在`logs/`，池/提示音日志保留在`primitives/`，网络日志保留在`network-bound/`。这些是私有执行证据路径，生成文件未进入Git。

Task3精确证据按源码区分：`task3/logs/all-firmware-final-v4.log`对应d156e8d，205项/440.789s、1项提示音夹具skip另行通过，上游脚本110项/7.991s通过；`task3-disconnect/all-firmware-final.log`对应d6c6ff3，206项/388.211s通过且无skip，包含21项提示音/517帧和实际网络封装测试；`task3-control/`对应14df705，`source-final.sha256`的25项与本机逐文件一致，`focused-green.log`20项/67.462s及`legacy-protocol-green.log`4配置8次构建/运行通过。目录`task3/`下无版本后缀的`all-firmware-final.log`是更早候选，不能混用作最新源码证据。

独立复核发现并完成两处带RED→GREEN的修正：网络接收断开回调只做连接专属的非阻塞撤销/通知，main轮询关闭，销毁时先停止并等待socket回调再删除协议event group；匹配当前播放的错误stop样本数立即关闭PCM入口并请求owner清理，避免继续接纳后续PCM。旧连接、session或播放identity的迟到控制不撤销新会话；正常stop仍仅Seal并保留已接收尾音。两项已通过原复核者复查，尚不能代替精确目标构建及实机验收。

Go新入口仅在version1、pcm_s16le及pcm_s16le_v1明确协商时接受PCM，旧共享ParseHello仍只接受Opus。输入恰1920B、输出恰2880B，沿用鉴权、归属、记忆、设备控制、取消和drain策略。PCM零输出不伪造静音或无begin的stop，会话仍可接下一次输入。

## 固定存储与提示音

输入池2×960样本，包括部分帧和排队帧；输出池5×1440样本，包括排队与在途lease。Reset保留在途存储，旧lease不能释放新槽。固定PCM池载荷18240B，加提示音1440样本scratch共21120B；另计最多1920B在途上行包，应用PCM副本载荷上限23040B。网络、元数据、DMA、共同MR重采样和AEC独立计入实机预算。

转换器保持原OggDemuxer的包时钟，使用libopus1.4逐包转换全部common和zh-CN共21项提示音：517帧、744480样本、31.02秒、PCM1488960B，完整包1490032B，SHA256为`7457718e68e984c3ebced58164e7e967a1304b52003b02d23d1baffa9ca779da`。完整包和body均需真实SHA校验；读取通过字节复制到对齐scratch。

计划安装范围为assets分区相对0x50000/绝对0x650000的独立trailer，结束不得超过0x800000。原generated_assets.bin长度269881B、SHA256为`56295f2ecb92f21bdb737a462807e9d2acc7781a606d6a90628f71c65d28a604`，本轮构建须重新核对其字节不变且长度不超过0x50000。基础解析器通过不代表Flash映射、媒体接线或安装通过。

## 网络边界

锁定78/esp-ml3073.7.6，registry包SHA256为`b7d30d48a8cd8fa98b061673afdb2abdab10ad994ab71d7d27edbc0ada956036`。生成器先验证两份源文件、三份头文件和许可证的六项摘要及转换锚点，再输出独立构建目录副本和manifest；不修改managed component、原ABI或原生依赖锁。

握手2048B、文本4096B、二进制累计恰2880B、原TCP分块1500B、接收累计6144B。正常完整帧直接借用接收字节；首次分片额外预留4096B并保持。失败锁存并通知owner，接收回调不调用Disconnect自等待；析构先停止并等待TCP，再删除握手event group。发送锁先于封装分配，只有完整写入才成功。

明文TCP在DNS后有5秒连接预算，发送使用非阻塞send/select共享250毫秒总预算。实际阻塞POSIX对端测得250281/250295微秒。DNS不包含在该预算中；TLS保持原实现，新C3媒体接线须在连接前明确拒绝wss。

未改原实现的对照结果为WebSocket30/39失败、EspTcp10/12失败，保留在`websocket-red-final.log`和`tcp-red-final.log`；最终结果在`green-final.log`，生成来源在`generated-final/family_c3_network.manifest.json`。主机RTOS桩没有运行真实接收任务，因而这些测试仅证明受测方法、边界和退出调用顺序，不证明真实FreeRTOS调度。

## 尚未通过的门槛

- 整合构建接线后的精确源码完整回归；媒体任务、提示音调度、退出屏障、资源下载保护和FD数值观测已完成源码接线与主机复核。
- CMake受控源替换、精确五variant与stock构建，以及实际app/trailer/ELF/map的独立核验。
- 带trailer范围的安装与恢复工具、失败注入、USB补集回读和稳定app1保护。
- 实机网络/SRC/FD/PCM同时负载下内部堆最低值至少16KiB、最大连续块至少8KiB，任务栈/处理时限有余量且无积压或参考故障。
- 无人讲话不自触发、播放中真人插问、新回答完整、记忆/音量/按键与故障恢复。

不以启动成功、数字回环或主机测试替代实机资源和声学验收。

## 58c14cd精确构建与实际产物

精确源码`58c14cdefdad54db8aac7610a7c2d0971e2abf27`的[experiment41271574904](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41271574904)五目标及[stock41271574932](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41271574932)全部success。C3 job124745250823的248项完整主机测试/190.381s、上游110项/7.595s无skip通过，原生编译/链接/构建校验/打包均通过。

六份实际ZIP均按GitHub artifact ID/digest/字节数、CRC及解包全量文件匹配。五个非PCM产物的独立报告在云端`task4a-actual-artifacts/report.md`，12项证据清单SHA为`9c26d6f5996888f6249ff78e4a7c6a047f21e9da9e94842ba9ac1e492676682b`；原网络双源和六原输入、配置/源码库存、实际镜像checksum/digest、分区/资源、flash_args/JSON、全部merged字节及release均通过。四个patched S3严格匹配f0568dd原生锁；stock按原始未补丁manifest解析为b26df81锁，关键版本相同，仅button/esp_jpeg/iot_usbh_cdc三个非关键minor版本不同，详报保留边界，不把stock称为patched锁字节一致。微雪唯一共享`xiaoman_orb_view.cc`按0008 owning CMake核验，无错选M5 codec。

C3 artifact13069607051 ZIP SHA为`fdc819ca2899cbacdae4fa2c70a47a1a1367f462f7d1ccbd1f538bfa396bf68d`。完整云端独立检查重建120项实际输入及网络/提示音规范生成器，核对原始compile_commands、全部flash/merged/release、e178原生锁、IDF来源、配置与原assets/分区保持。app2234816B，SHA`375b19ee59f7e094ba93da9e0175a5f3447a70a1c5c5cd029d5b4e2dc9a03b44`；实际ELF42411076B，SHA`41a8be584f3cbcf139267b324775f390c23893ace79d548c780a12ba1ef5c333`等于app描述符；包1490032B/7457718e摘要保持，chip5/rev3..199支持本机rev4、secure_version0。报告在`task4b/actual-c3-58c14cd.json`，实际inventory摘要`8ffae3a68d4cebbe3a662c9b676ed7416fc38c1164d8ed27e9a6f536d4a9e348`。

root通过GitHub push run的精确head/path及实际artifact身份独立建立外部binding，Mac标准库client gate逐文件复验通过，binding规范摘要`0824b91778ba974b65bc8a68b1d5dc45e2aa6c6aade39d665c57425163263dca`。此为技术前置核验记录，不替代实机验收。私有工具历经规格/质量反馈后最终51项/10.947s通过，原规格及原质量复审均PASS；缺失诊断、晚零栈、累计旧值、保护区漂移、Git未跟踪/ignored/blob漂移及bytecode影子导入均有拒绝回归。代码、备份、产物留在私有目录，未入Git。

## 受控服务端更新与首次USB安装

2026-11-01 12:25 CST，仅将Go二进制更新为58c14cd，旧二进制及当前身份/记忆备份在云端`task5-go-build/rollout-58-20261101T042514Z`；更新前双方active0。Go手动重启后PID1304344，ready/memory_ready=true、max_sessions2、active0；gateway仍PID4139310，未重启。六项保护配置/属主/权限及身份/记忆文件逐项保持，14项引导/鉴权/交叉拒绝检查通过，0声音会话/模型请求。新二进制SHA`a5ea30f5d70996dd79cc3a82cb5ad35d22ed4b394470a479f5e483b323931213`。未改共同VAD/取消/drain策略，本轮没有用M5/微雪实际出声验证。

12:26 CST启动经核验的USB安装工具，唯一原设备`/dev/cu.usbmodem14201`/68:c6:3a:39:64:98；打开前无其他串口持有者。私有工作目录`.superpowers/c3-pcm-install-20261101/usb-58-20261101T0426Z`，安装日志位于`.superpowers/c3-pcm-rollout-20261101/install-58c14cd.log`。正在鲜取备份与核对16MiB补集，后续只允许app0、精确扇区取整trailer及selector0，不写稳定app1或eFuse。此处尚无安装成功、启动资源、实际网络或插话结论。

### 首次启动拒绝确认，已完成稳定版恢复

写前全部三域原字节已备份，16MiB补集字节SHA/设备MD5核对通过；实际只写app0 `0x20000+0x222000`、trailer `0x650000+0x16c000`、selector0 `0xd000+0x1000`，序号5。三域写入及各自逐字节回读通过。

首次boot准确加载ELF41a8be584，原设备身份/SKU保持；ES8311和24k单声道TX/RX打开，1663ms LiteAudioEngine报告Initialized/WakeNet:no。随后Wi-Fi/IP、原assets映射校验及8000引导HTTP成功；3553ms唯一错误为`C3 audio initialization failed; refusing OTA confirmation`，设备重启并自动OTA回退app1，ELFbf00948a1、Activationdone/idle。没有panic/assert或明确AEC创建错误，没有进入资源日志主循环，不能据此说OOM、语音网络故障或回声验收失败。

`first-boot.proof.json`记录同句柄故障后尾部8.127秒、14209B、日志SHA`e4e062ec2e561887dedd93f6e48009a331eeccaa178c8590893c440100571a48`，ready=false。安装工具随后核对稳定启动路径，先将候选ABORTED，再恢复app0/trailer/原selector0；最终保护区回读及稳定启动核验仍在进行。不能将自动回退那次启动等同于整个恢复工具已完成。

12:51已完成恢复工具：`stable_restored=true`，三授权域原字节均恢复、稳定路径和eFuse身份通过，稳定app1激活/idle后观察15.011秒无故障；进程以候选失败exit1结束且finally关闭串口。保护范围0..0xd000有变化，故明确`whole_flash_restored=false`，没有声称全部16MiB逐字节恢复。工具没有写回未授权NVS。

随后12:52做独立只读审计，0 Flash写：新取128KiB前缀与原备份差1964B，全部位于0x9000..0xd000的NVS；逻辑27项→27项，仅`phy/cal_data`校准记录变化，其余26项及13个SSID/password/token/url/UUID/volume等字段逐项相同，值未输出或上传。稳定app1又激活/idle后观察15.057秒通过，串口关闭。私有证据在`nvs-read-audit-20261101T0452Z/{logical-diff.json,result.json,stable-audit-boot.log}`。

原日志还明确记录：1663ms Lite Initialized后，1703ms PHY版本1235/8bd8c6fc，1733ms因checksum失败或过期保存新校准数据；自动稳定回退时同PHY版本但无Saving行。该事件与NVS差异吻合，可能涉及非IRAM I2S回调在Flash操作期间的延迟，但首fault也可能更早发生在pack/hash阶段，当前不能确定因果；诊断将用首次微秒时间及来源区分，未先更改回调/时序策略。

只读代码/日志对齐把范围缩到codec打开后的VerifyHardware或DMA/reference静默FaultLocked路径，尚无唯一根因。显式pack/SRC/AEC/task创建失败通常会关闭输入/输出并打印false，本轮没有这些日志；constructor/setup在EnableInput入口前通过，AEC首次Create失败应有另一条错误但实际Initialized。空闲时Pair仍检查时序，故启动期静默锁存是可证路径而非本次已证明原因。实际app含期望pack SHA恰1次、硬件SHA配置启用；Assets MMU空闲候选4608KB/稳定6016KB仅作映射旁证。下一步保存首次fault来源/时间/Pair原因与初始化阶段状态、在OTA拒绝前一次输出；不在ISR打印，不放宽容错，不先重排初始化，不追加真人盲测。

### 首次故障诊断源码，尚未构建或安装

bd0723f增加PCM专用的定长数值诊断：同一mux内保存不可覆盖的首次来源、微秒时间、原Pair原因、TX/RX样本和回调前时间；硬件失败保存位图、查询返回值和实际参数。初始化阶段分别记录partition/map、两次真实PSA hash、SRC、AEC及任务创建状态，NotEntered与成功返回0区分。最多10个内部堆快照，两个结构分别限制在160B和448B内。仅在既有ReferenceFaulted/OTA拒绝分支从任务统一打印，不在ISR打印或分配，不更改门禁、阈值、FD参数、缓冲边界或初始化顺序。8b3e2a0另在锁外报告故障打印时的当前内部堆。

云端`task5-startup-diag/`的bd源码专项为startup2项/11.808s、codec2项/31.693s、duplex1项/8.041s、media33项/84.165s，均通过；codec覆盖57场景的PCM0/1与O2/ASan四配置，duplex覆盖34场景的双配置。8b日志修正的`report-green.log`确认O2/ASan均exit0；该次188源输入清单`c3-report-source.sha256.json`自身SHA为`8255d16d1be11bf61371aadea94e28cc18b3e633ec246f5700ed29fcecd726c1`。中途准备路径/SSH环境失败的运行不算产品RED或通过证据。

规格复核发现的唯一范围缺口已由374ea95修正：PCM0移除DiagnosticNow及诊断参数求值，恢复FaultLocked()/PublishReferenceLocked()原私有签名。公共base virtual和诊断状态守卫保持，不涉及M5/S3运行行为或对象布局变化。云端旧源码nm回归的两个PCM0组合均失败（5.441s）；新源码codec2项/23.205s（57场景×4及符号断言）、startup2项/8.675s、duplex1项/5.314s通过。188项清单`c3-pcm0-source.sha256.json`自身SHA为`6fd473d3b717e29ef813f66268d9ff3ffbcb0d7ba099f49a121485628f7aacca`，本地冻结HEAD逐项相同，双目标依赖锁和提示音包保持；原规格复审PASS，独立质量复核进行中。

374ea95随后通过独立质量复核，未见P1/P2或阻断项；该批准仅针对诊断代码，主机证据不能证明实机ISR时长或故障已修复。下一步推送精确源码并重跑六目标CI及实际产物检查，之前58的构建/安装结果不能替代新源码证据。当前没有再次USB操作；Go58/PID1304344及gateway4139310的`/healthz`只读复查正常、active0，不因固件诊断重启服务。

### ee4249d六目标和实际产物通过，受控启动采集进行中

已推送精确源码`ee4249da3a8628d7553579de9a7245d660d15e83`（诊断代码374ea95及交接文档）。[experiment41276195174](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41276195174)五目标及[stock41276195166](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41276195166)均success；独立核对push事件、head及workflow路径一致。C3 job124758740588的253项完整主机测试/154.487s、上游110项/5.467s均OK无skip，原生编译/链接/校验/打包通过。

全部六份实际ZIP均经GitHub artifact ID/digest/字节数、CRC及全文件解包核对。五个非PCM产物沿用原验证逻辑，仅重钉source、目录和实际artifact ID/digest；云端`task4a-actual-artifacts-ee4249d/summary.json` SHA为`8f7c1ddfcc411344d761b779a6fa376f83cf019eec8e52f9ddf0bd0d484dfffc`，原网络双源、完整源库存/config、实际ESP镜像/分区/资源、flash/merged/release均通过。四patched S3仍为f0568dd锁，stock仍为原manifest的b26df81锁。

C3 artifact13071118358为24262443B，ZIP SHA`d95f8ab8152ff83aebe85467c3142a75135ab4f69a2bd4b83d07302f881dcc56`。冻结检查器在云端2.70s/exit0完整重建121项保留输入；原assets/分区、e178原生锁、745提示音包保持。app2238880B、SHA`28bfee4b388143130d83b75eda994402611e561814d34fa0e8b420a216ffb520`；精确ELF SHA`b289999d59024331313670680b693b277b9c3e150f53fc073d2ea68e83b1af9b`等于app描述符。新增9份诊断源与reviewed374逐字节一致，8个overlay prepared副本、6个patched hook、精确ELF的6个实际符号及7条诊断格式串均核对，artifact库存221文件保持。`task4b-ee4249d/actual-c3-ee4249d.json` SHA为`b5e2d96ffcb9bcce6f4be6a95a440cea88bcbb191849e01180d892bbc96b2772`。

私有安装工具仅SOURCE重钉及测试/README更新，检查器逐字节未改；51项/9.390s无skip通过，规格→质量均PASS。root依据独立run/artifact事实建立外部binding；文件SHA`9af3d57ae35995e806444c6c06e180a54109fa839f612d696432cc458d8c1fd8`，规范内容SHA`02fab7d6b7bb1280624b6ac9ea78a20fd1f39b46c0f485b92618eb21c914dc01`。云端带binding重构和Mac标准库client gate均通过。第一次未带install开关的命令仅完成只读检查，exit0且未开串口；随后13:28实际带install开始重新备份，唯一串口由该工具持有，不得另开或复位。私有目录`usb-ee4249d-20261101`、日志`install-ee4249d-usb.log`；本段不代表新候选启动成功或插话修复。

#### 新诊断确认RxPair/Cadence，恢复进行中

新鲜三域备份及写前补集MD5/字节SHA通过；只写app0 `0x20000+0x223000`、trailer `0x650000+0x16c000`、selector0 `0xd000+0x1000`，各域逐字节回读及写后补集通过后才首次复位。13:43的精确ELFb289999d启动中，首故障记录为source21/RxPair、pair3/Cadence，timestamp1,622,845µs，前次RX1,596,972µs，间隔25,873µs；active/armed/open=0/1/1，TX时间1,596,777µs，TX/RX均31,920样本。首次故障发生在没有业务capture时，原Pair原因已保存，未被后续Setup清理覆盖。

提示音partition/map/open、两次真实PSA hash及32B匹配、SRC、FD AEC和两个xTaskCreate均成功。StartComplete1,529,950µs时内部free97,568B/largest86,016B；OTA拒绝打印时free45,900B/largest31,744B/min34,800B。没有panic、bad_alloc或已观察到的初始化分配失败；这只是启动资源证据，不是实际负载通过。硬件查询返回0/0、失败mask0；不把未采集的actual全0误当实际配置。

日志1589ms Lite Initialized、1619ms PHY版本、1649ms Saving calibration，3289ms拒绝OTA确认并esp_restart，随后自动回退stable app1。ESP_LOG与esp_timer的时间原点不同，不能直接把log1649ms与fault1622ms比较；正核固定SDK的PHY→NVS/Flash保存和非IRAM中断路径，不仅凭时间相关认定因果。`first-boot.proof.json`记录8.144秒完整故障尾部、16056B，日志SHA`d259c460e61512c83034c9884b0ca6e7f636b66cbef7e19cd4a28d9e86a1dbdc`。未请求真人语音测试。

13:53恢复工具已完成：`stable_restored=true`，三个授权域原字节恢复，eFuse/稳定路径通过，稳定app1 ready后15.014s观察无故障；候选失败exit1且finally关闭串口。保护0..0xd000有变化，明确`whole_flash_restored=false`，没有写回未授权NVS。

随后独立零Flash写审计读取128KiB前缀，6593个变化字节全部在NVS9000..d000；35项含namespace记录前后相同（8个namespace头、27项实际值），仅phy/cal_data的逻辑值变化，其余26项含Wi-Fi/身份/音量值保持，不输出值。稳定app1再次ready后15.109s观察通过，审计exit0/finally关闭串口。私有证据在`nvs-read-audit-ee4249d/{logical-diff.json,result.json,stable-audit-boot.log}`；原始备份及日志均私有，无NVS值/音频/产物入Git。

固定SDK源码已核闭PHY保存→NVS→esp_flash_write/erase→默认OS cache guard→禁非IRAM IRQ的可行链路；I2S_IRAM_SAFE=n使当前GDMA channel不带IRAM flag，底层GDMA handler位于IRAM不能弥补。精确ee ELF的Sent/Received/Complete及Pair/Playback/fault独立helper均在Flash，应用部分约3400B；另需SDK回调约316B与数据/包装，必须重新核SRAM。本次25.873ms仍是软件回调时间异常，不是物理DMA停止证明。

下一步为C3-only完整IRAM闭包＋四点cache-span的单候选假设检验。拒绝旧spi_flash_guard_set观测：固定SDK新esp_flash OS函数直接调用disable/enable函数，不经过该guard。使用固定SDK私有linker包装，所有原调用保持、默认透明，记录同一esp_timer的保守cache-off内部区间与其中RX/TX次数；实际ELF的重定位/指令必须证明包装归向及代码/数据闭包。先源码和云端审查、六目标实际产物及新SRAM预算，再做受控实机；不改变门禁/阈值或提前宣称PHY保存是唯一根因。详见执行计划Task6。

## 构建接线与产物绑定进度

Task4A已接入C3专用CMake生成依赖：生成header与独立trailer，且只替换锁定网络组件的两个编译输入；原managed sources与依赖锁不改。PCM pack通过冻结IDF的四参数flash-image API注册到0x650000，实际`flash_args`、JSON、合并镜像和release必须一致。其他四种experiment与stock必须保留原网络编译源，六目标均保留compile_commands；C3主产物额外保留精确ELF/map与全部受控源。

质量复核发现的三处P2已实现修正，并在cbc49de由同一规格与质量复核者依次PASS：按规范偏移和解析后的路径绑定实际刷写文件，拒绝同名重定向或额外输入；保留并交叉核对真实`flash_args`与OTA初始化文件；按原生raw merge的base0、FF填充、无尾部padding规则重建整份镜像，拒绝空隙篡改和附加字节。已用四份可信历史S3合并镜像验证格式规则；其flash_args是按原JSON重建，旧C3 app与新pack的组合仅验证文件绑定格式，不作为新候选或目标构建证据。

验证目录为云端`task4a/`。`closed-full.log`、`closed-upstream.log`仅对应3ff3951基线；cbc49de对应`flash-fix-focused-final.log`、`flash-fix-upstream-green.log`及`flash-fix-provenance.json`。188项`flash-fix-source.sha256`清单SHA为`6d0c1f364a28d290ae56683cb117cfa820b800ade1a4ca564f5428d99cb7cd0c`，前后188/188一致。来源清单统一排除Python bytecode缓存，真实源码漂移仍拒绝；下载缓存只复用已验证SHA的原归档，各测试类仍重新准备源码。第一次上游运行因缺少已有任务目录的python别名出现6项exit127，补齐PATH后110项通过，期间未改源码。

已推送109c92f15c2fe0980b0b58b087ff14a3fd65fbd2触发六目标CI：[experiment41270102646](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41270102646)、[stock41270102652](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/41270102652)。四个S3 experiment及stock全部success；C3 job124740947461的完整238项主机测试/202.302s、上游110项/7.461s均OK且无skip，随后原生audio_service.cc:6找不到`mbedtls/sha256.h`而失败。实际编译行已包含mbedtls和tf-psa-crypto公共目录；冻结IDF的owning CMake明确mbedTLS4，原因是旧SHA API不再存在，不是缺少组件包含路径。正在核对公共PSA API并最小修正，不借用OpenThread内部旧库、不改变依赖锁/包格式。主机回归通过不代表C3原生构建通过。

随后32024dc7693001652c90ca25086038f9f76c5579仅更换C3公共include和SHA适配器：显式检查`psa_crypto_init`、`psa_hash_compute`返回值及精确32B长度。固定IDF/mbedTLS owning sources确认重复init安全、系统启动初始化及`MBEDTLS_PSA_ASSUME_EXCLUSIVE_BUFFERS`的借用输入路径，不为1.49MB映射包复制整包；adapter仅32B摘要和长度变量。真实OpenSSL SHA承接PSA契约，9种故障分别在O2/ASan测试。云端`task4a-native-fix/psa-red-sanitized.log`的30项/61.283s有19个预期失败，`psa-green.log`30项/61.197s全通过；首次缺少该云主机既有ASan preload的环境失败不计作RED。186项`source.sha256`清单SHA为`475805bda67505b69b46fb74f54ead0b46cef255ce54d59796a5e22fad1ae76a`，前后186/186一致，15项API证据摘要匹配，C3/S3原生锁和pack745保持。独立规格后质量均PASS，但没有云主机现成精确IDF工具链，故仍需新六目标CI实际验证，不将专项通过称为原生构建修复完成。

USB仅做枚举，仍见`/dev/cu.usbmodem14201`及原C3 serial；没有开串口、复位或写入。Go新二进制已在云端独立`task5-go-build/`编译，SHA为`6ae8f8d86525247c5d87b57f03e4a667ecfbce64cc8677deb7a83bf19efbe46a`，110项Go/mod/sum输入与此前通过完整回归的server目录逐项一致；尚未替换正式二进制。正式配置、身份和记忆的摘要只保留云端私有目录，没有复制配置内容到Mac。

Task4B私有工具30项云端失败注入/5.570s通过，独立规格复核仍要求修正：保护区漂移不能阻挡先disarm候选、loaded判定需要本会话前后累计统计的增量、完整性证明要标明最后实际回读在两次启动之间而非宣称覆盖后续监控。完整云端产物重建/转换与Mac标准库approved-client gate已拆开，后者须以root独立核验的外部binding和逐文件摘要批准，不能依赖Mac安装转换库。该工具尚未通过USB使用前置复核，实际新bundle检查仍待C3构建通过。

## 云端依赖安装附带重启核对

Task4安装云端CMake时，apt的needrestart自动重启了保留的Python测试单元`family-robot-realtime-output-live.service`，超出该步骤预期；未主动执行服务部署或重启命令。安装日志位于云端`logs/task4-cmake-install.log`，needrestart的stderr另行保留。已停止继续包安装，后续云端apt使用`NEEDRESTART_MODE=l`只列出待重启项并保留stdout/stderr。

2026-11-01核对：正式Go仍PID4139298、8000入口仍PID4139310，二者启动时间均为10月31日13:00:45、NRestarts0；18040为Go09d22e6、ready/记忆就绪true、active0，8000health为ok。Python测试单元为PID1202653、11月1日10:49:35启动，127.0.0.1:18032健康检查ready=true/busy=false。没有再执行重启来“撤销”该事件，C3及其他实体设备未操作。
