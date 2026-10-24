# Final whole-change review brief

Review the full recorded range from preservation baseline9cbe153 (pre-barge-in tag) to the supplied final HEAD/diff. This is broad integration review after Task1+Task2 source reviews, not physical acceptance. Work from /Users/chen/code/家庭陪伴机器人. Main-only read-only checkout, no new branches/worktrees or file/index/HEAD changes. User rests; no user-facing chatter/questions, no serial/device/flash/reset/live capture or production server actions.

## Approved product goal

Existing M5StopWatch v2.5.2 and minimal Go gateway for native Qwen realtime speech. During active conversation, direct speech interrupts without a new wake word; standby wake remains. Approved experiment: real completed-DMA software reference→joint resampled MR→ESP-SR FD AEC, observe/measure before explicit realtime on. Marked interrupted stop clears old playback while preserving live realtime mic/reference. Ordinary auto conversation remains protected. Existing volume/brightness,2minidle, wake greeting preserved. No camera, family business, localVADabort or gain/power workaround.

Read docs/superpowers/specs/2026-10-21-natural-barge-in-design.md and docs/superpowers/plans/2026-10-22-software-aec-integration.md. Prep/buffer/Go small plans linked under plans/. Source authority locked patch/overlays, not generated tree. UpstreamMIT attribution preserved. Device+gateway unchanged tonight; Go optionalinterruptedreason not yet deployed.

## Global constraints (verbatim from implementation plan)

- main 直接开发、提交、推送；不建分支或 worktree。
- 用户休息期间不发进度或提问，不打开串口、不刷机/重启设备、不部署/重启线上网关、不采集现场音频。
- 现有物理单麦克风、原引脚、24kHz I2S 和普通 m5stack-stopwatch 构建保持；新增唯一实验名称 m5stack-stopwatch-software-aec。
- 参考 PCM 固定 16000 个 int16 样本（32000 字节），对象不超过 32768 字节；调用者统一 ISR 安全锁。RX 队列另设固定容量，不允许无界分配/积压。
- 不能用 Write 调用时间、模型输出或网关时间替代实际播放时间。不能将播放中的参考缺失补零伪装成功；真正 DMA 零值可以作为静音参考。
- 停播不重置参考，覆盖 DMA/DAC 已提交尾音。硬件时钟/格式重启才换 generation，旧块不能混入。
- 先测 AEC，再启用播放时实时上行；不增加本地 VAD abort 决策。待机唤醒、音量/亮度工具、两分钟退出保留。
- 只有明确测试窗口可以采集原始 MR 与 AEC 对照，默认无音频记录；禁止日志泄漏 Wi-Fi/token/API Key。
- 构建/主机测试不代表声学通过；现场至少十轮插话、回声与实际扬声器停播验收沿用已批准设计。
- USB 连接且唤醒时刺耳声另行调查，不在没有根因证据时捎带修改增益、充电或功放行为。


## Review method

Read full provided diff package once, requirement and task reports; context lines are the changed source. Named cross-cutting risk checks in complete prepared source/locked SDK allowed when diff hunk ends mid-function or contract/lock/epoch integration needs it. Do not crawl unrelated repository code. Review actual code independently, reports are claims. Task reviews found/fixed4 lifecycle/clock/tail issues; inspect final behavior on merits, not just closure labels. No currently unhandled Minor list at dispatch.

Check real data provenance and complete callback/lifecycle ownership; ISR safety and bounded memory; reference integrity/common/relative drift and fixed mapping uncertainty; in-flight generation boundaries across raw/resampler/AFE/wake encode/directsend/ordinarycloudsend; observer/fallback/realtime transitions; stop/decoder physicaltail semantics; bounded private diagnostics and collector interoperability/gaps/loss; atomic sourceprep and locked builds of bothflagstates; evidence/rollback safety and honest readiness boundaries. Exact physical/acoustic acceptance is required later, not proven by tests.

Use /Users/chen/.agents/skills/requesting-code-review/code-reviewer.md output structure: Strengths; Critical/Important/Minor issues with file:line, consequence and fix; recommendations; clear source/software readiness verdict. Separate exact targetCI evidence from physical/production readiness. Read-only: no edits/commits, no rerun unchanged evidenced suites. Only a narrowly focused reproduction for a concrete doubt not answered by existing runs; report command/output and reason. Do not rerun whole/race/highcount suites. Save full report .superpowers/firmware-development-20261022/sdd/aec-final-review.md. Return concise status/paths/findings.

Reports: aec-integration-report.md, aec-integration-fix-report.md, aec-integration-fix-review.md; aec-delivery-report.md and taskreview supplied at dispatch. Controller CI/artifact record supplied at dispatch. Exact source after Task1fix .superpowers/firmware-development-20261022/prepared-aec-fix/ if unchanged; later target preparedtree follows manifest. Exact SDK excerpts research/ include idf61-i2s-common.c/.h/types.h and codec-data-i2s-1.6.2.c; actualbuild headers are final authority. Do not fabricate inability to see API if source provided; request controller focused material as needed.
