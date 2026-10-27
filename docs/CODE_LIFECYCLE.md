# 代码使用状态与旧语音链路退役标记

**2026-10-28模块复用更新：`internal/devicetools`已由当前`internal/realtimelive`直接复用，提供18040/18041的当前设备状态/音量/亮度控制，不属于旧链路可直接删除的模块。普通语音保留内置搜索，工具由独立无声原生会话处理；本次未停旧服务。具体代码、发布和实体边界见[设备控制验证](verification/2026-10-27-realtime-device-controls.md)。旧退役清单执行前须重新核对新增消费者。**

2026-10-27补充：用户已认可清理旧 Python 语音实现与完整三段式运行链路的方向，已整理[退役清理方案](superpowers/plans/2026-10-27-legacy-voice-retirement.md)及逐文件清单。本次仅写方案，尚未删除代码、停服或卸载依赖；实际执行须等设备控制开发交接，刷新基线并完成测试依赖拆分。下文保留此前标记与运行核对的日期边界。

更新：2026-10-25。用户已确定以当前Go Realtime为基础，逐项接入旧Go业务功能；先将被替代的Python运行实现和完整三段式链路标记弃用。本页只改变代码/文档状态，不删除代码、停服务或迁移数据。

## 当前唯一默认语音核心

- `cmd/realtime-output-live/`、`internal/realtimelive/`：Go原生Realtime会话与服务，云端回环18040。
- `internal/realtimeaudio/`、`internal/realtimestate/`：当前编解码/重采样、播放状态、已播上下文与小满身份。
- `tools/realtime-output-live/open.command`、`main.swift`、`LiveClient.swift`及构建入口：当前Mac客户端，默认18040。
- `deploy/family-robot-realtime-output-go.service`：当前Go部署模板。
- `config/product-identity.json`：共用身份事实配置，不属于弃用代码。

当前Go运行时不调用Python语音服务。后续业务模块通过明确接口接入这一语音核心，保留已验证的流式、插话排空、身份、搜索与已播记录行为。内置搜索与tools互斥等限制仍需在具体合入项中设计，标记弃用不等于已合入任何新功能。

## 已弃用为产品路径，保留源码

以下源码使用`DEPRECATED_PRODUCT_PATH`标记。含义是“不再作为默认入口、后续产品语音方案或新功能开发基线”，不是文件失效或云端进程已停止。

| 代码/入口 | 状态与保留用途 | 当前替代/后续处理 |
|---|---|---|
| `tools/realtime-output-live/{server,upstream,audio,state,history,identity}.py` | 旧Python Realtime实现；仅临时回退、对照与差异测试参考 | 主运行由对应Go模块承担；新功能只进Go |
| `services/livekit-agent/family_voice/` | 完整LiveKit ASR→LLM→TTS三段式实现，退出后续产品主路径 | 保留历史和业务设计参考，不重新接回为常设第二语音后端 |
| `deploy/family-robot-livekit-dev.service` | 旧三段式18010模板 | 保留到独立停服清理；本次未重新安装模板 |
| `cmd/livekit-device-gateway/`、`deploy/family-robot-livekit-device.service` | 旧18020→18010桥接入口 | 后续设备适配接当前Go核心，不继续依赖旧三段式 |
| `tools/native-voice/open.command` | 旧三段式原生客户端入口 | 当前入口为`tools/realtime-output-live/open.command` |
| `tools/mac-voice/open.command` | 旧三段式浏览器入口 | 不再作为产品/主测入口 |

三段式旧模块中的原稿、检查点、档案等思路可以评估后逐项转成Go业务能力；它们没有因为本次标记自动接入Go。将来如有固定稿或精确播控需求，应单独设计该能力，不据此恢复整条旧ASR→LLM→TTS产品链路。

## 仍在使用的验证参考与共用组件

以下内容不能随旧后端一并删除，也不能标成“完全没有使用”。

