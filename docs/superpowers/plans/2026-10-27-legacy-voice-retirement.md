# 旧 Python 语音实现与三段式链路退役清理方案

> **For agentic workers:** 执行时使用 `superpowers:executing-plans` 逐项落实与复核。本文件当前仅为方案，不启动删除、停服、卸载或刷机。遵守用户约定：直接在 `main` 工作，不创建分支或 worktree；云端运行后端检查，Mac 仅用于代码、Git 和客户端验证。用户明确安排执行后再进入实施，不因方案写完自动执行。

**Goal:** 删除已退出产品主路径的 Python Realtime 和 LiveKit 三段式专用代码、入口与部署配置，解除当前测试的旧源码依赖，保留 Go Realtime、两台设备、有效测试与历史资料。

**Architecture:** 先将旧 Python 对照结果冻结为独立合成测试样本，再退役旧客户端与旧服务，最后删除专用实现。当前 Go 生产代码、共用音频组件、OTA 与设备控制模块保持有效，不同时迁移服务器目录、改语音行为或开发新业务。

**Tech Stack:** Go、现有 libopus、Swift/C 客户端、systemd、Git；Python 仍可用于固件构建、辅助脚本和本次一次性样本导出，不再保留旧 Python 语音运行实现。

日期：2026-10-27。代码核对基线：`2f8406477696ee298ddeafa392431af7cb744e0b`。

状态：用户已认可清理旧 Python 语音实现及完整三段式运行链路的方向，本轮仅整理方案。设备控制开发有未提交改动；实际清理须等该模块交接后刷新基线。

---

## 1. 交付结果与范围

完成清理后的默认语音路径仍为：

```text
M5 / Waveshare → Go Realtime 设备入口 18041 → 千问原生 Realtime → 设备播放
Mac 测试客户端 → Go Realtime 回环入口 18040
设备激活与 OTA 引导 → 既有 Go 服务 8000
```

本次处理三类旧内容：

1. Python Realtime 18032 的运行实现、专用测试、研究入口和对照启动选项。
2. Python/LiveKit ASR→LLM→TTS 18010、Go 专用桥 18020，以及仅服务于该链路的客户端、探针和部署模板。
3. 当前 Go 测试读取旧 Python 实现的依赖，替换为可追溯、独立的冻结样本。

本次不实施：判题、提醒、长期记忆、精确同稿恢复、声纹、多设备声学仲裁、电源策略或暂停故障排查。清理不能被描述成这些旧业务能力已经迁入 Go。

不以“目录名字旧”“文件后缀是 .py”作为删除依据。旧代码从 Git 历史找回，不建立长期维护的第二份源码副本。

## 2. 静态核对结果和逐文件清单

配套文件：[逐文件清理清单](2026-10-27-legacy-voice-retirement-inventory.json)。该清单逐项记录路径、处理建议、条件、当前内容 SHA256 与 HEAD blob。

本次纳入 **128 个文件**：90 个有条件删除候选、9 个文档重写项、3 个测试依赖替换项、3 个现用入口局部修改项、21 个现用或共用文件保留项、1 个交接文档待更新项、1 个诊断文件暂缓项。90 个候选不是立即删除清单，也不代表实际执行后数量必然不变。

清单还记录了设备控制开发中的工作区状态。不得据此清理那些未提交文件；该信息只是执行前必须重新交接的证据。

| 范围 | 静态结论 | 处理 |
| --- | --- | --- |
| `services/livekit-agent/family_voice/`、`web/`、`tests/`、`requirements.txt` | 旧三段式运行、网页和专项测试 | 停服并核对消费者后删除 |
| `services/livekit-agent/README.md` | 多份历史验证链接指向它 | 保留短归档说明和必要锚点，去掉当前启动指令 |
| `cmd/livekit-device-gateway/`、`internal/devicebridge/` | 当前查到的桥模块 Go 消费者是前一个命令 | 先删除命令消费者，再删除桥实现与专项测试；README 留归档入口 |
| `deploy/family-robot-livekit-{device,dev}.service` | 专用于 18020/18010 | 核实已安装单元并退役后，删除仓库模板 |
| `tools/realtime-output-live/*.py` | 旧后端、其测试及依赖旧后端的重放/模型探针 | 冻结对照、退役 18032 后删除 |
| `tools/realtime-output-probe/` | 历史 18031 实验；其中 alignment 被当前状态测试读取 | 先解除测试依赖，再删除实现、客户端和专用测试；README 留归档入口 |
| `tools/livekit-audit/` | 旧 SDK 专项探针 | 删除实现、依赖表和专项测试；README 留历史说明 |
| `tools/mac-voice/open.command` | 旧 18010 浏览器入口 | 删除；README 改为退役说明 |
| `tools/native-voice/` | 混合了旧三段式 App 与当前客户端共用文件 | 按文件处理，禁止整目录删除 |
| `tools/realtime-output-live/` | 混合了现用 Swift 客户端与旧 Python | 保留现用客户端，局部清理 Python 对照分支 |

