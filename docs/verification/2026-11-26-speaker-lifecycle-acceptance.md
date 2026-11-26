# 声纹 worker 生命周期改造：云端验收清单（2026-11-26）

本轮改造把 helper 从「每句 fork 一个推理 worker」改成「会话内常驻复用」，并修掉三处会让声纹过滤整场失效的缺陷。Mac 上只能编译根 module、跑不了 `//go:build linux` 的测试与真实 helper，**下面所有结论必须在云端 Linux 上跑出来才算数**。

已推 main、尚未部署的提交：

| 提交 | 内容 |
| --- | --- |
| `bbb7e5a` | 一个会话复用一个 worker；去掉 32 句 / 5 分钟硬上限，改为跟随会话 |
| `fea11d2` | 空闲不再杀 worker；单句失败只降级；连续 3 次失败才退出；ready 前预热 |
| `d1a21ad` | 测试门禁改用注入 worker；预热失败不再终场 |
| `8e608dd` | observer 接受 `uncertain/worker_unavailable`（跨包协议漏项） |
| `fb3b286` | 未知 reason 只清空不终结会话；两侧共读协议表；预热失败不占三次额度 |

## 1. 前置条件

语音服务需带固定参数启动（路径与摘要按云端实际值）：

```bash
--speaker-binary /path/to/speaker-lab \
--speaker-model  /private/models/eres2net.onnx \
--speaker-sha256 <64 位十六进制>
```

**不要**带 `--speaker-lifetime` / `--speaker-max-clips`（默认 0 = 跟随会话）。带非 0 值会重新引入中途退出，测出来的结论不适用于默认部署。

进程树预期：`realtime-output-live` → `speaker-lab session … --realtime-test`（常驻）→ `speaker-lab worker --socket …`（常驻、复用）。

## 2. 分层模块：测试要跑两条命令

`tools/speaker-lab` 是独立的嵌套 module，从仓库根跑会报 `main module (familyrobot) does not contain package familyrobot/tools/speaker-lab`。必须拆开：

```bash
cd /path/to/repo && GOCACHE="$TMPDIR/gocache-dsh" go test ./internal/speakerlive/ -count=1
cd /path/to/repo/tools/speaker-lab && GOCACHE="$TMPDIR/gocache-dsh" go test ./... -count=1
```

只看这两条是否 PASS，先确认测试门禁本身是绿的，再做下面的实机验收。`TestSessionReusesOneWorkerForConsecutiveClips` 需要 `SPEAKER_LAB_TEST_BINARY`（已构建的 helper），不设即 skip，属预期。

## 3. 实机验收项

| 编号 | 目的 | 操作 | 判定 |
| --- | --- | --- | --- |
| A | 会话内只起一个推理 worker | 连续说 40 句，期间每 0.5s 采样 `pgrep -f 'speaker-lab worker'` | PID 全程不变、数量恒为 1；改造前每句都会换 PID |
| B | 单句延迟 | 用 `speaker_test`（MemoryOnly）模式让 `speaker_observation` 下发，采集 `elapsed_ms` 算 p50/p95 | 温态 p50 应在 140～200ms 量级；若仍是 0.4～0.6s，说明还在每句冷启动 |
| C | 挂死与重启 | `kill -STOP <worker pid>` 后说一句；再 `kill -CONT` | 该句在 15s 请求预算内出结果或被判 uncertain；helper 不退出、无孤儿进程 |
| E | helper 退出链路 | 会话建立后 `kill -9 $(pgrep -f 'speaker-lab session')` | 生产 `events.jsonl` 出现 `speaker_observer_unavailable`；`pgrep -af speaker-lab` 为空；普通对话继续 |
| F | 启动失败 reason | 用不存在的 binary 或非法 SHA 格式启动 | `events.jsonl` 记 `observer_start_failed`，会话 fail-open 继续；注意与「格式合法但摘要错」区分，后者走 helper 的 `startup_failed` |
| G | 空闲不杀 worker | 说一句 → 停 6 分钟 → 再说一句 | 两句都出标签，worker PID 不变 |
| H | 单句失败只降级 | 说一句期间 `kill -9 $(pgrep -f 'speaker-lab worker')` | 该句 `uncertain/worker_unavailable`，会话不退出；紧接着一句正常出标签（新 worker） |
| I | 连续三次失败退出 | **先建会话**，再 kill worker 并把 binary 改名，连续说 3 句 | 前 2 句 uncertain，第 3 句后 helper 退出并记 `helper_exited`；普通对话继续 |
| J | ready 预热延迟 | 冷启动后量 ready 到首句的时间 | ready 延迟远小于 observer 的 15s 启动预算；首句 `elapsed_ms` 不应含模型加载 |

I 的口径：预热失败不计入三次额度（`fb3b286`），所以无论 worker 坏在会话建立前还是建立后，都是**第 3 句合格语音后退出**，不用分两种时序触发。

## 4. 记录落盘的前提

`speaker_observer_unavailable` 只在生产（非 MemoryOnly）会话写 `events.jsonl`；`recorder.add` 在 MemoryOnly 时直接返回。`speaker_test` 模式只能从 wire 事件看到通用的 `observer_unavailable`，**看不到具体 reason**。要证明 reason 落地必须读生产会话记录。

## 5. 已知限制（不在本轮修）

- 预热失败在 app 侧完全不可观测：helper 的 stderr 被丢弃，会话结束前上层收不到信号，只能靠第 3 句后的 `helper_exited` 间接看到。
- `speakerFailed` 一旦置位不回落，同一 WebSocket 内不会自愈，靠重连换新会话恢复。
- Mac 上的 `/var` 符号链接、`libopus.so.0`、linux-only 包红灯不算缺陷，以云端为准。
