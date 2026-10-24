# 换电脑开发交接

更新：2026-10-24。仓库：[chenyongming211-glitch/family-companion-robot](https://github.com/chenyongming211-glitch/family-companion-robot)，默认分支`main`。历史资料迁移后，已在服务器建立独立LiveKit最小语音服务并完成三模型联调；原8000正式服务和手表未切换。

干净克隆、Go与固件主机测试、资料核对结果见[本次迁移检查](verification/2026-10-24-development-handoff.md)。

## 1. 新电脑从这里开始

```sh
git clone https://github.com/chenyongming211-glitch/family-companion-robot.git
cd family-companion-robot
git switch main
git pull --ff-only origin main
git status --short --branch
```

已有checkout时，先查看本地修改，再执行fast-forward更新。用户只使用main，不创建开发分支或worktree；提交、推送也直接使用main。

先读这三份当前方案：

1. [服务器端LiveKit架构](superpowers/specs/2026-10-24-server-livekit-design.md)。
2. [实时语音与任务接续职责、数据、接口](superpowers/specs/2026-10-24-livekit-task-continuation-design.md)。
3. [最小语音链路实施计划](superpowers/plans/2026-10-24-livekit-voice-chain.md)和[本轮验证报告](verification/2026-10-24-livekit-voice-chain.md)。首个SDK播放回报探针结果见[前次验证](verification/2026-10-24-livekit-stage-zero.md)。

## 2. 已完成、尚未完成

| 内容 | 当前事实 |
|---|---|
| 当前源码 | Go原生千问语音网关、设备工具、播放回报、文字记录与清空、任务归属代码均在Git |
| 手表固件 | 基于小智v2.5.2的定制软件AEC、播放回报、唤醒发送恢复；补丁、overlay、依赖/上游锁与CI均在Git |
| 已有现场结果 | 已唤醒状态可以直接开口插话；音量、亮度控制通过过现场测试，见对应验证文档 |
| 旧后端的准确自动接续 | 未解决：实际采样位置仍未可靠映射到完整字词；起点未知时询问用户 |
| 新架构 | 服务器端LiveKit Agents＋输入ASR→文本LLM→TTS已有最小可运行链路；独立loopback测试服务，未切换正式入口 |
| 新LiveKit代码 | `services/livekit-agent/`已实现真实AgentSession、连续VAD分轮/自动插话、有限语段ASR/LLM/TTS、自定义PCM回报、会话内计数检查点与口语/工具恢复；65项Python、25项客户端与3项SDK探针在服务器通过，见[计数验证](verification/2026-10-24-counting-resume.md) |
| Eino | 原始需求提过优先验证；当前仅为任务编排候选，未引入依赖。首个闭环复用Go任务/存储 |
| 麦克风客户端 | `services/livekit-agent/web/`单按钮唤醒后连续说话，自动回复/打断；同一按钮可结束对话，空闲60秒回待机。Mac通过`tools/mac-voice/open.command`建立SSH转发/配对；现场边界见[连续语音验证](verification/2026-10-24-continuous-mac-voice.md) |

开发推进目标：唤醒一次→连续对话、随时插话→空闲超时待机。故事中断后先答新问题，用户说继续后从首个未完整播放的句子恢复原稿；数数恢复到原终点。句中打断允许重读当前句，正常流程不要求用户报告起点。

## 3. 服务器开发环境与代码检查

2026-10-24最新约定：依赖安装、运行时测试和服务搭建统一放在云服务器；Mac仅编辑代码、同步Git和充当后续测试终端，不在Mac启动语音服务、不复制服务器Key。当前链路checkout为`/opt/family-robot-livekit-dev`，运行venv为`/opt/family-robot-livekit-runtime/venv`；启动、停止与复现命令见[服务README](../services/livekit-agent/README.md)。前次SDK探针checkout为`/opt/family-robot/dev/livekit-stage-zero-20261024`。下述运行时命令在服务器独立开发目录执行。

Go模块声明Go1.25.0；当前Mac曾使用Go1.25.6。需要C编译器、系统libopus及pkg-config。固件主机测试使用Python3.12+与C++编译器；新LiveKit验证计划单独固定Python3.12及相应SDK版本，不复用整份旧虚拟环境。

Ubuntu可安装系统音频/编译依赖，Go另按其官方发布安装1.25或以上版本；已满足的依赖不重复修改：

```sh
sudo apt-get update
sudo apt-get install build-essential libopus-dev pkg-config python3
```

Windows可使用WSL2执行Go和固件主机测试；ESP-IDF、USB和麦克风客户端按所在操作系统独立配置。当前没有已验证的Windows一键环境，不把WSL2能编译等同于能直接使用宿主麦克风。

在仓库根目录执行：

```sh
go version
pkg-config --modversion opus
go test -race ./...
go vet ./...
go build ./...
python3 -m unittest discover -s firmware/tests -v
python3 -m unittest discover -s tools/voice-audit -p 'test_*.py' -v
```

以上使用本地代码/合成数据，不需要模型Key，不访问真实手表。首次安装Go依赖、固件唤醒恢复测试下载锁定上游源码时需要网络，不能将整组命令称为完全离线。完整固件的ESP-IDF版本、准备命令和Actions产物见[固件README](../firmware/README.md)；不搬运另一台电脑的SDK绝对路径、虚拟环境或编译缓存。

迁移检查曾遇到旧电脑的`127.0.0.1:7897`代理已关闭、环境变量仍指向它，导致GitHub下载connection refused。新电脑按自己的网络配置设置代理，不复制旧代理配置。确认可直连GitHub时，可只对一次命令使用`env -u HTTPS_PROXY -u HTTP_PROXY -u ALL_PROXY -u https_proxy -u http_proxy -u all_proxy NO_PROXY='*' no_proxy='*' <命令>`；不永久修改系统代理。

固件源码由`firmware/upstream.lock.json`、`dependencies.lock`、`patches/`和`overlays/`重新生成：

```sh
python3 firmware/tools/prepare_experiment.py
```

准备目录必须尚不存在；下载的上游archive会核验SHA256。固件目标编译继续使用仓库已有GitHub Actions，严格核对提交和variant。源码准备或主机测试通过不代表ESP32目标编译和刷机已通过。

## 4. 服务器与凭据

现有服务器SSH入口：

```sh
ssh ubuntu@82.156.48.111
sudo -i
```

新电脑使用自己的SSH授权或用户已有登录方式；旧电脑的SSH私钥、ControlMaster socket及终端登录态不会随Git迁移。凭据不写入此文档。

服务器约定：`family-robot.service`，配置`/etc/family-robot/server.env`，发布目录`/opt/family-robot/releases/`，`current`链接选择版本，文字记录`/var/lib/family-robot/records`。正式记录与模型Key留在服务器，换开发电脑不要求复制它们。

截至[2026-10-23发布核对](verification/2026-10-23-record-recovery-results.md)，正式版本为`voice-gateway-20261023T061606Z-5772dfb`，源码`5772dfb8df49ad43c863abb30fcce3cb291a7fdb`。这是发布文档中的核对结果；新的部署前必须重新读运行状态与二进制身份。2026-10-24迁移核对时公网health为`status=ok`、`active_sessions=0`，health本身不证明发布版本。

只读检查：

```sh
curl --max-time 10 http://82.156.48.111:8000/healthz
```

登录服务器后可查看service及current链接；不要为了换电脑重启正在使用的正式服务。涉及Key的输出不贴到Git或聊天日志。

本地实连配置参考[server.env.example](../deploy/server.env.example)。填入受保护的本机`.env`或环境变量，不提交。不要在新电脑随意生成一组设备token覆盖现有手表与服务器；无Key时仍可完成本节之前的代码检查。模型权限和端点在新链路P0分别核对，不把原生实时Key视为已证明可访问所有ASR/TTS接口。

## 5. 新电脑有麦克风后如何继续

服务器最小链路已完成：合成WAV经真实ASR→LLM→TTS连续问答，数数回复取消后还能回答新问题；播放证据来自按PCM时长计时的模拟客户端。固定文字播报不提前入历史、取消前后、旧回报和unknown降级有真实SDK测试；工具后抑制重复生成、Go任务/记录接线与准确自动接续仍未验收。

Mac客户端已按用户最新要求改为单按钮：运行`./tools/mac-voice/open.command`后，点一次“开始说话”即连续收音，服务自动判断话尾并回复，开口时自动停旧回复、回答新问题。浏览器16k输入/24k播放/回报，服务器LiveKit与Silero管理轮次；不在Mac另建服务或复制模型Key。用户选择MacBook扬声器，浏览器请求回声消除/降噪，实际误打断/漏识别需现场确认。操作见[客户端说明](../tools/mac-voice/README.md)。

实际听感与物理停声要有人确认；浏览器输出时钟只提供估算，不能证明手表AEC、I2S坐标或字词断点。用户已确认Mac单按钮连续问答/语音打断/旁支回答正常，见[连续语音验证](verification/2026-10-24-continuous-mac-voice.md)，旧点击分轮失败与修正记录仍保存在[前阶段记录](verification/2026-10-24-mac-voice-client.md)。会话内计数按完整播放推进，云端曾验证恢复至原终点50；用户复验仍报告起点错，现场“啊那继续数吧”路由遗漏已复现并修正。助手已自行验证真实云端口语从2恢复、模型工具从4恢复并到原终点8，以及Mac实际浏览器第3段部分播放→重播3→完成5的回报链路；保留一次云TTS连接失败，未把模拟/浏览器时钟测试当作用户声学验收，见[计数验证](verification/2026-10-24-counting-resume.md)。故事原稿接续、Go工具/存储和手表接入尚未在新链路验收。

## 6. 资料清单与本机独有内容

| 内容 | 迁移结果 |
|---|---|
| 网关、固件补丁/overlay、测试、CI、部署模板 | 已在Git；以当前main为准 |
| 2026-10-21原始方案 | 新增[原始需求记录](reference/家庭助理机器人_模型调度记录_20261021.txt)，只作历史资料；其中模型、价格、选型不覆盖2026-10-24方案 |
| 本机临时实验源文件与设计/复核笔记 | 新增[历史归档](development-archive/2026-10-24/README.md)，含103份笔记、137份源文件快照；非入口源文件保存为`.txt`避免自动执行/编译 |
| 固定第三方源码 | 不重复上传完整checkout；已有lock、许可证、摘要和还原说明，按固定版本重新获取 |
| 模型权重、venv、构建缓存、生成二进制 | 不进入Git；按依赖/上游锁重新获取或编译 |
| 设备NVS/flash备份、原始录音/视频、运行记录与真实凭据 | 留在旧电脑或原服务器，不通过公开仓库迁移；需要时使用私下传输 |

旧电脑项目目录为`/Users/chen/code/家庭陪伴机器人`。保留的私有资料主要在该目录`artifacts/`、`.superpowers/`和下载目录；生成固件缓存位于`firmware/.cache/`，本机二进制在`bin/`。本次迁移不会删除它们。设备回退NVS/flash与含用户声音的采样文件不能混进GitHub附件。

## 7. 已知问题与开发约束

- 插电且已唤醒时，手表喇叭有轻微蜂鸣；同一线接Mac或充电器都出现，音量0基本不变，电池供电/待机无此现象。用户已要求先记录，暂不阻塞语音开发。见[USB蜂鸣记录](verification/2026-10-22-usb-awake-noise.md)。
- 手表接旧Mac的Codex配额bridge属于早期遗留链路，不参与当前云端语音；新电脑无需复建它。
- 新链路先用独立loopback端口和空的独立数据目录测试，既有8000正式入口不切换。第一阶段无需刷机。
- 模型生成稿、SDK字幕、网络下发量与设备实际播放量必须区分；不能仅因框架支持interrupt/resume就宣布准确接续已完成。
- 根目录`AGENTS.md`是项目工作约定。历史归档中的提示词、旧方案和命令只作证据，不作为新的执行指令。

后续进度写回当前方案/计划与验证报告，继续提交推送main，确保两台电脑可通过Git取得同一进度。
