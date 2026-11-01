# 手动会话内说话人测试（Mac 客户端）

独立 AppKit 窗口；打开窗口不会连接 SSH、申请麦克风或收音。每次点击“采集 4 秒”，首句先连接独立云端 `speaker-lab session`，收到 `ready` 后才申请麦克风权限并采集；后续每句也必须手动点击。采集完成立即停止麦克风，串行发送、等待结果，不自动连续采集，不播放声音。

参与者须自愿，每段仅一人说话。当前只做单人多句一致性检查；`访客 A/B` 只是本次会话内的匿名标签，没有姓名输入、登记、人物库或历史回查。短于1.5秒、低能量片段为“不确定”，质量门禁不证明语音／单人／无重叠。研究阈值来自 ERes2Net 合成基准，未经家庭真人校准，不用于身份认证。

## 构建和启动

Mac 需要现有 Xcode Command Line Tools（AppKit、AVFoundation SDK）；不安装后端运行依赖、不下载模型、不启动本地语音服务。

```sh
cd /your/path/family-companion-robot
./tools/speaker-session/build.command

SPEAKER_SSH_TARGET='user@your-cloud-host' \
SPEAKER_SSH_KEY='/your/private/path/client-key' \
SPEAKER_LAB_BINARY='/your/cloud/private/evidence/speaker-lab' \
SPEAKER_MODEL='/your/cloud/private/models/eres2net.onnx' \
SPEAKER_MODEL_SHA='<64位小写模型SHA256>' \
./tools/speaker-session/open.command
```

替换所有占位符。SSH 使用已有授权私钥、BatchMode、独立参数数组和逐项 shell quoting 的远端命令，不复制云端服务凭据；不继承 SSH 配置中的转发。主机必须已加入本机信任的 `known_hosts`，本工具不自动放宽主机密钥验证。配置仅来自本次启动环境，不写配置文件。输出为忽略目录 `.superpowers/speaker-session/SpeakerSession.app`，使用不同 bundle ID 和 ad-hoc 签名；`open.command` 直接启动已构建 App 可执行文件以保留本次环境。

点击“结束会话”或关闭窗口会停止麦克风、移除 tap、清除客户端缓冲并关闭 SSH 输入／终止 SSH；云端随 EOF／取消关闭临时 Tracker。待授权、ready、音频等迟到回调受会话代次保护，不能重开麦克风。再点采集会产生新随机 session 和重新编号。每句最多64000个16kHz mono s16le样本，每会话最多32句／5分钟（包含空闲），超时或协议错误关闭会话。

应用不把 PCM、向量、标签结果或原文写文件、日志、数据库；stderr 丢弃。云端父进程只保留短期 Tracker，每个合格片段独立冷启动 worker（10秒上限，实际请求2秒上限），因此本工具等待时间不是产品延迟指标。客户端 core limit 尝试设为0，云端强制为0；“仅应用内存”不证明所有系统／内核副本或既有 swap 的状态，不更改系统 swap。

音频使用真实输入格式，经 `AVAudioConverter.channelMap = [0]` 显式选通道0再转16k mono Int16，复用现有 `tools/native-voice/Sources/MicrophonePCM.swift`；没有完整语音管线、Opus或播放依赖。

## 验证边界

`build.command` 只编译／签名；构建通过不代表麦克风、实际输入质量或多人区分通过。人工确认窗口行为与单人多句结果后，才能记录对应现场事实；多人／重叠和真实家庭校准仍待单独授权与验收。切勿把真实 session stdout 重定向到文件。
