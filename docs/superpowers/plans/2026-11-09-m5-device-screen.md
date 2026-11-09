# M5 屏幕扫码与配网完成计划

> **For agentic workers:** Use subagent-driven-development; fresh implementer and independent review. 用户已明确要求扫M5自身二维码，本计划直接执行该补齐，不再要求重复确认。

**Goal:** M5屏幕展示本机码，安卓扫码自动连接、选择Wi-Fi、confirmed加入family-home。

**Architecture:** 复用FRC1/SetupView/Security2及现有factory身份；M5圆屏适配和配网候选NVS增容，保留应用/资产槽。独立只读审计实际BLE超时，再最小修复及实机验收。

**Global Constraints:** main直接开发/提交/推送；无分支/worktree。测试/构建/备份/固件/凭据仅云端，Mac只做代码与USB/微信客户端。保留已confirmed微雪及原家庭/权限/历史，不重复供应M5，不引入C3工作/新依赖/明文配网。

### Task 1: M5圆屏码与出厂布局

Files: `firmware/tools/prepare_onboarding.py`, `provision_factory_nvs.py`, `firmware/overlays/onboarding/family_setup_view.{cc,h}`, `family_onboarding.cc`, focused host tests, firmware README。

- [ ] 先在云端复现缺失M5显示接入、M5屏幕NVS拒绝和小屏布局等RED。对圆屏224px可扫描区/字体/不遮挡、Wave像素保持做有意义的实际渲染/行为检查。
- [ ] 接入M5显示、紧凑码布局、已有状态、NVS0x6000与新ota/phy位置。复用I1和现有字体，身份/CRC/安全协议不变。生成码同设备ID，不把secret或设备码文本写Git。
- [ ] 必要诊断只记录固定安全阶段/长度/状态；按独立BLE审计证据修复实际问题，不推测放宽安全。
- [ ] 云端S3专项host检查与M5原生构建、来源/表/5段/整片与FRC/NVS校验。可增量复用已核对M5编译树，但先冻结原c22全部manifest输入/产物及源码证据；原zip/manifest保持，禁止覆盖现有未核验产物当新结果。
- [ ] 独立规格/质量/整体复审，解决阻断项。

### Task 2: 当前M5更新与现场

- [ ] 当前状态/备份/私有包/新布局/同硬件/有效claim重新核对；必要通过已有完整回读及当前硬件MD5、只读运行元数据重建当前完整回退快照，所有字节有校验。
- [ ] 复用同factory，新FRC/NVS/完整候选一次计划写入和完整回读；不对已绑定或有效用户任务盲目刷写，不整份覆盖管理配置。
- [ ] 启动后核对屏幕码/状态；用户直接扫M5屏幕，保持微信前台完成。后台独立confirmed及用户回执后核对原家庭、新绑定起点、旧历史隔离和微雪保持。
- [ ] 清理临时中继、更新handoff/验证记录，main提交推送并核对origin一致。真实手机/实体屏幕和自动化测试分别记录。
