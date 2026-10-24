# 计数按完整播放检查点继续

2026-10-24。用户已现场确认单按钮连续问答、语音插话及插话问题回答正常，同时发现“从1数到50，中途问2+3，再继续”会从51开始。本次修正生成进度与播放事实混淆，不以提示词猜断点。

## 行为与边界

独立CountController保存start/end/next/state。计数每个数字为固定文本/PCM单位，AgentSession.say指定音频且add_to_chat_ctx=False；只有SDK未中断、匹配块/代次、valid且played_samples=sent_samples=本数字样本量，才推进next。部分/unknown/过期回报不推进，允许重读当前数字。到原终点后直接说明已完成，不调用LLM生成下一个数字。

VAD检测新语音时先暂停计数，旁支问答走原ASR→LLM→TTS；“那继续数吧”命中原任务。最多预取3个数字，每个最多5秒PCM，暂停期间不发起新预取，已提交/已准备结果留内存供恢复。替换任务/关闭会取消并等待资源；结果未知/合成失败不自动重试。范围0..1000、递增、最多100个数字。

当前仅覆盖一次唤醒内、进程内的计数任务；未接Go任务持久化、跨连接恢复或故事字词接续。浏览器播放时钟是估算，仍不等于物理扬声器的声学测量。Mac只运行测试页面与SSH，依赖、模型和运行时测试均在服务器，8000原服务不切换。

## 自动化证据

核心与接线按测试先行开发；服务器`.superpowers/counting/`保存core-red.log、core-edge-red.log、sdk-red.log、continuous-red.log、client-red.log以及最终绿灯日志。核心额外反例覆盖resume后立即pause的唤醒竞态，确保暂停期间不启动新合成。

真实SDK/Silero受控边界回归已验证：数1到3，完整确认1，2未完成时VAD插话，回答二加三为5，再从2恢复到3；再次继续说明已经到3。仅旁支调用一次LLM，数字稿未进入助手历史。PCM回归覆盖完整、部分、unknown、取消收尾。核心还验证1到50在3被打断后重读3，最终只合成/播放到50，completed的next=51只为终态哨兵。

服务器全量60项Python（最终38.877秒）、24项Node（1.133秒）及3项原SDK探针通过，合计87项，无fixture跳过；pip check无冲突。最终证据为python-final.log、js-final.log、sdk-final.log。独立规格静态复核与质量静态复核通过，本地与服务器29份Python/JS/HTML/CSS源码摘要一致。

质量复核发现一个取消收尾竞态：仅调用SpeechHandle.interrupt便退出回调时，旧PCM块仍未结算，快速恢复可能提前记录旧epoch。真实SDK测试cleanup-red.log已复现；改为interrupt后等待同handle.wait_for_playout，再释放Controller替换屏障，cleanup-green.log的9项SDK/VAD回归通过，最终全量再次执行。未改SDK内部实现，也未创建未跟踪的后台等待任务。

## 真实云端验证

真实Fun-ASR/LLM/Qwen TTS、同一连续AgentSession和按PCM时长计时的模拟播放器完成如下链路：

| 场景 | 结果 |
|---|---|
| 请求从1数到50，途中直接开口问二加三 | 计数暂停于next=4，原end=50；回答5 |
| 直接说“那继续数吧” | resumed_next=4，从原检查点恢复 |
| 恢复到原终点 | completed_end=50 |
| 完成后再次说继续 | 固定说明已经数到50，不再启动计数 |
| 协议 | 2902个listen.audio、53次playback、0个error；无手动分轮/停播消息 |

最终证据目录`/var/lib/family-robot-livekit-dev/counting-20261024-2`，结果ok=true、physical_audio_tested=false；脚本与输出摘要另存服务器`.superpowers/counting/live-counting.py/live-final.log`。此前第一组counting-20261024-1也通过，保留作取消收尾修正前的独立证据；最终组在该修正后重新执行。两组均使用既有合成计数/二加三输入，仅第一组新合成一句“那继续数吧”，最终组复用。无真实麦克风录音，私有WAV/事件/配置不进Git。模拟客户端按时长回报证明服务与检查点链路，不能替代Mac实际听感验收。

## Mac现场

此前用户确认仅证明连续收音/自动插话/旁支问答可用，不能作为本次计数修复的验收。新版本部署后仍须现场重复“从1到50 → 中途问2+3 → 那继续数吧”，核对暂停数字与原终点；此项待用户反馈。
