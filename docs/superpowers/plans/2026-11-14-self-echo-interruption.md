# 开头回声误打断修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. User requires main directly, no worktree or branch. All runtime checks/builds/deployment run on cloud.

**Goal:** 防止当前设备回复开头的极短疑似回声打断自己并启动重复回复，同时保留不同内容的真人打断。

**Architecture:** 在现有Session输入准入流程内使用当前输出生成稿作为疑似回声参考，绑定输入开始时的输出对象。仅可信且新鲜的递增硬件播放回执启用防护；判断与原输入/输出生命周期共用Session.mu，不改播放结算、记忆证据或协议。

**Tech Stack:** 现有Go、WebSocket Session测试夹具、云端Go1.27.3。无新依赖、服务、模型请求或固件代码。

---

## Task 1: Session准入回归测试与最小实现

**Files:**
- Create `internal/realtimelive/session_echo.go`: 有界规范化、当前设备/输出/播放进度验证和疑似回声判断。
- Create `internal/realtimelive/session_echo_test.go`: 真实listener准入链路和播放回执验证。
- Modify `internal/realtimelive/session_input.go`: 输入绑定输出、预览暂缓、最终拒绝。
- Modify `internal/realtimelive/session.go`: 每个输出记录最近真实播放进度时间，仅在有效回执推进Completed后刷新。

- [x] 在现有newAdmissionRig基础上先添加最小故障回归。使用合成输入而非用户录音，真实receiveReceipt更新播放账本；设置已鉴权所有者和当前生成稿，验证匹配预览不会Cut、最终不会产生第二轮：

```go
func TestInputEchoOpeningDoesNotInterruptOrGenerate(t *testing.T) {
    r := newEchoAdmissionRig(t)
    old := echoOutput(t, r, "好的，我们从前有一颗星星讲起。", 19)
    if err := r.s.receiveReceipt(echoReceipt(old, 12960)); err != nil {
        t.Fatal(err)
    }
    r.start(t, "opening")
    r.preview(t, "opening", "text", "好")
    r.stop(t, "opening")
    r.final(t, "opening", "好的")
    assertEchoOutputAlive(t, r, old)
}
```

该故障入口及其辅助函数完整保存在`internal/realtimelive/session_echo_test.go`：echoOutput通过begin及19次真实binary建立27360样本账本，echoReceipt构造真实i2s_dma/valid/played_samples/state回执；assertEchoOutputAlive检查原输出、history及主生成/表情选择调用数。完整回归另验证PCM清除、原音频继续及重复final无效。

- [x] 云端先运行新增故障回归，确认失败原因为原回复被打断或新回复被启动；保存RED日志。Mac不执行Go测试。
- [x] 添加每输出`lastPlaybackProgress time.Time`，在receiveReceipt原有校验成功且`Completed`严格递增时写入time.Now；相同计数、拒绝的回执都不得刷新。
- [x] 输入开始时绑定当时的输出对象。辅助判断必须同时满足：当前鉴权xiaozhi设备、相同当前输出、非media/非Cut/非PlaybackFinal、Completed为正且≤36000、PlaybackBasis为i2s_dma、最近递增进度≤500ms。
- [x] 规范化仅忽略空白/标点并小写化，输入1至8字符，逐字匹配生成稿开头。不使用关键词黑名单或模糊语义。预览若text和stash并存必须考虑全部内容，避免不同内容被其中一个匹配字段掩盖。
- [x] 满足条件的预览保持原输出，不设置已确认打断标志；后续出现不同内容走原有interruptInput。最终还需0<len(p.pcm)≤32000，同一输出及新鲜进度仍有效时丢弃输入PCM并记录无正文的input_rejected原因，不activateTurn、不生成、不执行工具/记忆。
- [x] 补齐后运行普通/race：不同内容和“停止”正常打断；最终单独到达；空白/标点/英文大小写；8字符/1秒/1.5秒边界；9字符/超时/停止后/无依据/无稿/音乐/未鉴权/Mac恢复原行为；重复回执不延长时限；新输入、输出替换、迟到事件和实体abort隔离；回声路径不执行工具或记忆、不改播放结算。
- [x] 以实际运行来源为底，只叠这四个文件的精确差异；测试文件复用现有夹具且不引入未部署speaker/MemoryOnly代码。比较完整来源清单，确认其他文件零变化。两套来源运行相关完整普通/race、vet和build，检查零失败和非预期skip。
- [x] 规格审查与质量审查按顺序进行，修复所有重要问题并重新验证；main提交推送。

## Task 2: Voice独立发布与现场验收

**Cloud paths:** 新任务`/home/ubuntu/self-echo-20261114`；原运行来源`/home/ubuntu/personal-music-20261113/voice-runtime-source`只读。

- [x] 发布前核对当前Voice二进制、PID、启动参数/稳定环境与完整来源；确认manager/Caddy/配置/设备/家庭资料/曲库/旧审计事务身份。保护快照仅留云端私有目录，不复制凭据至Mac。
- [x] 使用当前实际来源候选编译产物，新增单一Voice ExecStart覆盖；先核对systemd有效配置确实选择该产物，且参数和写权限未改变，再确认0活动会话并单次重启Voice。保留旧产物及所有失败/事务记录；不恢复旧数据、不重设历史事务基线。
- [x] 发布后核对新PID/二进制摘要/health build、ready/memory_ready，manager PID和Caddy保持；核对资料/权限及既有记忆启动Chmod仅ctime变化的明确边界。任何失败如实记录，不自动重试发布。
- [ ] 用户手动发起讲故事：静听完整回复是否仍自我打断；随后测试不同内容的真人插话。未收到反馈前明确标注“云端验证通过，实机待验”，不自动开麦。
- [x] 更新`docs/verification/2026-11-14-self-echo-interruption.md`、本计划和`docs/DEVELOPMENT_HANDOFF.md`，记录测试计数、来源/产物/部署证明及现场范围。固件保持2.5.4、3534368B，不把AEC残余具体原因或启动watchdog标为已解决。

执行记录：实现来源fddfc11。云端主分支759/实际来源734项普通与race零失败/跳过，vet/build及独立规格/质量审查通过；实际Voice已单次重启为PID732503/build self-echo-20261114-fddfc11，管理/Caddy/固件保持。首次重启前保护拒绝与同一发布续执行的seq原SHA证明均保留，详细记录见验证文档。现场反馈仍待用户返回。
