# 独立 Realtime / 小智 Mac 体验入口

日期：2026-10-24。范围为独立体验对比，不是双后端集成。用户要求保留现有三段式，并补充原Realtime早已部署且接入小智；本轮据此复用原有网关代码，没有重新开发一套Realtime语音核心。

## 实际部署

核对现有`family-robot.service`运行于8000，发布目录`voice-gateway-20261023T061606Z-5772dfb`，模型别名`qwen3.8-omni-flash-realtime`、Tina、semantic_vad；检查时1个活动会话、上限1。因此不占用该入口，另运行`family-robot-realtime-test.service`，回环`127.0.0.1:18030`。

新增命令`cmd/realtime-mac-test`直接引用现有`internal/gateway`、`internal/qwen`、Opus与小智协议。只增加一次性票据、专用测试设备身份和独立启动配置；网关内存背景、任务规则、中断后上游重连仍沿用旧实现。该实例不是没有应用逻辑的纯模型基准，也不接三段式的历史查询、准备回应、固定稿或检查点。

独立受保护配置为服务器`/etc/family-robot/realtime-test.env`，仅在服务器提取模型Key、端点、音色、VAD及可选人设；权限0600，不包含正式设备token/OTA秘密。内部网关token每次启动随机生成；Mac只接收120秒单次票据。未配置RecordDirectory，不加载或保存正式记录；文字只在本会话内存中作为原有网关上下文使用。

新服务PID42018。部署后核对原8000 PID2261506、18010 PID4188798、18020 PID4188800均未变；没有重启原服务、切换手表或重刷固件。

新二进制SHA256：`a696aa74575afed97e8feeaba5fc3c8fcea26192bcc6641cc0e7f8e230765403`。源码构建使用现有云端Go1.27.3与libopus；不在Mac搭建服务或安装依赖。

## Mac入口与播放边界

`tools/realtime-voice/open.command`打开“家庭陪伴 · Realtime 独立测试”。开始时配对、请求麦克风权限；一次开始后持续收音，结束后窗口保留可重连；关窗清理自己的SSH转发。与三段式窗口交替使用，先结束另一窗口的对话。

静态复用已有AVAudioEngine Voice Processing、第一声道转单声道、Opus和CoreAudio音频代码；没有改动原客户端。两边同为16k输入/24k输出，20ms上行/60ms下行Opus。Realtime沿用旧网关的中断立即清空策略；三段式使用drain，比较中单列这个差异。

旧Realtime播放回报契约只接受`i2s_dma`，Mac不协商或伪造该能力。正常播放结束由本地CoreAudio `dataPlayedBack`确认；这不是服务端已收到逐字播放位置，也不等于声学核听。固定稿精确恢复继续作为能力限制，不为测试移植三段式机制。

## 回归与真实音频验证

- 新入口先只写测试，云端执行因缺失Server/New而失败；实现后握手进入实际旧网关、票据单用/过期、Origin、设备身份、OTA隔离、有限票据及无凭据health检查通过。
- 云端`go test -race ./...`、`go vet ./...`、`go build ./...`均通过。vet发现新测试一处未检查HTTP响应错误，补齐后通过。
- 原三段式`python -m unittest discover -s tests -v`：**214项通过，226.511秒**；`pip check`无依赖冲突。首次误用pytest命令发现该环境未安装pytest，改用项目已有unittest，不新增依赖。
- Mac纯Swift状态测试通过：端点限制、正常stop不等于播放完成、旧代完成不会结束新输出、有限输出与完成样本校验。原生App构建、签名通过。
- 机器合成中文语音通过真实上游及Mac实际CoreAudio：介绍小猫名字→追问名字→讲长故事→故事播放600ms后开始插问二加三。首轮与最终修复后均成功：3个正常回答完整播放、1次插话；名字追问及数学答案的生成转写断言通过。没有采集真人麦克风，也未把生成转写当作用户实际听到的文字。

独立审查发现1项P2：上一条正常stop后，尾音未完成时下一条start会错误关闭会话。用云端受控小智服务向实际Mac客户端发送连续两条回复，修复前真实出现`overlapping_tts`并失败；新增最多16个60ms包的等待区后，上一段实际播完再交接，两段均获得4320样本CoreAudio完成回报，通过。未起播的待播回复若被中断会撤销。该测试只使用合成静音Opus，无真实模型/凭据，临时服务及18031转发测试后清理。

修复后再跑真实四轮流程，仍得到`realtime.fixture_success`，3个正常输出、1次插话，数学输出43200样本实际完成。合成输入复测中出现过`audio.underrun`诊断，保留在证据中，不声称零卡顿或长期稳定领先。

本机忽略目录`.superpowers/realtime-voice/`保留`fixture-first.log`、`fixture-final.log`、`handoff-red.log`、`handoff-green.log`和构建记录；机器合成WAV不进Git。云端回归记录在`/var/lib/family-robot-realtime-test/verification/`，日志无真人正文/原始音频或Key。

## 体验对比尚待完成

新窗口供用户直接体验，不自动开启麦克风。重点比较自然度、等待感、抢话/误停、插问后新答案、连续指代、续讲和持续稳定性。准备回应首播与实质答案首播分别记录；Tina/Cherry音色偏好单列。

本轮证明独立入口可用、真实模型与原生播放闭环成立；没有证明真人AEC/主观听感、按原句断点恢复、15分钟长期压力结果、P50/P95或Realtime优于三段式。是否需要保留双链路或进一步集成，仍由后续体验决定。
