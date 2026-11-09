# 微雪屏幕扫码配网 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to execute task-by-task.

**Goal:** 微雪首次添加显示匹配设备的小程序码、真实配网状态与云端确认结果。

**Architecture:** 工厂供应私有码位图到NVS；独立Display接口渲染；既有FamilyOnboarding产生状态。保持原协议和绑定权威源。

**Tech Stack:** Python3/Pillow、ESP-IDF6.1、LVGL9、C++、现有微信平台码工具。

## Global Constraints

- 直接main；不创建分支/worktree。S3 only。
- 云端依赖/测试/构建；Mac仅代码/Git与后续测试客户端。
- 不存AppSecret于设备，不写原始claim字符串于NVS；私有图/工厂包/固件不进Git或Mac。
- FRC1 = magic4 + LE16 width280 + LE16 height280 + device_id36 + LE32 CRC32(payload) + packed MSB1黑0白9800字节，总9848字节。
- 只有设备独立云端确认或合法bootstrap恢复才能成功；IP/BLE完成不算成功。
- 微雪专用画面；无动画/导航/新服务。保持普通固件/M5原显示与现有认证。

### Task 1: 私有屏幕码供应

Files: tools/onboarding_mini_code.py、新tools/onboarding_screen_code.py、firmware/tools/provision_factory_nvs.py；对应tools/tests与firmware/tests；工具文档。

Interface: 码工具generate新增可选output_format='screen'（默认image），CLI --format screen；从同次官方图片生成匹配device_id的FRC1。NVS CLI新增--screen-code PATH，仅微雪支持，写family_factory/screen_code blob。

- [ ] 先写云端测试：官方请求尺寸/黑白；screen解码像素/头/CRC/device一致；异常图像/公开路径/取消无部分输出；NVS成功与错ID/CRC/尺寸拒绝。
- [ ] 云端运行新测试，记录预期RED；实现最小供应逻辑，保留默认原图16项行为。
- [ ] 固定Pillow版本并在云端隔离安装；运行原码工具、工厂NVS与新增测试，记录GREEN并提交。

### Task 2: 微雪配网页面和状态接线

Files: firmware/overlays/onboarding/family_onboarding.cc、新屏幕位图解析/UI文件、firmware/tools/prepare_onboarding.py、实际C++/LVGL测试。

Interface: Display新增默认无操作SetOnboardingStatus(title,detail,packed,size)，nullptr title清屏；仅微雪Display override。packed是校验后的280×280 1bit载荷。控制器持有码内存，显示owner拷贝为带固定黑白调色板的原生I1位图，在LVGL锁内更新；任务结束不悬挂指针。状态从既有事件产生，不开放新的认证路径。

- [ ] 写解析/实际UI及控制器状态回归：损坏码/错设备拒绝；待扫码/连Wi-Fi/校时/确认/失败/超时/成功；IP不提前成功，过期/旧回调不覆盖终态。
- [ ] 云端见RED后实现：持久码页面、中文状态、成功后恢复显示；补接准备工具，保持来源清单完整。
- [ ] 云端实际C++/LVGL、onboarding专项、普通显示回归GREEN；范围复审通过并提交。

### Task 3: 原生候选与交付

- [ ] 云端构建微雪/M5候选，检查实际配置/分区/容量/来源及图像缓冲生命周期；合成位图离屏渲染检查尺寸、留白、文字和阶段。
- [ ] 整体复审；记录实现、候选、真实码/刷写/手机验收的分别状态。
- [ ] 更新交接/验证/供应说明，提交推送main并核对远端。
- [ ] 如真实配置与目标设备可用，再按已核对备份/身份/迁移条件准备具体刷写；未满足前不宣称设备已更新。
