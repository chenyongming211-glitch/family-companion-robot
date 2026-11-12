# Web Device Settings Implementation Plan

> **For agentic workers:** Use subagent-driven-development for the two scoped implementation tasks, fresh task review and final review. User already approved implementation and release. Main only; root alone stages/commits serially.

**Goal:** 家庭网页和内部后台可修改昵称/位置，复用已有设置规则并读回确认。
**Architecture:** Scope-aware existing Go settings handler; one typed web API, controller and shared Vue form. ConfigStore remains the sole writer; voice deviceprofile stays separate.
**Tech Stack:** Existing Go, Vue3, TypeScript, CSS, node:test; no new dependency.

## Global Constraints

- Direct main, no branches/worktrees. Runtime tests/build/dependencies/deploy only on cloud; Mac code/Git/browser client.
- No changes to user audio-capture files/M01 note, miniprogram behavior, Voice or firmware. No product mock/seed/demo fallback.
- GET/PUT `/api/{family|admin}/device-settings/{id}` DTO `deviceId,name,location,version,canEdit`; PUT exactly `name,location,version`. Version64 lowercase hex; deviceId must match request.
- Read: current effective binding + explicit device grant; admin additionally InternalAdmin. Write additionally family CanAddDevices. Recheck scope/session/role/permissions/version after writer lock; audit actual scope.
- Name1..12 Unicode code points, location0..24; existing server normalization/validation/version/transaction authoritative. No change to voice profile/name, binding, audio settings or past records.
- Save once PUT then GET exact matching id/name/location/version; no automatic retry.30second operation timeout becomes unconfirmed; late results cannot show success; explicit fresh read required before another save.
- Hide/unmount/device/scope/route/401/account change clears drafts/results and invalidates late callbacks. No sensitive storage.
- Preserve current UI layout/palette. Simple shared form, no new framework/animation or unrelated refactor.
- Production scope management executable+web only. Preserve current config/data/profile/audit prefix, Voice and other service processes, Caddy/firewall; retain original unclosed audit transaction without rebaseline/restore.

### Task 1: Shared backend settings authorization

**Files:** Modify `internal/management/server.go`, `settings.go`; add `admin_settings_test.go`; adjust only now-obsolete admin-settings denial assertion in `admin_device_test.go` if needed. Existing settings tests remain.
**Interfaces:** Existing `settings(c,actor,id)` may gain explicit scope; route passes `parts[0]`. Reuse Config.Authorize and current management permission rule. GET/PUT family shapes unchanged.

- [x] Add tests exercising real Server GET/PUT admin scope with separate admin Cookie, persistent name/location read by family and admin, dynamic audit scope and unchanged voice profile. Expected new admin GET200 currently fails.
- [x] Add negative cases: family Cookie on admin401, role rejected by existing scope authentication401, absent grant/binding403, read-only canEdit=false and write403, lock-delayed role/grant/manage/session revocation, cross-scope version409, invalid/extraneous fields and failed audit/config write preserve data. Use existing fixtures and writer-lock test patterns.
- [x] Run cloud RED before production edits: `umask 077; go test -race ./internal/management -run 'AdminSettings' -count=1` using installed cloud Go/libopus env; record actual failure.
- [x] Minimal implementation: `if resource == "device-settings" { s.deviceSettings(...) }`; replace hardcoded family in settings/transaction session/audit with validated scope; keep one version/normalization/write implementation. Recheck fresh account via `s.account(freshRequest, scope)`.
- [x] Run new + existing settings and related scope tests, full management functional tests, `go vet ./internal/management`, `go build ./cmd/management`; report exact cloud command/source/result. Root commits owned backend files and dispatches fresh spec/quality reviewer.

### Task 2: Shared webpage settings and confirmation

**Files:** Modify `web/management/src/{types.ts,remote-api.ts}`, `pages/CorePages.vue`; create `src/device-settings.ts`, `src/components/DeviceSettings.vue`, `tests/device-settings.test.ts`; extend existing remote API tests. Keep `api.ts` inferred type compatible.
**Interfaces:** `DeviceSettings {deviceId:string;name:string;location:string;version:string;canEdit:boolean}`; `getDeviceSettings(id,scope)`, `updateDeviceSettings(id,{name,location,version},scope)`. New component accepts deviceId/scope, emits saved DTO after verified GET. Core refreshes loaded device and selector list.

