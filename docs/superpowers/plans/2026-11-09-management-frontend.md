# 管理平台三批 mock 前端实施计划

> 执行方式：当前会话按任务持续实施；使用subagent-driven-development进行独立任务实施和只读审查。直接main开发、提交、推送，不创建分支或worktree。

**Goal:** 连续交付两端全部31页的mock前端，先完成设备→聊天→详情→设置核心流程，再补家庭与数据、能力与运维。

**Architecture:** 一个Vue项目、内部与家庭两个HTML入口，共用类型和集中异步mock API；按领域组织页面，普通hash导航与表单。复用已有HTML原型的视觉和内容边界，不引入模拟服务器或额外框架。

**Tech Stack:** Vue 3、TypeScript、CSS、Vite、vue-tsc；Node内置测试器验证mock数据行为。依赖版本从官方npm元数据选择并锁定，云端生成lockfile。

## Global Constraints

- Develop, commit and push directly on `main`; do not create branches or worktrees.
- Install dependencies, run runtime tests and build on the cloud server; the Mac is for code/Git and browser review.
- Use the smallest working implementation; reuse the existing prototype design, without a component library, global state framework or mock server.
- All data is fictitious. Every page and mutation must clearly identify the demonstration. Never read production records, credentials, audio or device backups.
- No backend, voice service, model invocation, microphone, audio playback or firmware change in this task.
- Supported firmware is ESP32-S3 only; C3 is not a release gate.

## Task 1: Shared API and first batch (10 pages)

Files: `web/management/package.json`, `vite.config.ts`, `tsconfig.json`, `index.html`, `family.html`; `src/{main.ts,App.vue,types.ts,seed.ts,api.ts,styles.css,navigation.ts}`; `src/components/`, `src/pages/CorePages.vue`, `tests/api.test.ts`.

Interfaces: `Scope = 'admin' | 'family'`; typed device, conversation, turn, memory, family and capability structures in `types.ts`. `api.ts` exports `createMockApi(storage?)` for tests and shared `api` for pages. Reads and writes return Promises and detached values. Core methods: `listDevices`, `getDevice`, `updateDevice`, `listConversations`, `getConversation`, `reset`.

- [ ] Write behavior tests for device isolation, invalid settings, unknown IDs, forbidden body and snapshot isolation; verify expected failures in cloud before implementing corresponding behavior when cloud access is available.
- [ ] Implement simple fixtures and API functions, rejecting invalid input before mutation; persist only demonstration state.
- [ ] Build both entry layouts, shared navigation and core pages: admin overview/devices/device/conversations/configuration and family home/device/conversations/conversation/settings.
- [ ] Show loaded/empty/error states; make selected device and conversation IDs explicit in links; avoid stale asynchronous data overwriting a newer route.
- [ ] Cloud `npm install`, `npm test`, `npm run build`; Mac browser checks core flow and responsive layout. Record unavailable checks honestly if access is still blocked.
- [ ] Review the task's source, fix important findings, commit first batch to main.

## Task 2: Family and data pages (14 pages)

Files: `src/pages/DataPages.vue`, necessary additions to `api.ts`, `types.ts`, `seed.ts`, navigation and tests.

Interfaces: consume shared typed API and layout; add only page-required functions for families, members/invitation/binding, demonstration login/logout, memories, privacy and daily grouping. Mutations return updated demonstration values and throw named errors; no real account, authorization or archive implementation.

- [ ] Add behavior tests for invitation/binding validation, memory clearing, conversation deletion and privacy preference consistency before relevant API behavior.
- [ ] Implement admin login/families/family/accounts/policies/memories/daily and family login/family/invite/bind/privacy/memories/memory.
- [ ] Ensure memory details link to their actual source conversation; deleting one conversation removes its demonstration derived memory/summary, while memory-only clearing preserves chat records.
- [ ] Check ordinary form feedback, action confirmation, duplicated submissions and persistence/reset semantics.
- [ ] Cloud focused tests/build; browser all new routes and main mutations; review and commit batch 2.

## Task 3: Capabilities and operations (7 pages)

Files: `src/pages/OperationsPages.vue`, necessary fixture/API/style additions, README and verification records.

Interfaces: use shared API types, navigation and feedback. Methods for capability list, expression options, task/firmware/operation/audit demonstration data are narrowly scoped read functions.

- [ ] Implement capabilities/expressions/voiceprints/tasks/firmware/operations/audit, retaining true product-stage wording and fictitious metric labels.
- [ ] Provide capability filter, 15-expression visual preview and named detail views; unknown or unsupported operations explain their stage rather than claim execution.
- [ ] Cloud full API tests, typecheck/build and dependency audit; browser 31 routes × 1440/768/390 plus 375 mobile core flow.
- [ ] Resolve all important review findings, update README, previous plan and handoff with exact achieved/unverified scope.
- [ ] Commit/push main, verify remote SHA and clean working tree, deliver review links and validation evidence.
