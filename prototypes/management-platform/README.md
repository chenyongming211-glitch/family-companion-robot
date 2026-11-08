# 管理平台页面原型

这是用于评审页面布局与交互的离线 HTML 原型，包含19个内部后台页面和12个家庭端页面。所有家庭、账号、聊天正文和运行指标均为虚构展示数据；能力实现阶段按现有产品边界标注。不存在真实登录、录音、设备控制、模型调用或后台API。

## 打开方式

直接在浏览器打开 [内部后台](index.html) 或 [家庭端](family.html)。不需要安装依赖或启动服务器。页面右上角的“全部页面”按钮可查看完整页面目录，两端可互相切换。

共享示例配置仅尝试存入当前浏览器的 sessionStorage，不保存真实资料；浏览器限制文件页面存储时退化为当前页面内存。原型页面可单独打开，hash 路由均可直接访问。

## 内部后台

| 页面 | 链接 |
| --- | --- |
| 工作台 | [打开](index.html#/overview) |
| 家庭列表 / 家庭详情 | [列表](index.html#/families) / [详情](index.html#/family) |
| 设备列表 / 五页签详情 | [列表](index.html#/devices) / [详情](index.html#/device) |
| 对话中心 / 页面状态预览 | [打开](index.html#/conversations) |
| 每日摘要 | [打开](index.html#/daily) |
| 记忆管理 | [打开](index.html#/memories) |
| 能力总览 | [打开](index.html#/capabilities) |
| 表情预览 | [打开](index.html#/expressions) |
| 声纹管理 | [打开](index.html#/voiceprints) |
| 任务中心 | [打开](index.html#/tasks) |
| 产品配置 | [打开](index.html#/configuration) |
| 会话与数据策略 | [打开](index.html#/policies) |
| 固件版本 | [打开](index.html#/firmware) |
| 运行状态 | [打开](index.html#/operations) |
| 操作审计 | [打开](index.html#/audit) |
| 账号与角色 | [打开](index.html#/accounts) |
| 登录 | [打开](index.html#/login) |

## 家庭端

| 页面 | 链接 |
| --- | --- |
| 首页 / 设备详情 | [首页](family.html#/home) / [设备](family.html#/device) |
| 聊天列表 / 正文详情 | [列表](family.html#/conversations) / [正文](family.html#/conversation) |
| 记忆列表 / 来源详情 | [列表](family.html#/memories) / [来源](family.html#/memory) |
| 我的家庭 | [打开](family.html#/family) |
| 邀请 / 加入家庭 | [打开](family.html#/invite) |
| 绑定机器人 | [打开](family.html#/bind) |
| 机器人设置 | [打开](family.html#/settings) |
| 数据与隐私 | [打开](family.html#/privacy) |
| 登录 | [打开](family.html#/login) |

## 推荐评审顺序

1. 工作台 → 对话中心：选择故事会话，查看已播与未播内容；用右上选择器预览空结果、同步中、保存失败和未授权状态。
2. 设备列表 → 设备详情：查看概况/对话/设置/能力/诊断；修改示例名称，确认页面显示变化。
3. 能力总览 → 声纹 → 任务中心：检查已接入/验证中/待开发的表达是否清楚。
4. 家庭首页 → 聊天 → 记忆来源 → 设置：查看手机阅读和操作方式。
5. 数据策略 → 审计 → 登录：评审授权、删除、生效与反馈文案。

## 已演示的交互

- 内部导航和两端切换、页面目录、页面搜索、家庭/设备列表筛选与详情。
- 对话主题选择、关键词/设备/日期筛选、正文权限状态、未播内容展开和轮次详情。
- 配置页签、示例名称与滑块保存、表情/状态预览、能力筛选、任务流程抽屉。
- 家庭聊天搜索、记忆来源、清除记忆确认与空态、邀请表单、绑定码校验、数据偏好设置。
- 所有“导出/发布/绑定/清除/邀请/删除”均为演示流程或确认回执，不产生真实外部动作。

视觉样式见 [DESIGN.md](DESIGN.md)；完整范围与正式开发路线见 [开发计划](../../docs/superpowers/plans/2026-11-08-management-platform-development.md)。正式前端建议使用 TypeScript + React + Vite，当前原型保持 HTML + CSS + JavaScript。
