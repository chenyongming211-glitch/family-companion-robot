# Realtime 历史版本对照执行计划

> **执行：** 使用superpowers:executing-plans在当前会话内逐项执行。用户已批准独立旧版/当前版对照，不再请求重复执行确认；遵守仓库main工作流，不创建分支或worktree。

**Goal:** 在不替换当前产品的前提下，准备可由同一Mac音频客户端交替测试的两个历史源码实例，定位重复回答/误触发插话是否随服务版本变化。

**Architecture:** 云端使用Git归档快照构建，旧版为c9050e0bb040cb67ae6aecd11fa0f1aca6acd109，当前功能版为09d22e67f990d24eaffa1a270ee14455d2551ff3。仅在私有快照中修改启动命令的固定回环地址及对应启动日志，分别监听18050、18051；语音实现保持历史原样。两实例使用独立文字记录和设备登记表，复用云端受保护配置及只读产品身份；不启用小智设备监听。Mac复用同一已构建Swift客户端，以独立profile和明确窗口标识交替连接。

**Tech Stack:** Go1.27.3、libopus、systemd临时服务、现有Swift/AppKit/CoreAudio客户端、SSH回环转发。

## 对照边界

- 用户批准的是独立对照，不是正式回退；不改正式二进制、unit、凭据、设备登记、模型设置、VAD或固件，不开串口。
- 当前程序固定绑定18040，不能直接传入新端口。对两份归档的cmd/realtime-output-live/main.go只作回环端口和启动日志替换，保存原件与差异；所有其他归档源码逐文件校验。
- 两实例读取同一配置/身份，搜索同为开启、最大单实例一会话。当前版对照使用--memory=false，不加载正式历史；旧版没有记忆功能。正式18040继续原配置。
- 旧版不是“已知正常”结论，只是第一个待验证基线。两者均异常时向更早入口/客户端/声学条件继续缩小，不能称已找到首次异常提交。
- 保持Mac内置扬声器、音量、位置、距离和测试语句不变；不同时开始两个窗口。相同模型ID不保证供应商过去和今天实际实现完全相同。
- 原音频、设备凭据、构建产物和详细通话内容仅在私有路径，不入Git。启动/健康检查不调用真实模型；真人开始后才建立模型会话。

## Task 1：封存基线并准备快照

- [x] 核对main工作区，记录正式health、PID/NRestarts及6个受保护文件和正式二进制SHA256。
- [x] 用git archive分别导出指定提交，传到云端独立私有目录，核对压缩包SHA256与解包文件清单。
- [x] 仅替换两份启动命令的固定回环地址18040→18050/18051及对应日志；保存原件、补丁和完整源码校验结果。

## Task 2：云端验证与启动

- [x] 两份快照均执行：`go test -race -json ./internal/realtimelive ./internal/realtimestate ./internal/realtimeaudio -count=1`，保存日志并统计失败/跳过。
- [x] 旧版执行：`go build -ldflags '-X main.build=c9050e0-compare' -o /tmp/realtime-version-compare-20261030-7Nr4MR/realtime-old ./cmd/realtime-output-live`；当前版执行：`go build -ldflags '-X main.build=09d22e6-compare' -o /tmp/realtime-version-compare-20261030-7Nr4MR/realtime-current ./cmd/realtime-output-live`；核对产物哈希。
- [x] 以和当前正式服务相同的root账户在回环18050/18051启动独立临时systemd实例，最长运行2小时、不自动重启；启用ProtectSystem=strict、NoNewPrivileges，仅新记录目录可写。当前版增加--memory=false --max-sessions=1；均不启用device-listen。首次family-robot账户启动因无权读取root0600配置失败，已按证据改为相同账户，不放宽原文件权限、不复制凭据到Mac。
- [x] 健康检查确认旧/新build、ready、空闲与单实例限额；确认只有回环监听。注册/换票和Origin拒绝检查不升级WebSocket、不调用模型。
- [x] 再核对正式PID、NRestarts、二进制及受保护文件未改变。

## Task 3：Mac测试交接

- [x] 两端使用相同Swift音频/协议实现；窗口明显标注旧版/当前版及对应提交，不更改AEC或播放语义。测试profile独立于日常Mac身份。
- [x] 仅建立SSH回环转发，凭据仍在云端；打开两个待开始的窗口，不自动收音或播放。
- [x] 完成旧版与当前版首次问候对照，均正常。未形成版本差异后，按单变量原则改测记忆开关和原客户端入口，未继续数学/故事场景；这些保留为后续范围，不标为已验收。
- [x] 保存会话归属、实播及事件证据；本轮未复现，不声称故障修复或找到异常提交。
- [x] 测试结束后停止本次临时实例、窗口及SSH转发，保留私有文字证明，恢复原Mac正式入口和身份。
- [x] 更新验证与开发交接记录，main提交推送。

## 单变量补充对照结果

- [x] 当前版隔离实例仅将memory由false改true；首次空历史与恢复1/2轮历史均完整播放，用户反馈连续正常。
- [x] 原Mac客户端改接同一隔离实例，沿用相同测试身份与历史；用户连续两次正常，后台完整played。
- [x] 原Mac客户端重开并回到正式入口和原身份；恢复23/24轮原历史仍正常，未清空原记忆。

结果与限制见[验证记录](../../verification/2026-10-30-realtime-version-comparison.md)。只完成本轮问候/重连对照；不外推故事插话、长稳或ESP32已通过。
