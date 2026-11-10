# 小程序家庭端实施计划

> **For agentic workers:** 使用 subagent-driven-development 或 executing-plans 按任务执行；用户已授权直接 main 开发、提交和推送，不建分支或 worktree。

**Goal:** 登录→家庭设备→聊天详情→真实云端设备昵称/位置设置，并保留原扫码配网。

**Architecture:** 原生 App 上共享 RAM Api，会话与页面失效代数隔离；复用 Go 只读接口，设置通过现有 ConfigStore 事务保存。

**Tech Stack:** 原生 TypeScript/WXML/WXSS、现有 Go 管理服务与 Caddy，无新增依赖。

## Global Constraints

- Mac 仅代码/Git与微信测试客户端；安装、运行测试、构建、服务均在云端。
- 凭据、正文、原始音频、固件与备份不入 Git，不写入 Mac。
- 所有家庭数据仅当前授权及绑定起点后可见；未知设备在线状态不伪装在线。
- 设置仅家庭展示昵称和位置，云端保存后重读确认；本批不声称硬件参数下发或回读。
- 不刷或重新绑定微雪；不再操作已恢复标准固件的退货 M5；不重启正式语音服务。

### Task 1: 真实设备首页与会话桥

- [ ] 创建 `web/miniprogram/tests/family.test.ts`、`tests/family-pages.test.ts`，先验证共享 RAM 登录、授权设备列表、隐藏后清空及迟到请求失效；云端运行并记录 RED。
- [ ] 扩展 `src/api.ts` 精确允许设备/聊天/设置路径，添加共享 `src/family.ts` 与 `src/family-page.ts`。接口为 `familySession(platform)`，页框架 `familyPage(kind)`；登录/退出/401通知所有监听者，Onboarding 通过可选 Api 复用当前登录。
- [ ] 首页、设备详情原生页面与 build/verify-native 支持多页；默认首页，老设备码 index 路径保留，添加成功可打开对应详情。
- [ ] 云端 `npm test`、`npm run typecheck`、`npm run build` 验证会话桥及原配网回归；提交 Task 1。

### Task 2: 聊天列表与详情

- [ ] 先写真实 API 请求、分页加载、设备/聊天配对、未授权正文、已播/未播区分与隐藏清理的失败回归。
- [ ] 同一 `familyPage(kind)` 支持 `chats`、`chat`，请求明确 device 参数；分页一次20条，失败清旧正文，重试重新鉴权；WXML分别呈现已播与生成稿。
- [ ] 云端完整测试/类型/原生产物检查；核对超长正文布局与空/错误状态；提交 Task 2。

### Task 3: 真实云端展示设置

- [ ] 先写 `internal/management/settings_test.go` 的 GET/PUT、跨家庭/只读成员/权限撤销、409、非法键与审计/存储失败测试；云端定向 RED。
- [ ] 新增 `settings.go` 和 DeviceConfig 可选 display_name，Reader 展示覆盖；Server 精确 settings 分发，ConfigStore 事务内再授权与版本核对，仅目标展示字段修改。接口 JSON 由设计文档定义。
- [ ] 小程序设置表单校验/加载/保存重读，409提示刷新；Caddy 只代理确切新路径。保留原设备不可读遥测的未上报状态。
- [ ] 云端 `go test -race ./internal/management ./cmd/management`、`go vet`、`go build` 与小程序全量检查通过，记录生产配置兼容/备份与回滚条件；提交 Task 3。

### Task 4: 交付检查与发布

- [ ] 独立源码审查，修复阻断问题并定向复验。
- [ ] 云端构建管理候选及小程序 dist，精确核对源与产物摘要；私有备份原配置，管理仅换二进制保留 live web 静态目录，不修改语音与记录源。
- [ ] Caddy validate 后只添加授权家庭路径并 reload；未登录公网401、无效路径404、设备与历史保护摘要核对。
- [ ] 将小程序产物复制到 Mac 既有测试工程并用官方工具编译/预览；有现场阻碍时明确记录，不补记手机 PASS。
- [ ] 更新 README、验证文档与 DEVELOPMENT_HANDOFF，核对 main 推送及最终工作区。
