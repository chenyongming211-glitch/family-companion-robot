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

- [ ] 先写云端失败行为测试：有效迁移保持设备/绑定/账号完整、旧认证与身份文件只读；错误身份/旧密钥/绑定/家庭/权限、重试/过期/取消/并发和普通供应拒绝旧 MAC。
- [ ] 实现最小供应事务、配置校验及同家庭原绑定确认；不得复制权限或自动绑定。
- [ ] 云端 RED/GREEN、完整管理与 CLI race、vet、build；独立复审并修正。
- [ ] 提交 main。

### Task 2: 可回退的 USB 备份与安装准备

**Files:** 必要的 USB 转发工具及使用文档；私有设备数据仅云端。

- [ ] 使用官方 pySerial RFC2217 协议，回环监听与 SSH 隧道，检查云端 esptool 原生 USB 复位方式。
- [ ] 在 ROM 核对目标芯片/MAC/Flash/安全状态；双份完整读取并比较，云端备份配置及受保护摘要。
- [ ] 验证对应微信版本可打开；部署单独管理服务候选，供应匹配私有包/真实设备码/SRP/NVS。
- [ ] 构造并验证完整预期镜像及可执行回退资料，尚未齐备前不撤旧认证或写 Flash。

### Task 3: 切换、安装与现场验收

- [ ] 仅撤销目标旧认证并重载原语音二进制，确认其他身份及源文件保持。
- [ ] 写完整镜像、完整读回对比后启动；记录屏幕/启动状态，需要时回退。
- [ ] 交用户扫码和手机 BLE 配网；独立云端确认后核对原设备 ID/家庭/绑定起点和记录权限。
- [ ] 更新交接、验证和计划，复审，main 提交推送；明确已实现/已部署/已刷写/手机待验证。