执行前重建引用关系：新文件、新消费者、内容哈希变化均重新审查。清单内 `defer_*` 项不进入自动删除；清单外文件也不能顺手删除。

## 3. 必须保护的内容

### 3.1 当前 Go 与固件

保留当前 `cmd/realtime-output-live/`、`internal/realtimelive/`、`internal/realtimeaudio/` 和 `internal/realtimestate/` 的生产实现及测试覆盖；本轮只改明确列出的测试适配。

保留 `internal/devicetools/`、`internal/gateway/`、`internal/qwen/`、`internal/recordstore/`、`internal/deviceauth/`、`internal/voiceidentity/`、相关旧 Go 模块以及 `cmd/gateway/`。8000 仍承担设备激活/OTA，当前设备控制还在复用旧 Go 工具，不能因名称“旧”一起删除。

18030 旧 Go Realtime 对照入口及 `tools/realtime-voice/` 不属于本次 Python/三段式退役目标，另行盘点。它的 `Sources/Fixture.swift` 还是当前客户端直接依赖。

固件补丁、overlays、构建/核验 Python、CI 和上游依赖不变，不刷机。`go.mod` 当前只有 websocket 与 x/text 两项直接依赖，没有可借本次退役删除的 LiveKit Go 依赖，不顺手升级或 tidy 依赖。

### 3.2 共用 Mac 组件

以下文件按现有路径保留，不在本轮做目录改名：

```text
tools/native-voice/OpusBridge.c
tools/native-voice/OpusBridge.h
tools/native-voice/Sources/AudioPipeline.swift
tools/native-voice/Sources/MicrophonePCM.swift
tools/native-voice/Sources/Opus.swift
tools/native-voice/Sources/PlaybackLedger.swift
tools/realtime-voice/Sources/Fixture.swift
```

同时保留 `NativeEndpoint.swift`、`Tests/LedgerSpec.swift`、`Tests/MicrophoneSpec.swift` 的共用测试组合，另设独立测试入口。`MicrophoneProbe.swift` 是可复用诊断，暂不删除。

旧三段式专用的 `VoiceClient.swift`、`DrainPlayer.swift`、`SelfTest.swift`、`main.swift`、旧 App plist/build/open 及相应专用探针、FixtureSpec/launcher 测试，按清单在替代共用测试入口建立后退役。

### 3.3 服务器共享目录

**不能整删以下路径：**

```text
/opt/family-robot-livekit-dev
/opt/family-robot-livekit-runtime
/opt/family-robot-livekit-runtime/bin
/etc/family-robot/server.env
```

当前 Go unit 的 WorkingDirectory、身份文件和二进制仍在这些名字带 livekit 的路径下。改目录是另一项部署迁移，不与本次清理合并。

系统 Python、ESP-IDF Python、libopus、当前 Go 二进制/CLI、设备凭据表、身份库、现用环境变量和 SSH 主连接均保留。即使停止了旧服务，也不能据此认定整套 venv、运行父目录或环境文件都无人使用。

### 3.4 历史资料与数据

保留设计、验证报告、原始决策及原稿/检查点/存档的设计思路；不保留第二套可启动产品链路。

本次不删除服务器聊天 JSONL、审计记录、照片、用户数据、凭据、固件与设备备份，也不批量导入当前模型记忆。旧数据保留原权限和来源说明；停掉读取 CLI 不等于授权删除数据。

## 4. 执行前置条件

- [ ] 设备控制开发完成交接，当前新增工具、Bridge 和能力提示的提交/部署状态已记录。
- [ ] 执行者确认工作区没有其他进行中的改动；如仍有改动，仅继续独立只读核对，不执行删除或重排提交。
- [ ] 重新读取最新 `DEVELOPMENT_HANDOFF.md`，记录实际 HEAD、远端 main 和云端 Go build，而不是沿用本文日期的版本。
- [ ] 刷新逐文件哈希与消费者证据，确认清理不覆盖后来新增的实现或测试。
- [ ] 在云端取得可运行基线；原有失败、跳过及环境问题单独记录，不能归到删除后的回归结果中。
- [ ] 旧服务的实际 FragmentPath、DropInPaths、进程与连接已核对；本轮方案没有远程确认其当前运行状态，不能直接照抄 10月25日的 active 清单。

