# 扫码自动连接实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 用户扫码后自动连接对应机器人并进入 Wi-Fi 页，只需选择热点、填写密码、点连接。

**Architecture:** 已鉴权 waiting 认领响应提供由工厂 MAC 生成的 `ble_name`；控制器定位唯一同名广播并执行原安全握手。页面在有效前台扫码/登录完成时自动启动一次，多个家庭须先选择。

**Tech Stack:** 既有 Go 管理服务、TypeScript 微信原生小程序、安全配网 Security2；不增加依赖。

## Global Constraints

- Develop, commit and push directly on `main`; no branches/worktrees.
- All runtime tests/builds/dependencies execute on cloud, Mac is code/Git/native test client only.
- Preserve current activated Waveshare, original device/family/binding time/body access; do not reset, re-provision, flash firmware or restart voice services.
- Never connect the first arbitrary nearby device or weaken Security2 patch1/claim checks. No credentials, firmware, raw audio or private device backups in Git/Mac.
- Keep changes minimal; no new account system, telemetry, storage, animation or dependencies.

### Task 1: 自动连接完整行为

**Files:** `internal/management/onboarding.go`, `internal/management/onboarding_test.go`; `web/miniprogram/src/api.ts`, `controller.ts`, `pages/index/index.ts`, `pages/index/index.wxml`, existing/new onboarding tests and native verifier if required; `web/miniprogram/README.md`.

**Interfaces:** waiting create DTO adds `ble_name: "FAMILY_" + strings.ReplaceAll(d.MAC, ":", "")`; MAC is already canonical lower-case validated by factory store. Confirmed shortcut unchanged. `View` carries a plain text connection progress field. `Onboarding.start(familyId, name)` automatically discovers exact target and internally connects; page no longer exposes connect-device buttons. Existing native service filter stays intact.

- [x] Write failing cloud tests for authenticated target name (including arbitrary serial), no credential disclosure, automatic unique-target connection, no target/duplicate name failure, confirmed bypass, duplicate start guard and stale cancellation. Page tests cover exactly-once foreground scan→start, queued hidden scan only after onShow, login→continue, multiple families require explicit selection, and cancel/unload/session invalidation.
- [x] Run those tests on cloud and record actual expected RED failures before implementation. Use Go binary `/home/ubuntu/management-backend-20261109/runtime/go/bin/go` and Node from `/home/ubuntu/management-frontend-20261109/node-v24.22.2-linux-x64/bin`; SSH control `/tmp/family-management-preview-chen/control` to `ubuntu@82.156.48.111`.
- [x] Implement minimal DTO, controller auto connection and page auto start. Remove manual discovery/device list/nickname, keep default name and optional family selection. Show bounded connection feedback and clear no-match error; final Wi-Fi button “连接”. Keep official RSSI fix, literal deferred crypto require, lifecycle cleanup and server confirmation unchanged.
- [x] Run full cloud `go test -race ./internal/management ./cmd/management`, vet/build; mini-program `npm test`, typecheck/build/verify-native. Review against spec and commit scoped implementation. Report exact results and any limits.
- [x] Task spec/quality review and final broad review pass; resolve findings with cloud tests.

### Delivery (root)

- [x] Snapshot cloud service identity/config/protected hashes; deploy only management binary with same static assets/config/mounts. Verify health, protection invariants and confirmed ownership using safe read-only checks. Do not disturb real voice session.
- [x] Independently hash cloud-built client outputs, copy only public dist to the existing Mac native client, preserve private tool configuration, generate official CLI preview.
- [x] Update handoff/verification with actual tested/untested boundaries, commit/push main and verify origin exact HEAD.

代码/云端验证/独立复审/管理部署/原生预览已完成，证据见 [验证记录](../../verification/2026-11-09-onboarding-auto-connect.md)。本表完成指本次开发交付，不代表新设备手机现场整链验收；现场边界保留于验证记录。
