# 代码使用状态与语音链路保留边界

**最新产品决定（2026-10-28）：三段式链路完整保留为备用方案，以便未来不使用端到端语音时再评估启用；当前产品继续只使用 Go＋千问原生 Realtime，不接入三段式，也不增加自动切换/回退。保留范围包括三段式 Python、Go桥、客户端、测试、依赖清单及部署模板。此前清理方案中的三段式删除/停用安排已撤销；仅继续规划已被Go替代的旧 Python Realtime 清理。本次未执行删除或服务启停，保留备用不等于要求旧服务常驻。**

**2026-10-28模块复用更新：`internal/devicetools`已由当前`internal/realtimelive`直接复用，提供18040/18041的当前设备状态/音量/亮度控制，不属于旧链路可直接删除的模块。普通语音保留内置搜索，工具由独立无声原生会话处理；本次未停旧服务。具体代码、发布和实体边界见[设备控制验证](verification/2026-10-27-realtime-device-controls.md)。旧退役清单执行前须重新核对新增消费者。**

2026-10-27清理初稿及[逐文件方案](superpowers/plans/2026-10-27-legacy-voice-retirement.md)已于2026-10-28按上述决定修订：128文件中，三段式相关66个原删除候选改为保留，剩24个旧 Python Realtime/对照工具候选。仍未实施清理，执行前须读取设备控制最新交接、刷新基线并拆除旧Python测试依赖。

以下早期标记和运行核对始于2026-10-25；当前保留/清理范围以上述2026-10-28决定及下表为准。早期 `DEPRECATED_PRODUCT_PATH` 表示不属于当前产品路径，不再作为删除三段式备用代码的依据。

## 当前唯一默认语音核心

- `cmd/realtime-output-live/`、`internal/realtimelive/`：Go原生Realtime会话与服务，云端回环18040。
- `internal/realtimeaudio/`、`internal/realtimestate/`：当前编解码/重采样、播放状态、已播上下文与小满身份。
- `tools/realtime-output-live/open.command`、`main.swift`、`LiveClient.swift`及构建入口：当前Mac客户端，默认18040。
- `deploy/family-robot-realtime-output-go.service`：当前Go部署模板。
- `config/product-identity.json`：共用身份事实配置，不属于弃用代码。

当前Go运行时不调用Python语音服务。后续业务模块通过明确接口接入这一语音核心，保留已验证的流式、插话排空、身份、搜索与已播记录行为。内置搜索与tools互斥等限制仍需在具体合入项中设计，标记弃用不等于已合入任何新功能。

## 非当前产品路径：Python Realtime待清理，三段式备用保留

以下源码中仍有历史`DEPRECATED_PRODUCT_PATH`标记。它不表示文件失效、云端进程已经停止，或用户最新决定保留的备用链路可以删除。三段式未来可作为独立替代方案重新验证，当前不接入产品。

| 代码/入口 | 状态与保留用途 | 当前替代/后续处理 |
|---|---|---|
| `tools/realtime-output-live/{server,upstream,audio,state,history,identity}.py` | 旧Python Realtime实现；仅临时回退、对照与差异测试参考 | 主运行由对应Go模块承担；新功能只进Go |
| `services/livekit-agent/` | 完整LiveKit ASR→LLM→TTS备用实现 | 保留Python、网页、测试、依赖和恢复说明，当前产品不调用 |
| `deploy/family-robot-livekit-dev.service` | 备用三段式18010模板 | 保留模板，本清理方案不启停或移除 |
| `cmd/livekit-device-gateway/`、`internal/devicebridge/`、`deploy/family-robot-livekit-device.service` | 备用18020→18010桥接及验证 | 完整保留，不接入当前Go产品会话 |
| `tools/native-voice/` | 备用三段式原生客户端，也含当前共用音频组件 | 保留独立App、build/open及测试；当前默认入口仍为`tools/realtime-output-live/open.command` |
| `tools/mac-voice/` | 备用三段式浏览器验证入口 | 保留复现/验证用途，不作为当前产品/主测入口 |

三段式的原稿、检查点、档案等思路仍可评估后逐项转成Go业务能力；它们没有自动接入Go。同时保留整条三段式作为未来替代候选。以后若决定启用，再核对SDK/模型权限、设备适配与实际体验；本次不创建双后端路由或自动切换。

## 仍在使用的验证参考与共用组件

以下内容不能随旧后端一并删除，也不能标成“完全没有使用”。

