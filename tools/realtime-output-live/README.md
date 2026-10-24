# 真人Realtime已播位置实验

运行`./tools/realtime-output-live/open.command`，打开“家庭陪伴 · Realtime 已播位置实验”。点一次开始并允许麦克风，之后自由说话。结束按钮停止收音，窗口保留；关窗退出并清理本启动器的SSH转发。不要同时开启其他语音窗口的对话。

当前真人验收先用数数：说“从1数到50”，播放中插问“2加3等于几”，等回答后说“继续刚才的数数”。对照最后听到的数字和续数起点，再换语速/打断点。实现没有计数专用恢复规则。故事用于之后验证语义自然度。

这是独立实验，不是18030日常入口。云端18032回环、一次性票据、单个连接、拒绝浏览器Origin。Mac只构建/运行客户端；Key只留服务器，不安装依赖。依赖已有Python3.12/aiohttp/libopus与Mac Xcode/libopus。使用audioop，未适配Python3.13。

## 数据路径与差别

Mac Voice Processing持续收音，16k/20ms Opus上传；云端原生千问连接只做VAD与输入转写。按原生VAD音频时间戳切出实际话段，原音频只在内存保留最长35秒；每段最多25秒。每轮新千问原生音频连接接该话段＋最近16轮文字背景，边生成边输出。这里多了原生VAD监听连接和逐轮建连，不能当成原18030无改动性能。没有接入输入Fun-ASR、文本LLM或TTS，也没有共享三段式档案。

输出按24k/60ms Opus放行，常规完成回报窗口约300ms。编码和解码保持同一时间轴，**只有实际下发包**解码后进入输出Fun-ASR。没有预备整段源音频给ASR。Mac用CoreAudio完成包回报；插话取消旧生成并停本地播放，包携带响应序号防止迟到串音。未知位置不自动当成完成；严格最终ASR前缀校验失败则unknown。120ms保护量仍是实验值，不保证声学逐字准确。

下一轮只带用户转写和候选已播前缀，不把旧未播生成稿放进模型背景。有界等待旧ASR最多1.2秒，通常与用户新一句话的收音并行。每轮重建会丢失此前部分声音上下文；仅保留近16轮，不是长期记忆。

## 云端运行与记录

在服务器checkout根目录使用既有环境：

```sh
/opt/family-robot-livekit-runtime/venv/bin/python -m unittest discover -s tools/realtime-output-live -v
/opt/family-robot-livekit-runtime/venv/bin/python tools/realtime-output-live/server.py
```

本轮通过独立临时systemd单元`family-robot-realtime-output-live.service`保持运行。只读健康检查`http://127.0.0.1:18032/healthz`。不修改8000、18010、18020、18030的配置或入口。关闭客户端释放会话，服务不自动关停，以便用户反复开始。

私有文字证据在`/var/lib/family-robot-realtime-test/verification/output-live/<session>/events.jsonl`：输入转写、带未播标记的生成稿、ASR稳定字时间戳、播放回报和实际送入下一轮的背景分别记录。音频仅内存；普通日志只记录事件/计数/错误类型，不输出正文、票据或Key。真实模型调用前落无正文intent，无未知结果自动重试。与三段式存档独立。

合成输入检查可加`--fixtures /absolute/path/plan.json`，复用原生客户端的按20ms定时输入工具；不启用麦克风。它验证实时I/O与传输，不替代用户声学核对和准确接续验收。当前已观察到严格ASR匹配返回unknown的情况，参见[验证记录](../../docs/verification/2026-10-24-realtime-output-live.md)。
