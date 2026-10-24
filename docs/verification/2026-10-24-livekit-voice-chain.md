# 服务器 AgentSession 与三模型最小链路验证

2026-10-24。实施范围由用户明确确认：服务器AgentSession接线、取消行为、ASR→LLM→TTS实测，之后再接Mac终端。原Go网关/8000与手表均未切换。本轮新增独立loopback测试服务，不称为正式语音后端上线。

## 环境与接口

- Ubuntu24.04、Linux6.8.0-71 x86_64、Python3.12.3。
- Agents/OpenAI插件/Silero插件均1.8.8，RTC1.1.20；实际SDK与前次P0相同，不修改SDK内部计数器或源码。
- 源码：服务器`/opt/family-robot-livekit-dev`；审计与运行venv分离，后者`/opt/family-robot-livekit-runtime/venv`。
- `family-robot-livekit-dev.service`使用独立非root账户，只监听127.0.0.1:18010；Key留在既有受保护配置，测试令牌单独生成，仅存服务器。
- ASR/LLM/TTS按同一已配置业务空间的官方三个协议分别调用；未新增供应商账号。所有调用先保存无正文intent，关闭自动重试；取消/未确认终态保持unknown。

## 三模型独立真实调用

| 环节 | 实际观察 |
|---|---|
| Qwen实时TTS | 合成问题得到80640字节24k配置的单声道PCM；本次678ms，usage.characters=17。独立流式请求不是普通问答首音频延迟 |
| Fun-ASR | 将上述音频转换为16k并按设备速度发送，收到3个interim与final“请问1+1等于几？”；本次1869ms，usage.duration=2秒 |
| 文本LLM | 流式回答“2”；本次首文字409ms、总475ms，prompt18/completion1/total19 tokens |

最初ASR判定器只接受汉字“一加一”，将有效的“1+1”标成false；保留原始输出，在独立verdict中修正等价表达判定，未为修正判定器重发计费请求。请求模型是别名，未据此宣称固定云端权重版本。

## 红绿与真实反例

新增测试先因缺少对应实现失败，随后实现并在服务器运行真实SDK测试；没有在Mac安装运行本轮环境。测试涵盖协议错误、身份/PCM、取消前后、unknown、服务租约、输入提交失败和模拟播放器。

真实首轮组合测试没有通过：独立三模型都有响应，但ASR流注册表中已关闭实例令提交失败；旧提交异常未对客户端报告，SDK还可能在超时后将interim提升为final并生成回复。修复为回收/跳过已关闭流、显式报告提交失败、等真实provider终态后再提交，并对输入提交与迟到final加代次门控。

修复后的诊断轮又显示：ASR/LLM/TTS已完成，但有限音频输入结束后VAD仍处于speaking。补充700ms终止静音、输入消费与provider首帧屏障，使前端分帧和VAD确实排空；短静音测试也覆盖首帧/提交竞态。没有用改提示词或重试模型掩盖生命周期问题。

## 真实完整链路结果

取消身份修复后的最终复验使用同一真实AgentSession、三个云接口、复用的已知合成WAV和按PCM时长计时的模拟播放器：

| 场景 | 实际结果 |
|---|---|
| 第一轮“一加一” | 实际回答“1+1等于2。”，输入结束到reply事件2916ms |
| 同会话第二轮“二加三” | 实际回答“2+3等于5。”，对应2641ms |
| 数数回复开始后取消 | 返回控制取消确认10ms；不是开口到扬声器停声测量 |
| 取消后再问“二加三” | 实际回答“2+3等于5。”，对应4227ms |
| 音频身份 | 接收293帧、0个error事件，取消后未收到退休代次的迟到帧；单元测试另主动注入旧帧，确认播放器丢弃 |

上述reply事件时间包含模拟播放完成和调度，不作为首音频延迟或p95指标。`physical_audio_tested=false`；没有麦克风、Mac扬声器或手表听感验收。

私有证据：服务器`/var/lib/family-robot-livekit-dev/smoke-20261024-1`保留首次失败与合成fixture，`diagnostic-round-1/2`保留定位，`smoke-20261024-2`保留复核前通过组（2007/1684/1440ms、取消确认11ms），`smoke-20261024-3`保存上表最终复验；模型调用intent在该服务的`audit/`。本次复验复用fixture，未再次调用TTS生成输入。原始证据未提交Git。运行输出目录已存在时CLI拒绝重用，不自动重复不确定请求。

## 独立复核与最终回归

一次独立只读代码复核提出两项Important关注，无Critical；按实际复现分别处理：

1. **取消后的旧输入身份仍有效：已复现并修复。** `input.start→audio→cancel→旧audio/end`在修复前超时失败。现在取消同步退休当前输入、清空待转发PCM并取消尚未完成的commit；旧身份返回`invalid_input`，不会触发旧问题回答，随后新`input.start`仍可提交。回归测试由红转绿。
2. **旧ASR不返回终态会阻塞下一轮：未复现。** 增强测试扣住第一次ASR终态，取消后要求旧socket关闭，且不释放旧终态就完成第二轮ASR/LLM/TTS。该用例在复核前代码已通过：真实SDK的`clear_user_turn()`会关闭并替换旧识别流。保留此用例，不把它描述为已复现缺陷，也未保留额外的provider任务管理重写。

最终服务器测试：新增整组**21/21通过**（6.258秒），原SDK探针**3/3通过**；测试venv与运行venv的`pip check`均无冲突。证据位于checkout的`.superpowers/livekit-chain/evidence/`：`review-red.log`、`review-final.log`、`sdk-final.log`。红灯日志中只有旧输入身份用例失败，ASR阻塞用例当时已经通过。

实现提交`5072c57`已通过Git bundle在服务器fast-forward采用；Python源码、测试、requirements与systemd模板的SHA256逐文件核对为受测版本。原始清单保留，macOS AppleDouble元数据不计入Git源码清单。运行依赖冻结文件为`runtime-resolved-final.txt`，SHA256为`e22ea50cb3ed2027c612438d7149c51c4b6e9ebe0dd870ea89703886a5e27d1c`。报告与交接文档的后续提交不改变受测运行源码。

## 状态与限制

当前已提供[启动/停止、协议、复现命令](../../services/livekit-agent/README.md)。服务器采用受测源码后，独立测试服务重新启动且health为ok、活动会话0；非root、仅loopback、未开机自启。复验后内存约136MiB，不作为并发容量结论。正式8000服务始终运行，health为ok、活动会话0，`current`仍为`voice-gateway-20261023T061606Z-5772dfb`。

没有交付Mac麦克风客户端、Go设备桥、设备控制/存储接线、故事固定稿任务接续或手表AEC验收。当前测试配置的采样率、模型调用和有效PCM，不替代真实终端坐标标定与用户听辨。暂不启用新服务开机自启或公网入口。
