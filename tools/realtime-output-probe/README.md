# Realtime输出ASR与实际播放前缀实验

这是机器合成输入的隔离探针，不是日常Realtime入口，也未改造三段式。目标是先验证：机器人输出的Fun-ASR字时间戳是否能和Mac实际播放样本对上、稳定前缀何时可用、用该前缀恢复新千问会话能否衔接。当前不保证原稿逐字恢复或真人交流零延迟。

## 组成

- `providers.py`：只在服务器读取独立Realtime配置；真实千问和Fun-ASR调用，有界输入/输出/超时，调用前保存无正文intent，无自动重试。稳定ASR句子可增量提供，不等待全文完成。
- `codec.py`：24k/60ms Opus编码并解码，ASR与Mac共同使用解码后样本时间轴，包含编码延迟/填充。
- `alignment.py`：只接受ASR最终结果、连续句序与有效时间戳；与生成稿前缀去标点后严格匹配，按实际播放样本和120ms实验保护量选择候选。时间戳属于识别估计，不是声学逐字证明；不一致返回unknown。
- `probe.py`：预先生成故事供播放/识别，临时回环18031。客户端在完整CoreAudio包边界停止；服务端用已稳定前缀（最多等1.2秒）恢复独立新千问会话，再提交机器合成的数学插问及继续请求。不把旧未播稿传给新会话。
- `OutputProbe.swift`与`play.command`：Mac无麦克风播放客户端，实际CoreAudio `dataPlayedBack`回报；结束后只清理自己的18031转发，不改原生窗口。切断后丢弃有限迟到旧包，收到下一条有序begin才接受新音频。
- `transport_fixture.py`：仅云端运行的无模型回归服务，主动制造切断后的迟到音频包；不读取模型配置，不产生模型费用。

## 运行

依赖沿用服务器已有Python3.12/aiohttp/libopus；重采样暂用Python3.12的audioop，因此不能直接运行在删除audioop的Python3.13环境。Mac只使用已有Xcode/libopus构建原生客户端，不安装依赖或启动服务。

输入目录需要机器合成的16k/单声道/PCM16 WAV：`story.wav`（请求森林故事）、`math.wav`（插问二加三）、`resume.wav`（复述讲到哪里并继续）。不要用真人录音。源输入及所有输出置于服务器受保护目录，不进入Git。

在服务器仓库根目录，以可读取受保护独立配置的身份执行：

```sh
/opt/family-robot-livekit-runtime/venv/bin/python tools/realtime-output-probe/probe.py prepare \
  --work /var/lib/family-robot-realtime-test/verification/output-asr-new-run \
  --inputs /var/lib/family-robot-realtime-test/verification/output-asr-inputs
/opt/family-robot-livekit-runtime/venv/bin/python tools/realtime-output-probe/probe.py serve \
  --work /var/lib/family-robot-realtime-test/verification/output-asr-new-run \
  --inputs /var/lib/family-robot-realtime-test/verification/output-asr-inputs
```

prepare要求新目录，调用一次千问生成及一次输出ASR；serve再次调用输出ASR、新会话数学与继续，均产生模型用量。失败保留intent和证据，不能通过重新运行伪装同一次请求成功。临时服务只供本机SSH转发使用，拒绝浏览器Origin；单次客户端后自动退出，等待窗口180秒，无公众部署入口。

在Mac运行 `./tools/realtime-output-probe/play.command`。它建立18031专用SSH转发，播放机器生成的故事并在默认4200ms已完成包边界停止，继续播放后续实验回复。整个过程不申请或开启麦克风。

云端单元测试：`python -m unittest discover -s tools/realtime-output-probe -v`。无模型传输回归：云端运行`transport_fixture.py`，Mac运行相同play.command。

## 结果边界

实验提前备好完整源音频，因此ASR可提前处理将来才播放的声音。这验证了对齐、播放回报、增量前缀和新会话背景恢复；它不代表实时生成时也能同样早拿到结果，尚未测真人输入/VAD、逐包在线旁路、长期压力或最终听感。

新会话通过明确标注的背景数据恢复文字信息，会损失原会话部分声音上下文。探针只保留简短的原故事请求，原任务完整范围管理未接入；继续是语义续讲，不是未播原稿冻结后续播。实例不共享三段式档案或任务模块。
