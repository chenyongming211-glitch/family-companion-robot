# Device live control implementation plan

> **For agentic workers:** 使用subagent-driven-development任务实现和独立审查。用户授权main直接提交推送，不建分支或worktree。

**Goal:** 微雪待机也能真实上报在线状态并执行音量/亮度设置，以读回回执确认。
**Architecture:** 既有Go管理服务中的有界RAM设备状态/单命令；两S3共用HTTPS固件轮询；原生小程序显式提交和短轮询。
**Tech Stack:** 现有Go、ESP-IDF6.1/C++、TypeScript/WXML/WXSS，无新产品依赖。

## Global Constraints

- 精确JSON/路径、设备认证、当前家庭授权和管理权限；queued/sent不等于confirmed。
- 服务端在线TTL20秒，命令45秒；固件正常5秒轮询、HTTP5秒超时、失败退避最多30秒；小程序前台10秒刷新和提交后最多观察50秒。
- 两S3公共实现保持一致，退货M5不刷；仅微雪新私有备份后保留槽外升级。
- 运行/测试/build/凭据/固件/备份全部云端；Mac仅Git与后续测试客户端/字节中继。
- 不重启语音服务，不改模型/语音播放窗口/现有绑定和历史，不存原始音频。

### Task 1: 管理设备状态和单命令
- [x] 新建 `internal/management/runtime_test.go`，精确协议正常闭环、权限/激活隔离、无效输入、旧boot/seq、丢回执/过期/重启及审计失败先云端RED。
- [x] 新建 `runtime.go` 和Server有界状态成员，复用authenticateFactory/activated及Authorize/canAdd，GET/PUT家庭接口与POST设备state。
- [x] 云端定向GREEN、完整management/CLI race、vet/build；独立Spec/Quality审查和main提交。

### Task 2: 固件共享轮询
- [x] 在共享固件添加可host测试的协议/命令状态机，先失败用例，再实现严格解析、去重、渐变读回、超时和boot序号。
- [x] 共用network worker复用HTTP客户端，Onboarding就绪事件启动，Application主线程设置/读取；prepare_onboarding和manifest纳入新增共享文件。
- [x] 云端host完整门槛、两S3原生配置/build与共享源一致性验证；独立审查，main提交。
- [x] 首版实机暴露连续请求丢JSON头：真实冻结SDK getter/setter与Worker连续三轮RED→GREEN，最小共享修正后完整158项及两板原生构建/来源核验通过。首版失败证据保留，不计实机控制成功。

### Task 3: 小程序状态与硬件设置
- [x] 先写真实页面在线/离线/unknown、权限/能力禁用、queued/sent等待、confirmed读回、超时及隐藏/迟到/401隔离的RED。
- [x] 扩展Api精确device-runtime路由；首页/详情显示状态，设置加独立音量/亮度滑块和显式按钮；保留昵称/位置表单及原配网。
- [x] 云端完整测试/type/build/WXML检查、官方CLI原生预览，独立审查，main提交。

### Task 4: 发布与微雪升级
- [x] 精确源/产物匹配、云端保护基线/私有备份；隔离候选模拟回执、真实权限读取检查，不给生产创建假心跳。
- [x] 管理兼容升级、Caddy仅新增确切设备state与家庭runtime路由；确认语音PID及配置/绑定/历史保持。
- [x] 核实微雪USB身份、读取当前16MiB私有备份及OTA/分区；只升级活动应用槽并回读，槽外保持，受控启动。
- [x] 实际心跳/待机控制读回及安全恢复原值；更新小程序产物和预览。声音效果/手机现场无回执则明确待验。
- [x] 更新README/验证/交接、main推送及最终工作区核对。
