# 小智Python服务端：中断与历史实测

固定官方`xinnan-tech/xiaozhi-esp32-server`提交`fdf280f84aaa84211f680936cbfd98bf9d7be01b`。本轮检查Python主服务，不把整个仓库其他语言实现或未来版本一起作否定结论。

## 实际运行层级

从官方固定版本获取`connection.py`、`utils/dialogue.py`、`handle/abortHandle.py`、TTS DTO及上游dialogue测试；业务方法来源的Git blob均与官方API一致。动态导入原版Dialogue/Message/DTO/abort，使用AST提取未经改写的chat、clear_queues、clearSpeakStatus。网络、LLM、TTS生产端和外部sink为明确的替身；没有启动完整服务、真实ASR/LLM/TTS、数据库或Web管理后台。

模拟每个生成数字作为一条TTS文本，sink仅接受前8条，其余排队。它不是音频包、真实语速、I2S或人实际听到的证明。两个场景分别生成完整50项后abort，及已生成12项后abort；然后检查数学答复与用户“继续”之前的历史。

| 场景 | 模拟sink接受 | abort之后的计数历史 |
| --- | --- | --- |
| 生成已经完成 | 1–8 | 完整1–50 |
| 生成到12后被中断 | 1–8 | 1–12 |

两个场景均清空队列、设置client_abort、发tts.stop并清讲话状态；历史在abort前后逐字相等。加入数学回答2和用户“继续”后，原计数历史仍保留。没有调用模型生成续数答案，不据此宣称模型会从51或其他数字继续。

上游原版dialogue测试11项通过，两个自建方法级场景断言通过。保存的portable脚本改为显式source/out参数并校验固定源码SHA，复跑同样通过。不是完整小智服务端的端到端通过数。

## 为什么会保留生成前缀

- [chat](https://github.com/xinnan-tech/xiaozhi-esp32-server/blob/fdf280f84aaa84211f680936cbfd98bf9d7be01b/main/xiaozhi-server/core/connection.py#L1055)循环检查client_abort，将LLM生成的content追加到response_message并投给TTS队列；结束时`1374–1378`把response_message写入assistant历史，不等设备回报。
- [abortHandle](https://github.com/xinnan-tech/xiaozhi-esp32-server/blob/fdf280f84aaa84211f680936cbfd98bf9d7be01b/main/xiaozhi-server/core/handle/abortHandle.py)设置中断、清队列并发stop，没有按已播前缀改写Dialogue。
- [clear_queues](https://github.com/xinnan-tech/xiaozhi-esp32-server/blob/fdf280f84aaa84211f680936cbfd98bf9d7be01b/main/xiaozhi-server/core/connection.py#L1662)清TTS/音频/report队列并重置流控；[Dialogue](https://github.com/xinnan-tech/xiaozhi-esp32-server/blob/fdf280f84aaa84211f680936cbfd98bf9d7be01b/main/xiaozhi-server/core/utils/dialogue.py)序列化文本与工具消息，没有消费采样或已播字词的输入。

本轮Python主链为ASR文本→LLM→TTS；其[OpenAI兼容LLM provider](https://github.com/xinnan-tech/xiaozhi-esp32-server/blob/fdf280f84aaa84211f680936cbfd98bf9d7be01b/main/xiaozhi-server/core/providers/llm/openai/openai.py)调用chat.completions，不是千问原生Realtime音频接口。不能把该链式对照包装成与我们相同模型协议的实连排名。

## 对本项目的含义

清队列和停止生成很有用，但“历史中有已生成文本”仍不等于“历史只含用户已经听到的文本”。本测试没有发现可直接拿来解决准确断点的机制；不因此断言所有小智后端都没有任何续答能力。是否迁移服务端还要看具体协议和播放边界，不能仅凭同为小智固件就认为问题会消失。

复现工具：[xiaozhi_resume_probe.py](../../tools/voice-audit/xiaozhi_resume_probe.py)。输入是固定源码目录，不修改上游；日志/上下文位于`artifacts/resume-root-cause-20261023/{xiaozhi,portable-xiaozhi}/`。本轮无凭据、线上或硬件改动。
