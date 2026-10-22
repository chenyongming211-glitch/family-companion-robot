# 无声语音链路审计

这些工具只用于隔离测试，不是另一个语音服务端。不访问手表、麦克风或扬声器。只使用自己生成的16kHz、单声道、PCM16 WAV；不要填入现场录音。

## Go 协议探针

编译：`go build -o artifacts/voice-audit ./tools/voice-audit`。

启动独立网关时设置 `ROBOT_LISTEN_ADDR=127.0.0.1:18001`，保留同一模型和配置。凭据由受保护环境传入，勿放在命令行或提交文件。探针需要 `ROBOT_SMOKE_URL=ws://127.0.0.1:18001/xiaozhi/v1/`、`ROBOT_DEVICE_TOKEN`、`ROBOT_ALLOWED_DEVICE`；它拒绝非回环地址及正式8000端口。网关实例与探针使用相同设备认证，但不连接实际设备。

fixture目录中放 `hello.wav`、`count50.wav`、`stop.wav`、`query.wav`、`continue.wav`，内容分别为唤醒词、完整数到五十、插话数学问题、查询原请求、继续原任务。可在Mac用 `say -v Tingting -o <file.aiff> '<text>'` 静默生成，再用 `afconvert -f WAVE -d LEI16@16000 -c 1 <file.aiff> <file.wav>` 转换。每次模型调用可能计费。

```sh
artifacts/voice-audit --dir artifacts/synthetic-inputs --out artifacts/count.json --scenario normal --order cache-first
artifacts/voice-audit --dir artifacts/synthetic-inputs --out artifacts/interrupt.json --scenario interrupt --order cache-first
artifacts/voice-audit --dir artifacts/synthetic-inputs --out artifacts/wake-overlap.json --scenario wake-interrupt --order cache-first
artifacts/voice-audit --dir artifacts/synthetic-inputs --out artifacts/idle.json --scenario idle --order cache-first
```

输出文件必须不存在。退出0和 `transport_completed` 只表示预定协议阶段结束，**不表示接续语义正确**。应核对 `input_transcripts`、`generated_text` 与网关的暂停任务事件。`wake-interrupt` 在问候中就发数数指令，再打断数数；`idle` 验证当前两分钟空闲配置，不适用于修改了超时的实例。`cache-first` 使用缓存音频→detect→start，另有 `start-first` 控制变量。除唤醒缓存突发上传外，持续以60ms节拍上传语音或静音；不会在模型说话时停止上行。插话延迟字段从合成输入入队计时，含约一个60ms节拍的排队，不能称为实际语音起点或停声时延。

可选 `--receipts` 在隔离实例协商播放回报，用已接收帧计数模拟回报。虽然线上字段要求 `basis=i2s_dma`，这里**没有I2S硬件，结果必须标记为模拟**；默认不开启。探针不会执行设备MCP工具，遇到控制调用即失败。日志含合成对话文字，不能将工具改用于真实家庭音频后直接公开日志。

## 实际固件源码的主机测试

先按 `firmware/tools/prepare_experiment.py` 从锁定archive准备源码；传入已核验的准备目录。下面会创建新的输出目录、提取原函数或编译原缓存源码，不修改输入目录。

```sh
python3 tools/voice-audit/wake_lifecycle.py --source-root <prepared-source> --out artifacts/wake-audit
python3 tools/voice-audit/cache_stress.py --source-root <prepared-source> --out artifacts/cache-audit
```

唤醒工具将 `ContinueWakeWordInvoke` 原文放进故障注入stub，保留方法SHA256；退出2表示发现上传失败仍继续聆听，退出0表示这三条故障路径已拒绝，其他异常需看编译/运行日志。它不覆盖真实网络、FreeRTOS调度、完整状态机或声学唤醒。

缓存工具编译传入源码的环形缓存及epoch guard，用固定随机种子和标准deque作比较；同时测试分配失败和并发调用。运行ASAN/UBSAN，**未运行TSAN**；主机堆替代PSRAM，不验证ESP32性能或实时性。已有全套测试仍需运行 `python3 -m unittest discover -s firmware/tests -v`。
