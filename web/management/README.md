# 伴伴管理平台 · 两端 mock 前端

Vue 3＋TypeScript＋CSS＋Vite，内部后台19页、家庭端12页，按三批完成。既有HTML原型保留在`prototypes/management-platform/`，本目录是可构建的前端应用。

## 评审入口

- 内部后台：`index.html#/overview`。
- 家庭端：`family.html#/home`。
- 两端顶部“页面目录”列出全部19/12页，可以直接跳转。
- 建议先看设备→聊天列表→聊天详情→设置，再看家庭/记忆/隐私，最后查看内部能力和运维页面。

所有家庭、成员、聊天与运行指标都是虚构示例。登录、邀请、绑定、设置、清除和删除只修改当前标签页的演示数据，不发送邮件/短信、不控制设备、不调用模型。真实管理后端和服务端权限尚未接入，待用户UI评审后开展。

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
| `src/api.ts`、`types.ts`、`seed.ts` | 集中异步API、必要类型、虚构初始数据 |
| `src/pages/CorePages.vue` | 设备、聊天及基础设置 |
| `src/pages/DataPages.vue` | 登录、家庭、成员、记忆、隐私与每日分组 |
| `src/pages/OperationsPages.vue` | 能力、表情、声纹、任务、固件、运行与审计 |
| `src/components/FacePreview.vue` | 15种规范情绪的SVG示意；L2模拟嘴型可关闭 |
| `tests/*.test.ts` | 数据归属、校验、持久化、清除/删除范围与审计行为 |

页面调用集中API；后续逐项改为真实请求，不自动回退mock掩盖连接失败。不引入组件库、全局状态框架、mock服务器或场景引擎。

演示数据使用`sessionStorage`，同一标签页切换两端可保持修改，不同标签页不实时同步。顶部“重置演示”恢复初始数据；存储不可用时显示告警并使用内存。API返回独立快照，未知ID、设备/聊天不匹配、未授权正文均明确报错。

聊天详情分别展示最终转写、实际已播正文、未播生成稿、播放状态和保存状态，不提供原音播放。清除记忆保留聊天；删除一段演示聊天同时删除它关联的演示记忆和每日条目。30分钟会话规则只展示已确认设计，未新增业务执行；30天档案留存仍是建议。声纹、归档、Eino及扩展能力保持各自实际阶段，不把UI演示计为产品验收。

## 浏览器复核

云端构建并启动预览、Mac建立转发后，用Ego Browser作为测试客户端：

```sh
ego-browser skill
ego-browser nodejs < tests/browser-review.mjs
```

脚本核对31路由×1440/768/390共93项，包括正确标题、示例标识、页面宽度与重复ID；结果JSON保存到系统临时目录。操作流程和375px补充检查另外记录在[本轮验证](../../docs/verification/2026-11-09-management-frontend.md)。

设计与执行依据：[实施设计](../../docs/superpowers/specs/2026-11-09-management-frontend-design.md)、[三批计划](../../docs/superpowers/plans/2026-11-09-management-frontend.md)。