清理直接在 main 分小提交执行；先消费者、后提供者。某个文件发现现用依赖时只延后该删除批次，不删除调用方的测试来制造通过。

## 5. 任务一：冻结 Python 对照结果

**涉及文件：**

- `internal/realtimestate/differential_test.go`、`testdata/oracle.py`。
- `internal/realtimeaudio/codec_test.go`、`resample_test.go`、`testdata/reference.py`。
- 新增状态 `testdata/legacy-state-v1.json`、音频 `testdata/legacy-audio-v1.jsonl` 及各自 `legacy-golden-manifest.json`。

### 5.1 基线与样本来源

- [ ] 云端独立 scratch 使用交接后的完整已提交源码；不在 Mac 启动服务，也不占用正式端口。
- [ ] 先用现有 Python oracle 跑通 Go 两包测试，记录 Go/Python/libopus 版本与构建环境。
- [ ] 记录原 Python 冻结提交、实际读取文件的 SHA256、产品身份文件哈希、导出器来源和完整输入/输出哈希。如果使用外部基线 checkout，必须记录其真实提交，不能把它标成当前 HEAD。
- [ ] 状态样本保留现有全部 **494 组**：number 19、equivalent 9、context 183、tracker 180、instructions 13、receipt 90。执行时若数量已变，逐项解释变更并使用交接后的真实数量，不裁回旧数字。
- [ ] 音频冻结当前 **30 组请求**：两种 Opus 帧配置、10 个尾部边界、6 类输入×3 类重采样分块。全部为代码生成的数值、静音、脉冲和正弦数据，不含用户声音。

### 5.2 一次性导出方式

只在云端 scratch 给现有测试增加导出分支，导出期仍继续执行原断言。以下片段放在状态测试解析 Python 输出后、给 instructions 添加 Go 专属规则之前：

```go
if out := os.Getenv("LEGACY_STATE_EXPORT"); out != "" {
    payload, err := json.MarshalIndent(map[string]any{
        "schema_version": 1,
        "cases": json.RawMessage(raw),
        "expected_legacy": expected,
    }, "", "  ")
    if err != nil { t.Fatal(err) }
    if err := os.WriteFile(out, append(payload, '\n'), 0600); err != nil {
        t.Fatal(err)
    }
}
```

音频在现有 `pythonReference` 得到并解析 Python 返回值后增加以下一次性分支；`request`、`result` 均为现有类型：

```go
if out := os.Getenv("LEGACY_AUDIO_EXPORT"); out != "" {
    payload, err := json.Marshal(map[string]any{
        "schema_version": 1, "request": request, "expected": result,
    })
    if err != nil { t.Fatal(err) }
    f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
    if err != nil { t.Fatal(err) }
    _, writeErr := f.Write(append(payload, '\n'))
    closeErr := f.Close()
    if writeErr != nil { t.Fatal(writeErr) }
    if closeErr != nil { t.Fatal(closeErr) }
}
```

- [ ] 导出路径使用 scratch 内新建空文件；禁止向旧导出追加第二次运行。两包顺序执行，音频导出不启用 parallel 子测试。
- [ ] 导出两次到不同新目录，比较样本内容与 SHA256；输出应完全相同。输入与期望来自旧 Python，不用正在测试的 Go 结果生成期望。
- [ ] 保留布尔、null/空数组、数字词法类型、Unicode 与控制字符；尤其保留 `100.0`、`500.5`、实际 U+2028 与字面量转义字符串的区别。
- [ ] manifest 记录样本数、来源与数据哈希；Opus 字节样本记录实际库版本。库版本变化要单独复核，不跳过比较或自动重写样本。
- [ ] 正式入库仅有合成 JSON/JSONL 与 manifest；临时导出分支、绝对机器路径和运行配置不进入产品代码。

## 6. 任务二：用冻结样本替换在线 oracle

### 6.1 状态回归

