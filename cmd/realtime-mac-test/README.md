# 已有 Realtime 网关的隔离 Mac 入口

`go build ./cmd/realtime-mac-test`，运行依赖和测试在服务器执行。此命令直接复用`internal/gateway`、`internal/qwen`及现有小智音频协议，不包含LiveKit，也不接三段式服务。与8000服务独立实例、设备身份和会话；不设置RecordDirectory，不读取或持久保存正式记录。

`REALTIME_TEST_LISTEN`默认`127.0.0.1:18030`且只接受回环。模型配置从受保护环境读取`DASHSCOPE_API_KEY`、`QWEN_REALTIME_URL`、`QWEN_VOICE`、`QWEN_VAD`与可选`ROBOT_INSTRUCTIONS`，不读取原设备token或OTA密钥。网关内部凭据每次启动随机生成，不离开进程。

- `GET /healthz`：模型别名、音色、活动会话数和能力，默认无正文记录。
- `POST /v1/pair`：严格JSON `{"device_id":"native-realtime"}`，返回120秒单次票据，待用票据最多8个；活动会话上限1。
- `GET /xiaozhi/v1/`：Bearer票据、Device-Id=`native-realtime`、Protocol-Version=`1`；内部映射专用测试MAC。小智hello/listen/tts/stt和Opus协议沿用原网关。

拒绝带Origin的配对与WS请求，不开放OTA。票据只用于当前入口，不是模型Key或正式设备token。窗口结束会话便释放服务槽；IdleTimeout与SessionTimeout防护均为24小时，不设置原先15分钟测试自动关闭。

部署模板：[独立systemd服务](../../deploy/family-robot-realtime-test.service)。独立env由服务器管理员在服务器内提取上游配置，文件权限0600；不得将其拷贝到Mac或提交Git。`systemctl stop family-robot-realtime-test`可单独停用；不需要重启旧服务。

此实例保留原网关内存背景、任务处理及中断重连代码，因此不能把其续讲或等待表现全归因于模型。它不是未加应用逻辑的“纯模型”基准。
