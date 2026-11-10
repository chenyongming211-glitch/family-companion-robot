# Slider release and voice query implementation plan

> **For agentic workers:** 使用subagent-driven-development实现UI独立任务并审查；根代理同时只读调查语音查询，完成后统一收尾。用户已明确交互并授权main直接提交推送。

**Goal:** 滑块松手按整10档提交、显示圆点和具体反馈；查明并修正实际语音查询失败。
**Architecture:** 复用familyPage控制/回执状态机，只扩展事件和展示；语音沿用现有MCP固定状态工具。
**Tech Stack:** 既有TypeScript/WXML/WXSS、Go；无新产品依赖。

## Global Constraints

- main直接提交推送，不创建分支/worktree。
- 运行测试、构建和服务只在云端，Mac仅代码/Git/微信客户端；凭据、原始音频/正文和固件产物不进Git。
- changing只预览，change松手一次提交；0..100、step10、11个0..100圆点刻度，不拦截触摸。
- queued/sent不等于成功；保留50秒截止、已有权限/在线/能力门槛、单命令和页面生命周期隔离。
- 语音查询不得变更状态，当前值必须来自真实工具；未验证的现场效果明确待验。
- 不刷微雪或M5；语音发布若必要，只对当前部署源码做必要修正，空闲切换。

### Task 1: 小程序滑块松手提交

**Files:** `web/miniprogram/src/family-page.ts`、`src/runtime.ts`、`src/pages/settings/settings.wxml`、`src/app.wxss`、`tests/runtime.test.ts`。
**Consumes:** 既有`control(e)`、`observeCommand(epoch)`、`refreshRuntime()`及固定device-runtime DTO。
**Produces:** 独立changing/松手事件与整10归一化、11刻度展示、音量/亮度明确反馈；原回执协议不变。

- [x] 云端先加入RED：changing无PUT；松手一次PUT且整10取值；重复/无效/无权限/离线/隐藏不提交；相同当前值不提交；排队等待、匹配confirmed才成功；WXML确实无两个设置按钮且step10/刻度有效。
- [x] 最小实现：`bindchanging="runtimeInput" bindchange="runtimeCommit" step="10"`；松手校验当前页面/权限和数值后更新草稿并调用原control；保留同一次命令观察。每10一圆点，少量0/50/100数值辅助定位。
- [x] 完整`npm test`、`npm run typecheck`、`npm run build`在云端通过；提交仅本任务文件，提供RED/GREEN和精确源码摘要；独立Spec/Quality复核。

### Task 2: 语音查询诊断和必要修正

**Files:** 调查`internal/realtimelive/control_scope.go`、`native_tools.go`、`device_controls.go`、`internal/devicetools/bridge.go`和实际冻结部署源；仅证据指向处增加最小回归/修正。

- [x] 对齐实际二进制/冻结源；在云端定位用户原话对应轮次，按发现→意图→工具调用→过滤返回→语音事实链只报告脱敏状态。
- [x] 原话“现在的亮度和音量是多少？”应只得到`device_get_status`且同时要求volume/brightness；若实际缺陷，先RED后GREEN，不凭猜测改提示词。
- [x] 若需发布，验证必要包、来源清单和空闲会话后切换同一服务；保留设备身份、记录、600ms策略和其他服务。若无需改后端则不重启。

### Task 3: 客户端预览与交接

- [x] 原生官方CLI编译预览，更新既有Mac客户端产物；手机触摸/语音实际回答无回执时明确待验。
- [x] 记录实现/部署/自动化/现场边界，更新交接及计划，main推送、工作区与远端核对。

### 2026-11-11追加：相对亮度与模型语义方向

- [x] “把屏幕亮度调大一点”带明确亮度字段的兼容补丁c2523cc完成RED/GREEN、完整178顶层/587含子测race、vet/build及独立Spec/Quality复核；未发布。
- [x] 上下文/反话策略、语义规划及执行检查已按[后续计划](2026-11-11-contextual-device-control.md)实现发布；旧词表已替换，现场待验。
