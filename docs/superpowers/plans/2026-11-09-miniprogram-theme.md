# 小程序主题与原生预览 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 按用户参考图实现扁平、少量半透明的蓝紫浅色小程序，并完成真实微信预览兼容。

**Architecture:** 保持原生WXML/WXSS和现有控制器；视觉任务只改模板、样式及导航背景。协调者独立处理预览打包兼容、云端构建、原生GUI验证和交接。

**Tech Stack:** 微信原生小程序、TypeScript、esbuild、Go管理服务。

## Global Constraints

- 直接main提交推送，不创建分支/worktree。
- 依赖、测试和构建在云端，Mac只代码/Git和微信测试客户端。
- 不改变API、登录/权限、加密、状态与取消语义；不创建工厂身份、刷机或操作麦克风。
- 用户图片的视觉要求已明确；色彩/尺寸属本次授权的常规实现选择。

### Task 1: 现有流程视觉统一

Files: web/miniprogram/src/app.wxss、src/pages/index/index.wxml、src/app.json。

- [x] 按设计重写样式与模板，所有现有事件名、data-key、设备data-id、picker范围、maxlength及success条件保持；仅使用已有data字段。
- [x] 用原生view/WXSS完成扁平robot插画、扫码/蓝牙/Wi-Fi/完成步骤图标，半透明只作局部装饰。
- [x] 将三份文件传入云端client-final/web/miniprogram，执行npm test、npm run typecheck、npm run build；结果须70通过、0skip，真实WXML阶段校验通过。
- [x] 小范围源码复审通过后提交本任务文件；不自行push或操作GUI/生产。

### Task 2: 预览兼容与最终验收

Files: web/miniprogram/build.mjs、verify-native.mjs、project.config.json；验证文档/交接。

- [x] 依据实际微信错误定位并修复加密模块依赖过滤与压缩兼容；保留延迟加载及BigInt能力检查，不能降级加密。
- [x] 云端测试/构建后只复制无凭据客户端构建包到Mac，保留合法域名校验。
- [x] 在微信开发者工具重新编译、检查实际依赖/预览包、默认页面与错误登录反馈，生成包含加密模块的新预览码。
- [x] 记录已实现/已部署/原生验证/手机与设备待验收，提交推送main。

## 实际结果与待验收

主题6af25aa、生命周期/请求3cab149、打包ba6e620已提交并推main；三项专项复审通过。云端最终70项零skip、类型/构建与编译后官方协议向量通过，7份Mac预览内容与最终云端dist摘要一致。微信原生首屏正常、58KB压缩预览成功，仅private.config被过滤，加密模块已入包；实际错误账号401反馈正确，合法域名校验保持。

本计划的开发与原生默认页/打包验证已完成。真实家庭登录、375px及后续业务阶段原生视觉、手机扫码生命周期与BLE、两款S3配网/绑定/播放仍待现场验证，未算作本次通过。详见验证记录，不因源码或预览成功扩大生产就绪结论。