- [ ] 保留 `baselineTurn`、`evaluateBaselineCase` 及 Go 语义断言。将 `TestPythonDifferential` 改成 `TestLegacyStateGolden`，读取冻结输入和期望，不执行 Python。
- [ ] corpus 用 `[]json.RawMessage` 保存 cases/expected_legacy；每个输入经 `Decoder.UseNumber()` 解码，期望按原测试的普通 JSON 数值方式解码，继续与 Go 规范化输出做 DeepEqual。
- [ ] 保留固定时钟 `2026-10-25T05:14:15Z`、完整提示词比较及已有 `StorytellingGuidance` 的明确差异。先冻结 Python 原始期望，再在测试中应用已经声明的 Go 差异，不把当前 Go 输出反写成历史正确答案。
- [ ] 样本缺失、schema 错误、数量/哈希不匹配必须失败；不增加 `t.Skip`，不因 Python 不存在跳过状态回归。
- [ ] 通过后删除 `testdata/oracle.py`、Python 启动代码与 `REALTIME_PYTHON_BASELINE` 依赖。

### 6.2 音频回归

- [ ] 将 `pythonReference` 改为 `legacyReference`：读取受 8MiB 上限约束的 JSONL，逐条匹配现有 `referenceRequest`，返回对应 `referenceResult`。用 JSON Decoder 读取多条记录，避免 Scanner 默认 64KiB 行上限截断长样本。
- [ ] request 的 operation/rate/frame/chunks 均参与匹配；找不到、重复或 schema 错误均失败，不能临时使用 Go 结果补样本。
- [ ] 现有 codec/resample 测试只替换期望来源和名称；继续验证每包字节、解码 PCM、Opus lookahead、312 样本尾部公式、负数取整、状态持久性及逐块/整段相等。
- [ ] 保留非法长度、坏包、构造/关闭、并发关闭、零值重采样与错误后相位不变测试。
- [ ] 通过后删除 `testdata/reference.py` 与 `REALTIME_AUDIO_PY`，从 `go-tests.command` 移除两个基线环境变量导出；保留其云端执行约定和原 Go 工具链选择。

云端验证命令，逐条记录退出码：

```sh
go test -count=1 -race ./internal/realtimestate ./internal/realtimeaudio
go test -count=1 -race ./...
go vet ./...
go build ./...
```

使用交接后已验证的 Go 二进制执行，不因本次清理升级工具链。再将一个只输出固定错误并退出非零的 `python3` 拒绝脚本置于该次 Go 检查的 PATH 最前面，重复运行两包回归；预期不调用它，所有冻结测试照常执行。不要卸载服务器 Python 来证明这一点。

这一批独立提交：测试依赖替换先进入 main；旧运行源码此时尚未删除。

## 7. 任务三：整理现用客户端与共用测试

**局部修改：** `tools/realtime-output-live/open.command`、`main.swift`；**新增：** `tools/native-voice/test-common.command`。

- [ ] 从启动器健康识别中移除 18032/Python 特例，Go 标识仍以 `health.backend` 为依据，不能仅凭端口号强标为 Go。
- [ ] 删除 Swift 的 Python 标签、橙色、Python 参数分支及对应匿名身份特例；保留 Go/unknown 的既有识别、device profile、默认 18040、注册/票据、停止与清理逻辑。
- [ ] 保留本地/远端端口参数能力，不能把不默认使用的端口参数一并删掉。
- [ ] 不改 LiveClient 音频、Opus、播放账本、插话排空、麦克风和 AEC。现用 build/build-soak/test-device-identity 继续使用现有共用文件。
- [ ] 新增纯客户端共用逻辑测试入口，替代旧 NativeVoice App builder 附带执行这些测试的职责：

```bash
#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
common="$repo_root/tools/native-voice"
out="$repo_root/.superpowers/native-common-tests"
mkdir -p "$out"
xcrun swiftc "$common/Sources/PlaybackLedger.swift" \
  "$common/Sources/NativeEndpoint.swift" "$common/Tests/LedgerSpec.swift" \
  -o "$out/ledger-spec"
"$out/ledger-spec"
xcrun swiftc -framework AVFoundation "$common/Sources/PlaybackLedger.swift" \
  "$common/Sources/MicrophonePCM.swift" "$common/Tests/MicrophoneSpec.swift" \
  -o "$out/microphone-spec"
"$out/microphone-spec"
```

Mac 执行脚本语法、现用客户端构建和该纯逻辑测试，均不启动本地语音服务；实际麦克风/播音复验按用户当时可参与的条件安排，不能在静默限制下自动触发。

