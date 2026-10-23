# 固件唤醒发送失败恢复：主机验证

2026-10-23 完成源码修复与主机回归。本轮未操作 USB、串口、录音、喇叭或刷机；ESP32 两款候选构建及现场验收另行记录，主机通过不能代替这些证据。

## 修改与接口

修复由 `firmware/patches/0003-wake-send-recovery.patch` 持久保存，按顺序接在现有 0001/0002 后；没有只修改临时准备树。缓存首/中/末包 `SendAudio` 返回 false 时立即停止后续网络发送；在批次仍有效时继续消费本次异步编码到 EOF，再恢复，避免前一轮编码未结束就复用其队列/任务存储。不再发送 detect 或进入 listening。detect 控制失败同样终止。初次、延迟的 `StartListeningAudio` 和 listening 中再次唤醒的 start 控制失败均恢复。

恢复函数清除 pending start/popup，关闭上行及语音处理，关闭音频连接，在关闭所排入的回调之后调度回到 idle 并重新启用唤醒检测，防止旧 close 回调取消刚开始的重试。OpenAudioChannel 失败和无效 wake batch 也走恢复；无效 batch 保留原有 AEC 降级调用。缓存关闭时仍不发送音频缓存/detect，并保留成功后的 popup 行为。

`Protocol::SendWakeWordDetected`、`Protocol::SendStartListening` 从 virtual void 改为 virtual bool，原样返回 `SendText` 的结果，JSON 内容未修改。全准备源码检索只有基类的两处定义，没有派生 override；应用调用点为 ContinueWakeWordInvoke 一处 detect、StartListeningAudio 和 HandleWakeWordDetectedEvent 两处 start，均处理失败。外部未纳入该锁定源树的 override 需要同步返回类型。

## RED → GREEN

新测试 `firmware/tests/test_wake_recovery.py` 提取并编译准备源码中的实际方法：BeginWakeWordInvoke、ContinueWakeWordInvoke、StartListeningAudio、SetListeningMode、HandleWakeWordDetectedEvent、两个 Protocol 控制序列化方法及 ScheduleAudioChannelClosed。修复后也编译实际 RecoverListeningFailure。测试没有复制业务分支或用源码 grep 代替行为断言；硬件、任务队列与 transport 使用明确的 host fake。

RED 对象为 `.superpowers/firmware-analysis-20261022/exact-17bb1e0`。命令：

```sh
FIRMWARE_WAKE_SOURCE_ROOT=.superpowers/firmware-analysis-20261022/exact-17bb1e0 \
  python3 -m unittest discover -s firmware/tests -p test_wake_recovery.py -v
```

四种配置都编译成功并出现预期行为断言失败：有缓存时音频失败后仍继续发送；无缓存时 start 控制失败后仍处于 listening。日志 `artifacts/wake-fix-20261023/firmware/red.log`。最初测试对配置下未使用参数启用 Werror 导致两种无缓存配置编译失败；测试增加 `-Wno-unused-parameter` 后重跑得到上述四种真实行为失败，最终 RED 日志不把编译错误算作复现。

GREEN 默认从校验和锁定的 v2.5.2 归档重新准备完整源码，应用全部 patch 和 overlay，四种软件 AEC 开/关 × wake cache 开/关均通过。编译使用 C++17、Wall/Wextra/Werror、ASAN/UBSAN、`-fno-sanitize-recover=all`；运行设置 `ASAN_OPTIONS=halt_on_error=1:abort_on_error=1`、`UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1`。

覆盖首/中/末包失败立即停止、detect 失败、start 失败（包括 pending 标志清理）、再次唤醒 start 失败、Open 失败、无效 batch、失败后新一轮唤醒成功、正常路径、有效空 batch、错误状态拒绝、旧连接 close 回调不影响重连后的 listening、三个 listening mode JSON 序列化。失败场景断言 idle、连接关闭、唤醒检测恢复、未启用 voice/uplink 且没有 popup。空有效 batch 延续原有合法行为，不新增缓存内容判断。

完整命令：

```sh
python3 -m unittest discover -s firmware/tests -v
```

结果：**72 项通过，11.124 秒**。四个配置测试包含在 72 项内，不能额外累加。日志 `artifacts/wake-fix-20261023/firmware/full-tests.log`。

## 准备链与证据边界

使用本机归档 `.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz`，校验值由 `firmware/upstream.lock.json` 检查：`b3460397122eff7edeb8c07bd9c26e2c216b24cc2e5c2d836ec2209ba5be0c63`，上游 commit `49b4380b3faa0ceadd28a3e78aaf0f311c46c57e`。独立保留的全新准备树为 `.superpowers/wake-fix-20261023/firmware/final-reviewed-v2`；完整 provenance 和源码摘要位于 `artifacts/wake-fix-20261023/firmware/prepared-source.json`。

0003 patch SHA256：`d140cb2e4751456f3f8b3788586bee2afacb8d334d9a66da22165be1559c058c`。

这证明锁定上游的完整 patch 链可应用，以及实际生产方法在主机故障注入下满足恢复契约。仅凭本节主机证据不能证明 ESP-IDF 6.1/ESP32S3 编译、板上 FreeRTOS 时序、真实 Wi-Fi 丢包、PSRAM 压力或远场再次唤醒成功率。随后普通与软件 AEC 两款完整 MCU CI 均通过，接口与声明的完整编译及离线镜像核验见[实施结果](2026-10-23-wake-recovery-results.md)；板上与声学验收仍待进行。

## 独立审查后的第二轮 RED → GREEN

独立审查发现初版提前返回会在非软件 AEC 的异步 encoder 仍生产后续包时恢复唤醒；另外旧 close 回调排队期间立即恢复会让 Run 中先处理 WAKE、后处理 SCHEDULE 的顺序取消重试。补充测试先对初版准备树 `final` 运行：两种缓存配置在“必须消费终端”断言失败，四种配置的 close-order 场景在“回调前不能恢复唤醒”断言失败，记录于 `review-red.log`。

修正后首/中/尾发送失败不再继续上传；批次仍有效时消费直到本次 EOF。host fake 新增独立 producer 线程与条件变量，每次 Pop 才允许 encoder 生产下一个包，并将终端作为独立步骤；断言没有在终端前恢复检测。另通过真实 HandleWakeWordDetectedEvent/ScheduleAudioChannelClosed 模拟 close 尚在队列、WAKE 优先处理，再排空回调并成功重试。四配置 ASAN/UBSAN GREEN 记录于 `review-green.log`。

这是消除本补丁新增的提前复用窗口，不声称修复上游非软件 AEC encoder 原有 EOF 入队到 vTaskDelete 之间的任务退出边界。最初编写异步 fake 时漏启动 producer 导致一次 host 测试等待，已终止该次进程并修正；最终 RED/GREEN 均使用启动成功的独立线程，运行设 20 秒超时，等待不计作产品故障。

复核又补充“先 SendAudio 失败、后 batch validity 翻转”组合注入：先对 `final-reviewed` 运行得到未执行 AEC 降级的 RED（`review-combined-red.log`），最终先处理 invalid+AecOff，再处理 upload_failed，四配置 GREEN。validity 无效时保留原 break/降级路径，不强行继续消费：实际软件 AEC 的 generation 失效读取可能已经返回 false，旧 worker 不保证再发布旧批次 EOS；非软件 AFE 的 batch validity 恒为 true，因此有效批次排空覆盖本补丁新增的危险窗口。
