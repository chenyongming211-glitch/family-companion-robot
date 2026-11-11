# 网页UI UX Pro Max → unslop实施计划

采用当前会话直接执行，顺序已由用户明确；复用现有Vue/CSS与云端构建、浏览器客户端，不引入业务依赖或mock，不新建分支/worktree。

**Goal:** 让真实管理两端的层级、响应式、图标与文字适合家庭机器人日常管理。
**Architecture:** 仅展示组件/样式与辅助导航调整，领域控制器、API和隐私状态所有权保持。
**Tech Stack:** Vue3/TypeScript/CSS/Vite、云端Node、Ego浏览器、18px本地Lucide SVG片段。

## Global Constraints

- 直接main；运行依赖/测试/构建仅云端，Mac只代码/Git/设计查询/浏览器。
- 保持真实API、权限/隐私隔离、整10滑块与回执/清除规则；不得新增mock、业务写接口、设备控制或数据迁移。
- 不改Voice/管理二进制或重启服务，不改Caddy/防火墙，不重设原审计事务基线；原用户audio-capture三文件和M01文档不动。
- 产品名伴伴、灰白表面、陶橙#9c4b24、中文系统字体、18px统一线图标，正文至少14px/手机16px；44px可点目标、可见焦点、4.5:1文字对比；桌面家庭1200px、手机单列底栏。

### Task1 UI UX Pro Max

Files: web/management/src/{App.vue,styles.css,data-pages.css,pages/CorePages.vue,pages/AuditPage.vue,components/RuntimeControls.vue}；新增components/UiIcon.vue及THIRD_PARTY_NOTICES.md。领域控制器/API文件不改。

- [x] 读skill并运行--design-system与Vue/UX/Flat Design查询，记录不合产品的自动推荐理由；实际两端截图。
- [x] 统一tokens、侧栏/列表/表单/统计行、图标、状态语义及响应式。纯展示在线数直接使用devices.filter(d=>d.connection==='online').length，不使用服务已接入常量充当遥测。
- [x] 云端同步当前web源码并记录逐项SHA；复用既有node_modules，运行npm test/typecheck/build，确认源码前后不变。
- [x] 用现有发布保护读函数比较进程/config/records/.memory/Caddy/firewall/audit前缀，只静态更新；浏览器检查第一轮实际结果。

### Task2 unslop

Files: 同一组展示文件；若家庭标题改变，tests/app-auth.test.ts仅调整该标题预期。

- [x] 读取signs/tokens/visuals/refs，记录产品五行方向，按九项复核第一轮；只修复具体发现，保持功能/主内容顺序，不套营销页或更换趋势。
- [x] 精简通用欢迎/技术徽章，页面目录降为辅助导航，统一圆角/分隔/留白；保留真实授权、未知/失败与播放解释。
- [x] 云端最终覆盖验证及构建，核对源与4项静态产物；原子发布后实际两端读取与375/768/1024/1440布局、焦点/对比/深色偏好检查，无实际设置/清除。
- [x] 移除无引用旧资产，更新README/交接/验证，直接main提交推送并核对远端；保留UI/UX和unslop技能来源/版权；完成浏览器任务。

## 收尾

两轮已按顺序完成并静态发布，最终52项/类型/构建通过、35源项与提交a7e4173相同；独立审查2处对比问题修正后PASS。8组视口及真实两端登录/设备/控制只读/聊天/记忆/来源/审计通过；旧4资产已清、当前4文件、服务及数据保护通过。浏览器视口模拟已清除，TaskSpace10保留两结果页。未操作设备或新增业务功能，原native CI DNS与审计事务问题仍未关闭。main文档收尾单独推送并核对，详情见[验证](../../verification/2026-11-11-web-uiux-unslop.md)。