```sh
bash -n tools/realtime-output-live/open.command
bash -n tools/realtime-output-live/build.command
bash -n tools/realtime-output-live/build-soak.command
bash tools/native-voice/test-common.command
bash tools/realtime-output-live/test-device-identity.command
bash tools/realtime-output-live/build.command
```

预期全部退出 0。共用测试仍覆盖旧代次、采样边界与收音数据逻辑，不能只证明 Swift 能编译。这批也独立提交。

## 8. 任务四：云端精确退役旧服务

### 8.1 先识别，后停用

| 服务/入口 | 本轮决策 |
| --- | --- |
| `family-robot-livekit-device.service` / 18020 | 退役，先于被依赖的 18010 |
| `family-robot-livekit-dev.service` / 18010 | 退役 |
| `family-robot-realtime-output-live.service` / 18032 | 核对动态/静态实际单元后退役 |
| 历史 18031 探针 | 如仍有进程，仅在核对其精确 PID/所有者/用途后停止该实例 |
| `family-robot-realtime-output-go.service` / 18040、18041 | 保留，不为停旧服务重启 |
| `family-robot.service` / 8000 | 保留，仍承担 OTA/激活 |
| `family-robot-realtime-test.service` / 18030 | 本轮不处理 |

- [ ] 用 systemctl 的白名单属性、依赖关系和 ss/lsof 核对实际服务、连接与消费者；属性限 Id/FragmentPath/DropInPaths/ActiveState/SubState/MainPID/NRestarts/ExecMainStartTimestamp，不把环境值或凭据打印到聊天。
- [ ] 客户端旧会话自然结束后再停旧服务；不能把“无 TCP 连接的一时快照”当作所有消费者永久消失。
- [ ] 备份旧 unit、drop-in、源码提交、必要二进制和环境引用到云端 0700 私有回退目录。涉及秘密的原件只在云端保存，输出摘要而非文件正文。
- [ ] 为旧 Python 依赖准备可恢复的固定源码包；可从 Git archive 导出到独立私有恢复目录，不创建 worktree，不覆盖当前 Go 工作目录。venv 在退役验收前保留，不先卸依赖。
- [ ] 结束仅属于旧客户端的连接/本地转发；不杀共享 SSH 主连接，不触碰 Go 客户端现有隧道。

### 8.2 停用顺序

核对实际 unit 与无旧活动会话后，先 18020，再 18010，最后独立的 18032。下列是未来执行命令，本轮不运行：

```sh
sudo systemctl stop family-robot-livekit-device.service
sudo systemctl stop family-robot-livekit-dev.service
sudo systemctl stop family-robot-realtime-output-live.service
```

持久 unit 清除 enabled 状态；若 Python 是 transient unit，记录它的实际创建入口，确认已停止且没有定时/启动脚本重建，不直接假定 `disable` 对 transient 有效。核实文件来源后，才移除本次三个旧 unit 的安装副本/对应专用 drop-in 并 daemon-reload；不删除其他 unit。

禁止按 `python`、`livekit` 关键字批量 pkill，禁止删除父目录或整个运行环境。

- [ ] 复核目标端口不再监听、旧服务不再自动启动；目标不存在时如实记录 absent，不把不存在的服务停用记为成功执行。
- [ ] 核对 Go build、PID/重启次数、健康与当前连接状态未被本批影响；8000 激活/OTA 和两台准入配置保持。
- [ ] 旧文字/审计文件权限、数量/完整性与退役前一致，不向新模型自动注入旧数据。

仅停旧服务不要求重启 Go。若发现仍有真实旧消费者，先把该消费者迁离/关闭，再继续受影响的退役，不强行停用户正在使用的会话。

## 9. 任务五：删除专用代码

- [ ] 重新导出执行时清单：逐文件确认无保留消费者、与当批审查内容一致；本方案附带的 `planning_only` JSON 不可直接当删除授权脚本输入。
- [ ] 根据实际已通过的门槛生成精确 NUL 分隔文件列表 `.superpowers/legacy-retirement/remove-paths.nul`，先用 `git rm -n --pathspec-from-file=.superpowers/legacy-retirement/remove-paths.nul --pathspec-file-nul` 检查，只包含已审核的旧文件。
- [ ] 删除消费者：旧客户端启动/App 专用实现、三段式与探针入口、旧 bridge 命令和部署模板。
- [ ] 删除提供者：`family_voice`、旧网页、旧 Python Realtime、历史 probe/audit 实现、`internal/devicebridge` 及仅测试这些旧实现的测试。
- [ ] 删除依赖旧 Python 的 `replay_history.py`、`probe_grounding.py`，不留下 import 已删 server/state 的伪可用工具。必要分析能力以后用当前 Go 数据接口单独实现，本轮不新建付费模型探针。
- [ ] 保留现用组件、独立 golden 回归、固件/诊断工具、旧 Go 业务模块及历史资料。
- [ ] 每批执行 `git diff --check`、受影响构建/测试并独立提交，失败先处理当前批，不把多批删除混成难以回退的大提交。

