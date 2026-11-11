# 伴伴管理平台 · 两端前端

Vue 3＋TypeScript＋CSS＋Vite，内部后台19页、家庭端12页，按三批完成。既有HTML原型保留在`prototypes/management-platform/`，本目录是可构建的前端应用。

## 真实服务入口

核心UI已获确认，真实后端已接入两端：账号密码登录/退出、授权家庭与设备、绑定后的连接记录列表/详情，以及近期记忆列表/详情。真实构建使用同源Go管理服务，不回退演示数据。已发布设备心跳、音量/亮度控制与近期记忆清除；成员、设备资料编辑、聊天删除和留存写操作尚未接入。未上报遥测保持未知值，显示“尚未上报”。部署与现场验收状态以交接/验证记录为准。真实聊天已按明确结束/轮次结算后30分钟无有效交流分段归档，跨零点不拆分；原连接记录保留作来源。缺少终态或尾部未完整时显示待结算/记录更新中。

在云端生成两种独立产物：

```sh
npm run build
VITE_MANAGEMENT_API_MODE=live npm run build -- --outDir dist-live
```

默认`dist/`是完整31页mock；`dist-live/`只部署给真实服务。构建live时始终指定输出目录，避免覆盖演示产物。管理服务启动与私有账户配置见[部署说明](../../deploy/management/README.md)。当前SSH转发后的真实内部端为`http://127.0.0.1:18791/index.html`、家庭端为`http://127.0.0.1:18791/family.html`；演示保留18790。近期记忆入口为`#/memories`，从设备详情或“记忆”导航进入；显示实际快照中本次绑定后、7天内最多24轮的片段及来源聊天，支持手动刷新。它是可能截断的近期上下文，不等同于完整档案或长期人物记忆；清除需要当前授权正文读取成功及设备管理权限提示，系统一次确认后等待匹配设备confirmed，再重读近期记忆；聊天档案保留，新对话可产生新记忆。会话由HttpOnly cookie保存，正文仅保留页面内存。admin/family使用各自独立会话；任一入口的退出、401或账号变化均使当前页面敏感请求失效，重启服务需重新登录。

## mock评审入口

- 内部后台：`index.html#/overview`。
- 家庭端：`family.html#/home`。
- 两端顶部“页面目录”列出全部19/12页，可以直接跳转。
- 建议先看设备→聊天列表→聊天详情→设置，再看家庭/记忆/隐私，最后查看内部能力和运维页面。

所有家庭、成员、聊天与运行指标都是虚构示例。登录、邀请、绑定、设置、清除和删除只修改当前标签页的演示数据，不发送邮件/短信、不控制设备、不调用模型。这段仅描述默认mock构建；真实入口的范围见上文。

## 云端安装、验证与预览

依赖安装、测试、类型检查和构建只在云服务器执行。Node要求`>=22.18`，本轮使用经官方摘要校验的Node24.22.2；Mac仅用于代码/Git和浏览器评审。

```sh
cd web/management
npm ci --registry=https://registry.npmjs.org
npm test
npm run build
npm audit
python3 -m http.server 18790 --bind 127.0.0.1 --directory dist
```

`npm run build`先执行`vue-tsc --noEmit`再构建两个入口。需要开发热更新时，在云端运行`npm run dev -- --host 127.0.0.1 --port 18790`，与静态预览二选一。

Mac通过已有SSH授权建立转发，浏览器访问`http://127.0.0.1:18790/`：

```sh
ssh -N -L 127.0.0.1:18790:127.0.0.1:18790 ubuntu@82.156.48.111
```

本轮独立云端源码与运行目录位于`/home/ubuntu/management-frontend-20261109/`，静态预览只绑定云端回环地址，不修改正式语音服务。登录凭据不落盘，不进入仓库。

## 代码与数据边界

| 位置 | 职责 |
| --- | --- |
| `src/App.vue`、`navigation.ts` | 双端布局、31页目录、hash导航、账号/存储反馈 |
| `src/api.ts`、`remote-api.ts`、`mock-api.ts` | 显式选择真实同源API或演示适配器 |
| `src/pages/CorePages.vue` | 设备、聊天及基础设置 |
| `src/pages/DataPages.vue` | 登录、家庭、成员、记忆、隐私与每日分组 |
| `src/pages/OperationsPages.vue` | 能力、表情、声纹、任务、固件、运行与审计 |
| `src/runtime-control.ts`、`components/RuntimeControls.vue` | 双端共用设备控制、回执匹配、滑块草稿与50秒观察窗口 |
| `src/sensitive-session.ts` | RAM请求代次、隐藏/pagehide/账号失效通知；不保存Cookie或正文 |
| `src/components/FacePreview.vue` | 15种规范情绪的SVG示意；L2模拟嘴型可关闭 |
| `tests/*.test.ts` | 数据归属、校验、持久化、清除/删除范围与审计行为 |

