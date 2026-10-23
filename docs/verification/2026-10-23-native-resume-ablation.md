# 千问接续：合成上下文信息消融

本轮完成8条真实API续答样本（8个独立会话，含3次工具查询，共11个completed response），比较四种上下文各两次。所有输入转写均为同一句“请继续刚才没有完成的数数任务，一直数到原来要求的终点”。**没有手表参与，没有播放或保存输出音频；结果只核对生成转写。**

## 实验配置

以当前Go基础指令、口头履约段、历史说明及暂停任务工具指令拼接session instructions。只提供两个会话工具，设备控制标为不可用；不是实机MCP三项控制工具齐全的正式设备配置。每次新建千问会话，把固定历史作为系统消息中的JSON数据，上传既有机器合成continue.wav，manual commit后response.create。没有实际经历本次唤醒、上一轮音频、中途VAD取消、Opus、I2S、上游重连或额外的暂停任务查询复述。

历史固定为：用户要求完整1–50、assistant生成完整1–50但interrupted、用户插话问一加一、assistant回答二并标playback_confirmed。固定采样数字只是合成状态标签，不是实际音频对齐结果。已播文字“1–8”同样是人为给定的反事实条件，绝不由采样或语速倒推。

| 条件 | 与基准的差异 |
| --- | --- |
| current_full_draft | 保留完整generated_text，任务与位置仅可通过工具查询 |
| draft_removed | 仅删除被中断计数回复的generated_text |
| task_snapshot_unknown | 基准上自动附paused_task，含原请求/采样，text_position仍unknown |
| synthetic_oracle | 在上一条件上，将位置标签改为synthetic_oracle，同时附played_text=1–8及last_completed_item=8；仍保留完整生成稿 |

工具查询在前三组返回相同的原请求与unknown文字位置；oracle组若查询则返回相应合成明确进度。工具状态变更禁用，不执行硬件动作。

两轮请求分别设置seed=20261023、20261024，每轮按上表固定顺序执行。只记录了session.updated事件类型，没有保存各次effective seed；不能称服务端确定性或严格随机配对实验。结束时读取的当前环境为qwen3.8-omni-flash-realtime / Tina，不代替各次session.updated回显证据。

## 实际结果

| 条件 | 第一轮生成转写 | 第二轮生成转写 |
| --- | --- | --- |
| current_full_draft | 查询工具后说明不知道停在哪，询问是否从头重读 | 未查询工具，从1读到50 |
| draft_removed | 查询工具后明确从头读1–50 | 未查询工具，从3读到50 |
| task_snapshot_unknown | 查询工具后明确从头读1–50 | 未查询工具，从31读到50 |
| synthetic_oracle | 未查询工具，连续9–50 | 未查询工具，连续9–50 |

8条协议均completed。独立复核逐项核对oracle两条转写均有连续42项9–50，没有遗漏或越过50；没有听取模型音频，不能进一步声称实际发音或声学输出也通过。

前三组第一轮都主动查询，第二轮都没有查询。因此不能说基准模型完全看不到原任务，也不能单独把某次差异归为自动注入目标的效果：工具选择及额外续答本身是干预后的中介变化。

未知位置时询问或明确从头读，是当前提示词允许的回退；它们不满足用户所要的无缝准确接续，但不能与oracle组共用一个“准确率”分母当作全部错误。没有起点证据却声称接着从3/31开始，则仍是无依据的恢复选择。

## 可以与不可以推出的结论

- 明确给定断点后两次生成9–50，反驳“模型绝对无法继续数”的说法；样本太少，不能宣称稳定成功率或排除偶发漏数。
- 删除生成稿本身没有产生准确断点；保留全文的oracle仍成功，不能说“只要有全文稿就必然失败”，也不能认定删稿即修复。
- 自动提供原任务但仍没有文字位置，没有补出下一数字；任务目标与已播进度是不同信息。
- oracle同时改变状态标签、提供文字前缀与数字断点，只支持这个冗余信息包在本样本有效，不能判断哪个单字段起作用。
- 当前历史说明仍无条件写“准确的暂停字词位置未知”，与oracle数据存在契约冲突；实验成功不能替代一份清晰的生产恢复契约。
- 这是新会话中的合成历史重放，不能证明真实插话、迟到final、二次任务绑定、已播字词映射或任何产品修复通过。

## 来源与复现

真实执行的私有脚本SHA256：`7cb1889530e5150f8cf4ac1461fe3448aabfbc6fe5722a3d5ad73070166c90b6`；plan：`285d1cf1dfb90b853b44733fbd5d146f5717f46e6ba62f2d50b6e2a12656c1a8`；continue.wav：`e76609689529279d6876ed5121570a0b704ae38bc3b18cc9a13fd6aef03f9a52`。原始8份结果、summary、provenance位于`artifacts/resume-root-cause-20261023/native/`。凭据仅在服务器root进程内存读取，结果扫描未发现实际密钥/token/OTA secret。

归档的[portable runner](../../tools/voice-audit/native_resume_ablation.py)增加路径参数和失败退出码；它没有被倒替成8次实连的执行脚本。该副本只另用回环fake provider核验成功退出0、拒绝退出1、四组输出及不泄露占位key，不新增计费模型调用。synthetic plan位于[固定fixture](../../tools/voice-audit/fixtures/resume-context-20261023.json)。
