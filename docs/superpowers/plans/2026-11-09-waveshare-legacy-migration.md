# 当前微雪扫码迁移实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 当前微雪经可回退备份后显示匹配的官方设备码，扫码重新确认仍归原家庭且保留历史权限。

**Architecture:** 复用工厂供应与认领，在工厂记录中关联原绑定；新认证复用原 device_id。Mac USB 只转发字节，云端执行备份、供应和安装。

**Tech Stack:** 现有 Go 管理服务、ESP-IDF 6.1、esptool、pySerial、微信原生小程序。

## Global Constraints

- 直接在 main 开发、提交和推送，不建分支或 worktree。
- 只迁移当前微雪 ESP32-S3；不改变其他设备、语音算法或模型。
- 保留原 device_id、设备资料、family-home 原绑定及其 2026-11-09 10:05:08 北京时间起点、账号和正文权限。历史文件不搬迁、不删除，不重新开放绑定前记录。
- 云端执行依赖、运行测试、构建、备份、发码和服务部署。Mac 只操作代码/Git、微信客户端以及连接 USB 的内存字节转发；设备备份、密钥、真实设备码、NVS、固件不得落 Mac/Git。
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
- [x] 部署单独管理服务候选；仅在云端配置副本供应匹配私有包/真实 develop 设备码/SRP/NVS，正式工厂权威尚未改变。
- [x] 构造并验证完整预期镜像，核对候选 manifest/各段与实际 NVS 解码/CRC/身份/SRP/屏幕码。
- [ ] 验证对应微信版本可打开。用户回复手机预览暂时不方便，与实刷/重新配网一起后置。
- [ ] 齐备切换及回退门槛后才应用暂存工厂认证、撤目标旧认证或写 Flash。当前只准备回退资料，未实机回写演练。

### Task 3: 切换、安装与现场验收

- [ ] 仅撤销目标旧认证并重载原语音二进制，确认其他身份及源文件保持。
- [ ] 写完整镜像、完整读回对比后启动；记录屏幕/启动状态，需要时回退。
- [ ] 交用户扫码和手机 BLE 配网；独立云端确认后核对原设备 ID/家庭/绑定起点和记录权限。
- [ ] 更新交接、验证和计划，复审，main 提交推送；明确已实现/已部署/已刷写/手机待验证。

本轮已完成 Task1 和 Task2 双备份、安装输入及管理发布准备；代码/集成整体复审通过，阶段文档已更新。用户手机暂时不方便，Task3 不启动；双备份一致后已重新启动未刷写的原固件，实际starting→activating→idle、35.3秒无panic，USB与SSH反向通道关闭。后续继续同一授权，不再询问是否使用当前微雪。阶段证据见[迁移验证](../../verification/2026-11-09-waveshare-legacy-migration.md)。
