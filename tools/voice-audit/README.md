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

`--scenario resume-twice --receipts` 另需机器合成的 `continue_restart.wav`：明确请求继续原任务，并允许位置未知时从一重读、终点仍为五十。它会在第一次恢复播放期间再次插话，再查询任务并恢复。此用例验证任务归属和重复打断，**从一重读不是准确接续通过**。每段输出新增 `phase`，便于独立检查生成内容。

`--interrupted-final-delay 1500ms` 可延迟模拟的中断最终回报（需 `--receipts`，范围0至5秒），正常最终回报仍立即发送。延迟通过同一事件循环调度，不暂停上行输入，也不表示真实设备延迟。

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

## 接续状态与开源框架对照

本轮[总报告](../../docs/verification/2026-10-23-resume-framework-root-cause.md)区分真实模型、原版框架模块、合成播放和实机。固定上游commit、源码/依赖摘要与原执行脚本摘要保存在[resume-reference-lock.json](resume-reference-lock.json)。上游checkout、依赖和原始结果不随本仓库分发，需分别保留/遵守原许可；Pipecat为BSD-2-Clause。本轮没有将它们的产品实现复制进Go网关。

Go审计只用回环httptest和内存PCM，通过overlay调用当前生产方法，不连模型、不改正式文件：

```sh
python3 tools/voice-audit/resume_state_audit.py --out artifacts/new-resume-audit --race
```

这三项是**当前行为characterization**，不是修复验收。产品修复后，相应“观察现有缺口”的断言可能预期失效。`resume_state_test.go.in`不会进入日常Go包编译。

Pipecat须在单独环境安装固定源码的基础包（实测Python3.13.3，无GPU/模型/extra依赖）；传入同一checkout，输出目录必须不存在：

```sh
<pipecat-venv>/bin/python tools/voice-audit/pipecat_resume_probe.py \
  --source-root <pipecat-checkout-at-3dfcd17a> --out artifacts/new-pipecat-resume
```

脚本运行5个有/无PTS及sink落后的真实框架场景，不调用模型。它还截获原版Realtime方法发出的truncate事件；没有远端ACK或设备回报。不要将模拟sink接受数字当作人实际听到的数字。上游320项测试需另外安装pytest/pytest-asyncio/pytest-xdist，命令见分报告。

TEN脚本需要隔离的runtime-root布局：`upstream/`为锁定仓库源码；`packages/ten_ai_base/`为官方0.7.41包，`packages/ten_ai_base-0.7.22/`为0.7.22包；根目录保留相应`ten_ai_base-<version>.tpkg`及`download_manifest.json`（commit/files取reference-lock中的TEN项）。它核验41个源码文件及两种包的归档SHA，使用native runtime与厂商连接的替身，运行原版Python控制代码。依赖与准备细节见[TEN报告](../../docs/verification/2026-10-23-ten-resume-comparison.md)。

```sh
<ten-venv>/bin/python tools/voice-audit/ten_resume_probe.py \
  --runtime-root <prepared-ten-root> --suite chained --out artifacts/new-ten-chained
<ten-venv>/bin/python tools/voice-audit/ten_resume_probe.py \
  --runtime-root <prepared-ten-root> --suite realtime --out artifacts/new-ten-realtime
```

小智脚本使用固定源码的扁平目录，包含`dialogue.py`、`connection.py`、`abortHandle.py`、TTS的`dto.py`、上游`test_dialogue.py`；路径/来源见[报告](../../docs/verification/2026-10-23-xiaozhi-resume-comparison.md)。校验字节后动态导入模块/提取原方法，不启动完整服务。只跑两个方法级场景不需第三方依赖；加`--upstream-tests`需pytest及pytest-asyncio。

```sh
python3 tools/voice-audit/xiaozhi_resume_probe.py \
  --source-root <pinned-flat-python-source> --out artifacts/new-xiaozhi-resume
```

## 千问合成上下文消融（会调用计费API）

`native_resume_ablation.py`依赖aiohttp，直接测试原生API，**没有设备或音频播放**。提供机器合成16kHz单声道PCM16的`continue.wav`和固定JSON plan。`--env-file`可从服务器受保护文件读取配置；省略时使用继承环境中的`QWEN_REALTIME_URL`、`DASHSCOPE_API_KEY`、`QWEN_VOICE`、可选`ROBOT_INSTRUCTIONS`，不要把凭据放到参数里。

```sh
<aiohttp-venv>/bin/python tools/voice-audit/native_resume_ablation.py \
  --plan tools/voice-audit/fixtures/resume-context-20261023.json \
  --fixtures <synthetic-wav-directory> --out <new-private-output-directory> --trials 2
```

plan的已播1–8是人为给定的oracle，不是生产映射。每组2次、固定顺序、请求相同seed，不证明服务端确定性。具体混杂因素与原8次调用的执行身份见[消融报告](../../docs/verification/2026-10-23-native-resume-ablation.md)。portable副本增加CLI和错误退出行为，只另用本地fake provider检查成功/拒绝退出码，未用它重跑并冒充原8次实连。