| 内容 | 实际用途 |
|---|---|
| 旧Realtime Python的state/history/identity/server/audio，以及`tools/realtime-output-probe/alignment.py`等 | Go测试的Python参考：`internal/realtimestate/testdata/oracle.py`读取/解析旧源码，`internal/realtimeaudio/testdata/reference.py`读取旧编解码实现 |
| `tools/realtime-output-live/test_*.py`、`tools/realtime-output-probe/test_alignment.py` | 仍可运行的冻结基线回归；不是产品语音后端 |
| `tools/realtime-output-probe/` | `LEGACY_VALIDATION_REFERENCE`：历史探针及参考帮助模块；不作为新产品运行入口 |
| `tools/livekit-audit/` | 历史SDK契约验证，按需保留，不属于当前Go运行依赖 |
| `tools/native-voice/Sources/`、`OpusBridge.c`、`OpusBridge.h` | 当前Go Mac客户端仍编译/使用的共用音频、AEC、Opus与播放回报实现，保持有效 |
| `tools/realtime-voice/Sources/Fixture.swift` | 当前Go客户端仍使用的合成输入调度工具，保持有效 |
| `tools/realtime-output-live/replay_history.py`、`probe_grounding.py` | 手动日志分析/研究工具，按使用场景保留；不在Go默认请求路径执行 |

`LEGACY_VALIDATION_REFERENCE`只限制产品运行用途，不禁止差异测试使用Python。新Go服务不因此产生Python子进程依赖。保留测试参考的行为基线，不借标记改提示词、音频处理或历史规则。

## 旧Go功能待逐项复用

`internal/gateway`、`internal/qwen`、`internal/audio`、`internal/protocol`、`internal/devicetools`、`internal/recordstore`等旧Go代码不整包标弃用：旧8000/18030入口仍运行，且其中包含待复用的设备协议、鉴权、工具、存储和身份隔离能力。先盘点依赖，再单模块适配；不整包覆盖当前Go主会话。`internal/devicebridge`目前属于旧三段式适配，可取低层经验，不能把它当成新Realtime已完成的设备适配。

## 本次核对的运行状态

2026-10-25检查时：

| 服务 | 端口 | 本次状态/定位 |
|---|---:|---|
| `family-robot-realtime-output-go` | 18040 | active，当前唯一默认入口 |
| `family-robot-realtime-output-live` | 18032 | active，已弃用的Python实现，临时回退/对照 |
| `family-robot-livekit-dev` | 18010 | active，三段式历史服务，待另行退役 |
| `family-robot-livekit-device` | 18020 | active，旧三段式桥；Requires上面的18010服务 |
| `family-robot` | 8000 | active，原Go设备入口，暂保持兼容 |
| `family-robot-realtime-test` | 18030 | active，旧Go Realtime对照入口，暂保持 |

检查时18010/18020/18032未发现已建立TCP连接，但这只是一时快照，不等同已确认无消费者。服务依赖显示18020仍Requires18010。本次不执行stop/disable、daemon-reload、删除源码、卸载依赖或删除记录；现有窗口由用户自行关闭。

后续停旧服务必须作为独立动作：核对消费者和保留回退方式；若停三段式，先处理18020桥的依赖再处理18010。停进程、清理源码、清理历史数据分别处理，不把本页的代码弃用标记当作已经完成这些动作。

## 当前开发约定

新语音能力只进入当前Go核心。旧功能复用先列功能与依赖台账，接入一项就验证该项和现有语音体验；判题、提醒、长期记忆等未实现功能仍单独排期。以最新开发交接和本页为当前状态，旧方案/报告保留其当时证据，不作为新默认入口指令。

## 本次标记验证

云端独立验证目录：`/opt/family-robot-livekit-validation/legacy-marking-20261025`，基线为4240bd2。32个Python文件的AST与基线完全一致；另5个Go入口/部署模板/启动脚本除新增注释外正文一致。共37个实现、入口或模板文件获得标记，没有运行逻辑变更。

Python原Realtime49项＋probe8项回归通过；Go realtimestate/realtimeaudio两包race测试通过（使用本次带标记的Python文件作参考），旧livekit-device-gateway构建通过；两份旧启动脚本语法检查通过。更新文档的131个本地链接已核对存在。本次没有重新安装运行依赖、调用真实模型或重启服务。
