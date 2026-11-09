# 近期记忆只读接入实施计划

> 使用subagent-driven-development逐任务实施/审查；用户已授权继续既有平台功能，遵循现有记忆和新绑定授权政策。直接main提交推送。

**Goal:** 两端安全查看机器人真实保存的近期记忆片段，并关联授权聊天来源。

**Architecture:** 在既有只读Reader增加有界快照投影、HTTP读取与审计；Vue复用原有记忆页面/集中真实适配器。不增加源写入、锁、服务、依赖或数据库。

**Tech Stack:** 现有Go标准库/recordstore.Snapshot DTO、Vue3/TypeScript/CSS/Vite。

## Global Constraints

- Develop, commit and push directly on main; no branches or worktrees.
- Install dependencies, run runtime tests/builds and deploy only on the cloud server; Mac is code/Git and browser client.
- Supported firmware ESP32-S3 only; no firmware, voice process, model, microphone or playback changes.
- No raw audio, runtime record bodies, credentials or generated builds in Git or Mac files.
- Only current binding's new memory with explicit household/device/body authorization; no historical import or inferred ownership.
- No source writes/cleanup/locks; never invoke recordstore.Open/Load from management.
- Keep existing7-day/24-turn policy, simple readonly pages and no mock fallback for real failures.

## Task 1: Snapshot Reader and scoped HTTP

Files: new internal/management/memories.go, memories_test.go, memories_http_test.go; modify types.go/server.go/audit.go. Reuse records.go safe filesystem helpers and Config.Authorize; no production voice code change.

Interfaces: `MemoryDTO{ID,DeviceID,ConversationID,Text,Date,User,Assistant,TextSource,Boundary}` JSONcamelCase; Reader.ListMemories(accountID,scope,deviceID string)([]MemoryDTO,error), Reader.ReadMemory(accountID,scope,id,deviceID string)(MemoryDTO,error). Current-binding check first with Authorize(now,true); join sources by trusted source SessionID/device and bindingID/version, then validate turn timestamp within binding and source start, now and7days. MemoryID `mem-<session32hex>-<positive int64>`. `.memory` path under Reader.Root, fixed SHA256 filename, max1MiB/24turns/24000runes, source text length limits512/1024. Directory missing503, device file missing200empty. Read-only snapshot, not recordstore manager.

HTTP list/detail as spec, mandatory exactly1 device query only. Invalid400, forbidden403, notfound404, source/tooLarge503. Audit read_memories/read_memory before output, even empty list; no text. Existing APIs unaffected.

- [x] Add failing tests for allowed snapshot/source link and double-family/body/admin denial; observe RED in cloud.
- [x] Add privacy/time/source/path/size/damage/atomic replacement tests and implement bounded reader. Verify no snapshot/lock/directory writes.
- [x] Add failing HTTP tests for mandatory/query/body/device/audit checks; implement branches and small auditAction helper retaining old conversation audit behavior.
- [x] Cloud gofmt, race/vet/build management+CLI; independent spec/quality review, fix then commit.

Core test shape (using synthetic existing fixture):
```go
items, err := reader.ListMemories("alice", "family", cfg.Devices[0].ID)
if err != nil || len(items) != 1 || items[0].ConversationID != "source-a" { t.Fatal("eligible memory missing") }
_, err = reader.ListMemories("metadata", "family", cfg.Devices[0].ID)
if !errors.Is(err, ErrForbidden) { t.Fatal("body permission bypass") }
```
Run cloud `go test -race ./internal/management ./cmd/management && go vet ./internal/management ./cmd/management && go build -o ../management-memory-candidate ./cmd/management`; all exit0.

## Task 2: Vue live memory routes

Files: remote-api.ts/mock-api.ts/types.ts/App.vue/CorePages.vue/DataPages.vue and focused tests. No dependency changes.

Interfaces: remote listMemories({deviceId?,scope?}) uses scoped query, getMemory(id,scope?,expectedDeviceId?) passes selected device; mock getMemory gains optional expectedDeviceId check. Memory types add optionaluser/assistant/textSource/boundary. App real whitelist adds memories bothscopes and memory family; admin detail stays existing memories?memory route. DataPages resolves omitted device to first authorized only, explicitly invalid device never substituted. Render live source/provenance/date, ordinary refresh; no clear action in live mode.

- [x] Add failing transport tests for exact memory endpoints/query and rejection of clear without fetch; implement methods.
- [x] Wire existing pages/default selection/source links/readonly controls and truthful policy labels; preserve mock and stale-response guards.
- [x] Cloud npm test/typecheck/mock+live builds; source review and commit.
- [x] Mac browser: bothscope list/empty/default/wrongdevice/error/fixture detail/source link/refresh/no-clear/mobile, mock memory regression. Runtime source remains cloud.

Example transport:
```ts
await api.listMemories({scope:"family",deviceId:"dev_fixture"});
assert.equal(seen[0], "/api/family/memories?device=dev_fixture");
await assert.rejects(api.clearMemories("dev_fixture","family"), /尚未接入/);
```

## Task 3: Deploy, review and handoff

- [x] Broad source review across both tasks; resolve important findings with covering tests.
- [x] Cloud install new binary/live bundle to new release; restart management only. No reinitialization or binding/password changes. Existing recordroot readonly mount already includes .memory.
- [x] Actual authenticated API new-binding filtered memory result, no raw snapshot export; source snapshot hashes/directory metadata before/after unchanged. Unauthorized responses checked synthetically.
- [x] Voice process/health/protectedhashes unchanged; documentation/roadmap/handoff distinguish implemented/deployed/syntheticverified/physicalpending.
- [x] Commit/push main, verify originSHA/clean tree; give same preview URLs. User deferred physical utterance; keep actual new-record/memory validation pending.

## 交付结果

后端2224c70、前端功能97b7f0d及样式4b57bde，管理发布memory-20261109-r3。实际API仅返回当前绑定范围内的空列表，源文件和语音保护核对保持；云端与浏览器证据见[验证记录](../../verification/2026-11-09-management-memory.md)。

- [ ] 用户后续方便时验证实机新对话→近期记忆保存→页面显示。本项现场验收不计为开发/合成检查完成。
