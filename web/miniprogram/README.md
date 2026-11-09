# 家庭机器人添加设备小程序

微信原生 TypeScript / WXML / WXSS；仅做扫码添加设备。固定请求源 `https://robot.songguoxue.com`，复用家庭账号，测试 AppID `wxb3f1745f8ed1fa1d` 为公开标识。本工程不需要 AppSecret、微信登录或手机号授权；进入微信开发者工具和测试号预览仍需要账号持有者微信扫码登录，并满足平台成员/体验者和能力权限要求。

## 构建与导入

遵守仓库要求，依赖安装、测试、构建只在云端执行：

```sh
export PATH=/home/ubuntu/management-frontend-20261109/node-v24.22.2-linux-x64/bin:$PATH
cd /home/ubuntu/onboarding-20261109/client-final/web/miniprogram
npm ci --ignore-scripts
npm test
npm run typecheck
npm run build
npm audit
```

云端 `dist/` 是可导入原生工程（含 `project.config.json`）。把构建后的 `dist/` 复制到 Mac 测试客户端目录，再在微信开发者工具“导入项目”选择该目录和测试号；不要直接导入源码根目录，不在 Mac 安装依赖或构建。保持合法域名校验开启，并在平台配置 request 合法域名 `https://robot.songguoxue.com`。开发工具无法证明真实手机蓝牙行为；预览需用支持 BLE、安全随机数和 BigInt 的真实微信客户端。旧客户端收到更新微信提示；Security2 独立模块只在能力检查后加载。

入口与延迟加密模块均输出 CommonJS/ES2018 语法，保留字面量 `require` 供微信依赖扫描，避免加密模块被静默过滤。BigInt 仍是运行时能力要求；不能为打包兼容降低安全协议。构建验证还执行编译后的 Security2 官方握手及双向密文向量。

首轮是登录家庭账号→扫码（或启动参数 `scene=<32位小写hex>`）→选家庭（唯一可添加家庭自动选中）→选择附近设备并证明持有码→Wi-Fi 列表/手动输入→等待云端确认。只接受原始 32 位小写 hex 或 `scene=<code>`；不打开任意二维码 URL。账号需由服务端明确授予 `canAddDevices`，不可直接使用微信身份替代家庭账号。

## 安全与传输

- Cookie、认领码和 Wi-Fi 密码只保留内存；不调用 storage、不记录密码。页面隐藏/app 后台清空 Wi-Fi 密码、控制器认领码、安全会话和 BLE；家庭 Cookie/授权列表仍只在 RAM，普通隐藏保留，卸载/退出会话清除。系统扫码有独立操作标记：隐藏时仅可暂存该扫描的已校验结果，保持控制器为空，onShow 后才应用；取消、卸载、重新扫描、账号/会话变更都会清除回调持有的结果。扫描最长60秒，返回页面再次核对绝对截止时间，后台暂停定时器也不能延长有效期。微信真实扫码生命周期仍待实机验证。
- 服务 UUID `1775244d-6b43-439b-877c-060f2d9bed07`；固定官方同基址 UUID：`ff51` session、`ff52` config、`ff50` scan、`ff53` version、首个自定义 `ff54` family-claim。固定对应 `network_provisioning` 1.3.2 提交 `87f3042b89ccb23e791620a283ae4929c229af5a`。
- `proto-ver` 必须是 Security 2 patch 1；SRP 用户名=供应序列号，密码=设备码，必须校验设备证明。安全会话前不发送 Wi-Fi 或 claim。加密 `family-claim` 请求为 `{id,challenge}`，响应必须 `{ok:true}`。
- 官方 protocomm 不能把普通 20 字节写直接当应用分包；本工程与本项目固件 framing v1 配套，保持官方 Security2/protobuf 内容。每次 GATT 写≤20 字节：头 10 字节 `46 52 01 op idLE16 offsetLE16 totalLE16`，最多10字节数据。op1连续请求分片；op2无数据拉取指定响应偏移，随后read；op3响应分片。请求/响应≤481字节，同一连接串行；单步≤10秒、整笔≤30秒，错误身份/偏移/长度或断连关闭。没有明文或原始官方传输降级。proto-ver 使用 `ESP` 请求。
- Wi-Fi 使用官方 `network_config.proto`/`network_scan.proto`；扫描每次最多取4条，最多32条，避免响应超限。写入成功、拿到IP或BLE完成都不显示添加成功；仅服务端 `confirmed` 且合法设备ID允许成功。
- 密码库固定 `@noble/hashes` 2.0.1、`@noble/ciphers` 2.0.1，浏览器纯JS，无 Node crypto/Buffer。仅 Security2 模块使用 BigInt。

## 验证边界

行为测试包含扫码边界、固定源/Cookie/401/网络错误、后台清理、旧任务回调隔离、20字节分包/重组/错误身份/超时/断连、Wi-Fi protobuf及云端确认状态。构建还验证路由文件、WXML事件处理器、原生条件绑定分隔符、实际成功文案的祖先条件在所有阶段中的可见性与无需 BigInt 的首屏模块。绑定验证使用实际 WXML 和打包页面初始数据，包含字面量/恒真条件反例；它是结构与条件求值检查，不能替代微信开发者工具编译和真机渲染。新增回归覆盖取消期间等待资源释放、晚到连接/适配器成功的补偿关闭、隐藏/卸载/取消后的扫描结果隔离、空闲蓝牙断开即时反馈。Security2 互通证据由协议专项负责。

当前源码云端70项测试（0skip）、类型检查、构建及编译后协议向量通过；既有依赖审计0漏洞。本轮按用户参考图统一为蓝紫浅色、扁平插画/步骤图标与局部半透明，未增加导航功能。微信开发者工具 Stable 2.01.2510280 / 基础库3.10.3 已实际冷启动、显示首屏并生成58KB预览包；只过滤 project.private.config.json，加密模块保留。合法域名校验和压缩均开启；合成错误账号实际请求返回401并显示“账号或密码不正确”。该预览码用于打开开发版，不是工厂设备认领码。

注册服务和语音认证桥已发布，管理服务因原生请求兼容更新为r2；见[本批验证](../../docs/verification/2026-11-09-device-onboarding.md)。Mac客户端目录为 `artifacts/miniprogram-onboarding-20261109/dist/`。真实账号登录、测试号BLE权限、Android/iOS、微雪/M5真机配网与真实家庭绑定仍待验收；375px及业务后续阶段的真机视觉检查也未完成。dist不入Git；没有固件刷写、真实工厂供应或AppSecret使用。当前设备未刷入匹配候选与工厂NVS，不能直接作为新流程的可用配网设备。
