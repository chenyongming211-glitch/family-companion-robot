# Speaker lab（隔离 Linux 实验）

本嵌套 Go module 只在云端 Linux 运行，使用 sherpa-onnx Linux v2.0.1。
根仓库的普通 Go 构建不加载 ONNX 运行库。当前只有有界特征提取与私有 IPC，
没有接正式 Realtime、登记、人物数据库、会话队列或跨会话识别。

## 云端构建与探测

Go 1.25+、C 工具链；模型和 WAV 均使用显式绝对路径。权重、原始音频、二进制和运行日志不进 Git。

```sh
cd /path/to/source/tools/speaker-lab
go mod download github.com/k2-fsa/sherpa-onnx-go-linux/v2
go test -v -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build -o /private/evidence/speaker-lab .
/private/evidence/speaker-lab probe \
  --model /private/models/model.onnx \
  --sha256 <64位小写SHA256> \
  --wav /private/synthetic/fixture.wav
```

不带子命令时退出；不会默认监听。`probe` 用当前可执行文件启动子进程 `worker`，
冷启动（含权重摘要校验和模型加载）最多 10 秒，每次请求含 IPC 和推理最多 2 秒。
超时、取消或协议错误会终止并回收实际子进程。每个 worker 固定一个模型，CPU、1 线程、debug=0，串行处理。
父进程给 worker 只传最小 `PATH/LANG/LC_ALL` 环境，不传服务密钥；子进程输出丢弃，原生库 stdout/stderr 另行关闭到 `/dev/null`。

成功 stdout JSON 仅包含：`model_sha256`、`dimension`、`samples`、`inference_ms`、
`cold_start_ms`、`overlap_assessment: "unavailable"`、`quality_evidence: "fixture-only"`。
失败只有固定错误码；没有向量、PCM、文件路径、姓名或原文。单人 fixture 标签不是重叠检测证据。
不据此判断片段是否语音、单人或可登记；原始音频须另行具备获得授权的来源和质量事实。

WAV 由有界 Go 解析器读取：RIFF/WAVE、单一 16 字节 PCM `fmt `、16kHz、mono、16 bit、
单一 `data`，1～128000 samples。RIFF 长度必须精确，拒绝截断/尾随字节、重复 fmt/data、格式不符和超限；
元数据连同 PCM 总计最多 321536 字节。输入必须是无符号链接的 regular file，拒绝 FIFO/设备等输入。
不会调用原生库的无限制 `ReadWave`。

## 私有 worker 与协议 v1

```sh
# 调用者先建立属于当前用户、权限恰为 0700 的目录。
/private/evidence/speaker-lab worker \
  --socket /private/0700-dir/worker.sock \
  --model /private/models/model.onnx \
  --sha256 <64位小写SHA256>
```

只允许显式 Unix socket，没有 TCP。路径须是规范绝对路径且每级没有符号链接；
父目录须属于当前 UID 且权限为 0700，socket 为 0600。
拒绝已有任何同名路径，不自动 unlink；正常结束仅清理自己创建的同一 socket inode；父进程也保留连接前后核验的原 inode，
路径被替换时保留替换项，启动失败且未确认归属时仅尝试删除空临时目录。
模型必须为无符号链接的 regular file、大小 1～100 MiB；摘要完全一致后通过已验证文件描述符加载，
不重新打开可被替换的模型路径。冷启动成功后连接收到 `ready` 帧，包含固定摘要和模型实际维度（1～1024）。

每帧：4 字节 big-endian JSON header 长度（1～4096），然后 UTF-8 JSON，再按 header 长度读 binary payload。
所有 JSON 字段必需；拒绝未知字段、重复字段、null、类型不符和尾随 JSON。

请求 header：

```json
{"version":1,"id":1,"session":"session1","scope":"scope1","generation":1,"start":0,"end":16000,"model_sha256":"<sha256>","sample_rate":16000,"pcm_bytes":32000}
```

`id` 是 worker 生命周期内严格递增的正 uint64；不同会话可按顺序共享 worker，worker 不保存会话模板。
`session/scope` 仅为 1～64 个 ASCII 字母、数字、下划线或连字符，`generation` 为正 uint64。
区间为绝对半开 samples `[start,end)`；`end>start`、最多 128000 samples；PCM 为 s16le mono binary，
`pcm_bytes=(end-start)*2`，最多 256000 字节。模型路径不来自请求；请求摘要必须等于启动锁定摘要。
重复或迟到的 ID、格式错误均关闭连接；各会话区间顺序由调用方 Tracker 校验。

结果 header 回显 `id/session/scope/generation/start/end/model_sha256`，另带 `version:1`、
`kind:"result"`、`dimension`、`vector_bytes`、非负 `inference_us` 和 `code:"ok"`；
payload 为最多 1024 个 little-endian float32。特征不可用时为固定 `code:"unavailable"` 且没有 payload。
父进程把每项归属、摘要、维度和长度与当前请求/ready 精确匹配，拒绝 NaN、Inf、全零和截断向量。
向量只允许存在于此受保护 IPC 和父/子进程内存；不得打印、落盘或写报告。
每次读请求/写响应的绝对期限是 2 秒；等待新请求的连接空闲 2 秒后关闭，worker 可接收新连接。

同包后续评估可以复用 `readWAV(io.Reader)`、`startWorkerBinary(ctx, executable, model, sha)`、
`workerProcess.request(ctx, requestHeader, pcm)` 和幂等 `Close()`；这些 API 由单个串行调用者拥有，
不提供无限队列或并发 goroutine 推理。处理失败后应重新创建 worker，不能复用被退休的连接。

CLI 父进程和 worker 均要求 core dump 上限为 0。现有服务器 swap 不改变，因此 RAM 处理不等于已证明不会进入 swap；
当前实验仅用明确的合成 fixture，不处理真人声纹。

## 模型 opt-in 检查

默认测试不要求模型，真实模型相关用例明确报告 SKIP，不能当作模型执行通过。
显式构建二进制后，在云端以合成 WAV 运行：

```sh
SPEAKER_LAB_TEST_MODEL=/private/models/model.onnx \
SPEAKER_LAB_TEST_SHA256=<64位小写SHA256> \
SPEAKER_LAB_TEST_WAV=/private/synthetic/fixture.wav \
SPEAKER_LAB_TEST_BINARY=/private/evidence/speaker-lab \
go test -v -count=1 -run 'TestReal(ModelProbe|Worker.*)OptIn' ./...
```

模型探测校验安全 JSON；真实 worker 另测环境隔离、SIGTERM 正常退出、socket 清理及替换 socket 保留。
协议、WAV、路径权限、模型摘要和子进程超时测试独立运行；超时 fake extractor 只用于确定性控制协议/回收路径，
不能代替真实 ONNX 探测或说话人质量评估。
