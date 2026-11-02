# Realtime 声纹聊天测试客户端

独立原生窗口复用正常 Realtime 聊天、AEC、Opus 和播放回报，同时显示本次发言的匿名访客候选。打开窗口不会自动开麦，只有点击「开始对话」才请求麦克风并连接；服务端 `ready` 的试点能力和开关状态通过核验后才启动收音。

请轮流说话；重叠发言尚未验证。访客 A～H 只是本会话的临时候选，不代表已登记身份，也不会向模型注入姓名。音频、临时特征和聊天上下文只在内存，不保存声纹库；候选结果不写日志。结束或重新开始会清空候选并重新建立会话。

开始前可勾选「本会话开启声纹观测」，连接和聊天期间开关禁用，结束后可切换再开始。开启连接云端回环 `18052`，关闭连接独立的 `18053`；两者都须为同一隔离试点的仅内存模式，使用不同 `speaker-observation-on` / `speaker-observation-off` 设备身份配置。普通产品入口不参与这项对照。

服务端须先完成两个隔离入口部署。使用已获授权且已在本机存在的 SSH 私钥及已信任主机记录，通过环境变量运行：

```bash
SPEAKER_SSH_TARGET='用户@已配置的云端主机' \
SPEAKER_SSH_KEY='/现有SSH私钥的绝对路径' \
  tools/realtime-speaker-test/open.command
```

脚本构建独立 `.superpowers/realtime-speaker-live/OutputLive.app`，bundle ID 为 `com.familycompanion.speakerlive`。它用独立 `ssh -F /dev/null -N -T` 子进程仅转发两个回环端口，先核验两端的 `speaker_test`、`speaker_enabled`、就绪、内存及诊断状态，再打开窗口。关闭窗口或中断脚本会回收其自己的 App 与 SSH 子进程，不修改全局 SSH/Git 配置，不取消其他转发。脚本清除音频 trace/correlation 环境变量，并丢弃 App stdout/stderr；不要另加正文、录音或候选结果重定向。

不启动麦克风的开发检查：

```bash
xcrun swiftc -parse-as-library \
  tools/realtime-output-live/SpeakerObservation.swift \
  tools/realtime-output-live/tests/SpeakerObservationTests.swift \
  -o /tmp/realtime-speaker-parser-tests
/tmp/realtime-speaker-parser-tests
OUTPUT_LIVE_SPEAKER_BUILD=1 tools/realtime-output-live/build.command
bash -n tools/realtime-speaker-test/open.command
```

SDK 构建和解析测试只证明客户端接线；真人听感、多人轮流标签一致性和重叠能力需要单独现场验证。

试点边界：每个完整发言片段需1.5～8秒，短句、长句或坐标不可靠时显示不确定；这不妨碍普通回答。识别最多运行5分钟／32片段，达到后显示不可用、聊天继续；重新开始会话会清空访客标签。整场测试连接最长15分钟。现阶段只在话尾观测，未实现连续流中的可靠换人／重叠检测，也未把候选加入模型上下文。

云端命令为`cmd/realtime-speaker-test`，两入口使用相同模型API配置、相同资源限制与独立的新设备配置；原产品不切换。部署和回归依据见[验证报告](../../docs/verification/2026-11-03-realtime-speaker-observation.md)。
