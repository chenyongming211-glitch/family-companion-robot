# Connection Diagnostics Implementation Plan

> **For agentic workers:** Use subagent-driven-development for independent code tasks and review; root coordinates firmware/cloud/hardware. User already approved the design and execution. No additional worktrees or branches.

**Goal:** Identify the last completed stage when Waveshare stays connecting, without changing credentials or masking faults by automatic restart.

**Architecture:** Common S3 trace events and a forwarding NetworkInterface/Tcp adapter correlate with bounded Go entry logs through `X-Robot-Connection-Id`. Existing transport, authorization and audio policies retain ownership.

**Tech Stack:** C++/ESP-IDF 6.1, Go, existing Python cloud test/build tooling.

## Global Constraints

- Direct main; only Waveshare ESP32-S3 2.16 is a product build/install gate.
- All runtime tests/dependencies/build/deployment on cloud; Mac code/Git and USB byte client only.
- Trace header exact format `^c1-[0-9a-f]{16}-[0-9a-f]{8}$`, length28; invalid or missing header ignored without changing auth/results.
- Trace is not authority. No raw headers/credentials/body/audio/SSID in logs or Git.
- No synthetic product records/mock UI, new services/dependencies, automatic reboot or unrelated fixes.
- Firmware sockets retain their own immutable trace ID; old callbacks cannot acquire a newer ID.
- Existing user work in audio-capture tools and M01 verification remains untouched.

### Task 1: Backend request correlation

**Files:** Create `internal/realtimelive/connection_trace.go`, `connection_trace_test.go`; modify `xiaozhi_entry.go` and minimally `server.go` for an internal log sink.

**Interfaces:** Header above. Fixed stages and numeric statuses. Existing `DeviceHandler`, authenticated ownership and admission leases remain authority.

- [x] Add failing tests for valid/missing/malformed IDs, auth rejection, successful upgrade/hello and post-disconnect reconnect. Assert original HTTP status and lease cleanup, and absence of token/body/header injection in captured logs.
- [x] Run cloud RED against unchanged production code.
- [x] Implement strict parser and bounded stage logger, hook actual entry/auth/version/admission/upgrade/hello/session boundaries; include elapsed milliseconds and server-owned session ID where available.
- [x] Cloud `go test -race ./internal/realtimelive -run 'ConnectionTrace|DeviceHandler|MultiDevice'`, relevant full package checks and vet/build; report exact commands/results. No production deploy by task agent.
- [x] Self-review and independent spec/quality review; commit only task-owned files.

### Task 2: Common firmware trace and failure fixtures

**Files:** New shared diagnostics header(s), tracked patch for application/protocol wiring, preparation/source tests and host harness.

**Interfaces:** `c1-<boot64>-<attempt32>` IDs; immutable attempt snapshot passed to network adapter; fixed stage names and numeric codes, times and heap values. Adapter forwards NetworkInterface/Tcp semantics exactly and never receives logging ownership of auth/body data.

- [x] Add failing C++ host tests for stage sequencing, per-socket ID isolation, TLS and transmit failure, fragmented HTTP response, 401/503/101 responses and repeated connection attempts. Compile actual extracted application/protocol hooks with existing harness where possible.
- [x] Run cloud RED; implement the smallest shared trace and forwarding adapter. Observe TLS+underlying connect as one phase, HTTP upgrade separately.
- [x] Add safe application button/state/continue/recovery/hello/listening records and header wiring; no change to retry, timeout or audio behavior without a reproducer.
- [x] Run cloud diagnostics/supported firmware host tests; validate fresh locked preparation, source invariants and unchanged auth/byte forwarding.
- [x] Review final actual code and test gaps; commit explicit owned files.

### Task 3: Native build, deployment, installation and evidence

**Files:** Cloud-private task directory; verification document and latest handoff in repository. Generated firmware/backup/config/serial logs stay out of Git.

- [x] Freeze committed public source manifest and prepare a new cloud source tree; reuse only checked current Waveshare build dependencies/cache. Verify all project compile inputs and effective SDK/partition/assets, build app and record hashes.
- [x] Test backend against isolated fixtures; build candidate against actual runtime-source baseline, preserving legitimate differences. Deploy only after empty-session check and preserve service/config/data/firewall snapshots.
- [x] Identify unique Waveshare USB, no competing reader; cloud backup/current identity and active slot check. Install active app only and verify whole slot and outside ranges before one controlled boot.
- [x] Capture startup/first attempt with cloud-private logs, verify heartbeat and correlated trace when a real device connection occurs. Preserve live user conversations; do not silently inject faults into production.
- [x] Record implemented/deployed/verified/pending separately. Root cause only confirmed by a repeatable failing case and a fix that passes that same case; brief simulated outages do not establish real Wi-Fi/long-idle acceptance.

## Progress

- 代码及独立审查完成：诊断来源0fb6a4c，最终固件b0d8f01含图标安全区；云端160/160零skip、原生整包/来源证明、93/93运维回归及最终审查通过。Actions仍受已有cJSON夹具摘要不一致阻断，不能计CI成功。
- 实际Voice发布及证明完成；微雪当前16MiB备份、活动槽整槽回读、槽外/整片MD5和一次启动完成。USB重枚举后的原失败采集证据保留，另建被动采集300秒/844825B完成、无panic。临时字节客户端及仅42217转发已关闭，18791和原SSH保留。
- 用户确认按键进入聆听，16条设备阶段与7条Go阶段同CID关联，按键至聆听3469ms；正常连接通过。最初启动片段受USB重枚举影响不完整，后续稳定日志及首个按键连接已抓到；原始偶发卡住、真实Wi-Fi断网和长待机仍未现场复现/验收，图标圆角可见性仍待用户确认。

- 独立RuntimeDTO核对完成：在线、心跳年龄1.5秒、音量/亮度80/80；安装前NVS同为80/80。独立临时家庭会话读取后已注销，其余会话、权限及实际设置不受影响。
