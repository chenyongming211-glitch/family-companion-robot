# 当前微雪扫码迁移实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 当前微雪经可回退备份后显示匹配的官方设备码，扫码重新确认仍归原家庭且保留历史权限。

**Architecture:** 复用工厂供应与认领，在工厂记录中关联原绑定；新认证复用原 device_id。Mac USB 只转发字节，云端执行备份、供应和安装。

**Tech Stack:** 现有 Go 管理服务、ESP-IDF 6.1、esptool、pySerial、微信原生小程序。

**最新执行授权与状态：** 完整实刷/读回与BLE启动后，安卓现场暴露的负RSSI解析缺陷已修复，用户确认手机添加成功/微雪回机器人，后台独立confirmed保留原绑定起点。最新状态见[现场闭环验证](../../verification/2026-11-09-waveshare-discovery-fix.md)；安装输入/读回见[实际安装验证](../../verification/2026-11-09-waveshare-full-install.md)。按键电源、再次上电恢复等未验收项单独保留。

## Global Constraints

- 直接在 main 开发、提交和推送，不建分支或 worktree。
- 只迁移当前微雪 ESP32-S3；不改变其他设备、语音算法或模型。
- 保留原 device_id、设备资料、family-home 原绑定及其 2026-11-09 10:05:08 北京时间起点、账号和正文权限。历史文件不搬迁、不删除，不重新开放绑定前记录。
- 云端执行依赖、运行测试、构建、备份、发码和服务部署。Mac 只操作代码/Git、微信客户端以及连接 USB 的内存字节转发；设备备份、密钥、NVS、固件不得落 Mac/Git。用户后续明确要求远程展示码，仅设备码PNG可临时显示于Mac私有目录/Codex，不公开发布或入Git；FRC1及其他私有产物仍仅云端。
- 不写 eFuse；首次刷写前须确认芯片/MAC/容量/安全配置，双份完整 Flash 备份一致且备份可读。
- 刷写使用已验证的 c22b807 微雪完整工厂候选，私有 NVS 最后覆盖；先完整读回验证，再正常启动。

### Task 1: 原绑定迁移供应与确认

**Files:** internal/management/onboarding_store.go、onboarding.go、新迁移测试；cmd/management/main.go、迁移 CLI 及测试；deploy/onboarding/README.md。

**Interfaces:** FactoryDevice 可选 migration_binding_id；migrate-legacy CLI 明确 config/device-id/binding-id/mac/serial/model/device-auth/identities/output，输出仅状态。FactoryBundle 现有九字段不变。

- [x] 先写云端失败行为测试：有效迁移保持设备/绑定/账号完整、旧认证与身份文件只读；错误身份/旧密钥/绑定/家庭/权限、重试/过期/取消/并发和普通供应拒绝旧 MAC。
- [x] 实现最小供应事务、配置校验及同家庭原绑定确认；不得复制权限或自动绑定。
- [x] 云端 RED/GREEN、完整管理与 CLI race、vet、build；独立复审并修正。78顶层/238含子测试，零失败/跳过。
- [x] 提交并推送 main：762e597。

### Task 2: 可回退的 USB 备份与安装准备

**Files:** 必要的 USB 转发工具及使用文档；私有设备数据仅云端。

- [x] 使用官方 pySerial RFC2217 协议，回环监听与 SSH 隧道，检查云端 esptool 原生 USB 复位方式。f2f8db5；云端4测试及修正复审通过。
- [x] 在 ROM 核对目标芯片/MAC/Flash/安全状态；双份完整读取并比较，云端备份配置及受保护摘要。两份各16MiB、逐字节一致，当前活动镜像与分区/选择项校验通过。
- [x] 部署单独管理服务候选；先在云端配置副本供应匹配私有包/真实 develop 设备码/SRP/NVS。后续远程扫码修正已把同一工厂记录选择性预登记正式配置，预登记时未激活，现已独立confirmed，禁止重复供应。
- [x] 构造并验证完整预期镜像，核对候选 manifest/各段与实际 NVS 解码/CRC/身份/SRP/屏幕码。
- [x] 用户通过修正版手机入口/设备码完成当前安卓微雪配网；后台及手机成功、设备回机器人终态一致。
- [x] 远程扫码所需工厂记录已单独预登记，原账号/绑定/权限保持，现场独立确认后新认证已可用；不重复合入。
- [x] 按最新先刷授权复核切换/回退输入，保全最新8保护/312源，只撤目标旧认证后安装；完整旧备份回写演练仍未执行。

### Task 3: 切换、安装与现场验收

- [x] 仅撤销目标旧认证并重载原语音二进制，确认其他身份及源文件保持。PID2957197、同binary、其他三服务PID保持，运行态鉴权检查通过。
- [x] 写完整镜像、完整读回对比后启动；记录实际启动状态，保留回退资料。16MiB写97.5秒/完整读480.0秒，逐字节一致；第二次受控启动观察50.5秒见BLE_INIT及目标配网服务启动，无panic；当时屏幕待现场，现用户确认配网后已回机器人界面，完整视觉验收另列。
- [x] 用户扫码和手机BLE配网；独立云端confirmed后核对原设备ID/家庭/绑定起点和记录权限均保持。仅当前安卓/微雪，不推广其他平台验收。
- [x] 更新交接、验证和计划，复审，main提交推送；明确已实现/已部署/已刷写，当前安卓微雪已现场验证、其他平台待验证。

此前已完成Task1/Task2并因手机不方便后置Task3；最新已完成Task3认证切换、完整安装/读回、实际BLE启动和当前安卓手机独立配网确认。USB与SSH反向通道已关闭。此前旧固件重启证据见[迁移准备](../../verification/2026-11-09-waveshare-legacy-migration.md)，不能把它混作本次新固件启动证据；本次手机结果见[现场闭环](../../verification/2026-11-09-waveshare-discovery-fix.md)。

此前登录更新/扫码404修正曾先选择性预登记factory1、未激活；当前已实际confirmed，原账号ID/新登录设置/家庭保持。禁止重新运行migrate-legacy/provision、重复追加或用早期未激活NVS覆盖当前设备。旧staged/config.json不可整份覆盖正式配置。最新保全及状态见[现场闭环验证](../../verification/2026-11-09-waveshare-discovery-fix.md)；此前安装输入见[完整安装验证](../../verification/2026-11-09-waveshare-full-install.md)，预登记与登录历史分别见[预登记验证](../../verification/2026-11-09-onboarding-registration-fix.md)、[登录验证](../../verification/2026-11-09-family-login-remote-scan.md)。
