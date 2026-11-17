# C 链路故事早停的最小修正

用户要求直接最小化修复。代码 `ea7c3a3`，仅改既有讲述提示和 C 原生终态审计；没有新增判定模型、关键词意图层、自动续讲/重试，也未调整 token、音色、播放窗口、前端或固件。

## 依据与改动

11 月 16 日 23:05，真实 C 会话第 2 轮生成 37 字，故事仅到人物开场。下发 188,640 个 24kHz 样本、约 7.86 秒，设备 I2S 回执全部 played；该轮没有 interrupt、turn_error 或断线。上游 completed，因此服务端结束单轮生成，设备播完后回聆听。现有日志没有保存详细原因，不能推断模型内部为何提前结束，不能说是 token 上限或播放截断。

- `realtimestate.StorytellingGuidance` 仅新增一句：直接从故事正文开始，在同一回复推进到人物行动和结果，不能停在开场/背景。原约 120 字指导、长故事分段、续讲与普通问答规则保留，A/B/C 复用同一份指导。
- `realtimelive.Native.Stream` 在 response 身份匹配后，向原 `live_native_turn` intent 增加 `terminal_status`、`terminal_reason`、`terminal_details_type`、`output_text_chars`。状态/原因按协议标识白名单收取，未返回或无法识别时为 unknown/null；不落任意 provider error.message/body、额外嵌套对象或原始正文。
- 原 streaming、PCM/转写、成功/错误/取消、usage、播放回执语义不变。completed 仍仅表示上游本轮生成结束，不能用它证明故事或用户任务完成。[官方终态定义](https://help.aliyun.com/en/model-studio/server-events)

生产差异为 Native 约 29 行和一条提示；没有新增公开接口或依赖。职责仍是 realtimestate 持有指导、Native 持有受控供应方审计、Session/固件持有播放生命周期。

## 云端验证

| 检查 | 结果 |
| --- | --- |
| 新增真实隔离 WebSocket/intent 文件测试 | 12 个终态子例，覆盖 completed/failed/incomplete/cancelled、未知/缺失/恶意超长字段、错误身份、无音频、Unicode 字数与原状态/输出保持 |
| TDD | 新字段/提示要求先 RED，随后 GREEN；不是先写实现后补测试 |
| 公开来源两包完整回归 | 745 PASS、0 FAIL、2 个既有可选 SKIP |
| 公开来源专项 race / vet / Voice build | 14 PASS、无 race；vet/build 通过 |
| 实际部署来源两包回归 | realtimelive 602 PASS/2 既有 SKIP，realtimestate 12 PASS，无最终失败 |
| 实际来源专项 race / vet | 14 PASS、无 race，vet 通过 |
| 实际来源及产物 | 225 文件、仅两项原生产文件变化；1,406 依赖文件/232 包前后核对，两次构建产物逐字节相同 |

两项已有 SKIP 是未启用的 quiet-listener 60 秒/open-budget 可选检查，不计为长时间待机通过。首次实际 State 差分测试因冻结源缺少 Python alignment 基准模块失败；保留原日志，补齐五份与 Git `9447bee` 一致的原测试资产后，仅重跑受影响 State 包通过，未改产品逻辑。两份实际测试适配只改变 package/import 包装，测试函数与公开提交相同，另有七项字节等价检查。

实际来源从已发布的封存 Voice 原 218 文件复制，逐项匹配旧 pins；只应用本批两生产文件 patch，保留既有源码与 main 的差异，不整包提升未部署功能。私有 provider 测试 helper 不进入候选。候选 SHA `29016fe83f824e3c246014a67a77f488c1045496913af1490d2ce398312d7d76`，build `story-minimal-20261117-ea7c3a3`；构建证明 SHA `750f095bb90595d80cbda193c1170a40b17a68ccaede83b212414f72be4ea498`。

## 有界真实模型对照

一次 TTS 合成公开请求“讲个小故事吧”，三次原生调用共用同一 16k PCM、上下文、`qwen3.8-omni-flash-realtime` 和 Tina，仅替换讲述指导。输入 SHA `5a8c98de894aebf9d31fe9dff2fa3a0be4257e89d1d1cfa467345d51b9683bb6`。

| 方案 | 生成字数 / 音频 | 人工阅读结果 |
| --- | --- | --- |
| 旧提示一次 | 99 字 / 21.92 秒 | 狐狸从关注外表到雨夜帮助兔子，有行动与结果 |
| 新提示第一次 | 72 字 / 14.96 秒 | 刺猬分享苹果并获得满足，有行动与结果 |
| 新提示第二次 | 69 字 / 16.24 秒 | 蜗牛参加赛跑并超过打盹的兔子，推进过原开场 |

三次均 completed，详细原因均 null，数值 usage 已保留。人工按情节检查，未用字数阈值直接宣布完成。旧提示这次也成功，因此原现场故障没有复现，不能证明因果改善、成功率或根治；本批是最小提示缓解与诊断补齐。没有连接设备或播放音频，不能算微雪实机听感/连续讲述通过。总调用严格为一次输入合成和三次主生成，没有自动重跑；原音、生成音频/正文和凭据只留云端私有目录，不入 Git/Mac。

## 发布与现场边界

代码、实际来源/产物及 Voice-only 操作器均已独立审查通过。正式封存 pins SHA `ee0cbf100649cd5db7e82803c6cf04c9efc580bd8973f4bad8bc8eedcb9080dc`，正式只读预检通过；新事务首次执行成功，仅更换 Voice exe 和一个 ExecStart drop、reload/重启 Voice 各一次，没有重试、回退或恢复旧数据。

实际 Voice PID `1847403`，release `story-minimal-20261117-r1`，exe SHA 与候选 `29016fe8…` 匹配，build `story-minimal-20261117-ea7c3a3`，ready/memory_ready true、闲置，systemd NRestarts=0。管理 PID `1597057`、静态文件/会话、配置/绑定、聊天/记忆内容及权限、审计前缀、Caddy、其他进程/current、单元权限保持；三项 `.memory` 仅因既有启动 chmod 改变 ctime，其他字段严格保护。云盾规范 SSH 临时拒绝名单动态增删，静态防火墙政策和原安全进程身份保持；本任务不写防火墙。

发布后只读检查：真实授权 Unix `/voice-route/status` 200，已选 C/idle/activeRoute=null；两项管理 namespace 实际 `ro`；匿名 admin 401，公网 admin 及私有两路径 404。没有替用户切换链路、断开会话、调用实际设备模型或播放。备份结果 SHA `45d4b3d356fc974664ff511ed48166e9d8369dc5f4a5121d344913e442fe2527`，status=verified、manager_unchanged=true、data_restored=false；BACK 只有 baseline/intent/lock/restart-intent/result，无失败记录。

完整来源/日志/模型用量/发布证据在云端 `/home/ubuntu/story-minimal-20261117`；新封存目录和新备份事务独立于以前发布，禁止复用任何已消费 marker 或回写旧数据。

现场仍需验证微雪 C 的完整故事、声音、真实打断和再次续讲；旧回声/待机/连接故障不宣称修复。之前审计/防火墙历史差异仍独立未关闭。
