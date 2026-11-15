# 微雪 A/B/C 三链路对比记录

日期：2026-11-15。用户明确要求将A、B、C一起对比，按[最小接入计划](../superpowers/plans/2026-11-15-abc-waveshare-comparison.md)执行。

**当前：三链路接入、云端检查及规格/质量复核已通过，第一段A现场窗口已于14:44:29单次开放，约14:53:29自动收尾。** 开启及首次后续核对均没有设备会话或本轮样本，B/C尚未开放；实时状态和结果以云端apply/restore及records为准。A/B上午各一条云端成功结果、C下午四轮设备结果为此前独立证据，不能算作本次三路同条件对比完成。

## 实际路线与共同条件

| 路线 | 语音理解与主回答 | 输出声音 | 当前实现 |
| --- | --- | --- | --- |
| A | qwen3.8-omni-flash-realtime，输入原音、只输出文字 | cosyvoice-v3-flash / longanyang | 回答和TTS最终元数据收齐后播放 |
| B | fun-asr-realtime→qwen-plus非思考 | 同A的TTS/音色 | 与A同策略 |
| C | qwen3.8-omni-flash-realtime直接语音 | 受保护原配置音色 | 公共逐轮原生语音流式 |

每次会话固定一路，开测前向用户说明标签，记录实际主模型、ASR、TTS、音色和模式；错误不会回退到其他路线。三边共用支持中的微雪、鉴权、Opus、输入监听、OutputASR、播放信用/I2S回执和中断排空，首轮统一关闭点歌/设备工具/表情/搜索/声纹及跨会话记忆。无新生产多后端框架或固件改动。

同一简洁短答约束、各会话独立的有界已播历史，未播生成全文不进入上下文。A/B先复用已验证批处理客户端，C为流式；首包数据反映本次具体实现，不能据此下最终流式架构排名。C音色与A/B不同，声音单列，三条各4答属于初筛，不能证明长期稳定率或P95。

## API费用口径

记录主模型分模态token、累计历史、ASR时长、TTS计费字符及失败/取消/未知；公共输入监听、OutputASR不可漏算。TTS分句可能对应多次请求，12个主回答不是12次API请求。表情/点歌/设备控制本轮关闭，其开启后的产品费用需追加，不能将本轮主链路小计当整机全部费用。

B在本次最小接入中保留公共输入监听，同时另调用Fun-ASR-Realtime取得主回答转写；两项必须分别记录，不能把重复识别费用隐去。后续若复用一份ASR，需要验证对应新实现，不能直接把节省假设计入本次结果。输入监听若没有返回独立usage则保持未知，OutputASR音频样本量也不冒充已核对的供应商账单。

2026-11-15复核北京区公开原价：Omni文本输入1.5、音频输入6、文本输出4.5、音频输出12元/百万Token；CosyVoice v3 Flash为1元/万计费字符；Plus非思考输入≤128K为0.8/2元每百万输入/输出Token；fun-asr-realtime及qwen3-asr-flash-realtime为0.00033元/秒。免费额度、优惠和实际账单另核对。成品按每天30分钟、约900分钟/月比较，短会话结果不直接外推月费排名。

来源：[Omni3.8](https://help.aliyun.com/zh/model-studio/qwen3-8-omni-flash-realtime)、[CosyVoice v3 Flash](https://help.aliyun.com/zh/model-studio/cosyvoice-v3-flash)、[百炼价格表](https://help.aliyun.com/zh/model-studio/model-pricing)。

## 开窗前保护准备

私有证据根`/home/ubuntu/abc-voice-compare-20261115/`。开窗前正式2进程、7份受保护配置与健康已冻结，A路由当时仅prepare、没有apply。复用固定SHA的原路由操作器，更新三路线health契约后每路18项合成保护检查通过；模型标签已在冻结来源/二进制时再次核对。准备阶段没有真实模型或设备调用。

历史账本协议9/主回答38保持；本次最多新增12主回答，每路线4，累计至50。旧工具默认48未改，历史失败/未知全部保留，新阶段上限不代表人民币扣费上限。接入、云端检查、实际A、B、C现场结果及结束恢复分别以新证据记录，未测不算通过。

## 接入实现与最终云端验证

新增一个默认不构建的现场资产，A/B复用原Client，C复用generateNative；公共Session/设备播放代码未改。唯一产品代码增量为Native完成记录补充数值白名单内的文本/音频/缓存token细项，原顶层计数保留。三个冻结来源：

| 来源 | SHA-256 |
| --- | --- |
| abc_comparison_test.go.txt | 10e7deb06a2f518715184256c77143e0ca675700d2cf6f19e1b39ffaacf23463 |
| internal/realtimelive/native.go | 494dcfdfd725c3130ab579fbbf3b84c803acde82818f5d11128a4d2aa34f0de7 |
| internal/realtimelive/native_usage_test.go | f35130abaa1815c1732d423d4b4a7abaaf726149119072085a8999c4f2cee99e |

云端Go1.27.3，五包voicecompare/realtimelive/realtimestate/realtimeaudio/devicetools普通与race各934 PASS、5 SKIP、0 FAIL，vet（含正式CLI）及test-c/正式CLI build均exit0。编译相关164份Go文件中161份仓库/ABC资产与Mac逐项匹配，另3份为既有固定TTS、规划并行和私有重放，go.mod/go.sum一致。检查期间冻结源码未变，无真实提供方、设备、服务或原账本调用。

覆盖精确gate/route/target/Opus、持久4/12/50、重复/破损/未知/取消、实际Client/Native对合成HTTP/WS服务的路由、API intent先于网络、A/B同TTS、失败不播放不降级、C输出超限、私有事件8MiB及fsync、cut历史不升级Generated、只读字词审计及计费字段。独立规格复核最初发现失败原因丢失，先以B HTTP503及结果验证错误复现RED，再补私有error{reason,message,truncated}（最多2048字符）；复核及质量最终PASS。

最终现场binary `abc-pilot.test` SHA29f224176fdd02cb88c8df7410a993cc508a699feff31935fbeb195ae12e39c0；未部署的正式CLI candidate SHA1ec99f35ed2fec8cbc0c34f42c3bc7c4ffc8f408c71f24fce5ed70eef008c493。私有根下final-tests.jsonl、final-race.jsonl、final-verification-summary.json、final-compilation-input-check.json及dev/保留完整证据，二进制和原始数据不入Git。

现场顺序A→B→C，每条重新核对source/binary/PID/health proof，单次apply并在第四答硬件结算后停止/restore。第一A窗口已绑定binary29f22417、来源/控制器SHA、PID1146868及exact health；启动/首次核对active_sessions=0，用户已收到四轮说明。未取得样本不能报告A通过；B/C未开始，不报告三链路已测完或费用排名。窗口过期后续测须先核对restore，再fresh prepare/proof/intent，不复用已消费A-v1。