允许删除针对已经退役协议/产品的专用测试，但必须在验证记录中列清消失的测试和理由。不能删除仍保护现用组件的测试；测试总数降低也不能只用“剩余全部通过”作解释。

## 10. 任务六：文档、环境与收尾

- [ ] 更新 README、CODE_LIFECYCLE、当前客户端 README 和交接：分别写清代码删除、服务停止/禁用、依赖清理、数据保留的真实状态。
- [ ] 旧 README 留短归档说明和稳定 Git 提交链接；`services/livekit-agent/README.md` 保留 `语音文字存档` 等历史报告仍引用的必要锚点，说明数据未删除，旧命令已退役。
- [ ] 历史设计/验证报告不批量改写其当时事实。旧源码链接可以转为固定提交链接，不能让读者误认为是当前启动指令。
- [ ] 移除当前操作文档和默认入口中的 Python 回退、旧服务启动及两后端对比说明；历史对照结果继续可读。
- [ ] 旧专用 venv/缓存/二进制的物理空间回收另批执行，前提是查清真实文件路径、当前 Go/固件/测试无引用且回退观察完成；不以本次方案授权删除整套云端父目录或系统包。
- [ ] 记录每批 commit、实际退役 unit/端口、保留数据、测试变化、失败/跳过与回退方法。提交并推送 main，核对远端 SHA，不只报告本地提交成功。

建议最终验证报告路径：`docs/verification/2026-10-27-legacy-voice-retirement.md`；执行日期变化时使用实际日期，不提前创建“已通过”报告。

## 11. 验收条件

| 维度 | 完成条件 |
| --- | --- |
| 源码 | 清单内通过审查的旧运行实现、入口和模板已删除；每个延期项有具体依赖理由 |
| 测试 | 冻结状态/音频回归独立运行，不需要旧 Python 源码或模型 Key；保留现有边界和失败断言 |
| Go | 云端全仓 race/vet/build 通过，现用会话、记录、设备控制测试无退化；既有跳过单列 |
| Mac | 当前 Go App 可构建，共用测试/身份测试通过，默认端口及健康识别正确；客户端音频行为不被本轮更改 |
| 云端 | 目标旧服务停止且没有重建入口；18040/18041、8000 和本轮保护的 18030 不受影响 |
| 设备 | 后续必要短复验涵盖唤醒、问答、插话及设备控制，并分别记录软件与实机证据；无固件刷写 |
| 数据 | 原有聊天/审计、身份、凭据和设备备份未被删除、改写或公开 |
| 文档 | 当前启动说明唯一且可用；历史设计、试验和源码仍可定位 |

新增 golden 若因库版本或输入生成发生差异，应调查并独立评审，不能自动重新录制。清理不改变语音流式、声音、提示、内置搜索、播放封口、已播上下文或记录归属。

## 12. 回退与停止条件

出现共用文件误判、现用构建/测试失败、当前服务变化或身份/记录异常时，停止对应清理批次，保留现场并恢复该批删除；不继续扩大清理。

代码使用清理专属 commit 逐项 revert，不 reset 整个 main，不覆盖设备控制或后来提交。运行回退只恢复旧服务的固定源码与 unit 到私有恢复位置；按备份的实际 ExecStart/WorkingDirectory 设置，仅启动需要恢复的旧 unit，不回退当前 Go 工作目录、二进制或设备固件。

不得以恢复旧服务为由放宽鉴权、复制 Key 到 Mac、重写设备身份或回放历史用户录音。旧记录不在本次删除范围，因此不应需要数据回滚。

## 13. 本轮完成边界

本轮仅完成：源码/消费者静态核对、128 文件清单和本方案。没有运行模型、后端测试、服务停用、源码删除、依赖卸载或设备操作。

执行前仍需设备控制交接、刷新实际基线和云端服务核对。当前文档不是这些前置检查已经通过的证明，也不自动启动执行。
