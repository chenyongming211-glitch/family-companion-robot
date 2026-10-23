# Wake Source Recovery Implementation Plan

> **For agentic workers:** Use test-driven development and independent review; execute in this session without another approval question because the user explicitly authorized continuation. Firmware and gateway may be handled independently.

**Goal:** 修复已复现的问候占任务槽与固件唤醒发送失败恢复，完成无人值守验证及候选构建。

**Architecture:** 首个start前缓存以manual分段→显式commit绑定wake来源；ACK与start到齐后创建问候，在response.created后恢复实时模式和业务PCM。来源跟随输入身份进入播放记录。固件发送失败统一结束本次唤醒并恢复idle。

**Tech Stack:** Go/WebSocket/Opus、千问原生Realtime、C++/ESP-IDF6.1、Python主机测试、GitHub Actions。

## Global Constraints

- main直接开发、提交和推送，不创建分支或worktree。
- 不刷机、不播放声音、不启用现场录音；不要求用户现场操作。
- 不加入辅助ASR，不更换服务端框架，不调整正式提示词。
- 密钥仅在受保护服务器环境中读取；artifact与凭据不进Git。
- 生成、网络下发、模拟播放、I2S确认与逐字位置分别验收。

## Tasks

- [x] 网关RED：`internal/gateway/wake_origin_test.go`覆盖同音转写、迟到STT、start前缓存独立commit、ACK前后业务输入完整、无缓存detect、已在会话中detect、重连、超限和超时；观察当前版本失败。
- [x] 网关GREEN：修改`session.go`首次连接/PCM/控制分段，`wake_origin.go`收敛为有界状态机，`playback_progress.go`携带wake来源。事件顺序为manual mode→缓存append→commit→ACK及start→response create→created→VAD mode update→排队业务append；普通手动commit保留。
- [x] 固件RED/GREEN：新增真实函数主机回归及最小恢复patch；运行`python3 -m unittest discover -s firmware/tests -v`，重新prepare验证patch链。
- [x] 独立审查Go来源/生命周期、固件恢复/配置；修复重要发现后复跑相关回归。
- [x] macOS及Linux `go test -race -count=1 ./...`、`go vet ./...`和Go构建；真实千问隔离probe核对问候/原请求/连续插话，不能把无法数够5秒的样本算成插话成功。
- [x] 提交推送main触发两款固件CI，核对exact-head和产物provenance；不刷机。检查正式服务不被测试影响。
- [x] 在前两项稳定后进行文字位置能力/契约验证，记录仍未知部分及现场验收项目，更新报告。该项是能力核对，不代表精确接续修复。

## Current evidence

网关d68ec44已通过精确提交Linux验证及真实千问隔离复验，09:31空闲时上线；固件1fba13b双配置CI与离线镜像/provenance核对通过，尚未刷机。参见[实施结果](../../verification/2026-10-23-wake-recovery-results.md)。准确继续位置保持打开。
