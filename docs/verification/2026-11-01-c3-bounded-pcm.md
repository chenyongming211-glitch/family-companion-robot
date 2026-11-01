# C3有界PCM实施与验证

当前状态：实现进行中，尚未完成精确目标构建、安装或声学验收。C3仍为稳定按键版6b79e4c/app1；自然插话尚未修好。正式服务未部署本轮PCM适配，M5/微雪保留Opus。实施门槛见[设计](../superpowers/specs/2026-11-01-c3-bounded-pcm-design.md)和[计划](../superpowers/plans/2026-11-01-c3-bounded-pcm.md)。

## 已完成的源码和云端验证

| 范围 | 提交 | 已核对证据 |
|---|---|---|
| Go显式PCM协商与共同Session接线 | 5d90ef7 | 全仓376主测试/869含子用例通过，2可选skip；PCM及race各10主测试/30含子用例通过；独立规格和质量复核通过 |
| 固定PCM池与提示音包基础 | f0ef155 | 云端16项测试通过，包含O2、ASan/UBSan、全部21项真实源提示音及517帧读取；独立规格和质量复核通过 |
| C3受限网络构建输入 | d9b4cac | 云端7项Python测试通过，实际生成WebSocket39、EspTcp12、辅助器14项分别在O2与ASan/UBSan通过；独立规格和质量复核通过 |
| 实际PCM媒体、流式提示音、FD和资源观测接线 | d156e8d、d6c6ff3、14df705 | 最终25项源码摘要与云端一致；独立规格和质量复核均在14df705通过。d6完整206项/388.211s通过且无skip；最终PCM控制修正的20项专项/67.462s及4配置8次旧协议构建/运行通过。最终整合源码的完整回归仍属Task4/CI门槛 |

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
