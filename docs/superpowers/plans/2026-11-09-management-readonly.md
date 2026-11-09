# 管理平台真实只读闭环实施计划

> 使用subagent-driven-development持续按任务实施和审查；直接main提交/推送，不建分支或worktree。用户已确认核心UI并授权继续开发，旧记录不得自动导入。

**Goal:** 真正登录后，两端按显式家庭/设备与绑定时间查看新产生的最终转写、生成稿和可靠播放证据。

**Architecture:** 一个独立Go管理HTTP服务，同源提供live前端及API，复用现有元数据合同/播放证据算法；初版账号和绑定由云端私有配置供应，原始语音记录只读，保留独立mock入口。

**Tech Stack:** Go标准库（net/http、crypto/pbkdf2）、现有Go包、已确认Vue3/TypeScript/Vite，不新增数据库、执行框架或前端依赖。

## Global Constraints

- Develop, commit and push directly on main; no branches or worktrees.
- Install dependencies, execute runtime tests and build/deploy on the cloud server. Mac is code/Git and browser client only.
- Only records from the current Waveshare device after this binding may be opened. No historical import or inferred family ownership.
- No credentials, raw audio, runtime records or generated artifacts in Git or Mac files. No voice/model/firmware changes, microphone or playback.
- Every resource request checks scope/account/family/device/time; admin role does not imply body access. Unsupported real operations never fall back to mock.
- The smallest working implementation; no extra database/index service, frameworks or speculative abstractions.

## Task 1: Private authority and record projection

Files: new `internal/management/types.go`, `config.go`, `records.go` and focused tests. Reuse existing source types/helpers and PlaybackEvidence; avoid changing production voice behavior.

Interfaces: `Config` contains schemaVersion1, accounts (id,username,passwordHash,internalAdmin,memberships), families, devices and nonoverlapping dated bindings. `LoadConfig(path)` validates regular private0600 file and bounded fields; `WritePrivateConfig` uses existing private atomic JSON writer. `RecordReader` accepts record root and profile source, lists authorized metadata and projects one selected source record into frontend DTOs with evidence provenance. Pure projection can be tested with synthetic JSONL; no actual private record in test fixtures.

- [x] Add red tests for configuration validation, binding-before/after and two-family/role restrictions; implement smallest authority/DTO helpers.
- [x] Add red tests for latest full response without settled event, partial/interrupted evidence, missing totals, rejected input, damaged/partial tail, mismatched source identity and path escape; implement bounded reader using real source contract.
- [x] Cloud `go test -race ./internal/management`, focused vet; review spec/quality and commit task1.

## Task 2: HTTP authentication, scoped reads and cloud command

Files: new `internal/management/auth.go`, `server.go`, `audit.go`, `cmd/management/main.go`, tests, `deploy/management/` service template and README.

Interfaces: `NewServer(Config, Options)` returns `http.Handler`; Options carries records/profile/identity/static/audit paths and now function for tests. Endpoints `/api/{admin|family}/{login,logout,session,devices,families,accounts,conversations}`. `RecordReader` from Task1 supplies scoped DTOs. CLI subcommands `serve` and `init` generate protected initial config/password on cloud; no plaintext secret in command args or source.

- [x] Red tests: password/session/login limits, separated scope cookies, two-family and body denial, explicit device mismatch, origin guard, pagination bounds, audit failure and unsupported endpoints.
- [x] Implement standard-library sessions and password derivation; all resources check authority before reading body, deny wrong scope, never include private paths in errors.
- [x] Add loopback-only command/static server, no-store/security headers, bounded server timeouts, private audit and independent systemd deployment.
- [x] Cloud target/race tests, vet and command build; review/commit task2.

## Task 3: Live frontend adapter and truthful core UI

Files: `web/management/src/api.ts`, new remote client/mode module, minimal types/CorePages/DataPages/App/navigation changes, frontend tests and README. No new dependencies.

Interfaces: explicit live build (`VITE_MANAGEMENT_API_MODE=live`) selects real client; default mock remains. Same page API contract, plus paginated conversations. Live reads use same-origin credentials, errors remain errors, unsupported methods reject. Nullable unreported values and source/evidence fields remain truthful. Login supports account/password for both live scopes; no email/SMS verification promise.

- [x] Add red live-client behavior tests (requests/auth/401/403/error/no-mock fallback/paging); implement only real methods used by this slice.
- [x] Gate live pages behind session; use actual/default authorized device IDs, clear on logout; show connected/read-only mode, timestamp grouping and unknown fields. Restrict nav to implemented live routes; keep full31-page mock app available.
- [x] Manual/visible low-frequency record refresh; preserve async route guards and cached-page restoration. Disable all unimplemented writes in live mode.
- [x] Cloud frontend tests/typecheck and demo+live builds; Mac browser checks both routes, login/logout/device/empty/permission/failure paths and regression of mock core.
- [x] Review/commit task3.

## Task 4: Isolated deployment and end-to-end evidence

- [x] Verify exact current Waveshare ID from trusted server metadata without copying any provider/device credentials; capture only hashes/health/service identity for protected baseline.
- [x] Create private initial account/family and binding timestamp at actual deployment; historical data remains outside authorized range. Deliver initial account access without credential files on Mac or Git.
- [x] Deploy standalone manager127.0.0.1:18791 and built live UI, keep mock18790; SSH forwarding for browser only. Bound service filesystem writes to its own config/audit/runtime area.
- [x] Cloud HTTP/browser validate real login, authorized device and initial empty scope; synthetic two-family/body/evidence tests remain distinct from physical acceptance.
- [x] Service/UI ready 后已请求用户正常发言；目前无完成回执，按本项要求明确报告physical step pending，未代用户开麦。
- [ ] 用户新实机发言后，核对新增记录、最终转写与播放证据；此现场验收仍待完成。
- [x] Final broad review, fix findings with focused verification; update handoff/roadmap and validation report, commit/push main and verify remote SHA/clean tree.

## 当前结果

功能源码4083e55，云端独立发布live-20261109-r3，绑定从2026-11-09T02:05:08.453461251Z开始。云端与浏览器检查、复审见[验证记录](../../verification/2026-11-09-management-readonly.md)。开发与部署完成，实机新增记录验收保持待验证。后续再增加业务聊天归档及写操作，不把mock页面视为真实交付。
