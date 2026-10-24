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

此前用户确认仅证明连续收音/自动插话/旁支问答可用，不能作为本次计数修复的验收。38093fb版本现场复验仍失败，用户报告继续起点有问题；不得用上面的模拟成功覆盖此反馈。用户随后要求由助手自行测试，后续证据如下。

## 口语路由遗漏及后续修正

现场页面最后转写是“啊那继续数吧。”。原解析器对该句返回False，对“那继续数吧。”返回True，服务器直接复现；旧入口漏接时会把任务交回普通模型，而计数固定稿不在模型历史中，模型也没有只读播放检查点。因此已复现一个明确的错误入口；未取得用户实际停点/恢复点，不能断言它解释现场所有偏差。

后续在原方案内补齐：锚定的口语前缀规范化；模型可调用无起点参数的resume_counting工具，工具只恢复Controller实际next/end，StopResponse阻止工具后自动续稿；普通模型每轮通过上下文副本获得只读任务事实，可以区分完整播放前缀和未确认的当前数字。否定、故事、从头重数不误归直接恢复。新开口递增代次，工具必须匹配首次LLM步骤绑定的SpeechHandle代次，即使短开口已回到listening也不能执行旧工具。数数开始前等前一SDK语音完成再记录块身份。

页面仅在内存保留有界路由/计数/播放元数据日志，结束后显示上次检查点并标注会话已结束，下次唤醒不能恢复它；转写/回答不进入该日志，不写localStorage或文件。

服务器回归覆盖口语漏接、真实SDK工具恢复且不二次LLM生成、短开口后旧工具失效、前序语音身份、日志正文隔离。红绿日志位于`.superpowers/counting-language/`：red.log、client-red.log、prefix-red/green.log、onset-red/green.log。最终65项Python（42.264秒）、25项Node（1.006秒）及3项原SDK探针通过，合计93项，无fixture跳过；pip check无冲突。对应python-final.log、js-final.log、sdk-final.log。独立规格复核发现并修正短发言结束后旧工具可执行的问题，随后规格/质量静态复核均通过。

## 助手在Mac实际浏览器自行测试

临时探针只在服务器loopback运行，经SSH映射到Mac；直接取生产client.mjs的Speaker代码及audio-core.mjs，使用真实Web Audio输出时钟。自动点击开始后使用5个已知定长提示音，在第3段中间暂停、保守重播第3段，再完成第5段；无需麦克风、无需用户报数字。

`browser-count-20261024-2`结果ok=true：第3段14400个采样中，浏览器回报2758个已输出、valid=true；暂停检查点仍为3，恢复后终点5、prepared=[1,2,3,4,5]，证明未重复准备当前段或跳过它。页面可见PASS，私有截图为Mac`.superpowers/browser-count-pass.png`。未将浏览器时钟估算升级为物理扬声器测量，physical_audio_tested仍为false。

此前`browser-count-20261024-1`使用云端数字音频，已确认第3段部分回报2886/13440、恢复重播3；但准备4时云TTS连接早闭，记录WSMessageTypeError/outcome_unknown，整体ok=false。该失败单独保留，没有自动重试未知提交，也没有把它写成完整通过；第二组使用无云调用的固定音频隔离浏览器协议链路。

实际ASR/模型/TTS的自然语言验证另在服务器执行，与Mac浏览器时钟验证分开记录；无用户录音或正文落盘。最终源码验证目录`counting-language-20261024-2`（第一组同名-1也通过，最终组复用其合成输入，不再生成fixture）：

| 场景 | 最终结果 |
|---|---|
| 数1至8，二加三插话后说“啊，那继续数吧” | 暂停2，直接任务路由恢复next=2、end=8 |
| 再次插话后说“把没数完的那一段接上” | 暂停4，实际模型调用resume_counting，恢复next=4、end=8 |
| 最终状态 | completed，end=8；next=9只为终态哨兵 |
| 模型调用 | LLM共3次：两次数学回答、一次工具决策，没有工具后再次生成数字稿；0个error |

该组ok=true、physical_audio_tested=false；SDK代次守卫为最终源码，结果/脚本摘要为`.superpowers/counting-language/live-final.log`。这证明真实云语音理解与任务恢复，以及独立Mac浏览器回报链路；未声称助手通过麦克风听见了物理扬声器，也没有要求用户再报停点来完成这次自测。