页面调用集中API；真实请求不自动回退mock掩盖连接失败。`getDeviceRuntime(id, scope)`、`controlDevice(id, {field,value,boot}, scope)`和`clearMemories(id, scope)`只使用所选作用域。运行状态与清除回执在API验证形状/设备，控制器再匹配命令ID、字段、目标、boot、confirmed和实际读值。不引入组件库、全局状态框架、mock服务器或场景引擎。

设备列表每5秒更新前台心跳，详情/配置共用控制组件每2秒刷新；滑块0..100、每10一档、11圆点，input只预览、change提交一次。已有queued/sent操作禁用新提交。PUT/观察GET不返回也在50秒结束本次观察，迟到响应不能变为成功，清除及确认后重读同样有50秒截止；未知结果不重试。隐藏、pagehide、卸载或切设备清理状态/定时器，重新进入时读取新状态，刷新不覆盖正在编辑的草稿。

演示数据使用`sessionStorage`，同一标签页切换两端可保持修改，不同标签页不实时同步。顶部“重置演示”恢复初始数据；存储不可用时显示告警并使用内存。API返回独立快照，未知ID、设备/聊天不匹配、未授权正文均明确报错。

聊天详情分别展示最终转写、实际已播正文、未播生成稿、播放状态和保存状态，不提供原音播放。清除记忆保留聊天；删除一段演示聊天同时删除它关联的演示记忆和每日条目。mock中的30分钟会话仍为示例；真实入口使用共享Go归档读模型，语音模型上下文未切换；30天档案留存仍是建议。声纹、归档、Eino及扩展能力保持各自实际阶段，不把UI演示计为产品验收。

## 浏览器复核

云端构建并启动预览、Mac建立转发后，用Ego Browser作为测试客户端：

```sh
ego-browser skill
ego-browser nodejs < tests/browser-review.mjs
ego-browser nodejs < tests/browser-history-review.mjs
```

第一个脚本核对31路由×1440/768/390共93项，包括正确标题、示例标识、页面宽度与重复ID；结果JSON保存到系统临时目录。第二个脚本以确定性`pageshow.persisted`事件模拟缓存恢复，验证另一端删除的演示数据不会被旧页面保存复活，不代表自然bfcache路径已复现。操作流程和375px补充检查另外记录在[本轮验证](../../docs/verification/2026-11-09-management-frontend.md)。

设计与执行依据：[实施设计](../../docs/superpowers/specs/2026-11-09-management-frontend-design.md)、[三批计划](../../docs/superpowers/plans/2026-11-09-management-frontend.md)。

业务聊天实现与验证：[分段设计](../../docs/superpowers/specs/2026-11-09-chat-archive-design.md)、[执行计划](../../docs/superpowers/plans/2026-11-09-chat-archive.md)、[本批验证](../../docs/verification/2026-11-09-chat-archive.md)。


## 内部后台真实管理接入（2026-11-11）

内部端设备列表/详情显示心跳在线状态，设备详情和配置页提供音量/亮度，记忆列表/详情可按权限清除并核对回执；家庭网页共用相同组件/API。admin独立会话及当前内部角色不代替设备/正文/管理授权。新admin API只在原管理回环/SSH入口提供，未公开到小程序域名。

最终53项测试、类型/mock/live构建、合成浏览器流程与375/1440布局通过，后台服务和4份live产物已发布，原24条真实记忆及聊天保持。刷新不解除尚未结束的清除保护；50秒未知不自动重试。真实设备控制/清除未在本批现场执行。管理重启需重新登录；昵称/位置网页编辑、邀请、删除和留存仍未接入。详见[交付验证](../../docs/verification/2026-11-11-admin-device-management.md)。


## 内部后台操作审计范围

真实内部端 `index.html#/audit` 按设备、日期和操作类型查询已授权审计记录，展示请求及已核实结果；旧记录缺少回执时保持未知。本页显示平台已记录的操作；语音指令和设备按键变化暂未纳入。此批仅接入管理平台及家庭API已有操作记录，不修改Voice；直接语音工具和设备按键变化没有管理审计来源，也不能据尚未验收的声纹识别归属到家庭账号。
