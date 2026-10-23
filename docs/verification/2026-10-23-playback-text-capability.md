# 播放采样与文字位置：能力核对

当前可以可靠保存原请求和设备播放采样；尚不能可靠回答“打断时说到了哪个字”。本轮先核对原生接口和实际事件，不接入辅助 ASR，不用生成稿、包序号或时间比例推算已听文字。

## 原生事件实测

使用服务器已有机器合成的数数输入，单独建立千问原生 WebSocket 会话：qwen3.8-omni-flash-realtime、Tina、16kHz 输入、24kHz 输出。manual commit 后显式 response.create，输出只在内存中统计，不保存或播放音频，也没有设备参与。此探针使用简化履约提示词，只验证协议能力，不作为正式提示词效果或连续收音性能对照。

实际收到 98 个音频增量、37 个文字增量，response.done=completed：

| 观测 | 本轮单样本 |
| --- | --- |
| 首个/最后音频增量到达 | 连接起点后916 / 6177ms |
| 输出PCM总时长 | 31.36秒 |
| 首个/最后文字增量到达 | 566 / 1394ms |
| 全部文字长度 | 140字符 |

即使假定收到首帧立即按正常速度播放，文字全部到齐时也只过去478ms。这个计算仅说明事件到达时间不能代表播放位置，**不证明当时已播放哪个字**。本轮没有实际播放，不能把31.36秒音频在5.261秒内到齐当成设备性能。

四类实际输出事件的字段集合如下；所有包都纳入集合，不只抽查第一包：

```text
response.audio.delta:
  type,event_id,response_id,item_id,output_index,content_index,delta
response.audio.done:
  type,event_id,response_id,item_id,output_index,content_index
response.audio_transcript.delta:
  type,event_id,response_id,item_id,output_index,content_index,delta
response.audio_transcript.done:
  type,event_id,response_id,item_id,output_index,content_index,transcript
```

本轮没有返回字词时间戳、音频采样区间或文字增量对应的音频区间。response/item/content身份只能归属同一内容，不能把两个delta序列按序号一一配对。当前[千问服务端事件文档](https://help.aliyun.com/zh/model-studio/server-events)所列字段与本轮一致；[客户端事件文档](https://help.aliyun.com/zh/model-studio/client-events)没有列出逐字对齐配置。结合此前truncate未得到有效确认，当前不能依赖这两项完成精确文字裁剪；这不是对未来型号或所有接口能力的断言。

私有证据：`artifacts/wake-fix-20261023/alignment-capability.json`，只含字段名、累计样本/字符数和到达时刻，没有输出音频或正文。

## 需要补齐的对应契约

1. 位置必须绑定同一设备连接、播放块和输出内容；上游重连、旁支回复和工具续答不能错配。I2S最终有效回报用于结算采样前缀，定时器或WebSocket收包数不能冒充它。
2. 对齐结果必须描述与该播放块匹配的音频坐标；网关下行按1440采样分帧，末帧会补零，工具续答可共享播放块。后续还需核验Opus编码/解码延迟，不能直接假设云端PCM坐标等于设备解码坐标。
3. 只有已验证、单调、未越过最终有效播放前缀的字词区间才能进入“已播文字”；缺失、失效或位于未完成字词内部均保留未知，不能给出精确数字。
4. 原请求与终点是任务目标；已播字词是进度。修复前者不等于后者完成，不能让完整生成稿把目标改成51–100。

当前生产实现仍返回`text_position=unknown`。未实现新的对齐器或精确继续位置。本轮真实隔离复验中，保存的原请求已正确，但继续位置分别出现从46继续和从1重读，说明位置问题仍然开放。先保留这两条失败证据，后续对齐实验应以它们为验收对象；不再以“能打断、能回答数学题”代替接续验收。
