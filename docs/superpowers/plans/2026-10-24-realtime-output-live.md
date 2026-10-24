# 真人Realtime输出ASR测试入口实施计划

**Goal:** 打开真正接入已播前缀恢复的独立原生窗口，供用户真人插话测试。
**Architecture:** 云端原生Qwen VAD监听＋逐轮原音频Qwen生成，输出Opus/CoreAudio回报和只读已下发音频的增量Fun-ASR；Mac独立窗口，不改日常入口。
**Tech Stack:** Python3.12/aiohttp/libopus、Swift/AppKit/AVAudioEngine、现有千问/Fun-ASR权限。

- [x] `tools/realtime-output-live/test_state.py`：云端先失败后通过；6项状态测试。
- [x] `audio.py`/`upstream.py`：有界持续Opus、原生监听/流式生成及只看实际下发音频的输出ASR。
- [x] `server.py`：票据、单会话、VAD样本切片、代次/窗口与私有记录；独立18032。
- [x] 原生单按钮窗口和构建启动脚本；原客户端不变，最新提示优先数数。
- [x] 云端6＋8项回归、Mac构建、两轮按真实时长合成I/O；第2轮unknown明确未算准确接续通过。
- [x] 文档与真人窗口就绪；只提交本轮源码、同步main，不重启原服务及已开始收音的实验服务。
- [ ] 用户实际听感、数数停点/续点、不同语速与多次插话：等待现场结果，不代填通过。
