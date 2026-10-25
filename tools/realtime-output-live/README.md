# 真人Realtime已播位置实验

运行`./tools/realtime-output-live/open.command`，打开“家庭陪伴 · Realtime 排空测试”。点一次开始并允许麦克风，之后自由说话。结束按钮停止收音，窗口保留；关窗退出并清理本启动器的SSH转发。不要同时开启其他语音窗口的对话。

当前先听排空策略：让它讲一个故事，播放中插问“2加3等于几”；听旧尾音多久停、新回答多久开始。之后说“继续刚才的故事”，核对语义衔接。也可以用数数核对候选断点，实现没有计数专用规则。准确接续位置仍未验收。

这是独立实验，不是18030日常入口。云端18032回环、一次性票据、单个连接、拒绝浏览器Origin。Mac只构建/运行客户端；Key只留服务器，不安装依赖。依赖已有Python3.12/aiohttp/libopus与Mac Xcode/libopus。使用audioop，未适配Python3.13。

## 数据路径与差别

Mac Voice Processing持续收音，16k/20ms Opus上传；云端原生千问连接只做VAD与输入转写。按原生VAD音频时间戳切出实际话段，原音频只在内存保留最长35秒；每段最多25秒。每轮新千问原生音频连接接该话段＋最近16轮文字背景，边生成边输出。这里多了原生VAD监听连接和逐轮建连，不能当成原18030无改动性能。没有接入输入Fun-ASR、文本LLM或TTS，也没有共享三段式档案。

输出按24k/60ms Opus放行，常规完成回报窗口约300ms。编码和解码保持同一时间轴，**只有实际下发包**解码后进入输出Fun-ASR。没有预备整段源音频给ASR。包携带响应序号防止迟到串音。插话取消旧生成，未下发旧队列随任务取消释放；播放端保留已收到音频并排空，不能立即清空。120ms保护量仍是实验值，不保证声学逐字准确。

服务器封口发 `drain {id, sent}`。播放端核对 `accepted == sent`，等全部 CoreAudio `dataPlayedBack` 后回 `receipt`，其中 `terminal=drained`、`completed == accepted == sent`。封口与包发送用同一锁排序。旧回复完成确认前，输入仍持续收音，但下一轮背景快照与新输出不能越过这个栅栏；3秒无完成确认则提示并断开，重新开始可重连。正常 end 与 drain 竞争也会确认同一边界。主动结束/关窗仍立刻关闭音频设备。

下一轮带用户转写和结构化已播证据，不把旧未播生成稿放进模型背景。有界等待旧ASR最多1.2秒，通常与用户新一句话的收音并行。每轮重建会丢失此前部分声音上下文；仅保留近16轮，不是长期记忆。排空对齐的是下发和播放的样本边界，不会自动截断千问内部历史，也不保证输出ASR精确对齐每个字。本实验靠新连接和恢复背景隔离旧未播稿。

## 已播历史与回查约束

`playback_evidence`保留两种来源：中断回复的最终输出ASR安全时间前缀，以及整段正常生成且全量完成播放的原生转写。常见中阿数字和宽度格式可保守对齐原稿；有歧义的数字串不强行等价。对齐失败仍保留ASR观察文本，不能整段null。缺句或非法后续时间停止扩展，保留此前有效部分。原始严格probe候选仍单独记录用于比较。

`source_playback_records`索引源内容的播放记录；历史提问的助手回答保留在`recent_turns`并标注reference角色，不允许把“我说过数到五”当作数数源音频的进度。角色判断使用保守的中文问题形态提示，只影响证据范围，不直接路由生成话术。识别有误或说法未覆盖时可能漏判。

对明确回查播放位置/历史的问题，在第一条原生输出事件时依据已到达的输入转写选择播前检查。该类回复先暂存音频，完成后检查常见无证据数字、缺少边界限定或错误“没有记录”的表述；已完成但未获放行的候选只允许模型重写一次，仍失败则可视提示本轮未完成。普通聊天继续逐包流式输出；没有固定兜底语音、额外文本LLM或独立TTS。语义重写是已知拒绝后的新生成，不对提供方未知结果自动重试。

该检查依赖问题识别、输出转写和有限中文表达形态，不是通用语义验证器，也不保证所有故事位置或任意语言都不会误报。原始ASR和120ms边界仍有误差。回查需要等待完整短回复，若重写还会增加一次模型生成等待；不能将其时延宣传为普通流式首包。

这是一项通用播放端策略，当前只有Mac适配和验证。微信小程序、App、ESP32固件都需分别实现封口、排空与可信完成回报；不是改服务器后它们就自动具备该行为。见[本轮实现计划](../../docs/superpowers/plans/2026-10-25-realtime-drain-test.md)。

## 云端运行与记录

在服务器checkout根目录使用既有环境：

```sh
/opt/family-robot-livekit-runtime/venv/bin/python -m unittest discover -s tools/realtime-output-live -v
/opt/family-robot-livekit-runtime/venv/bin/python tools/realtime-output-live/server.py
```

本轮通过独立临时systemd单元`family-robot-realtime-output-live.service`保持运行。只读健康检查`http://127.0.0.1:18032/healthz`。不修改8000、18010、18020、18030的配置或入口。关闭客户端释放会话，服务不自动关停，以便用户反复开始。

私有文字证据在`/var/lib/family-robot-realtime-test/verification/output-live/<session>/events.jsonl`：输入转写、带未播标记的生成稿、ASR稳定字时间戳、播放回报和实际送入下一轮的背景分别记录。音频仅内存；普通日志只记录事件/计数/错误类型，不输出正文、票据或Key。真实模型调用前落无正文intent，无未知结果自动重试。与三段式存档独立。

合成输入检查可加`--fixtures /absolute/path/plan.json`，复用原生客户端的按20ms定时输入工具；不启用麦克风。它验证实时I/O与传输，不替代用户声学核对和准确接续验收。当前已观察到严格ASR匹配返回unknown的情况，参见[验证记录](../../docs/verification/2026-10-24-realtime-output-live.md)。

云端`replay_history.py /private/path/events.jsonl`只输出历史重放的统计；`probe_grounding.py --events /private/path/events.jsonl --turn N --wav /private/synthetic.wav --run-provider`明确运行真实模型回归，默认不打印正文，`--show-reply`才显示生成回答。结果/调用intent保留服务器私有目录，合成探针不落音频，不替代真人验收。
