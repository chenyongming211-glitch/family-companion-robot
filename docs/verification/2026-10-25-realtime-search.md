# 18032内置联网搜索验证

日期：2026-10-25，Asia/Shanghai。用户已明确要求开启联网搜索，并了解其他配置项。范围仅18032，保持小满身份、Tina、流式音频、自然反问、排空与已有服务器边界；不启动Go迁移、不改原四个入口。

## 代码

- 18032专用`upstream.settings()`默认启用搜索，服务器环境`OUTPUT_LIVE_ENABLE_SEARCH=0`可关闭；不改/复制保护凭据文件。
- 生成连接在首次session.update发送`enable_search=true`和`search_options.enable_source=true`，核对服务端确认；VAD/转写监听连接用独立配置副本明确false，仍不自动生成。
- 底层探针Native只有显式传入配置时才改变搜索参数，旧探针默认不启用。配置同时含搜索和工具时，在连接前拒绝不兼容配置；这不是对用户回答内容的拦截。
- 小满的`live_information`随开关与连接状态变化。联网问题有当前北京时间，提示区分搜索结果、天气预报和实况；没有新增固定准备语或回答审查。
- 读取真实`response.done.usage.plugins.search`的计量和`response.search_info.search_results`来源，白名单保留计数、策略、标题、HTTP(S)链接与索引，最多32条；缺失或异常可选元数据不会阻断音频。普通日志不打印正文/链接，私有intent及`native_search`事件保存证据。

## 回归

云端目录`/opt/family-robot-livekit-validation/native-search-20261025-{red,green}`。新增9项搜索合同测试；旧版原8项中7项按预期失败，旧探针默认关闭通过。随后先复现来源丢失、可选搜索元数据为空会打断流，再修复。

当前实验49项＋旧输出probe 8项，共57项通过。覆盖参数上送、来源请求、监听关闭、配置不互相修改、服务端确认、搜索/工具互斥、计量/来源审计、异常可选元数据不中断、能力开关联动和环境回退。既有身份/流式/已播历史/排空回归保持。

```sh
/opt/family-robot-livekit-runtime/venv/bin/python -m unittest discover -s tools/realtime-output-live -v
/opt/family-robot-livekit-runtime/venv/bin/python -m unittest discover -s tools/realtime-output-probe -v
```

## 真实模型

合成语音问北京现在天气及来源，再问2＋3；每项新建原生连接。没有启用真人麦克风，PCM仅在内存，所有调用intent与结果在服务器`/var/lib/family-robot-realtime-test/verification/`私有目录。

| 私人目录 | 服务端确认 | 天气查询证据 | 首PCM |
|---|---|---|---|
| `native-search-20261025T121401` | enabled=true，source_requested=true | usage.plugins.search.count=1，strategy=agent；返回7条来源，包括中国天气网/中央气象台 | 天气3537ms，数学1054ms |
| `native-search-20261025T121700` | 同上 | count=1，strategy=agent；解析保留17条搜索来源 | 天气6915ms，数学912ms |

数学两次均回答5，响应没有搜索计量或来源字段，不能把缺失字段擅自填成一个已确认的0。监听连接单独实测`native-search-listener-20261025T121841`，ready=true、enabled=false、source_requested=false。

这些样本证明当前账号配置成功且实际发生过联网搜索；不是仅凭回答“来自气象台”认定查询成功。首PCM包含建连/上传，不是完整真人声学时延，也不是统计分位数。联网查询增加等待，不代表每次普通聊天都必须联网。

## 资料准确性限制

两次天气回复仍把温度范围说成“现在”，没有给出可独立核对的观测时间；结果包含预报页和实况页，未逐页核实数据。因此只确认联网和来源返回通过，不能宣称天气数值已验证为当前实况。已添加区分预报/实况的提示，但不以提示词代替天气专业接口或事实核对。

## 部署与终端

初建本报告时，云端回归及真实生成/监听配置验证完成，尚未切换18032；部署及CoreAudio证据执行后补录。

完整开关/参数与模型版本限制见[配置清单](2026-10-25-realtime-config-switches.md)。