- [x] Add tests for both scope URLs/method/body and rejecting mismatched/malformed DTO. Controller tests use injectable transport/deferred promises: one PUT+GET confirmation, mismatch/version conflict, revoked/read-only writes blocked, duplicate submission, failed read/write,30s timeout and ignored late completion, fresh read after unknown, hide/device/scope/account invalidation.
- [x] Run cloud RED against absent methods/controller before product code. Tests are isolated input fixtures, never a runtime mock product.
- [x] Implement typed response checks and minimal controller factory `createDeviceSettingsController(api,state,...)`, form using existing panel/form styles. Code-point length validation must allow astral characters without UTF16 maxlength truncation. Capture active epoch before PUT, verify returned DTO then read again; only matching final GET emits saved.
- [x] Add form on settings/configuration pages before existing RuntimeControls. Reuse sensitive invalidation and document visibility lifecycle; refresh currently loaded device/list safely after saved, keeping selected device/scope explicit. Preserve draft/feedback clearing on hide/unmount and all late response guards.
- [x] Cloud `npm test`, `npm run typecheck`, `npm run build`; report source and output hashes. Root commits only owned frontend files and dispatches fresh spec/quality review. Browser integration is root's Task3.

### Task 3: Integration, actual candidate and release

**Files:** Verification/handoff/active overall plan; ignored local code-only reports/operators and private cloud task directory `/home/ubuntu/web-device-settings-20261112`.

- [x] Root packages frozen public task source to cloud; preserve all existing dirty files. Freeze actual management manifest/exe/static and create minimal candidate from that actual source, overlay only reviewed task backend files; prove other inputs unchanged. Run candidate settings/scope race plus full management functional tests, vet/build; use public related race plus complete functional regression as recorded below. Full package race remains incomplete and is not counted as a pass. Verify the frozen frontend build.
- [x] Fresh final review consumes scoped diffs and cloud reports. Test any required release operator changes in isolated fake fixtures; retain original private transaction and current authoritative data, never silently retry failed operations.
- [x] Browser in one TaskSpace: isolated backend+real built UI, synthetic family/admin cookies and devices, save→GET confirmation, cross-view refresh, forbidden/readonly/conflict and375/1440 layouts. Failed/late/privacy cases use controller and compiled-component automation, not additional browser fault injection. Keep fixtures isolated, close temporary listeners/forwards after.
- [x] Deploy management+web only with quiet/health/protection checks. Use a new release and task-owned drop, unchanged argv except exe/static paths, original release preserved. Do not mutate Caddy/firewall, Voice, USB or actual device settings. Explicit rollback may restore only owned executable/static selection, never old user data.
- [x] Authorized production GET of both settings scopes and existing runtime/chat/memory/audit routes; use isolated temporary sessions and logout only those cookies. No production PUT/clear. Record real write/miniprogram field refresh separately if not executed.
- [x] Update actual implementation/deploy/test/pending evidence, architecture self-review, plan and latest handoff; direct main commit/push, verify remote SHA and unrelated dirty files preserved.

## Progress

- Task1/2代码、独立规格/质量/架构审查完成，最终无剩余Critical/Important。源码b69389a、f0f212f、44e3078；冻结230项逐字节匹配44e3078。
- 网页82/82（含22个真实编译组件用例）、类型/构建通过。连续保存刷新竞态及手动重新读取标签不同步两项P2均先失败、修复后通过、复审关闭。
- public完整功能124/348、相关race21/70、vet/build；实际来源完整功能122/346、新AdminSettings race4/16、vet/build通过，零skip。完整race超时/显式停止不计通过，最终审查接受scope-only改动的相关race加全量功能。
- 最终实际二进制3f071303、网页main-D8jiwrLl.js。最终浏览器双端保存/交叉刷新/409及重新读取标签、只读403、家庭Cookie拒绝admin、无设备授权403、375/1440布局通过；TaskSpace12/合成进程/18797转发已关闭。
- 用户要求先清磁盘：清掉无引用可重建缓存和重复二进制约6.5GiB，100%降至90%（约6.7GiB可用）；原数据/备份/服务保持，本任务目录约39MiB。未清理并发音乐任务、原始记录或凭据。
- 生产只读preflight通过，两临时会话已注销；最终pins9a161812及绑定c08c1f99发布后，管理PID2273712/3f071303、四项实际HTTP产物和两端授权设置/聊天/记忆/runtime/后台审计GET通过；两临时会话已注销，验收后保护仍相同。最终文档与Git同步见交接；原admin-audit中断事务独立未关闭。
