# MacBook 实时语音测试客户端

> 使用 writing-plans、executing-plans 与 TDD；用户已授权继续这一明确阶段，直接main、当前会话执行，不重复请求计划批准。运行时/自动化测试在服务器；Mac只运行浏览器客户端与SSH转发。

**Goal:** Mac真实麦克风以16k mono s16发送到既有AgentSession，24k输出在浏览器播放；本地立即取消、过滤旧代次并报告浏览器输出时钟估算。物理听感与停声须用户参与确认。

**Architecture:** 服务器18010同时提供静态页面与原PCM协议。浏览器不能设置WS Authorization header，故新增SSH授权领取的120秒一次性配对票据，换900秒HttpOnly/SameSite cookie，严格检查同源与loopback Host；既有Bearer入口不变。最初300秒测试窗口在实际联调中修正为900秒，页面显示时间/轮数，仍最多8轮。模型Key和长期测试令牌始终留服务器。Mac仅做SSH转发；不部署本地HTTP/语音服务。

**Alternatives:** 原生Swift客户端需本机编译/签名和单独音频适配；Python客户端需本机音频依赖。选服务器托管的Chrome页面，可逐层显示权限、输入电平、转写、回答与播放状态，无Mac运行库安装。

**Boundaries:** 首版显式“开始说话/结束发送/立即停播”；先用耳机。自动端点和自由声学插话、任务接续与手表不加入本轮。默认不持久化正文/录音，不自动发起或重试模型请求。页面明确浏览器输出时钟是估算，不能证明物理扬声器停声或字词断点。

## 文件与接口

- `family_voice/browser.py`：静态文件与一次性票据/cookie，同源/过期/重放/限额检查；`install_browser(app,cfg)`返回WS鉴权入口。
- `family_voice/pair.py`：在服务器读取专用客户端令牌，调用本机票据接口，只输出短期配对URL；不输出Key/长期令牌。
- `family_voice/server.py`：增加浏览器入口复用同一个session_factory/单会话租约，不复制语音编排。
- `web/audio-core.mjs`：有状态重采样/PCM分帧、输出分段账本与保守采样前缀。
- `web/input-worklet.mjs`：收音、计算电平、结束屏障与取消丢弃；不保存音频。
- `web/client.mjs`、`index.html`、`client.css`：配对、连接、收音、WebAudio播放、立即停止、错误和用户验收项；断线/页面隐藏结束所有音频。
- `tools/mac-voice/open.command`：SSH转发、领取临时票据并打开浏览器，终端退出清理本次隧道；无本地服务/Key文件。
- Python/Node测试：服务器执行协议鉴权、重放/过期/跨源、分帧连续性、播放前缀与旧代次测试。

## 执行与验收

- [x] 先写并在服务器运行失败测试：`/client`存在但无凭据、一次性票据、错误Origin/Host拒绝、过期cookie、浏览器WS与Bearer共享单会话。
- [x] 实现最小鉴权/静态入口，重跑新增Python测试及既有21+3项回归。
- [x] 先写并在服务器运行Node失败测试：44.1k/48k分块重采样无丢/重复，结束/取消屏障，播放间隙不计为样本，旧代次与非法PCM拒绝。
- [x] 实现纯音频核心/Worklet与页面；服务器Node测试通过。队列有界，断线/后台释放mic，取消先停本地再发控制，输出无时钟时unknown。
- [ ] 部署独立开发服务，Mac建立SSH隧道，在Chrome实际检查页面、麦克风权限/输入电平、播放与停止操作。只对用户真实参与的部分给出通过结论。
- [x] 更新README/交接/验证报告并提交推送main；服务器同步同一源码，旧8000健康；保留用户未参与/浏览器权限未授予等未验收项。

服务器命令：`PYTHONPATH=services/livekit-agent .superpowers/livekit-chain/venv/bin/python -m unittest discover -s services/livekit-agent/tests -v`；`node --test services/livekit-agent/tests/*.test.mjs`；原探针单独运行。源码编写和Git在Mac，浏览器实测属于已授权的Mac终端阶段。

已完成25项Python、11项Node与原3项SDK探针；用户确认麦克风电平/提示音，实际问答已返回并播放。数数实测暴露队列计数和12秒突发容量问题，修复与三条队列回归通过；独立复核的并发连接和晚到权限问题也已加生命周期代次处理。长回复/停播/后续新问答的用户复验进行中，结果见[验证报告](../../verification/2026-10-24-mac-voice-client.md)。