| 内容 | 实际用途 |
|---|---|
| 旧Realtime Python的state/history/identity/server/audio，以及`tools/realtime-output-probe/alignment.py`等 | Go测试的Python参考：`internal/realtimestate/testdata/oracle.py`读取/解析旧源码，`internal/realtimeaudio/testdata/reference.py`读取旧编解码实现 |
| `tools/realtime-output-live/test_*.py`、`tools/realtime-output-probe/test_alignment.py` | 仍可运行的冻结基线回归；不是产品语音后端 |
| `tools/realtime-output-probe/` | `LEGACY_VALIDATION_REFERENCE`：历史探针及参考帮助模块；不作为新产品运行入口 |
| `tools/livekit-audit/` | 备用三段式的SDK契约验证，保留；不属于当前Go运行依赖 |
| `tools/native-voice/Sources/`、`OpusBridge.c`、`OpusBridge.h` | 当前Go Mac客户端仍编译/使用的共用音频、AEC、Opus与播放回报实现，保持有效 |
| `tools/realtime-voice/Sources/Fixture.swift` | 当前Go客户端仍使用的合成输入调度工具，保持有效 |
| `tools/realtime-output-live/replay_history.py`、`probe_grounding.py` | 手动日志分析/研究工具，按使用场景保留；不在Go默认请求路径执行 |

`LEGACY_VALIDATION_REFERENCE`只限制产品运行用途，不禁止差异测试使用Python。新Go服务不因此产生Python子进程依赖。保留测试参考的行为基线，不借标记改提示词、音频处理或历史规则。

## 旧Go功能待逐项复用

`internal/gateway`、`internal/qwen`、`internal/audio`、`internal/protocol`、`internal/devicetools`、`internal/recordstore`等旧Go代码不整包标弃用：8000仍承担设备引导，且其中包含有效或待复用的协议、鉴权、工具、存储和身份隔离能力。先盘点依赖，再单模块适配；不整包覆盖当前Go主会话。`internal/devicebridge`属于备用三段式适配，完整保留，但不能把它当成当前Realtime的设备适配。

## 本次核对的运行状态

2026-10-25检查时：

| 服务 | 端口 | 本次状态/定位 |
|---|---:|---|
| `family-robot-realtime-output-go` | 18040 | active，当前唯一默认入口 |
| `family-robot-realtime-output-live` | 18032 | active，已弃用的Python实现，临时回退/对照 |
| `family-robot-livekit-dev` | 18010 | 当时active；按10月28日决定保留备用，本方案不退役 |
| `family-robot-livekit-device` | 18020 | 当时active；备用三段式桥，Requires上面的18010服务，本方案不退役 |
| `family-robot` | 8000 | active，原Go设备入口，暂保持兼容 |
| `family-robot-realtime-test` | 18030 | active，旧Go Realtime对照入口，暂保持 |

检查时18010/18020/18032未发现已建立TCP连接，但这只是一时快照，不等同已确认无消费者。服务依赖显示18020仍Requires18010。本次不执行stop/disable、daemon-reload、删除源码、卸载依赖或删除记录；现有窗口由用户自行关闭。

本次清理规划仅涉及18032旧Python Realtime及确认独立的对照工具，不授权启停18010/18020备用三段式。未来如另行安排三段式服务运维，需核对消费者、18020→18010依赖与恢复方式。进程状态、源码保留和历史数据分别处理，不把历史标记当作已经完成启停或删除。

## 当前开发约定

新语音能力只进入当前Go核心。旧功能复用先列功能与依赖台账，接入一项就验证该项和现有语音体验；判题、提醒、长期记忆等未实现功能仍单独排期。以最新开发交接和本页为当前状态，旧方案/报告保留其当时证据，不作为新默认入口指令。

## 本次标记验证

云端独立验证目录：`/opt/family-robot-livekit-validation/legacy-marking-20261025`，基线为4240bd2。32个Python文件的AST与基线完全一致；另5个Go入口/部署模板/启动脚本除新增注释外正文一致。共37个实现、入口或模板文件获得标记，没有运行逻辑变更。

Python原Realtime49项＋probe8项回归通过；Go realtimestate/realtimeaudio两包race测试通过（使用本次带标记的Python文件作参考），旧livekit-device-gateway构建通过；两份旧启动脚本语法检查通过。更新文档的131个本地链接已核对存在。本次没有重新安装运行依赖、调用真实模型或重启服务。
