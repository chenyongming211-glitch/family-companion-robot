# C3 PCM 隔离评估工具

本目录不参与普通服务或固件构建。测试以 `.go.txt` 保存，避免普通 `go test ./...` 将它当成独立产品包。`apply_trial.py` 只为云端评估归档增加原型支持，不改当前仓库的 `internal/`，没有正式服务开关或部署命令。

设计和结果分别见[评估设计](../../docs/superpowers/specs/2026-11-01-c3-pcm-assessment-design.md)与[验证报告](../../docs/verification/2026-11-01-c3-pcm-assessment.md)。基线为 `2294240`；本目录的脚本和测试另行复制进评估目录。运行依赖 Go、libopus、现有重采样依赖及 Python3，全部在云端使用已有环境。

在云端将干净的基线源码归档解包至一个全新的目录后执行以下命令。`trial_root` 必须指向该源码副本，`assessment_tools` 指向本目录的云端副本；不得指向运行中的服务目录或Git checkout。

```sh
trial_root=/opt/family-robot-validation/c3-pcm-assessment-repro/source
assessment_tools=/opt/family-robot-validation/c3-pcm-assessment-repro/tools
mkdir "$trial_root/.assessment-bin"
ln -s /usr/bin/python3 "$trial_root/.assessment-bin/python"
export PATH="$trial_root/.assessment-bin:/opt/family-robot/toolchains/go1.27.3/bin:$PATH"
cp "$assessment_tools/pcm_trial_test.go.txt" "$trial_root/internal/realtimelive/pcm_trial_test.go"
cd "$trial_root"
go test ./internal/realtimelive -run '^TestPCMTrialNegotiation$' -count=1
# 基线应在 valid PCM 子用例失败：格式尚未支持。先保留此失败，再显式应用原型。
touch "$trial_root/.c3-pcm-assessment-only"
python3 "$assessment_tools/apply_trial.py" "$trial_root"
gofmt -w internal/protocol/protocol.go internal/realtimelive/xiaozhi_entry.go internal/realtimelive/xiaozhi_wire.go internal/realtimelive/session.go internal/realtimelive/pcm_trial_test.go
go test ./internal/realtimelive -run '^TestPCMTrial' -count=1
go test ./...
go test -race ./internal/realtimelive -run '^TestPCMTrial' -count=1
```

旧共享 `ParseHello` 保持 Opus-only；仅隔离Realtime入口显式调用原型解析器。PCM 使用version1、`pcm_s16le`及`pcm_s16le_v1`，16k输入1920B/24k输出2880B，均60ms。没有隐式格式回退。输出ASR处理实际发送的PCM，最后一帧按1440样本补齐，不加入Opus专用312样本补尾。

脚本要求显式 marker、拒绝Git checkout；先验证所有源锚点再写文件，重复应用或源锚点不匹配会拒绝。已有云端验证证明重复应用失败后四个文件哈希保持一致。试验只使用httptest回环、假供应商和内存合成样本，不调用真实模型、不保存声音。

通过这些测试仅说明云端原型的协议与状态兼容性。C3的固定字节队列、本地提示音、FD/AEC资源和实际声学效果仍须单独实现并测量；本目录不能直接生成或安装可交付C3固件。
