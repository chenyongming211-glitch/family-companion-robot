# 原生不回复与快速重连失败修复

日期：2026-10-24，基线`68d5c90`。用户报告不回复，截图显示`event_before_ready`。前轮只有窗口/收音连接检查和自动回归，不能据此宣布真人问答已经通过。

## 根因与修复

1. Go桥在客户端断开后释放会话名额，而Python仍需约2秒清理AgentSession。新连接立即到达时Python返回409，Go转成`backend_unavailable`；原生客户端先检查ready，错误又被掩盖成`event_before_ready`。实连复现6次尝试中4次失败。现在Go仅针对这个未建立语音会话的409等待，合计最多3秒，不重发音频或模型请求；其他错误不重试。原生先处理服务错误，保留窗口。
2. 原稿校验用末尾标点猜测生成是否完成。真实模型在合成测试请求下正常返回单个数字`5`、`finish_reason=stop`，旧校验仍拒绝；完整括号结尾也会误拒。固定LiveKit1.8.8插件丢弃了这个完成原因，现使用每条流独立的适配保留`finish_reason`，只有正常stop可接纳原稿；length、content_filter和缺失终态均拒绝。仍保留文本/单位/令牌额度，既不补造句号也不截掉尾稿。
3. 草稿失败曾发全会话`error`，导致桥与播放器关闭。现在发`turn.error`并结束本轮生成，保留麦克风、原稿和连接，用户可直接继续说话；原生界面给出本轮失败提示。fixture遇到它仍明确失败，不伪报通过。

旧现场正文未保存，不能确定此前那轮`ending`失败的具体末字符。此次用可复现的正常数字/括号回答验证了误判机制。

## 验证

- Go瞬时409回归先失败，再通过；全库`go test -race ./...`、`go vet ./...`与桥二进制构建通过。
- Python真实SDK/VAD、受控提供方全量116项通过，110.129秒。覆盖无句末标点正常回复、带句号但length/缺失终态不播放、失败后下一次发言能播放新答案且未准备原请求仍保留。
- 真实云端ASR→LLM→TTS连续3轮合成语音“二加三等于几”，均识别完成、答案包含五、返回音频并完成内容；分别收到28、36、24帧PCM。本层使用模拟播放回执，不算物理播放。
- 部署后通过真实18020→18010连续6次断开后立即重新配对，6次全部ready；首轮124ms，其余约2.08—2.10秒。未发送用户音频或重试问题。
- Mac原生终端使用`--pair-on-start --keep-open --fixtures`实连云端：三句故事→实际播放中注入问题→旁支完成→恢复同一`main:1 / revision1`，3句全部完成、最终ACK齐全。`fixture.success`基于CoreAudio实际`.dataPlayedBack`，恢复句索引0；合成输入，不是新一轮真人麦克风或声学AEC验收。
- 原8000服务保持active且PID仍为2261506，只重启独立18010/18020服务。

云端日志：`.superpowers/reply-fix/`内的`reconnect-red.log`、`reconnect-green.log`、`draft-red.log`、`draft-green.log`、`python-full.log`、`go-full.log`、`live-reconnect.jsonl`。Mac日志：`.superpowers/native-voice/reply-fix-real-client.jsonl`与`reply-fix-build-final.log`。只保留元数据，不保存用户语音或正文，均不进入Git。

原生窗口按常开入口重新打开，运行状态仍以当时界面和日志为准。真实用户听感与AEC压力测试继续由该窗口进行，不能用上述合成测试代替。

## 现场麦克风会话补充

修复后的GUI实际打开并启用麦克风，闲置上行已持续超过两分钟。随后同一会话的元数据已观察到7条内容完整播放（包含故事）、16个最终播放ACK、1262次CoreAudio完成回调，`session.failure=0`。这些数据来自`.superpowers/native-voice/reply-fix-live.jsonl`，不是fixture注入；未保存或输出用户说话正文。它证明当前麦克风输入到实际播放回调已经产生多轮回复，不能替代用户听感确认或完整AEC压力验收。窗口继续保留，后续Git同步不重启服务或关闭会话。
