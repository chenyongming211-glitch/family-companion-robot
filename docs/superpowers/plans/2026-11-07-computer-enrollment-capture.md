# 电脑登记候选采集实施计划

> **For agentic workers:** 使用superpowers:subagent-driven-development完成同一小客户端／接收器的实现、规格和质量复核。直接main，不建分支或worktree。用户已要求电脑替代本轮手机采集，按本轮条件记账，无需重复请求许可。

**Goal:** 用户手动通过Mac麦克风采4秒真实人声，原件及规范WAV只保存在私有云端，为电脑登记条件准备候选素材。

**Architecture:** 无本地服务的AppKit客户端→既有SSH→独立Python receiver→原始f32le及云端固定ffmpeg转换WAV。不调用声纹模型，不修改正式服务。

**Tech Stack:** macOS Swift／AppKit／AVFoundation、云端Python标准库及现有ffmpeg，复用robot-audio-capture的安全输出帮助类。

## Task 1: 实现及工程验证

**Files:** 新增tools/computer-voice-capture/中的main.swift、NativeCapture.swift、Transport.swift、State.swift、Tests、Info.plist、build.command、open.command、README.md；新增tools/robot-audio-capture/receive_computer.py及test_receive_computer.py。已有采集器和普通客户端保持原功能。

- [ ] 云端新隔离目录先写receiver严格协议与权限／格式／声道／失败测试，运行RED，再实现GREEN。接收头／payload边界、输出字段和限制遵照设计，不保存任何真实音频到Mac。
- [ ] 客户端先写无麦状态测试：只有手动动作且远端ready后才可申请权限和采集、忙／取消／代次、4秒格式与容量、stored匹配clip、SSH失败／EOF／超时及子进程回收。SDK测试取得RED再实现GREEN。
- [ ] 云端运行receiver全部专项及既有43项采集器回归；Mac构建／SDK测试／签名／脚本检查。源码摘要绑定；独立规格、质量复核通过再提交。

## Task 2: 一人现场及记账

- [ ] 保存正式保护状态，准备私有匿名M01采集目录和既有SSH配置；打开新窗口，验证未自动收音。由用户手动点首段并授权。
- [ ] 核对首段原生格式、原件与WAV摘要、云端保存及非静音；按界面不同提示逐段手动采集，质量不足不计完成。机器人登记素材与电脑素材分批。
- [ ] 原始及规范素材仅云端保存，声纹及两条件准确率暂不运行。记录实际数量／质量／仍缺的人和条件，更新交接及验证报告、推送main并核对工作区。
